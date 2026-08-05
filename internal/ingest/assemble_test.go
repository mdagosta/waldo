package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAssembleTextObjectsRotatesVerifiesAndIsDeterministic(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "a.txt"), "aaaaaa")
	writeFixture(t, filepath.Join(directory, "b.txt"), "bbbbbb")
	writeFixture(t, filepath.Join(directory, "c.txt"), "cccccc")
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Writer.RowGroupLogicalBytes = 12
	plan.Writer.CompressedTarget = 1
	plan.Writer.CompressedMaximum = 1 << 20
	first, err := AssembleTextObjects(context.Background(), plan, filepath.Join(t.TempDir(), "first"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Objects) != 2 || first.Objects[0].Docs != 2 || first.Objects[1].Docs != 1 || first.Objects[0].Tokens <= 0 || first.Objects[1].Tokens <= 0 {
		t.Fatalf("objects = %+v", first)
	}
	second, err := AssembleTextObjects(context.Background(), plan, filepath.Join(t.TempDir(), "second"))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Objects) != len(first.Objects) {
		t.Fatalf("second objects = %+v", second)
	}
	for index := range first.Objects {
		if first.Objects[index].SHA256 != second.Objects[index].SHA256 || first.Objects[index].Bytes != second.Objects[index].Bytes {
			t.Fatalf("object %d is not deterministic: %+v / %+v", index, first.Objects[index], second.Objects[index])
		}
		if _, err := os.Stat(first.Objects[index].Path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAssembleTextObjectsDeduplicatesOnDisk(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "a.txt"), "same")
	writeFixture(t, filepath.Join(directory, "b.txt"), "same")
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 2 || result.RetainedDocs != 1 || result.DuplicateDocs != 1 || len(result.Objects) != 1 || result.Objects[0].Docs != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAssembleTextObjectsRefusesUnimplementedCanonicalMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	writeFixture(t, path, "text")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC0-1.0", Mode: "canonical",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AssembleTextObjects(context.Background(), plan, t.TempDir()); err == nil {
		t.Fatal("expected canonical-mode execution rejection")
	}
}
