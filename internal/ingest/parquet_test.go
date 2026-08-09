// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/shard"
	"github.com/parquet-go/parquet-go"
)

type rawParquetFixture struct {
	ID      int64  `parquet:"id"`
	Text    string `parquet:"text"`
	Ignored string `parquet:"ignored"`
}

type nullableParquetFixture struct {
	Text *string `parquet:"text"`
}

type unmappedParquetFixture struct {
	File int64 `parquet:"file"`
}

func TestStreamParquetTextBatchesProjectsDirectlyIntoCanonicalWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.parquet")
	if err := parquet.WriteFile(path, []rawParquetFixture{
		{ID: 1, Text: "first", Ignored: "not canonical"},
		{ID: 2, Text: "second", Ignored: "still not canonical"},
	}); err != nil {
		t.Fatal(err)
	}
	plan := parquetFixturePlan(t, path)
	var output bytes.Buffer
	writer := shard.NewTextParquetWriter(&output)
	var logical int64
	err := StreamParquetTextBatches(context.Background(), plan, func(batch TextBatch) error {
		logical += batch.LogicalBytes
		_, err := writer.Write(batch.Rows)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if logical != int64(len("firstsecond")) {
		t.Fatalf("logical bytes = %d", logical)
	}
	file, err := parquet.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if file.NumRows() != 2 {
		t.Fatalf("canonical rows = %d", file.NumRows())
	}
	reader := parquet.NewGenericReader[shard.TextRow](bytes.NewReader(output.Bytes()))
	rows := make([]shard.TextRow, 2)
	if count, err := reader.Read(rows); count != 2 || (err != nil && !errors.Is(err, io.EOF)) {
		t.Fatalf("read canonical rows = %d, %v", count, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if rows[0].Text != "first" || rows[1].Text != "second" || rows[0].Source == rows[1].Source {
		t.Fatalf("canonical rows = %+v", rows)
	}
}

func TestStreamParquetTextBatchesRejectsChangedArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.parquet")
	if err := parquet.WriteFile(path, []rawParquetFixture{{Text: "first"}}); err != nil {
		t.Fatal(err)
	}
	plan := parquetFixturePlan(t, path)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("changed")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := StreamParquetTextBatches(context.Background(), plan, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("expected changed artifact rejection")
	}
}

func TestStreamParquetTextBatchesRejectsNullText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.parquet")
	if err := parquet.WriteFile(path, []nullableParquetFixture{{Text: nil}}); err != nil {
		t.Fatal(err)
	}
	plan := parquetFixturePlan(t, path)
	if err := StreamParquetTextBatches(context.Background(), plan, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("expected null text rejection")
	}
}

func TestPlanRejectsNestedParquetTextMapping(t *testing.T) {
	artifact := Artifact{Parquet: &ParquetInfo{Columns: []string{"payload.text"}}}
	if _, err := chooseTextColumn(artifact, "payload.text"); err == nil {
		t.Fatal("expected nested mapping rejection")
	}
}

func TestDefaultParquetWithoutTextColumnFallsBackLosslessly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.parquet")
	if err := parquet.WriteFile(path, []unmappedParquetFixture{{File: 42}}); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "code/example", Title: "Code", License: "Apache-2.0",
		Source: PlanSource{Name: "example", URL: "https://example.test/code", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Inputs[0].Adapter != "opaque-base64" || len(plan.TextFallbacks) != 1 || plan.TextFallbacks[0].Adapter != "opaque-base64" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestExplicitParquetTextColumnRemainsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.parquet")
	if err := parquet.WriteFile(path, []unmappedParquetFixture{{File: 42}}); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPlan(probe, PlanRequest{
		Destination: "code/example", Title: "Code", License: "Apache-2.0", TextColumn: "text",
		Source: PlanSource{Name: "example", URL: "https://example.test/code", Category: "public-dataset"},
	})
	if err == nil {
		t.Fatal("expected explicit missing text column rejection")
	}
}

func TestStreamCanonicalTextBatchesRoutesMixedInputsInPlanOrder(t *testing.T) {
	directory := t.TempDir()
	textPath := filepath.Join(directory, "a.txt")
	writeFixture(t, textPath, "plain")
	parquetPath := filepath.Join(directory, "b.parquet")
	if err := parquet.WriteFile(parquetPath, []rawParquetFixture{{Text: "columnar"}}); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC-BY-4.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/raw", Category: "public-dataset"},
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
	if len(texts) != 2 || texts[0] != "plain" || texts[1] != "columnar" {
		t.Fatalf("texts = %v", texts)
	}
}

func parquetFixturePlan(t *testing.T, path string) Plan {
	t.Helper()
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/example", Title: "Example", License: "CC-BY-4.0",
		Source: PlanSource{Name: "fixture", URL: "https://example.test/raw", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
