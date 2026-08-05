# ADR 0011: Separate immutable model plans, run BOMs, and observed state

- Status: accepted
- Date: 2026-08-04

## Context

A recipe contains machine-local corpus paths, while model identity must be
portable. Training state also changes over time, whereas the inputs authorized
for a run must remain immutable. Combining these into one mutable document
would make failures hard to audit and paths accidentally identity-bearing.

## Decision

Resolve and verify every corpus export before creating a model. Persist an
immutable build plan whose stages contain corpus OpenWALDO BOM hashes rather
than local paths. Content-hash that plan as the initial model identity.

Before launching each backend stage, write an immutable run OpenWALDO BOM that
embeds the corpus BOM and pins exported files, architecture, backend revision,
objective, and parameters. Persist changing run state and backend observations
in a separate atomic record. Maintain a model OpenWALDO BOM that aggregates
run-BOM, observation, and artifact hashes.

## Consequences

- Moving a verified corpus export does not change model identity.
- A failed or interrupted backend remains part of model history.
- Planned corpus totals cannot be mistaken for backend-observed consumption.
- Model inspection can validate the hash chain without an index checkout.
- A recipe must be completely preflighted before any model directory appears.
