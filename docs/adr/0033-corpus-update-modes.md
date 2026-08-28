# ADR 0033: Corpus updates are authoritative rebuilds

- Status: amended by ADR 0064
- Date: 2026-08-05

## Context

Combining old Parquet with new input implicitly retains obsolete shard layouts
and makes it unclear whether the supplied input is complete or incremental.
Sources help fetchers avoid old upstream material, but their aggregate hashes
cannot prove record membership. A corpus update needs one unambiguous meaning.

## Decision

`waldo index ingest --update <manifest-directory> <manifest>` always performs
an authoritative rebuild. Its input must contain the complete desired corpus.
The ingestion manifest owns the same metadata and input mappings during update
as during initial ingestion. WALDO does not read old shard bodies. It
deduplicates the new acquisition internally, writes with the current canonical
Parquet profile, and replaces the manifest's source and shard arrays. Old
objects are not deleted implicitly.

Every touched manifest and directory navigation file is emitted as schema-1
YAML. The contribution lists superseded JSON or YML paths, pins and rechecks
the original manifest hash, and remains separate from the Git worktree.

## Consequences

- Public-index migration can rebuild 150 MB-era corpora into new 256 MiB-target
  shards from a complete manifest-backed raw directory without downloading the
  old objects.
- Ingest refuses an existing destination unless `--update` is explicit.
- Every update has the same complete-replacement semantics.
- There is no flag or implicit mode that appends records to existing shards.
- Git history retains the retired shard references; lookaside deletion is a
  separate explicit maintenance decision.
