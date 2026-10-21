// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package tinyshakespeare_test

import (
	"reflect"
	"testing"

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
	if forecast.ApproximateParameters != 10721280 || forecast.PlannedTokens != 81920000 {
		t.Fatalf("forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}

	stage := compose.Stages[0]
	if got := corpusPaths(stage.Corpora); !reflect.DeepEqual(got, []string{"core/reference/tiny-shakespeare"}) {
		t.Fatalf("corpus = %v", got)
	}
	resolved, err := stage.ResolvePlanningParameters()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Evaluation == nil || resolved.Profile != stage.Parameters.Profile || resolved.BatchSize != stage.Parameters.BatchSize || resolved.SequenceLength != stage.Parameters.SequenceLength || resolved.LearningRate != stage.Parameters.LearningRate || resolved.Seed != stage.Parameters.Seed {
		t.Fatalf("training contract changed: raw=%+v resolved=%+v", stage.Parameters, resolved)
	}
}

func corpusPaths(selections []model.CorpusSelection) []string {
	paths := make([]string, len(selections))
	for index, selection := range selections {
		paths[index] = selection.Path
	}
	return paths
}
