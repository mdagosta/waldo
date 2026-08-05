package model

import (
	"strings"
	"testing"
)

func TestForecastPlanFiltersNonFitsAndSortsSlowestFirst(t *testing.T) {
	recipe := validRecipe("unused")
	architecture, err := recipe.Architecture.Forecast()
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		Architecture: recipe.Architecture, Forecast: architecture,
		Stages: []PlannedStage{{Name: "pretrain", Parameters: recipe.Stages[0].Parameters, PlannedTokens: 10_000_000}},
	}
	report, err := forecastPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if report.PlannedTokens != 10_000_000 || report.TrainingFLOPs <= 0 || len(report.Configurations) == 0 {
		t.Fatalf("report = %+v", report)
	}
	for position, configuration := range report.Configurations {
		if configuration.RequiredPerGPUBytes > configuration.MemoryPerGPUBytes-configuration.MemoryPerGPUBytes/10 {
			t.Fatalf("non-fitting configuration returned: %+v", configuration)
		}
		if position > 0 && report.Configurations[position-1].ApproximateSeconds < configuration.ApproximateSeconds {
			t.Fatalf("configurations are not slowest first: %+v", report.Configurations)
		}
	}
}

func TestForecastPlanRejectsModelTooLargeForCatalog(t *testing.T) {
	architecture := Architecture{
		Family: "decoder-transformer", ContextTokens: 2048, VocabularySize: 32000,
		HiddenSize: 65_536, IntermediateSize: 262_144, Layers: 256,
		AttentionHeads: 256, KeyValueHeads: 32, ParameterDType: "bfloat16",
		Tokenizer: Tokenizer{Name: "test", Revision: "sha256:test"},
	}
	forecast, err := architecture.Forecast()
	if err != nil {
		t.Fatal(err)
	}
	_, err = forecastPlan(Plan{
		Architecture: architecture, Forecast: forecast,
		Stages: []PlannedStage{{Name: "pretrain", Parameters: validRecipe("unused").Stages[0].Parameters, PlannedTokens: 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "does not fit any hardware configuration") {
		t.Fatalf("error = %v", err)
	}
}
