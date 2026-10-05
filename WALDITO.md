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
- A round closes when any `join` sees its deadline passed; claimed closers merge
  independently and must agree on the hash. If nobody is online, the round closes when someone returns.
- `waldo model build <plan>` is the one-machine mode: every unit done locally, to test a plan
  before asking others for compute. It shares the code path with `join`.

## `waldito join` v1 design

Built with a local git repo and a `file://` store: `testing/e2e/waldito-join.sh` runs two
identities through a bootstrap, a pretrain and a post-train round, with a free-rider's unit
dropped in round 1. Next: the GitHub and Hugging Face paths below.

- `waldito join <git-url> <run-dir> --identity alice [--once]`: each identity gets its own clone
  under `~/.waldo/waldito/join/<identity>/`, so several contributors can run on one Mac.
  `--once` does one unit and exits (tests, cron); without it, loop until the run is done.
- Plan keys beyond build's: `identities` (name -> ssh signing key and Hugging Face user),
  `replicas`, `units` (named data shards; `pretrain.shards` is keyed by them), `store`,
  optional `round_minutes` deadline.
- Round 0 is the bootstrap: one unit on all data, replicated; its accepted replica is the
  round-1 base, so the start is verified like any round.
- Next unit: the lowest unit in the open round with fewer than `replicas` submissions that this
  identity has not submitted. A race just yields an extra replica.
- A round closes when every unit has its replicas (or the deadline passed with at least one
  accepted unit): the closer probes, votes, merges, publishes, and submits `merge.yaml`.
- Records are JSON-compatible YAML signed with `ssh-keygen -Y sign`, committed `-s` with the
  identity as author. Records that don't verify against the plan's keys are ignored.

### Records arrive as pull requests

- The runs repo is public; contributors need no write access. `join` commits each record to the
  contributor's fork and opens a PR (`gh`). A PR is a transaction id: opened, checked, merged,
  branch deleted, all unattended. The commit on `main` and its signed file are the record.
- A GitHub Action is the stateless verifier. It auto-merges a PR only if it adds nothing but
  record files under an existing run's `rounds/`, each file matches the identity it names, its
  signature verifies against the plan's key, and its round has no `merge.yaml` on `main` yet.
  It reads files and runs `ssh-keygen`; it never executes PR code. Anyone can rerun the same
  checks, which are the ones `join` applies when reading.
- Rebase-merge, so each contributor's signed-off commit lands on `main` with them as author.
- `main` orders everything: the first `merge.yaml` merged for a round stands, a later one is
  rejected, and its closer compares hashes (the merge is deterministic, so they should match).
- Plan changes (new runs, adding yourself to `identities`) are ordinary PRs reviewed by the
  run's organizer via CODEOWNERS; the Action never merges them.
- Cost: seconds per check, and Actions are free on public repos. About units x replicas + 1 PRs
  per round (~100 at 50 Macs); bursts queue behind the 20 concurrent jobs and `join` waits.
  Organizers watch the repo as "Participating", not "All activity".
- Batching records per PR would cut PR count but delay visibility, which unit assignment needs;
  left out until volume requires it.

### Automation account (next to set up)

`python-basics-v1` opened 129 PRs from the organizer's own account, and every PR plus every
merged commit authored with a linked email lands on that account's contribution graph. Records
should come from a machine account instead: GitHub allows one per person, for automation only.

- Create `mdagosta-bot` (a plus-address email works), add it as a Write collaborator on the runs
  repo, and accept the invite as the bot.
- Token: a classic token with only `public_repo`. A fine-grained token only reaches repos its own
  account owns, so it cannot open PRs on a repo owned by another personal account; the bot has
  access to nothing else, so the broader scope exposes nothing.
- Run as the bot without touching the personal `gh` login: `GH_TOKEN=<bot token> waldito join ...
  --email mdagosta-bot@users.noreply.github.com`. The noreply email keeps commits off the
  personal graph too.
- Identity is unchanged: records are still signed with each identity's key and weights still go
  to its Hugging Face account. One bot can carry several identities, as in the testbed.
- Untested: that `gh auth git-credential` (used for `join`'s pushes) honors `GH_TOKEN`. Planned:
  `join --github-token-file`, so the token never appears on a command line.
- Contributions already on the personal graph stay; they only go away with the repo.

### Weights on Hugging Face

- Each identity uploads to its own Hugging Face account, one model repo per submission or merge:
  `huggingface://<user>/<model>@<commit>`. No shared org is needed.
- Records carry the commit revision and a `files` map of every published file's sha256; readers
  download exactly those files at that revision and check each hash before using any of them.
  `file:///path` stores (`<store>/<run>/<model>/`) stay for tests.
- A store that cannot be read is retried, then the pass stops without committing: a network
  failure never becomes a verdict. Only a complete download whose hashes differ counts against
  a replica, since that is what the store serves at the pinned commit.
- Storage is finite (8.8 TB per account here) but not a concern until full scale.
  `python-basics-v1` uploads ~19 MB per model, ~133 MB per round: 24/7 on one Mac fills it in
  ~5 years of pretraining or ~1 year of post-training. At milestone 4 (~400 MB per model), one
  Mac submitting ~4 replicas an hour fills its account in ~230 days.
- Later: delete a round's submissions from Hugging Face once its merge is committed (after an
  optional audit window), keeping only merges. Records keep every hash, so attribution
  survives; only re-checking an old replica's bytes is lost. Cuts storage ~99% at milestone 4.

### Claims: nobody does the same work twice (next to build)

Today two identities that both see a free unit both train it, and every joiner that sees a full
round closes it; only the first record lands. Claims make the repo a lock on the work itself.

- Before any unit, `join` opens a signed claim PR and waits for it to merge. Claims live in
  `rounds/round-NNNN/claims/`, one file per slot: `<unit>-<replica>.yaml` for training
  (`u3-1.yaml`, `u3-2.yaml`) and `close-<n>.yaml` for closing.
- Slots have fixed names, so two claims for one slot conflict and the verifier closes the second.
  The loser takes the next free slot, or another unit. Only the winner trains or closes.
- Next unit: the lowest unit with a free replica slot that this identity has not claimed; the
  open round's close slots come first once every unit has its submissions.
- A claim expires if its submission hasn't landed within the plan's `claim_minutes` (default
  2x the plan's expected unit time). A new attempt, `<slot>.<attempt>.yaml` (`u3-1.2.yaml`), is
  accepted only once the previous attempt has expired and its slot has no submission.
- The verifier judges expiry by GitHub's clock, not the claimer's: it rejects a claim whose
  `created_at` is more than 5 minutes from its own time, so nobody can backdate one.
- Cost: one more PR round trip (~15-60 s) per unit, about twice the PRs per round (~200 at 50
  Macs). Small next to the hours a duplicate training unit wastes.

### Replicated closing (next to build)

A round's merge is the next round's base, so it is verified like training: by independent
replicas, not by trusting whoever closed first.

- Closing is a unit with `close_replicas` slots (default 2). Each closer fetches every
  submission, probes, votes, merges, and submits `rounds/round-NNNN/merges/<identity>.yaml`.
- The round is closed once `close_replicas` merge records agree on the accepted set, the dropped
  set, and the merged `files` hashes. The mean in a fixed order is deterministic: in `smoke-v1`,
  two identities closed round 1 independently and got the same merged hash.
- Disagreeing merge records flag the round instead of closing it: nobody builds on it until more
  closers settle it or the organizer amends the plan. Probes are not bit-reproducible, so honest
  closers can split on a replica pair right at `replica_tolerance`; that is a flag, never a
  silent accept.
- Cost: `close_replicas` downloads of the round's submissions (~40 GB per closer at 100 units of
  ~200M parameters), not one per contributor. Everyone else checks the agreeing hashes only.
- The verifier accepts `merges/*.yaml` under the same checks as submissions, one per identity.
  `merge.yaml` stays readable for runs recorded before this.

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

- A run is a voyage: contributors embark on one plan together, from a pinned start to a
  final merge. The product is that lineage, not its pieces. "Build flask" is never done on
  its own; flask trained from another start, mix, or round is a different, unmergeable flask.
- Mergeable = same tokenizer + same architecture + shared ancestor weights, synced each
  round. Matching a compose yaml alone gives the same shape, not compatible weights; building
  on another model (e.g. a waldo foundation) needs its published checkpoint and tokenizer.
- One shared pretrain run per mix (e.g. Python + TypeScript + Go): vscode chunks, django,
  flask, thefuck are all units in the same rounds. Result: the general base.
- Add data later by continuing the run: a new plan whose round-1 base is the last merge,
  with some earlier data replayed. Post-training is then replayed on the new base.
  Not built: a plan `base:` key (HF URL @commit, sha256, `from:` round) that replaces the
  bootstrap, so e.g. python-basics -> full stack skips the original pretraining.
- Separately finished pretrains are not averaged together; they drift apart.
- Specialists (vscode, your company's code) are fine-tunes of the shared base, run locally.
- Combining specialists = routing (mixture of experts, FlexOlmo-style), a later step.

## Milestones

1. Done: network-built model matches single-machine training at equal steps.
2. Testbed on `mdagosta/waldo-builds`: two identities, `replicas: 2`, one injected disagreement
   (done: `smoke-v1`, then `python-basics-v1` at 372/374 and 220/220). Next: records from an
   automation account (see above), then a round with 3-5 real people.
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
