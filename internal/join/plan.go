// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package join contributes to a training run kept in a git repository: each
// pass reads the run's plan and signed records, does the next unit of work,
// and commits its own signed record. No process coordinates the run.
package join

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/openwaldo/waldo/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	PlanKind   = "waldo-join-plan"
	PlanSchema = 1
)

// Plan is a run's plan.yaml, the one file every contributor reads to know
// the run's units, rounds, identities, and store.
type Plan struct {
	Kind             string              `yaml:"kind"`
	Schema           int                 `yaml:"schema"`
	Name             string              `yaml:"name"`
	Units            []string            `yaml:"units"`
	Replicas         int                 `yaml:"replicas"`
	Merge            string              `yaml:"merge"`
	Store            string              `yaml:"store"`
	Identities       map[string]Identity `yaml:"identities"`
	Pretrain         PretrainPlan        `yaml:"pretrain"`
	Posttrain        *PosttrainPlan      `yaml:"posttrain"`
	MinStepsPerRound int                 `yaml:"min_steps_per_round"`
	ReplicaTolerance *float64            `yaml:"replica_tolerance"`
	ReplicaFloor     *float64            `yaml:"replica_floor"`
	RoundMinutes     float64             `yaml:"round_minutes"`
	Closers          int                 `yaml:"closers"`
	ClaimMinutes     float64             `yaml:"claim_minutes"`
}

// Identity is a plan contributor: an ssh signing key and, for Hugging Face
// stores, the account its uploads come from. A bare string is just the key.
type Identity struct {
	Key         string `yaml:"key"`
	HuggingFace string `yaml:"hf"`
}

type PretrainPlan struct {
	Compose         string              `yaml:"compose"`
	BootstrapTokens int64               `yaml:"bootstrap_tokens"`
	Rounds          int                 `yaml:"rounds"`
	TokensPerRound  int64               `yaml:"tokens_per_round"`
	Shards          map[string][]string `yaml:"shards"`
}

type PosttrainPlan struct {
	Compose        string `yaml:"compose"`
	Corpus         string `yaml:"corpus"` // index path; each unit trains its own slice of it
	Rounds         int    `yaml:"rounds"`
	EpochsPerRound int    `yaml:"epochs_per_round"`
}

func (identity *Identity) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		identity.Key = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("identity must be a signing key or a {key, hf} mapping")
	}
	for index := 0; index < len(node.Content); index += 2 {
		if key := node.Content[index].Value; key != "key" && key != "hf" {
			return fmt.Errorf("identity has unknown field %q", key)
		}
	}
	type plain Identity
	return node.Decode((*plain)(identity))
}

func LoadPlan(path string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var plan Plan
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("%s: %w", path, err)
	}
	if plan.Kind != PlanKind || plan.Schema != PlanSchema {
		return Plan{}, fmt.Errorf("%s: unsupported plan identity %q schema %d; expected kind %s, schema %d", path, plan.Kind, plan.Schema, PlanKind, PlanSchema)
	}
	if plan.Replicas == 0 {
		plan.Replicas = 1
	}
	if plan.Merge == "" {
		plan.Merge = "mean"
	}
	if plan.Closers == 0 {
		plan.Closers = 3
	}
	if plan.ClaimMinutes == 0 {
		plan.ClaimMinutes = 60
	}
	if plan.Name == "" || len(plan.Units) == 0 || plan.Replicas < 1 || plan.Closers < 1 || plan.ClaimMinutes < 0 || plan.Pretrain.Compose == "" {
		return Plan{}, fmt.Errorf("%s: a plan needs a name, units, replicas, and a pretrain compose", path)
	}
	if plan.Merge != "mean" {
		return Plan{}, fmt.Errorf("%s: merge %q is not supported; use mean", path, plan.Merge)
	}
	if plan.Store != "huggingface" && !strings.HasPrefix(plan.Store, "file://") {
		return Plan{}, fmt.Errorf("%s: store %q: expected huggingface or file:///path", path, plan.Store)
	}
	// Run, unit, and identity names become directory, model, and branch names.
	names := append([]string{plan.Name}, plan.Units...)
	for name, identity := range plan.Identities {
		if identity.Key == "" {
			return Plan{}, fmt.Errorf("%s: identity %s has no signing key", path, name)
		}
		names = append(names, name)
	}
	for _, name := range names {
		if model.ValidateName(name) != nil {
			return Plan{}, fmt.Errorf("%s: %q must use lowercase letters, digits, '.', '_', and '-'", path, name)
		}
	}
	if slices.Contains(plan.Units, "all") || slices.Contains(plan.Units, "close") || len(slices.Compact(slices.Sorted(slices.Values(plan.Units)))) != len(plan.Units) {
		return Plan{}, fmt.Errorf("%s: units must be distinct and not named all, the bootstrap's unit, or close, the closing slots' name", path)
	}
	return plan, nil
}

// Stages lists every round in order: the bootstrap, pretraining, then post-training.
func (plan Plan) Stages() []string {
	stages := []string{"bootstrap"}
	for range plan.Pretrain.Rounds {
		stages = append(stages, "pretrain")
	}
	if plan.Posttrain != nil {
		for range plan.Posttrain.Rounds {
			stages = append(stages, "posttrain")
		}
	}
	return stages
}

// RoundUnits is the bootstrap's single shared unit, or the plan's units.
func (plan Plan) RoundUnits(stage string) []string {
	if stage == "bootstrap" {
		return []string{"all"}
	}
	return plan.Units
}

func (plan Plan) Tolerance() float64 {
	if plan.ReplicaTolerance == nil {
		return 0.25
	}
	return *plan.ReplicaTolerance
}

// Floor is the loss gap below which honest replicas always agree, since a
// converged round moves the loss less than replica noise.
func (plan Plan) Floor() float64 {
	if plan.ReplicaFloor == nil {
		return 0.001
	}
	return *plan.ReplicaFloor
}

// Quorum is how many closers must sign the same merge for a round to close:
// a majority of the plan's closers, or of its identities if it has fewer.
func (plan Plan) Quorum() int {
	closers := plan.Closers
	if count := len(plan.Identities); count > 0 && count < closers {
		closers = count
	}
	return closers/2 + 1
}

// Closing is how many closing slots are open before any disagreement: the
// plan's closers, or every identity if it has fewer.
func (plan Plan) Closing() int {
	if count := len(plan.Identities); count > 0 && count < plan.Closers {
		return count
	}
	return plan.Closers
}
