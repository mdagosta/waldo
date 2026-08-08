# ADR 0009: Stabilize the corpus export BOM

- Status: accepted
- Date: 2026-08-04

## Context

Model workflows need a durable corpus input that can be inspected without the
original checkout or machine configuration. An export log containing only
paths and hashes would not preserve resolved licenses, conversion recipes,
source provenance, or the difference between a lookaside object and a derived
interchange file.

## Decision

Adopt `EXPORT.json` schema 1 as the first stable OpenWALDO provenance
interchange contract. Its embedded OpenWALDO BOM contains complete resolved
leaf facts and exact redundant totals. The envelope separately identifies the
files created by one materialization.

Validate all self-contained invariants before writing or accepting a document.
Provide offline `waldo index bom` and `waldo index verify`; verification also
hashes every exported file. Canonical JSONL conversion reconciles emitted
document and token totals with the shard declaration.

Schema-1 readers accept unknown fields. Removing fields or changing their
meaning requires a new schema.

## Consequences

- Later model composes can consume the BOM rather than reinterpret an index.
- Native and derived file identity cannot be confused.
- Redundant totals are useful to readers but are verified, never trusted as
  independent facts.
- The document states its limits: it is not evidence of later trainer
  consumption or a new legal conclusion about indexed assertions.
