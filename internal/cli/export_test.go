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
)

func TestIndexExportEndToEnd(t *testing.T) {
	root := t.TempDir()
	content := []byte("small native shard")
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
	t.Setenv("WALDO_CACHE", cache)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--index", root, "index", "export", "books", "--output", destination}, &stdout, &stderr)
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
}

func TestParseExportOptions(t *testing.T) {
	options, err := parseExportOptions([]string{"core", "science", "--output=dest", "--license", "CC0-*, CC-BY-*", "--exclude-license=CC-BY-NC-*", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Paths) != 2 || len(options.Include) != 2 || len(options.Exclude) != 1 || options.Output != "dest" || !options.Force {
		t.Fatalf("parseExportOptions() = %+v", options)
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
