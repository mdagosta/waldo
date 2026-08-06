# ADR 0030: Pin a bounded deterministic held-out evaluation set

- Status: accepted
- Date: 2026-08-05

## Context

Reporting the loss of the batch currently being optimized is not held-out
evaluation. It cannot establish whether training generalizes, and an
unrecorded sample cannot be reproduced or audited.

## Decision

The schema-1 causal-pretraining profile selects a deterministic held-out set
using the lowest SHA-256 scores over each canonical shard-and-row identity,
salted by the training seed. The default target is one percent of records,
capped at 256 records and 1 MiB of source text, while leaving at least one
record for training. A corpus with fewer than two records records an empty set
because a genuine split is impossible.

The run BOM pins the selection algorithm, seed, record count, byte-token
target count, text bytes, and a digest over the ordered selected identities.
Selected records are excluded from every training epoch. WALDO streams them to
the backend separately, verifies their counts against the run BOM, and records
no-gradient held-out loss and perplexity at the resolved evaluation cadence.
Evaluation uses per-record EOS packing so no target crosses document
boundaries.

Portable compose parameters may change the fraction and resource caps or
explicitly disable evaluation. All resolved values remain part of the
immutable run BOM.

## Consequences

- Evaluation evidence is reproducible from the corpus BOM without embedding
  record contents in model metadata.
- Evaluation memory and runtime remain bounded for very large corpora.
- Training step derivation excludes held-out tokens.
- More elaborate task-specific, SFT, and benchmark evaluation contracts can
  be added without calling training loss an evaluation metric.

