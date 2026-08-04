# ADR 0006: Use an index-centered CLI

- Status: accepted
- Date: 2026-08-04

## Context

Index and corpus are useful separate implementation concepts: the index owns
metadata meaning while corpus workflows resolve, ingest, and materialize data.
As adjacent CLI groups, however, they make users decide which one owns the same
indexed corpus. Operations such as list, add, update, export, and remove can
plausibly appear under either name.

## Decision

Expose corpus workflows beneath `waldo index`. `index list` recursively lists
the corpora beneath a path; `index show` provides one detailed view; `index add`,
`update`, `export`, and `remove` own corpus mutation and materialization.

Retain corpus as an internal domain and as terminology for the data itself. CLI
organization follows the user's mental model, not the package graph.

## Consequences

- Users have one place to discover all indexed-data operations.
- Export fits because it exports a selection resolved from the index, not an
  arbitrary directory of files.
- The `index` command group is broader, so its verbs and help text must clearly
  separate read-only inspection from mutation and transfer.
- Internal OpenWALDO BOM and ingestion code remains independently testable.
