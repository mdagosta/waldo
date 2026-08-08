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
