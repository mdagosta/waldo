# ADR 0031: Resume durable model-compose transactions

Status: accepted

## Decision

WALDO content-identifies a compose transaction from the target model, strict
compose, ordered corpus BOM hashes, and current model identity. The active
model remains at `<model.root>/<name>` while transaction metadata lives beneath
`<model.root>/.waldo-compose`.

Each invocation takes a non-blocking per-model lock. Completed stages are
verified and skipped. An interrupted stage resumes the same run from its newest
verified compatible checkpoint. Failed stages are terminal and are not silently
replayed.

The transaction is removed only after every stage completes and the model BOM
commits. `waldo model continue <name>` is allowed only when such a pending
transaction exists.
