# ADR 0023: Reset unreleased schemas to version 1

- Status: accepted
- Date: 2026-08-05

## Context

WALDO and its public index have not been released. Directory indexes had
reached schema 2 during development even though corpus manifests, canonical
Parquet records, BOMs, ingest recipes, and model formats still began at schema
1. Preserving a pre-release version history would create needless user-facing
complexity.

## Decision

Every persistent WALDO format begins at schema 1 for the first release. In
particular, both `index.json` directory metadata and corpus manifests use
schema 1, and canonical Parquet records use record schema 1.

The existing `waldo-index` checkout is migrated in place. WALDO emits and
accepts directory schema 1 only; it does not carry a legacy schema-2 reader.
The formats retain independent schema fields because they can evolve
independently after release, but their initial version is uniformly 1.

## Consequences

- Users see one initial version across all format families.
- Pre-release schema-2 directory indexes must be migrated before use.
- Future incompatible changes increment only the affected format and require
  an ADR, fixtures, and a migration or compatibility policy.
- Additive fields that follow a format's schema-1 rules do not require a
  version increase.
