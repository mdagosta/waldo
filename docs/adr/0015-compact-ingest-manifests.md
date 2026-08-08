# ADR 0015: Keep ingest manifests compact and token counts referential

- Status: accepted
- Date: 2026-08-04

## Context

The public schema-1 index format is lightweight Git metadata. Existing corpus
manifests contain high-level source identity and one compact entry per
lookaside shard. An early rebuilt ingestion path added one `sources[].files[]`
entry per acquired input artifact. A corpus assembled from many small files
therefore produced a 25 MiB manifest, nearly as large as its Parquet object.
That made record cardinality leak into Git metadata size and contradicted the
existing index shape.

The same path emitted zero shard tokens because canonical Parquet is
tokenizer-neutral. Existing manifests nevertheless carry useful reference
token estimates and name the counter in `converted_by.tokenizer`.

## Decision

Generated manifests use schema 2 while retaining the existing compact shape:

- one high-level record per declared source with one deterministic aggregate
  acquisition SHA-256 and its default license;
- one shard entry containing canonical URL, SHA-256, represented source and
  license sets, exact per-license document/token usage, total document count,
  reference-token count, and encoded bytes;
- compact conversion and, when applicable, one ingest-recipe collector pin.

Generated text manifests omit `format`, `processing`, `composed_by`, source
`files` and `usage`, and shard `modalities`. They preserve compact source-level
`license_evidence`, `content`, and `acquisition` declarations without copying
them per file or record. `record_schema` remains because it identifies the
Parquet row contract explicitly.

The acquisition digest is streamed over the canonical source declaration and
length-delimited accepted input facts. No per-input path, record, hash, or
adapter array is serialized into the Git manifest. The immutable plan and
recovery journal may retain those facts while execution needs them;
record-level evidence belongs in canonical Parquet.

WALDO counts retained text with the offline embedded
`tiktoken/cl100k_base` reference counter during shard assembly. It records the
counter name and aggregate shard counts in the manifest and stores the same
estimate in the existing nullable per-row `token_count` column so exports can
reconcile exact totals. The rows still contain text rather than training token
IDs, but changing the reference counter changes object identity and requires a
new writer-recipe identifier. The first counted recipe is
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v3`.

Plain, gzip, and zstd JSONL enter the same bounded adapter pipeline. The JSONL
adapter requires a top-level string `text` field, ignores unknown input fields,
and streams decompression without an intermediate expanded file.

## Consequences

- Manifest size scales with published shard count, not source-file or document
  count.
- Git diffs remain reviewable and match the established public-index model.
- Static processing prose and nested composition metadata are not repeated in
  every corpus manifest.
- Shard and corpus summaries again report useful, interpretable token totals.
- Canonical Parquet stays reusable across training tokenizers.
- Token counting adds CPU work during assembly but no second corpus copy.
- Raw acquisition details required for a future audit format need a separate,
  explicitly designed artifact; they cannot silently grow the Git manifest.
- JSONL records without a string `text` field fail instead of guessing a
  provider-specific mapping.
