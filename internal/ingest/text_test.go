// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamTextBatchesPreservesFilesAndBoundsBatches(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "a.md")
	second := filepath.Join(directory, "b.txt")
	writeFixture(t, first, "# Heading\n\nExact Markdown.\n")
	writeFixture(t, second, "second document")
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
	var batches []TextBatch
	err = streamTextBatches(context.Background(), plan, 20, 64, func(batch TextBatch) error {
		batches = append(batches, batch)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0].Rows) != 1 || len(batches[1].Rows) != 1 {
		t.Fatalf("batch row counts = %+v", batches)
	}
	if got := batches[0].Rows[0].Text; got != "# Heading\n\nExact Markdown.\n" {
		t.Fatalf("first text = %q", got)
	}
	if got := batches[0].Rows[0].Source; got != "sha256:"+plan.Inputs[0].Artifact.SHA256 {
		t.Fatalf("first source = %q", got)
	}
	if batches[0].Rows[0].SourceName == nil || *batches[0].Rows[0].SourceName != "fixture" {
		t.Fatalf("source name = %v", batches[0].Rows[0].SourceName)
	}
}

func TestStreamTextBatchesRejectsChangedArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, path, "original")
	probe, err := ProbePaths(context.Background(), []string{path})
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
	writeFixture(t, path, "modified")
	if err := StreamTextBatches(context.Background(), plan, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("expected changed artifact rejection")
	}
}

func TestStreamTextBatchesRejectsOversizedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, path, "five!")
	probe, err := ProbePaths(context.Background(), []string{path})
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
	if err := streamTextBatches(context.Background(), plan, 4, 4, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("expected oversized record rejection")
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
