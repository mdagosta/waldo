// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/record"
	"github.com/parquet-go/parquet-go"
)

func TestWriteJSONL(t *testing.T) {
	text := "portable text"
	rows := []Row{{
		SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text,
		Source: "fixture", License: "CC-BY-4.0", Meta: `{"z":2}`,
	}}
	var native bytes.Buffer
	writer := parquet.NewGenericWriter[Row](&native)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var interchange bytes.Buffer
	stats, err := WriteJSONL(&interchange, bytes.NewReader(native.Bytes()), int64(native.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Bytes != int64(interchange.Len()) || stats.Docs != 1 || !strings.Contains(interchange.String(), `"text":"portable text"`) || !strings.HasSuffix(interchange.String(), "\n") {
		t.Fatalf("JSONL = %q, stats = %+v", interchange.String(), stats)
	}
}

func TestWriteJSONLRejectsInvalidRecord(t *testing.T) {
	rows := []Row{{SHA256: record.TextHash("different"), Kind: record.KindPretrain, Text: "text", Source: "fixture", License: "CC0-1.0"}}
	var native bytes.Buffer
	writer := parquet.NewGenericWriter[Row](&native)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteJSONL(&bytes.Buffer{}, bytes.NewReader(native.Bytes()), int64(native.Len())); err == nil {
		t.Fatal("expected invalid record error")
	}
}

func TestWriteJSONLReadsCanonicalSchemaOnePhysicalRecipe(t *testing.T) {
	var native bytes.Buffer
	hash := sha256.Sum256([]byte("hello"))
	writer := NewTextParquetWriter(&native)
	if _, err := writer.Write([]TextRow{{
		ContentSHA256: hash, Text: "hello", Source: "fixture:1", License: "CC0-1.0",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	stats, err := WriteJSONL(&output, bytes.NewReader(native.Bytes()), int64(native.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Docs != 1 || stats.Tokens != 0 || !strings.Contains(output.String(), `"kind":"pretrain"`) || !strings.Contains(output.String(), `"text":"hello"`) {
		t.Fatalf("output/stats = %q / %+v", output.String(), stats)
	}
}
