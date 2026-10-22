// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/model"
	"gopkg.in/yaml.v3"
)

func signingKey(t *testing.T, name string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", name, "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, output)
	}
	public, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return path, strings.Join(strings.Fields(string(public))[:2], " ")
}

func writePlan(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPlanReadsWalditoPlans(t *testing.T) {
	plan, err := LoadPlan(writePlan(t, `name: smoke
units: [u0, u1]
replicas: 2
store: file:///tmp/store
identities:
  alice: ssh-ed25519 AAAA
  bob:
    key: ssh-ed25519 BBBB
    hf: bob
tamper: {round: 1, unit: u1, identity: bob, kind: lazy}
pretrain:
  compose: pretrain.yaml.tmpl
  bootstrap_tokens: 2000000
  rounds: 1
  tokens_per_round: 4000000   # ~244 steps
  shards:
    u0: [TheAlgorithms/Python]
    u1: [pallets/flask, httpie/cli]
posttrain:
  compose: sft.yaml.tmpl
  rounds: 2
  epochs_per_round: 1
replica_tolerance: 0.5
`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identities["alice"].Key != "ssh-ed25519 AAAA" || plan.Identities["bob"].HuggingFace != "bob" || plan.Merge != "mean" {
		t.Fatalf("plan = %+v", plan)
	}
	if got := strings.Join(plan.Stages(), ","); got != "bootstrap,pretrain,posttrain,posttrain" {
		t.Fatalf("stages = %s", got)
	}
	if plan.Tolerance() != 0.5 || plan.Floor() != 0.001 || len(plan.Pretrain.Shards["u1"]) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestLoadPlanRejectsUnsupportedPlans(t *testing.T) {
	for name, text := range map[string]string{
		"store":  "name: x\nunits: [u0]\nstore: s3://bucket\npretrain: {compose: p.tmpl}\n",
		"merge":  "name: x\nunits: [u0]\nmerge: diloco\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"units":  "name: x\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"signer": "name: x\nunits: [u0]\nstore: huggingface\nidentities: {alice: {hf: alice}}\npretrain: {compose: p.tmpl}\n",
	} {
		if _, err := LoadPlan(writePlan(t, text)); err == nil {
			t.Fatalf("%s: plan accepted", name)
		}
	}
}

func TestReadStateVerifiesSignatures(t *testing.T) {
	aliceKey, alicePublic := signingKey(t, "alice")
	malloryKey, _ := signingKey(t, "mallory")
	plan := Plan{Name: "run", Units: []string{"u0"}, Replicas: 1, Identities: map[string]Identity{"alice": {Key: alicePublic}, "bob": {Key: alicePublic}}}
	runDir := t.TempDir()
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		t.Fatal(err)
	}
	submissions := filepath.Join(roundDir(runDir, 0), "submissions")
	if err := writeSigned(filepath.Join(submissions, "all-alice.yaml"), Submission{Round: 0, Unit: "all", Identity: "alice"}, aliceKey); err != nil {
		t.Fatal(err)
	}
	// Forged: claims alice but signed by another key. Misplaced: a valid record in the wrong file.
	if err := writeSigned(filepath.Join(submissions, "all-bob.yaml"), Submission{Round: 0, Unit: "all", Identity: "bob"}, malloryKey); err != nil {
		t.Fatal(err)
	}
	if err := writeSigned(filepath.Join(submissions, "u9-alice.yaml"), Submission{Round: 0, Unit: "all", Identity: "alice"}, aliceKey); err != nil {
		t.Fatal(err)
	}
	state, err := ReadState(runDir, plan, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Rounds[0].Submissions["all"]; len(got) != 1 || got[0].Identity != "alice" {
		t.Fatalf("submissions = %+v", state.Rounds[0].Submissions)
	}
	if err := writeSigned(filepath.Join(roundDir(runDir, 0), "merge.yaml"), MergeRecord{Round: 0, Identity: "bob"}, malloryKey); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(runDir, plan, allowed); err == nil || !strings.Contains(err.Error(), "not a valid merge record") {
		t.Fatalf("forged merge error = %v", err)
	}
}

func TestNextAction(t *testing.T) {
	plan := Plan{Units: []string{"u0", "u1"}, Replicas: 2, Pretrain: PretrainPlan{Rounds: 1}}
	merged := Round{Merge: &MergeRecord{ClosedAt: "2026-10-21T10:00:00+00:00"}}
	submitted := func(pairs ...string) Round {
		round := Round{Submissions: map[string][]Submission{}}
		for index := 0; index < len(pairs); index += 2 {
			round.Submissions[pairs[index]] = append(round.Submissions[pairs[index]], Submission{Unit: pairs[index], Identity: pairs[index+1]})
		}
		return round
	}
	now := time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		rounds   []Round
		identity string
		minutes  float64
		want     Action
	}{
		"bootstrap":       {nil, "alice", 0, Action{Kind: "contribute", Round: 0, Stage: "bootstrap", Unit: "all"}},
		"second replica":  {[]Round{submitted("all", "alice")}, "bob", 0, Action{Kind: "contribute", Round: 0, Stage: "bootstrap", Unit: "all"}},
		"waits on others": {[]Round{submitted("all", "alice")}, "alice", 0, Action{Kind: "idle", Round: 0, Stage: "bootstrap"}},
		"closes full":     {[]Round{submitted("all", "alice", "all", "bob")}, "carol", 0, Action{Kind: "close", Round: 0, Stage: "bootstrap"}},
		"lowest unit":     {[]Round{merged, submitted("u0", "alice")}, "alice", 0, Action{Kind: "contribute", Round: 1, Stage: "pretrain", Unit: "u1"}},
		"late round":      {[]Round{merged, submitted("u0", "alice", "u0", "bob")}, "alice", 20, Action{Kind: "close", Round: 1, Stage: "pretrain"}},
		"not yet late":    {[]Round{merged, submitted("u0", "alice", "u0", "bob")}, "alice", 60, Action{Kind: "contribute", Round: 1, Stage: "pretrain", Unit: "u1"}},
		"done":            {[]Round{merged, merged}, "alice", 0, Action{Kind: "done", Round: 1}},
	} {
		plan.RoundMinutes = test.minutes
		if got := (State{Plan: plan, Rounds: test.rounds}).Next(test.identity, now); got != test.want {
			t.Fatalf("%s: next = %+v, want %+v", name, got, test.want)
		}
	}
}

func TestComposeInputsMatchWaldito(t *testing.T) {
	// Reference values from waldito's piece_seed, string.Template, and json.dumps.
	if got := pieceSeed("mixed-v1", 0, "all"); got != 7855955 {
		t.Fatalf("pieceSeed = %d", got)
	}
	if got := pieceSeed("python-basics-v1", 7, "u2"); got != 15339753 {
		t.Fatalf("pieceSeed = %d", got)
	}
	if got, err := fillTemplate("a $x ${y} $$z", map[string]string{"x": "1", "y": "2"}); err != nil || got != "a 1 2 $z" {
		t.Fatalf("fillTemplate = %q, %v", got, err)
	}
	if _, err := fillTemplate("$missing", nil); err == nil {
		t.Fatal("unknown template variable accepted")
	}
	if got := jsonList([]byte(`["a/b","c"]`)); got != `["a/b", "c"]` {
		t.Fatalf("jsonList = %s", got)
	}
}

func TestPosttrainComposeTakesItsUnitsSlice(t *testing.T) {
	runDir := t.TempDir()
	template, err := os.ReadFile("../../testing/join/sft.yaml.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "sft.yaml.tmpl"), template, 0o644); err != nil {
		t.Fatal(err)
	}
	joiner := &Joiner{runDir: runDir, plan: Plan{Name: "run", Units: []string{"u0", "u1", "u2"}, Posttrain: &PosttrainPlan{Compose: "sft.yaml.tmpl", Corpus: "post-train/sft/python-hello-v1", EpochsPerRound: 4}}}
	values, err := joiner.values("posttrain", "u1")
	if err != nil {
		t.Fatal(err)
	}
	text, err := joiner.compose("posttrain", "base-model", 7, values["budget"], values)
	if err != nil {
		t.Fatal(err)
	}
	var compose model.Compose
	if err := yaml.Unmarshal([]byte(text), &compose); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	selection := compose.Stages[0].Corpora[0]
	if selection.Path != "post-train/sft/python-hello-v1" || selection.Filter == nil || *selection.Filter.Slice != (corpus.SliceFilter{Index: 1, Count: 3}) {
		t.Fatalf("corpus selection = %+v\n%s", selection, text)
	}
	if compose.Base == nil || compose.Base.Model != "base-model" || compose.Stages[0].Parameters.Epochs != 4 {
		t.Fatalf("compose = %+v", compose)
	}
}
