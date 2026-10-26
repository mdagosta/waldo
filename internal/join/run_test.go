// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/model"
	"gopkg.in/yaml.v3"
)

// runCompose is a run file's training: one pre-training and one fine-tuning stage.
const runCompose = `kind: waldo-model-compose
schema: 1
interaction:
  template: user-assistant-v1
architecture:
  family: decoder-transformer
  context_tokens: 512
  vocabulary_size: 259
  hidden_size: 32
  intermediate_size: 64
  layers: 1
  attention_heads: 2
  key_value_heads: 1
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer: {name: byte, revision: builtin-byte-schema-1}
stages:
  - name: python
    type: pre-training
    objective: causal-language-modeling
    filter:
      main_content: true
    corpora:
      - path: code/permissive
        weight: 1
    parameters:
      profile: causal-pretrain-weighted
      steps: 2000                  # a single-model baseline's budget; join sets each unit's
      batch_size: 32
      sequence_length: 512
      learning_rate: 0.0006
      evaluation_selection: lowest-sha256-v1
  - name: sft
    type: fine-tuning
    objective: assistant-response-modeling
    conversation: {template: user-assistant-v1, supervised_roles: [assistant]}
    corpora:
      - path: post-train/sft/python-basics-v4
    parameters:
      profile: causal-pretrain-shuffled
      epochs: 8
      batch_size: 8
      sequence_length: 512
      learning_rate: 0.0005
`

const runJoin = `join:
  name: run
  units:
    u1: [pallets/flask, httpie/cli]
    u0: [TheAlgorithms/Python]
  replicas: 2
  closers: 2
  store: file:///tmp/store
  identities:
    alice: ssh-ed25519 AAAA
    bob: {key: ssh-ed25519 BBBB, hf: bob}
  rounds: {bootstrap_tokens: 2000000, pretrain: 2, tokens_per_round: 8192000, posttrain: 3, epochs_per_round: 8}
  replica_floor: 0.01
`

func writeRun(t *testing.T, text string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, RunFile)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRunReadsTheJoinBlock(t *testing.T) {
	plan, err := LoadRun(writeRun(t, runCompose+runJoin))
	if err != nil {
		t.Fatal(err)
	}
	// Units in sorted order, which is the order fine-tuning units take their slices in.
	if strings.Join(plan.Units, ",") != "u0,u1" || len(plan.UnitSources["u1"]) != 2 || plan.Identities["bob"].HuggingFace != "bob" {
		t.Fatalf("plan = %+v", plan)
	}
	if got := strings.Join(plan.Stages(), ","); got != "bootstrap,pretrain,pretrain,posttrain,posttrain,posttrain" {
		t.Fatalf("stages = %s", got)
	}
	if plan.Floor() != 0.01 || plan.Closers != 2 || plan.ClaimMinutes != 60 || plan.Merge != "mean" {
		t.Fatalf("plan = %+v", plan)
	}
	if steps, err := plan.pretrainSteps(); err != nil || steps != 500 {
		t.Fatalf("pretrain steps = %d, %v", steps, err)
	}
	// The same file is an ordinary compose, so it trains a single-model baseline too.
	compose, _, err := model.LoadCompose(writeRun(t, runCompose+runJoin))
	if err != nil || len(compose.Stages) != 2 {
		t.Fatalf("compose = %+v, %v", compose, err)
	}
}

func TestLoadRunRefusesWhatJoinCannotRun(t *testing.T) {
	for name, text := range map[string]string{
		"no join":         runCompose,
		"unknown key":     runCompose + runJoin + "  tamper: {round: 1}\n",
		"store":           runCompose + strings.Replace(runJoin, "file:///tmp/store", "s3://bucket", 1),
		"unit name":       runCompose + strings.Replace(runJoin, "u0: [", "../u0: [", 1),
		"all unit":        runCompose + strings.Replace(runJoin, "u0: [", "all: [", 1),
		"no epochs":       runCompose + strings.Replace(runJoin, ", epochs_per_round: 8", "", 1),
		"no bootstrap":    runCompose + strings.Replace(runJoin, "bootstrap_tokens: 2000000, ", "", 1),
		"no fine-tuning":  strings.SplitN(runCompose, "  - name: sft", 2)[0] + runJoin,
		"invalid compose": strings.Replace(runCompose, "schema: 1", "schema: 9", 1) + runJoin,
	} {
		if _, err := LoadRun(writeRun(t, text)); err == nil {
			t.Fatalf("%s: run accepted", name)
		}
	}
}

// unitStage decodes the one stage of a unit's compose.
func unitStage(t *testing.T, plan Plan, stage, unit, base string) (model.Compose, model.Stage) {
	t.Helper()
	budget, err := plan.budget(stage)
	if err != nil {
		t.Fatal(err)
	}
	text, err := plan.unitCompose(stage, unit, base, 7, budget)
	if err != nil {
		t.Fatal(err)
	}
	var compose model.Compose
	if err := yaml.Unmarshal([]byte(text), &compose); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if len(compose.Stages) != 1 || compose.Join != nil || compose.Stages[0].Parameters.Seed != 7 {
		t.Fatalf("%s compose:\n%s", stage, text)
	}
	return compose, compose.Stages[0]
}

func TestUnitComposesCutTheRunFile(t *testing.T) {
	plan, err := LoadRun(writeRun(t, runCompose+runJoin))
	if err != nil {
		t.Fatal(err)
	}
	// The bootstrap trains every unit's sources from random weights for its tokens.
	compose, stage := unitStage(t, plan, "bootstrap", "all", "")
	if compose.Base != nil || stage.Type != "pre-training" || stage.Parameters.Tokens != 2000000 ||
		strings.Join(stage.Filter.Sources.Include, ",") != "TheAlgorithms/Python,pallets/flask,httpie/cli" {
		t.Fatalf("bootstrap = %+v, %+v", compose.Base, stage)
	}
	// A pretraining unit trains its own sources from the round's base for fixed steps; the stage's own filter stands.
	compose, stage = unitStage(t, plan, "pretrain", "u1", "round-base")
	if compose.Base == nil || compose.Base.Model != "round-base" || stage.Parameters.Steps != 500 ||
		strings.Join(stage.Filter.Sources.Include, ",") != "pallets/flask,httpie/cli" || stage.Filter.MainContent == nil || !*stage.Filter.MainContent ||
		stage.Parameters.EvaluationSelection != "lowest-sha256-v1" {
		t.Fatalf("pretrain = %+v, %+v", compose.Base, stage)
	}
	// A post-training unit trains its slice of the fine-tuning corpus for its epochs.
	compose, stage = unitStage(t, plan, "posttrain", "u1", "round-base")
	selection := stage.Corpora[0]
	if stage.Type != "fine-tuning" || stage.Parameters.Epochs != 8 || selection.Filter == nil || selection.Filter.Slice == nil ||
		*selection.Filter.Slice != (corpus.SliceFilter{Index: 1, Count: 2}) || compose.Interaction.Template != "user-assistant-v1" {
		t.Fatalf("posttrain = %+v", stage)
	}
	if interaction, err := plan.interaction(); err != nil || !strings.Contains(interaction, "template: user-assistant-v1") {
		t.Fatalf("interaction = %q, %v", interaction, err)
	}
	if plan.vocabulary() != 259 {
		t.Fatalf("vocabulary = %d", plan.vocabulary())
	}
}

func TestPinIsTheRunFile(t *testing.T) {
	path := writeRun(t, runCompose+runJoin)
	before, err := Pin(filepath.Dir(path), Plan{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(runCompose, "0.0006", "0.0007", 1)+runJoin), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, err := Pin(filepath.Dir(path), Plan{}); err != nil || after == before {
		t.Fatalf("an edited training kept the pin: %s, %v", after, err)
	}
}

func TestPlanBaseStartsTheRun(t *testing.T) {
	digest := strings.Repeat("a", 64)
	base := "  base:\n    model: python-base\n    url: huggingface://someone/python-base@" + strings.Repeat("0", 40) + "\n    model_sha256: " + digest + "\n    files:\n      model.safetensors: " + digest + "\n"
	withBase := strings.Replace(runJoin, "bootstrap_tokens: 2000000, ", "", 1) + base
	plan, err := LoadRun(writeRun(t, runCompose+withBase))
	if err != nil {
		t.Fatal(err)
	}
	// No bootstrap: round 0 is the first pretraining round, from the base.
	if got := strings.Join(plan.Stages(), ","); got != "pretrain,pretrain,posttrain,posttrain,posttrain" || plan.start().ModelSHA256 != digest {
		t.Fatalf("stages %s, start %+v", got, plan.start())
	}
	merge := MergeRecord{Kind: MergeKind, Schema: RecordSchema, Run: "run", Stage: "pretrain", Model: "m", ModelSHA256: strings.Repeat("c", 64),
		Files: map[string]string{"model.safetensors": strings.Repeat("c", 64)}, Accepted: []AcceptedUnit{{Unit: "u0", Model: "u"}}}
	if validMerge(merge, plan, 0, "pretrain", plan.start()) {
		t.Fatal("a round-0 merge that did not start from the base counted")
	}
	merge.BaseSHA256 = &digest
	if !validMerge(merge, plan, 0, "pretrain", plan.start()) {
		t.Fatal("a round-0 merge from the base did not count")
	}
	for name, bad := range map[string]string{
		"unpinned url":  strings.Replace(base, "@"+strings.Repeat("0", 40), "", 1),
		"unpinned file": strings.Replace(base, "model.safetensors: "+digest, "model.safetensors: "+strings.Repeat("b", 64), 1),
		"bad name":      strings.Replace(base, "model: python-base", "model: ../x", 1),
	} {
		if _, err := LoadRun(writeRun(t, runCompose+strings.Replace(withBase, base, bad, 1))); err == nil {
			t.Fatalf("%s: base accepted", name)
		}
	}
}

func TestGatesBuildATrainingLadder(t *testing.T) {
	gates := "  gates:\n    - {after: pretrain, heldout_loss_at_most: 1.5, extra_rounds: 2}\n"
	plan, err := LoadRun(writeRun(t, runCompose+runJoin+gates))
	if err != nil {
		t.Fatal(err)
	}
	stages := func(slots []Slot) string {
		var names []string
		for _, slot := range slots {
			name := slot.Stage
			if slot.Gate != nil {
				name += "*"
			}
			names = append(names, name)
		}
		return strings.Join(names, ",")
	}
	merge := func(passed *bool) *MergeRecord {
		record := &MergeRecord{}
		if passed != nil {
			record.Gate = &GateResult{After: "pretrain", Passed: *passed}
		}
		return record
	}
	yes, no := true, false
	for name, test := range map[string]struct {
		decided []*MergeRecord
		want    string
		stopped bool
	}{
		// Until the gate round is decided, nothing after it is known.
		"planned":        {nil, "bootstrap,pretrain,pretrain*", false},
		"gate passes":    {[]*MergeRecord{merge(nil), merge(nil), merge(&yes)}, "bootstrap,pretrain,pretrain*,posttrain,posttrain,posttrain", false},
		"gate fails":     {[]*MergeRecord{merge(nil), merge(nil), merge(&no)}, "bootstrap,pretrain,pretrain*,pretrain*", false},
		"passes late":    {[]*MergeRecord{merge(nil), merge(nil), merge(&no), merge(&yes)}, "bootstrap,pretrain,pretrain*,pretrain*,posttrain,posttrain,posttrain", false},
		"never passes":   {[]*MergeRecord{merge(nil), merge(nil), merge(&no), merge(&no), merge(&no)}, "bootstrap,pretrain,pretrain*,pretrain*,pretrain*", true},
		"undecided gate": {[]*MergeRecord{merge(nil), merge(nil), nil}, "bootstrap,pretrain,pretrain*", false},
	} {
		slots, stopped := plan.Schedule(test.decided)
		if got := stages(slots); got != test.want || stopped != test.stopped {
			t.Fatalf("%s: schedule %s (stopped %t), want %s (stopped %t)", name, got, stopped, test.want, test.stopped)
		}
	}
	// A gate round's merges carry its result, and only gate rounds' do.
	gate := plan.gate("pretrain")
	if !gated(MergeRecord{Gate: &GateResult{After: "pretrain"}}, gate) || gated(MergeRecord{}, gate) || gated(MergeRecord{Gate: &GateResult{After: "pretrain"}}, nil) {
		t.Fatal("gate results do not follow gate rounds")
	}
	// Whether the gate passed is part of what closers must agree on.
	if (MergeRecord{Gate: &GateResult{After: "pretrain", Passed: true}}).decision() == (MergeRecord{Gate: &GateResult{After: "pretrain"}}).decision() {
		t.Fatal("a passed and a failed gate made the same decision")
	}
	for name, bad := range map[string]string{
		"two kinds":   "  gates:\n    - {after: pretrain, heldout_loss_at_most: 1.5, evaluation: e.json, evaluator: [python3, e.py]}\n",
		"no kind":     "  gates:\n    - {after: pretrain}\n",
		"bad stage":   "  gates:\n    - {after: bootstrap, heldout_loss_at_most: 1}\n",
		"twice":       "  gates:\n    - {after: pretrain, heldout_loss_at_most: 1}\n    - {after: pretrain, heldout_loss_at_most: 2}\n",
		"no runner":   "  gates:\n    - {after: posttrain, evaluation: e.json}\n",
		"escape path": "  gates:\n    - {after: posttrain, evaluation: ../e.json, evaluator: [python3, e.py]}\n",
	} {
		if _, err := LoadRun(writeRun(t, runCompose+runJoin+bad)); err == nil {
			t.Fatalf("%s: gate accepted", name)
		}
	}
}

func TestPinCoversTheGateFiles(t *testing.T) {
	gates := "  gates:\n    - {after: posttrain, evaluation: eval.json, evaluator: [python3, evaluate.py]}\n"
	path := writeRun(t, runCompose+runJoin+gates)
	directory := filepath.Dir(path)
	for _, file := range []string{"eval.json", "evaluate.py"} {
		if err := os.WriteFile(filepath.Join(directory, file), []byte(file), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := Pin(directory, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "evaluate.py"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, err := Pin(directory, plan); err != nil || after == before {
		t.Fatalf("an edited evaluator kept the pin: %s, %v", after, err)
	}
}

func TestTheE2EComposeIsARunFile(t *testing.T) {
	compose, err := os.ReadFile("../../testing/join/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	join := strings.Replace(runJoin, "posttrain: 3, ", "posttrain: 0, ", 1)
	plan, err := LoadRun(writeRun(t, string(compose)+join))
	if err != nil {
		t.Fatal(err)
	}
	if _, stage := unitStage(t, plan, "pretrain", "u0", "base"); strings.Join(stage.Filter.Sources.Include, ",") != "TheAlgorithms/Python" {
		t.Fatalf("stage = %+v", stage)
	}
}
