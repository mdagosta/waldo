// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpaqueFallbackLosslesslyRetainsUnknownBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.bin")
	original := []byte{0x00, 0xff, 0x10, 0x80, 'W', 'A', 'L', 'D', 'O'}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "mixed/example", Title: "Mixed", License: "CC0-1.0",
		Source: PlanSource{Name: "example", URL: "https://example.test", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Inputs[0].Adapter != "opaque-base64" || len(plan.TextFallbacks) != 1 || plan.TextFallbacks[0].Adapter != "opaque-base64" {
		t.Fatalf("plan = %+v", plan)
	}
	var encoded string
	if err := StreamOpaqueTextBatches(context.Background(), plan, func(batch TextBatch) error {
		encoded = batch.Rows[0].Text
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(encoded, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatalf("opaque record = %q", encoded)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(original) {
		t.Fatalf("decoded bytes = %x, want %x", decoded, original)
	}
}
