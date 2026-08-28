# ADR 0002: OpenWALDO BOMs are the model boundary

Status: accepted
- Date: 2026-08-04

## Context

Training needs local verified shard paths and provenance, but allowing model
code to traverse the index, resolve licenses, and fetch objects duplicates the
data domain and couples training to mutable checkout state.

## Decision

The corpus domain resolves a selection into an immutable OpenWALDO BOM
(`corpus.BOM`).
Model workflows consume that OpenWALDO BOM and cannot independently
reinterpret its manifests, licenses, or object locations.

The OpenWALDO BOM records index identity, the normalized selection, manifest and
shard pins, effective licenses, and totals. Materialized paths are execution
details paired with those pins, never substitutes for them.

## Consequences

- Model orchestration has one trustworthy input abstraction.
- Export and training share selection and verification behavior.
- Tests can use synthetic OpenWALDO BOMs without constructing an index checkout.
- Its JSON serialization is the language-independent interchange contract.
- Changes to index internals do not require changes to training backends.
