// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Round is what the repo records for one round: its verified submissions by unit, and its merge once closed.
type Round struct {
	Merge       *MergeRecord
	Submissions map[string][]Submission
}

// State is a run as its signed records describe it; unsigned or misplaced records are ignored.
type State struct {
	Plan   Plan
	Rounds []Round
}

func roundDir(runDir string, number int) string {
	return filepath.Join(runDir, "rounds", fmt.Sprintf("round-%04d", number))
}

// ReadState reads every round's records under runDir, verifying each signature against the plan's keys.
func ReadState(runDir string, plan Plan, allowedSigners string) (State, error) {
	state := State{Plan: plan}
	for number := range plan.Stages() {
		directory := roundDir(runDir, number)
		round := Round{Submissions: map[string][]Submission{}}
		mergePath := filepath.Join(directory, "merge.yaml")
		if _, err := os.Stat(mergePath); err == nil {
			var record MergeRecord
			ok, err := readSigned(mergePath, allowedSigners, plan.Identities, &record, func() string { return record.Identity })
			if err != nil {
				return State{}, err
			}
			// A bad merge record would fork the run, so nobody builds on it.
			if !ok || record.Round != number {
				return State{}, fmt.Errorf("%s is not a valid merge record; the run needs a fix before anyone continues", mergePath)
			}
			round.Merge = &record
		}
		paths, err := filepath.Glob(filepath.Join(directory, "submissions", "*.yaml"))
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
			// A record must match the file it is in, so none can be replayed elsewhere.
			if ok && record.Round == number && filepath.Base(path) == record.Unit+"-"+record.Identity+".yaml" {
				round.Submissions[record.Unit] = append(round.Submissions[record.Unit], record)
			}
		}
		state.Rounds = append(state.Rounds, round)
		if round.Merge == nil {
			break
		}
	}
	return state, nil
}

// Action is the next thing one identity should do in a run.
type Action struct {
	Kind  string // "done", "idle", "contribute", or "close"
	Round int
	Stage string
	Unit  string
}

// Next picks this pass's work: close a full (or late) round, else train the
// lowest unit still short of replicas that this identity has not submitted.
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
	submitted := map[string][]Submission{}
	if number < len(state.Rounds) {
		submitted = state.Rounds[number].Submissions
	}
	full := 0
	for _, unit := range units {
		if len(submitted[unit]) >= state.Plan.Replicas {
			full++
		}
	}
	late := false
	if state.Plan.RoundMinutes > 0 && number > 0 {
		if closed, err := time.Parse(time.RFC3339, state.Rounds[number-1].Merge.ClosedAt); err == nil {
			late = now.After(closed.Add(time.Duration(state.Plan.RoundMinutes * float64(time.Minute))))
		}
	}
	if full == len(units) || (late && full > 0) {
		return Action{Kind: "close", Round: number, Stage: stage}
	}
	for _, unit := range units {
		if len(submitted[unit]) >= state.Plan.Replicas {
			continue
		}
		mine := false
		for _, record := range submitted[unit] {
			mine = mine || record.Identity == identity
		}
		if !mine {
			return Action{Kind: "contribute", Round: number, Stage: stage, Unit: unit}
		}
	}
	return Action{Kind: "idle", Round: number, Stage: stage}
}
