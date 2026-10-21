// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package smallsimpleenglish_test

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
	if forecast.ApproximateParameters != 64732800 || forecast.PlannedTokens != 1189806080 {
		t.Fatalf("forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}

	architecture := compose.Architecture
	if architecture.ContextTokens != 1024 || architecture.VocabularySize != 8192 || architecture.HiddenSize != 640 || architecture.IntermediateSize != 1728 || architecture.Layers != 12 || architecture.AttentionHeads != 10 || architecture.KeyValueHeads != 10 || architecture.Dropout != 0.1 || architecture.QKNormalization || architecture.Initialization != "depth-scaled" || !architecture.TieEmbeddings || architecture.ParameterDType != "float32" {
		t.Fatalf("architecture changed: %+v", architecture)
	}
	tokenizer := architecture.Tokenizer.Training
	if tokenizer == nil || tokenizer.Algorithm != model.TokenizerAlgorithmByteBPEV1 || tokenizer.SampleBytes != 134217728 || tokenizer.Seed != 42 || tokenizer.DistributionPolicy != corpus.DistributionPolicyDistributable || !reflect.DeepEqual(corpusPaths(tokenizer.Corpora), []string{"core/common-pile/pressbooks"}) {
		t.Fatalf("tokenizer changed: %+v", tokenizer)
	}

	stage := compose.Stages[0]
	if stage.Name != "simple-english-pretrain" || stage.Type != "pre-training" || stage.Objective != "causal-language-modeling" || !reflect.DeepEqual(corpusPaths(stage.Corpora), []string{"core/synthetic/tinystories-gpt4-v2", "core/synthetic/cosmopedia-v2"}) {
		t.Fatalf("stage changed: %+v", stage)
	}
	for index, selection := range stage.Corpora {
		if selection.Weight == nil || *selection.Weight != 1 {
			t.Fatalf("corpus %d weight = %v", index, selection.Weight)
		}
	}
	filter := stage.Corpora[1].Filter
	if filter == nil || !reflect.DeepEqual(filter.Metadata["audience"].Include, []string{"young_children"}) || !reflect.DeepEqual(filter.Metadata["format"].Include, []string{"textbook_narrative"}) {
		t.Fatalf("Cosmopedia filter changed: %+v", filter)
	}
	parameters := stage.Parameters
	if parameters.Profile != "causal-pretrain-weighted" || parameters.Tokens != 1189798400 || parameters.BatchSize != 16 || parameters.GradientAccumulation != 4 || parameters.SequenceLength != 1024 || parameters.LearningRate != 0.0004 || parameters.Optimizer != "adamw" || parameters.Schedule != "cosine" || parameters.Seed != 42 || parameters.EvaluationSelection != "indexed-sample-v1" {
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
