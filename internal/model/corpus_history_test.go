// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"reflect"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
)

func TestSkipCompletedCorporaFiltersCompletedPathsAndWeights(t *testing.T) {
	compose := validCompose()
	compose.Stages[0].Corpora = NewCorpusSelections([]string{"core/books", "science/new", "post-train/dialogue"})
	compose.Stages[0].Parameters.CorpusWeights = map[string]uint64{"core/books": 3, "science/new": 2, "post-train/dialogue": 1}
	compose.Stages = append(compose.Stages, Stage{
		Name: "fine-tune", Type: "fine-tuning", Objective: "causal-language-modeling",
		Corpora: NewCorpusSelections([]string{"post-train/dialogue"}), Parameters: testStage("unused").Parameters,
	})
	inspection := Inspection{
		Model: ModelRecord{Runs: []RunPin{
			{State: RunComplete},
			{State: RunFailed},
		}},
		RunBOMs: []RunBOM{
			{CorpusBOM: corpus.BOM{Paths: []string{"core/books.yaml", "post-train/dialogue"}}},
			{CorpusBOM: corpus.BOM{Paths: []string{"science/new"}}},
		},
	}

	filtered, skipped := SkipCompletedCorpora(compose, inspection)
	if !reflect.DeepEqual(skipped, []SkippedCorpus{{Stage: "pretrain", Path: "core/books"}, {Stage: "pretrain", Path: "post-train/dialogue"}, {Stage: "fine-tune", Path: "post-train/dialogue"}}) {
		t.Fatalf("skipped = %+v", skipped)
	}
	if len(filtered.Stages) != 1 || !reflect.DeepEqual(CorpusPaths(filtered.Stages[0].Corpora), []string{"science/new"}) || !reflect.DeepEqual(filtered.Stages[0].Parameters.CorpusWeights, map[string]uint64{"science/new": 2}) {
		t.Fatalf("filtered compose = %+v", filtered)
	}
	if len(compose.Stages) != 2 || len(compose.Stages[0].Corpora) != 3 || len(compose.Stages[0].Parameters.CorpusWeights) != 3 {
		t.Fatal("input compose was mutated")
	}
}

func TestSkipCompletedCorporaCanProduceNoWork(t *testing.T) {
	compose := validCompose()
	inspection := Inspection{
		Model:   ModelRecord{Runs: []RunPin{{State: RunComplete}}},
		RunBOMs: []RunBOM{{CorpusBOM: corpus.BOM{Paths: []string{"example.json"}}}},
	}
	filtered, skipped := SkipCompletedCorpora(compose, inspection)
	if len(filtered.Stages) != 0 || len(skipped) != 1 || skipped[0].Path != "example" {
		t.Fatalf("filtered = %+v, skipped = %+v", filtered, skipped)
	}
}
