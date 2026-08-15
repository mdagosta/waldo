// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/shard"
)

func TestUnassessedContentFilterWarning(t *testing.T) {
	present := true
	bom := corpus.BOM{
		Paths: []string{"books"},
		RecordFilter: &corpus.RecordFilterPolicy{Schema: corpus.RecordFilterSchema, Global: &corpus.RecordFilter{Exclude: &corpus.ExclusionFilter{
			RepetitiveContent: &present, BoilerplateContent: &present,
		}}},
		Shards: []corpus.ShardPin{
			{Manifest: "books/one.yaml", RecordSchema: shard.FormerTextRecordSchema},
			{Manifest: "books/two.yaml", RecordSchema: shard.TextRecordSchema},
		},
	}
	var output bytes.Buffer
	emitUnassessedFilterWarning(&output, "pretrain", bom)
	warning := output.String()
	if !strings.Contains(warning, "warning: stage pretrain: 1 schema-1 shard") || !strings.Contains(warning, "boilerplate_content, repetitive_content filters will be ignored") {
		t.Fatalf("warning = %q", warning)
	}
}
