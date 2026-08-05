# ADR 0001: One binary with bounded domains

- Status: accepted
- Date: 2026-08-04

## Context

Contributing data, verifying it, selecting a corpus, and carrying its provenance
into a model are one user journey. Separate installations would add friction
and make the reference end-to-end path harder to demonstrate. The former
implementation also showed that one binary can become internally tangled when
command proximity is mistaken for shared ownership.

## Decision

Distribute one `waldo` binary. Inside it, maintain bounded index, corpus,
lookaside, provenance, model, and training domains with the dependency rules in
`docs/ARCHITECTURE.md`.

There is no separate `compose` product surface. Declarative composition is a
model compose consumed by `waldo model compose`.

## Consequences

- Users install and version one tool.
- End-to-end tests can exercise the complete provenance path.
- Schema and behavior changes can land atomically.
- Package boundaries and tests, rather than repositories, must prevent reverse
  dependencies and duplicated concepts.
- The decision may be revisited if a domain develops independent implementers,
  governance, or release cadence—not merely because it has many files.
