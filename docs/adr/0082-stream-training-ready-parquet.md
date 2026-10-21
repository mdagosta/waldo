# ADR 0082: Stream training-ready Parquet into token-budget runs

Status: accepted

## Context

Canonical OpenWALDO Parquet already contains the normalized records and
row-level metadata required by training. Nevertheless, token-budget stages
made two corpus-scale passes before accelerator work: exact held-out selection
and tokenization/packing to prove how many complete source passes could satisfy
the requested steps. Startup therefore scaled with the complete corpus rather
than the bounded training budget.

## Decision

Default held-out selection uses deterministic bounded indexed row reads. The
run BOM still pins the policy, seed, selected identities, selected-model token
targets, source bytes, and digest. Exact lowest-hash selection remains an
explicit legacy policy for operators who require a full eligible-row ranking.

Token-budget stages treat canonical Parquet as the training stream. WALDO
applies record filters, tokenizes, and continuously packs rows while the
training worker runs. Deterministic source passes repeat until the exact
optimizer-step target is supplied. No separate packing-capacity pass is made.
Epoch-driven stages retain their finite full-stream scan because their step
count is defined by the amount of filtered, tokenized data in each epoch.

Existing checkpointed fixed-token runs may upgrade only the derived source
mechanic to this repeating stream. WALDO records the correction as
`streaming-token-source-v1`; every immutable user-controlled training fact and
the held-out split must still match.

## Consequences

- Token-budget startup work is bounded by materialization, verification, and a
  small indexed evaluation sample rather than corpus-wide tokenization.
- License, language, and content filters remain row-level and are applied as
  records stream into training.
- Tokenization overlaps accelerator execution and each backend receives the
  same deterministic token stream.
- Exact corpus-wide tokenizer measurement remains an explicit diagnostic, not
  a training prerequisite.
- Epoch-driven budgets and explicit legacy evaluation policies still perform
  full scans by definition.
