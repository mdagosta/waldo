# ADR 0031: Resume durable model-compose transactions

- Status: accepted
- Date: 2026-08-05

## Context

A model compose may run several expensive training stages. A process-local
temporary directory discards every completed stage after interruption, while
publishing the destination model early exposes incomplete state. Replacement
is especially sensitive because the existing usable model must remain intact.

## Decision

WALDO content-identifies a compose transaction from the destination name,
strict compose, ordered corpus BOM hashes, replacement intent, and current
target model identity. It stores the transaction and staged model beneath
`<model.root>/.waldo-compose/<transaction-sha256>` and takes a non-blocking
advisory lock for the duration of each invocation.

Every staged run uses the normal immutable run BOM, atomic state transitions,
checkpoint verification, and same-run resume contract. When a process exits
without writing a terminal state, the released advisory lock proves no prior
owner remains; the next exact invocation changes that attempt from `running`
to `interrupted` and resumes it. Completed stages are verified and skipped.
Failed work is cleared rather than treated as resumable.

The published destination is untouched until every stage completes. For
`--replace`, WALDO verifies that the original target still has the identity
pinned by the transaction, then swaps the staged model into place. Changed
input facts produce a separate transaction.

## Consequences

- Ctrl-C or process loss does not discard completed compose stages or a usable
  checkpoint from the current stage.
- Concurrent identical composes fail clearly instead of sharing mutable state.
- Partial new or replacement models never appear under their public name.
- Staging is intentionally machine-local and is not part of model identity or
  an export.

