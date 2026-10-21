// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSeparatedTextPreservesLogicalRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stories.txt")
	writeFixture(t, path, "\nFirst story.\n<|endoftext|>\nSecond story.\n<|endoftext|>")
	profile := InputProfile{Type: ProfileSeparatedText, Separator: "<|endoftext|>", OnEmpty: "skip"}
	rows := collectMappedRows(t, mappedFixturePlan(t, path, profile))
	if len(rows) != 2 || rows[0].Text != "First story.\n" || rows[1].Text != "Second story.\n" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Source == rows[1].Source {
		t.Fatalf("record source identities are not distinct: %q", rows[0].Source)
	}
}

func TestSeparatedTextRejectsOversizedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stories.txt")
	writeFixture(t, path, "too-long<END>ok")
	plan := mappedFixturePlan(t, path, InputProfile{Type: ProfileSeparatedText, Separator: "<END>"})
	plan.Writer.RecordMaximumBytes = 4
	plan.Writer.AdapterBatchBytes = 4
	if err := StreamCanonicalTextBatches(context.Background(), plan, func(TextBatch) error { return nil }); err == nil {
		t.Fatal("oversized separated record was accepted")
	}
}
