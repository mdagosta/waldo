# 0055: Apply automatic privacy redaction before canonical identity

Status: accepted

## Decision

Every ingestion applies `waldo/privacy-redaction-v1` to every record, regardless
of source or ingest profile. WALDO retains names and public attribution, replaces
email addresses, IP addresses, phone numbers, and high-confidence credentials
with typed placeholders, and removes RFC 822 routing headers from recognized
message header blocks.

Redaction runs before content hashing, deduplication, token measurement,
assessment, and Parquet encoding. Raw values therefore do not enter canonical
shards, lookaside objects, exports, or training input. Schema-2 rows record
redaction counts; shard footers, embedded shard BOMs, manifests, and corpus
OpenWALDO BOMs preserve the policy identity and aggregate counts. Names are
explicitly recorded as retained.

The writer identity is
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v9-privacy-redaction`. This is an
in-place schema-2 expansion, not schema 3. Existing schema-2 writer v8/v7 and
schema-1 shards remain readable but are not retroactively described as
redacted. Rebuilding is required to obtain the new guarantee.

`filter.exclude.email_addresses` is removed. Email removal is a mandatory
canonicalization policy, not an optional training-time selection.

## Limits

The policy is deterministic risk reduction, not a claim of anonymity or GDPR
compliance. Names, indirect identifiers, and detector misses can remain.
Acquisition directories may contain raw upstream material while ingestion is
running and must remain private temporary storage.
