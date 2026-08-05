package ingest

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/openwaldo/waldo/internal/shard"
)

func TestCompressedJSONLStreamsThroughCanonicalAdapter(t *testing.T) {
	for _, compression := range []string{"gzip", "zstd"} {
		t.Run(compression, func(t *testing.T) {
			extension := ".jsonl.gz"
			if compression == "zstd" {
				extension = ".jsonl.zst"
			}
			path := filepath.Join(t.TempDir(), "input"+extension)
			writeCompressedJSONL(t, path, compression, "{\"text\":\"first document\",\"metadata\":1}\n{\"text\":\"second document\"}\n")
			probe, err := ProbePaths(context.Background(), []string{path})
			if err != nil {
				t.Fatal(err)
			}
			artifact := probe.Artifacts[0]
			if artifact.Format != "jsonl" || artifact.Compression != compression {
				t.Fatalf("artifact = %+v", artifact)
			}
			plan, err := NewPlan(probe, PlanRequest{
				Destination: "core/jsonl", Title: "JSONL", License: "CC0-1.0",
				Source: PlanSource{Name: "jsonl", URL: "https://example.test/data", Category: "public-dataset"},
			})
			if err != nil {
				t.Fatal(err)
			}
			var texts []string
			err = StreamCanonicalTextBatches(context.Background(), plan, func(batch TextBatch) error {
				for _, row := range batch.Rows {
					texts = append(texts, row.Text)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(texts) != 2 || texts[0] != "first document" || texts[1] != "second document" {
				t.Fatalf("texts = %q", texts)
			}
		})
	}
}

func TestCompressedJSONLAssemblesDocumentAndTokenCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.jsonl.gz")
	writeCompressedJSONL(t, path, "gzip", "{\"text\":\"first document\"}\n{\"text\":\"second document\"}\n")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/jsonl", Title: "JSONL", License: "CC0-1.0",
		Source: PlanSource{Name: "jsonl", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(assembly.Objects) != 1 || assembly.Objects[0].Docs != 2 || assembly.Objects[0].Tokens <= 0 {
		t.Fatalf("assembly = %+v", assembly)
	}
	object, err := os.Open(assembly.Objects[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	stats, err := shard.WriteJSONL(io.Discard, object, assembly.Objects[0].Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Docs != assembly.Objects[0].Docs || stats.Tokens != assembly.Objects[0].Tokens {
		t.Fatalf("export stats = %+v, object = %+v", stats, assembly.Objects[0])
	}
}

func TestSingleRecordCompressedJSONLUsesCompoundExtensionHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.jsonl.gz")
	writeCompressedJSONL(t, path, "gzip", "{\"text\":\"only document\"}\n")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if probe.Artifacts[0].Format != "jsonl" || probe.Artifacts[0].Compression != "gzip" {
		t.Fatalf("artifact = %+v", probe.Artifacts[0])
	}
}

func TestCompressedJSONLRejectsMissingText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl.gz")
	writeCompressedJSONL(t, path, "gzip", "{\"content\":\"not silently inferred\"}\n")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/jsonl", Title: "JSONL", License: "CC0-1.0",
		Source: PlanSource{Name: "jsonl", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := StreamCanonicalTextBatches(context.Background(), plan, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("JSONL without text was accepted")
	}
}

func writeCompressedJSONL(t *testing.T, path, compression, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var write func([]byte) (int, error)
	var closeWriter func() error
	switch compression {
	case "gzip":
		writer := gzip.NewWriter(file)
		write = writer.Write
		closeWriter = writer.Close
	case "zstd":
		writer, err := zstd.NewWriter(file, zstd.WithEncoderConcurrency(1))
		if err != nil {
			t.Fatal(err)
		}
		write = writer.Write
		closeWriter = writer.Close
	default:
		t.Fatalf("unsupported test compression %q", compression)
	}
	if _, err := write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := closeWriter(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
