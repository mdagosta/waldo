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
then atomically rename it into a hash-derived lookaside cache path. Re-hash a
cache entry before materialization. Provide an independent cache scrub.

Native export copies and re-verifies bytes into an atomic destination file; it
does not hard-link exports to the cache. Existing export files are resumed only
when their size and hash match. An `EXPORT.json` containing a different
OpenWALDO BOM blocks reuse of that directory.

## Consequences

- A successful materialization has one clear verified-byte guarantee.
- Cache hits spend sequential I/O to defend against local corruption.
- Exports use additional disk rather than sharing cache inodes.
- Interrupted downloads and copies never appear at their final paths.
- Mirror and transport implementations can change without changing object
  identity or corpus meaning.
