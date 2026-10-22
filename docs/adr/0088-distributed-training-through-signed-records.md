# ADR 0088: Coordinate distributed training through signed records in a git repository

Status: proposed

## Context

Training an open model with many contributors' machines needs a way to divide
the work, check each contribution, combine them, and keep the record of how the
model was built, without trusting any contributor and without running a
coordinator. The initial scope in [VISION](../VISION.md) excluded automatic pull
requests and multi-node orchestration; synchronous multi-host training
([0025](0025-torchtitan-distributed-adapter.md)) requires trusted, homogeneous
hosts.

## Decision

`waldo model join <runs-repo> <run-dir> --identity <name>` contributes to a
training run kept in a git repository. This expands WALDO's scope to
asynchronous, untrusted distributed training.

- A run is a directory with `plan.yaml` (`kind: waldo-join-plan`, schema 1,
  unknown fields rejected): units of data, replicas, rounds, a store, and the
  identities allowed to contribute, each an SSH signing key and optional
  Hugging Face account.
- Each pass reads the default branch, verifies every record against the plan,
  and does the next unit: claim a slot, then train a unit or close a full
  round. Records are JSON signed with `ssh-keygen -Y sign` in the `waldo-join`
  namespace by the identity they name (`waldo-join-claim`,
  `waldo-join-submission`, `waldo-join-merge`, schema 1). A record counts only
  if its signature verifies, it matches its file name, run, round, and stage,
  its model and file names are plain names, and, for work, its identity holds
  the claim for it. A claim is a fixed slot file, so two claims on one slot
  conflict in git; an unfilled claim expires after the plan's `claim_minutes`.
- Closing a round verifies each unit: every replica must start from the round's
  merged weights, report the same tokens, and submit distinct weights; the
  closer then scores each replica itself with `--evaluate-only` (ADR 0087) on
  the records the unit's training held out, and accepts the replica most others
  agree with when gaps stay within the plan's tolerance of the round's
  progress. Accepted units are merged with `base.merge` (ADR 0084), and the
  merge record carries the measured evidence. The plan's `closers` (default 3)
  each close the round independently, and the round is decided by the merge a
  majority of them sign; when no majority forms, another closing slot opens.
- Weights and their training provenance (ADR 0083) are published to a file or
  Hugging Face store, pinned by hash in the records. On GitHub, each record
  goes through a pull request that the runs repository's own Action verifies
  and merges.
- Each identity keeps its signing key, optional GitHub token (held as WALDO's
  other credentials are, [0035](0035-local-s3-credentials.md)), clone, and work
  in `~/.waldo/join/<identity>`. Join runs the external `git`, `ssh-keygen`,
  `gh`, and `hf` commands.

## Consequences

- No process coordinates a run; its repository history is the record of how
  the model was built, and every model it produces traces back to its data.
- A free-rider who submits the starting weights is dropped on measured
  evidence, and a verbatim copy of another replica is dropped as a duplicate.
- Open: a lightly perturbed copy of another replica is not detected; the
  complete defense is commit-reveal, where replicas commit their weights' hash
  before any is published. Spot-checking at scale is also future work.
- Identities claim slots before working, so a round's work is divided and
  every record traces to the claim that allowed it; this costs one more record
  per unit and per closer.
- A round is decided by a majority of independent closers, so no single
  closer is trusted; a majority of colluding closers is not defended against.
- Fine-grained GitHub tokens cannot open pull requests on repositories their
  owner does not control, so contributors need a classic `public_repo` token,
  best held by a machine account; a verifier that collects records from
  contributors' forks would remove that need.

`internal/join` is a bounded domain of the one binary ([0001](0001-single-binary.md)) that trains,
measures, and merges only through the model domain. Relates to [0035](0035-local-s3-credentials.md),
[0075](0075-immutable-evaluation-and-contamination-gates.md), and
[0025](0025-torchtitan-distributed-adapter.md).
