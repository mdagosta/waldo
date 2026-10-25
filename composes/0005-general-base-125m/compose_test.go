// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package generalbase_test

import (
	"reflect"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/model"
)

func TestPromotedReference(t *testing.T) {
	compose, _, err := model.LoadCompose("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if compose.Base != nil || compose.Interaction.Template != "" || len(compose.Stages) != 1 {
		t.Fatalf("reference is not fresh one-stage pretraining: %+v", compose)
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 124635456 || forecast.PlannedTokens != 2124005376 {
		t.Fatalf("forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}

	architecture := compose.Architecture
	if architecture.ContextTokens != 2048 || architecture.VocabularySize != 32000 || architecture.HiddenSize != 576 || architecture.IntermediateSize != 1536 || architecture.Layers != 30 || architecture.AttentionHeads != 9 || architecture.KeyValueHeads != 3 || architecture.Dropout != 0 || architecture.QKNormalization || architecture.Initialization != "depth-scaled" || !architecture.TieEmbeddings || architecture.ParameterDType != "float32" {
		t.Fatalf("architecture changed: %+v", architecture)
	}
	tokenizer := architecture.Tokenizer.Training
	if tokenizer == nil || tokenizer.Algorithm != model.TokenizerAlgorithmByteBPEV1 || tokenizer.SampleBytes != 268435456 || tokenizer.Seed != 42 || tokenizer.DistributionPolicy != corpus.DistributionPolicyDistributable || !reflect.DeepEqual(corpusPaths(tokenizer.Corpora), []string{"core/common-pile/pressbooks"}) {
		t.Fatalf("tokenizer changed: %+v", tokenizer)
	}

	stage := compose.Stages[0]
	if stage.Name != "general-base-qualification" || stage.Type != "pre-training" || stage.Objective != "causal-language-modeling" || !reflect.DeepEqual(corpusPaths(stage.Corpora), []string{"core/common-pile/wikimedia", "core/synthetic/cosmopedia-v2", "core/common-pile/stackexchange", "core/synthetic/tinystories-gpt4-v2"}) {
		t.Fatalf("stage changed: %+v", stage)
	}
	wantWeights := []uint64{9, 6, 3, 2}
	for index, selection := range stage.Corpora {
		if selection.Weight == nil || *selection.Weight != wantWeights[index] {
			t.Fatalf("corpus %d weight = %v", index, selection.Weight)
		}
	}
	filter := stage.Corpora[1].Filter
	if filter == nil || !reflect.DeepEqual(filter.Metadata["audience"].Include, []string{"young_children"}) || !reflect.DeepEqual(filter.Metadata["format"].Include, []string{"textbook_narrative"}) {
		t.Fatalf("Cosmopedia filter changed: %+v", filter)
	}
	parameters := stage.Parameters
	if parameters.Profile != "causal-pretrain-weighted" || parameters.Tokens != 2124000000 || parameters.BatchSize != 8 || parameters.GradientAccumulation != 4 || parameters.SequenceLength != 2048 || parameters.LearningRate != 0.0006 || parameters.Optimizer != "adamw" || parameters.Schedule != "cosine" || parameters.Seed != 42 || parameters.EvaluationSelection != "indexed-sample-v1" {
		t.Fatalf("training controls changed: %+v", parameters)
	}
}

func corpusPaths(selections []model.CorpusSelection) []string {
	paths := make([]string, len(selections))
	for index, selection := range selections {
		paths[index] = selection.Path
	}
	return paths
}
