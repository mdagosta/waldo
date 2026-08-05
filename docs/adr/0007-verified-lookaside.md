# ADR 0007: Verify objects before admission and export

- Status: accepted
- Date: 2026-08-04

## Context

A manifest's object hash is useful only if every consumer checks the bytes it
actually receives. A cache can also corrupt after download, and hard-linking an
editable export to a cache entry would let user activity mutate supposedly
verified shared state.

## Decision

Stream every fetched object through SHA-256 and optional exact-size validation,
then atomically install it into a hash-derived retained cache path. Re-hash an
existing object before materialization. Partial downloads use a separate
disposable scratch directory and never appear at a cache path until complete.
Bound the retained cache by least-recently-used file time and provide an
independent scrub. Configurations created before this distinction that set only
`lookaside.scratch` retain their historical purge-on-success behavior.

Native export copies and re-verifies bytes into an atomic destination file; it
does not hard-link exports to the cache. Existing export files are resumed only
when their size and hash match. An `EXPORT.json` containing a different
OpenWALDO BOM blocks reuse of that directory.

Canonical JSONL export streams rows from verified native Parquet objects,
validates schema-1 records and their text hashes, and atomically publishes the
converted file. Its export entry records the lookaside-object hash separately
from the converted file hash. An existing converted file is retained only when
a fresh conversion produces the same bytes.

## Consequences

- A successful materialization has one clear verified-byte guarantee.
- Cache hits spend sequential I/O to defend against local corruption.
- Repeated audits and exports reuse verified objects without redownloading.
- Exports use additional disk rather than sharing cache inodes.
- Interrupted downloads and copies never appear at their final paths.
- Mirror and transport implementations can change without changing object
  identity or corpus meaning.
