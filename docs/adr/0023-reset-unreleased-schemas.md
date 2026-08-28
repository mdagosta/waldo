# ADR 0023: Reset unreleased schemas to version 1

- Status: amended
- Date: 2026-08-05

## Context

WALDO and its public index have not been released. Directory indexes had
reached schema 2 during development even though corpus manifests, canonical
Parquet records, BOMs, ingestion manifests, and model formats still began at schema
1. Preserving a pre-release version history would create needless user-facing
complexity.

## Decision

Every persistent WALDO format begins at schema 1 for the first release. In
particular, both directory navigation metadata and corpus manifests use schema
1, and canonical Parquet records use record schema 1. Their later YAML-primary
encoding does not change that schema identity.

WALDO emits directory schema 1. Because the public `waldo-index` Git repository
still contains schema-2 JSON directory indexes, WALDO also accepts that legacy
directory schema on read. Corpus manifests originally remained schema 1;
schema 2 was subsequently assigned to multi-source and mixed-license manifest
facts by ADR 0034.
The formats retain independent schema fields because they can evolve
independently after release, but their initial version is uniformly 1.

## Consequences

- Users see one initial version across all format families.
- Pre-release schema-2 directory indexes remain readable but new writes use
  schema 1.
- Future incompatible changes increment only the affected format and require
  an ADR, fixtures, and a migration or compatibility policy.
- Additive fields that follow a format's schema-1 rules do not require a
  version increase.
