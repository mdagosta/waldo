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
	"testing"

	"github.com/openwaldo/waldo/internal/index"
)

func TestStageContributionProducesMinimalValidIndexOverlay(t *testing.T) {
	root := t.TempDir()
	writeIndexJSON(t, filepath.Join(root, "index.json"), index.Directory{
		Kind: "index", Schema: 1, Path: "", Entries: []index.Entry{{Name: "core", Type: "dir"}},
	})
	writeIndexJSON(t, filepath.Join(root, "core", "index.json"), index.Directory{
		Kind: "index", Schema: 1, Path: "core", Entries: []index.Entry{},
	})
	input := filepath.Join(t.TempDir(), "input.txt")
	writeFixture(t, input, "document")
	plan := textFixturePlan(t, input)
	plan.Destination = "core/example"
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	result, err := StageContribution(root, staging, plan, manifest)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := StageContribution(root, staging, plan, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Root != result.Root || len(resumed.Files) != len(result.Files) {
		t.Fatalf("resumed contribution = %+v, want %+v", resumed, result)
	}
	want := []string{"core/example/example.yaml", "core/example/index.yaml", "core/index.yaml"}
	if len(result.Files) != len(want) {
		t.Fatalf("files = %v", result.Files)
	}
	for index := range want {
		if result.Files[index] != want[index] {
			t.Fatalf("files = %v", result.Files)
		}
	}
	wantRemoved := []string{"core/index.json"}
	if len(result.Removed) != len(wantRemoved) {
		t.Fatalf("removed = %v", result.Removed)
	}
	for position, relative := range wantRemoved {
		if result.Removed[position] != relative {
			t.Fatalf("removed = %v", result.Removed)
		}
	}
	result, err = ApplyContribution(root, result)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.IndexRoot != root {
		t.Fatalf("applied contribution = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "core", "index.json")); !os.IsNotExist(err) {
		t.Fatalf("superseded index still exists: %v", err)
	}
	target, err := index.Resolve(root, "")
	if err != nil {
		t.Fatal(err)
	}
	verification, err := index.Verify(target)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Corpora != 1 || verification.Shards != 1 {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestApplyContributionRollsBackInvalidIndex(t *testing.T) {
	root := t.TempDir()
	original := index.Directory{Kind: "index", Schema: 1, Path: ""}
	writeIndexJSON(t, filepath.Join(root, "index.json"), original)
	before, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	overlay := t.TempDir()
	if err := os.WriteFile(filepath.Join(overlay, "index.json"), []byte("not valid index data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ApplyContribution(root, ContributionResult{Root: overlay, Files: []string{"index.json"}})
	if err == nil {
		t.Fatal("expected invalid applied index to fail")
	}
	after, readErr := os.ReadFile(filepath.Join(root, "index.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Fatalf("rollback changed index:\n%s", after)
	}
}

func writeIndexJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWorkLocationsRejectsCheckoutOverlap(t *testing.T) {
	root := t.TempDir()
	if err := ValidateWorkLocations(root, filepath.Join(root, "work"), t.TempDir()); err == nil {
		t.Fatal("expected staging overlap rejection")
	}
	outside := t.TempDir()
	if err := ValidateWorkLocations(root, outside, filepath.Join(outside, "cache")); err == nil {
		t.Fatal("expected staging/cache overlap rejection")
	}
}
