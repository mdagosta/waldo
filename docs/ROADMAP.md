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
- Schema-1 directory index reader
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
- Retained content-addressed cache plus disposable download scratch with
  atomic verified materialization
- Anonymous HTTP and S3 lookaside reads plus local-file fixtures
- Positional machine configuration for scratch, staging, publication, and ordered read mirrors
- `waldo index verify --objects`
- Recursive header-only canonical-object availability and size checks, with an
  explicit `--offline` structural mode
- `waldo lookaside status` and cache scrubbing
- Native shard export with safe resume and `EXPORT.json`
- Streaming canonical JSONL export with record and text-hash validation
- Recursive, hash-verified submanifest expansion with aggregate validation
- Stable corpus export BOM contract with offline `bom show` and `bom verify`
- Real-index OpenWALDO BOM acceptance tests

Exit: a selection from the public index can be materialized with every object
hash checked and an independently readable OpenWALDO BOM.

## Phase 3: corpus contribution — complete

Implemented foundation:

- Empty schema-1 index initialization
- Ingestion and training-data contract (`docs/INGESTION-DESIGN.md`)
- Basic text and Markdown ingestion
- Streaming plain, gzip, and zstd JSONL ingestion
- Canonical records and deterministic Parquet packing
- License partitioning
- Bounded progress event stream for probe, conversion, shard, upload, and purge
- Parallel S3 lookaside publication with remote verification and backpressure
- Journal-before-purge staged-object reclamation
- Manifest and navigation generation
- Durable staging and recovery
- DCO-oriented Git handoff
- Strict YAML/JSON ingest recipe detection without a separate command
- Explicit sequential external fetcher execution into WALDO-owned temporary input
- Compact aggregate source identity plus a recipe repository/commit/path collector pin
- Successful prepared-source purge and verified retry state

The direct and recipe-driven end-to-end contribution paths are complete.
Source-specific fetchers remain outside this repository; a separate project
provides reviewed shell scripts and recipe files. Scripts stop after writing
to WALDO's supplied temporary directory.

Exit: a directory of source documents becomes a reviewable, reproducible index
contribution through one command.

## Phase 4: model lifecycle with a fake backend — complete

Implemented foundation:

- Model identity and immutable architecture
- Strict declarative YAML/JSON model composes
- Ordered curriculum, architectural, and transparent resource validation
- Direct index selection, canonical shard audit, and OpenWALDO BOM attachment
- Durable planned/running/complete/failed/interrupted run state machine
- Deterministic fake backend with completion, failure, and interruption tests
- Immutable architecture plans plus model and run OpenWALDO BOMs
- Named-model `init`, `list`, `summary`, `bom`, `train`, `compose`, `export`,
  `chat`, and `rm` command surface; chat remains capability-gated until real
  weights exist
- Direct index-backed training selections through the verified shard cache
- Training-stage classification in the compose and run BOM
- EU GPAI training-content gap analysis and fail-closed
  `bom export --format eu-gpai`

The current GPAI export is a versioned machine-readable mapping and gap report.
Filling the Commission's official editable Word file remains a separate
renderer slice: WALDO must transform the exact pinned official artifact, not
present a similar-looking document as the official template.

Exit: orchestration and provenance work end to end without Python or a GPU.

## Integrity slice before a real training backend

- Stream-read every newly written canonical shard before publication and
  reject unreadable Parquet, schema drift, invalid record identities, content
  hash mismatches, and incorrect document or token totals. **Implemented.**
- Add recursive `waldo index audit <path>` for semantic validation of all
  referenced canonical records after normal object retrieval and hash checks.
  **Implemented.**
- Add local `waldo shard summary`, `waldo shard audit`,
  `waldo shard list-records`, and `waldo shard export-record` commands for
  exported or otherwise local canonical Parquet files and directories.
  **Implemented.**
- Reuse one canonical record reader and invariant checker across ingestion,
  index audit, local shard tooling, export, and the future training backend.
  **Implemented for both established and tokenizer-neutral schema-1 physical
  layouts.**
- Separate retained, verified shard caching from disposable partial-download
  scratch, with bounded retention and explicit machine configuration.
  **Implemented.**

Exit: bytes admitted to an index are not merely reachable and hash-identical;
they are proven readable as canonical WALDO records, and operators can inspect
the same files independently of an index checkout.

## Robustness gate before real training — complete

- Adversarial shard tests for hashes, tokens, metadata, required fields,
  footer totals, duplicates, cancellation, truncation, and physical schemas
- Recursive path, glob, and deterministic deduplication tests
- Cache retention, LRU eviction, corruption repair, and partial-download cleanup
- Manifest-versus-streamed-total validation through `index audit`
- Complete raw-input through fake-model and EU GPAI lifecycle E2E
- Guarded S3 write and public-index read/audit entry points
- Live audit of the real Foodista corpus using the established schema-1 layout

## Phase 5: real training backends — complete

- Add `waldo model forecast <compose-or-index-path...>` before any resource allocation.
  **Implemented.**
- Forecast runtime and memory across exact Apple, NVIDIA, and AMD accelerator
  profiles, including viable 1, 4, and 8 accelerator configurations per node.
  **Implemented with a versioned planning catalog; empirical calibration is
  ongoing.**
- Resolve multiple direct index paths through the current or configured index,
  deduplicate their corpora, recommend a model rung, and forecast one pass.
  **Implemented.**
- Select MLX automatically on Apple Silicon. **Implemented for the built-in
  byte tokenizer with verified Metal runtime discovery.**
- On Linux, prefer an installed TorchTitan and then an installed PyTorch.
  **Implemented with a single-node TorchTitan distributed adapter and a
  single-process PyTorch fallback.**
- Persist the resolved backend and immutable environment facts in each run BOM.
  **Implemented for MLX, PyTorch, and TorchTitan, including Python/framework
  versions, selected devices, node/world size, and accelerator facts.**
- Keep the execution adapter portable across MLX, PyTorch, TensorFlow, and
  PyTorch-based distributed engines. **Backend-neutral contract plus real MLX
  and single-process PyTorch adapters implemented, plus single-node TorchTitan
  device-mesh/FSDP2 execution; TensorFlow and multi-node orchestration remain.**
- Resolve compact compose parameters into a versioned AdamW/cosine training
  profile, deterministic bounded shuffle, continuous EOS packing contract,
  checkpoint/evaluation cadence, and planned token capacity. **Implemented.**
- Stream canonical records through a schema-1 NDJSON worker protocol without
  exposing Parquet or index logic to framework adapters. **Implemented through
  the real MLX, PyTorch, and TorchTitan workers.**
- Persist and validate typed progress, checkpoint, evaluation, final-loss, and
  artifact observations. **Implemented and exercised through real MLX plus
  guarded PyTorch and TorchTitan lifecycles.**
- Weight checkpoints. **Implemented.** Optimizer-state resume remains.
- Actual consumption totals. **Implemented.**
- Training-loss evaluation results and output weight hashes. **Implemented;
  held-out evaluation remains.**
- Safetensors export with attached provenance. **Implemented.**

Exit: a tiny model can be rebuilt from a compose and its complete observed run
record can be inspected.

## Phase 6: useful model operations — in progress

- Real MLX chat and one-shot generation with verified current artifacts,
  persistent sessions, KV caching, safe streaming, and deterministic test
  controls. **Implemented.** Instruction tuning and chat templates remain.
- Separate native WALDO, Hugging Face, MLX, GGUF, and Ollama release packages,
  each carrying the OpenWALDO and EU BOMs. **Implemented, including live
  Ollama import/generation parity for the GGUF converter.**
- Additional execution backends **implemented with single-process PyTorch and
  single-node distributed TorchTitan on Linux.**
- Training-quality Hugging Face Safetensors download with immutable repository
  revision, source inventory, origin BOM, lossless name normalization, direct
  continued training, and pulled-base composes. **Implemented for the
  standard Llama plus OpenWALDO byte-tokenizer compatibility profile.**
- Held-out evaluation
- Fork and lineage
- Explicit additional training runs **implemented through `waldo model train`.**
- Training-content report rendering **implemented as a versioned JSON EU GPAI
  mapping; exact official editable-document rendering remains.**

General Hugging Face tokenizers and architecture profiles, SFT, preference
training, and cluster orchestration remain deferred until this smaller
lifecycle is reliable.

## Phase 7: operations and transition — pending

- Lookaside-to-lookaside replication; ordered mirror reads, scrubbing, and
  explicit object removal are implemented
- Append-only corpus updates
- Compatibility aliases where they materially help users
- Packaging and releases
- Migration guidance and website reconciliation
- Retirement criteria for the former backend
