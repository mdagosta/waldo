package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/record"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/parquet-go/parquet-go"
)

func TestIndexExportEndToEnd(t *testing.T) {
	root := t.TempDir()
	text := "small native shard"
	var parquetData bytes.Buffer
	writer := parquet.NewGenericWriter[shard.Row](&parquetData)
	if _, err := writer.Write([]shard.Row{{
		SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text,
		Source: "fixture", License: "CC0-1.0", Tokens: 3,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	content := parquetData.Bytes()
	digestArray := sha256.Sum256(content)
	digest := hex.EncodeToString(digestArray[:])
	source := filepath.Join(root, "source.parquet")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 2, "path": "",
  "entries": [{"name": "books", "type": "dir"}]
}`)
	writeCLIFile(t, filepath.Join(root, "books", "index.json"), `{
  "kind": "index", "schema": 2, "path": "books",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books", "title": "Books",
  "description": "Small books.", "license": "CC0-1.0",
  "sources": [{"name": "source", "source": "Fixture", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": [{"url": %q, "sha256": %q, "sources": ["source"], "docs": 1, "tokens": 3, "bytes": %d}]
}`, strings.Repeat("a", 64), source, digest, len(content))
	writeCLIFile(t, filepath.Join(root, "books", "books.json"), manifest)

	cache := filepath.Join(t.TempDir(), "cache")
	destination := filepath.Join(t.TempDir(), "export")
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Scratch: cache}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--index", root, "index", "export", "books", destination}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "EXPORT.json") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(destination, "EXPORT.json")); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(destination, "data", "books", "*.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("exported parquet files = %v", matches)
	}
	assertNoCacheFiles(t, cache)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "verify", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("bom verify code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verified OpenWALDO BOM") {
		t.Fatalf("bom verify output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bom", "show", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("bom show code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpenWALDO corpus export") || !strings.Contains(stdout.String(), "native") {
		t.Fatalf("bom show output = %q", stdout.String())
	}

	jsonlDestination := filepath.Join(t.TempDir(), "jsonl-export")
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--index", root, "index", "export", "books", "--format=jsonl", jsonlDestination}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("JSONL Run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	jsonlMatches, err := filepath.Glob(filepath.Join(jsonlDestination, "data", "books", "*.jsonl"))
	if err != nil || len(jsonlMatches) != 1 {
		t.Fatalf("exported JSONL files = %v, error = %v", jsonlMatches, err)
	}
	jsonl, err := os.ReadFile(jsonlMatches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonl), `"text":"small native shard"`) {
		t.Fatalf("JSONL = %q", jsonl)
	}
	assertNoCacheFiles(t, cache)
}

func assertNoCacheFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			t.Fatalf("cache file remains after successful command: %s", path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestParseExportOptions(t *testing.T) {
	options, err := parseExportOptions([]string{"core", "science", "--format=jsonl", "--license", "CC0-*, CC-BY-*", "--exclude-license=CC-BY-NC-*", "--force", "dest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Paths) != 2 || len(options.Include) != 2 || len(options.Exclude) != 1 || options.Output != "dest" || options.Format != "jsonl" || !options.Force {
		t.Fatalf("parseExportOptions() = %+v", options)
	}
}

func TestIndexExportRejectsRemovedOutputOption(t *testing.T) {
	_, err := parseExportOptions([]string{"core", "--output", "dest"})
	if err == nil || !strings.Contains(err.Error(), "unknown index export option") {
		t.Fatalf("parseExportOptions error = %v", err)
	}
}

func writeCLIFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
