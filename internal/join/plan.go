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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Base             *PlanBase           `yaml:"base"`
}

// PlanBase is an existing model a run starts from instead of random weights: a
// published package pinned by its weights and every file's SHA-256. It may live
// in any Hugging Face account, at a pinned commit, or in a file store.
type PlanBase struct {
	Model       string            `yaml:"model"`
	URL         string            `yaml:"url"`
	ModelSHA256 string            `yaml:"model_sha256"`
	Files       map[string]string `yaml:"files"`
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
	if base := plan.Base; base != nil {
		pinned := strings.HasPrefix(base.URL, "file://") || huggingFacePin.MatchString(base.URL)
		if model.ValidateName(base.Model) != nil || !pinned || !validFiles(base.Files, base.ModelSHA256) {
			return Plan{}, fmt.Errorf("%s: base needs a model name, a file:// or huggingface://<user>/<model>@<commit> url, its model_sha256, and every file's sha256, model.safetensors included", path)
		}
	}
	if slices.Contains(plan.Units, "all") || slices.Contains(plan.Units, "close") || len(slices.Compact(slices.Sorted(slices.Values(plan.Units)))) != len(plan.Units) {
		return Plan{}, fmt.Errorf("%s: units must be distinct and not named all, the bootstrap's unit, or close, the closing slots' name", path)
	}
	return plan, nil
}

// Stages lists every round in order: the bootstrap, unless the run starts from
// a base, then pretraining, then post-training.
func (plan Plan) Stages() []string {
	var stages []string
	if plan.Base == nil {
		stages = append(stages, "bootstrap")
	}
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

// runFile resolves a path a plan names: relative to the run directory, or
// absolute, or under the home directory with ~/.
func runFile(runDir, value string) string {
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(runDir, value)
}

// Pin is what every record pins: the SHA-256 of a manifest listing plan.yaml
// and each compose template it names, one "<path> <sha256>" line per file, so a
// changed template changes the run as surely as a changed plan.
func Pin(runDir string, plan Plan) (string, error) {
	files := []string{"plan.yaml", plan.Pretrain.Compose}
	if plan.Posttrain != nil {
		files = append(files, plan.Posttrain.Compose)
	}
	var manifest strings.Builder
	for _, file := range files {
		if file == "" {
			continue // LoadPlan requires a pretrain compose; only hand-built plans lack one
		}
		data, err := os.ReadFile(runFile(runDir, file))
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%s %s\n", file, hex.EncodeToString(digest[:]))
	}
	digest := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(digest[:]), nil
}

var huggingFacePin = regexp.MustCompile(`^huggingface://[A-Za-z0-9._-]+/[A-Za-z0-9._-]+@[0-9a-f]{40}$`)

// start is the plan's base as the record round 0 starts from, or nil for a bootstrap.
func (plan Plan) start() *MergeRecord {
	if plan.Base == nil {
		return nil
	}
	return &MergeRecord{Model: plan.Base.Model, ModelSHA256: plan.Base.ModelSHA256, Files: plan.Base.Files, URL: plan.Base.URL}
}
