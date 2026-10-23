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
training run kept in a git repository, expanding WALDO's scope to asynchronous,
untrusted distributed training.

- A run is a `plan.yaml` (`waldo-join-plan`, schema 1, strict): units, replicas,
  closers, rounds, a store, and the identities allowed to contribute, each an
  SSH key and optional Hugging Face account.
- Work is claimed before it is done. Records (`waldo-join-claim`,
  `-submission`, `-merge`, schema 1) are JSON signed with `ssh-keygen -Y sign`
  in the `waldo-join` namespace, and count only if they verify, match their file
  name, run, round, and stage, and, for work, come from the slot's claimant.
- Closers set aside replicas one at a time (wrong base, identical weights, odd
  tokens, provenance that does not rebuild, another unit's corpus), score the
  rest with `--evaluate-only` (0087), and merge the agreed ones with
  `base.merge` (0084). A round is decided by the first decision, in slot order,
  that a majority of closing slots sign, including the published provenance.
- Weights and their provenance (0083) are published to a file or Hugging Face
  store, pinned by hash. On GitHub, a verifier Action applies join's rules to
  each record's pull request.
- Each identity lives in `~/.waldo/join/<identity>`; credentials as in
  [0035](0035-local-s3-credentials.md). Join runs `git`, `ssh-keygen`, `gh`, and `hf`.

## Consequences

- No process coordinates a run, and no single contributor or closer is trusted;
  the repository's history is the record of how the model was built.
- Every model traces back to its corpora: closers rebuild each submission's
  training closure, and the majority's decision commits to it.
- Not defended: a colluding majority of closers or replicas, a lightly perturbed
  copy (commit-reveal would stop it), the plan's author, and runs repositories
  without a verifier.
- Contributors on GitHub need a classic `public_repo` token, best on a machine
  account, and spend about one API call per record.

`internal/join` is a bounded domain of the one binary ([0001](0001-single-binary.md)) that trains,
measures, and merges only through the model domain. Relates to [0035](0035-local-s3-credentials.md),
[0075](0075-immutable-evaluation-and-contamination-gates.md), and
[0025](0025-torchtitan-distributed-adapter.md).
