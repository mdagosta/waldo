// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownBinaryFormatIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.bin")
	original := []byte{0x00, 0xff, 0x10, 0x80, 'W', 'A', 'L', 'D', 'O'}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPlan(probe, PlanRequest{
		Destination: "mixed/example", Title: "Mixed", License: "CC0-1.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported raw format "unknown"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestLargeUnknownBinaryFormatIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.bin")
	original := make([]byte, opaqueChunkBytes*2+17)
	for position := range original {
		original[position] = byte(position % 251)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPlan(probe, PlanRequest{
		Destination: "mixed/example", Title: "Mixed", License: "CC0-1.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported raw format "unknown"`) {
		t.Fatalf("error = %v", err)
	}
}
