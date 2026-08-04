# ADR 0010: Canonical text uses record schema 1 Parquet

- Status: accepted
- Date: 2026-08-04

## Context

OpenWALDO needs a tokenizer-neutral corpus representation that can be produced
from very large inputs with bounded memory and consumed without first passing
through JSONL. The existing schema-1 objects remain part of the compatibility
surface, but their writer recipe must not be reused for a different physical or
logical representation.

The lookaside contains only Parquet objects. Consequently, every fact needed to
interpret one logical record must either be in its Parquet row, in the Parquet
footer, or in the Git manifest that names the immutable object.

## Decision

Adopt text record schema 1. One row is one independently attributable UTF-8
document with this ordered physical schema:

1. required 32-byte `content_sha256`;
2. required UTF-8 `text`;
3. required UTF-8 `source`;
4. nullable UTF-8 `source_name`;
5. required UTF-8 `license`;
6. nullable UTF-8 `license_raw`;
7. nullable UTF-8 `language`;
8. nullable signed 32-bit `language_score`;
9. nullable UTF-8 `date`;
10. nullable signed 64-bit `token_count`; and
11. nullable canonical-JSON UTF-8 `meta`.

Null means absent; an empty string or zero never means absent. The content hash
covers the exact bytes in `text`. Extraction, normalization, language
detection, token counting, and interpretation of `meta` are named recipe facts,
not hidden behavior of the writer.

The initial physical writer identity is
`parquet-go/0.30.1/zstd-6/page-1m/rg-64m/v2`. It uses parquet-go 0.30.1,
klauspost/compress 1.17.9's mapping of Zstandard level 6, one encoder worker,
data page v2, 1 MiB target pages, 64 MiB logical row groups, and dictionary
encoding only for the low-cardinality source-name, license, language, and date
columns. Text, source, and metadata omit page bounds and page statistics.
Callers flush row groups explicitly and stream the writer into a temporary
file; the writer does not accumulate a complete shard in memory.

Shard packing uses a 256 MiB compressed soft target and a 512 MiB compressed
maximum, except when one indivisible row itself exceeds the maximum. These
packing limits are part of the ingestion recipe, while the footer records the
record schema and physical writer identity.

On the 16 MiB deterministic text fixture on an Apple M4 Max, three-run medians
were:

| Recipe | Throughput | Encoded bytes |
| --- | ---: | ---: |
| former Zstd-best, 4 MiB pages | 137 MB/s | 2,081,431 |
| Zstd-3, 1 MiB pages | 491 MB/s | 2,284,903 |
| selected Zstd-6, 1 MiB pages | 392 MB/s | 2,028,045 |

The selected recipe was about 2.9 times as fast as the former recipe and
produced a slightly smaller fixture. Zstd-3 was about 25 percent faster than
the selection but produced about 13 percent more data. This synthetic fixture
is a regression control, not a claim about every corpus.

## Consequences

- Raw Parquet can be projected into typed rows and written directly without an
  intermediate interchange format or whole-input buffer.
- Existing schema-1 objects remain readable. The exact physical representation
  is distinguished by its writer recipe and footer metadata; new objects must
  never claim an older physical recipe identity.
- Changing column order, types, nullability, encodings, relevant writer
  versions, compression settings, statistics policy, or footer semantics needs
  a new physical recipe identity. A logical column change needs a new record
  schema.
- Golden fixtures and real-corpus benchmarks are still required before remote
  contribution is enabled. A failure there results in a new recipe identity;
  it never silently changes this one.
- Multimodal and task-specific records receive separate logical schemas rather
  than sparse additions to the text schema.
