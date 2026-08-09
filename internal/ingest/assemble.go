// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/parquet-go/parquet-go"
)

// ObjectResult describes one fully closed and verified staged lookaside
// object. Path is machine-local staging state; the other fields are durable
// manifest facts.
type ObjectResult struct {
	Path         string                    `json:"path"`
	SHA256       string                    `json:"sha256"`
	Bytes        int64                     `json:"bytes"`
	Docs         int64                     `json:"docs"`
	Tokens       int64                     `json:"tokens"`
	LogicalBytes int64                     `json:"logical_bytes"`
	RowGroups    int                       `json:"row_groups"`
	License      string                    `json:"license"`
	Licenses     []string                  `json:"licenses,omitempty"`
	Sources      []string                  `json:"sources,omitempty"`
	LicenseUsage map[string]index.Measures `json:"license_usage,omitempty"`
}

type AssemblyResult struct {
	Objects       []ObjectResult   `json:"objects"`
	InputDocs     int64            `json:"input_docs"`
	RetainedDocs  int64            `json:"retained_docs"`
	DuplicateDocs int64            `json:"duplicate_docs"`
	RejectedDocs  int64            `json:"rejected_docs,omitempty"`
	Rejections    map[string]int64 `json:"rejections,omitempty"`
}

// DedupSeed streams existing canonical content identities into an update's
// disk-backed exact membership set before any new records are admitted.
type DedupSeed func(func([]DedupIdentity) error) error

// AssembleTextObjects runs the accepted adapters and packs their canonical
// rows into verified Parquet files beneath stagingDirectory/objects. Complete
// objects are content-addressed and safe for a later journaled publication or
// local-admission step.
func AssembleTextObjects(ctx context.Context, plan Plan, stagingDirectory string) (AssemblyResult, error) {
	return AssembleTextObjectsWithSink(ctx, plan, stagingDirectory, nil)
}

// AssembleTextObjectsWithSink delivers each durable object as soon as it is
// closed. A blocking sink deliberately applies backpressure to the encoder.
func AssembleTextObjectsWithSink(ctx context.Context, plan Plan, stagingDirectory string, sink func(ObjectResult) error) (AssemblyResult, error) {
	return AssembleTextObjectsWithSeedAndSink(ctx, plan, stagingDirectory, nil, sink)
}

func AssembleTextObjectsWithSeedAndSink(ctx context.Context, plan Plan, stagingDirectory string, seed DedupSeed, sink func(ObjectResult) error) (AssemblyResult, error) {
	return assembleTextObjectsWithSeedAndSink(ctx, plan, stagingDirectory, seed, 0, sink)
}

func assembleTextObjectsWithSeedAndSink(ctx context.Context, plan Plan, stagingDirectory string, seed DedupSeed, workers int, sink func(ObjectResult) error) (AssemblyResult, error) {
	if err := plan.Validate(); err != nil {
		return AssemblyResult{}, err
	}
	if plan.Mode != "streaming" {
		return AssemblyResult{}, fmt.Errorf("canonical ingestion execution requires the external-sort stage, which is not enabled yet")
	}
	if stagingDirectory == "" {
		return AssemblyResult{}, fmt.Errorf("staging directory is required")
	}
	objectDirectory := filepath.Join(stagingDirectory, "objects")
	if err := os.MkdirAll(objectDirectory, 0o755); err != nil {
		return AssemblyResult{}, err
	}
	dedupPath := filepath.Join(stagingDirectory, "dedup.db")
	if err := os.Remove(dedupPath); err != nil && !os.IsNotExist(err) {
		return AssemblyResult{}, err
	}
	dedup, err := openDeduplicator(dedupPath)
	if err != nil {
		return AssemblyResult{}, err
	}
	defer dedup.database.Close()
	if seed != nil {
		if err := seed(dedup.seedIDs); err != nil {
			return AssemblyResult{}, fmt.Errorf("seed existing corpus identities: %w", err)
		}
	}
	if workers <= 0 {
		workers = min(runtime.GOMAXPROCS(0), 32)
	}
	assembler := objectAssembler{ctx: ctx, plan: plan, directory: objectDirectory, sink: sink, workers: workers}
	err = StreamCanonicalTextBatches(ctx, plan, func(batch TextBatch) error {
		unique, err := dedup.filter(batch)
		if err != nil || len(unique.Rows) == 0 {
			return err
		}
		return assembler.addBatch(unique)
	})
	if err == nil {
		err = assembler.finishAll()
	}
	if err != nil {
		assembler.discardActive()
		return AssemblyResult{}, err
	}
	if len(assembler.results) == 0 {
		if seed != nil && dedup.input > 0 {
			return AssemblyResult{InputDocs: dedup.input + dedup.rejected, DuplicateDocs: dedup.input, RejectedDocs: dedup.rejected, Rejections: dedup.reasons}, nil
		}
		return AssemblyResult{}, fmt.Errorf("ingestion produced no canonical records")
	}
	return AssemblyResult{
		Objects: assembler.results, InputDocs: dedup.input + dedup.rejected, RetainedDocs: dedup.kept,
		DuplicateDocs: dedup.input - dedup.kept, RejectedDocs: dedup.rejected, Rejections: dedup.reasons,
	}, nil
}

type objectAssembler struct {
	ctx       context.Context
	plan      Plan
	directory string
	active    map[string]*activeObject
	clock     int64
	results   []ObjectResult
	sink      func(ObjectResult) error
	workers   int
	counters  []tokenizer.Counter
}

type activeObject struct {
	path            string
	file            *os.File
	stream          *countingHashWriter
	writer          *parquet.GenericWriter[shard.TextRow]
	docs            int64
	tokens          int64
	logicalBytes    int64
	rowGroupLogical int64
	licenses        map[string]bool
	licenseUsage    map[string]index.Measures
	sources         map[string]bool
	lastUsed        int64
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (assembler *objectAssembler) addBatch(batch TextBatch) error {
	if assembler.active == nil {
		assembler.active = map[string]*activeObject{}
	}
	counts, err := assembler.countTokens(batch.Rows)
	if err != nil {
		return err
	}
	for position := range batch.Rows {
		if err := assembler.ctx.Err(); err != nil {
			return err
		}
		row := batch.Rows[position]
		if row.License == "" {
			return fmt.Errorf("canonical row has no effective license")
		}
		active, err := assembler.writerFor()
		if err != nil {
			return err
		}
		rowBytes := int64(len(row.Text))
		if active.rowGroupLogical > 0 && active.rowGroupLogical+rowBytes > assembler.plan.Writer.RowGroupLogicalBytes {
			if err := assembler.flushRowGroup(active); err != nil {
				return err
			}
			active, err = assembler.writerFor()
			if err != nil {
				return err
			}
		}
		count := counts[position]
		row.TokenCount = &count
		active.licenses[row.License] = true
		usage := active.licenseUsage[row.License]
		usage.Docs++
		usage.Tokens += count
		active.licenseUsage[row.License] = usage
		if row.SourceName != nil && *row.SourceName != "" {
			active.sources[*row.SourceName] = true
		}
		if err := shard.ValidateTextRow(row); err != nil {
			return fmt.Errorf("validate canonical ingest row: %w", err)
		}
		active.tokens += count
		if _, err := active.writer.Write([]shard.TextRow{row}); err != nil {
			return err
		}
		active.docs++
		active.logicalBytes += rowBytes
		active.rowGroupLogical += rowBytes
		if active.rowGroupLogical >= assembler.plan.Writer.RowGroupLogicalBytes {
			if err := assembler.flushRowGroup(active); err != nil {
				return err
			}
		}
	}
	return nil
}

func (assembler *objectAssembler) countTokens(rows []shard.TextRow) ([]int64, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	workers := min(len(rows), assembler.workers)
	for len(assembler.counters) < workers {
		var counter tokenizer.Counter
		var err error
		if len(assembler.counters) == 0 {
			counter, err = tokenizer.Get(tokenizer.Default)
		} else {
			counter, err = tokenizer.New(tokenizer.Default)
		}
		if err != nil {
			return nil, fmt.Errorf("load reference tokenizer worker %d: %w", len(assembler.counters)+1, err)
		}
		assembler.counters = append(assembler.counters, counter)
	}
	counts := make([]int64, len(rows))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * len(rows) / workers
		end := (worker + 1) * len(rows) / workers
		counter := assembler.counters[worker]
		group.Add(1)
		go func() {
			defer group.Done()
			for position := start; position < end; position++ {
				counts[position] = int64(counter.Count(rows[position].Text))
			}
		}()
	}
	group.Wait()
	return counts, nil
}

func (assembler *objectAssembler) writerFor() (*activeObject, error) {
	assembler.clock++
	if active := assembler.active[""]; active != nil {
		active.lastUsed = assembler.clock
		return active, nil
	}
	file, err := os.CreateTemp(assembler.directory, ".waldo-shard-*")
	if err != nil {
		return nil, err
	}
	stream := newCountingHashWriter(file)
	active := &activeObject{
		path: file.Name(), file: file, stream: stream,
		writer: shard.NewTextParquetWriter(stream), licenses: map[string]bool{}, licenseUsage: map[string]index.Measures{}, sources: map[string]bool{}, lastUsed: assembler.clock,
	}
	assembler.active[""] = active
	return active, nil
}

func (assembler *objectAssembler) flushRowGroup(active *activeObject) error {
	if active == nil || active.rowGroupLogical == 0 {
		return nil
	}
	if err := active.writer.Flush(); err != nil {
		return err
	}
	active.rowGroupLogical = 0
	// Writer.Size observes completed row groups even when parquet-go's small
	// output buffer has not yet reached the underlying file.
	if active.writer.Size() >= assembler.plan.Writer.CompressedTarget {
		return assembler.finishActive(active)
	}
	return nil
}

func (assembler *objectAssembler) finishActive(active *activeObject) error {
	if active == nil {
		return nil
	}
	delete(assembler.active, "")
	if err := setAggregateMetadata(active, assembler.plan); err != nil {
		_ = active.writer.Close()
		_ = active.file.Close()
		_ = os.Remove(active.path)
		return err
	}
	if err := active.writer.Close(); err != nil {
		_ = active.file.Close()
		_ = os.Remove(active.path)
		return err
	}
	if err := active.file.Sync(); err != nil {
		_ = active.file.Close()
		_ = os.Remove(active.path)
		return err
	}
	if err := active.file.Close(); err != nil {
		_ = os.Remove(active.path)
		return err
	}
	digest := hex.EncodeToString(active.stream.hasher.Sum(nil))
	licenses := sortedKeys(active.licenses)
	result := ObjectResult{
		Path: active.path, SHA256: digest, Bytes: active.stream.n,
		Docs: active.docs, Tokens: active.tokens, LogicalBytes: active.logicalBytes,
		Licenses: licenses, Sources: sortedKeys(active.sources), LicenseUsage: active.licenseUsage,
	}
	if len(licenses) == 1 {
		result.License = licenses[0]
	}
	if result.Bytes > assembler.plan.Writer.CompressedMaximum {
		_ = os.Remove(active.path)
		return fmt.Errorf("encoded shard %s is %d bytes; maximum is %d bytes", digest, result.Bytes, assembler.plan.Writer.CompressedMaximum)
	}
	rowGroups, err := verifyAssembledObject(result)
	if err != nil {
		_ = os.Remove(active.path)
		return err
	}
	result.RowGroups = rowGroups
	destination := filepath.Join(assembler.directory, digest)
	if _, err := os.Stat(destination); err == nil {
		existing := result
		existing.Path = destination
		if _, err := verifyAssembledObject(existing); err != nil {
			_ = os.Remove(active.path)
			return fmt.Errorf("existing staged object %s is invalid: %w", digest, err)
		}
		if err := os.Remove(active.path); err != nil {
			return err
		}
		result.Path = destination
	} else if !os.IsNotExist(err) {
		_ = os.Remove(active.path)
		return err
	} else if err := os.Rename(active.path, destination); err != nil {
		_ = os.Remove(active.path)
		return err
	} else {
		result.Path = destination
	}
	if err := syncDirectory(assembler.directory); err != nil {
		return err
	}
	assembler.results = append(assembler.results, result)
	emitProgress(assembler.ctx, ProgressEvent{Phase: "shard", Status: "ready", Shard: result.SHA256, Sequence: len(assembler.results), Bytes: result.Bytes})
	if assembler.sink != nil {
		if err := assembler.sink(result); err != nil {
			return err
		}
	}
	return nil
}

func (assembler *objectAssembler) finishAll() error {
	keys := make([]string, 0, len(assembler.active))
	for key := range assembler.active {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := assembler.finishActive(assembler.active[key]); err != nil {
			return err
		}
	}
	return nil
}

func setAggregateMetadata(active *activeObject, plan Plan) error {
	active.writer.SetKeyValueMetadata("waldo.records", fmt.Sprint(active.docs))
	active.writer.SetKeyValueMetadata("waldo.tokens", fmt.Sprint(active.tokens))
	active.writer.SetKeyValueMetadata("waldo.content_bytes", fmt.Sprint(active.logicalBytes))
	licenses := sortedKeys(active.licenses)
	encoded, _ := json.Marshal(licenses)
	active.writer.SetKeyValueMetadata("waldo.licenses", string(encoded))
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	bom, err := shard.EncodeBOM(shard.NewBOM(identity, tokenizer.Default, active.docs, active.tokens, active.logicalBytes, licenses))
	if err != nil {
		return err
	}
	active.writer.SetKeyValueMetadata(shard.BOMMetadataKey, bom)
	return nil
}

func (assembler *objectAssembler) discardActive() {
	for _, active := range assembler.active {
		_ = active.writer.Close()
		_ = active.file.Close()
		_ = os.Remove(active.path)
	}
	assembler.active = nil
}

func verifyAssembledObject(object ObjectResult) (int, error) {
	file, err := os.Open(object.Path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() != object.Bytes {
		return 0, fmt.Errorf("assembled object size is %d, want %d", info.Size(), object.Bytes)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return 0, err
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != object.SHA256 {
		return 0, fmt.Errorf("assembled object hash is %s, want %s", got, object.SHA256)
	}
	parquetFile, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		return 0, err
	}
	if parquetFile.NumRows() != object.Docs {
		return 0, fmt.Errorf("assembled object has %d rows, want %d", parquetFile.NumRows(), object.Docs)
	}
	wantColumns := []string{"content_sha256", "text", "source", "source_name", "license", "license_raw", "language", "language_score", "date", "token_count", "meta"}
	columns := parquetFile.Schema().Columns()
	gotColumns := make([]string, len(columns))
	for index, column := range columns {
		if len(column) != 1 {
			return 0, fmt.Errorf("assembled object contains nested canonical column %v", column)
		}
		gotColumns[index] = column[0]
	}
	if !slices.Equal(gotColumns, wantColumns) {
		return 0, fmt.Errorf("assembled object columns are %v", gotColumns)
	}
	if value, ok := parquetFile.Lookup("waldo.record_schema"); !ok || value != fmt.Sprint(shard.TextRecordSchema) {
		return 0, fmt.Errorf("assembled object has invalid record schema metadata")
	}
	if value, ok := parquetFile.Lookup("waldo.recipe"); !ok || value != shard.TextWriterRecipe {
		return 0, fmt.Errorf("assembled object has invalid writer recipe metadata")
	}
	audited, err := shard.VerifyWithOptions(context.Background(), []string{object.Path}, shard.AuditOptions{Workers: 1})
	if err != nil {
		return 0, fmt.Errorf("verify assembled object attestation: %w", err)
	}
	if audited.Attested != 1 || audited.DeepScanned != 0 {
		return 0, fmt.Errorf("assembled object is missing its ingest attestation")
	}
	if audited.Records != object.Docs || audited.Tokens != object.Tokens || audited.ContentBytes != object.LogicalBytes {
		return 0, fmt.Errorf("assembled object audit totals do not match assembly totals")
	}
	expectedLicenses := object.Licenses
	if len(expectedLicenses) == 0 && object.License != "" {
		expectedLicenses = []string{object.License}
	}
	if !slices.Equal(audited.Licenses, expectedLicenses) {
		return 0, fmt.Errorf("assembled object licenses do not match %v", expectedLicenses)
	}
	return len(parquetFile.RowGroups()), nil
}

type countingHashWriter struct {
	destination io.Writer
	hasher      hash.Hash
	n           int64
}

func newCountingHashWriter(destination io.Writer) *countingHashWriter {
	hasher := sha256.New()
	return &countingHashWriter{destination: io.MultiWriter(destination, hasher), hasher: hasher}
}

func (writer *countingHashWriter) Write(data []byte) (int, error) {
	written, err := writer.destination.Write(data)
	writer.n += int64(written)
	return written, err
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
