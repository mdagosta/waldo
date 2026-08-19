// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testMbox = "From sender@example.test Mon Jan  1 00:00:00 2024\nFrom: Sender <sender@example.test>\nSubject: First\n\nFirst body.\nFrom other@example.test Tue Jan  2 00:00:00 2024\nFrom: Other <other@example.test>\nSubject: Second\n\nSecond body.\n"

func TestMboxStreamsOneRawMessagePerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.mbox")
	writeFixture(t, path, testMbox)
	assertMboxRecords(t, path, "")
}

func TestGzipMboxStreamsOneRawMessagePerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.mbox.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte(testMbox)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	assertMboxRecords(t, path, "gzip")
}

func assertMboxRecords(t *testing.T, path, compression string) {
	t.Helper()
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if probe.Artifacts[0].Format != "mbox" || probe.Artifacts[0].Compression != compression {
		t.Fatalf("artifact = %+v", probe.Artifacts[0])
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "community/mail", Title: "Mail", License: "CC0-1.0",
		Source: PlanSource{Name: "mail", URL: "https://example.test/mail", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Inputs[0].Adapter != "mbox" {
		t.Fatalf("adapter = %q", plan.Inputs[0].Adapter)
	}
	var texts []string
	if err := StreamCanonicalTextBatches(context.Background(), plan, func(batch TextBatch) error {
		for _, row := range batch.Rows {
			texts = append(texts, row.Text)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || texts[0] != "From: Sender <sender@example.test>\nSubject: First\n\nFirst body.\n" || texts[1] != "From: Other <other@example.test>\nSubject: Second\n\nSecond body.\n" {
		t.Fatalf("texts = %#v", texts)
	}
}
