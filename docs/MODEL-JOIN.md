# Distributed training with `waldo model join`

`waldo model join` lets many machines train one model together without a
coordinator. A training run lives in a git repository: its plan names the data,
rounds, and contributors, and every unit of work lands as a signed record. Each
contributor runs the same command; each pass reads the repository, does the
next unit the plan calls for, and commits its record. The repository's history
is the record of how the model was built, and every model it produces traces
back to the corpora it was trained on.

`model join` is the asynchronous, untrusted-contributor counterpart to
[multi-host training](MULTI-NODE-TRAINING.md): contributors train separate
units and the run merges them every round, instead of nodes stepping together
through all-reduce.

## Command

```bash
waldo model join <runs-repo> <run-dir> --identity <name> [--once] [--email <address>] [--key <path>] [--wait <seconds>]
```

| Flag | Meaning |
| --- | --- |
| `--identity` | The contributor's name under the plan's `identities`. Required. |
| `--once` | Do one unit of work and exit. Without it, join loops until the run is merged, waiting `--wait` seconds (default 60) between checks while others work. |
| `--email` | Commit author email for records. Default `<identity>@waldo.invalid`. |
| `--key` | SSH signing key. Default `~/.waldo/join/<identity>/signing-key`, created on first use. |

`<runs-repo>` is any git URL or local path; `<run-dir>` is the run's directory
in it, such as `runs/python-basics-v1`. An identity not yet in the plan gets a
signing key and the exact `identities` entry to add.

Each identity keeps everything in `~/.waldo/join/<identity>/`: its signing
key, an optional `github-token`, its clone of the runs repository, its own model
store and WALDO configuration, and its exports. Several identities can share
one machine, and retiring one removes one directory.

## Runs repository

```
runs/<run>/
  plan.yaml                      # data, units, rounds, identities, store
  pretrain.yaml.tmpl             # compose template filled for each unit
  sft.yaml.tmpl                  # optional post-training template
  rounds/round-NNNN/
    submissions/<unit>-<identity>.yaml(.sig)
    merge.yaml(.sig)
```

### Plan

| Key | Meaning |
| --- | --- |
| `name` | Run name, used in record and model names. |
| `units` | Named units of data. Every round trains each unit. |
| `replicas` | How many identities train each unit per round. `2` is the smallest verifying plan. |
| `merge` | `mean`. Accepted units are merged by token-weighted mean. |
| `store` | `huggingface` (each identity's own account) or `file:///path` (one machine, tests). |
| `identities` | Name to `{key: <ssh public key>, hf: <Hugging Face user>}`; a bare string is just the key. |
| `pretrain.compose` | Template for the bootstrap and pretraining rounds. |
| `pretrain.bootstrap_tokens` | Token budget of round 0, which trains one shared unit, `all`, on every unit's sources. |
| `pretrain.rounds` | Pretraining rounds after the bootstrap. |
| `pretrain.tokens_per_round` | Each unit's per-round budget, converted to fixed steps with repetition allowed. |
| `pretrain.shards` | Unit to source names; each unit's compose includes only its sources. |
| `posttrain.compose` | Optional post-training template. |
| `posttrain.corpus` | Index path that post-training units divide: each trains its own `filter.slice` of it. |
| `posttrain.rounds`, `posttrain.epochs_per_round` | Post-training rounds and each unit's per-round budget. |
| `replica_tolerance` | Largest replica loss gap, as a share of the round's progress, still counted as agreement. Default 0.25. |
| `replica_floor` | Absolute gap below which replicas always agree, for converged rounds. Default 0.001. |
| `min_steps_per_round` | Refuse a unit that trained fewer optimizer steps. |
| `round_minutes` | Optional deadline after which a round may close with the units it has. |

Templates are WALDO composes with `$name` placeholders: `base_block`, `seed`,
`budget`, `sources`, `corpus`, `learning_rate`, `weight_decay`, and `extra`
(`$$` is a literal `$`).

## Rounds

Round 0 is the bootstrap; then come the pretraining rounds, then the
post-training rounds. Each round starts from the previous round's merged weights.
Every pass picks its work from the records on the default branch:

1. When every unit has its replicas, or the round is past its deadline with at
   least one full unit, close the round.
2. Otherwise train the lowest unit that is short of replicas and that this
   identity has not submitted.
3. Otherwise wait for others.

A unit's seed is a hash of the run, round, and unit, so all of its replicas
train the same data the same way.

### Submissions

Training a unit exports the model in Hugging Face format, publishes it to the
store, and records `model_sha256`, every published file's SHA-256, the tokens
trained, the round's starting weights (`base_sha256`), and the published URL.
The release `BOM.json` carries the weights' lineage.

### Closing a round

The closer fetches each submission and checks it against the hashes its record
commits to. Then, for each unit:

- **Starting weights.** Every replica must record the round's merged weights as
  its `base_sha256`. A unit with a replica that started anywhere else is dropped.
- **Tokens.** Replicas must report the same tokens trained.
- **Held-out loss.** The closer measures each replica itself, never trusting a
  contributor's own report, with `waldo model train <model> <compose>
  --evaluate-only` (see [Model lifecycle](MODEL-LIFECYCLE.md)) and the unit's
  training seed. The probe therefore scores exactly the records that the unit's
  training held out, and every closer scores the same records.
- **Agreement.** GPU training is not bit-reproducible, so replicas are judged by
  their progress from the round's starting loss: honest replicas improve alike,
  and a free-rider that submits the starting weights does not improve at all.
  Two replicas agree when their loss gap is within `replica_tolerance` of the
  round's progress or below `replica_floor`. A replica that agrees with a
  majority of the others is accepted; a unit without such a majority is
  dropped.

A round with one accepted unit, always so for the bootstrap, takes that unit's
weights as its merge. Two or more accepted units are merged with a merge-only
compose:

```yaml
base:
  merge:
    - model: <unit submission>
      weight: <tokens trained>
```

The merged model is a WALDO merged origin whose records pin every parent and
the lineage they share; WALDO refuses to merge parents without a recorded
common ancestor (see [Model compose](MODEL-COMPOSE.md#merged-base)). Its export
is published, and `merge.yaml` records the accepted and dropped units and why,
the merged weights' hashes and URL, and, for every unit whose replicas were
compared, the evidence: the starting loss, the held-out set's SHA-256, and
every replica's measured loss.
The merge is deterministic, so independent closers of one round compute the
same hash; the first `merge.yaml` on the default branch stands.

## Choosing replicas

Replicas trade compute for verification: two replicas cost twice the compute of
one, and three cost three times. Verification works by measuring each replica's
progress from the round's starting loss, so its power depends on how far a
round moves the loss. The numbers below come from the runs recorded so far and
are guidance, not enforced rules.

| Round | Progress (starting minus replica loss) | Honest replica gap | Free-rider gap |
| --- | --- | --- | --- |
| Early pretraining | about 0.3 to 2 | 0 to 3% of progress | 100% of progress |
| Late post-training | about 0.0001 | below `replica_floor` | also below `replica_floor` |

- **Replicate while rounds make clear progress.** While each round's progress is
  well above `replica_floor` (roughly ten times it, about 0.01 with the
  default), honest replicas sit far inside `replica_tolerance` and a free-rider
  sits far outside it, so `replicas: 2` catches free-riders reliably.
- **Below that, replicas stop verifying.** Once a round's progress falls under
  `replica_floor`, a free-rider who submits the starting weights is
  indistinguishable from honest work and passes. Further replicas only
  duplicate compute; spend it on new data instead, which restores measurable
  progress and with it the check's power.
- **Use three replicas for high stakes.** Two disagreeing replicas are both
  dropped; three can outvote one bad submission, at three times the compute.
  Reserve them for rounds whose loss would be costly, or for identities not yet
  trusted.
- **Spot-check at scale (not built).** Replicating a random share of units,
  chosen after submissions arrive, and removing an identity caught free-riding
  would deter cheating at a fraction of full replication's cost: a contributor
  risks their whole standing on every unit.

Each merge record's evidence holds the starting loss and every replica's
measured loss, so every run measures its own progress curve and shows where
replication stopped verifying.

## Records and signatures

Records are JSON (valid YAML) signed with `ssh-keygen -Y sign` in the
`waldo-join` namespace by the identity they name, and committed with `-s` with
that identity as author. A reader ignores a record that does not verify against
the plan's key for the identity it names, or that sits in a file other than
`<unit>-<identity>.yaml` for its round. A merge record that does not verify
stops the run, since nobody may build on it.

## Stores

`file:///path` stores keep `<path>/<run>/<model>/` on one machine. With
`huggingface`, each identity uploads to its own namespace
(`<hf>/waldo-<run>-r<round>-<unit>-<identity>`, one repository per submission
or merge) through the `hf` command of `huggingface_hub`, and records pin the
uploaded commit, `huggingface://<hf>/<model>@<commit>`. Readers download exactly
the recorded files at that commit and check every hash before using any of them;
a URL outside the named identity's own account is refused.

## GitHub runs repositories

For a GitHub runs repository, each record goes to its own branch,
`waldo-join/<run>/<identity>/<round path>`, on the runs repository when the
identity's account can push to it, or otherwise on the account's fork. Join
opens a pull request and waits for the repository's verifier to merge or close
it. The verifier is the repository's own Action: it merges a pull request only
if it adds nothing but record files under an existing run's `rounds/`, each
signature verifies against the plan's key for its identity, and the round has
no merge yet. It reads files and runs `ssh-keygen`, never code from the pull
request.

GitHub's fine-grained tokens can push to a fork but cannot open a pull request
on a repository their owner does not control, so record pull requests need a
classic token with `public_repo`. Give that token to a GitHub machine account
rather than a personal one: it then reaches only the machine account's forks,
and the records stay off the person's contribution graph. Store it in
`~/.waldo/join/<identity>/github-token` with mode 600; join passes it only
to its own `gh` and `git` calls and refuses a token file others can read.
Without that file, join uses the caller's `GH_TOKEN` or `gh` login.

## Testing

`testing/e2e/model-join.sh` runs two identities through a local bare runs
repository with a `file://` store and real MLX training: a bootstrap and a
pretraining round over three units, with one identity free-riding on one unit.
It asserts that the closer drops that unit on measured evidence, merges the
honest units by token-weighted mean, and records the evidence. Set
`JOIN_POSTTRAIN_CORPUS` to an index path to add a post-training round over
`filter.slice` shares of it.
