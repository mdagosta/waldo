// Copyright (c) 2026 OpenWALDO Project contributors
// SPDX-License-Identifier: Apache-2.0

package interactionassistant_test

import (
	"reflect"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
)

func TestPromotedInteractionPipelineIsPinned(t *testing.T) {
	compose, _, err := model.LoadCompose("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := compose.Base
	if base == nil || base.Model != "general-base-350m-20tpp-01" || base.ModelID != "e724bd198b94a53b6985d64155aaced87b87ccb57c489cf48f198a62abfab5d5" || base.RunID != "126406ff4349f479" || base.RunBOMSHA256 != "5157f4db160107c26f689be1551e7b1ebacb9cb107247f4ae80acc4a3fbc6d6d" || base.ArtifactSHA256 != "5a2d31dbc5b5efd0feb1b6865cc5b7fb89d315e12acffd1eb63d1fe202a3d541" || base.ArtifactBytes != 1381451752 {
		t.Fatalf("base changed: %+v", base)
	}
	if compose.Interaction.Template != model.InteractionUserAssistantV1 || !compose.Interaction.Tools || len(compose.Stages) != 2 {
		t.Fatalf("pipeline changed: %+v", compose)
	}

	contract := compose.Stages[0]
	if contract.Name != "interaction-assistant-sft" || contract.Type != "fine-tuning" || contract.Objective != "assistant-response-modeling" || contract.Conversation.Template != model.InteractionUserAssistantV1 || !reflect.DeepEqual(contract.Conversation.SupervisedRoles, []string{"assistant"}) || !reflect.DeepEqual(corpusPaths(contract.Corpora), []string{"post-train/sft/interaction-contract-v1"}) {
		t.Fatalf("contract stage changed: %+v", contract)
	}
	if contract.Parameters.Steps != 75 || contract.Parameters.BatchSize != 8 || contract.Parameters.GradientAccumulation != 4 || contract.Parameters.SequenceLength != 512 || contract.Parameters.LearningRate != 0.00002 || contract.Parameters.Seed != 47 || contract.Parameters.WarmupSteps == nil || *contract.Parameters.WarmupSteps != 2 {
		t.Fatalf("contract controls changed: %+v", contract.Parameters)
	}

	booster := compose.Stages[1]
	if booster.Name != "formatting-booster-sft" || booster.Type != "fine-tuning" || booster.Objective != "assistant-response-modeling" || booster.Conversation.Template != model.InteractionUserAssistantV1 || !reflect.DeepEqual(booster.Conversation.SupervisedRoles, []string{"assistant"}) || !reflect.DeepEqual(corpusPaths(booster.Corpora), []string{"post-train/sft/interaction-formatting-v1"}) {
		t.Fatalf("booster stage changed: %+v", booster)
	}
	if booster.Parameters.Steps != 5 || booster.Parameters.BatchSize != 8 || booster.Parameters.GradientAccumulation != 4 || booster.Parameters.SequenceLength != 512 || booster.Parameters.LearningRate != 0.000005 || booster.Parameters.Seed != 53 || booster.Parameters.WarmupSteps == nil || *booster.Parameters.WarmupSteps != 1 {
		t.Fatalf("booster controls changed: %+v", booster.Parameters)
	}
}

func corpusPaths(selections []model.CorpusSelection) []string {
	paths := make([]string, len(selections))
	for index, selection := range selections {
		paths[index] = selection.Path
	}
	return paths
}
