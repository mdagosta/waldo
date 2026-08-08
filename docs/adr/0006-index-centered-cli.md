# ADR 0006: Use an index-centered CLI

- Status: accepted; path-default details superseded by ADR 0036
- Date: 2026-08-04

## Context

Index and corpus are useful separate implementation concepts: the index owns
metadata meaning while corpus workflows resolve, ingest, and materialize data.
As adjacent CLI groups, however, they make users decide which one owns the same
indexed corpus. Operations such as list, ingest, update, and export can
plausibly appear under either name.

## Decision

Expose corpus workflows beneath `waldo index`. `index list` recursively lists
the corpora beneath a path; `index show` provides one detailed view; `index
ingest`, `update`, and `export` own corpus mutation and materialization.

Index locations are positional, not a global option. An existing checkout,
subtree, corpus directory, or manifest anchors checkout discovery by walking
upward like Git, and recursive commands begin at that positional target. When
the path is omitted, discovery starts at the current directory. A prospective
ingestion destination may be absolute even though the durable plan records
only its checkout-relative path.

ADR 0036 later replaces current-directory default discovery with a managed
default checkout and configured-relative resolution. The index-centered
command organization remains unchanged.

Retain corpus as an internal domain and as terminology for the data itself. CLI
organization follows the user's mental model, not the package graph.

## Consequences

- Users have one place to discover all indexed-data operations.
- Commands do not require users to separately name both a checkout root and a
  target inside it.
- Export fits because it exports a selection resolved from the index, not an
  arbitrary directory of files.
- The `index` command group is broader, so its verbs and help text must clearly
  separate read-only inspection from mutation and transfer.
- Internal OpenWALDO BOM and ingestion code remains independently testable.
