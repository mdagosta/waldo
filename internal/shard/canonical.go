// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/parquet-go/parquet-go"
	"go.etcd.io/bbolt"
)

var canonicalColumns = []string{"content_sha256", "text", "source", "source_name", "license", "license_raw", "language", "language_score", "date", "token_count", "meta"}
var legacyColumns = []string{"sha256", "kind", "text", "source", "source_name", "license", "license_raw", "lang", "lang_score", "date", "tokens", "meta"}

type RecordView struct {
	ID       string `json:"id"`
	Text     string `json:"-"`
	Source   string `json:"source"`
	License  string `json:"license"`
	Language string `json:"language,omitempty"`
	Tokens   int64  `json:"tokens"`
	Bytes    int64  `json:"bytes"`
}

type Summary struct {
	Shards       int64    `json:"shards"`
	Attested     int64    `json:"attested_shards,omitempty"`
	DeepScanned  int64    `json:"deep_scanned_shards,omitempty"`
	Records      int64    `json:"records"`
	Tokens       int64    `json:"tokens"`
	ContentBytes int64    `json:"content_bytes"`
	EncodedBytes int64    `json:"encoded_bytes"`
	RowGroups    int64    `json:"row_groups"`
	Licenses     []string `json:"licenses"`
	Recipes      []string `json:"writer_recipes"`
}

type AuditOptions struct {
	// Workers controls concurrent shard scans. Zero selects a conservative
	// automatic value capped at four to bound decompression and text memory.
	Workers  int
	Progress func(AuditProgress)
}

type AuditProgress struct {
	Current int
	Total   int
	Path    string
	Summary Summary
}

func ResolvePaths(arguments []string) ([]string, error) {
	if len(arguments) == 0 {
		return nil, fmt.Errorf("at least one shard path is required")
	}
	seen := map[string]bool{}
	var paths []string
	add := func(path string) error {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if !seen[absolute] {
			seen[absolute] = true
			paths = append(paths, absolute)
		}
		return nil
	}
	for _, argument := range arguments {
		matches, err := filepath.Glob(argument)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", argument, err)
		}
		if len(matches) == 0 {
			matches = []string{argument}
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, err
			}
			if !info.IsDir() {
				if err := add(match); err != nil {
					return nil, err
				}
				continue
			}
			err = filepath.WalkDir(match, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".parquet") {
					return add(path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no Parquet shard files found")
	}
	return paths, nil
}

func Summarize(paths []string) (Summary, error) {
	licenses, recipes := map[string]bool{}, map[string]bool{}
	var total Summary
	for _, path := range paths {
		file, parquetFile, size, err := openShard(path)
		if err != nil {
			return Summary{}, err
		}
		one, complete := footerSummary(parquetFile, size)
		if !complete {
			one, err = scan(parquetFile, false, nil)
			one.EncodedBytes = size
		}
		file.Close()
		if err != nil {
			return Summary{}, fmt.Errorf("%s: %w", path, err)
		}
		total.Shards++
		total.Records += one.Records
		total.Tokens += one.Tokens
		total.ContentBytes += one.ContentBytes
		total.EncodedBytes += size
		total.RowGroups += int64(len(parquetFile.RowGroups()))
		for _, v := range one.Licenses {
			licenses[v] = true
		}
		for _, v := range one.Recipes {
			recipes[v] = true
		}
	}
	total.Licenses = keys(licenses)
	total.Recipes = keys(recipes)
	return total, nil
}

func Audit(ctx context.Context, paths []string) (Summary, error) {
	return AuditWithOptions(ctx, paths, AuditOptions{})
}

// VerifyWithOptions verifies builder-attested shards from their Parquet
// footer. Shards without a recognized ingest attestation fall back to a deep
// record scan; already attested content is never re-tokenized.
func VerifyWithOptions(ctx context.Context, paths []string, options AuditOptions) (Summary, error) {
	workers := options.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers > 16 {
			workers = 16
		}
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	if workers == 0 {
		return Summary{}, nil
	}
	type result struct {
		index   int
		summary Summary
		err     error
	}
	auditContext, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	results := make(chan result, workers)
	var wait sync.WaitGroup
	worker := func() {
		defer wait.Done()
		var counter tokenizer.Counter
		for index := range jobs {
			one, err := verifyAttestedOne(paths[index])
			if errors.Is(err, errDeepScanRequired) {
				if counter == nil {
					counter, err = tokenizer.New(tokenizer.Default)
				}
				if err == nil {
					one, err = auditOne(auditContext, paths[index], counter, func(string) error { return nil })
				}
			}
			results <- result{index: index, summary: one, err: err}
			if err != nil {
				cancel()
				return
			}
		}
	}
	wait.Add(workers)
	for range workers {
		go worker()
	}
	go func() {
		defer close(jobs)
		for index := range paths {
			select {
			case jobs <- index:
			case <-auditContext.Done():
				return
			}
		}
	}()
	go func() {
		wait.Wait()
		close(results)
	}()
	licenses, recipes := map[string]bool{}, map[string]bool{}
	var total Summary
	completed := 0
	var verifyErr error
	for result := range results {
		if result.err != nil {
			if verifyErr == nil || errors.Is(verifyErr, context.Canceled) {
				verifyErr = fmt.Errorf("%s: %w", paths[result.index], result.err)
			}
			continue
		}
		addSummary(&total, result.summary, licenses, recipes)
		completed++
		if options.Progress != nil {
			progress := total
			progress.Licenses, progress.Recipes = keys(licenses), keys(recipes)
			options.Progress(AuditProgress{Current: completed, Total: len(paths), Path: paths[result.index], Summary: progress})
		}
	}
	if verifyErr != nil {
		return Summary{}, verifyErr
	}
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	total.Licenses, total.Recipes = keys(licenses), keys(recipes)
	return total, nil
}

var errDeepScanRequired = errors.New("shard has no recognized ingest attestation")

func verifyAttestedOne(path string) (Summary, error) {
	file, parquetFile, size, err := openShard(path)
	if err != nil {
		return Summary{}, err
	}
	defer file.Close()
	one, complete := footerSummary(parquetFile, size)
	if !complete || parquetFile.NumRows() != one.Records {
		return Summary{}, errDeepScanRequired
	}
	recipe, _ := parquetFile.Lookup("waldo.recipe")
	switch recipe {
	case TextWriterRecipe:
		if _, ok := parquetFile.Lookup(BOMMetadataKey); !ok {
			return Summary{}, errDeepScanRequired
		}
		bom, err := ReadBOM(parquetFile)
		if err != nil {
			return Summary{}, err
		}
		if bom.Records != one.Records || bom.Tokens != one.Tokens || bom.ContentBytes != one.ContentBytes || !slices.Equal(bom.Licenses, one.Licenses) {
			return Summary{}, fmt.Errorf("embedded shard BOM differs from Parquet footer aggregates")
		}
	case FormerTextRecipe:
		// Writer v4 performed a complete record/hash/token audit before its
		// object hash was published, but predates the explicit BOM metadata.
	default:
		return Summary{}, errDeepScanRequired
	}
	one.Shards = 1
	one.Attested = 1
	return one, nil
}

func addSummary(total *Summary, one Summary, licenses, recipes map[string]bool) {
	total.Shards += one.Shards
	total.Attested += one.Attested
	total.DeepScanned += one.DeepScanned
	total.Records += one.Records
	total.Tokens += one.Tokens
	total.ContentBytes += one.ContentBytes
	total.EncodedBytes += one.EncodedBytes
	total.RowGroups += one.RowGroups
	for _, value := range one.Licenses {
		licenses[value] = true
	}
	for _, value := range one.Recipes {
		recipes[value] = true
	}
}

func AuditWithOptions(ctx context.Context, paths []string, options AuditOptions) (Summary, error) {
	dedupFile, err := os.CreateTemp("", "waldo-shard-audit-*.db")
	if err != nil {
		return Summary{}, err
	}
	dedupPath := dedupFile.Name()
	if err := dedupFile.Close(); err != nil {
		return Summary{}, err
	}
	_ = os.Remove(dedupPath)
	defer os.Remove(dedupPath)
	database, err := bbolt.Open(dedupPath, 0o600, nil)
	if err != nil {
		return Summary{}, err
	}
	defer database.Close()
	transaction, err := database.Begin(true)
	if err != nil {
		return Summary{}, err
	}
	defer transaction.Rollback()
	seen, err := transaction.CreateBucket([]byte("records"))
	if err != nil {
		return Summary{}, err
	}
	workers := options.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers > 4 {
			workers = 4
		}
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	if workers == 0 {
		return Summary{}, nil
	}

	type auditResult struct {
		index   int
		summary Summary
		err     error
	}
	auditContext, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	results := make(chan auditResult, workers)
	var dedupMutex sync.Mutex
	var wait sync.WaitGroup
	worker := func() {
		defer wait.Done()
		counter, counterErr := tokenizer.New(tokenizer.Default)
		if counterErr != nil {
			results <- auditResult{err: counterErr}
			cancel()
			return
		}
		for index := range jobs {
			path := paths[index]
			const dedupBatchSize = 8192
			ids := make([]string, 0, dedupBatchSize)
			flush := func() error {
				if len(ids) == 0 {
					return nil
				}
				dedupMutex.Lock()
				defer dedupMutex.Unlock()
				if err := auditContext.Err(); err != nil {
					return err
				}
				for _, id := range ids {
					key := []byte(id)
					if previous := seen.Get(key); previous != nil {
						return fmt.Errorf("record %s is duplicated in %s and %s", id, string(previous), path)
					}
					if err := seen.Put(key, []byte(path)); err != nil {
						return err
					}
				}
				ids = ids[:0]
				return nil
			}
			one, scanErr := auditOne(auditContext, path, counter, func(id string) error {
				ids = append(ids, id)
				if len(ids) == dedupBatchSize {
					return flush()
				}
				return nil
			})
			if scanErr == nil {
				scanErr = flush()
			}
			results <- auditResult{index: index, summary: one, err: scanErr}
			if scanErr != nil {
				cancel()
				return
			}
		}
	}
	wait.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}
	go func() {
		defer close(jobs)
		for index := range paths {
			select {
			case jobs <- index:
			case <-auditContext.Done():
				return
			}
		}
	}()
	go func() {
		wait.Wait()
		close(results)
	}()

	licenses, recipes := map[string]bool{}, map[string]bool{}
	var total Summary
	completed := 0
	var auditErr error
	for result := range results {
		if result.err != nil {
			if auditErr == nil || errors.Is(auditErr, context.Canceled) {
				auditErr = fmt.Errorf("%s: %w", paths[result.index], result.err)
			}
			continue
		}
		one := result.summary
		path := paths[result.index]
		addSummary(&total, one, licenses, recipes)
		completed++
		if options.Progress != nil {
			progress := total
			progress.Licenses = keys(licenses)
			progress.Recipes = keys(recipes)
			options.Progress(AuditProgress{Current: completed, Total: len(paths), Path: path, Summary: progress})
		}
	}
	if auditErr != nil {
		return Summary{}, auditErr
	}
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	total.Licenses = keys(licenses)
	total.Recipes = keys(recipes)
	return total, nil
}

func auditOne(ctx context.Context, path string, counter tokenizer.Counter, addID func(string) error) (Summary, error) {
	file, parquetFile, size, err := openShard(path)
	if err != nil {
		return Summary{}, err
	}
	one, err := scan(parquetFile, true, func(position int64, view RecordView, canonical record.Record, meta string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := int64(counter.Count(canonical.Text))
		if canonical.Tokens != count {
			return fmt.Errorf("record %d (%s): token count is %d, want %d", position, view.ID, canonical.Tokens, count)
		}
		return addID(view.ID)
	})
	if err == nil {
		footer, complete := footerSummary(parquetFile, size)
		recipe, _ := parquetFile.Lookup("waldo.recipe")
		if recipe == TextWriterRecipe && !complete {
			err = fmt.Errorf("current writer recipe is missing valid aggregate footer metadata")
		} else if complete && (footer.Records != one.Records || footer.Tokens != one.Tokens || footer.ContentBytes != one.ContentBytes || !slices.Equal(footer.Licenses, one.Licenses)) {
			err = fmt.Errorf("footer aggregates do not match streamed records")
		}
	}
	closeErr := file.Close()
	if err != nil {
		return Summary{}, err
	}
	if closeErr != nil {
		return Summary{}, closeErr
	}
	one.Shards = 1
	one.DeepScanned = 1
	one.EncodedBytes = size
	one.RowGroups = int64(len(parquetFile.RowGroups()))
	return one, nil
}

func WalkRecords(path string, callback func(int64, RecordView) error) error {
	file, parquetFile, _, err := openShard(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = scan(parquetFile, false, func(position int64, view RecordView, _ record.Record, _ string) error {
		return callback(position, view)
	})
	return err
}

func ExportRecord(path, id string, output io.Writer) error {
	found := false
	err := WalkRecords(path, func(_ int64, view RecordView) error {
		if view.ID != id {
			return nil
		}
		if found {
			return fmt.Errorf("record %s occurs more than once", id)
		}
		found = true
		_, err := io.WriteString(output, view.Text)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("record %s not found", id)
	}
	return nil
}

func openShard(path string) (*os.File, *parquet.File, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, 0, err
	}
	pf, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		file.Close()
		return nil, nil, 0, err
	}
	columns := pf.Schema().Columns()
	got := make([]string, len(columns))
	for i, column := range columns {
		if len(column) != 1 {
			file.Close()
			return nil, nil, 0, fmt.Errorf("nested canonical column %v", column)
		}
		got[i] = column[0]
	}
	canonical := slices.Equal(got, canonicalColumns)
	legacy := slices.Equal(got, legacyColumns)
	if !canonical && !legacy {
		file.Close()
		return nil, nil, 0, fmt.Errorf("columns are %v, want canonical %v or established schema-1 %v", got, canonicalColumns, legacyColumns)
	}
	if value, ok := pf.Lookup("waldo.record_schema"); (canonical && (!ok || value != strconv.Itoa(TextRecordSchema))) || (legacy && ok && value != strconv.Itoa(TextRecordSchema)) {
		file.Close()
		return nil, nil, 0, fmt.Errorf("unsupported or missing waldo.record_schema")
	}
	return file, pf, info.Size(), nil
}

func footerSummary(file *parquet.File, size int64) (Summary, bool) {
	values := map[string]int64{}
	for _, key := range []string{"waldo.records", "waldo.tokens", "waldo.content_bytes"} {
		raw, ok := file.Lookup(key)
		if !ok {
			return Summary{}, false
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return Summary{}, false
		}
		values[key] = value
	}
	var licenses []string
	raw, ok := file.Lookup("waldo.licenses")
	if !ok || json.Unmarshal([]byte(raw), &licenses) != nil {
		return Summary{}, false
	}
	recipe, _ := file.Lookup("waldo.recipe")
	return Summary{Records: values["waldo.records"], Tokens: values["waldo.tokens"], ContentBytes: values["waldo.content_bytes"], EncodedBytes: size, RowGroups: int64(len(file.RowGroups())), Licenses: licenses, Recipes: []string{recipe}}, true
}

func scan(file *parquet.File, validate bool, callback func(int64, RecordView, record.Record, string) error) (Summary, error) {
	columns := columnNames(file)
	if slices.Equal(columns, legacyColumns) {
		return scanLegacy(file, validate, callback)
	}
	if slices.Equal(columns, canonicalColumns) {
		return scanCanonical(file, validate, callback)
	}
	return Summary{}, fmt.Errorf("unsupported schema-1 physical columns %v", columns)
}

func scanCanonical(file *parquet.File, validate bool, callback func(int64, RecordView, record.Record, string) error) (Summary, error) {
	reader := parquet.NewGenericReader[TextRow](file)
	defer reader.Close()
	rows := make([]TextRow, 512)
	consumer := newRowConsumer(file, validate, callback)
	for {
		count, readErr := reader.Read(rows)
		for i := 0; i < count; i++ {
			row := rows[i]
			canonical := canonicalTextRow(row)
			if err := consumer.add(canonical, stringValue(row.Meta), row.TokenCount != nil); err != nil {
				return consumer.finish(), err
			}
		}
		if errors.Is(readErr, io.EOF) || (readErr == nil && count == 0) {
			break
		}
		if readErr != nil {
			return consumer.finish(), readErr
		}
	}
	return consumer.finish(), nil
}

// ValidateTextRow applies the complete canonical record contract immediately
// before ingestion writes a row. Published shards therefore carry builder
// evidence for checks that do not need to be repeated after object hashing.
func ValidateTextRow(row TextRow) error {
	if row.TokenCount == nil {
		return fmt.Errorf("token_count is required")
	}
	canonical := canonicalTextRow(row)
	if err := canonical.Validate(); err != nil {
		return err
	}
	meta := stringValue(row.Meta)
	if meta != "" && (!json.Valid([]byte(meta)) || meta[0] != '{') {
		return fmt.Errorf("record meta is not a JSON object")
	}
	return nil
}

func canonicalTextRow(row TextRow) record.Record {
	return record.Record{
		SHA256: hex.EncodeToString(row.ContentSHA256[:]), Kind: record.KindPretrain,
		Text: row.Text, Source: row.Source, SourceName: stringValue(row.SourceName),
		License: row.License, LicenseRaw: stringValue(row.LicenseRaw),
		Lang: stringValue(row.Language), LangScore: int64(int32Value(row.LanguageScore)),
		Date: stringValue(row.Date), Tokens: int64Value(row.TokenCount),
	}
}

func scanLegacy(file *parquet.File, validate bool, callback func(int64, RecordView, record.Record, string) error) (Summary, error) {
	reader := parquet.NewGenericReader[Row](file)
	defer reader.Close()
	rows := make([]Row, 512)
	consumer := newRowConsumer(file, validate, callback)
	for {
		count, readErr := reader.Read(rows)
		for i := 0; i < count; i++ {
			row := rows[i]
			canonical := record.Record{SHA256: row.SHA256, Kind: row.Kind, Text: row.Text, Source: row.Source, SourceName: row.SourceName, License: row.License, LicenseRaw: row.LicenseRaw, Lang: row.Lang, LangScore: row.LangScore, Date: row.Date, Tokens: row.Tokens}
			if err := consumer.add(canonical, row.Meta, true); err != nil {
				return consumer.finish(), err
			}
		}
		if errors.Is(readErr, io.EOF) || (readErr == nil && count == 0) {
			break
		}
		if readErr != nil {
			return consumer.finish(), readErr
		}
	}
	return consumer.finish(), nil
}

type rowConsumer struct {
	validate bool
	callback func(int64, RecordView, record.Record, string) error
	result   Summary
	licenses map[string]bool
}

func newRowConsumer(file *parquet.File, validate bool, callback func(int64, RecordView, record.Record, string) error) *rowConsumer {
	recipe, _ := file.Lookup("waldo.recipe")
	return &rowConsumer{validate: validate, callback: callback, result: Summary{Recipes: []string{recipe}}, licenses: map[string]bool{}}
}

func (consumer *rowConsumer) add(canonical record.Record, meta string, tokenPresent bool) error {
	position := consumer.result.Records
	if consumer.validate {
		if !tokenPresent {
			return fmt.Errorf("record %d (%s): token_count is required", position, canonical.SHA256)
		}
		if err := canonical.Validate(); err != nil {
			return fmt.Errorf("record %d: %w", position, err)
		}
		if meta != "" && (!json.Valid([]byte(meta)) || meta[0] != '{') {
			return fmt.Errorf("record %d (%s): meta is not a JSON object", position, canonical.SHA256)
		}
	}
	view := RecordView{ID: canonical.SHA256, Text: canonical.Text, Source: canonical.Source, License: canonical.License, Language: canonical.Lang, Tokens: canonical.Tokens, Bytes: int64(len(canonical.Text))}
	if consumer.callback != nil {
		if err := consumer.callback(position, view, canonical, meta); err != nil {
			return err
		}
	}
	consumer.result.Records++
	consumer.result.Tokens += canonical.Tokens
	consumer.result.ContentBytes += int64(len(canonical.Text))
	consumer.licenses[canonical.License] = true
	return nil
}

func (consumer *rowConsumer) finish() Summary {
	consumer.result.Licenses = keys(consumer.licenses)
	return consumer.result
}

func columnNames(file *parquet.File) []string {
	columns := file.Schema().Columns()
	names := make([]string, len(columns))
	for index, column := range columns {
		if len(column) == 1 {
			names[index] = column[0]
		}
	}
	return names
}

func keys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
