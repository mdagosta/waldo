# ADR 0045: Balance declared training corpora

Status: Accepted

## Context

The original causal pretraining profile shuffled complete shard inputs. A run
whose token budget was smaller than its selected corpus could finish before it
read any record from one or more declared corpus selections. Its BOM described
available inputs, but its observation did not prove what training consumed.

## Decision

`causal-pretrain-v1` remains unchanged for reproducibility.
`causal-pretrain-v2` adds two general contracts:

- records are selected from the logical corpus path with the fewest emitted
  tokenizer targets, with bounded deterministic shuffle inside each path;
- held-out records are selected deterministically and evenly across those paths.

Every materialized input carries its selected logical corpus identity into the
trainer. After sequence packing, the trainer attributes each consumed next-token
target to that identity. A successful v2 observation must account for every
declared corpus and exactly equal the run's total consumed token targets.

## Consequences

- A finite v2 run cannot silently omit a declared corpus.
- Run observations provide auditable per-corpus consumption rather than inferring
  use from selected shard sizes.
- Equal tokenizer-target exposure is the v2 policy while every selected corpus
  still has records; explicit weighting is a future, separately versioned
  profile concern.
- Existing v1 runs and resumes keep their original data order.
