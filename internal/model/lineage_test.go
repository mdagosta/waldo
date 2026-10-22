// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// releasePackage writes an exported package whose release BOM claims lineage;
// salt changes one weight byte so packages can hold different weights.
func releasePackage(t *testing.T, lineage []string, salt byte) string {
	t.Helper()
	architecture := Architecture{Family: "decoder-transformer", ContextTokens: 32, VocabularySize: 259, HiddenSize: 4, IntermediateSize: 8, Layers: 1, AttentionHeads: 2, KeyValueHeads: 1, TieEmbeddings: true, ParameterDType: "bfloat16", Tokenizer: Tokenizer{Name: "byte", Revision: "builtin-byte-schema-1"}}
	directory := t.TempDir()
	files := huggingFaceFixture(t, architecture, HuggingFaceTokenizerClass)
	weights := files["model.safetensors"]
	weights[len(weights)-1] ^= salt
	bom, err := json.Marshal(map[string]any{"kind": "openwaldo-bom", "schema": 1, "subject": "model-release", "lineage": lineage})
	if err != nil {
		t.Fatal(err)
	}
	files["BOM.json"] = bom
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestMergeComposeRecordsLineageAndFailsClosed(t *testing.T) {
	common, other := strings.Repeat("a", 64), strings.Repeat("b", 64)
	root := t.TempDir()
	for name, directory := range map[string]string{"left": releasePackage(t, []string{common}, 0), "right": releasePackage(t, []string{common}, 1), "stranger": releasePackage(t, []string{other}, 2)} {
		if _, err := (Puller{Root: root}).PullDirectory(name, directory); err != nil {
			t.Fatal(err)
		}
	}
	builder := Builder{Root: root}
	merged, err := builder.Compose(context.Background(), "merged", Compose{Kind: "waldo-model-compose", Schema: ComposeSchema, Base: &ComposeBase{Merge: []ComposeMergeParent{{Model: "left", Weight: 1}, {Model: "right", Weight: 3}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	origin := merged.Origin
	if origin == nil || origin.Source.Provider != "merge" || origin.Merge == nil || origin.Merge.Method != "weighted-mean" || len(origin.Merge.Parents) != 2 || origin.Merge.Parents[1].Weight != 3 {
		t.Fatalf("origin = %+v", origin)
	}
	if !slices.Contains(origin.Lineage, common) || !slices.Contains(origin.Lineage, origin.Merge.Parents[0].WeightsSHA256) {
		t.Fatalf("lineage = %v", origin.Lineage)
	}
	if merged.BOM.CurrentOriginSHA256 == "" || len(merged.Runs) != 0 {
		t.Fatalf("merged model is not usable from its origin: %+v", merged.BOM)
	}
	// The merged model's own descendants inherit everything it descends from.
	lineage, err := ExportLineage(merged)
	if err != nil || !slices.Contains(lineage, common) {
		t.Fatalf("export lineage = %v, %v", lineage, err)
	}
	_, err = builder.Compose(context.Background(), "unrelated", Compose{Kind: "waldo-model-compose", Schema: ComposeSchema, Base: &ComposeBase{Merge: []ComposeMergeParent{{Model: "left"}, {Model: "stranger"}}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "share no recorded ancestor") {
		t.Fatalf("unrelated merge error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "unrelated")); !os.IsNotExist(err) {
		t.Fatalf("unrelated merge left a model: %v", err)
	}
}

func TestMergeComposeValidation(t *testing.T) {
	for name, base := range map[string]ComposeBase{
		"one parent":     {Merge: []ComposeMergeParent{{Model: "a"}}},
		"duplicate":      {Merge: []ComposeMergeParent{{Model: "a"}, {Model: "a"}}},
		"partial weight": {Merge: []ComposeMergeParent{{Model: "a", Weight: 2}, {Model: "b"}}},
		"with model":     {Model: "c", Merge: []ComposeMergeParent{{Model: "a"}, {Model: "b"}}},
		"run pin":        {RunID: "r", Merge: []ComposeMergeParent{{Model: "a"}, {Model: "b"}}},
	} {
		if err := (Compose{Kind: "waldo-model-compose", Schema: ComposeSchema, Base: &base}).Validate(); err == nil {
			t.Fatalf("%s: compose accepted", name)
		}
	}
	if err := (Compose{Kind: "waldo-model-compose", Schema: ComposeSchema, Base: &ComposeBase{Model: "a"}}).Validate(); err == nil {
		t.Fatal("stageless compose without base.merge accepted")
	}
}
