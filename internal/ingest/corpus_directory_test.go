// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
)

func TestCorpusDirectorySingleSourceBoundary(t *testing.T) {
	root := t.TempDir()
	writeProbeFile(t, filepath.Join(root, "nested", "document.txt"), "single source\n")
	raw := testDirectoryRaw(t, root, []string{"nested/document.txt"})
	manifest := CorpusDirectory{
		Kind: CorpusDirectoryKind, Schema: SourceDirectorySchema,
		Corpus: SourceDirectoryCorpus{ID: "example", Title: "Example", Description: "Single source."},
		Source: testDirectorySource("example", "CC0-1.0"), Raw: &raw,
	}
	writeJSONFile(t, filepath.Join(root, "manifest.json"), manifest)

	loaded, ok, err := LoadCorpusDirectory(root)
	if err != nil || !ok {
		t.Fatalf("LoadCorpusDirectory() = ok %v, err %v", ok, err)
	}
	if len(loaded.InputPaths()) != 1 || filepath.Base(loaded.InputPaths()[0]) != "document.txt" {
		t.Fatalf("InputPaths() = %v", loaded.InputPaths())
	}
	probe, err := ProbePaths(context.Background(), loaded.InputPaths())
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.VerifyProbe(probe); err != nil {
		t.Fatal(err)
	}
	request := PlanRequest{Destination: "core/example"}
	loaded.Apply(&request)
	if request.Destination != "core/example" || len(request.Sources) != 1 || request.Sources[0].InputRoot != root {
		t.Fatalf("request = %+v", request)
	}
}

func TestCorpusDirectoryMultipleSourceBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		directory := filepath.Join(root, name)
		writeProbeFile(t, filepath.Join(directory, "nested", name+".txt"), name+" source\n")
		raw := testDirectoryRaw(t, directory, []string{"nested/" + name + ".txt"})
		writeJSONFile(t, filepath.Join(directory, "manifest.json"), sourceDirectoryWire{
			Kind: SourceDirectoryKind, Schema: SourceDirectorySchema,
			Source: *testDirectorySource(name, map[string]string{"alpha": "CC0-1.0", "beta": "Apache-2.0"}[name]), Raw: raw,
		})
	}
	writeJSONFile(t, filepath.Join(root, "manifest.json"), CorpusDirectory{
		Kind: CorpusDirectoryKind, Schema: SourceDirectorySchema,
		Corpus:  SourceDirectoryCorpus{ID: "suite", Title: "Suite", Description: "Multiple sources."},
		Sources: []string{"alpha", "beta"},
	})

	loaded, ok, err := LoadCorpusDirectory(root)
	if err != nil || !ok {
		t.Fatalf("LoadCorpusDirectory() = ok %v, err %v", ok, err)
	}
	if len(loaded.Loaded) != 2 || len(loaded.InputPaths()) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	probe, err := ProbePaths(context.Background(), loaded.InputPaths())
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.VerifyProbe(probe); err != nil {
		t.Fatal(err)
	}
	request := PlanRequest{Destination: "core/suite"}
	loaded.Apply(&request)
	if len(request.Sources) != 2 || request.Sources[0].ID != "alpha" || request.Sources[1].ID != "beta" {
		t.Fatalf("sources = %+v", request.Sources)
	}
}

func TestCorpusDirectoryRejectsUndeclaredEntry(t *testing.T) {
	root := t.TempDir()
	writeProbeFile(t, filepath.Join(root, "unexpected.txt"), "unexpected\n")
	writeJSONFile(t, filepath.Join(root, "manifest.json"), CorpusDirectory{
		Kind: CorpusDirectoryKind, Schema: SourceDirectorySchema,
		Corpus:  SourceDirectoryCorpus{ID: "suite", Title: "Suite", Description: "Multiple sources."},
		Sources: []string{"missing"},
	})
	_, ok, err := LoadCorpusDirectory(root)
	if !ok || err == nil {
		t.Fatalf("LoadCorpusDirectory() = ok %v, err %v", ok, err)
	}
}

func testDirectorySource(id, license string) *DirectorySource {
	return &DirectorySource{
		ID: id, License: license,
		Input: InputProfile{Format: "text"},
		Source: RecipeSource{
			Name: id, URL: "https://example.test/" + id, Category: "public-dataset",
			LicenseEvidence: &index.LicenseEvidence{Declaration: license},
		},
	}
}

func testDirectoryRaw(t *testing.T, root string, paths []string) DirectoryRaw {
	t.Helper()
	sort.Strings(paths)
	hash := sha256.New()
	var bytes int64
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		fmt.Fprintf(hash, "%x\t%d\t%s\n", digest, len(data), relative)
		bytes += int64(len(data))
	}
	return DirectoryRaw{FileCount: int64(len(paths)), ByteCount: bytes, TreeSHA256: fmt.Sprintf("%x", hash.Sum(nil))}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
