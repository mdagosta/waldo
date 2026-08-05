package model

import (
	"fmt"
	"math"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/openwaldo/waldo-new/internal/training"
)

// PreparedStage is the model-domain boundary for an already resolved and
// verified corpus selection. The CLI/corpus layers own index and lookaside
// access; the model lifecycle receives only an immutable BOM and local,
// content-addressed inputs.
type PreparedStage struct {
	Stage  Stage
	BOM    corpus.BOM
	Inputs []training.Input
}

func PrepareStage(stage Stage, bom corpus.BOM, inputs []training.Input) (PreparedStage, error) {
	if err := validateStage(stage, Architecture{}); err != nil {
		return PreparedStage{}, err
	}
	if err := bom.Validate(); err != nil {
		return PreparedStage{}, fmt.Errorf("corpus OpenWALDO BOM: %w", err)
	}
	if len(bom.Shards) == 0 || bom.Totals.Docs <= 0 || bom.Totals.Tokens <= 0 {
		return PreparedStage{}, fmt.Errorf("stage %s corpus selection contains no training records", stage.Name)
	}
	for _, selected := range bom.Shards {
		if selected.Format != "parquet" || selected.RecordSchema != shard.TextRecordSchema {
			return PreparedStage{}, fmt.Errorf("stage %s shard %s is %s record schema %d; causal-language-modeling requires Parquet record schema %d", stage.Name, selected.SHA256[:12], selected.Format, selected.RecordSchema, shard.TextRecordSchema)
		}
	}
	if len(inputs) == 0 {
		return PreparedStage{}, fmt.Errorf("stage %s has no materialized shard inputs", stage.Name)
	}
	expected := make(map[string]int64, len(bom.Shards))
	for _, selected := range bom.Shards {
		if size, exists := expected[selected.SHA256]; exists && size != selected.Bytes {
			return PreparedStage{}, fmt.Errorf("stage %s object %s has conflicting declared sizes", stage.Name, selected.SHA256[:12])
		}
		expected[selected.SHA256] = selected.Bytes
	}
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		size, exists := expected[input.SHA256]
		if input.Path == "" || !exists || input.Bytes != size || seen[input.SHA256] {
			return PreparedStage{}, fmt.Errorf("stage %s has an invalid or duplicate materialized input %s", stage.Name, input.SHA256)
		}
		seen[input.SHA256] = true
	}
	if len(seen) != len(expected) {
		return PreparedStage{}, fmt.Errorf("stage %s materialized %d of %d unique shard objects", stage.Name, len(seen), len(expected))
	}
	return PreparedStage{Stage: stage, BOM: bom, Inputs: append([]training.Input(nil), inputs...)}, nil
}

func composePlan(name string, compose Compose) (Plan, error) {
	architectureHash, err := canonicalHash(compose.Architecture)
	if err != nil {
		return Plan{}, err
	}
	forecast, err := compose.Architecture.Forecast()
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Kind: "waldo-model-plan", Schema: PlanSchema, Name: name,
		ArchitectureSHA256: architectureHash, Architecture: compose.Architecture,
		Forecast: forecast,
	}, nil
}

func forecastPlanForCompose(compose Compose) (Plan, error) {
	plan, err := composePlan("forecast", compose)
	if err != nil {
		return Plan{}, err
	}
	for _, stage := range compose.Stages {
		capacity, overflow := multiplyInt64(stage.Parameters.Steps, stage.Parameters.BatchSize, stage.Parameters.SequenceLength)
		if overflow {
			return Plan{}, fmt.Errorf("stage %s planned token capacity overflows int64", stage.Name)
		}
		plan.Stages = append(plan.Stages, PlannedStage{Name: stage.Name, Type: stage.Type, Objective: stage.Objective, Parameters: stage.Parameters, PlannedTokens: capacity})
	}
	return plan, nil
}

func validateStage(stage Stage, architecture Architecture) error {
	if !validName.MatchString(stage.Name) {
		return fmt.Errorf("invalid training stage name %q", stage.Name)
	}
	if stage.Type != "pre-training" && stage.Type != "fine-tuning" && stage.Type != "alignment" && stage.Type != "other" {
		return fmt.Errorf("stage %s has unsupported type %q", stage.Name, stage.Type)
	}
	if stage.Objective != "causal-language-modeling" {
		return fmt.Errorf("stage %s has unsupported objective %q", stage.Name, stage.Objective)
	}
	parameters := stage.Parameters
	if _, err := training.ResolveParameters(parameters); err != nil {
		return fmt.Errorf("stage %s training parameters: %w", stage.Name, err)
	}
	if architecture.ContextTokens > 0 && uint64(parameters.SequenceLength) > architecture.ContextTokens {
		return fmt.Errorf("stage %s sequence_length exceeds architecture context_tokens", stage.Name)
	}
	return nil
}

func multiplyInt64(values ...int64) (int64, bool) {
	result := int64(1)
	for _, value := range values {
		if value <= 0 || result > math.MaxInt64/value {
			return 0, true
		}
		result *= value
	}
	return result, false
}
