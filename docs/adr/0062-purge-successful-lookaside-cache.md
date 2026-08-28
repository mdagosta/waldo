# ADR 0062: Purge verified cache objects after successful use

Status: accepted
- Date: 2026-08-21

## Context

ADR 0007 introduced a bounded retained cache so repeated operations could
reuse verified lookaside objects. Model training can require a complete corpus
selection to remain materialized for the life of a run, so active objects are
allowed to exceed the nominal cache bound. Keeping that entire selection after
a successful run consumes substantial local storage without aiding recovery.

Failed and interrupted operations do benefit from verified objects remaining
available: an exact retry can reuse them without another transfer.

## Decision

Every successful command removes the verified cache objects returned to that
command after its output or durable state commits. Partial download scratch is
still always disposable. A failed or interrupted command does not invoke
success cleanup, leaving its verified objects available for retry.

`lookaside.cache.max-size` remains an LRU bound for objects retained by
incomplete work. It does not describe post-success retention.

## Consequences

- Successful audit, verification, export, calibration, and training release
  the local copies they consumed.
- Exact retries after failure or interruption can still reuse verified bytes.
- A later independent command redownloads an object purged by an earlier
  successful command.
- Durable model artifacts, index state, published lookaside objects, and
  ingestion recovery state are unaffected.
