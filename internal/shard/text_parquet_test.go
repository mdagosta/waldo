// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package shard

import (
	"bytes"
	"crypto/sha256"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestTextRowPhysicalSchemaAndMetadata(t *testing.T) {
	var output bytes.Buffer
	writer := NewTextParquetWriter(&output)
	hash := sha256.Sum256([]byte("hello"))
	if _, err := writer.Write([]TextRow{{
		ContentSHA256: hash, Text: "hello", Source: "fixture:1", License: "CC0-1.0",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parquet.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{
		"content_sha256", "text", "source", "source_name", "license", "license_raw",
		"language", "language_score", "date", "token_count", "meta", "email_addresses",
	}
	columns := file.Schema().Columns()
	if len(columns) != len(wantColumns) {
		t.Fatalf("columns = %v", columns)
	}
	for i, want := range wantColumns {
		if len(columns[i]) != 1 || columns[i][0] != want {
			t.Fatalf("column %d = %v, want %s", i, columns[i], want)
		}
	}
	if !strings.Contains(file.Schema().String(), "fixed_len_byte_array(32) content_sha256") {
		t.Fatalf("schema does not use a 32-byte hash:\n%s", file.Schema())
	}
	if value, ok := file.Lookup("waldo.recipe"); !ok || value != TextWriterRecipe {
		t.Fatalf("waldo.recipe = %q, %v", value, ok)
	}
	if file.NumRows() != 1 || len(file.RowGroups()) != 1 {
		t.Fatalf("file rows/groups = %d/%d", file.NumRows(), len(file.RowGroups()))
	}
}

func BenchmarkTextParquetRecipes(b *testing.B) {
	rows, logicalBytes := benchmarkTextRows(16 << 20)
	b.SetBytes(logicalBytes)
	benchmarks := []struct {
		name    string
		options []parquet.WriterOption
	}{
		{name: "former-zstd-best-page-4m", options: formerTextWriterOptions()},
		{name: "candidate-zstd-3-page-1m", options: textWriterOptionsForLevel(3)},
		{name: "proposed-zstd-6-page-1m", options: proposedTextWriterOptions()},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.SetBytes(logicalBytes)
			b.ReportAllocs()
			var encodedBytes int64
			for range b.N {
				output := new(countingWriter)
				writer := parquet.NewGenericWriter[TextRow](output, benchmark.options...)
				if _, err := writer.Write(rows); err != nil {
					b.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					b.Fatal(err)
				}
				encodedBytes += output.n
			}
			b.ReportMetric(float64(encodedBytes)/float64(b.N), "encoded-B/op")
		})
	}
}

type countingWriter struct {
	n int64
}

func (writer *countingWriter) Write(data []byte) (int, error) {
	writer.n += int64(len(data))
	return len(data), nil
}

func benchmarkTextRows(target int64) ([]TextRow, int64) {
	const documentBytes = 16 << 10
	rows := make([]TextRow, 0, target/documentBytes+1)
	var logical int64
	for logical < target {
		text := benchmarkDocument(documentBytes, len(rows))
		hash := sha256.Sum256([]byte(text))
		rows = append(rows, TextRow{
			ContentSHA256: hash, Text: text, Source: "benchmark:" + strings.Repeat("0", len(rows)%12),
			License: "CC-BY-4.0",
		})
		logical += int64(len(text))
	}
	return rows, logical
}

func benchmarkDocument(size, seed int) string {
	words := []string{"training", "corpus", "parquet", "provenance", "language", "document", "model", "source"}
	random := rand.New(rand.NewPCG(uint64(seed+1), uint64(seed+17)))
	var builder strings.Builder
	builder.Grow(size)
	for builder.Len() < size {
		builder.WriteString(words[random.IntN(len(words))])
		builder.WriteByte(' ')
	}
	text := builder.String()
	return text[:size]
}
