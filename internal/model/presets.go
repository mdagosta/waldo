// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openwaldo/waldo/internal/training"
)

type Preset struct {
	Name         string
	Architecture Architecture
}

func PresetByName(name string) (Preset, error) {
	for _, candidate := range modelPresets {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return Preset{}, fmt.Errorf("unknown model preset %q; use %s", name, strings.Join(PresetNames(), ", "))
}

func PresetNames() []string {
	names := make([]string, 0, len(modelPresets))
	for _, candidate := range modelPresets {
		names = append(names, candidate.Name)
	}
	return names
}

var modelPresets = []Preset{
	preset("10m", 512, 259, 384, 1024, 6, 6, 2, "byte", "builtin-byte-schema-1"),
	preset("35m", 1024, 259, 512, 1408, 12, 8, 2, "byte", "builtin-byte-schema-1"),
	preset("90m", 2048, 259, 768, 2048, 12, 12, 4, "byte", "builtin-byte-schema-1"),
	preset("300m", 2048, 100259, 1024, 2816, 24, 16, 4, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	preset("1b", 4096, 100259, 2048, 8192, 16, 32, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	preset("3b", 4096, 100259, 3072, 8192, 28, 24, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	preset("7b", 4096, 100259, 4096, 14336, 32, 32, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	// Larger rungs remain useful for planning even when the current local
	// backend cannot execute them.
	preset("13b", 4096, 100259, 5120, 16384, 40, 40, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	preset("34b", 4096, 100259, 8192, 22016, 48, 64, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
	preset("70b", 4096, 100259, 8192, 28672, 80, 64, 8, "tiktoken/cl100k_base", "tiktoken-cl100k-base"),
}

func preset(name string, context, vocabulary, hidden, intermediate, layers, heads, keyValueHeads uint64, tokenizerName, tokenizerRevision string) Preset {
	return Preset{Name: name, Architecture: Architecture{
		Family: "decoder-transformer", ContextTokens: context, VocabularySize: vocabulary,
		HiddenSize: hidden, IntermediateSize: intermediate, Layers: layers,
		AttentionHeads: heads, KeyValueHeads: keyValueHeads, TieEmbeddings: true,
		ParameterDType: "bfloat16", Tokenizer: Tokenizer{Name: tokenizerName, Revision: tokenizerRevision},
	}}
}

func RecommendedPreset(tokens int64) (Preset, error) {
	if tokens <= 0 {
		return Preset{}, fmt.Errorf("index selection contains no training tokens")
	}
	ladder := append([]Preset(nil), modelPresets...)
	sort.Slice(ladder, func(i, j int) bool {
		left, _ := ladder[i].Architecture.Forecast()
		right, _ := ladder[j].Architecture.Forecast()
		return left.ApproximateParameters < right.ApproximateParameters
	})
	selected := ladder[0]
	for _, candidate := range ladder {
		forecast, err := candidate.Architecture.Forecast()
		if err != nil {
			return Preset{}, err
		}
		if uint64(tokens)/20 >= forecast.ApproximateParameters {
			selected = candidate
		}
	}
	return selected, nil
}

func ForecastIndexSelection(tokens int64) (Preset, ResourceForecast, error) {
	return ForecastIndexSelectionWithCalibration(tokens, nil)
}

func ForecastIndexSelectionWithCalibration(tokens int64, calibrations []ForecastCalibration) (Preset, ResourceForecast, error) {
	selected, plan, err := planIndexSelection(tokens)
	if err != nil {
		return Preset{}, ResourceForecast{}, err
	}
	report, err := forecastPlanWithCalibration(plan, calibrations)
	return selected, report, err
}

func planIndexSelection(tokens int64) (Preset, Plan, error) {
	selected, err := RecommendedPreset(tokens)
	if err != nil {
		return Preset{}, Plan{}, err
	}
	architecture, err := selected.Architecture.Forecast()
	if err != nil {
		return Preset{}, Plan{}, err
	}
	batch := int64(8)
	sequence := int64(selected.Architecture.ContextTokens)
	steps := tokens / (batch * sequence)
	if tokens%(batch*sequence) != 0 {
		steps++
	}
	plan := Plan{
		Architecture: selected.Architecture, Forecast: architecture,
		Stages: []PlannedStage{{
			Name: "pretrain", Objective: "causal-language-modeling", PlannedTokens: tokens,
			Parameters: training.Parameters{Steps: steps, BatchSize: batch, SequenceLength: sequence, LearningRate: 0.0003, Seed: 42},
		}},
	}
	return selected, plan, nil
}
