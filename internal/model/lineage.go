// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/openwaldo/waldo/internal/modelweights"
)

// Ancestry is the SHA-256 of every weights artifact a model's weights descend
// from: the weights it started from and the ancestors recorded for them. A
// model trained from random weights has none.
func Ancestry(inspection Inspection) []string {
	seen := map[string]bool{}
	if parent := inspection.Model.Parent; parent != nil {
		seen[parent.Artifact.SHA256] = true
		for _, digest := range parent.Lineage {
			seen[digest] = true
		}
	}
	if origin := inspection.Origin; origin != nil {
		for _, artifact := range origin.Artifacts {
			if artifact.Role == "weights" {
				seen[artifact.SHA256] = true
			}
		}
		for _, digest := range origin.Lineage {
			seen[digest] = true
		}
	}
	return sortedKeys(seen)
}

// ExportLineage is what a release records for descendants to find: the
// model's current weights and their ancestry.
func ExportLineage(inspection Inspection) ([]string, error) {
	_, digest, err := currentWeights(inspection)
	if err != nil {
		return nil, err
	}
	return sortedKeys(lineageSet(inspection, digest)), nil
}

// currentWeights is the newest complete real run's weights, else the origin's.
func currentWeights(inspection Inspection) (string, string, error) {
	initialization, err := resolveRunInitialization(inspection, "")
	if err != nil {
		return "", "", err
	}
	if initialization != nil {
		return initialization.Path, initialization.Artifact.SHA256, nil
	}
	if inspection.Origin != nil && inspection.BOM.CurrentOriginSHA256 != "" {
		for _, artifact := range inspection.Origin.Artifacts {
			if artifact.Role == "weights" {
				return filepath.Join(inspection.Path, filepath.FromSlash(artifact.Path)), artifact.SHA256, nil
			}
		}
	}
	return "", "", fmt.Errorf("model %q has no complete real weights", inspection.Model.Name)
}

func lineageSet(inspection Inspection, weights string) map[string]bool {
	set := map[string]bool{weights: true}
	for _, digest := range Ancestry(inspection) {
		set[digest] = true
	}
	return set
}

// sortedKeys is nil for an empty set, matching how an omitted lineage reads back.
func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type mergeParent struct {
	inspection Inspection
	path       string
	weights    string
	weight     float64
}

// mergeParents resolves base.merge and fails closed unless every parent has
// the same architecture and all of them share a recorded ancestor. Averaging
// weights is only meaningful for models trained from common weights.
func (builder Builder) mergeParents(base ComposeBase) ([]mergeParent, error) {
	var parents []mergeParent
	var shared map[string]bool
	for _, declared := range base.Merge {
		inspection, err := Inspect(builder.Root, declared.Model)
		if err != nil {
			return nil, fmt.Errorf("resolve base.merge parent %q: %w", declared.Model, err)
		}
		if len(parents) > 0 && inspection.Model.ArchitectureSHA256 != parents[0].inspection.Model.ArchitectureSHA256 {
			return nil, fmt.Errorf("base.merge parent %q does not have the architecture of %q", declared.Model, parents[0].inspection.Model.Name)
		}
		if inspection.Model.Architecture.Tokenizer.Name != "byte" {
			return nil, fmt.Errorf("base.merge supports byte-tokenizer models; %q uses %s", declared.Model, inspection.Model.Architecture.Tokenizer.Name)
		}
		path, weights, err := currentWeights(inspection)
		if err != nil {
			return nil, fmt.Errorf("base.merge parent: %w", err)
		}
		lineage := lineageSet(inspection, weights)
		if shared == nil {
			shared = lineage
		} else {
			for digest := range shared {
				if !lineage[digest] {
					delete(shared, digest)
				}
			}
		}
		parents = append(parents, mergeParent{inspection, path, weights, declared.Weight})
	}
	if len(shared) == 0 {
		return nil, fmt.Errorf("base.merge parents share no recorded ancestor; only models trained from common weights can be merged")
	}
	return parents, nil
}

// mergeOrigin averages the parents into a merged origin under .merges, named
// by the parents and weights so the same merge is built once.
func (builder Builder) mergeOrigin(base ComposeBase) (Inspection, error) {
	parents, err := builder.mergeParents(base)
	if err != nil {
		return Inspection{}, err
	}
	merge := &OriginMerge{Method: "mean"}
	var paths []string
	var weights []float64
	lineage := map[string]bool{}
	for _, parent := range parents {
		merge.Parents = append(merge.Parents, OriginMergeParent{Model: parent.inspection.Model.Name, ModelID: parent.inspection.Model.ID, WeightsSHA256: parent.weights, Weight: parent.weight})
		paths = append(paths, parent.path)
		weights = append(weights, parent.weight)
		for digest := range lineageSet(parent.inspection, parent.weights) {
			lineage[digest] = true
		}
	}
	if parents[0].weight > 0 {
		merge.Method = "weighted-mean"
	} else {
		weights = nil
	}
	key, err := hashJSON(merge)
	if err != nil {
		return Inspection{}, err
	}
	root := filepath.Join(builder.Root, ".merges")
	if _, err := os.Stat(filepath.Join(root, key)); err == nil {
		return Inspect(root, key)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Inspection{}, err
	}
	temporary, err := os.MkdirTemp(root, ".waldo-merge-*")
	if err != nil {
		return Inspection{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	artifactDirectory := filepath.Join(temporary, "origin", "artifacts")
	if err := os.MkdirAll(artifactDirectory, 0o755); err != nil {
		return Inspection{}, err
	}
	builder.report(Progress{Phase: "merge", Message: fmt.Sprintf("merging %d models (%s)", len(parents), merge.Method)})
	if _, err := modelweights.Merge(paths, weights, filepath.Join(artifactDirectory, "model.safetensors")); err != nil {
		return Inspection{}, err
	}
	architecture := parents[0].inspection.Model.Architecture
	artifacts, err := writeOriginRuntime(temporary, architecture, parents[0].inspection.Model.ArchitectureSHA256)
	if err != nil {
		return Inspection{}, err
	}
	origin := OriginBOM{
		Kind: "openwaldo-bom", Schema: 1, Subject: "model-origin", Source: OriginSource{Provider: "merge"},
		ArchitectureSHA256: parents[0].inspection.Model.ArchitectureSHA256, SourceArtifacts: []OriginArtifact{}, Artifacts: artifacts,
		Merge: merge, Lineage: sortedKeys(lineage),
	}
	if err := publishOrigin(root, key, temporary, origin, architecture, builder.clock()()); err != nil {
		return Inspection{}, err
	}
	committed = true
	return Inspect(root, key)
}
