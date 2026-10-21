// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package tinystoriesbpe_test

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
	if forecast.ApproximateParameters != 32777728 || forecast.PlannedTokens != 613613568 {
		t.Fatalf("forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}

	if got := corpusPaths(compose.Stages[0].Corpora); !reflect.DeepEqual(got, []string{"core/synthetic/tinystories-reference"}) {
		t.Fatalf("corpus = %v", got)
	}
	architecture := compose.Architecture
	if architecture.ContextTokens != 512 || architecture.VocabularySize != 4096 || architecture.HiddenSize != 512 || architecture.IntermediateSize != 1536 || architecture.Layers != 9 || architecture.AttentionHeads != 8 || architecture.KeyValueHeads != 8 || architecture.Dropout != 0.2 || architecture.QKNormalization || architecture.Initialization != "depth-scaled" || !architecture.TieEmbeddings || architecture.ParameterDType != "float32" {
		t.Fatalf("architecture changed: %+v", architecture)
	}
	tokenizer := architecture.Tokenizer.Training
	if tokenizer == nil || tokenizer.Algorithm != model.TokenizerAlgorithmByteBPEV1 || tokenizer.SampleBytes != 134217728 || tokenizer.Seed != 42 || tokenizer.DistributionPolicy != corpus.DistributionPolicyDistributable || !reflect.DeepEqual(corpusPaths(tokenizer.Corpora), []string{"core/common-pile/pressbooks"}) {
		t.Fatalf("tokenizer changed: %+v", tokenizer)
	}
	parameters := compose.Stages[0].Parameters
	if parameters.Profile != "causal-pretrain-shuffled" || parameters.Tokens != 613611520 || parameters.BatchSize != 12 || parameters.GradientAccumulation != 3 || parameters.SequenceLength != 512 || parameters.LearningRate != 0.0006 || parameters.Optimizer != "adamw" || parameters.Schedule != "cosine" || parameters.Seed != 42 || parameters.EvaluationSelection != "indexed-sample-v1" {
		t.Fatalf("training controls changed: %+v", parameters)
	}
	resolved, err := compose.Stages[0].ResolvePlanningParameters()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Evaluation == nil {
		t.Fatal("reference has no held-out evaluation policy")
	}
}

func corpusPaths(selections []model.CorpusSelection) []string {
	paths := make([]string, len(selections))
	for index, selection := range selections {
		paths[index] = selection.Path
	}
	return paths
}
