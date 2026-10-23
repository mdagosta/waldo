// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openwaldo/waldo/internal/corpus"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/training"
)

// releasePackage writes an exported package whose release BOM records a
// lineage node descending from parent; salt changes one weight byte so
// packages can hold different weights.
func releasePackage(t *testing.T, parent string, salt byte) string {
	t.Helper()
	architecture := Architecture{Family: "decoder-transformer", ContextTokens: 32, VocabularySize: 259, HiddenSize: 4, IntermediateSize: 8, Layers: 1, AttentionHeads: 2, KeyValueHeads: 1, TieEmbeddings: true, ParameterDType: "bfloat16", Tokenizer: Tokenizer{Name: "byte", Revision: "builtin-byte-schema-1"}}
	directory := t.TempDir()
	files := huggingFaceFixture(t, architecture, HuggingFaceTokenizerClass)
	weights := files["model.safetensors"]
	weights[len(weights)-1] ^= salt
	// Published run BOMs are pinned, like a model's runs, by the hash of their canonical JSON.
	runBOM := RunBOM{Kind: "openwaldo-bom", Subject: "training-run", ID: fmt.Sprintf("run-%d", salt),
		CorpusBOM: corpus.BOM{Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{fmt.Sprintf("code/run-%d", salt)}}}
	corpusDigest, err := hashJSON(runBOM.CorpusBOM)
	if err != nil {
		t.Fatal(err)
	}
	runBOM.CorpusBOMSHA256 = corpusDigest
	runDigest, err := hashJSON(runBOM)
	if err != nil {
		t.Fatal(err)
	}
	runJSON, err := json.MarshalIndent(runBOM, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["provenance-"+runDigest+".json"] = runJSON
	node := LineageNode{WeightsSHA256: strings.Repeat(string("0123456789"[salt]), 64), Parents: []LineageLink{{WeightsSHA256: parent}}, Runs: []string{runDigest}}
	bom, err := json.Marshal(map[string]any{"kind": "openwaldo-bom", "schema": 1, "subject": "model-release", "lineage": node})
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
	for name, directory := range map[string]string{"left": releasePackage(t, common, 0), "right": releasePackage(t, common, 1), "stranger": releasePackage(t, other, 2)} {
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
	if origin == nil || origin.Source.Provider != "merge" || origin.Merge == nil || origin.Merge.Method != "weighted-mean-f32-v1" || len(origin.Merge.Parents) != 2 || origin.Merge.Parents[1].Weight != 3 {
		t.Fatalf("origin = %+v", origin)
	}
	// Like a git merge commit, the merged node names each parent's node and nothing older.
	if origin.Lineage == nil || len(origin.Lineage.Parents) != 2 || origin.Lineage.Parents[0].LineageSHA256 != origin.Merge.Parents[0].LineageSHA256 || origin.Merge.Parents[0].LineageSHA256 == "" {
		t.Fatalf("lineage = %+v", origin.Lineage)
	}
	for _, artifact := range origin.Artifacts {
		if artifact.Role == "weights" && artifact.SHA256 != origin.Lineage.WeightsSHA256 {
			t.Fatalf("merged node weights = %s, artifact %s", origin.Lineage.WeightsSHA256, artifact.SHA256)
		}
	}
	// The common ancestor is two links up the chain, through each release's own node.
	if reached := ancestry(*origin.Lineage, builder.lineageIndex()); !reached[common] || reached[other] {
		t.Fatalf("ancestry = %v", reached)
	}
	// The merge carries both parents' published run BOMs, so its release proves the round's data.
	carried, err := ProvenanceRecords(merged)
	if err != nil || len(carried) != 2 {
		t.Fatalf("merged provenance = %v, %v", carried, err)
	}
	for _, name := range []string{"left", "right"} {
		parent, err := Inspect(root, name)
		if err != nil {
			t.Fatal(err)
		}
		records, err := ProvenanceRecords(parent)
		if err != nil || len(records) != 1 {
			t.Fatalf("%s provenance = %v, %v", name, records, err)
		}
		for digest := range records {
			if _, ok := carried[digest]; !ok || !slices.Contains(parent.Origin.Lineage.Runs, digest) {
				t.Fatalf("%s run %s was not carried", name, digest)
			}
		}
	}
	if merged.BOM.CurrentOriginSHA256 == "" || len(merged.Runs) != 0 {
		t.Fatalf("merged model is not usable from its origin: %+v", merged.BOM)
	}
	// A release of the merge records the merged node, so the chain continues past publication.
	lineage, err := ExportLineage(merged)
	if err != nil || !reflect.DeepEqual(*lineage, *origin.Lineage) {
		t.Fatalf("export lineage = %+v, %v", lineage, err)
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

func TestSameParentAcceptsPinsRecordedBeforeLineage(t *testing.T) {
	pin := ModelParent{Model: "base", ModelID: "id", RunID: "run", RunBOMSHA256: "bom", Artifact: training.Artifact{Path: "base/model.safetensors", SHA256: "weights"}}
	withLineage := pin
	withLineage.LineageSHA256 = strings.Repeat("a", 64)
	if !sameParent(&pin, &withLineage) || !sameParent(&withLineage, &withLineage) {
		t.Fatal("matching parents rejected")
	}
	other := withLineage
	other.LineageSHA256 = strings.Repeat("b", 64)
	if sameParent(&withLineage, &other) {
		t.Fatal("a recorded lineage must match exactly")
	}
	changed := pin
	changed.RunID = "another"
	if sameParent(&changed, &withLineage) || sameParent(nil, &withLineage) {
		t.Fatal("a different parent matched")
	}
}

func TestPullRefusesProvenanceThatDoesNotMatchItsName(t *testing.T) {
	directory := releasePackage(t, strings.Repeat("a", 64), 0)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "provenance-") {
			if err := os.WriteFile(filepath.Join(directory, entry.Name()), []byte(`{"tampered":true}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := (Puller{Root: t.TempDir()}).PullDirectory("tampered", directory); err == nil || !strings.Contains(err.Error(), "does not hash") {
		t.Fatalf("tampered provenance error = %v", err)
	}
}
