// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package groundedresponder_test

import (
	"reflect"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
)

func TestCandidatePreservesFoundationAndBoundsSFT(t *testing.T) {
	compose, _, err := model.LoadCompose("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if compose.Base != nil || compose.Interaction.Template != "user-assistant-v1" || len(compose.Stages) != 2 {
		t.Fatalf("unexpected pipeline: %+v", compose)
	}

	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 32777728 || forecast.PlannedTokens != 616120320 {
		t.Fatalf("forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}

	architecture := compose.Architecture
	if architecture.ContextTokens != 512 || architecture.VocabularySize != 4096 || architecture.HiddenSize != 512 || architecture.IntermediateSize != 1536 || architecture.Layers != 9 || architecture.AttentionHeads != 8 || architecture.KeyValueHeads != 8 || architecture.Dropout != 0.2 || architecture.QKNormalization || architecture.Initialization != "depth-scaled" || !architecture.TieEmbeddings || architecture.ParameterDType != "float32" {
		t.Fatalf("architecture changed: %+v", architecture)
	}

	pretrain := compose.Stages[0]
	if pretrain.Name != "tinystories-pretrain" || pretrain.Type != "pre-training" || pretrain.Objective != "causal-language-modeling" || !reflect.DeepEqual(corpusPaths(pretrain.Corpora), []string{"core/synthetic/tinystories-reference"}) {
		t.Fatalf("foundation stage changed: %+v", pretrain)
	}
	if pretrain.Parameters.Tokens != 613611520 || pretrain.Parameters.BatchSize != 12 || pretrain.Parameters.GradientAccumulation != 3 || pretrain.Parameters.SequenceLength != 512 || pretrain.Parameters.LearningRate != 0.0006 || pretrain.Parameters.Seed != 42 {
		t.Fatalf("foundation controls changed: %+v", pretrain.Parameters)
	}

	sft := compose.Stages[1]
	if sft.Name != "narrow-grounded-sft" || sft.Type != "fine-tuning" || sft.Objective != "assistant-response-modeling" || sft.Conversation.Template != "user-assistant-v1" || !reflect.DeepEqual(sft.Conversation.SupervisedRoles, []string{"assistant"}) || !reflect.DeepEqual(corpusPaths(sft.Corpora), []string{"post-train/sft/tiny-grounded-responses-v1"}) {
		t.Fatalf("SFT stage changed: %+v", sft)
	}
	if sft.Parameters.Tokens != 2500000 || sft.Parameters.BatchSize != 16 || sft.Parameters.GradientAccumulation != 2 || sft.Parameters.SequenceLength != 512 || sft.Parameters.LearningRate != 0.00002 || sft.Parameters.WeightDecay == nil || *sft.Parameters.WeightDecay != 0.01 || sft.Parameters.Seed != 43 {
		t.Fatalf("SFT controls changed: %+v", sft.Parameters)
	}
}

func corpusPaths(selections []model.CorpusSelection) []string {
	paths := make([]string, len(selections))
	for index, selection := range selections {
		paths[index] = selection.Path
	}
	return paths
}
