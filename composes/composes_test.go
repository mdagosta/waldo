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

func TestReferenceComposesFormCapabilityLadder(t *testing.T) {
	tests := []struct {
		file       string
		stages     int
		minCorpora int
	}{
		{file: "0000-canary.yaml", stages: 1, minCorpora: 4},
		{file: "0001-babble.yaml", stages: 1, minCorpora: 4},
		{file: "0002-reader.yaml", stages: 1, minCorpora: 4},
		{file: "0003-writer.yaml", stages: 1, minCorpora: 2},
		{file: "0004-knowledge.yaml", stages: 1, minCorpora: 4},
		{file: "0005-generalist.yaml", stages: 1, minCorpora: 5},
		{file: "0006-assistant.yaml", stages: 2, minCorpora: 5},
	}
	var previousParameters uint64
	var previousTokens int64
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			compose, _, err := model.LoadCompose(filepath.Join(".", test.file))
			if err != nil {
				t.Fatal(err)
			}
			if len(compose.Stages) != test.stages || len(compose.Stages[0].Corpora) < test.minCorpora {
				t.Fatalf("compose stages/corpora = %d/%d", len(compose.Stages), len(compose.Stages[0].Corpora))
			}
			if compose.Architecture.Tokenizer.Name != "tiktoken/cl100k_base" || compose.Architecture.Tokenizer.Revision != "tiktoken-cl100k-base" || compose.Architecture.VocabularySize != 100259 {
				t.Fatalf("compose does not use the portable subword tokenizer: %+v", compose.Architecture.Tokenizer)
			}
			forecast, err := model.ForecastCompose(compose)
			if err != nil {
				t.Fatal(err)
			}
			if forecast.ApproximateParameters < previousParameters || forecast.PlannedTokens <= previousTokens {
				t.Fatalf("ladder regressed from %d parameters/%d tokens to %d/%d", previousParameters, previousTokens, forecast.ApproximateParameters, forecast.PlannedTokens)
			}
			previousParameters, previousTokens = forecast.ApproximateParameters, forecast.PlannedTokens
		})
	}
	assistant, _, err := model.LoadCompose("0006-assistant.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if assistant.Stages[1].Type != "fine-tuning" || assistant.Stages[1].Corpora[0] != "post-train/sft" {
		t.Fatalf("assistant dialogue stage = %+v", assistant.Stages[1])
	}
}

func TestReferenceComposesDoNotEncodeHardwareInNames(t *testing.T) {
	for _, legacy := range []string{"babble-mac.yaml", "h200-02h.yaml", "h200-06h.yaml", "h200-12h.yaml", "h200-24h.yaml", "h200-48h.yaml"} {
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("hardware-specific compose %s still exists", legacy)
		}
	}
}
