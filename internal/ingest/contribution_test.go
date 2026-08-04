package ingest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo-new/internal/index"
)

func TestBuildManifestMatchesCurrentIndexContract(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "a.txt"), "same")
	writeFixture(t, filepath.Join(directory, "b.txt"), "same")
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/fixture", Title: "Fixture", Description: "Fixture corpus.", License: "CC0-1.0",
		Source: PlanSource{Name: "fixture-source", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RecordSchema != 2 || len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Sources[0].Usage["text"].Samples != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.ConvertedBy.Tokenizer != "" {
		t.Fatalf("tokenizer = %q", manifest.ConvertedBy.Tokenizer)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip index.Manifest
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Shards) != 1 || roundTrip.Shards[0].SHA256 != manifest.Shards[0].SHA256 {
		t.Fatalf("round-trip manifest = %+v", roundTrip)
	}
}
