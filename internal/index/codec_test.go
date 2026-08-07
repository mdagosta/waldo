// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLManifestRoundTripsInlineAndRollupShards(t *testing.T) {
	inline := Manifest{
		Kind: "manifest", Schema: 1, Name: "books", Title: "Books", Description: "Readable YAML.", License: "CC0-1.0",
		Sources:     []Source{{Name: "source", Source: "Source", URL: "https://example.test", SHA256: strings.Repeat("a", 64), CollectedFrom: "2024-01-01"}},
		ConvertedBy: Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1"},
		Shards:      []Shard{{URL: "https://example.test/object", SHA256: strings.Repeat("b", 64), Docs: 2, Tokens: 3, Bytes: 4}},
	}
	path := filepath.Join(t.TempDir(), "books.yaml")
	data, err := MarshalYAML(inline)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "kind: manifest\nschema: 1\n") || strings.Contains(string(data), "{") {
		t.Fatalf("YAML is not readable canonical output:\n%s", data)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Shards) != 1 || loaded.Shards[0].Tokens != 3 || loaded.Sources[0].CollectedFrom != "2024-01-01" {
		t.Fatalf("loaded inline manifest = %+v", loaded)
	}

	inline.Shards = nil
	inline.Rollup = &Rollup{URL: "https://example.test/rollup", SHA256: strings.Repeat("c", 64), Count: 5, Docs: 6, Tokens: 7, Bytes: 8}
	data, err = MarshalYAML(inline)
	if err != nil {
		t.Fatal(err)
	}
	rollupPath := filepath.Join(t.TempDir(), "books.yml")
	if err := os.WriteFile(rollupPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadManifest(rollupPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Rollup == nil || loaded.Rollup.Count != 5 || len(loaded.Shards) != 0 {
		t.Fatalf("loaded rollup manifest = %+v", loaded)
	}
}

func TestYAMLReaderPreservesUnquotedDatesAndRejectsAmbiguity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	data := `kind: manifest
schema: 1
name: dates
title: Dates
description: Date fixture.
license: CC0-1.0
sources:
  - name: source
    source: Source
    url: https://example.test
    sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    collected_from: 2024-01-01
converted_by:
  tool: test
  version: "1"
  profile: text
  recipe: test/v1
shards: []
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Sources[0].CollectedFrom != "2024-01-01" {
		t.Fatalf("date = %q", manifest.Sources[0].CollectedFrom)
	}

	for name, invalid := range map[string]string{
		"duplicate": "kind: manifest\nkind: manifest\n",
		"documents": "kind: manifest\n---\nkind: manifest\n",
		"alias":     "kind: &kind manifest\nname: *kind\n",
	} {
		t.Run(name, func(t *testing.T) {
			invalidPath := filepath.Join(t.TempDir(), "invalid.yaml")
			if err := os.WriteFile(invalidPath, []byte(invalid), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(invalidPath); err == nil {
				t.Fatal("invalid YAML metadata was accepted")
			}
		})
	}
}

func TestResolveRejectsCompetingDirectoryMetadata(t *testing.T) {
	root := t.TempDir()
	directory := Directory{Kind: "index", Schema: 1, Entries: []Entry{}}
	yamlData, err := MarshalYAML(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.yaml"), yamlData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte(`{"kind":"index","schema":1,"path":"","entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("", root); err == nil || !strings.Contains(err.Error(), "competing metadata") {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestVerifyTraversesMixedJSONAndYAMLCheckout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.json"), `{
  "kind": "index", "schema": 1, "path": "",
  "entries": [{"name": "alpha", "type": "dir"}]
}`)
	alpha := Directory{Kind: "index", Schema: 1, Path: "alpha", Entries: []Entry{{Name: "books.yml", Type: "manifest"}}}
	alphaData, err := MarshalYAML(alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha", "index.yaml"), alphaData, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Kind: "manifest", Schema: 1, Name: "books", Title: "Books", Description: "Mixed encoding fixture.", License: "CC0-1.0",
		Sources:     []Source{{Name: "source", Source: "Source", URL: "https://example.test", SHA256: strings.Repeat("a", 64)}},
		ConvertedBy: Conversion{Tool: "test", Version: "1", Profile: "text", Recipe: "test/v1"},
		Shards:      []Shard{{URL: "https://example.test/object", SHA256: strings.Repeat("b", 64), Sources: []string{"source"}, Docs: 2, Tokens: 3, Bytes: 4}},
	}
	manifestData, err := MarshalYAML(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha", "books.yml"), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := Resolve("", root)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Directories != 2 || verified.Corpora != 1 || verified.Shards != 1 {
		t.Fatalf("verification = %+v", verified)
	}
}
