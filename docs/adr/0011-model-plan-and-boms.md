# ADR 0011: Separate immutable model plans, run BOMs, and observed state

Status: accepted
- Date: 2026-08-04

## Context

A compose contains corpus selections, while model identity must be
portable. Training state also changes over time, whereas the inputs authorized
for a run must remain immutable. Combining these into one mutable document
would make failures hard to audit and paths accidentally identity-bearing.

## Decision

Persist an immutable architecture plan before training and content-hash it as
the stable model identity. Resolve index selections into corpus OpenWALDO BOMs,
materialize their shards through the verified cache, and audit canonical
records before launching each training run. A separate corpus export is not a
training prerequisite.

Before launching each backend stage, write an immutable run OpenWALDO BOM that
embeds the corpus BOM and pins shard identities, architecture, backend revision,
objective, and parameters. Persist changing run state and backend observations
in a separate atomic record. Maintain a model OpenWALDO BOM that aggregates
run-BOM, observation, and artifact hashes. Aggregate paths are relative to the
model root and include their run directory; an explicit `path_base` makes that
resolution rule portable across managed and exported models. The aggregate
also labels backend and simulation identity, artifact roles, and the newest
complete non-simulated run with real weights. Historical simulated output is
retained as provenance rather than presented as a usable model.

## Consequences

- Adding training runs does not change immutable model architecture identity.
- A failed or interrupted backend remains part of model history.
- Planned corpus totals cannot be mistaken for backend-observed consumption.
- Model inspection can validate the hash chain without an index checkout.
- An aggregate BOM resolves every artifact without persisting an absolute
  machine path, and consumers can distinguish history from current weights.
- Compose-driven training creates an absent model or appends runs to an
  architecture-compatible model; it never replaces an existing model.
