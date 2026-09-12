# ADR 0045: Balance declared training corpora

Status: accepted

## Context

The original causal pretraining profile shuffled complete shard inputs. A run
whose token budget was smaller than its selected corpus could finish before it
read any record from one or more declared corpus selections. Its BOM described
available inputs, but its observation did not prove what training consumed.

## Decision

`causal-pretrain-shuffled` retains the original behavior.
`causal-pretrain-balanced` adds two general contracts:

- records are selected from the logical corpus path with the fewest emitted
  tokenizer targets, with bounded deterministic shuffle inside each path;
- held-out records are selected deterministically and evenly across those paths.

Every materialized input carries its selected logical corpus identity into the
trainer. After sequence packing, the trainer attributes each consumed next-token
target to that identity. A successful balanced-profile observation must
attribute every consumed token target to a declared corpus and exactly equal
the run's total consumed token targets. The observation is sparse: a declared
corpus from which record filters admit no rows has zero consumption and is
omitted rather than represented by an invalid non-positive entry.

## Consequences

- Preflight warns when filters leave a declared corpus with no eligible rows.
- A finite balanced-profile run cannot silently omit a corpus that has eligible
  records while balancing remains possible.
- Run observations provide auditable per-corpus consumption rather than inferring
  use from selected shard sizes.
- Equal tokenizer-target exposure is the balanced policy while every selected
  corpus still has records; explicit weighting belongs to the separate weighted
  profile.
- Existing shuffled runs and resumes keep their original data order.
