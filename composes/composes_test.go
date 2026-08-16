// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package composes_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestModelComposeGuideNamesEverySchemaField(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "docs", "MODEL-COMPOSE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		model.Compose{}, model.ComposeBase{}, model.Architecture{}, model.Tokenizer{}, model.Stage{}, model.CorpusSelection{}, corpus.RecordFilter{}, corpus.ValueFilter{}, corpus.DateFilter{}, training.Parameters{},
	} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := strings.Split(typeOf.Field(index).Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if !strings.Contains(string(guide), "`"+name+"`") && !strings.Contains(string(guide), name+":") {
				t.Errorf("%s.%s YAML field %q is absent from MODEL-COMPOSE.md", typeOf.Name(), typeOf.Field(index).Name, name)
			}
		}
	}
}

func TestEveryReferenceComposeSettingResolvesIntoTrainingContract(t *testing.T) {
	files, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			compose, _, err := model.LoadCompose(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, stage := range compose.Stages {
				raw := stage.Parameters
				resolved, err := stage.ResolveParameters()
				if err != nil {
					t.Fatalf("stage %s: %v", stage.Name, err)
				}
				profile := raw.Profile
				if profile == "" {
					profile = training.DefaultProfile
				}
				epochs := raw.Epochs
				if epochs == 0 {
					epochs = 1
				}
				if resolved.Profile != profile || resolved.Epochs != epochs || resolved.Steps != raw.Steps || resolved.BatchSize != raw.BatchSize || resolved.SequenceLength != raw.SequenceLength || resolved.LearningRate != raw.LearningRate || resolved.Seed != raw.Seed {
					t.Fatalf("stage %s direct settings were not preserved: raw=%+v resolved=%+v", stage.Name, raw, resolved)
				}
				if resolved.PlannedTokenCapacity != raw.Steps*raw.BatchSize*raw.SequenceLength {
					t.Fatalf("stage %s planned capacity = %d", stage.Name, resolved.PlannedTokenCapacity)
				}
				assertOptionalFloat(t, stage.Name+" weight_decay", raw.WeightDecay, resolved.Optimizer.WeightDecay)
				assertOptionalInt64(t, stage.Name+" warmup_steps", raw.WarmupSteps, resolved.Schedule.WarmupSteps)
				assertOptionalInt64(t, stage.Name+" checkpoint_every", raw.CheckpointEvery, resolved.CheckpointEvery)
				assertOptionalInt64(t, stage.Name+" evaluate_every", raw.EvaluateEvery, resolved.EvaluateEvery)
				if raw.ShuffleBufferRecords != nil && resolved.Data.ShuffleBufferRecords != *raw.ShuffleBufferRecords {
					t.Fatalf("stage %s shuffle_buffer_records = %d, want %d", stage.Name, resolved.Data.ShuffleBufferRecords, *raw.ShuffleBufferRecords)
				}
				assertOptionalInt64(t, stage.Name+" shuffle_buffer_bytes", raw.ShuffleBufferBytes, resolved.Data.ShuffleBufferBytes)
				expectedWeights := raw.CorpusWeights
				for _, selection := range stage.Corpora {
					if selection.Weight == nil {
						continue
					}
					if expectedWeights == nil {
						expectedWeights = map[string]uint64{}
					}
					expectedWeights[selection.Path] = *selection.Weight
				}
				if !reflect.DeepEqual(resolved.Data.CorpusWeights, expectedWeights) {
					t.Fatalf("stage %s corpus weights = %v, want %v", stage.Name, resolved.Data.CorpusWeights, expectedWeights)
				}
				if resolved.Evaluation == nil {
					t.Fatalf("stage %s has no resolved evaluation policy", stage.Name)
				}
				assertOptionalFloat(t, stage.Name+" evaluation_fraction", raw.EvaluationFraction, resolved.Evaluation.Fraction)
				if raw.EvaluationMaxRecords != nil && resolved.Evaluation.MaxRecords != *raw.EvaluationMaxRecords {
					t.Fatalf("stage %s evaluation_max_records = %d, want %d", stage.Name, resolved.Evaluation.MaxRecords, *raw.EvaluationMaxRecords)
				}
				assertOptionalInt64(t, stage.Name+" evaluation_max_bytes", raw.EvaluationMaxBytes, resolved.Evaluation.MaxBytes)
			}
		})
	}
}

func assertOptionalInt64(t *testing.T, name string, declared *int64, resolved int64) {
	t.Helper()
	if declared != nil && resolved != *declared {
		t.Fatalf("%s = %d, want %d", name, resolved, *declared)
	}
}

func assertOptionalFloat(t *testing.T, name string, declared *float64, resolved float64) {
	t.Helper()
	if declared != nil && resolved != *declared {
		t.Fatalf("%s = %g, want %g", name, resolved, *declared)
	}
}

func TestReferenceCanaryIsExecutableAndCompact(t *testing.T) {
	files, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 || files[0] != "0000-canary.yaml" || files[1] != "0001-babble.yaml" || files[2] != "0002-basic.yaml" || files[3] != "0003-intermediate.yaml" || files[4] != "0004-conversation.yaml" {
		t.Fatalf("reference composes = %v, want canary through conversation", files)
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

func TestConversationHasOrderedPretrainingAndInstructionTuning(t *testing.T) {
	compose, _, err := model.LoadCompose("0004-conversation.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 2 || compose.Stages[0].Type != "pre-training" || compose.Stages[1].Type != "fine-tuning" {
		t.Fatalf("conversation stages = %+v", compose.Stages)
	}
	if compose.Stages[0].Filter == nil || compose.Stages[0].Filter.MainContent == nil || !*compose.Stages[0].Filter.MainContent {
		t.Fatalf("conversation pretraining does not require main content: %+v", compose.Stages[0].Filter)
	}
	for _, stage := range compose.Stages {
		if stage.Filter == nil || stage.Filter.Exclude == nil || stage.Filter.Exclude.RepetitiveContent == nil || stage.Filter.Exclude.BoilerplateContent == nil {
			t.Fatalf("conversation stage %s does not declare content-quality exclusions: %+v", stage.Name, stage.Filter)
		}
	}
	forecast, err := model.ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	if forecast.ApproximateParameters != 139287552 || forecast.PlannedTokens != 6039961600 {
		t.Fatalf("conversation forecast = %d parameters/%d tokens", forecast.ApproximateParameters, forecast.PlannedTokens)
	}
	if compose.Stages[1].Parameters.LearningRate >= compose.Stages[0].Parameters.LearningRate || compose.Stages[1].Parameters.Epochs != 5 {
		t.Fatalf("conversation tuning controls = %+v", compose.Stages[1].Parameters)
	}
	wantCorpora := map[string]uint64{
		"post-train/sft/interaction-contract": 5,
		"post-train/sft/dolly":                2,
		"post-train/sft/oasst1":               3,
		"post-train/sft/oasst2":               4,
	}
	if len(compose.Stages[1].Corpora) != len(wantCorpora) {
		t.Fatalf("conversation tuning corpora = %+v", compose.Stages[1].Corpora)
	}
	for _, corpus := range compose.Stages[1].Corpora {
		if corpus.Weight == nil || wantCorpora[corpus.Path] != *corpus.Weight {
			t.Fatalf("conversation tuning corpus = %+v", corpus)
		}
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
	if compose.Architecture.Dropout != 0.1 || compose.Stages[0].Parameters.Profile != "causal-pretrain-weighted" || inlineWeightCount(compose.Stages[0]) != 3 || len(compose.Stages[0].Parameters.CorpusWeights) != 0 {
		t.Fatalf("basic tuning controls are not pinned: architecture=%+v parameters=%+v", compose.Architecture, compose.Stages[0].Parameters)
	}
}

func TestIntermediateHasTwoDayScalingBudget(t *testing.T) {
	compose, _, err := model.LoadCompose("0003-intermediate.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(compose.Stages) != 1 || len(compose.Stages[0].Corpora) != 4 || compose.Architecture.Dropout != 0.1 {
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

func inlineWeightCount(stage model.Stage) int {
	count := 0
	for _, selection := range stage.Corpora {
		if selection.Weight != nil {
			count++
		}
	}
	return count
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
