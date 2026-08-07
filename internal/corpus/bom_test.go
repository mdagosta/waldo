// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/lookaside"
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
	bom, err := BuildBOM(context.Background(), []index.Target{target, target}, policy, nil)
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
	if bom.Manifests[0].RecordSchema != 1 || bom.Manifests[0].ConvertedBy.Tool != "test" || len(bom.Manifests[0].Sources[0].Files) != 1 || bom.Shards[0].RecordSchema != 1 || bom.Shards[0].ConvertedBy.Tool != "override" {
		t.Fatalf("resolved format and provenance = %+v / %+v", bom.Manifests[0], bom.Shards[0])
	}
	if bom.Manifests[0].Processing == nil || len(bom.Manifests[0].Processing.Steps) != 1 || bom.Modalities["text"].Tokens != 30 || bom.Manifests[0].Modalities["text"].Samples != 3 || bom.Shards[0].Modalities["text"].ContentBytes != 240 {
		t.Fatalf("disclosure provenance was not preserved = %+v / %+v", bom.Manifests[0], bom.Shards[0])
	}
	if bom.Manifests[0].ComposedBy == nil || bom.Manifests[0].ComposedBy.Path != "composes/books.yaml" || len(bom.Manifests[0].ComposedBy.Steps) != 1 {
		t.Fatalf("composition evidence was not preserved = %+v", bom.Manifests[0].ComposedBy)
	}
	if bom.Totals.Shards != 1 || bom.Totals.Docs != 3 || bom.Totals.Tokens != 30 || bom.Totals.Bytes != 300 {
		t.Fatalf("bom totals = %+v", bom.Totals)
	}
	if bom.Licenses["CC-BY-4.0"].Tokens != 30 {
		t.Fatalf("bom licenses = %+v", bom.Licenses)
	}
}

func TestBuildBOMExpandsNestedSubManifests(t *testing.T) {
	root, rootHash, childHash := rollupBOMFixture(t, 50)
	target, err := index.Resolve(root, "books")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewLicensePolicy([]string{"CC-BY-*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := lookaside.NewCache(filepath.Join(t.TempDir(), "cache"), nil)
	if err != nil {
		t.Fatal(err)
	}
	bom, err := BuildBOM(context.Background(), []index.Target{target}, policy, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(bom.SubManifests) != 2 || bom.SubManifests[0].SHA256 != rootHash || bom.SubManifests[1].SHA256 != childHash || bom.SubManifests[1].ParentSHA256 != rootHash {
		t.Fatalf("sub-manifest pins = %+v", bom.SubManifests)
	}
	if len(bom.Shards) != 1 || bom.Shards[0].License != "CC-BY-4.0" || bom.Shards[0].SubManifestSHA256 != childHash {
		t.Fatalf("selected shards = %+v", bom.Shards)
	}
	if bom.Totals.Shards != 1 || bom.Totals.Docs != 3 || bom.Totals.Tokens != 30 || bom.Totals.Bytes != 300 {
		t.Fatalf("selected totals = %+v", bom.Totals)
	}
}

func TestBuildBOMRejectsIncorrectSubManifestTotals(t *testing.T) {
	root, _, _ := rollupBOMFixture(t, 51)
	target, err := index.Resolve(root, "books")
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := NewLicensePolicy(nil, nil)
	cache, err := lookaside.NewCache(filepath.Join(t.TempDir(), "cache"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBOM(context.Background(), []index.Target{target}, policy, cache); err == nil || !strings.Contains(err.Error(), "reference declares") {
		t.Fatalf("incorrect totals error = %v", err)
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
	if _, err := BuildBOM(context.Background(), []index.Target{leftTarget, rightTarget}, policy, nil); err == nil || !strings.Contains(err.Error(), "different checkouts") {
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
	bom, err := BuildBOM(context.Background(), []index.Target{target}, policy, nil)
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
  "kind": "index", "schema": 1, "path": "",
  "entries": [{"name": "books", "type": "dir"}]
}`)
	writeBOMFile(t, filepath.Join(root, "books", "index.json"), `{
  "kind": "index", "schema": 1, "path": "books",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books", "title": "Books",
  "description": "Example books.", "license": "CC0-1.0",
  "sources": [{"name": "source", "source": "Example", "url": "https://example.test", "sha256": %q,
    "category": "public-dataset", "usage": {"text": {"samples": 5, "tokens": 50, "content_bytes": 400}},
    "content": {"types": ["books"], "languages": ["en"], "copyrighted": "unknown"},
    "files": [{"name": "input", "url": "https://example.test/input", "sha256": %q}]}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "processing": {"steps": [{"name": "normalize", "description": "Normalize text."}]},
  "composed_by": {"path": "composes/books.yaml", "sha256": %q,
    "steps": [{"name": "fetch", "script": "fetchers/books.sh", "sha256": %q}]},
  "shards": [
    {"url": "https://example.test/a", "sha256": %q, "sources": ["source"], "docs": 2, "tokens": 20, "bytes": 200,
     "modalities": {"text": {"samples": 2, "tokens": 20, "content_bytes": 160}}},
    {"url": "https://example.test/b", "sha256": %q, "license": "CC-BY-4.0", "sources": ["source"],
     "converted_by": {"tool": "override", "version": "2", "profile": "text", "recipe": "override/v2", "tokenizer": "byte"},
     "docs": 3, "tokens": 30, "bytes": 300,
     "modalities": {"text": {"samples": 3, "tokens": 30, "content_bytes": 240}}}
  ]
}`, strings.Repeat("a", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64), strings.Repeat("b", 64), strings.Repeat("c", 64))
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

func TestCanonicalRecordSchemaAllowsTokenizerNeutralConversion(t *testing.T) {
	conversion := index.Conversion{Tool: "waldo", Version: "1", Profile: "canonical-text-schema-1", Recipe: "parquet-v1"}
	if !completeConversion(conversion, 1) {
		t.Fatal("canonical record schema 1 rejected tokenizer-neutral provenance")
	}
	if completeConversion(conversion, 0) {
		t.Fatal("legacy record accepted missing tokenizer provenance")
	}
}

func rollupBOMFixture(t *testing.T, declaredTokens int64) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	objects := filepath.Join(root, "objects")
	child := fmt.Sprintf(`{"kind":"sub-manifest","schema":1,"shards":[{"url":"https://example.test/b","sha256":%q,"license":"CC-BY-4.0","sources":["source"],"docs":3,"tokens":30,"bytes":300}]}`,
		strings.Repeat("c", 64))
	childHash := hashFixture(child)
	childPath := filepath.Join(objects, childHash+".json")
	writeBOMFile(t, childPath, child)
	rootSub := fmt.Sprintf(`{"kind":"sub-manifest","schema":1,"shards":[{"url":"https://example.test/a","sha256":%q,"sources":["source"],"docs":2,"tokens":20,"bytes":200}],"children":[{"url":%q,"sha256":%q,"count":1,"docs":3,"tokens":30,"bytes":300}]}`,
		strings.Repeat("b", 64), childPath, childHash)
	rootHash := hashFixture(rootSub)
	rootPath := filepath.Join(objects, rootHash+".json")
	writeBOMFile(t, rootPath, rootSub)
	writeBOMFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 1, "path": "",
  "entries": [{"name": "books", "type": "dir"}]
}`)
	writeBOMFile(t, filepath.Join(root, "books", "index.json"), `{
  "kind": "index", "schema": 1, "path": "books",
  "entries": [{"name": "books.json", "type": "manifest"}]
}`)
	manifest := fmt.Sprintf(`{
  "kind": "manifest", "schema": 1, "name": "books", "title": "Books",
  "description": "Nested example.", "license": "CC0-1.0",
  "sources": [{"name": "source", "source": "Example", "url": "https://example.test", "sha256": %q}],
  "converted_by": {"tool": "test", "version": "1", "profile": "text", "recipe": "test/v1", "tokenizer": "byte"},
  "shards": {"url": %q, "sha256": %q, "count": 2, "docs": 5, "tokens": %d, "bytes": 500}
}`, strings.Repeat("a", 64), rootPath, rootHash, declaredTokens)
	writeBOMFile(t, filepath.Join(root, "books", "books.json"), manifest)
	return root, rootHash, childHash
}

func hashFixture(contents string) string {
	digest := sha256.Sum256([]byte(contents + "\n"))
	return hex.EncodeToString(digest[:])
}
