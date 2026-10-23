# Distributed training with `waldo model join`

`waldo model join` lets many machines train one model with no coordinator. A
run lives in a git repository: a plan, plus signed records of every unit of
work. Each contributor runs the same command; each pass reads the repository,
does the next piece of work, and commits its record. Every model a run produces
traces back to the corpora it was trained on. Design:
[ADR 0088](adr/0088-distributed-training-through-signed-records.md).

```bash
waldo model join <runs-repo> <run-dir> --identity <name> [--once] [--email <address>] [--key <path>] [--wait <seconds>]
```

`--once` does one piece of work and exits; otherwise join loops until the run is
complete, waiting `--wait` seconds (default 60) while others work. An identity
keeps its signing key, optional `github-token`, clone, models, and exports in
`~/.waldo/join/<identity>/`; a new identity is given its key and the
`identities` entry to add to the plan.

## Runs repository

```
runs/<run>/
  plan.yaml, *.yaml.tmpl                 # the plan and its compose templates
  rounds/round-NNNN/
    claims/<slot>[.<attempt>].yaml(.sig) # slot: <unit>-<n> or close-<n>
    submissions/<unit>-<identity>.yaml(.sig)
    merges/<identity>.yaml(.sig)
```

| Plan key | Meaning |
| --- | --- |
| `kind`, `schema` | `waldo-join-plan`, `1`. Unknown keys are rejected. |
| `name`, `units` | Run and unit names: lowercase letters, digits, `.`, `_`, `-`. |
| `replicas` | Identities that train each unit per round. |
| `closers` | Identities that close each round; a majority must agree. Default 3. |
| `claim_minutes` | How long a claim holds its slot. Default 60. |
| `merge`, `store` | `mean`; `huggingface` or `file:///path`. |
| `identities` | Name to `{key: <ssh public key>, hf: <Hugging Face user>}`. |
| `pretrain` | `compose`, `bootstrap_tokens`, `rounds`, `tokens_per_round`, and `shards` (unit to sources). |
| `posttrain` | `compose`, `corpus` (an index path each unit takes a `filter.slice` of), `rounds`, `epochs_per_round`. |
| `replica_tolerance`, `replica_floor` | Agreement thresholds; defaults 0.25 and 0.001. |
| `min_steps_per_round`, `round_minutes` | Minimum steps per unit; optional round deadline. |

Templates are composes with `$base_block`, `$seed`, `$budget`, `$sources`,
`$corpus`, `$learning_rate`, `$weight_decay`, and `$extra` placeholders.

## Rounds

Round 0 trains one shared unit, `all`, on every unit's sources; pretraining and
post-training rounds follow, each starting from the previous round's merge. A
unit's seed hashes the run, round, and unit, so its replicas train identically.
Each pass, an identity:

1. **Claims a slot**, then does its work. A claim is a fixed file, so two claims
   on one slot conflict in git and the first stands. A claim not followed by its
   record within `claim_minutes` may be retaken as the slot's next attempt.
2. **Trains** a claimed unit: exports it, publishes it, and records every file's
   SHA-256, the tokens trained, and the round's starting weights.
3. **Closes** a full round (or a late one, past `round_minutes`) from a claimed
   closing slot: judges every unit's replicas, merges the accepted units by
   token-weighted mean (`base.merge`, [ADR 0084](adr/0084-merge-models-with-a-common-ancestor.md)),
   publishes the merge, and signs it.

Every record pins the `plan.yaml` it was made under by SHA-256 and counts only
under that plan, so editing a plan starts the run afresh. Records count only
from identities holding a claim, one slot per identity per unit. Each unit's replicas are taken one per slot, so every closer judges the
same set.

### Judging replicas

A closer fetches every replica and checks each against its record's hashes, then
sets aside, one replica at a time:

- one that did not start from the round's merged weights;
- every replica in a group with identical weights (one copied another);
- one whose tokens trained differ from the majority's;
- one whose training provenance does not check out (below);
- one whose run BOM records a corpus selection other than its unit's;
- one whose run trained with other parameters than the majority's, or another
  step count than the plan gives the unit.

The rest are measured by the closer itself, with `--evaluate-only` on the
records the unit's training held out
([ADR 0087](adr/0087-score-weights-without-training.md)). Honest replicas improve
alike from the round's starting loss, and a free-rider does not; two agree when
their gap is within `replica_tolerance` of the round's progress, or under
`replica_floor`. The replica most others agree with is accepted. A unit needs a
majority of its planned replicas left to be judged at all.

The merge record lists accepted units, dropped units and rejected replicas with
reasons, the evidence (starting loss, held-out set, corpus selection, every
replica's loss), and the merged package's hashes.

### Majority closing

Each of `closers` identities judges and merges the round on its own. The round
is decided by the first decision, in slot order, that a majority of closing
slots sign: the starting weights, accepted units, method, merged weights, and
published provenance files. One slot is one vote, however often it is retaken.
Measured losses can differ across hardware; they are evidence, not the decision.
If every slot has voted without a majority, another closing slot opens. A
decided round never changes, and each merge must start from the decision before
it. Dissenting merges stay on record and are reported when the run completes.

### Provenance

Each submission publishes its run BOM (`provenance-<sha256>.json`: index commit,
shards, record filter) and its `TRAINING-CLOSURE.json`, every corpus its whole
lineage trained on ([ADR 0083](adr/0083-record-lineage-and-training-provenance.md)).
Closers rebuild what that closure must be (the round base's closure plus the
submission's own run BOMs) and reject a submission whose closure leaves out or
adds a corpus, or whose history is incomplete. A merge republishes every
accepted unit's run BOM and the union of their closures, and the majority's
decision covers those files, so the final model's `EU-BOM.json` and
`ATTRIBUTION.md` cover every round's corpora. Each merge also publishes
`JOIN-ROUND.json`, its link back to the run: the runs repository, run, round,
plan hash, and every accepted submission's signed record by path and hash;
pulls keep it, and exporting unchanged weights republishes it. Every node imports network models
from their published packages, so all record the same lineage.

### Choosing replicas

Two replicas catch a free-rider while a round's progress is well above
`replica_floor`; below it, a free-rider is indistinguishable from honest work,
so spend compute on new data instead. Three replicas outvote one bad
submission. Each merge's evidence shows where replication stopped verifying.

## Stores

`file:///path` keeps `<path>/<run>/<model>/` on one machine. `huggingface`
uploads each submission or merge to the identity's own account, pinned in its
record as `huggingface://<hf>/<model>@<commit>`; readers fetch exactly those
files, check every hash, and refuse a URL outside the identity's account.

## GitHub

On GitHub, each record goes to its own branch, on the runs repository or the
identity's fork, as a pull request merged by the repository's own verifier
Action. The verifier merges only record files under an existing run's
`rounds/`, applies the same validity and claim rules join does, rejects a claim
dated ahead of its clock, and never runs code from a pull request.

Fine-grained tokens cannot open pull requests on repositories their owner does
not control, so contributors need a classic `public_repo` token, best held by a
machine account, in `~/.waldo/join/<identity>/github-token` (private file,
private directory; [ADR 0035](adr/0035-local-s3-credentials.md)). Join uses about
one API call per record: it watches for its record with `git fetch`, checks the
pull request only every five minutes, and waits out a rate limit rather than
failing.

## Testing

`testing/e2e/model-join.sh` (opt-in, `WALDO_E2E_JOIN=1`) trains three identities
for real through a local runs repository: one free-rides, another closes
dishonestly, and the test asserts both are caught. `testing/join/run-identities.sh`
burns in a GitHub run, and with `FAULTS`, `testing/join/burnin.py` injects
protocol faults; `burnin.py report runs/<run>` checks what the run did with each.
A new bug becomes a new fault.

## Limits

- A majority of colluding closers, or of a unit's replicas, is not defended
  against; the plan's identity list bounds who can vote.
- A lightly perturbed copy of another replica passes, since replicas publish
  weights as they submit; it earns credit without work and makes the replicas'
  agreement less independent. Commit-reveal would stop it (each replica commits
  its weights' hash, and all reveal once a unit's replicas have committed); it is
  a known gap, not built while runs are proofs of concept.
- The plan's author is trusted to write the plan; records pin it.
- A runs repository not on GitHub has no verifier; join's own checks still apply.
