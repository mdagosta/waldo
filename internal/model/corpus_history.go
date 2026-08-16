// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"maps"
	"strings"
)

// SkippedCorpus identifies a compose selection omitted because this model has
// already completed a run containing the same logical corpus path.
type SkippedCorpus struct {
	Stage string `json:"stage"`
	Path  string `json:"path"`
}

// SkipCompletedCorpora removes previously completed corpus paths from a
// compose. It intentionally compares logical paths only; it does not attempt
// record- or shard-level delta training.
func SkipCompletedCorpora(compose Compose, inspection Inspection) (Compose, []SkippedCorpus) {
	completed := map[string]bool{}
	for position, pin := range inspection.Model.Runs {
		if pin.State != RunComplete || position >= len(inspection.RunBOMs) {
			continue
		}
		for _, path := range inspection.RunBOMs[position].CorpusBOM.Paths {
			completed[logicalCorpusPath(path)] = true
		}
	}
	if len(completed) == 0 {
		return compose, nil
	}

	filtered := compose
	filtered.Stages = make([]Stage, 0, len(compose.Stages))
	var skipped []SkippedCorpus
	for _, original := range compose.Stages {
		stage := original
		stage.Corpora = make([]CorpusSelection, 0, len(original.Corpora))
		stage.Parameters.CorpusWeights = maps.Clone(original.Parameters.CorpusWeights)
		for _, selection := range original.Corpora {
			if !completed[logicalCorpusPath(selection.Path)] {
				stage.Corpora = append(stage.Corpora, selection)
				continue
			}
			skipped = append(skipped, SkippedCorpus{Stage: stage.Name, Path: selection.Path})
			for path := range stage.Parameters.CorpusWeights {
				if logicalCorpusPath(path) == logicalCorpusPath(selection.Path) {
					delete(stage.Parameters.CorpusWeights, path)
				}
			}
		}
		if len(stage.Corpora) > 0 {
			filtered.Stages = append(filtered.Stages, stage)
		}
	}
	return filtered, skipped
}

func logicalCorpusPath(path string) string {
	path = strings.TrimSuffix(strings.TrimSpace(path), "/")
	return strings.TrimSuffix(strings.TrimSuffix(path, ".yaml"), ".json")
}
