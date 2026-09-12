# 0068: Pin and reuse deterministic stage preflight

Status: accepted

## Context

Held-out selection enumerates every eligible record, and epoch-driven training
also scans the filtered token stream to derive an exact optimizer-step count.
Repeating an unchanged failed or interrupted stage formerly repeated both
operations even though their inputs are immutable.

## Decision

Each new run stores `PREFLIGHT.json` beside its run records. The artifact
contains the exact sorted held-out selection, its evaluation summary, resolved
parameters, post-filter and post-held-out training record counts by corpus, and
whether an explicit epoch capacity check succeeded. The per-corpus counts let
WALDO warn before training when filtering and held-out selection leave an
entire corpus with no training rows.
`RUN-BOM.json` pins its size and SHA-256.

WALDO computes a preflight identity from the immutable architecture, corpus
BOM, stage type and objective, conversation transform, and planning
parameters. It reuses a prior artifact only when this identity matches exactly,
the artifact hash verifies, and its contents agree with the prior run BOM.
Reconstruction reads only the selected held-out rows. Any mismatch fails
closed or causes a fresh scan when no matching artifact exists.

Legacy runs without this optional artifact remain valid and receive the former
full-scan behavior. Preflight artifacts written before eligible counts were
added remain valid and reusable; WALDO simply cannot issue the zero-row warning
from those older artifacts without rescanning.

## Consequences

Exact retries avoid repeated corpus-wide evaluation selection and
epoch-to-step scans. WALDO still materializes and verifies required shard
objects and rechecks the execution environment before training.
