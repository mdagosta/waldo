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
	if len(files) != 2 || files[0] != "0000-canary.yaml" || files[1] != "0001-babble.yaml" {
		t.Fatalf("reference composes = %v, want the canary and babble candidate", files)
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

func TestExperimentalBabbleHasMeasuredScalingBudget(t *testing.T) {
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
