# Ingestion and training-data design

Status: design proposal for Phase 3. The format identifiers and numeric tuning
values in this document are not stable until an ADR and golden fixtures accept
them.

## Goals

Ingestion must:

- detect the acquired input format from its contents and schema, not its file
  extension alone;
- convert large inputs in bounded memory and with backpressure;
- take raw Parquet directly to OpenWALDO Parquet without a JSONL or whole-file
  intermediate;
- produce reproducible objects whose exact recipe is recorded;
- preserve enough source evidence to audit every record;
- aggregate exact per-source and per-modality facts needed for reproducible
  public training-content disclosures;
- represent text, conversations, preferences, and ordered multimodal samples
  without flattening away their structure; and
- make training inputs reusable without making the durable corpus depend on one
  model tokenizer or training framework.

The last requirement creates two intentionally different artifacts.

## Two artifacts, two responsibilities

### Canonical corpus

The canonical corpus is the durable, indexed artifact. It contains normalized
records and provenance in a tokenizer-neutral representation. Every canonical
lookaside object is a self-contained Parquet file, including objects containing
image, audio, or video bytes. These objects are selected and pinned by an
OpenWALDO BOM.

The Git manifest defines what an object means: schema, source and license
assertions, conversion recipe, object hash, counts, and aggregate facts. The
lookaside contains only the corresponding Parquet bytes. There are no canonical
tar payloads, sidecar catalogs, or loose media objects.

The Git manifest is a compact object map, not an acquisition-file or record
catalog. It carries one aggregate source digest and one small entry per
published shard. Per-input paths and hashes may exist in temporary execution
state, but their count must not determine the size of the committed manifest.
Generated text manifests preserve compact shared source declarations, including
license evidence, content description/period/selection, acquisition period,
and category-specific acquisition facts. They do not repeat those facts per
file or record, nor embed input inventories. The conversion recipe names the
processing behavior, and composed acquisition is a single immutable
`converted_by.collector` reference.

Manifest and ingestion requirements for the EU GPAI public-summary projection
are specified in `docs/EU-GPAI-DISCLOSURE.md`. The core fields describe general
provenance; the Commission template is an export mapping, not the manifest
schema itself.

Canonical data is optimized for verification, projection, filtering,
recomposition, and long-term reuse. It is suitable for streaming training, but
it is not claimed to be the most efficient input to every trainer.

### Compiled training view

A compiled training view is a derived, reproducible cache. Its identity pins:

- the OpenWALDO BOM hash;
- the tokenizer artifact and exact implementation;
- objective and logical record schema;
- train, validation, and test split policy;
- context length;
- document-boundary and end-of-sequence policy;
- sequence-packing and shuffle algorithms; and
- any media decoder, resize, sampling, or augmentation that is performed ahead
  of training.

For text pretraining, the view contains contiguous token arrays and explicit
document/sample indexes, using a recorded integer width and byte order. Trainers
can memory-map or range-read these objects instead of decoding Parquet and
tokenizing the same text on every run. A backend adapter may translate this
stable view to a framework-native index without changing the canonical corpus.

The compiled view is disposable: it can always be rebuilt from its OpenWALDO
BOM and compilation recipe. It belongs to the model/training domain, never the
index domain.

This separation follows the shape used by large training loaders such as
[Megatron Core's binary data and index](https://docs.nvidia.com/megatron-core/developer-guide/latest/api-guide/core/datasets.html),
without imposing Megatron's format as OpenWALDO's permanent corpus format.

## Canonical text Parquet

### Logical unit

One Parquet row is one independently attributable logical document. The exact
UTF-8 bytes in `text` are the bytes hashed for `content_sha256`. Extraction and
normalization are named, versioned recipe steps; they are never implied by the
writer implementation.

A shard is homogeneous in:

- logical record schema and kind;
- conversion and normalization recipe; and
- token-counting recipe, when token counts are present.

Licenses and project sources may vary by row. The shard footer and manifest
record their represented sets, and the manifest records exact document and
token usage by license. Shards remain physical size-bounded packages; per-row
source evidence provides the logical attribution boundary.

### Accepted pretraining schema

| Column | Parquet/Arrow type | Required | Meaning |
| --- | --- | --- | --- |
| `content_sha256` | fixed-size binary, 32 bytes | yes | SHA-256 of the exact UTF-8 `text` bytes |
| `text` | UTF-8 string | yes | normalized training document |
| `source` | UTF-8 string | yes | concrete upstream record or artifact reference |
| `source_name` | UTF-8 string | no | normalized source identity |
| `license` | UTF-8 string | yes | effective normalized license |
| `license_raw` | UTF-8 string | no | upstream license evidence as observed |
| `language` | UTF-8 string | no | detector result, normally a BCP 47 language tag |
| `language_score` | signed 32-bit integer | no | confidence on the recipe's recorded integer scale |
| `date` | UTF-8 string | no | upstream date as observed; partial dates remain representable |
| `token_count` | signed 64-bit integer | no | count from the shard's explicitly named counting recipe |
| `meta` | UTF-8 string containing canonical JSON | no | source-specific evidence not promoted to a core column |
| `email_addresses` | boolean | yes in schema 2 | `true` when WALDO's pinned detector found an email-shaped string in `text`; the text is not changed |
| `repetitive_content` | boolean | yes in schema 2 | `true` when WALDO's pinned token n-gram detector exceeds its within-document threshold; the text is not changed |
| `boilerplate_content` | boolean | yes in schema 2 | `true` when WALDO's pinned duplicate-line or duplicate-paragraph detector exceeds its threshold; the text is not changed |
| `main_content` | boolean | yes in current schema 2 | Recipe-declared primary-content classification; defaults to `true` when no mapping exists, including older schemas |

For recipe-driven whole-file text and Markdown inputs, `meta.source_path`
preserves the validated acquisition-relative path. This keeps file-level
attribution after temporary recipe output is purged without expanding the Git
manifest into a per-file inventory.

Null means absent. Empty strings and zero values must not be overloaded as
absence. `content_sha256` is binary in the physical schema to avoid storing a
64-byte hexadecimal rendering in every row; user-facing JSON renders it as
lowercase hexadecimal.

`token_count` is a measurement, not universal truth. Its tokenizer/counter and
version are shard facts. It is useful for corpus accounting but does not replace
the token IDs in a compiled training view.

During assembly, WALDO counts each retained document with the offline embedded
`tiktoken/cl100k_base` reference counter. It stores that informational estimate
in the existing nullable per-row `token_count` column and aggregates it into
each compact manifest shard entry. The manifest's
`converted_by.tokenizer` makes those estimates interpretable. Canonical rows
remain training-tokenizer neutral because they contain text rather than token
IDs; changing the reference counter does change object identity and therefore
requires a new recorded writer recipe.

Record schema 2 is the current canonical logical contract. Schema-1 objects
remain valid, readable, and unassessed; they are not rewritten. New ingestion
always evaluates every retained row with `waldo/email-address-v1`, writes the
boolean result, and records detector identity plus flagged-record counts in the
shard footer, embedded shard BOM, manifest shard, and manifest aggregate. The
detector recognizes common Internet email-shaped strings. It does not determine
whether an address identifies a natural person or establish a legal conclusion.

Because canonical objects are immutable, adding this column does not migrate
existing shards. A corpus is upgraded only by an explicit rebuild. If a
compose requests an assessment exclusion across schema-1 shards, WALDO warns,
retains those unassessed rows, and applies the exclusion normally to schema-2
rows. All non-assessment filter conditions continue to apply.

### Other logical record kinds

New objectives get distinct logical schemas rather than being flattened into a
pretraining `text` column:

- supervised fine-tuning preserves an ordered list of typed messages;
- preference data preserves the shared prompt/messages and the chosen and
  rejected responses;
- multimodal/interleaved data preserves an ordered list of text and media
  parts; and
- task-specific additions use a new schema or typed optional field, not an
  undocumented shape inside `meta`.

The common identity, source, license, and evidence vocabulary is shared. The
payload is schema-specific.

### Physical encoding recipe

The first implementation uses these values. ADR 0010 records the writer
identity and initial benchmark; broader fixtures remain a release gate for
remote contribution:

| Property | Proposed value | Reason |
| --- | --- | --- |
| Compressed shard target | 256 MiB, soft | useful transfer/parallelism unit without excessive object counts |
| Compressed shard maximum | 512 MiB except one oversized row | bounds retry and cache cost |
| Row-group target | 64 MiB logical bytes | bounded writer memory and useful scan parallelism |
| Data-page target | 1 MiB | avoids the old coarse 4 MiB pages while retaining sequential throughput |
| Compression | Zstandard level 6 | throughput-oriented default; exact encoder/version is pinned |
| Parquet page version | data page v2 | modern level/value encoding and page compression behavior |
| Dictionary encoding | only low-cardinality columns | useful for license/language/source name, harmful for text and hashes |
| Row order | acquisition order in streaming mode; hash order in canonical mode | makes the chosen reproducibility contract explicit |

Apache Parquet defines row groups as the parallelization unit, column chunks as
the I/O unit, and pages as the encoding/compression unit. The physical recipe
uses those boundaries deliberately rather than treating a Parquet file as an
opaque blob. See the [Parquet concepts](https://parquet.apache.org/docs/concepts/)
and [configuration guidance](https://parquet.apache.org/docs/file-format/configurations/).
The latter's very large row-group guidance is HDFS-oriented; OpenWALDO must also
bound memory and support local files and object-store range reads.

Only low-cardinality columns receive dictionaries. Statistics are written for
columns where they are useful. Page indexes and a hash bloom filter are enabled
only if the chosen Go reader/writer proves they improve OpenWALDO operations;
they add little to a trainer that projects and scans only the payload.

The exact schema, column order, encodings, compression implementation, writer
version, metadata ordering, and size rules form the recipe identity. Golden
files verify byte stability on supported platforms.

### Reader memory and small-system load

Shard size is a transfer, cache, and scheduling unit; it is not the reader's
required resident memory. A conforming loader never reads a 256--512 MiB file
into one byte slice. It opens or memory-maps the file, projects required columns,
and decodes bounded batches from one row group. A memory map reserves address
space but file pages become resident only as they are touched, and the operating
system can reclaim its page cache.

With 64 MiB row groups and 8--32 MiB decoded batches, a single text reader
should normally remain in the low hundreds of MiB, including offsets, decode
buffers, and one prefetched batch. Four concurrent readers with prefetch should
be budgeted at roughly 0.5--2 GiB until measurements replace that conservative
range. Media decoding can use much more than Parquet reading: a compressed image
or video may expand many times when converted to pixels or tensors. The loader
therefore budgets decoded media separately and limits samples, frames, and
prefetch depth.

On a 128 GiB Apple Silicon system, the proposed shard size is not itself a
memory concern. A 512 MiB object is 0.4 percent of physical memory and normally
only a row-group/batch fraction is resident for the loader. Unified memory does
mean every loader allocation reduces memory available to model weights,
activations, and optimizer state, so the ingestion format does not authorize
unbounded data workers. The first loader should expose a memory budget, default
to no more than a few GiB on that machine, and reduce concurrency rather than
exceed it.

The acceptance benchmark must record peak resident memory for one and four
readers, with and without prefetch, and include text, images, and video clips.
It must also prove that peak reader memory is governed by configured batch and
media limits rather than total shard size.

### Assembly and publication

The writer writes directly to a temporary file through a SHA-256 calculating
writer. It never builds a complete shard in a byte slice.

1. Append typed record batches to the active row group.
2. Flush the row group at its logical target.
3. Observe encoded bytes after each flush and rotate at the soft shard target.
4. Close the footer, synchronize the file, and finish the object hash.
5. Reopen and verify schema, metadata, counts, and readable row groups.
6. Enqueue the verified object for bounded parallel publication.
7. Verify remote size and SHA-256, journal the object, then purge staging.

The initial assembler implements steps 1--5 in this boundary. It streams into
a temporary file through a SHA-256 writer, uses the Parquet writer's encoded
size estimate after explicit row-group flushes, closes and synchronizes the
file, re-hashes and reopens it, checks the exact schema/footer/counts, and then
renames the staged object to its digest. Publication is a separate journaled
transaction so a partially generated manifest can never expose an uncommitted
object set.

A single record larger than the normal limit receives its own shard and an
oversize fact. It is never silently truncated.

## Direct, scalable conversion

### Detection and planning

`waldo ingest` begins with a read-only probe. Detection uses magic bytes,
container metadata, Parquet footers, compression headers, and logical schema.
An extension is only a hint. The probe produces a plan containing:

- acquired artifacts and their verified hashes;
- detected containers and compression;
- candidate logical record mapping;
- projected source columns;
- normalization, license, language, and token-count recipes;
- expected partitions and resource estimate; and
- every ambiguity that requires an explicit mapping.

Execution records the accepted plan. Resume refuses to combine checkpoints with
changed input hashes, mappings, recipes, or writer versions.

### Bounded batch pipeline

Adapters emit typed columnar batches through a small internal batch interface.
They do not create an untyped `map[string]any` for every row. The implementation
may use Arrow buffers where that provides real projection or zero-copy benefits,
but WALDO's domain contract must not be an Arrow API.

The initial text-family adapter treats each file as one document, targets
16 MiB typed batches, and defaults to rejecting a single record larger than
64 MiB. A reviewed recipe may raise the indivisible-record ceiling to at most
256 MiB, still bounded by half the plan memory budget. These limits are
recorded in the immutable plan. Splitting large text streams remains a
separate, explicit transformation because line, paragraph, and byte-window
boundaries produce materially different training records and content hashes.

```text
probe -> read projected batch -> map/extract -> derive/validate
      -> partition/deduplicate -> encode/hash -> verify/admit -> journal
```

Every edge is bounded and propagates backpressure. Concurrency is computed from
an explicit memory budget and measured batch/writer cost, not from CPU count
alone. The principal controls are batch bytes, open row groups, derive workers,
active output partitions, and in-flight encoded objects.

Partition routing is by record kind and writer recipe, not by project or
license. One bounded active writer packs rows until the physical target size;
its footer records the represented source and license sets. This prevents rare
projects or licenses from producing undersized shards.

### Raw Parquet fast path

Raw Parquet is never loaded as one `[]byte` and never expanded to JSONL.

1. Open the source with `ReaderAt` or object-store range reads.
2. Read its footer and map the physical schema to a logical input schema.
3. Project only required source columns.
4. Read row groups as bounded batches.
5. Reuse compatible column buffers where practical; derive hashes, normalized
   values, language, and token counts in bounded parallel stages.
6. Append the resulting typed batches directly to canonical output writers.

The initial implementation re-hashes the planned file, opens its footer through
`ReaderAt`, constructs a one-column Parquet schema projection, and reads scalar
rows directly into the canonical typed-batch boundary. It currently accepts a
flat scalar text column only. Nulls, nested mappings, invalid UTF-8, NUL bytes,
and oversized values are explicit errors rather than coercions or dropped rows.

Parquet column projection and dataset scanning are standard Arrow capabilities;
see the [PyArrow Parquet documentation](https://arrow.apache.org/docs/python/parquet.html).
The Go implementation receives its own acceptance benchmark rather than
assuming identical performance.

Column chunks can be copied without decoding only when the source's logical and
physical schema, order, encodings, row-group boundaries, metadata, and exact
writer recipe already match the canonical recipe. In that exceptional case the
object is verified and adopted or copied verbatim. Otherwise decoding and
re-encoding are necessary, but still happen in one bounded streaming pass.

### Other adapters

- JSONL is scanned one record at a time with an explicit maximum record size.
  The initial adapter accepts plain, gzip, and zstd streams, requires a
  top-level string `text` field, ignores unknown fields, and never materializes
  a decompressed intermediate.
- CSV uses a streaming parser and an explicit dialect/schema; guessing is part
  of the probe, not silent execution behavior.
- WARC and archive formats stream entries and bound decompressed bytes.
- Compressed streams use bounded decompressors and defend against expansion
  bombs.
- Media directories and archives stream payloads while computing hashes and
  technical metadata.

### Deduplication and deterministic sharding

There are two useful modes, and the CLI and recipe must name which one ran.

#### Streaming mode

Streaming mode is the normal direct-conversion path. It preserves the artifact
and row order pinned in the ingestion plan. A disk-backed hash set can
perform exact global content deduplication while retaining the first occurrence;
only hashes and evidence pointers require scratch storage, not a second copy of
the corpus. Shards rotate in that stable order.

The same inputs, acquisition order, plan, memory-independent boundaries, and
writer recipe produce the same objects. Worker completion order must never
change emitted order.

The first executable slice enables only this streaming mode. A plan may
preflight `canonical`, but execution refuses it until the external hash-sort
stage and its scratch forecast are implemented; it never silently substitutes
acquisition order.

The streaming implementation uses a build-local Bolt database keyed by the
binary 32-byte content hash and commits one database transaction per typed
batch. This database is scratch, not a lookaside object or Git artifact. An
unjournaled restart rebuilds it from the immutable inputs so a hash cannot be
considered retained before its containing object is checkpointed.

#### Canonical mode

Canonical mode makes output independent of acquisition order and worker count.
It routes records by a prefix of the content hash, writes compact typed runs,
externally sorts each partition by the full identity key, deduplicates, and
greedily forms shards. Workers can own independent hash partitions and a
coordinator needs to merge only runs and metadata.

This mode necessarily uses scratch record data for unordered input. There is no
general algorithm that simultaneously provides arbitrary input order, global
deduplication, order-independent output shard hashes, and no external storage.
WALDO exposes that cost instead of hiding a massive JSONL intermediate.

An already sorted canonical deposit can take a k-way merge fast path and avoid
the external sort. This is the large-columnar optimization that a future
external-fetcher handoff contract should support.

### Recovery and distributed execution

The durable journal records input artifact, row-group/range, logical sequence,
completed object hashes, and output partition state. Recovery resumes only from
a boundary whose published outputs and recipe still verify. A crash may repeat a
bounded batch, but must not publish a partial object or duplicate a record.

`INGESTION.json` schema 1 atomically records the immutable plan identity,
assembly status, exact deduplication totals, and verified staged objects. An
`assembled` restart re-hashes and reopens every named object before reuse. A
changed plan, inconsistent totals, path escape, or corrupt object is refused.
An interrupted `assembling` state removes only WALDO-owned temporary shard
files, rebuilds scratch deduplication state, and deterministically resumes from
the immutable inputs and any content-addressed completed objects.

Normal contribution uses a bounded producer/uploader pipeline. Each finalized
and verified shard enters a small upload queue while assembly continues. A
configured number of workers upload content-addressed objects in parallel;
when the queue is full, backpressure stops assembly from consuming unbounded
disk. After remote size and checksum verification, the coordinator journals
the remote object, synchronizes the journal, removes the staged shard, and
synchronizes the staging directory. Ingestion does not populate the read cache
unless the user explicitly requests local retention. A manifest overlay is not
created until every referenced remote object has been verified.

Progress is a structured event stream shared by the terminal and `--json`
frontends. Events identify the phase, input path and row group where applicable,
logical and encoded byte counts, shard sequence and digest, upload worker and
remote destination, and bytes reclaimed by purge. Human output is rate-limited
and redraws concise status; machine output is newline-delimited JSON. Neither
mode requires parsing log prose to recover execution state—the journal remains
authoritative.

For distributed conversion, immutable work units are input artifact ranges or
canonical hash partitions. Workers return verified objects plus facts;
publication of the contribution remains a separate atomic coordinator step.
Remote execution is not required for the first implementation, but local file
formats and identities must not preclude it.

The local contribution step writes a minimal overlay: the new schema-1
YAML manifest with additive provenance for record schema 1, its leaf `index.yaml`, and only the
ancestor directory indexes that change. It validates the overlay against the
same manifest contract used to read the public index and is idempotent only
when every staged byte still matches. It never edits Git, uploads to the
declared public object base, commits, pushes, or opens a pull request.

Existing-corpus updates pin the current manifest's byte SHA-256 in the
ingestion plan. Append mode verifies and scans existing canonical shards into
the same exact disk-backed content-identity set used for within-run
deduplication, then appends only newly admitted objects and source evidence.
Complete `--rebuild-shards` mode instead treats the supplied recipe output as
the authoritative corpus and never reads old shard bodies. It replaces shard
and source arrays after normal publication verification. Both modes write the
touched metadata as schema-1 YAML and preserve old lookaside objects by
default.

## Multimodal material

Image, audio, and video samples are also self-contained Parquet files. They use
a multimodal logical schema rather than the text-pretraining schema. One row is
one independently attributable logical training sample and contains an ordered
`parts` list. Each part contains exactly one of:

- UTF-8 text; or
- binary media bytes together with their SHA-256, MIME type, byte size, and
  applicable facts such as dimensions, duration, sample rate, or frame rate.

The common row envelope contains sample identity, source evidence, license,
kind, and canonical JSON metadata. The sample identity covers the ordered part
descriptors and payload hashes. The exact nested Parquet schema and identity
encoding require their own ADR and golden fixtures.

Original acquired media bytes are embedded in the binary leaf column. A
normalized rendition is a different record whose recipe and parent payload hash
are asserted by its Git manifest. There are no tar bundles, loose payloads, or
authoritative Parquet sidecars.

Already-compressed JPEG, PNG, audio, and video values should normally use an
uncompressed Parquet column chunk; wrapping those bytes in Zstandard commonly
adds CPU with little size reduction. Text and structured leaf columns can still
use Zstandard because Parquet selects compression per column chunk. The exact
codec choices are benchmarked and form part of the writer recipe.

Multimodal row groups are bounded by encoded payload bytes as well as logical
record bytes. A value larger than the row-group target occupies its own row
group. An oversized sample occupies its own shard. Extremely large videos must
be converted into independently attributable training clips or use a future
chunked-media schema; the ingest command must never split or truncate one
silently.

The same 256 MiB soft and 512 MiB maximum shard policy applies initially. For
audio and video, shard construction also observes duration or decoded-frame
cost so equal byte sizes do not create pathological training-worker imbalance.

A compiled multimodal training view may precompute clips or media tokens, but
it is a model-specific local artifact rather than a different canonical
lookaside representation. Its identity pins every transform exactly as the text
training view pins the tokenizer and packing recipe.

## Training pathway

The intended path is:

```text
acquired artifacts
    -> canonical corpus objects
    -> index contribution
    -> OpenWALDO BOM selection
    -> compiled training view
    -> backend sample loader
```

Corpus ingestion performs extraction and durable normalization once. Training
view compilation performs tokenizer- and objective-specific restructuring once
per exact recipe. Individual runs then read ready-to-train samples directly.

For text, each compiled shard should contain:

- a contiguous little-endian token array (`uint16` or `uint32`, recorded);
- document offsets and source-record identities;
- packed-sample offsets/mappings for the selected context length;
- split and deterministic shuffle metadata; and
- a small manifest containing all parent hashes and compiler facts.

The document index preserves the ability to report actual consumed source
records and tokens in the training run record. Packing must not erase
provenance.

For multimodal training, the loader projects the ordered parts and embedded
payload columns directly from Parquet. A compiled view adds only transforms that
are model-specific.

## Verification and observability

Every ingest reports and journals:

- input bytes/rows and rejected rows;
- output documents, logical bytes, encoded bytes, and objects;
- per-stage time, throughput, queue occupancy, and peak accounted memory;
- deduplication counts and reason;
- partition/license totals;
- per-source and per-modality samples, items, tokens, duration, and content
  bytes where applicable;
- compression ratio and row-group/page counts; and
- warnings for oversized records, ambiguous mappings, replacement characters,
  truncation requests, or unsupported fields.

No silent row drops, truncation, encoding repair, schema coercion, or license
fallback is permitted. Rejections are a versioned artifact with source
coordinates and machine-readable reasons.

## Implementation sequence

1. Lock logical text schema 1 and the conversion-recipe vocabulary in an ADR.
2. Build a benchmark harness before choosing the Go columnar implementation and
   final physical tuning values.
3. Implement the typed batch boundary, probe/plan contract, and Markdown/text
   adapters.
4. Implement a direct raw-Parquet adapter and prove bounded memory on a generated
   input substantially larger than the configured memory budget.
5. Add deterministic partitioning, disk-backed deduplication, journaling, and
   crash/restart tests.
6. Lock the writer recipe with cross-platform golden files, then enable
   verified S3 publication and contribution generation.
7. Specify compiled text training views with the first real backend, so the
   supposedly direct format is validated by an actual loader.
8. Add the self-contained multimodal Parquet schema only with an end-to-end
   image or audio fixture and loader.

## Accepted decisions

The initial implementation accepts:

1. canonical corpus versus compiled training view as separate artifact classes;
2. streaming acquisition-order objects as the normal mode, with order-independent
   canonical sorting as an explicit scratch-using mode;
3. the proposed 256 MiB Parquet target, 64 MiB row groups, 1 MiB pages, and
   Zstandard level 6 as benchmark starting points; and
4. self-contained Parquet as the only canonical lookaside object format,
   including embedded multimodal payloads.
