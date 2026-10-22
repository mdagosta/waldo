// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if err := os.WriteFile(path, []byte("kind: waldo-join-plan\nschema: 1\n"+text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPlanReadsRunPlans(t *testing.T) {
	plan, err := LoadPlan(writePlan(t, `name: smoke
units: [u0, u1]
replicas: 2
store: file:///tmp/store
identities:
  alice: ssh-ed25519 AAAA
  bob:
    key: ssh-ed25519 BBBB
    hf: bob
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
	if plan.Tolerance() != 0.5 || plan.Floor() != 0.001 || len(plan.Pretrain.Shards["u1"]) != 2 || plan.Closers != 3 || plan.ClaimMinutes != 60 || plan.Quorum() != 2 || plan.Closing() != 2 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestLoadPlanRejectsUnsupportedPlans(t *testing.T) {
	for name, text := range map[string]string{
		"store":          "name: x\nunits: [u0]\nstore: s3://bucket\npretrain: {compose: p.tmpl}\n",
		"merge":          "name: x\nunits: [u0]\nmerge: diloco\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"units":          "name: x\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"signer":         "name: x\nunits: [u0]\nstore: huggingface\nidentities: {alice: {hf: alice}}\npretrain: {compose: p.tmpl}\n",
		"unit name":      "name: x\nunits: [../u0]\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"identity":       "name: x\nunits: [u0]\nstore: huggingface\nidentities: {Alice/x: k}\npretrain: {compose: p.tmpl}\n",
		"run name":       "name: X Run\nunits: [u0]\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"all unit":       "name: x\nunits: [all]\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"close unit":     "name: x\nunits: [close]\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"duplicate":      "name: x\nunits: [u0, u0]\nstore: huggingface\npretrain: {compose: p.tmpl}\n",
		"unknown":        "name: x\nunits: [u0]\nstore: huggingface\ntamper: {round: 1}\npretrain: {compose: p.tmpl}\n",
		"identity field": "name: x\nunits: [u0]\nstore: huggingface\nidentities: {alice: {key: k, email: a}}\npretrain: {compose: p.tmpl}\n",
	} {
		if _, err := LoadPlan(writePlan(t, text)); err == nil {
			t.Fatalf("%s: plan accepted", name)
		}
	}
}

func submissionFixture(unit, identity string) Submission {
	digest := strings.Repeat("a", 64)
	return Submission{
		Kind: SubmissionKind, Schema: RecordSchema, Run: "run", Round: 0, Stage: "bootstrap", Unit: unit, Identity: identity,
		Model: "waldo-run-r0000-" + unit + "-" + identity, ModelSHA256: digest,
		Files: map[string]string{"model.safetensors": digest, "config.json": strings.Repeat("b", 64)},
	}
}

func claimFixture(slot string, attempt int, identity string) Claim {
	return Claim{Kind: ClaimKind, Schema: RecordSchema, Run: "run", Round: 0, Stage: "bootstrap", Slot: slot, Attempt: attempt, Identity: identity, CreatedAt: "2026-10-21T10:00:00+00:00"}
}

func mergeFixture(identity, digest string) MergeRecord {
	return MergeRecord{Kind: MergeKind, Schema: RecordSchema, Run: "run", Round: 0, Stage: "bootstrap", Identity: identity, Method: "single",
		Accepted: []AcceptedUnit{{Unit: "all", Identity: "alice", Model: "waldo-run-r0000-all-alice", ModelSHA256: strings.Repeat("a", 64)}},
		Model:    "waldo-run-r0000-merge", ModelSHA256: digest, Files: map[string]string{"model.safetensors": digest}, ClosedAt: "2026-10-21T11:00:00+00:00"}
}

// signer writes signed records into one round of a run directory.
func signer(t *testing.T, runDir string) func(string, any, string) {
	return func(file string, record any, key string) {
		t.Helper()
		if err := writeSigned(filepath.Join(roundDir(runDir, 0), file), record, key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadStateVerifiesSignatures(t *testing.T) {
	aliceKey, alicePublic := signingKey(t, "alice")
	malloryKey, _ := signingKey(t, "mallory")
	plan := Plan{Name: "run", Units: []string{"u0"}, Replicas: 2, Closers: 1, Identities: map[string]Identity{"alice": {Key: alicePublic}, "bob": {Key: alicePublic}}}
	runDir := t.TempDir()
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		t.Fatal(err)
	}
	write := signer(t, runDir)
	write("claims/all-1.yaml", claimFixture("all-1", 1, "alice"), aliceKey)
	write("submissions/all-alice.yaml", submissionFixture("all", "alice"), aliceKey)
	// Forged: claims bob but signed by another key. Misplaced: a valid record in the wrong file.
	write("claims/all-2.yaml", claimFixture("all-2", 1, "bob"), malloryKey)
	write("submissions/all-bob.yaml", submissionFixture("all", "bob"), malloryKey)
	write("submissions/u9-alice.yaml", submissionFixture("all", "alice"), aliceKey)
	state, err := ReadState(runDir, plan, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Rounds[0].Submissions["all"]; len(got) != 1 || got[0].Identity != "alice" || len(state.Rounds[0].Claims) != 1 {
		t.Fatalf("round = %+v", state.Rounds[0])
	}
	// A forged merge is no merge: it never closes the round.
	write("claims/close-1.yaml", claimFixture("close-1", 1, "bob"), malloryKey)
	write("merges/bob.yaml", mergeFixture("bob", strings.Repeat("c", 64)), malloryKey)
	if state, err = ReadState(runDir, plan, allowed); err != nil || state.Rounds[0].Merge != nil || len(state.Rounds[0].Merges) != 0 {
		t.Fatalf("forged merge counted: %+v, %v", state.Rounds[0], err)
	}
}

func TestReadStateCountsOnlyClaimedWork(t *testing.T) {
	key, public := signingKey(t, "alice")
	plan := Plan{Name: "run", Units: []string{"u0"}, Replicas: 2, Closers: 1, Identities: map[string]Identity{"alice": {Key: public}}}
	runDir := t.TempDir()
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		t.Fatal(err)
	}
	write := signer(t, runDir)
	write("submissions/all-alice.yaml", submissionFixture("all", "alice"), key)
	write("merges/alice.yaml", mergeFixture("alice", strings.Repeat("c", 64)), key)
	// Out of place: a second attempt with no first, and a slot beyond the plan's replicas.
	write("claims/all-1.2.yaml", claimFixture("all-1", 2, "alice"), key)
	write("claims/all-3.yaml", claimFixture("all-3", 1, "alice"), key)
	write("claims/all-1.yaml", claimFixture("all-2", 1, "alice"), key)
	state, err := ReadState(runDir, plan, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if round := state.Rounds[0]; len(round.Claims) != 0 || len(round.Submissions) != 0 || len(round.Merges) != 0 {
		t.Fatalf("unclaimed work counted: %+v", round)
	}
	// One identity holds one replica slot of a unit, however many it claims.
	if err := os.Remove(filepath.Join(roundDir(runDir, 0), "claims", "all-1.2.yaml")); err != nil {
		t.Fatal(err)
	}
	write("claims/all-1.yaml", claimFixture("all-1", 1, "alice"), key)
	write("claims/all-2.yaml", claimFixture("all-2", 1, "alice"), key)
	if state, err = ReadState(runDir, plan, allowed); err != nil {
		t.Fatal(err)
	}
	if round := state.Rounds[0]; len(round.Claims) != 1 || len(round.Claims["all-1"]) != 1 || len(round.Submissions["all"]) != 1 || len(round.replicas("all", 2)) != 1 {
		t.Fatalf("round = %+v", round)
	}
}

func TestReadStateIgnoresMalformedRecords(t *testing.T) {
	key, public := signingKey(t, "bob")
	plan := Plan{Name: "run", Units: []string{"u0"}, Replicas: 1, Identities: map[string]Identity{"bob": {Key: public}}}
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Submission){
		"model path":    func(record *Submission) { record.Model = "../../.." },
		"file path":     func(record *Submission) { record.Files["../../.bashrc"] = strings.Repeat("d", 64) },
		"hidden file":   func(record *Submission) { record.Files[".git"] = strings.Repeat("d", 64) },
		"other run":     func(record *Submission) { record.Run = "another-run" },
		"wrong kind":    func(record *Submission) { record.Kind = MergeKind },
		"wrong stage":   func(record *Submission) { record.Stage = "pretrain" },
		"unpinned file": func(record *Submission) { record.Files["model.safetensors"] = strings.Repeat("e", 64) },
	} {
		runDir := t.TempDir()
		record := submissionFixture("all", "bob")
		mutate(&record)
		write := signer(t, runDir)
		write("claims/all-1.yaml", claimFixture("all-1", 1, "bob"), key)
		write("submissions/all-bob.yaml", record, key)
		state, err := ReadState(runDir, plan, allowed)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Rounds[0].Submissions) != 0 {
			t.Fatalf("%s: malformed record accepted: %+v", name, state.Rounds[0].Submissions)
		}
	}
}

func TestMajorityOfClosersDecidesRound(t *testing.T) {
	keys, identities := map[string]string{}, map[string]Identity{}
	for _, name := range []string{"alice", "bob", "carol", "dave"} {
		key, public := signingKey(t, name)
		keys[name], identities[name] = key, Identity{Key: public}
	}
	plan := Plan{Name: "run", Units: []string{"u0"}, Replicas: 1, Closers: 3, ClaimMinutes: 60, Pretrain: PretrainPlan{Rounds: 1}, Identities: identities}
	runDir := t.TempDir()
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	if err := writeAllowedSigners(allowed, plan.Identities); err != nil {
		t.Fatal(err)
	}
	write := signer(t, runDir)
	write("claims/all-1.yaml", claimFixture("all-1", 1, "alice"), keys["alice"])
	write("submissions/all-alice.yaml", submissionFixture("all", "alice"), keys["alice"])
	honest, cheat := strings.Repeat("c", 64), strings.Repeat("f", 64)
	for position, name := range []string{"alice", "bob", "carol"} {
		write("claims/close-"+strconv.Itoa(position+1)+".yaml", claimFixture("close-"+strconv.Itoa(position+1), 1, name), keys[name])
	}
	read := func() Round {
		t.Helper()
		state, err := ReadState(runDir, plan, allowed)
		if err != nil {
			t.Fatal(err)
		}
		return state.Rounds[0]
	}
	now := time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC)
	// One honest merge and one cheat: no majority yet, and the third closer's slot is still held.
	write("merges/alice.yaml", mergeFixture("alice", honest), keys["alice"])
	write("merges/bob.yaml", mergeFixture("bob", cheat), keys["bob"])
	if round := read(); round.Merge != nil || len(round.Merges) != 2 {
		t.Fatalf("round decided without a majority: %+v", round)
	}
	if got := (State{Plan: plan, Rounds: []Round{read()}}).Next("dave", now); got.Kind != "idle" {
		t.Fatalf("dave = %+v, want idle while close-3 is held", got)
	}
	// A fourth closer claimed before the vote split does not count.
	early := claimFixture("close-4", 1, "dave")
	early.CreatedAt = "2026-10-21T10:30:00+00:00"
	write("claims/close-4.yaml", early, keys["dave"])
	write("merges/dave.yaml", mergeFixture("dave", honest), keys["dave"])
	if round := read(); round.Merge != nil || len(round.Merges) != 2 {
		t.Fatalf("an early fourth closer counted: %+v", round.Merges)
	}
	for _, file := range []string{"claims/close-4.yaml", "claims/close-4.yaml.sig", "merges/dave.yaml", "merges/dave.yaml.sig"} {
		if err := os.Remove(filepath.Join(roundDir(runDir, 0), file)); err != nil {
			t.Fatal(err)
		}
	}
	// The third closer splits the vote three ways, which opens a fourth slot.
	write("merges/carol.yaml", mergeFixture("carol", strings.Repeat("e", 64)), keys["carol"])
	if got := (State{Plan: plan, Rounds: []Round{read()}}).Next("dave", now); got.Kind != "claim" || got.Slot != "close-4" {
		t.Fatalf("dave = %+v, want a claim on close-4", got)
	}
	late := claimFixture("close-4", 1, "dave")
	late.CreatedAt = "2026-10-21T11:05:00+00:00"
	write("claims/close-4.yaml", late, keys["dave"])
	write("merges/dave.yaml", mergeFixture("dave", honest), keys["dave"])
	round := read()
	if round.Merge == nil || round.Merge.ModelSHA256 != honest {
		t.Fatalf("merge = %+v, want the honest majority", round.Merge)
	}
	agreeing, dissenting := round.Agreeing()
	if len(agreeing) != 2 || len(dissenting) != 2 {
		t.Fatalf("agreeing %d, dissenting %d", len(agreeing), len(dissenting))
	}
}

func TestNextAction(t *testing.T) {
	plan := Plan{Units: []string{"u0", "u1"}, Replicas: 2, Closers: 1, ClaimMinutes: 60, Pretrain: PretrainPlan{Rounds: 1}}
	merged := Round{Merge: &MergeRecord{}, ClosedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)}
	// round builds a round from "slot identity" claims, "unit identity" submissions, and "merge identity" merges.
	round := func(entries ...string) Round {
		result := Round{Claims: map[string][]Claim{}, Submissions: map[string][]Submission{}}
		for _, entry := range entries {
			kind, rest, _ := strings.Cut(entry, ":")
			first, identity, _ := strings.Cut(rest, " ")
			switch kind {
			case "claim":
				result.Claims[first] = append(result.Claims[first], Claim{Slot: first, Attempt: len(result.Claims[first]) + 1, Identity: identity, CreatedAt: "2026-10-21T10:10:00+00:00"})
			case "late":
				result.Claims[first] = append(result.Claims[first], Claim{Slot: first, Attempt: len(result.Claims[first]) + 1, Identity: identity, CreatedAt: "2026-10-21T09:00:00+00:00"})
			case "submit":
				result.Submissions[first] = append(result.Submissions[first], Submission{Unit: first, Identity: identity})
			case "merge":
				result.Merges = append(result.Merges, MergeRecord{Identity: identity})
			}
		}
		return result
	}
	full := []string{"claim:all-1 alice", "claim:all-2 bob", "submit:all alice", "submit:all bob"}
	now := time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		rounds   []Round
		identity string
		minutes  float64
		want     Action
	}{
		"claims first slot": {nil, "alice", 0, Action{Kind: "claim", Round: 0, Stage: "bootstrap", Unit: "all", Slot: "all-1", Attempt: 1}},
		"claims next slot":  {[]Round{round("claim:all-1 alice")}, "bob", 0, Action{Kind: "claim", Round: 0, Stage: "bootstrap", Unit: "all", Slot: "all-2", Attempt: 1}},
		"trains its claim":  {[]Round{round("claim:all-1 alice")}, "alice", 0, Action{Kind: "contribute", Round: 0, Stage: "bootstrap", Unit: "all"}},
		"slots all held":    {[]Round{round("claim:all-1 alice", "claim:all-2 bob")}, "carol", 0, Action{Kind: "idle", Round: 0, Stage: "bootstrap"}},
		"retakes expired":   {[]Round{round("claim:all-1 alice", "late:all-2 bob")}, "carol", 0, Action{Kind: "claim", Round: 0, Stage: "bootstrap", Unit: "all", Slot: "all-2", Attempt: 2}},
		"waits on others":   {[]Round{round("claim:all-1 alice", "claim:all-2 bob", "submit:all alice")}, "alice", 0, Action{Kind: "idle", Round: 0, Stage: "bootstrap"}},
		"claims close":      {[]Round{round(full...)}, "carol", 0, Action{Kind: "claim", Round: 0, Stage: "bootstrap", Unit: "close", Slot: "close-1", Attempt: 1}},
		"closes its claim":  {[]Round{round(append(full, "claim:close-1 carol")...)}, "carol", 0, Action{Kind: "close", Round: 0, Stage: "bootstrap", Unit: "close"}},
		"waits on closer":   {[]Round{round(append(full, "claim:close-1 carol")...)}, "alice", 0, Action{Kind: "idle", Round: 0, Stage: "bootstrap"}},
		"lowest free unit":  {[]Round{merged, round("claim:u0-1 alice", "claim:u0-2 bob")}, "carol", 0, Action{Kind: "claim", Round: 1, Stage: "pretrain", Unit: "u1", Slot: "u1-1", Attempt: 1}},
		"late round":        {[]Round{merged, round("claim:u0-1 alice", "claim:u0-2 bob", "submit:u0 alice", "submit:u0 bob")}, "alice", 20, Action{Kind: "claim", Round: 1, Stage: "pretrain", Unit: "close", Slot: "close-1", Attempt: 1}},
		"not yet late":      {[]Round{merged, round("claim:u0-1 alice", "claim:u0-2 bob", "submit:u0 alice", "submit:u0 bob")}, "alice", 60, Action{Kind: "claim", Round: 1, Stage: "pretrain", Unit: "u1", Slot: "u1-1", Attempt: 1}},
		"done":              {[]Round{merged, merged}, "alice", 0, Action{Kind: "done", Round: 1}},
	} {
		plan.RoundMinutes = test.minutes
		if got := (State{Plan: plan, Rounds: test.rounds}).Next(test.identity, now); got != test.want {
			t.Fatalf("%s: next = %+v, want %+v", name, got, test.want)
		}
	}
}

func TestComposeInputsAreDeterministic(t *testing.T) {
	// Every replica of a unit must train, and every verifier probe, with the same seed.
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

func TestLoadPlanRequiresItsKindAndSchema(t *testing.T) {
	for name, header := range map[string]string{"missing": "", "kind": "kind: waldo-plan\nschema: 1\n", "schema": "kind: waldo-join-plan\nschema: 2\n"} {
		path := filepath.Join(t.TempDir(), "plan.yaml")
		if err := os.WriteFile(path, []byte(header+"name: x\nunits: [u0]\nstore: huggingface\npretrain: {compose: p.tmpl}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPlan(path); err == nil || !strings.Contains(err.Error(), "unsupported plan identity") {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
}

func TestLoadTokenAcceptsOnlyAPrivateRegularFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "identity")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(home, "github-token")
	if err := os.WriteFile(token, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	joiner := &Joiner{Home: home}
	if err := joiner.loadToken(); err != nil || joiner.token != "secret" {
		t.Fatalf("token = %q, %v", joiner.token, err)
	}
	if err := os.Chmod(token, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := joiner.loadToken(); err == nil || !strings.Contains(err.Error(), "readable by others") {
		t.Fatalf("readable token error = %v", err)
	}
	if err := os.Chmod(token, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := joiner.loadToken(); err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("readable directory error = %v", err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Mkdir(linked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(token, filepath.Join(linked, "github-token")); err != nil {
		t.Fatal(err)
	}
	if err := (&Joiner{Home: linked}).loadToken(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlinked token error = %v", err)
	}
}
