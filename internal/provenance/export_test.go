// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/index"
)

func TestWriteCorpusExport(t *testing.T) {
	destination := t.TempDir()
	document := exportFixture("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4)
	if err := WriteCorpusExport(destination, document); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "EXPORT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded CorpusExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != "waldo-corpus-export" || decoded.Format != "native" || decoded.Generated != "2026-08-04T12:00:00Z" || len(decoded.Files) != 1 {
		t.Fatalf("decoded export = %+v", decoded)
	}
}

func TestVerifyCorpusExportHashesFiles(t *testing.T) {
	destination := t.TempDir()
	content := []byte("native object")
	digestArray := sha256.Sum256(content)
	digest := hex.EncodeToString(digestArray[:])
	document := exportFixture(digest, int64(len(content)))
	filePath := filepath.Join(destination, filepath.FromSlash(document.Files[0].Path))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCorpusExport(destination, document); err != nil {
		t.Fatal(err)
	}
	_, report, err := VerifyCorpusExport(destination)
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 1 || report.Bytes != int64(len(content)) {
		t.Fatalf("verification = %+v", report)
	}
	if err := os.WriteFile(filePath, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyCorpusExport(destination); err == nil {
		t.Fatal("expected corrupt export failure")
	}
}

func TestCorpusExportValidationRejectsTraversalAndIncorrectTotals(t *testing.T) {
	document := exportFixture("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4)
	document.Files[0].Path = "../escape.parquet"
	if err := document.Validate(); err == nil {
		t.Fatal("expected traversal path failure")
	}
	document = exportFixture("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4)
	document.BOM.Totals.Tokens++
	if err := document.Validate(); err == nil {
		t.Fatal("expected totals failure")
	}
}

func TestCorpusExportSchemaOneGolden(t *testing.T) {
	document := exportFixture("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4)
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append(data, '\n'))
	got := hex.EncodeToString(digest[:])
	const want = "5b15d0031efcb93086b060b3f5b2bd28c1b4d5a48aa7794ab860d0c448a8066b"
	if got != want {
		t.Fatalf("schema-1 golden hash = %s, want %s", got, want)
	}
}

func exportFixture(objectHash string, objectBytes int64) CorpusExport {
	measure := index.Measures{Shards: 1, Docs: 1, Tokens: 2, Bytes: objectBytes}
	conversion := index.Conversion{Tool: "fixture", Version: "1", Profile: "text", Recipe: "fixture/v1", Tokenizer: "byte"}
	bom := corpus.BOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"books"},
		Manifests: []corpus.ManifestPin{{
			Path: "books/books.json", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Name: "books", Title: "Books", Description: "Fixture.", License: "CC0-1.0",
			Format: "parquet", RecordSchema: 1, ConvertedBy: conversion,
			Sources: []index.Source{{Name: "source", Source: "Fixture", URL: "https://example.test", SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}},
			Totals:  measure, Licenses: map[string]index.Measures{"CC0-1.0": measure},
		}},
		Shards: []corpus.ShardPin{{
			Manifest: "books/books.json", URL: "https://objects.test/item", SHA256: objectHash,
			Format: "parquet", RecordSchema: 1, License: "CC0-1.0", Sources: []string{"source"},
			ConvertedBy: conversion, Docs: 1, Tokens: 2, Bytes: objectBytes,
		}},
		Totals: measure, Licenses: map[string]index.Measures{"CC0-1.0": measure},
	}
	files := []corpus.ExportedFile{{
		Path: "data/books/item.parquet", Manifest: "books/books.json", ObjectSHA256: objectHash,
		SHA256: objectHash, Format: "parquet", License: "CC0-1.0", Docs: 1, Tokens: 2,
		ObjectBytes: objectBytes, Bytes: objectBytes,
	}}
	return NewCorpusExport(bom, "native", files, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
}
