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
	if len(first) != 2 || first[0].Docs != 2 || first[1].Docs != 1 {
		t.Fatalf("objects = %+v", first)
	}
	second, err := AssembleTextObjects(context.Background(), plan, filepath.Join(t.TempDir(), "second"))
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) {
		t.Fatalf("second objects = %+v", second)
	}
	for index := range first {
		if first[index].SHA256 != second[index].SHA256 || first[index].Bytes != second[index].Bytes {
			t.Fatalf("object %d is not deterministic: %+v / %+v", index, first[index], second[index])
		}
		if _, err := os.Stat(first[index].Path); err != nil {
			t.Fatal(err)
		}
	}
}
