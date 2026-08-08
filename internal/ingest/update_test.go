// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/record"
)

func TestAppendSeedDropsExistingContentWithoutProducingShard(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.txt")
	writeFixture(t, input, "already indexed")
	plan := textFixturePlan(t, input)
	result, err := AssembleTextObjectsWithSeedAndSink(context.Background(), plan, t.TempDir(), func(add func([]DedupIdentity) error) error {
		return add([]DedupIdentity{{SHA256: record.TextHash("already indexed"), License: plan.License}})
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 1 || result.RetainedDocs != 0 || result.DuplicateDocs != 1 || len(result.Objects) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAppendResolvesLegacyInheritedLicenseBeforeAddingAnother(t *testing.T) {
	input := filepath.Join(t.TempDir(), "document.txt")
	writeFixture(t, input, "new document")
	plan := textFixturePlan(t, input)
	plan.License = "MIT"
	plan.Update = &UpdatePlan{Manifest: "example.json", ManifestSHA256: fmt.Sprintf("%064x", 3), Mode: "append"}
	assembly, err := AssembleTextObjects(context.Background(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing := index.Manifest{
		Kind: "manifest", Schema: index.LegacyManifestSchema, Name: "example", Title: "Example",
		Description: "Legacy example.", License: "CC0-1.0", RecordSchema: 1,
		Sources:     []index.Source{{Name: "legacy", Source: "legacy", URL: "https://example.test/legacy", SHA256: fmt.Sprintf("%064x", 1)}},
		ConvertedBy: index.Conversion{Tool: "waldo", Version: "1", Profile: "text", Recipe: "parquet-v1"},
		Shards:      []index.Shard{{URL: "https://example.test/object", SHA256: fmt.Sprintf("%064x", 2), Sources: []string{"legacy"}, Docs: 1, Tokens: 2, Bytes: 3}},
	}
	updated, err := BuildUpdatedManifest(plan, existing, assembly, "https://example.test/objects", "example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Schema != index.ManifestSchema || updated.Shards[0].License != "CC0-1.0" || len(updated.Licenses) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestPreserveSourceContextReplacesDeclaredEvidence(t *testing.T) {
	existing := []index.Source{{
		Name: "source", Source: "upstream", Version: "old", URL: "https://example.test/source",
		CollectedFrom: "2024", CollectedTo: "2025",
		LicenseEvidence: &index.LicenseEvidence{Declaration: "old"},
		Content:         &index.Content{Selection: "old selection"},
		Acquisition:     &index.Acquisition{Basis: "old acquisition"},
		Usage:           index.Modalities{"text": {Samples: 1}},
	}}
	fresh := []index.Source{{
		Name: "source", Source: "upstream", URL: "https://example.test/source",
	}}

	got := preserveSourceContext(existing, fresh)
	if got[0].Version != "" || got[0].CollectedFrom != "" || got[0].CollectedTo != "" ||
		got[0].LicenseEvidence != nil || got[0].Content != nil || got[0].Acquisition != nil {
		t.Fatalf("stale declared evidence survived rebuild: %+v", got[0])
	}
	if got[0].Usage["text"].Samples != 1 {
		t.Fatalf("derived source context was not preserved: %+v", got[0].Usage)
	}
}
