// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/shard"
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
		Source:    PlanSource{Name: "fixture", URL: "https://example.test/data", Category: "public-dataset"},
		InputRoot: directory,
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
	if batches[0].Rows[0].Meta == nil {
		t.Fatal("recipe-relative source path metadata is absent")
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(*batches[0].Rows[0].Meta), &metadata); err != nil || metadata["source_path"] != "a.md" {
		t.Fatalf("source path metadata = %v, err = %v", metadata, err)
	}
}

func TestStreamTextBatchesLeavesDirectInputMetadataAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, path, "direct input")
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
	var row shard.TextRow
	if err := StreamTextBatches(context.Background(), plan, func(batch TextBatch) error {
		row = batch.Rows[0]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if row.Meta != nil {
		t.Fatalf("direct input unexpectedly gained metadata: %q", *row.Meta)
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

func TestStreamTextBatchesChunksOversizedFilesLosslessly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	contents := strings.Repeat("abcé", 20)
	writeFixture(t, path, contents)
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "text/example", Title: "Example", License: "CC0-1.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var rebuilt strings.Builder
	rows := 0
	if err := streamTextBatches(context.Background(), plan, 17, 17, func(batch TextBatch) error {
		for _, row := range batch.Rows {
			if !utf8.ValidString(row.Text) {
				t.Fatalf("invalid chunk %q", row.Text)
			}
			rebuilt.WriteString(row.Text)
			rows++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rows < 2 || rebuilt.String() != contents {
		t.Fatalf("rows=%d rebuilt=%q", rows, rebuilt.String())
	}
}

func TestStreamTextBatchesRejectsLateNULWhenProbeMissesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MediaController.java")
	original := append([]byte(strings.Repeat("a", probeBytes+1)), 0)
	original = append(original, []byte("still retained")...)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "code/example", Title: "Example", License: "Apache-2.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Inputs[0].Adapter != "text" {
		t.Fatalf("adapter = %q", plan.Inputs[0].Adapter)
	}
	err = streamTextBatches(context.Background(), plan, 1<<20, 1<<20, func(batch TextBatch) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "requires NUL-free UTF-8") {
		t.Fatalf("error = %v", err)
	}
}

func TestOversizedTextWithLateNULFailsBeforeEmittingTextChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.java")
	original := append([]byte(strings.Repeat("b", probeBytes+1)), 0)
	original = append(original, []byte("tail")...)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "code/example", Title: "Example", License: "Apache-2.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var rows []shard.TextRow
	err = streamTextBatches(context.Background(), plan, 17, 17, func(batch TextBatch) error {
		rows = append(rows, batch.Rows...)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "requires NUL-free UTF-8") {
		t.Fatalf("error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("emitted %d rows before validation failed", len(rows))
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
