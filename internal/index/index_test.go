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

func TestRollupManifestUsesPolymorphicShardsField(t *testing.T) {
	root := fixtureIndex(t)
	hash := strings.Repeat("d", 64)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books",
  "title": "Books", "description": "Rolled-up books.", "license": "CC0-1.0",
  "sources": [{"name": "upstream", "source": "Example", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": {"url": "https://objects.example/sub", "sha256": %q, "count": 2, "docs": 5, "tokens": 50, "bytes": 500}
}`, strings.Repeat("a", 64), hash)
	writeFile(t, filepath.Join(root, "alpha", "books.json"), manifest)
	target, err := Resolve(root, "alpha/books.json")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(target.Abs)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Rollup == nil || loaded.Rollup.Count != 2 || len(loaded.Shards) != 0 {
		t.Fatalf("rollup manifest = %+v", loaded)
	}
	verified, err := Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Shards != 2 {
		t.Fatalf("verified rollup = %+v", verified)
	}
	totals, err := Summarize(target)
	if err != nil {
		t.Fatal(err)
	}
	if totals.Shards != 2 || totals.Docs != 5 || totals.Tokens != 50 || totals.Bytes != 500 {
		t.Fatalf("rollup totals = %+v", totals)
	}
}

func TestVerifyAcceptsAdditiveMultimodalProvenance(t *testing.T) {
	root := fixtureIndex(t)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books",
  "title": "Images", "description": "Example image records.", "license": "CC0-1.0",
  "sources": [{
    "name": "upstream", "source": "Example", "url": "https://example.test", "sha256": %q,
    "category": "public-dataset",
    "usage": {"image": {"samples": 2, "items": 3, "content_bytes": 100}},
    "content": {"types": ["photography"], "copyrighted": "unknown"}
  }],
  "converted_by": {"tool": "test", "version": "1", "profile": "image", "recipe": "test/v2", "tokenizer": "none"},
  "processing": {
    "steps": [{"name": "validate", "description": "Validated media payloads."}],
    "rights_reservation_measures": ["Honoured recorded upstream exclusions."],
    "illegal_content_measures": ["Rejected payloads matching the configured blocklist."]
  },
  "record_schema": 2,
  "format": "parquet",
  "shards": [{
    "url": "https://example.test/a", "sha256": %q, "sources": ["upstream"],
    "docs": 2, "tokens": 0, "bytes": 200,
    "modalities": {"image": {"samples": 2, "items": 3, "content_bytes": 100}}
  }]
}`, strings.Repeat("a", 64), strings.Repeat("b", 64))
	path := filepath.Join(root, "alpha", "books.json")
	writeFile(t, path, manifest)
	target, err := Resolve(root, "alpha/books.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RecordSchema != 2 || loaded.Shards[0].Tokens != 0 || loaded.Shards[0].Modalities["image"].Items != 3 {
		t.Fatalf("multimodal manifest = %+v", loaded)
	}
}

func TestVerifyRejectsSourceUsageMismatch(t *testing.T) {
	root := fixtureIndex(t)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books",
  "title": "Images", "description": "Example image records.", "license": "CC0-1.0",
  "sources": [{
    "name": "upstream", "source": "Example", "url": "https://example.test", "sha256": %q,
    "category": "public-dataset", "usage": {"image": {"samples": 1, "items": 1}}
  }],
  "converted_by": {"tool": "test", "version": "1", "profile": "image", "recipe": "test/v2", "tokenizer": "none"},
  "shards": [{
    "url": "https://example.test/a", "sha256": %q, "sources": ["upstream"],
    "docs": 2, "tokens": 0, "bytes": 200,
    "modalities": {"image": {"samples": 2, "items": 2}}
  }]
}`, strings.Repeat("a", 64), strings.Repeat("b", 64))
	path := filepath.Join(root, "alpha", "books.json")
	writeFile(t, path, manifest)
	target, err := Resolve(root, "alpha/books.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(target); err == nil || !strings.Contains(err.Error(), "does not reconcile") {
		t.Fatalf("Verify() error = %v, want source-usage reconciliation error", err)
	}
}

func TestWebCrawlProvenanceRequiresCrawlerEvidence(t *testing.T) {
	source := Source{
		Name: "crawl", Category: SourceWebCrawl,
		Usage:       Modalities{"text": {Samples: 1, Tokens: 2}},
		Acquisition: &Acquisition{Domains: []DomainMeasure{{Domain: "example.com", RetainedBytes: 10}}},
	}
	if err := validateSourceProvenance(source); err == nil || !strings.Contains(err.Error(), "crawler details") {
		t.Fatalf("validateSourceProvenance() error = %v, want crawler error", err)
	}
	source.Acquisition.Crawler = &Crawler{Name: "waldo", Purpose: "Acquire public pages.", Behaviour: "Honours robots.txt."}
	if err := validateSourceProvenance(source); err != nil {
		t.Fatal(err)
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
