// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openwaldo/waldo/internal/model"
)

// Round is what the repo records for one round: its claims by slot, the
// submissions and merges of identities holding claims, and, once a majority
// of closers sign the same merge, that merge.
type Round struct {
	Claims      map[string][]Claim // by slot, attempts in order
	Submissions map[string][]Submission
	Merges      []MergeRecord // every closer's merge, agreeing or not
	Merge       *MergeRecord  // the majority's merge, once the round is decided
	ClosedAt    time.Time     // when the majority formed
}

// State is a run as its signed records describe it; unsigned or misplaced records are ignored.
type State struct {
	Plan   Plan
	Rounds []Round
}

func roundDir(runDir string, number int) string {
	return filepath.Join(runDir, "rounds", fmt.Sprintf("round-%04d", number))
}

// slotUnit is the unit a slot replicates, or "close" for a closing slot.
func slotUnit(slot string) string {
	unit, _, _ := cutLast(slot, "-")
	return unit
}

func cutLast(text, separator string) (string, string, bool) {
	if index := strings.LastIndex(text, separator); index >= 0 {
		return text[:index], text[index+len(separator):], true
	}
	return text, "", false
}

// ReadState reads every round's records under runDir, verifying each signature against the plan's keys.
func ReadState(runDir string, plan Plan, allowedSigners string) (State, error) {
	state := State{Plan: plan}
	for number := range plan.Stages() {
		directory := roundDir(runDir, number)
		round := Round{Claims: map[string][]Claim{}, Submissions: map[string][]Submission{}}
		paths, err := filepath.Glob(filepath.Join(directory, "claims", "*.yaml"))
		if err != nil {
			return State{}, err
		}
		var claims []Claim
		for _, path := range paths {
			var record Claim
			ok, err := readSigned(path, allowedSigners, plan.Identities, &record, func() string { return record.Identity })
			if err != nil {
				return State{}, err
			}
			if ok && record.Round == number && record.Attempt >= 1 && filepath.Base(path) == claimFile(record.Slot, record.Attempt) && validClaim(record, plan, number) {
				claims = append(claims, record)
			}
		}
		// Attempts in order, each only after the one before it, and one slot per identity per unit.
		sort.Slice(claims, func(i, j int) bool {
			if claims[i].Slot != claims[j].Slot {
				return claims[i].Slot < claims[j].Slot
			}
			return claims[i].Attempt < claims[j].Attempt
		})
		holds := map[string]string{} // identity and unit to the slot it holds
		for _, record := range claims {
			key := record.Identity + " " + slotUnit(record.Slot)
			if held, ok := holds[key]; (ok && held != record.Slot) || len(round.Claims[record.Slot]) != record.Attempt-1 {
				continue
			}
			holds[key] = record.Slot
			round.Claims[record.Slot] = append(round.Claims[record.Slot], record)
		}
		paths, err = filepath.Glob(filepath.Join(directory, "submissions", "*.yaml"))
		if err != nil {
			return State{}, err
		}
		sort.Strings(paths)
		for _, path := range paths {
			var record Submission
			ok, err := readSigned(path, allowedSigners, plan.Identities, &record, func() string { return record.Identity })
			if err != nil {
				return State{}, err
			}
			// A record must match the file and run it is in, so none can be replayed elsewhere,
			// and its identity must have claimed the unit, so none trains a unit unasked.
			if _, claimed := holds[record.Identity+" "+record.Unit]; ok && claimed && record.Round == number &&
				filepath.Base(path) == record.Unit+"-"+record.Identity+".yaml" && validSubmission(record, plan, number) {
				round.Submissions[record.Unit] = append(round.Submissions[record.Unit], record)
			}
		}
		paths, err = filepath.Glob(filepath.Join(directory, "merges", "*.yaml"))
		if err != nil {
			return State{}, err
		}
		sort.Strings(paths)
		for _, path := range paths {
			var record MergeRecord
			ok, err := readSigned(path, allowedSigners, plan.Identities, &record, func() string { return record.Identity })
			if err != nil {
				return State{}, err
			}
			if _, claimed := holds[record.Identity+" close"]; ok && claimed && record.Round == number &&
				filepath.Base(path) == record.Identity+".yaml" && validMerge(record, plan, number) {
				round.Merges = append(round.Merges, record)
			}
		}
		round.Merges = extraClosers(round, plan)
		round.Merge, round.ClosedAt = decide(round.Merges, plan.Quorum())
		state.Rounds = append(state.Rounds, round)
		if round.Merge == nil {
			break
		}
	}
	return state, nil
}

// extraClosers drops the merges of closers past the plan's own whose claim
// came before every earlier closing slot had its merge, since a slot past the
// plan's closers opens only when those closers disagree.
func extraClosers(round Round, plan Plan) []MergeRecord {
	closedAt := func(slot string) (time.Time, bool) {
		for _, claim := range round.Claims[slot] {
			for _, record := range round.Merges {
				if record.Identity == claim.Identity {
					closed, err := time.Parse(time.RFC3339, record.ClosedAt)
					return closed, err == nil
				}
			}
		}
		return time.Time{}, false
	}
	valid := map[string]bool{}
	for position := 1; ; position++ {
		claims, ok := round.Claims[fmt.Sprintf("close-%d", position)]
		if !ok {
			break
		}
		for _, claim := range claims {
			created, _ := time.Parse(time.RFC3339, claim.CreatedAt)
			opened := position <= plan.Closing()
			if !opened {
				opened = true
				for earlier := 1; earlier < position; earlier++ {
					if closed, ok := closedAt(fmt.Sprintf("close-%d", earlier)); !ok || created.Before(closed) {
						opened = false
					}
				}
			}
			valid[claim.Identity] = valid[claim.Identity] || opened
		}
	}
	var kept []MergeRecord
	for _, record := range round.Merges {
		if valid[record.Identity] {
			kept = append(kept, record)
		}
	}
	return kept
}

// decision is what closers must agree on: the round's starting weights, the
// units accepted, the method, and the merged weights. Their measured losses
// may differ slightly across hardware and are evidence, not the decision.
func (record MergeRecord) decision() string {
	base := ""
	if record.BaseSHA256 != nil {
		base = *record.BaseSHA256
	}
	parts := []string{base, record.Method, record.ModelSHA256}
	for _, unit := range record.Accepted {
		parts = append(parts, unit.Unit+" "+unit.Identity+" "+unit.ModelSHA256)
	}
	return strings.Join(parts, "\n")
}

// decide returns the merge a quorum of closers signed, and when that quorum
// formed; if more than one decision reaches quorum, the first to do so stands.
func decide(merges []MergeRecord, quorum int) (*MergeRecord, time.Time) {
	byDecision := map[string][]MergeRecord{}
	for _, record := range merges {
		byDecision[record.decision()] = append(byDecision[record.decision()], record)
	}
	var chosen *MergeRecord
	var closedAt time.Time
	for _, agreeing := range byDecision {
		if len(agreeing) < quorum {
			continue
		}
		times := make([]time.Time, len(agreeing))
		for index, record := range agreeing {
			times[index], _ = time.Parse(time.RFC3339, record.ClosedAt)
		}
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		formed := times[quorum-1]
		if chosen == nil || formed.Before(closedAt) || (formed.Equal(closedAt) && agreeing[0].Identity < chosen.Identity) {
			record := agreeing[0]
			chosen, closedAt = &record, formed
		}
	}
	return chosen, closedAt
}

// Agreeing is every merge that signed the round's decision, so its weights can
// be fetched from any of their stores; Dissenting is every other one.
func (round Round) Agreeing() (agreeing, dissenting []MergeRecord) {
	for _, record := range round.Merges {
		if round.Merge != nil && record.decision() == round.Merge.decision() {
			agreeing = append(agreeing, record)
		} else {
			dissenting = append(dissenting, record)
		}
	}
	return agreeing, dissenting
}

// Names and hashes in records become paths, model names, and URLs, so a record
// counts only if every one of them is well formed.
var (
	fileName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	sha256Value = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// validFiles requires plain file names, SHA-256 values, and model.safetensors pinned to the record's model hash.
func validFiles(files map[string]string, modelSHA256 string) bool {
	if !sha256Value.MatchString(modelSHA256) || files["model.safetensors"] != modelSHA256 {
		return false
	}
	for name, digest := range files {
		if !fileName.MatchString(name) || !sha256Value.MatchString(digest) {
			return false
		}
	}
	return true
}

func validSubmission(record Submission, plan Plan, number int) bool {
	stage := plan.Stages()[number]
	return record.Kind == SubmissionKind && record.Schema == RecordSchema && record.Run == plan.Name && record.Stage == stage &&
		slices.Contains(plan.RoundUnits(stage), record.Unit) && model.ValidateName(record.Model) == nil &&
		validFiles(record.Files, record.ModelSHA256)
}

func validClaim(record Claim, plan Plan, number int) bool {
	stage := plan.Stages()[number]
	unit, index, _ := cutLast(record.Slot, "-")
	position, err := strconv.Atoi(index)
	if err != nil || position < 1 || strconv.Itoa(position) != index {
		return false
	}
	if unit != "close" && (!slices.Contains(plan.RoundUnits(stage), unit) || position > plan.Replicas) {
		return false
	}
	_, err = time.Parse(time.RFC3339, record.CreatedAt)
	return err == nil && record.Kind == ClaimKind && record.Schema == RecordSchema && record.Run == plan.Name && record.Stage == stage
}

func validMerge(record MergeRecord, plan Plan, number int) bool {
	stage := plan.Stages()[number]
	if record.Kind != MergeKind || record.Schema != RecordSchema || record.Run != plan.Name || record.Stage != stage ||
		model.ValidateName(record.Model) != nil || !validFiles(record.Files, record.ModelSHA256) {
		return false
	}
	for _, unit := range record.Accepted {
		if !slices.Contains(plan.RoundUnits(stage), unit.Unit) || model.ValidateName(unit.Model) != nil {
			return false
		}
	}
	return true
}

// Action is the next thing one identity should do in a run.
type Action struct {
	Kind    string // "done", "idle", "claim", "contribute", or "close"
	Round   int
	Stage   string
	Unit    string // the unit to train, or "close"
	Slot    string // the slot to claim
	Attempt int
}

// expired is whether a claim's holder has had its claim_minutes to land its record.
func (state State) expired(claim Claim, now time.Time) bool {
	created, err := time.Parse(time.RFC3339, claim.CreatedAt)
	return err != nil || now.After(created.Add(time.Duration(state.Plan.ClaimMinutes*float64(time.Minute))))
}

// filled is whether any holder of slot has landed its record.
func (round Round) filled(slot string) bool {
	unit := slotUnit(slot)
	for _, claim := range round.Claims[slot] {
		if unit == "close" && slices.ContainsFunc(round.Merges, func(record MergeRecord) bool { return record.Identity == claim.Identity }) {
			return true
		}
		if slices.ContainsFunc(round.Submissions[unit], func(record Submission) bool { return record.Identity == claim.Identity }) {
			return true
		}
	}
	return false
}

// replicas are a unit's submissions in slot order, one per slot from its
// earliest holder who submitted, so every closer judges the same set even when
// a slot's expired holder submits after the attempt that replaced it.
func (round Round) replicas(unit string, count int) []Submission {
	var result []Submission
	for position := 1; position <= count; position++ {
		for _, claim := range round.Claims[fmt.Sprintf("%s-%d", unit, position)] {
			index := slices.IndexFunc(round.Submissions[unit], func(record Submission) bool { return record.Identity == claim.Identity })
			if index >= 0 && !slices.ContainsFunc(result, func(record Submission) bool { return record.Identity == claim.Identity }) {
				result = append(result, round.Submissions[unit][index])
				break
			}
		}
	}
	return result
}

// holds is whether identity holds a claim on a slot of unit.
func (round Round) holds(identity, unit string) bool {
	for slot, claims := range round.Claims {
		if slotUnit(slot) == unit && slices.ContainsFunc(claims, func(claim Claim) bool { return claim.Identity == identity }) {
			return true
		}
	}
	return false
}

// open is the slot's next attempt if it can be claimed now: never claimed,
// or its latest holder let the claim expire without landing a record.
func (state State) open(round Round, slot string, now time.Time) (int, bool) {
	claims := round.Claims[slot]
	if len(claims) == 0 {
		return 1, true
	}
	if round.filled(slot) || !state.expired(claims[len(claims)-1], now) {
		return 0, false
	}
	return len(claims) + 1, true
}

// Next picks this pass's work. A full (or late) round is closed by its claimed
// closers: the plan's closers, then one more for each that disagrees until a
// majority signs one merge. Otherwise an identity trains the unit it claimed,
// else claims a replica slot of the lowest unit it has not taken part in.
func (state State) Next(identity string, now time.Time) Action {
	stages := state.Plan.Stages()
	number := len(state.Rounds) - 1
	if number < 0 {
		number = 0
	}
	if number < len(state.Rounds) && state.Rounds[number].Merge != nil {
		number++
	}
	if number >= len(stages) {
		return Action{Kind: "done", Round: len(stages) - 1}
	}
	stage, units := stages[number], state.Plan.RoundUnits(stages[number])
	round := Round{Claims: map[string][]Claim{}, Submissions: map[string][]Submission{}}
	if number < len(state.Rounds) {
		round = state.Rounds[number]
	}
	full := 0
	for _, unit := range units {
		if len(round.replicas(unit, state.Plan.Replicas)) >= state.Plan.Replicas {
			full++
		}
	}
	late := false
	if state.Plan.RoundMinutes > 0 && number > 0 {
		late = now.After(state.Rounds[number-1].ClosedAt.Add(time.Duration(state.Plan.RoundMinutes * float64(time.Minute))))
	}
	if full == len(units) || (late && full > 0) {
		if slices.ContainsFunc(round.Merges, func(record MergeRecord) bool { return record.Identity == identity }) {
			return Action{Kind: "idle", Round: number, Stage: stage}
		}
		if round.holds(identity, "close") {
			return Action{Kind: "close", Round: number, Stage: stage, Unit: "close"}
		}
		// Past the plan's closers, a slot opens only once every one before it has a merge and none has a majority.
		everyFilled := true
		for position := 1; position <= state.Plan.Closing()+len(state.Plan.Identities); position++ {
			slot := fmt.Sprintf("close-%d", position)
			if position > state.Plan.Closing() && !everyFilled {
				break
			}
			if attempt, ok := state.open(round, slot, now); ok {
				return Action{Kind: "claim", Round: number, Stage: stage, Unit: "close", Slot: slot, Attempt: attempt}
			}
			everyFilled = everyFilled && round.filled(slot)
		}
		return Action{Kind: "idle", Round: number, Stage: stage}
	}
	submitted := func(unit string) bool {
		return slices.ContainsFunc(round.Submissions[unit], func(record Submission) bool { return record.Identity == identity })
	}
	for _, unit := range units {
		if !submitted(unit) && round.holds(identity, unit) {
			return Action{Kind: "contribute", Round: number, Stage: stage, Unit: unit}
		}
	}
	for _, unit := range units {
		if submitted(unit) || round.holds(identity, unit) || len(round.replicas(unit, state.Plan.Replicas)) >= state.Plan.Replicas {
			continue
		}
		for position := 1; position <= state.Plan.Replicas; position++ {
			slot := fmt.Sprintf("%s-%d", unit, position)
			if attempt, ok := state.open(round, slot, now); ok {
				return Action{Kind: "claim", Round: number, Stage: stage, Unit: unit, Slot: slot, Attempt: attempt}
			}
		}
	}
	return Action{Kind: "idle", Round: number, Stage: stage}
}
