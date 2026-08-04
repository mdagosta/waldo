# Rebuild roadmap

The rebuild proceeds in vertical slices. Each phase must leave behind an
observable command, tests, and a smaller stable contract for the next phase.
Features do not move forward merely because similar code exists in the former
backend.

## Phase 0: contracts and scaffold — complete

- Product charter and non-goals
- Command vocabulary
- Bounded-domain architecture
- Compatibility policy
- Fetcher handoff boundary
- Foundational ADRs and contributor instructions
- Buildable single-binary command scaffold

## Phase 1: read-only index — complete

Implemented:

- Explicit checkout and in-tree path resolution
- Schema-2 directory index reader
- Schema-1 manifest reader
- Indexed-tree traversal
- Recursive `waldo index list` with per-corpus paths and totals
- `waldo index show`
- `waldo index summary`
- Local structural `waldo index verify`
- JSON output
- Unit fixtures and acceptance tests against the real public index

Deferred to the writing slice: regenerating and byte-comparing `index.json`.

## Phase 2: verified OpenWALDO BOMs — complete

Implemented:

- License include/exclude policy
- Immutable OpenWALDO BOM with Git, manifest, shard, source, and license pins
- Local content-addressed scratch with atomic verified materialization
- Anonymous HTTP and S3 lookaside reads plus local-file fixtures
- Positional machine configuration for scratch, staging, publication, and ordered read mirrors
- `waldo index verify --objects`
- `waldo lookaside status` and cache scrubbing
- Native shard export with safe resume and `EXPORT.json`
- Streaming canonical JSONL export with record and text-hash validation
- Recursive, hash-verified submanifest expansion with aggregate validation
- Stable corpus export BOM contract with offline `bom show` and `bom verify`
- Real-index OpenWALDO BOM acceptance tests

Exit: a selection from the public index can be materialized with every object
hash checked and an independently readable OpenWALDO BOM.

## Phase 3: corpus contribution

Implemented foundation:

- Ingestion and training-data contract (`docs/INGESTION-DESIGN.md`)
- Basic text and Markdown ingestion
- Canonical records and deterministic Parquet packing
- License partitioning
- Bounded progress event stream for probe, conversion, shard, upload, and purge
- Parallel S3 lookaside publication with remote verification and backpressure
- Journal-before-purge staged-object reclamation
- Manifest and navigation generation
- Durable staging and recovery
- DCO-oriented Git handoff

The first end-to-end contribution path is complete. Source-specific
acquisition adapters are intentionally sequenced after Phase 4; they stay in
this binary and initially write only to a local directory.

Exit: a directory of source documents becomes a reviewable, reproducible index
contribution through one command.

## Phase 4: model lifecycle with a fake backend

- Model identity and immutable architecture
- Declarative build recipes
- Curriculum and resource validation
- OpenWALDO BOM attachment
- Durable run state machine
- Fake backend for completion, failure, and interruption tests
- Model and run BOMs
- EU GPAI training-content gap analysis and `bom export --format eu-gpai`

Exit: orchestration and provenance work end to end without Python or a GPU.

## Phase 5: integrated source fetchers

- Narrow acquisition interface inside the WALDO binary
- First reviewed source adapter
- Resumable streaming downloads into an explicit local directory
- Atomic schema-1 acquisition evidence
- Reviewable handoff to `waldo index ingest`
- No implicit lookaside publication, index mutation, or model execution

Exit: one upstream source can be acquired reproducibly into a local directory
and then ingested through a separate explicit command.

## Phase 6: one real training backend

- Select MLX or PyTorch as the first backend
- Streaming shard consumption
- Checkpoint and resume
- Actual consumption totals
- Evaluation results and output weight hashes
- Safetensors export with attached provenance

Exit: a tiny model can be rebuilt from a recipe and its complete observed run
record can be inspected.

## Phase 7: useful model operations

- Second local backend
- Evaluation and chat
- Fork and lineage
- Explicit additional training runs
- Training-content report rendering

Model import, SFT, preference training, and cluster orchestration remain
deferred until this smaller lifecycle is reliable.

## Phase 8: operations and transition

- Lookaside mirroring, scrubbing, and safe garbage collection
- Append-only corpus updates
- Compatibility aliases where they materially help users
- Packaging and releases
- Migration guidance and website reconciliation
- Retirement criteria for the former backend
