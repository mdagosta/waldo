# ADR 0031: Resume durable model-compose transactions

- Status: accepted; replacement behavior superseded by ADR 0043
- Date: 2026-08-05

## Context

A model compose may run several expensive training stages. A process-local
temporary directory discards every completed stage after interruption. The
active model must also be visible through the ordinary model path so `list`,
`summary`, and other read operations describe work in progress. Replacement
still requires a recoverable copy of the prior model.

## Decision

WALDO content-identifies a compose transaction from the destination name,
strict compose, ordered corpus BOM hashes, replacement intent, and current
target model identity. The active model always lives at
`<model.root>/<name>`. WALDO stores only transaction metadata and an optional
replacement backup beneath `<model.root>/.waldo-compose/<transaction-sha256>`
and takes a non-blocking per-name advisory lock for each invocation.

Every run uses the normal immutable run BOM, atomic state transitions,
checkpoint verification, and same-run resume contract. When a process exits
without writing a terminal state, the released advisory lock proves no prior
owner remains; the next exact invocation changes that attempt from `running`
to `interrupted` and resumes it. Completed stages are verified and skipped.
Failed work is cleared rather than treated as resumable. An interrupted model
remains at its ordinary path. While a transaction is unfinished, a different
compose for that name is refused.

ADR 0043 later removed replacement and extended this transaction to append
compose stages to an architecture-compatible existing model.

## Consequences

- Ctrl-C or process loss does not discard completed compose stages or a usable
  checkpoint from the current stage.
- Concurrent identical composes fail clearly instead of sharing mutable state.
- Active new models appear under their standard name and are
  visible to ordinary model inspection commands.
- Staging is intentionally machine-local and is not part of model identity or
  an export.
