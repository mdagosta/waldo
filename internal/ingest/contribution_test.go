// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/tokenizer"
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
	if manifest.Schema != index.ManifestSchema || manifest.RecordSchema != 1 || len(manifest.Shards) != 1 || manifest.Shards[0].Docs != 1 || manifest.Shards[0].Tokens <= 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Format != "" || manifest.Processing != nil || manifest.ComposedBy != nil || len(manifest.Sources[0].Files) != 0 || len(manifest.Sources[0].Usage) != 0 || manifest.Sources[0].Content != nil || len(manifest.Shards[0].Modalities) != 0 {
		t.Fatalf("generated manifest contains expanded metadata: %+v", manifest)
	}
	if manifest.ConvertedBy.Tokenizer != tokenizer.Default {
		t.Fatalf("tokenizer = %q", manifest.ConvertedBy.Tokenizer)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"files"`, `"usage"`, `"content"`, `"processing"`, `"composed_by"`, `"modalities"`, `"format"`} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("manifest contains expanded field %s: %s", forbidden, data)
		}
	}
	var roundTrip index.Manifest
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Shards) != 1 || roundTrip.Shards[0].SHA256 != manifest.Shards[0].SHA256 {
		t.Fatalf("round-trip manifest = %+v", roundTrip)
	}
}

func TestDeclarativeProfileIsPartOfConversionAndSourceIdentity(t *testing.T) {
	base := Plan{Inputs: []PlanInput{{
		Artifact: Artifact{SHA256: fmt.Sprintf("%064x", 1), Bytes: 10, Format: "json"}, Adapter: "json",
		Profile: InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"body"}}},
	}}}
	changed := base
	changed.Inputs = append([]PlanInput(nil), base.Inputs...)
	changed.Inputs[0].Profile.Fields.Text = []string{"content"}
	baseSource, err := sourceAcquisitionIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	changedSource, err := sourceAcquisitionIdentity(changed)
	if err != nil {
		t.Fatal(err)
	}
	if baseSource == changedSource || conversionProfile(base) == conversionProfile(changed) {
		t.Fatal("profile mapping did not change persisted identities")
	}
}

func TestBuildManifestSizeDoesNotScaleWithInputArtifactCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.txt")
	writeFixture(t, path, "seed document")
	probe, err := ProbePaths(context.Background(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{
		Destination: "core/large-source", Title: "Large source", Description: "Many acquired files.", License: "CC0-1.0",
		Source: PlanSource{Name: "large-source", URL: "https://example.test/data", Category: "public-dataset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := plan.Inputs[0]
	plan.Inputs = make([]PlanInput, 25_000)
	root := t.TempDir()
	for position := range plan.Inputs {
		input := seed
		input.Artifact.Path = filepath.Join(root, fmt.Sprintf("%06d.txt", position))
		input.SourcePath = fmt.Sprintf("%06d.txt", position)
		plan.Inputs[position] = input
	}
	assembly := AssemblyResult{
		Objects:   []ObjectResult{{SHA256: fmt.Sprintf("%064x", 1), Bytes: 1024, Docs: 25_000, Tokens: 50_000, LogicalBytes: 500_000, License: plan.License}},
		InputDocs: 25_000, RetainedDocs: 25_000,
	}
	manifest, err := BuildManifest(plan, assembly, "s3://openwaldo/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 4<<10 {
		t.Fatalf("manifest is %d bytes for 25,000 inputs; want at most 4 KiB", len(data))
	}
	if bytes.Contains(data, []byte(`"files"`)) {
		t.Fatal("manifest contains per-input evidence")
	}
}

func TestCompactCollectorPinsCleanAndDirtyRecipes(t *testing.T) {
	clean := &index.IngestRecipeEvidence{
		Path: "recipes/common-pile/foodista.yaml", Repository: "git@github.com:openwaldo/waldo-fetchers.git",
		Commit: "abc123", SHA256: fmt.Sprintf("%064x", 1),
	}
	if got, want := compactCollector(clean), "git@github.com:openwaldo/waldo-fetchers@abc123:recipes/common-pile/foodista.yaml"; got != want {
		t.Fatalf("clean collector = %q, want %q", got, want)
	}
	dirty := *clean
	dirty.Dirty = true
	if got, want := compactCollector(&dirty), "git@github.com:openwaldo/waldo-fetchers@abc123+dirty:recipes/common-pile/foodista.yaml#sha256="+dirty.SHA256; got != want {
		t.Fatalf("dirty collector = %q, want %q", got, want)
	}
}
