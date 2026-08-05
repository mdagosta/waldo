package shard

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSummaryUsesTinyFooterAggregatesAndAuditChecksRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.parquet")
	writeAuditFixture(t, path, "hello", 99)
	summary, err := Summarize([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Shards != 1 || summary.Records != 1 || summary.Tokens != 99 || len(summary.Licenses) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if _, err := Audit(context.Background(), []string{path}); err == nil || !strings.Contains(err.Error(), "token count") {
		t.Fatalf("Audit() error = %v", err)
	}
}

func TestAuditDetectsDuplicateIDsAcrossShards(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "first.parquet"), filepath.Join(directory, "second.parquet")
	writeAuditFixture(t, first, "hello", 1)
	writeAuditFixture(t, second, "hello", 1)
	if _, err := Audit(context.Background(), []string{first, second}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Audit() error = %v", err)
	}
}

func writeAuditFixture(t *testing.T, path, text string, tokens int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := NewTextParquetWriter(file)
	digest := sha256.Sum256([]byte(text))
	license := "CC0-1.0"
	row := TextRow{ContentSHA256: digest, Text: text, Source: "fixture", License: license, TokenCount: &tokens}
	if _, err := writer.Write([]TextRow{row}); err != nil {
		t.Fatal(err)
	}
	writer.SetKeyValueMetadata("waldo.records", "1")
	writer.SetKeyValueMetadata("waldo.tokens", strconv.FormatInt(tokens, 10))
	writer.SetKeyValueMetadata("waldo.content_bytes", strconv.Itoa(len(text)))
	writer.SetKeyValueMetadata("waldo.licenses", `["CC0-1.0"]`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
