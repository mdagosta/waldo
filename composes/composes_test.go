// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package composes_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
)

func TestReferenceCanaryIsExecutableAndCompact(t *testing.T) {
	files, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0] != "0000-canary.yaml" || files[1] != "0001-babble.yaml" || files[2] != "0002-basic.yaml" || files[3] != "0003-intermediate.yaml" {
		t.Fatalf("reference composes = %v, want canary, babble, basic, and intermediate", files)
	}
	compose, _, err := model.LoadCompose("0000-canary.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 1 || len(compose.Stages[0].Corpora) != 4 {
		t.Fatalf("canary stages/corpora = %d/%d", len(compose.Stages), len(compose.Stages[0].Corpora))
	}
	if compose.Architecture.Tokenizer.Name != "tiktoken/cl100k_base" || compose.Architecture.Tokenizer.Revision != "tiktoken-cl100k-base" || compose.Architecture.VocabularySize != 100259 {
		t.Fatalf("canary does not use the portable subword tokenizer: %+v", compose.Architecture.Tokenizer)
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 13620736 || forecast.PlannedTokens != 4096000 {
		t.Fatalf("canary forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}
}

func TestBasicHasTenHourScalingBudget(t *testing.T) {
	compose, _, err := model.LoadCompose("0002-basic.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 1 || len(compose.Stages[0].Corpora) != 3 {
		t.Fatalf("basic stages/corpora = %d/%d", len(compose.Stages), len(compose.Stages[0].Corpora))
	}
	if compose.Architecture.Tokenizer.Name != "tiktoken/r50k_base" || compose.Architecture.Tokenizer.Revision != "tiktoken-r50k-base" || compose.Architecture.VocabularySize != 50259 {
		t.Fatalf("basic does not use the compact portable subword tokenizer: %+v", compose.Architecture.Tokenizer)
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 114115584 || forecast.PlannedTokens != 3932160000 {
		t.Fatalf("basic forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}
	if compose.Architecture.Dropout != 0.1 || compose.Stages[0].Parameters.Profile != "causal-pretrain-v3" || len(compose.Stages[0].Parameters.CorpusWeights) != 3 {
		t.Fatalf("basic tuning controls are not pinned: architecture=%+v parameters=%+v", compose.Architecture, compose.Stages[0].Parameters)
	}
}

func TestIntermediateHasTwoDayScalingBudget(t *testing.T) {
	compose, _, err := model.LoadCompose("0003-intermediate.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 1 || len(compose.Stages[0].Corpora) != 5 || compose.Architecture.Dropout != 0.1 {
		t.Fatalf("intermediate architecture/stage is incomplete: %+v", compose)
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 336637440 || forecast.PlannedTokens != 11999969280 {
		t.Fatalf("intermediate forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}
	parameters := compose.Stages[0].Parameters
	if parameters.BatchSize != 16 || parameters.Steps != 366210 || parameters.SequenceLength != 2048 {
		t.Fatalf("intermediate memory-safe training shape = %+v", parameters)
	}
}

func TestValidatedBabbleHasMeasuredScalingBudget(t *testing.T) {
	compose, _, err := model.LoadCompose("0001-babble.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 1 || len(compose.Stages[0].Corpora) != 2 {
		t.Fatalf("babble stages/corpora = %d/%d", len(compose.Stages), len(compose.Stages[0].Corpora))
	}
	if compose.Architecture.Tokenizer.Name != "tiktoken/r50k_base" || compose.Architecture.Tokenizer.Revision != "tiktoken-r50k-base" || compose.Architecture.VocabularySize != 50259 {
		t.Fatalf("babble does not use the portable subword tokenizer: %+v", compose.Architecture.Tokenizer)
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 49858560 || forecast.PlannedTokens != 1048576000 {
		t.Fatalf("babble forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}
}

func TestReferenceComposesDoNotEncodeHardwareInNames(t *testing.T) {
	for _, legacy := range []string{"babble-mac.yaml", "h200-02h.yaml", "h200-06h.yaml", "h200-12h.yaml", "h200-24h.yaml", "h200-48h.yaml"} {
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("hardware-specific compose %s still exists", legacy)
		}
	}
}
