# ADR 0033: Corpus updates are authoritative rebuilds

Status: accepted

## Decision

`waldo index ingest <input> <destination> --update` treats the input as the
complete desired corpus. It does not append records or read old shard bodies.

Manifest-backed inputs own corpus metadata and mappings. Direct inputs must
provide the required metadata flags. WALDO writes current canonical shards,
replaces the manifest's source and shard sets, rechecks the original manifest
hash, and applies the verified metadata contribution to the configured writable
index checkout.

WALDO does not commit or push the checkout. Old lookaside objects are never
deleted implicitly.
