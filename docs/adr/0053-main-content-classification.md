# ADR 0053: Classify main content with a default-true row fact

Status: accepted

## Decision

Canonical text rows contain the required `main_content` boolean. Retained rows
default to `true`. A structured input profile may declare one or more exact
scalar field conditions; every condition must match for the row to be primary.
A missing mapped field fails ingestion.

Composes select primary rows with `main_content: true`. Older rows that predate
the field read as `true` for compatibility.

Current text output uses writer recipe
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v9-privacy-redaction`.
