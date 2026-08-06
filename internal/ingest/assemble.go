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
	"slices"
	"sort"

	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/parquet-go/parquet-go"
)

// ObjectResult describes one fully closed and verified staged lookaside
// object. Path is machine-local staging state; the other fields are durable
// manifest facts.
type ObjectResult struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	Docs         int64  `json:"docs"`
	Tokens       int64  `json:"tokens"`
	LogicalBytes int64  `json:"logical_bytes"`
	RowGroups    int    `json:"row_groups"`
	License      string `json:"license"`
}

type AssemblyResult struct {
	Objects       []ObjectResult `json:"objects"`
	InputDocs     int64          `json:"input_docs"`
	RetainedDocs  int64          `json:"retained_docs"`
	DuplicateDocs int64          `json:"duplicate_docs"`
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
	counter, err := tokenizer.Get(tokenizer.Default)
	if err != nil {
		return AssemblyResult{}, fmt.Errorf("load reference tokenizer: %w", err)
	}
	assembler := objectAssembler{ctx: ctx, plan: plan, directory: objectDirectory, sink: sink, counter: counter}
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
			return AssemblyResult{InputDocs: dedup.input, DuplicateDocs: dedup.input}, nil
		}
		return AssemblyResult{}, fmt.Errorf("ingestion produced no canonical records")
	}
	return AssemblyResult{
		Objects: assembler.results, InputDocs: dedup.input, RetainedDocs: dedup.kept,
		DuplicateDocs: dedup.input - dedup.kept,
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
	counter   tokenizer.Counter
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
	license         string
	lastUsed        int64
}

func (assembler *objectAssembler) addBatch(batch TextBatch) error {
	if assembler.active == nil {
		assembler.active = map[string]*activeObject{}
	}
	for position := range batch.Rows {
		if err := assembler.ctx.Err(); err != nil {
			return err
		}
		row := batch.Rows[position]
		if row.License == "" {
			return fmt.Errorf("canonical row has no effective license")
		}
		active, err := assembler.writerFor(row.License)
		if err != nil {
			return err
		}
		rowBytes := int64(len(row.Text))
		if active.rowGroupLogical > 0 && active.rowGroupLogical+rowBytes > assembler.plan.Writer.RowGroupLogicalBytes {
			if err := assembler.flushRowGroup(active); err != nil {
				return err
			}
			active, err = assembler.writerFor(row.License)
			if err != nil {
				return err
			}
		}
		count := int64(assembler.counter.Count(row.Text))
		row.TokenCount = &count
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

const maximumActiveLicenseWriters = 16

func (assembler *objectAssembler) writerFor(license string) (*activeObject, error) {
	assembler.clock++
	if active := assembler.active[license]; active != nil {
		active.lastUsed = assembler.clock
		return active, nil
	}
	if len(assembler.active) >= maximumActiveLicenseWriters {
		var evict *activeObject
		for _, candidate := range assembler.active {
			if evict == nil || candidate.lastUsed < evict.lastUsed || (candidate.lastUsed == evict.lastUsed && candidate.license < evict.license) {
				evict = candidate
			}
		}
		if err := assembler.finishActive(evict); err != nil {
			return nil, err
		}
	}
	file, err := os.CreateTemp(assembler.directory, ".waldo-shard-*")
	if err != nil {
		return nil, err
	}
	stream := newCountingHashWriter(file)
	active := &activeObject{
		path: file.Name(), file: file, stream: stream,
		writer: shard.NewTextParquetWriter(stream), license: license, lastUsed: assembler.clock,
	}
	assembler.active[license] = active
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
	delete(assembler.active, active.license)
	setAggregateMetadata(active)
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
	result := ObjectResult{
		Path: active.path, SHA256: digest, Bytes: active.stream.n,
		Docs: active.docs, Tokens: active.tokens, LogicalBytes: active.logicalBytes,
		License: active.license,
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
	licenses := make([]string, 0, len(assembler.active))
	for license := range assembler.active {
		licenses = append(licenses, license)
	}
	sort.Strings(licenses)
	for _, license := range licenses {
		if err := assembler.finishActive(assembler.active[license]); err != nil {
			return err
		}
	}
	return nil
}

func setAggregateMetadata(active *activeObject) {
	active.writer.SetKeyValueMetadata("waldo.records", fmt.Sprint(active.docs))
	active.writer.SetKeyValueMetadata("waldo.tokens", fmt.Sprint(active.tokens))
	active.writer.SetKeyValueMetadata("waldo.content_bytes", fmt.Sprint(active.logicalBytes))
	encoded, _ := json.Marshal([]string{active.license})
	active.writer.SetKeyValueMetadata("waldo.licenses", string(encoded))
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
	audited, err := shard.Audit(context.Background(), []string{object.Path})
	if err != nil {
		return 0, fmt.Errorf("audit assembled object: %w", err)
	}
	if audited.Records != object.Docs || audited.Tokens != object.Tokens || audited.ContentBytes != object.LogicalBytes {
		return 0, fmt.Errorf("assembled object audit totals do not match assembly totals")
	}
	if len(audited.Licenses) != 1 || audited.Licenses[0] != object.License {
		return 0, fmt.Errorf("assembled object license partition does not match %q", object.License)
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
