# ADR 0037: Embed ingest attestations in canonical shards

- Status: accepted
- Date: 2026-08-08

## Context

Canonical ingestion already hashes and validates source records, computes their
content identities and reference-token counts, performs exact per-license contribution
deduplication, writes aggregate Parquet footer metadata, hashes the completed
object, and verifies it before publication. Repeating the record hash, token,
and duplicate work after every materialization roughly doubles corpus-scale
processing and serializes global duplicate checks through one database.

An OpenWALDO shard needs to carry the immutable builder evidence needed to
distinguish verified ingestion output from an unattested legacy object. A
synthetic non-training row would alter record counts and require every reader
to filter provenance from training data.

## Decision

Canonical writer recipe v5 embeds a schema-1, subject `shard` OpenWALDO BOM in
the Parquet footer under `waldo.bom`. It records the ingestion-plan digest,
record schema, writer recipe, tokenizer, records, tokens, content bytes,
licenses, and the checks performed before publication. The enclosing
lookaside SHA-256 remains the shard's object identity; self-identifying that
hash inside the shard would be circular.

Ingestion validates canonical fields, content hashes, token counts, and exact
per-license deduplication before rows are admitted. It then checks the completed object's
SHA-256, physical schema, row count, footer aggregates, and embedded BOM before
publication.

`waldo index audit` uses scalable attestation verification by default:

1. resolve and validate the corpus BOM;
2. materialize each unique shard through the hash-verifying lookaside cache;
3. validate its physical schema, footer aggregates, and embedded shard BOM;
4. reconcile those totals with the resolved corpus BOM.

Writer v4 is accepted as an implicit ingestion attestation because that exact
writer path performed a complete post-write record audit before publication.
Other unattested writers fall back to a deep scan. `index audit --deep`
explicitly revalidates every record hash and token count.

Recipe-driven ingestion excludes malformed records, empty mapped records, and
record-level mapping failures when the adapter can still establish a safe
record boundary. WALDO counts those omissions by reason and prints a prominent
warning that they are absent from the published shards. Input corruption whose
record boundaries cannot be established remains a hard error. WALDO cannot
infer copyright ownership from prose; licensing exclusions require declared
or mapped license evidence and an explicit recipe policy.

## Consequences

- Provenance travels inside the shard without becoming training data.
- Normal audits no longer decompress and retokenize trusted immutable shards.
- Deep validation remains available for untrusted or newly introduced writer
  implementations.
- Existing unrecognized shards remain compatible through automatic deep-scan
  fallback.
- Footer semantics changed, so the physical writer recipe advances to v5.
- A contributor can forge an unsigned builder claim; governance and DCO review
  still determine whether that claim is trusted. Cryptographic contributor or
  auditor signatures may be added as a separate attestation layer.
