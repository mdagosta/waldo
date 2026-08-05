package model

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/openwaldo/waldo-new/internal/corpus"
	"github.com/openwaldo/waldo-new/internal/provenance"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/openwaldo/waldo-new/internal/training"
)

type resolvedStage struct {
	Plan       PlannedStage
	BOM        corpus.BOM
	Files      []corpus.ExportedFile
	Inputs     []training.Input
	ExportPath string
}

func preflight(recipe Recipe, progress func(Progress)) (Plan, []resolvedStage, error) {
	architectureHash, err := canonicalHash(recipe.Architecture)
	if err != nil {
		return Plan{}, nil, err
	}
	forecast, err := recipe.Architecture.Forecast()
	if err != nil {
		return Plan{}, nil, err
	}
	plan := Plan{
		Kind: "waldo-model-build-plan", Schema: PlanSchema, Name: recipe.Name,
		ArchitectureSHA256: architectureHash, Architecture: recipe.Architecture,
		Forecast: forecast, Backend: recipe.Backend,
	}
	resolved := make([]resolvedStage, 0, len(recipe.Stages))
	for _, stage := range recipe.Stages {
		if progress != nil {
			progress(Progress{Phase: "preflight", Stage: stage.Name, Message: "verifying exported corpus and OpenWALDO BOM"})
		}
		document, report, err := provenance.VerifyCorpusExport(stage.Corpus)
		if err != nil {
			return Plan{}, nil, fmt.Errorf("stage %s corpus: %w", stage.Name, err)
		}
		if document.Format != "native" {
			return Plan{}, nil, fmt.Errorf("stage %s corpus is %s; training requires a native canonical Parquet export", stage.Name, document.Format)
		}
		if report.Files == 0 || document.BOM.Totals.Tokens <= 0 {
			return Plan{}, nil, fmt.Errorf("stage %s corpus contains no training records", stage.Name)
		}
		for _, selected := range document.BOM.Shards {
			if selected.Format != "parquet" || selected.RecordSchema != shard.TextRecordSchema {
				return Plan{}, nil, fmt.Errorf("stage %s shard %s is %s record schema %d; causal-language-modeling requires Parquet record schema %d", stage.Name, selected.SHA256[:12], selected.Format, selected.RecordSchema, shard.TextRecordSchema)
			}
		}
		bomHash, err := hashJSON(document.BOM)
		if err != nil {
			return Plan{}, nil, err
		}
		capacity, overflow := multiplyInt64(stage.Parameters.Steps, stage.Parameters.BatchSize, stage.Parameters.SequenceLength)
		if overflow {
			return Plan{}, nil, fmt.Errorf("stage %s planned token capacity overflows int64", stage.Name)
		}
		planned := PlannedStage{
			Name: stage.Name, Objective: stage.Objective, CorpusBOMSHA256: bomHash,
			Files: int(report.Files), Docs: document.BOM.Totals.Docs, Tokens: document.BOM.Totals.Tokens,
			Bytes: report.Bytes, Parameters: stage.Parameters, PlannedTokens: capacity,
		}
		root := filepath.Dir(report.Path)
		inputs := make([]training.Input, 0, len(document.Files))
		for _, file := range document.Files {
			inputs = append(inputs, training.Input{Path: filepath.Join(root, filepath.FromSlash(file.Path)), SHA256: file.SHA256, Bytes: file.Bytes})
		}
		plan.Stages = append(plan.Stages, planned)
		resolved = append(resolved, resolvedStage{Plan: planned, BOM: document.BOM, Files: document.Files, Inputs: inputs, ExportPath: report.Path})
	}
	return plan, resolved, nil
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
