package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSummarizeAndVerify(t *testing.T) {
	root := fixtureIndex(t)

	target, err := Resolve(filepath.Join(root, "alpha"), "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Root != root {
		t.Fatalf("Resolve() root = %q, want %q", target.Root, root)
	}
	if target.Rel != "" {
		t.Fatalf("Resolve() rel = %q, want root", target.Rel)
	}

	alpha, err := Resolve(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	totals, err := Summarize(alpha)
	if err != nil {
		t.Fatal(err)
	}
	if totals.Corpora != 1 || totals.Shards != 2 || totals.Docs != 5 || totals.Tokens != 50 || totals.Bytes != 500 {
		t.Fatalf("Summarize() = %+v", totals)
	}
	if totals.Licenses["CC0-1.0"].Tokens != 20 || totals.Licenses["CC-BY-4.0"].Tokens != 30 {
		t.Fatalf("Summarize() licenses = %+v", totals.Licenses)
	}
	corpora, err := ListCorpora(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpora) != 1 || corpora[0].Path != "alpha/books" || corpora[0].Tokens != 50 || len(corpora[0].Licenses) != 2 {
		t.Fatalf("ListCorpora() = %+v", corpora)
	}

	verified, err := Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Directories != 2 || verified.Corpora != 1 || verified.Shards != 2 {
		t.Fatalf("Verify() = %+v", verified)
	}
}

func TestResolveRejectsPathOutsideCheckout(t *testing.T) {
	root := fixtureIndex(t)
	if _, err := Resolve(root, "../elsewhere"); err == nil || !strings.Contains(err.Error(), "outside index checkout") {
		t.Fatalf("Resolve() error = %v, want outside-checkout error", err)
	}
}

func TestVerifyRejectsUnsortedDirectory(t *testing.T) {
	root := fixtureIndex(t)
	writeFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 2, "path": "",
  "entries": [
    {"name": "z", "type": "dir"},
    {"name": "alpha", "type": "dir"}
  ]
}`)
	target := Target{Root: root, Abs: root}
	if _, err := Verify(target); err == nil || !strings.Contains(err.Error(), "not sorted") {
		t.Fatalf("Verify() error = %v, want sorting error", err)
	}
}

func TestPublicIndexAcceptance(t *testing.T) {
	path := os.Getenv("WALDO_ACCEPTANCE_INDEX")
	if path == "" {
		t.Skip("set WALDO_ACCEPTANCE_INDEX to an absolute public-checkout path to run acceptance tests")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("WALDO_ACCEPTANCE_INDEX must be absolute, got %q", path)
	}
	target, err := Resolve(path, "")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Corpora != 20 || verified.Shards != 1087 {
		t.Fatalf("public index shape changed: %+v", verified)
	}
	totals, err := Summarize(target)
	if err != nil {
		t.Fatal(err)
	}
	if totals.Docs != 75_122_304 || totals.Tokens != 124_010_554_159 || totals.Bytes != 169_363_482_410 {
		t.Fatalf("public index totals changed: %+v", totals)
	}
}

func fixtureIndex(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 2, "path": "",
  "entries": [{"name": "alpha", "type": "dir"}]
}`)
	writeFile(t, filepath.Join(root, "alpha", "index.json"), `{
  "kind": "index", "schema": 2, "path": "alpha",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	hashC := strings.Repeat("c", 64)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books",
  "title": "Books", "description": "Example books.", "license": "CC0-1.0",
  "sources": [{"name": "upstream", "source": "Example", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": [
    {"url": "https://example.test/a", "sha256": %q, "sources": ["upstream"], "docs": 2, "tokens": 20, "bytes": 200},
    {"url": "https://example.test/b", "sha256": %q, "license": "CC-BY-4.0", "sources": ["upstream"], "docs": 3, "tokens": 30, "bytes": 300}
  ]
}`, hashA, hashB, hashC)
	writeFile(t, filepath.Join(root, "alpha", "books.json"), manifest)
	return root
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
