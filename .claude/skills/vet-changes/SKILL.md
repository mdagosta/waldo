---
name: vet-changes
description: Vet a branch's changes with three independent review agents (code review, adversarial, compatibility with code and data outside the diff), verify their findings, and report only what holds up.
disable-model-invocation: true
argument-hint: "[worktree path, branch, or commit range; default: current branch vs origin's default branch]"
---

# Vet changes

The user stands behind every change they send upstream, so the goal is findings they can trust and
understand, not volume. Nothing changes on the branch during vetting: report, then let the user decide.

## 1. Scope (no questions)

- The range: the argument, or `origin/<default>...HEAD` after `git fetch origin`. Note the commits and `git diff --stat`.
- What the change is for, in two lines, from the commit messages, any ADRs in the diff, and the branch name.
- The repo's rules: `AGENTS.md`, contribution guide, and the contracts the diff touches under `docs/`.

## 2. Three agents, in parallel (one message, three Agent calls)

Give each the repo path, the range, the two-line purpose, and these rules: read-only on the branch's
worktree; any experiment (a throwaway test, a build) happens in a fresh `git worktree add --detach` under
the scratchpad, removed afterward; every finding names file:line and a concrete failure scenario
(inputs or state → wrong result), or it is not a finding. About 40 tool calls each.

1. **Reviewer**: correctness, error handling, tests for observable behavior, and whether the code reads
   like the code around it (naming, comment density, idiom). Flags repetition and defenses against
   things that can't happen.
2. **Adversary**: tries to break it. Malformed or hostile inputs, partial failures, retries and
   interruptions, ordering, concurrency, and, for provenance or trust code, any way to make the system
   accept something it should refuse or record something untrue. Writes and runs a failing test when it can.
3. **Compatibility**: how the change fits everything it doesn't touch, in both directions. Downstream:
   every caller and consumer of changed functions, types, files, and formats; CLI behavior, docs, ADRs,
   and other packages or repos that read them. Upstream and over time: durable formats (BOMs, records,
   manifests) and state written by earlier versions, such as interrupted work, that the new code must
   still accept, and what older versions do with what the new code writes.

## 3. Verify before reporting

For each finding, check it yourself: read the cited code, reproduce it, or run the agent's test. Drop what
doesn't hold up, merge duplicates, and mark each kept finding confirmed (reproduced) or plausible (read, not run).

## 4. Report at the user's pace

- First, one line per agent on what it looked at.
- Then findings ranked most severe first: what breaks, how to trigger it, file:line, and a suggested fix
  in a sentence. Show at most five at a time and offer the rest.
- Say plainly what was clean.
- Fix nothing until the user picks; they commit.
