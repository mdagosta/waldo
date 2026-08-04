package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/index"
)

func TestBuildBOMResolvesAndPinsSelection(t *testing.T) {
	root := bomFixture(t)
	target, err := index.Resolve(root, "books")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewLicensePolicy([]string{"CC-BY-*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bom, err := BuildBOM([]index.Target{target, target}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if bom.Kind != "openwaldo-bom" || bom.Schema != 1 || bom.Subject != "corpus" {
		t.Fatalf("bom identity = %q/%d", bom.Kind, bom.Schema)
	}
	if len(bom.Paths) != 1 || bom.Paths[0] != "books" {
		t.Fatalf("bom paths = %v", bom.Paths)
	}
	if len(bom.Manifests) != 1 || len(bom.Shards) != 1 {
		t.Fatalf("bom pins = %d manifests, %d shards", len(bom.Manifests), len(bom.Shards))
	}
	if bom.Manifests[0].SHA256 == "" || bom.Shards[0].License != "CC-BY-4.0" || bom.Shards[0].Format != "parquet" {
		t.Fatalf("bom pins = %+v / %+v", bom.Manifests[0], bom.Shards[0])
	}
	if bom.Totals.Shards != 1 || bom.Totals.Docs != 3 || bom.Totals.Tokens != 30 || bom.Totals.Bytes != 300 {
		t.Fatalf("bom totals = %+v", bom.Totals)
	}
	if bom.Licenses["CC-BY-4.0"].Tokens != 30 {
		t.Fatalf("bom licenses = %+v", bom.Licenses)
	}
}

func TestBuildBOMRejectsDifferentCheckouts(t *testing.T) {
	left := bomFixture(t)
	right := bomFixture(t)
	leftTarget, err := index.Resolve(left, "")
	if err != nil {
		t.Fatal(err)
	}
	rightTarget, err := index.Resolve(right, "")
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := NewLicensePolicy(nil, nil)
	if _, err := BuildBOM([]index.Target{leftTarget, rightTarget}, policy); err == nil || !strings.Contains(err.Error(), "different checkouts") {
		t.Fatalf("BuildBOM() error = %v", err)
	}
}

func TestPublicIndexBOMAcceptance(t *testing.T) {
	path := os.Getenv("WALDO_ACCEPTANCE_INDEX")
	if path == "" {
		t.Skip("set WALDO_ACCEPTANCE_INDEX to an absolute public-checkout path to run acceptance tests")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("WALDO_ACCEPTANCE_INDEX must be absolute, got %q", path)
	}
	target, err := index.Resolve(path, "")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewLicensePolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bom, err := BuildBOM([]index.Target{target}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if bom.Index.Commit == "" || len(bom.Manifests) != 20 || len(bom.Shards) != 1087 {
		t.Fatalf("public bom identity = commit %q, %d manifests, %d shards", bom.Index.Commit, len(bom.Manifests), len(bom.Shards))
	}
	if bom.Totals.Docs != 75_122_304 || bom.Totals.Tokens != 124_010_554_159 || bom.Totals.Bytes != 169_363_482_410 {
		t.Fatalf("public bom totals = %+v", bom.Totals)
	}
}

func bomFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeBOMFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 2, "path": "",
  "entries": [{"name": "books", "type": "dir"}]
}`)
	writeBOMFile(t, filepath.Join(root, "books", "index.json"), `{
  "kind": "index", "schema": 2, "path": "books",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books", "title": "Books",
  "description": "Example books.", "license": "CC0-1.0",
  "sources": [{"name": "source", "source": "Example", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": [
    {"url": "https://example.test/a", "sha256": %q, "sources": ["source"], "docs": 2, "tokens": 20, "bytes": 200},
    {"url": "https://example.test/b", "sha256": %q, "license": "CC-BY-4.0", "sources": ["source"], "docs": 3, "tokens": 30, "bytes": 300}
  ]
}`, strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64))
	writeBOMFile(t, filepath.Join(root, "books", "books.json"), manifest)
	return root
}

func writeBOMFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
