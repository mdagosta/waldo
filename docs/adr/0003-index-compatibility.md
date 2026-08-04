# ADR 0003: Preserve the public index format

- Status: accepted
- Date: 2026-08-04

## Context

The existing public index contains real, expensive-to-rebuild corpus metadata
and points to content-addressed objects. The backend implementation is being
replaced because its internal design and user experience should not constrain
the new system.

## Decision

Maintain read compatibility with the current `waldo-index` and its referenced
objects. Treat old internal Go APIs, model state, configuration, and command
organization as non-contractual.

Implement and test reads before implementing writes. Any intentional change to
canonical output uses a new schema or recipe identity rather than claiming byte
compatibility.

## Consequences

- The public corpus does not need migration to adopt the new binary.
- Golden fixtures and real-index acceptance tests are required.
- Some historical format complexity may remain in readers while new writers
  emit a smaller, more explicit subset.
- The team can redesign code and UX without recreating the former backend.

