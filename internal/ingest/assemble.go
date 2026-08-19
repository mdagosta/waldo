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
	"strings"
	"sync"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/parquet-go/parquet-go"
)

// ObjectResult describes one fully closed and verified staged lookaside
// object. Path is machine-local staging state; the other fields are durable
// manifest facts.
type ObjectResult struct {
	Path                      string                    `json:"path"`
	RecordKind                string                    `json:"record_kind"`
	RecordSchema              int                       `json:"record_schema"`
	WriterRecipe              string                    `json:"writer_recipe"`
	SHA256                    string                    `json:"sha256"`
	Bytes                     int64                     `json:"bytes"`
	Docs                      int64                     `json:"docs"`
	Tokens                    int64                     `json:"tokens"`
	LogicalBytes              int64                     `json:"logical_bytes"`
	RowGroups                 int                       `json:"row_groups"`
	License                   string                    `json:"license"`
	Licenses                  []string                  `json:"licenses,omitempty"`
	Sources                   []string                  `json:"sources,omitempty"`
	LicenseUsage              map[string]index.Measures `json:"license_usage,omitempty"`
	EmailAddressRecords       int64                     `json:"email_address_records"`
	RepetitiveContentRecords  int64                     `json:"repetitive_content_records"`
	BoilerplateContentRecords int64                     `json:"boilerplate_content_records"`
	Redaction                 index.ContentRedaction    `json:"redaction"`
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
	verification := newObjectVerificationPipeline(ctx, objectDirectory, workers, sink, verifyAndStageObject)
	assembler := objectAssembler{ctx: verification.ctx, plan: plan, directory: objectDirectory, sink: verification.enqueue, workers: workers}
	err = StreamCanonicalTextBatches(verification.ctx, plan, func(batch TextBatch) error {
		redacted, redactErr := redactCanonicalBatch(plan.Writer.RecordKind, batch)
		if redactErr != nil {
			return redactErr
		}
		batch = redacted
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
		verification.cancel()
		assembler.discardActive()
	}
	objects, verificationErr := verification.closeAndWait()
	if verificationErr != nil {
		return AssemblyResult{}, verificationErr
	}
	if err != nil {
		return AssemblyResult{}, err
	}
	if len(objects) == 0 {
		if seed != nil && dedup.input > 0 {
			result := AssemblyResult{InputDocs: dedup.input + dedup.rejected, DuplicateDocs: dedup.input, RejectedDocs: dedup.rejected, Rejections: dedup.reasons}
			emitIngestCompleted(ctx, plan, result, 0)
			return result, nil
		}
		return AssemblyResult{}, fmt.Errorf("ingestion produced no canonical records")
	}
	result := AssemblyResult{
		Objects: objects, InputDocs: dedup.input + dedup.rejected, RetainedDocs: dedup.kept,
		DuplicateDocs: dedup.input - dedup.kept, RejectedDocs: dedup.rejected, Rejections: dedup.reasons,
	}
	var tokens int64
	for _, object := range objects {
		tokens += object.Tokens
	}
	emitIngestCompleted(ctx, plan, result, tokens)
	return result, nil
}

func emitIngestCompleted(ctx context.Context, plan Plan, result AssemblyResult, tokens int64) {
	var bytes int64
	for _, input := range plan.Inputs {
		bytes += input.Artifact.Bytes
	}
	emitProgress(ctx, ProgressEvent{
		Phase: "ingest", Status: "completed", Bytes: bytes, TotalBytes: bytes,
		Files: int64(len(plan.Inputs)), TotalFiles: int64(len(plan.Inputs)), Docs: result.RetainedDocs, Tokens: tokens,
	})
}

type objectAssembler struct {
	ctx            context.Context
	plan           Plan
	directory      string
	active         map[string]*activeObject
	clock          int64
	sink           func(ObjectResult) error
	workers        int
	counters       []tokenizer.Counter
	progressDocs   int64
	progressTokens int64
}

type activeObject struct {
	path                      string
	file                      *os.File
	stream                    *countingHashWriter
	writer                    *parquet.GenericWriter[shard.TextRow]
	docs                      int64
	tokens                    int64
	logicalBytes              int64
	rowGroupLogical           int64
	licenses                  map[string]bool
	licenseUsage              map[string]index.Measures
	sources                   map[string]bool
	emailAddressRecords       int64
	repetitiveContentRecords  int64
	boilerplateContentRecords int64
	redaction                 index.ContentRedaction
	lastUsed                  int64
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
		redaction := privacyRedactionForRow(row)
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
		assessmentText := row.Text
		if assembler.plan.Writer.RecordKind == record.KindConversation {
			conversation, err := record.DecodeConversation(row.Text)
			if err != nil {
				return err
			}
			var parts []string
			for _, message := range conversation.Messages {
				parts = append(parts, message.Content)
				if message.Context != "" {
					parts = append(parts, message.Context)
				}
			}
			assessmentText = strings.Join(parts, "\n")
		}
		assessment := assessContent(assessmentText)
		_, remaining, err := redactPrivacy(assessmentText)
		if err != nil {
			return fmt.Errorf("verify canonical privacy redaction: %w", err)
		}
		if !remaining.empty() {
			return fmt.Errorf("canonical privacy redaction invariant failed (%s)", remaining.counts())
		}
		row.EmailAddresses = assessment.EmailAddresses
		row.RepetitiveContent = assessment.RepetitiveContent
		row.BoilerplateContent = assessment.BoilerplateContent
		if row.EmailAddresses {
			active.emailAddressRecords++
		}
		if row.RepetitiveContent {
			active.repetitiveContentRecords++
		}
		if row.BoilerplateContent {
			active.boilerplateContentRecords++
		}
		active.redaction.EmailAddresses += redaction.EmailAddresses
		active.redaction.IPAddresses += redaction.IPAddresses
		active.redaction.PhoneNumbers += redaction.PhoneNumbers
		active.redaction.MailRoutingHeaders += redaction.MailRoutingHeaders
		active.redaction.Credentials += redaction.Credentials
		active.licenses[row.License] = true
		usage := active.licenseUsage[row.License]
		usage.Docs++
		usage.Tokens += count
		active.licenseUsage[row.License] = usage
		if row.SourceName != nil && *row.SourceName != "" {
			active.sources[*row.SourceName] = true
		}
		validate := shard.ValidateTextRow
		if assembler.plan.Writer.RecordKind == record.KindConversation {
			validate = shard.ValidateConversationRow
		}
		if err := validate(row); err != nil {
			return fmt.Errorf("validate canonical ingest row: %w", err)
		}
		active.tokens += count
		assembler.progressDocs++
		assembler.progressTokens += count
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
	emitProgress(assembler.ctx, ProgressEvent{Phase: "ingest", Status: "records", Docs: assembler.progressDocs, Tokens: assembler.progressTokens})
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
	writer := shard.NewTextParquetWriter(stream)
	if assembler.plan.Writer.RecordKind == record.KindConversation {
		writer = shard.NewConversationParquetWriter(stream)
	}
	active := &activeObject{
		path: file.Name(), file: file, stream: stream,
		writer: writer, licenses: map[string]bool{}, licenseUsage: map[string]index.Measures{}, sources: map[string]bool{}, lastUsed: assembler.clock,
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
		RecordKind: assembler.plan.Writer.RecordKind, RecordSchema: assembler.plan.Writer.RecordSchema, WriterRecipe: assembler.plan.Writer.Recipe,
		Docs: active.docs, Tokens: active.tokens, LogicalBytes: active.logicalBytes,
		Licenses: licenses, Sources: sortedKeys(active.sources), LicenseUsage: active.licenseUsage,
		EmailAddressRecords:       active.emailAddressRecords,
		RepetitiveContentRecords:  active.repetitiveContentRecords,
		BoilerplateContentRecords: active.boilerplateContentRecords,
		Redaction:                 active.redaction,
	}
	result.Redaction.Policy = shard.PrivacyRedactionPolicy
	result.Redaction.NamesRetained = true
	if len(licenses) == 1 {
		result.License = licenses[0]
	}
	if result.Bytes > assembler.plan.Writer.CompressedMaximum {
		_ = os.Remove(active.path)
		return fmt.Errorf("encoded shard %s is %d bytes; maximum is %d bytes", digest, result.Bytes, assembler.plan.Writer.CompressedMaximum)
	}
	if assembler.sink != nil {
		if err := assembler.sink(result); err != nil {
			return err
		}
	}
	return nil
}

type objectVerificationJob struct {
	sequence int
	object   ObjectResult
}

type objectVerificationOutcome struct {
	job    objectVerificationJob
	object ObjectResult
	err    error
}

type objectVerificationCompletion struct {
	objects []ObjectResult
	err     error
}

type objectVerificationPipeline struct {
	ctx       context.Context
	cancel    context.CancelFunc
	jobs      chan objectVerificationJob
	outcomes  chan objectVerificationOutcome
	done      chan objectVerificationCompletion
	workers   sync.WaitGroup
	sequence  int
	directory string
}

func newObjectVerificationPipeline(parent context.Context, directory string, workers int, sink func(ObjectResult) error, verify func(string, ObjectResult) (ObjectResult, error)) *objectVerificationPipeline {
	ctx, cancel := context.WithCancel(parent)
	pipeline := &objectVerificationPipeline{
		ctx: ctx, cancel: cancel, directory: directory,
		jobs: make(chan objectVerificationJob, workers), outcomes: make(chan objectVerificationOutcome, workers), done: make(chan objectVerificationCompletion, 1),
	}
	for worker := 1; worker <= workers; worker++ {
		pipeline.workers.Add(1)
		go func(worker int) {
			defer pipeline.workers.Done()
			for job := range pipeline.jobs {
				emitProgress(parent, ProgressEvent{Phase: "audit", Status: "started", Shard: job.object.SHA256, Sequence: job.sequence, Worker: worker, TotalBytes: job.object.Bytes})
				object, err := verify(directory, job.object)
				if err != nil {
					_ = os.Remove(job.object.Path)
					pipeline.cancel()
				}
				pipeline.outcomes <- objectVerificationOutcome{job: job, object: object, err: err}
			}
		}(worker)
	}
	go pipeline.collect(parent, sink)
	return pipeline
}

func (pipeline *objectVerificationPipeline) enqueue(object ObjectResult) error {
	pipeline.sequence++
	job := objectVerificationJob{sequence: pipeline.sequence, object: object}
	emitProgress(pipeline.ctx, ProgressEvent{Phase: "audit", Status: "queued", Shard: object.SHA256, Sequence: job.sequence, TotalBytes: object.Bytes})
	select {
	case pipeline.jobs <- job:
		return nil
	case <-pipeline.ctx.Done():
		_ = os.Remove(object.Path)
		return pipeline.ctx.Err()
	}
}

func (pipeline *objectVerificationPipeline) collect(progressContext context.Context, sink func(ObjectResult) error) {
	pending := map[int]objectVerificationOutcome{}
	objects := make([]ObjectResult, 0)
	next := 1
	var firstErr error
	for outcome := range pipeline.outcomes {
		pending[outcome.job.sequence] = outcome
		for {
			current, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if current.err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("audit assembled shard %s: %w", current.job.object.SHA256[:12], current.err)
				}
			} else if firstErr == nil {
				emitProgress(progressContext, ProgressEvent{Phase: "audit", Status: "completed", Shard: current.object.SHA256, Sequence: next, Bytes: current.object.Bytes, TotalBytes: current.object.Bytes})
				emitProgress(progressContext, ProgressEvent{Phase: "shard", Status: "ready", Shard: current.object.SHA256, Sequence: next, Bytes: current.object.Bytes})
				if sink != nil {
					if err := sink(current.object); err != nil {
						firstErr = err
						pipeline.cancel()
					}
				}
				if firstErr == nil {
					objects = append(objects, current.object)
				}
			}
			next++
		}
	}
	pipeline.done <- objectVerificationCompletion{objects: objects, err: firstErr}
}

func (pipeline *objectVerificationPipeline) closeAndWait() ([]ObjectResult, error) {
	close(pipeline.jobs)
	pipeline.workers.Wait()
	close(pipeline.outcomes)
	completion := <-pipeline.done
	pipeline.cancel()
	return completion.objects, completion.err
}

func verifyAndStageObject(directory string, result ObjectResult) (ObjectResult, error) {
	rowGroups, err := verifyAssembledObject(result)
	if err != nil {
		return ObjectResult{}, err
	}
	result.RowGroups = rowGroups
	destination := filepath.Join(directory, result.SHA256)
	if _, err := os.Stat(destination); err == nil {
		existing := result
		existing.Path = destination
		if _, err := verifyAssembledObject(existing); err != nil {
			return ObjectResult{}, fmt.Errorf("existing staged object %s is invalid: %w", result.SHA256, err)
		}
		if err := os.Remove(result.Path); err != nil {
			return ObjectResult{}, err
		}
		result.Path = destination
	} else if !os.IsNotExist(err) {
		return ObjectResult{}, err
	} else if err := os.Rename(result.Path, destination); err != nil {
		return ObjectResult{}, err
	} else {
		result.Path = destination
	}
	if err := syncDirectory(directory); err != nil {
		return ObjectResult{}, err
	}
	return result, nil
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
	active.redaction.Policy = shard.PrivacyRedactionPolicy
	active.redaction.NamesRetained = true
	active.writer.SetKeyValueMetadata("waldo.records", fmt.Sprint(active.docs))
	active.writer.SetKeyValueMetadata("waldo.tokens", fmt.Sprint(active.tokens))
	active.writer.SetKeyValueMetadata("waldo.content_bytes", fmt.Sprint(active.logicalBytes))
	active.writer.SetKeyValueMetadata("waldo.email_address_records", fmt.Sprint(active.emailAddressRecords))
	active.writer.SetKeyValueMetadata("waldo.repetitive_content_records", fmt.Sprint(active.repetitiveContentRecords))
	active.writer.SetKeyValueMetadata("waldo.boilerplate_content_records", fmt.Sprint(active.boilerplateContentRecords))
	active.writer.SetKeyValueMetadata("waldo.privacy_redaction_policy", shard.PrivacyRedactionPolicy)
	active.writer.SetKeyValueMetadata("waldo.redacted_email_addresses", fmt.Sprint(active.redaction.EmailAddresses))
	active.writer.SetKeyValueMetadata("waldo.redacted_ip_addresses", fmt.Sprint(active.redaction.IPAddresses))
	active.writer.SetKeyValueMetadata("waldo.redacted_phone_numbers", fmt.Sprint(active.redaction.PhoneNumbers))
	active.writer.SetKeyValueMetadata("waldo.removed_mail_routing_headers", fmt.Sprint(active.redaction.MailRoutingHeaders))
	active.writer.SetKeyValueMetadata("waldo.redacted_credentials", fmt.Sprint(active.redaction.Credentials))
	licenses := sortedKeys(active.licenses)
	encoded, _ := json.Marshal(licenses)
	active.writer.SetKeyValueMetadata("waldo.licenses", string(encoded))
	identity, err := plan.Identity()
	if err != nil {
		return err
	}
	shardBOM := shard.NewBOMForRecord(identity, plan.Writer.RecordKind, plan.Writer.RecordSchema, plan.Writer.Recipe, tokenizer.Default, active.docs, active.tokens, active.logicalBytes, licenses)
	shardBOM.EmailAddressRecords = active.emailAddressRecords
	shardBOM.RepetitiveContentRecords = active.repetitiveContentRecords
	shardBOM.BoilerplateContentRecords = active.boilerplateContentRecords
	shardBOM.Redaction = active.redaction
	bom, err := shard.EncodeBOM(shardBOM)
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
	wantColumns := []string{"content_sha256", "text", "source", "source_name", "license", "license_raw", "language", "language_score", "date", "token_count", "meta", "email_addresses", "repetitive_content", "boilerplate_content", "main_content", "redacted_email_addresses", "redacted_ip_addresses", "redacted_phone_numbers", "removed_mail_routing_headers", "redacted_credentials"}
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
	if value, ok := parquetFile.Lookup("waldo.record_schema"); !ok || value != fmt.Sprint(object.RecordSchema) {
		return 0, fmt.Errorf("assembled object has invalid record schema metadata")
	}
	if value, ok := parquetFile.Lookup("waldo.record_kind"); !ok || value != object.RecordKind {
		return 0, fmt.Errorf("assembled object has invalid record kind metadata")
	}
	if value, ok := parquetFile.Lookup("waldo.recipe"); !ok || value != object.WriterRecipe {
		return 0, fmt.Errorf("assembled object has invalid writer recipe metadata")
	}
	audited, err := shard.VerifyWithOptions(context.Background(), []string{object.Path}, shard.AuditOptions{Workers: 1})
	if err != nil {
		return 0, fmt.Errorf("verify assembled object attestation: %w", err)
	}
	if audited.Attested != 1 || audited.DeepScanned != 0 {
		return 0, fmt.Errorf("assembled object is missing its ingest attestation")
	}
	if audited.Records != object.Docs || audited.Tokens != object.Tokens || audited.ContentBytes != object.LogicalBytes || audited.EmailAddressRecords != object.EmailAddressRecords || audited.RepetitiveContentRecords != object.RepetitiveContentRecords || audited.BoilerplateContentRecords != object.BoilerplateContentRecords {
		return 0, fmt.Errorf("assembled object audit totals do not match assembly totals")
	}
	if audited.Redaction != object.Redaction {
		return 0, fmt.Errorf("assembled object redaction totals do not match assembly totals")
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
