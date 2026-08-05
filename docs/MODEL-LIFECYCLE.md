# Model lifecycle

The model lifecycle separates a stable architecture from an append-only history
of explicit training runs. On Apple Silicon, WALDO automatically discovers a
Metal-capable Python installation of MLX and performs real decoder training.
It never silently substitutes simulation for training.

All durable formats in this document use schema 1.

## Machine configuration

Logical corpus paths use one configured index checkout. Model state and the
verified shard cache have independent locations:

```bash
waldo config set index /path/to/waldo-index
waldo config set model.root /fast-disk/waldo-models
waldo config set lookaside.cache /fast-disk/waldo-cache
```

Defaults are `~/.waldo/models` and `~/.waldo/cache`.
`model.backend` defaults to `auto`. On macOS it selects MLX and requires Apple
Silicon. On Linux it probes Python environments in deterministic order,
preferring an installed TorchTitan and then an installed PyTorch. It never
falls back to simulation. `mlx`, `torchtitan`, and `pytorch` are explicit
machine-local overrides; `fake` is an explicit simulation mode for development
and automated lifecycle tests whose artifacts are permanently marked as
simulated.

Backend resolution happens before a run record is created. A missing or
unusable selected backend writes a warning to standard error, then fails with
platform-appropriate official installation guidance. The current executable
adapter is MLX. Linux discovery and selection are implemented, while the
TorchTitan and PyTorch execution adapters remain roadmap work; detecting an
installed framework therefore reports that adapter gap explicitly.

## Basic commands

```bash
waldo model init small --preset 10m
waldo model list 'small*'
waldo model summary small
waldo model train small core/books science/papers
waldo model bom small
waldo model export small ./small-export
waldo model chat small
waldo model rm small
```

`init` creates an untrained immutable architecture. `train` resolves one or
more recursive index selections, deduplicates them into an OpenWALDO BOM,
materializes hash-verified Parquet through the shared cache, audits every
canonical record, and appends one run. Its current compact default is one pass,
batch size 8, the architecture context length, learning rate 0.0003, and seed
42. `--epochs <n>` controls complete passes over the selected records and
defaults to 1. For the built-in byte tokenizer, WALDO counts exact packed
byte-token targets for all epochs rather than reusing the manifest's reference
token estimate. It then reports and persists the derived optimizer-step count:

```text
steps = ceil(byte targets / (batch size × sequence length))
```

Epoch boundaries remain part of one continuous-EOS token stream, while each
epoch gets a deterministic seed-derived shuffle. Exact low-level or multi-stage
parameters belong in a model compose.

`model bom` writes JSON to standard output unless an output file is supplied.
`model export` requires a new destination directory because a model contains
multiple artifacts and provenance records. Its default `waldo` package exposes
the portable aggregate as `BOM.json` and always adds `EU-BOM.json`; the managed
model retains the internal `MODEL-BOM.json` name. Configure the provider once
with `waldo config set disclosure.provider provider.json`. A normal export
fails before publication if required disclosure facts are absent, while
`--allow-incomplete` writes a conspicuously marked development draft. When
`signing.method` is configured, export automatically signs both BOMs and fails
if signing fails. Otherwise it succeeds with an unsigned warning. `model rm`
accepts only exact model names.

`--format huggingface` exports the current verified, complete, non-simulated
run as a standalone Transformers package. WALDO rewrites only the Safetensors
header: tensor bytes remain unchanged while names move to the standard Llama
layout and container metadata identifies PyTorch. The package includes
`architecture.py`, the schema-1 byte tokenizer implementation and
configuration, `BOM.json`, and `EU-BOM.json`. The tokenizer is custom code, so
Transformers callers load it with `trust_remote_code=True`; the model itself
uses the standard Llama configuration. A model without a usable real run is
rejected rather than exporting simulated or incomplete artifacts.

`--format mlx` emits the same standard Llama tensor names with Safetensors
metadata for MLX, an executable binding to MLX-LM's Llama model, and the same
explicit byte tokenizer. It is a separate package, not a second copy bundled
into the Hugging Face export. Tensor data is again copied byte-for-byte; only
the container header and surrounding runtime files differ.

`model chat` opens the newest complete real-weight run identified by
`current_run_id`, verifies its weights, configuration, and tokenizer against
the model BOM, and then uses the backend recorded by that run. MLX sessions
load weights once and use incremental key/value caching while generating:

```bash
waldo model chat small
waldo model chat small "Once upon a time"
printf 'Once upon a time' | waldo model chat small
waldo --json model chat small "Once" --max-tokens 64 --temperature 0 --seed 7
```

No generation option is required. Defaults are 256 maximum tokens,
temperature 0.8, and top-p 0.95. A zero temperature is deterministic; `seed`
makes sampling reproducible. Interactive sessions support `/clear`, `/help`,
and `/exit`. Generated terminal bytes stream incrementally, but control and
invalid UTF-8 bytes are escaped so model output cannot emit terminal control
sequences. JSON is one-shot and includes model and run identity, prompt, text,
token count, finish reason, and generation duration.

The built-in byte-tokenizer models are causal pretraining models and carry no
chat template. Interactive mode therefore performs raw continuation;
instruction-following behavior requires later supervised fine-tuning and a
pinned chat template. Generation is ephemeral and does not mutate lifecycle
state or claim a new BOM observation.

The first MLX slice supports `decoder-transformer` architectures pinned to the
built-in `byte@builtin-byte-schema-1` tokenizer with vocabulary size 259. This
includes the 10m, 35m, and 90m presets. Unsupported tokenizers fail during
backend resolution before a run record is created. WALDO probes candidate
Python runtimes, verifies an actual MLX operation on Metal, and records the
selected Python path, Python version, MLX version, Apple accelerator, and
memory in the run BOM.

## Model compose

A model compose is strict YAML or JSON. The command supplies the local model
name, so the portable file contains no name and can be reused:

```yaml
kind: waldo-model-compose
schema: 1

architecture:
  family: decoder-transformer
  context_tokens: 2048
  vocabulary_size: 259
  hidden_size: 384
  intermediate_size: 1024
  layers: 6
  attention_heads: 6
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: byte
    revision: builtin-byte-schema-1

stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora:
      - core/books
      - core/common-pile/foodista
    parameters:
      profile: causal-pretrain-v1
      steps: 10000
      batch_size: 2
      sequence_length: 1024
      learning_rate: 0.0003
      seed: 7

      # Optional overrides; omitted values resolve from the profile.
      weight_decay: 0.1
      warmup_steps: 100
      checkpoint_every: 500
      evaluate_every: 500
      shuffle_buffer_records: 1024
      shuffle_buffer_bytes: 67108864
```

Run it with:

```bash
waldo model compose example model.yaml
```

Unknown fields, additional YAML documents, incomplete architecture, unsupported
objectives, empty corpus selections, duplicate stage names, and invalid
parameters are rejected. Corpus values are index paths, never raw directories
or corpus exports. Explicit paths discover their checkout; logical paths use
the current or configured checkout.

An existing name is refused. Explicit replacement uses one flag:

```bash
waldo model compose example model.yaml --replace
```

WALDO resolves and audits every stage and builds the replacement in a sibling
temporary directory. The old model remains intact if parsing, preflight,
backend resolution, or training fails. Only a complete replacement is swapped
into the configured model root.

## Durable layout

```text
<model.root>/<name>/
├── PLAN.json
├── MODEL.json
├── MODEL-BOM.json
└── runs/
    └── 0001-<stage>-<run-id>/
        ├── RUN-BOM.json
        ├── RUN.json
        └── artifacts/
```

- `PLAN.json` content-identifies the immutable architecture and local model
  name. Adding training does not change model identity.
- `RUN-BOM.json` embeds the resolved corpus OpenWALDO BOM and pins architecture,
  backend, objective, parameters, and execution environment before launch.
- `RUN.json` moves atomically through `planned`, `running`, and exactly one of
  `complete`, `failed`, or `interrupted`.
- `MODEL-BOM.json` aggregates run-BOM hashes, terminal states, backend and
  simulation identity, observation hashes, and artifact hashes. Its
  `path_base` is `model-root`: every `run_bom` and artifact `path` resolves
  from the directory containing `MODEL-BOM.json` in a managed model, or
  `BOM.json` in a model export. Paths are portable and never contain a
  machine-specific model root.
  `current_run_id` selects the newest complete, non-simulated run containing
  real weight artifacts; earlier simulated and real runs remain visible as
  provenance.

Every aggregate artifact has a role such as `weights`, `configuration`,
`tokenizer`, or `simulation`. `model export` rewrites any accepted legacy
schema-1 aggregate BOM into this unambiguous form and verifies the bytes,
sizes, and SHA-256 hashes of terminal and checkpoint artifacts before
publishing the exported directory.

Machine-local index roots and cache paths never enter identity. Run BOMs retain
logical index paths, manifest and shard hashes, licenses, source evidence, and
the index Git identity when available.

## Resource forecast

```bash
waldo model forecast model.yaml
waldo model forecast /path/to/waldo-index/core/books
waldo model forecast core/books science/papers
```

A compose supplies exact architecture and training budgets. Direct index paths
resolve a deduplicated selection, recommend the largest model rung supported by
roughly 20 tokens per parameter, and forecast one pass. Forecast creates no
model or run state.

Only configurations that fit are shown, from slowest to fastest:

```text
GPUS  MFR     ACCELERATOR                    MEMORY/GPU  APPROX. TIME
   1  Apple   M4 Max 40-core GPU                 128 GB       48 days
   8  NVIDIA  H100 SXM                           80 GB       44 hours
```

The estimate uses planned tokens, approximate parameters, optimizer and
activation memory, device headroom, and conservative effective throughput from
a versioned hardware catalog. JSON includes the formula and unrounded inputs.

## Backend boundary

Model composes never select MLX, PyTorch, TensorFlow, or TorchTitan. Before a
run is written, the environment-aware resolver chooses an adapter and records
its immutable identity, framework, runtime, host, accelerator, node count, and
world size in the run BOM. Every adapter receives the same architecture,
verified BOM, resolved training profile, deterministic canonical-record stream,
artifact directory, and progress sink. Adapters never parse Parquet or choose
record order. An embedded worker communicates through schema-1 NDJSON: a begin
frame, record frames from a shuffle bounded by both record count and retained
bytes, an end frame, then typed progress,
checkpoint, evaluation, completion, or error output frames.

The MLX adapter embeds its worker source in the WALDO binary while using the
machine's explicit Python and MLX runtime. It implements rotary grouped-query
attention, RMS normalization, gated feed-forward blocks, byte tokenization,
continuous-EOS packing, AdamW with warmup and cosine decay, real loss/gradient
updates, checkpoint and terminal Safetensors, progress, training-loss metrics,
and observed token totals. A later run verifies and initializes from the most
recent non-simulated terminal `model.safetensors`; its source run and weight
hash are pinned in the new run BOM. Optimizer-state checkpoint resume, held-out
evaluation, instruction tuning, and chat templates remain separate work.
