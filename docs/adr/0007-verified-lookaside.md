# ADR 0007: Verify lookaside objects before use

Status: accepted

## Decision

Stream every downloaded object through SHA-256 and exact-size validation when
a size is declared. Install verified objects atomically at hash-derived cache
paths. Partial downloads remain in disposable scratch storage.

Successful commands purge the verified cache objects they consumed after
their output or durable state commits. Failed and interrupted commands retain
verified objects for retry, subject to `lookaside.cache.max-size`.

Native export copies and re-verifies canonical objects. JSONL export streams
verified Parquet rows and records both source-object and converted-file hashes.
An existing export is reused only when its persisted BOM and file hashes match.
