# ADR 0052: Preserve content assessments as immutable row facts

Status: accepted

## Decision

Current canonical text rows record `repetitive_content` and
`boilerplate_content` booleans produced by the pinned Gopher-derived detectors.
Assessment runs after mandatory privacy redaction and before Parquet encoding.

Detector identities and flagged-record counts are preserved in shard footers,
embedded shard BOMs, manifests, aggregate metadata, and resolved corpus BOMs.
These flags are observations, not legal or safety conclusions.

Compose filters may exclude matching assessment facts. Conditions inside the
exclusion block are ORed. Schema-1 rows lack these facts and follow ADR 0054's
explicit compatibility behavior.

Current text output uses writer recipe
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v9-privacy-redaction`.
