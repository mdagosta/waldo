# 0034: Keep ingest profiles corpus-neutral

Status: amended by [0061](0061-stream-json-record-arrays.md) and [0064](0064-manifest-backed-ingestion.md)

## Decision

Physical containers and logical mappings are separate facts. WALDO detects and
pins primitive containers; a strict ingestion manifest declares a reusable
profile that maps each physical record to canonical text.

Supported record containers have explicit cardinality:

- JSON: one top-level object per file; arrays are rejected
- JSONL, gzip JSONL, and zstd JSONL: one object per line, streamed
- Parquet: one row per record

The logical record profiles are `record-map`, `dialogue-pair`, and
`ranked-conversation-tree`. Whole-file primitives are `bounded-text`, using
configured regular-expression boundaries, and `xml-record`, using a documented
XPath subset. Profile configuration is pinned into the plan, source acquisition
identity, and conversion identity.

Profiles never name or recognize a corpus. Source-specific marker expressions,
XML vocabulary knowledge, field cleanup, date assembly, and URL construction
belong in the reviewed ingestion manifest or acquisition tool. A fetcher may deposit a simpler
primitive format such as record-map JSONL when the upstream shape requires
source-specific transformation.

Profiles fail closed on empty required fields and embedded NUL characters.
Ingestion profiles may explicitly select `on_empty: skip` or `nul: space`;
these policies are part of the plan identity. A profile may also raise the default 64 MiB
indivisible-record ceiling to at most 256 MiB, subject to the plan memory
budget, without changing the canonical record schema.

Per-record licenses are normalized by WALDO and preserved as raw evidence.
Canonical rows retain their effective license, while each shard records the
sorted set of licenses represented by its rows; license does not force a shard
boundary.
License-policy selection remains fail-closed at the physical boundary: a mixed
shard is selectable only when every represented license is allowed. Supporting
partial mixed-shard selection would require an explicit filtered/repacked view,
not silently altered BOM totals.
Normalization recognizes canonical Creative Commons URLs both alone and
inside descriptive upstream labels, and maps an exact case-insensitive
`Public Domain` declaration to `LicenseRef-Public-Domain`. The original value
remains in `license_raw`.

## Consequences

Adding a corpus does not add an ingest adapter. New physical cardinalities,
including a future streaming JSON-array container, require an explicit format
and architecture decision rather than inference.
