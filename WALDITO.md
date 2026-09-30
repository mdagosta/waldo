# waldito network plan

Goal: pretrain open code models on waldo-indexed data with many small machines, every
contribution verified and attributed, with no server or coordinator of its own.

## What exists (2026-09-29)

`waldito build <plan>` runs every role of the network on one Mac. The `waldito-python-basics`
build: 3 contributors x 3 replicas, 4 pretrain + 10 post-train rounds, merged every round, a
free-rider rejected in round 2 (28% worse on the verifier's probe). Result: 371/374 held-out and
217/220 out-of-distribution, matching one model trained on all the data (367/374, 211/220) at
equal sequential steps.

## Runs repo: one directory per training run

Code and run history live in separate repos. The testbed is `mdagosta/waldo-builds`
(code stays on `mdagosta/waldo` `md/waldito`); an official home could later be
`openwaldo/waldo-builds`.

```
runs/py-ts-go-v1/
  plan.yaml                 # data mix, units, rounds, seeds, rules, allowed signing keys
  rounds/round-0007/
    submissions/*.yaml      # signed, signed-off records: sha256 + huggingface://...@commit
    merge.yaml              # the round's accepted set and merged-weights hash
```

- A new run is a PR adding `runs/<name>/plan.yaml`; nobody creates a repo per plan.
  CODEOWNERS lets each run's organizer review its own plan amendments.
- `join` takes any git URL (GitHub, Codeberg, a local path). Official repos or a registry
  are for finding runs, never a gate.
- Identity is a signing key listed in the plan, not a git host account: one account with
  several keys can test several contributors.
- The repo is the whole state, and its history is the record of how the model was built.
  Weights live anywhere addressed by sha256 (Hugging Face today); the repo can be mirrored.

## `join` is the only command

A contributor installs waldo, logs in to GitHub and Hugging Face once, and runs:

```
waldito join https://github.com/mdagosta/waldo-builds runs/py-ts-go-v1
```

(later `waldo model join`, same arguments)

for as long as they like. Each pass reads the repo, does the next unit the rules call for,
commits its signed record, and repeats. Nothing else runs anywhere; no process coordinates.

| unit | when | output | checked by |
| --- | --- | --- | --- |
| train | round open | weights, run BOM | replicas' probe losses agree |
| probe | submissions awaiting verification | held-out loss | several verifiers agree |
| merge | round deadline passed | accepted set + merged weights | recomputing it gives the same hash (CPU merge is deterministic) |

- A contributor's unit: hash(identity, round) mod units. Overlaps become replicas.
- `replicas: 2` is the smallest verifying plan: agreeing replicas are accepted; disagreeing
  ones are both dropped for that round (no majority to trust) and the merge proceeds without
  that unit. Three replicas can instead outvote one bad submission.
- A round closes when any `join` sees its deadline passed; whoever gets there first merges,
  later joiners confirm the hash. If nobody is online, the round closes when someone returns.
- `waldo model build <plan>` is the one-machine mode: every unit done locally, to test a plan
  before asking others for compute. It shares the code path with `join`.

## Local files

- Everything a run creates is named `waldito-<run>-r<round>-<unit>-<role>` (contributor
  runs, exports, probes, pulled merges), so it stands apart in `~/.waldo/models`.
- `join` records each model it creates in `~/.waldo/waldito/runs/<run>/created.txt`; cleanup
  deletes only what is listed there, never by prefix (older personal models may share it).
- After a round's merge is committed, that round's local files are deleted automatically:
  submissions are on Hugging Face, verdicts are in the repo, and the next base can be re-pulled
  by hash. Only the current base and the unit in progress stay. At ~200M parameters a round
  leaves several GB per contributor, so this is required, not tidying.
- `waldito status` lists the runs this machine is in and their disk use; `waldito clean <run>`
  removes a run's local files, e.g. after leaving it.
- Disagreements need nothing local: dropped units are recorded in `merge.yaml`, and a run gone
  wrong is replaced by a new run whose round-1 base is the last good merge.

## Rules learned

- Shard the data, not the skills. Split-by-skill failed with every merge (0-7/334).
- Merge every round. Merge-once lost to every-round in pretraining (3.55 vs 3.39).
- Match sequential steps: 5 post-train rounds scored 349/374, 10 rounds scored 371/374.
- Enough steps per round: 72 failed, ~200+ worked. More contributors need more data or
  more rounds, not finer splits.
- Units are equal work (fixed steps), not whole repos: split big repos (vscode 3-of-12),
  group small ones, weight merges by tokens.
- Verify behavior, not weights: GPU training is not bit-reproducible. Judge replica gaps
  against the round's improvement (lazy = zero improvement). Loses power as training converges.
- Plain mean beat DiLoCo momentum at 5 rounds, and a lower final-round learning rate or a
  post-merge consolidation step did not help.

## Composing models

- One shared pretrain run per mix (e.g. Python + TypeScript + Go): vscode chunks, django,
  flask, thefuck are all units in the same rounds. Result: the general base.
- Add data later by continuing the run: a new plan whose round-1 base is the last merge,
  with some earlier data replayed. Post-training is then replayed on the new base.
- Separately finished pretrains are not averaged together; they drift apart.
- Specialists (vscode, your company's code) are fine-tunes of the shared base, run locally.
- Combining specialists = routing (mixture of experts, FlexOlmo-style), a later step.

## Milestones

1. Done: network-built model matches single-machine training at equal steps.
2. Testbed on `mdagosta/waldo-builds`: two identities (own account + an automation account),
   `replicas: 2`, one injected disagreement; then a round with 3-5 real people.
3. `waldito join <git-url>` first, reusing waldito's working pieces (merge, probe) plus git and
   real Hugging Face uploads, so nothing is needed in waldo yet. `waldo model join` comes later.
4. Python + TypeScript + Go, ~200M parameters, ~4B tokens (~2 days on 50 Macs, ~6 h on one H100).
5. All of code/permissive, ~400M parameters, with vscode as ~12 units.

## Needed in waldo (proposal to maintainers)

Small, narrow patches first (`lookaside.cache.keep`, the absolute-path corpus-weights bug,
chat on pulled models, `model.gpu-throttle` config, skipping the checkpoint at a run's final
step since the terminal weights supersede it, which cuts a run's writes from ~91 MB to ~36 MB here
and leaves no dead optimizer state; interim checkpoints stay for resuming); the larger items once
`waldito join` works. Plans should set `checkpoint_every` by time per unit, not rely on the
500-step default.

- Merge stage in compose (N pinned parents, mean or DiLoCo).
- Deterministic record sharding in compose filters.
- `model eval` (in progress upstream).
- Chat on pulled models; publish weights by hash.
- Later: `model join` / `model build`, and deterministic training (makes verification exact).

## Open problems

- Identity: one person posing as five. Signed identities + an allow-list in the plan.
- Backdoors: changes that keep probe loss intact are not caught by replicas.
- Cost of redundancy: 3 replicas = 3x compute; spot-checking may be enough.
