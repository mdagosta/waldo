// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/parquet-go/parquet-go"
)

func TestSummaryUsesTinyFooterAggregatesAndAuditChecksRecords(t *testing.T) {
	text := "hello"
	tokens := tokenCount(t, text)
	path := filepath.Join(t.TempDir(), "one.parquet")
	writeCanonicalFixture(t, path, []TextRow{validTextRow(text, tokens)}, true, nil)
	summary, err := Summarize([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Shards != 1 || summary.Records != 1 || summary.Tokens != tokens || len(summary.Licenses) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	audited, err := Audit(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if audited.Records != summary.Records || audited.Tokens != summary.Tokens {
		t.Fatalf("audit = %+v, summary = %+v", audited, summary)
	}
}

func TestTargetedRecordReadsSeekWithoutWalkingShard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.parquet")
	rows := []TextRow{
		validTextRow("zero", tokenCount(t, "zero")),
		validTextRow("one", tokenCount(t, "one")),
		validTextRow("two", tokenCount(t, "two")),
		validTextRow("three", tokenCount(t, "three")),
	}
	writeCanonicalFixture(t, path, rows, true, nil)
	sizes, err := ReadRecordTextSizes(path, []int64{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sizes, []int64{3, 5}) {
		t.Fatalf("targeted text sizes = %v", sizes)
	}
	var positions []int64
	var texts []string
	if err := ReadRecordsAt(path, []int64{1, 3}, func(position int64, view RecordView) error {
		positions = append(positions, position)
		texts = append(texts, view.Text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(positions, []int64{1, 3}) || !slices.Equal(texts, []string{"one", "three"}) {
		t.Fatalf("targeted records = %v / %v", positions, texts)
	}
	if count, err := RecordCount(path); err != nil || count != 4 {
		t.Fatalf("record count = %d, err = %v", count, err)
	}
	if _, err := ReadRecordTextSizes(path, []int64{3, 1}); err == nil {
		t.Fatal("descending targeted positions accepted")
	}
}

func TestEstablishedSchemaOneRowsRemainReadableAndUnassessed(t *testing.T) {
	text := "legacy schema one"
	tokens := tokenCount(t, text)
	digest := sha256.Sum256([]byte(text))
	path := filepath.Join(t.TempDir(), "schema-one.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[textRowV1](file, proposedTextWriterOptions()...)
	writer.SetKeyValueMetadata("waldo.record_schema", strconv.Itoa(FormerTextRecordSchema))
	writer.SetKeyValueMetadata("waldo.recipe", FormerTextBOMRecipe)
	if _, err := writer.Write([]textRowV1{{ContentSHA256: digest, Text: text, Source: "fixture", License: "CC0-1.0", TokenCount: &tokens}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var got RecordView
	if err := ReadRecordsAt(path, []int64{0}, func(_ int64, view RecordView) error { got = view; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.Text != text || got.EmailAddresses != nil || !got.MainContent {
		t.Fatalf("schema-one view = %+v", got)
	}
}

func TestInitialSchemaTwoRowsDefaultToMainContent(t *testing.T) {
	text := "initial schema two"
	tokens := tokenCount(t, text)
	digest := sha256.Sum256([]byte(text))
	path := filepath.Join(t.TempDir(), "schema-two-v7.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[textRowV2](file, proposedTextWriterOptions()...)
	writer.SetKeyValueMetadata("waldo.record_schema", strconv.Itoa(TextRecordSchema))
	writer.SetKeyValueMetadata("waldo.recipe", FormerAssessmentRecipe)
	if _, err := writer.Write([]textRowV2{{ContentSHA256: digest, Text: text, Source: "fixture", License: "CC0-1.0", TokenCount: &tokens}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var got RecordView
	if err := ReadRecordsAt(path, []int64{0}, func(_ int64, view RecordView) error { got = view; return nil }); err != nil {
		t.Fatal(err)
	}
	if !got.MainContent || got.EmailAddresses == nil {
		t.Fatalf("initial schema-two view = %+v", got)
	}
}

func TestSchemaTwoMainContentRowsRemainReadableWithoutRedactionEvidence(t *testing.T) {
	text := "schema two v8"
	tokens := tokenCount(t, text)
	digest := sha256.Sum256([]byte(text))
	path := filepath.Join(t.TempDir(), "schema-two-v8.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[textRowV3](file, proposedTextWriterOptions()...)
	writer.SetKeyValueMetadata("waldo.record_schema", strconv.Itoa(TextRecordSchema))
	writer.SetKeyValueMetadata("waldo.recipe", FormerMainContentRecipe)
	if _, err := writer.Write([]textRowV3{{ContentSHA256: digest, Text: text, Source: "fixture", License: "CC0-1.0", TokenCount: &tokens, MainContent: false}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var got RecordView
	if err := ReadRecordsAt(path, []int64{0}, func(_ int64, view RecordView) error { got = view; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.MainContent || got.Text != text {
		t.Fatalf("v8 record = %+v", got)
	}
}

func TestInspectEmbeddedShardBOM(t *testing.T) {
	text := "embedded BOM fixture"
	tokens := tokenCount(t, text)
	path := filepath.Join(t.TempDir(), "attested.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewTextParquetWriter(file)
	if _, err := writer.Write([]TextRow{validTextRow(text, tokens)}); err != nil {
		t.Fatal(err)
	}
	writer.SetKeyValueMetadata("waldo.records", "1")
	writer.SetKeyValueMetadata("waldo.tokens", strconv.FormatInt(tokens, 10))
	writer.SetKeyValueMetadata("waldo.content_bytes", strconv.Itoa(len(text)))
	writer.SetKeyValueMetadata("waldo.email_address_records", "0")
	writer.SetKeyValueMetadata("waldo.repetitive_content_records", "0")
	writer.SetKeyValueMetadata("waldo.boilerplate_content_records", "0")
	setPrivacyFooter(writer)
	writer.SetKeyValueMetadata("waldo.licenses", `["CC0-1.0"]`)
	bom := NewBOM(strings.Repeat("a", 64), tokenizer.Default, 1, tokens, int64(len(text)), []string{"CC0-1.0"})
	encoded, err := EncodeBOM(bom)
	if err != nil {
		t.Fatal(err)
	}
	writer.SetKeyValueMetadata(BOMMetadataKey, encoded)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	attestation, err := InspectAttestation(path)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Status != "embedded" || attestation.BOMSHA256 == "" || attestation.BOM == nil || attestation.BOM.PlanSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("attestation = %+v", attestation)
	}
}

func TestAuditRejectsInvalidCanonicalRecordsAndFooters(t *testing.T) {
	text := "audited text"
	correctTokens := tokenCount(t, text)
	badHash := validTextRow(text, correctTokens)
	badHash.ContentSHA256 = sha256.Sum256([]byte("different"))
	missingToken := validTextRow(text, correctTokens)
	missingToken.TokenCount = nil
	badMeta := validTextRow(text, correctTokens)
	malformed := `[1,2,3]`
	badMeta.Meta = &malformed
	missingSource := validTextRow(text, correctTokens)
	missingSource.Source = ""
	badLanguage := validTextRow(text, correctTokens)
	score := int32(500)
	badLanguage.LanguageScore = &score
	tests := []struct {
		name      string
		rows      []TextRow
		footer    bool
		overrides map[string]string
		want      string
	}{
		{"content-hash", []TextRow{badHash}, true, nil, "does not match its text"},
		{"token-count", []TextRow{validTextRow(text, correctTokens+1)}, true, nil, "token count"},
		{"missing-token", []TextRow{missingToken}, true, nil, "token_count is required"},
		{"metadata", []TextRow{badMeta}, true, nil, "meta is not a JSON object"},
		{"required-fields", []TextRow{missingSource}, true, nil, "source and license are required"},
		{"language-score", []TextRow{badLanguage}, true, nil, "lang_score is set without lang"},
		{"footer-total", []TextRow{validTextRow(text, correctTokens)}, true, map[string]string{"waldo.tokens": "999"}, "footer aggregates"},
		{"missing-footer", []TextRow{validTextRow(text, correctTokens)}, false, nil, "missing valid aggregate footer"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.parquet")
			writeCanonicalFixture(t, path, test.rows, test.footer, test.overrides)
			if _, err := Audit(context.Background(), []string{path}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Audit() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAuditDetectsDuplicatesWithinAndAcrossShards(t *testing.T) {
	text := "duplicate"
	tokens := tokenCount(t, text)
	row := validTextRow(text, tokens)
	directory := t.TempDir()
	within := filepath.Join(directory, "within.parquet")
	writeCanonicalFixture(t, within, []TextRow{row, row}, true, nil)
	if _, err := Audit(context.Background(), []string{within}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("within-shard Audit() error = %v", err)
	}
	first, second := filepath.Join(directory, "first.parquet"), filepath.Join(directory, "second.parquet")
	writeCanonicalFixture(t, first, []TextRow{row}, true, nil)
	writeCanonicalFixture(t, second, []TextRow{row}, true, nil)
	if _, err := Audit(context.Background(), []string{first, second}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("cross-shard Audit() error = %v", err)
	}
}

func TestAuditWithOptionsScansShardsConcurrentlyAndReportsProgress(t *testing.T) {
	directory := t.TempDir()
	var paths []string
	for index, text := range []string{"first unique record", "second unique record", "third unique record"} {
		path := filepath.Join(directory, fmt.Sprintf("%d.parquet", index))
		writeCanonicalFixture(t, path, []TextRow{validTextRow(text, tokenCount(t, text))}, true, nil)
		paths = append(paths, path)
	}
	var progress []AuditProgress
	summary, err := AuditWithOptions(context.Background(), paths, AuditOptions{Workers: 2, Progress: func(event AuditProgress) {
		progress = append(progress, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Shards != 3 || summary.Records != 3 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(progress) != 3 || progress[2].Current != 3 || progress[2].Total != 3 || progress[2].Summary.Records != 3 {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestAuditRejectsCancellationTruncationAndWrongPhysicalSchema(t *testing.T) {
	text := "valid"
	tokens := tokenCount(t, text)
	directory := t.TempDir()
	valid := filepath.Join(directory, "valid.parquet")
	writeCanonicalFixture(t, valid, []TextRow{validTextRow(text, tokens)}, true, nil)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Audit(canceled, []string{valid}); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("canceled Audit() error = %v", err)
	}
	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(directory, "truncated.parquet")
	if err := os.WriteFile(truncated, data[:len(data)-8], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Audit(context.Background(), []string{truncated}); err == nil {
		t.Fatal("Audit accepted truncated Parquet")
	}
	wrong := filepath.Join(directory, "wrong.parquet")
	file, err := os.Create(wrong)
	if err != nil {
		t.Fatal(err)
	}
	type wrongRow struct {
		Text string `parquet:"text"`
	}
	writer := parquet.NewGenericWriter[wrongRow](file)
	if _, err := writer.Write([]wrongRow{{Text: text}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Audit(context.Background(), []string{wrong}); err == nil || !strings.Contains(err.Error(), "columns are") {
		t.Fatalf("wrong-schema Audit() error = %v", err)
	}
}

func TestResolvePathsRecursesExpandsGlobsSortsAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.parquet")
	second := filepath.Join(root, "nested", "b.PARQUET")
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := ResolvePaths([]string{root, filepath.Join(root, "*.parquet"), first})
	if err != nil {
		t.Fatal(err)
	}
	wantFirst, _ := filepath.Abs(first)
	wantSecond, _ := filepath.Abs(second)
	if len(paths) != 2 || paths[0] != wantFirst || paths[1] != wantSecond {
		t.Fatalf("ResolvePaths() = %v", paths)
	}
}

func TestAuditReadsEstablishedSchemaOnePhysicalLayout(t *testing.T) {
	text := "established schema one"
	tokens := tokenCount(t, text)
	path := filepath.Join(t.TempDir(), "legacy.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[Row](file)
	if _, err := writer.Write([]Row{{SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), Kind: "pretrain", Text: text, Source: "fixture", License: "CC0-1.0", Tokens: tokens}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	summary, err := Audit(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Records != 1 || summary.Tokens != tokens || summary.ContentBytes != int64(len(text)) {
		t.Fatalf("summary = %+v", summary)
	}
}

func validTextRow(text string, tokens int64) TextRow {
	digest := sha256.Sum256([]byte(text))
	return TextRow{ContentSHA256: digest, Text: text, Source: "fixture", License: "CC0-1.0", TokenCount: &tokens, MainContent: true}
}

func tokenCount(t *testing.T, text string) int64 {
	t.Helper()
	counter, err := tokenizer.Get(tokenizer.Default)
	if err != nil {
		t.Fatal(err)
	}
	return int64(counter.Count(text))
}

func writeCanonicalFixture(t *testing.T, path string, rows []TextRow, footer bool, overrides map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewTextParquetWriter(file)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if footer {
		var tokens, content, emailRecords, repetitiveRecords, boilerplateRecords int64
		licenses := map[string]bool{}
		for _, row := range rows {
			tokens += int64Value(row.TokenCount)
			content += int64(len(row.Text))
			licenses[row.License] = true
			if row.EmailAddresses {
				emailRecords++
			}
			if row.RepetitiveContent {
				repetitiveRecords++
			}
			if row.BoilerplateContent {
				boilerplateRecords++
			}
		}
		values := map[string]string{"waldo.records": strconv.Itoa(len(rows)), "waldo.tokens": strconv.FormatInt(tokens, 10), "waldo.content_bytes": strconv.FormatInt(content, 10), "waldo.email_address_records": strconv.FormatInt(emailRecords, 10), "waldo.repetitive_content_records": strconv.FormatInt(repetitiveRecords, 10), "waldo.boilerplate_content_records": strconv.FormatInt(boilerplateRecords, 10), "waldo.licenses": `[` + quotedKeys(licenses) + `]`, "waldo.privacy_redaction_policy": PrivacyRedactionPolicy, "waldo.redacted_email_addresses": "0", "waldo.redacted_ip_addresses": "0", "waldo.redacted_phone_numbers": "0", "waldo.removed_mail_routing_headers": "0", "waldo.redacted_credentials": "0"}
		for key, value := range overrides {
			values[key] = value
		}
		for _, key := range []string{"waldo.records", "waldo.tokens", "waldo.content_bytes", "waldo.email_address_records", "waldo.repetitive_content_records", "waldo.boilerplate_content_records", "waldo.licenses", "waldo.privacy_redaction_policy", "waldo.redacted_email_addresses", "waldo.redacted_ip_addresses", "waldo.redacted_phone_numbers", "waldo.removed_mail_routing_headers", "waldo.redacted_credentials"} {
			writer.SetKeyValueMetadata(key, values[key])
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func setPrivacyFooter(writer *parquet.GenericWriter[TextRow]) {
	writer.SetKeyValueMetadata("waldo.privacy_redaction_policy", PrivacyRedactionPolicy)
	writer.SetKeyValueMetadata("waldo.redacted_email_addresses", "0")
	writer.SetKeyValueMetadata("waldo.redacted_ip_addresses", "0")
	writer.SetKeyValueMetadata("waldo.redacted_phone_numbers", "0")
	writer.SetKeyValueMetadata("waldo.removed_mail_routing_headers", "0")
	writer.SetKeyValueMetadata("waldo.redacted_credentials", "0")
}

func quotedKeys(values map[string]bool) string {
	var result []string
	for value := range values {
		if value != "" {
			result = append(result, strconv.Quote(value))
		}
	}
	return strings.Join(result, ",")
}
