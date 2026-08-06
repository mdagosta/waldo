# 0034: Keep ingest profiles corpus-neutral

Status: accepted

## Decision

Physical containers and logical mappings are separate facts. WALDO detects and
pins primitive containers; a strict recipe optionally declares a reusable
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
belong in the reviewed recipe or fetcher. A fetcher may deposit a simpler
primitive format such as record-map JSONL when the upstream shape requires
source-specific transformation.

Profiles fail closed on empty required fields and embedded NUL characters.
Recipes may explicitly select `on_empty: skip` or `nul: space`; these policies
are part of the plan identity. A recipe may also raise the default 64 MiB
indivisible-record ceiling to at most 256 MiB, subject to the plan memory
budget, without changing the canonical record schema.

Per-record licenses are normalized by WALDO, preserved as raw evidence, and
partition canonical objects so each shard has one effective license.

## Consequences

Adding a corpus does not add an ingest adapter. New physical cardinalities,
including a future streaming JSON-array container, require an explicit format
and architecture decision rather than inference.
