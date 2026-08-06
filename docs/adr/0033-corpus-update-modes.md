# ADR 0033: Separate corpus append from authoritative recipe rebuilds

- Status: accepted
- Date: 2026-08-05

## Context

An existing corpus can receive a genuinely incremental source acquisition, or
it can be recreated completely using a newer reviewed ingest recipe and writer.
Combining old Parquet implicitly during the latter operation would retain an
obsolete shard layout and make it unclear which acquisition is authoritative.
Sources help fetchers avoid old upstream material, but their aggregate hashes
cannot prove record membership.

## Decision

`waldo index update <input-or-recipe> <manifest>` is append mode. It pins the
manifest, audits and materializes existing shards, and seeds their exact record
content hashes into a disk-backed set. New input passes through the normal
streaming ingestion and publication pipeline; only absent records become new
shards. Recipes receive existing source and aggregate facts in the temporary
file named by `WALDO_UPDATE_STATE`.

`--rebuild-shards` is an authoritative replacement mode. Its input must contain
the complete desired corpus. WALDO does not read old shard bodies, deduplicates
the new acquisition internally, writes with the current canonical Parquet
profile, and replaces the manifest's source and shard arrays. Old objects are
not deleted implicitly.

Every touched manifest and directory navigation file is emitted as schema-1
YAML. The contribution lists superseded JSON or YML paths, pins and rechecks
the original manifest hash, and remains separate from the Git worktree.

## Consequences

- Public-index migration can rebuild 150 MB-era corpora into new 256 MiB-target
  shards solely from reviewed recipes without downloading the old objects.
- Ordinary updates remain exact even when a fetcher returns overlapping data.
- Mixed historical shard sizes remain valid until a corpus is deliberately
  rebuilt.
- Git history retains the retired shard references; lookaside deletion is a
  separate explicit maintenance decision.
