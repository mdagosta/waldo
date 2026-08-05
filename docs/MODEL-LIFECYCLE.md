# Model lifecycle

The model lifecycle separates a stable architecture from an append-only history
of explicit training runs. The current backend is deliberately fake: it proves
index resolution, shard verification, state transitions, BOM persistence, and
artifact hashing without claiming to produce trained weights.

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
42. Exact or multi-stage parameters belong in a model compose.

`model bom` writes JSON to standard output unless an output file is supplied.
`model export` requires a new destination directory because a model contains
multiple artifacts and provenance records. `model rm` accepts only exact model
names. `chat` currently validates the model and then reports that no
chat-capable real weights exist; MLX is the next backend slice.

## Model compose

A model compose is strict YAML or JSON. The command supplies the local model
name, so the portable file contains no name and can be reused:

```yaml
kind: waldo-model-compose
schema: 1

architecture:
  family: decoder-transformer
  context_tokens: 2048
  vocabulary_size: 32000
  hidden_size: 768
  intermediate_size: 2048
  layers: 12
  attention_heads: 12
  key_value_heads: 12
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: example-tokenizer
    revision: sha256:replace-with-immutable-tokenizer-identity

stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora:
      - core/books
      - core/common-pile/foodista
    parameters:
      steps: 10000
      batch_size: 2
      sequence_length: 1024
      learning_rate: 0.0003
      seed: 7
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
- `MODEL-BOM.json` aggregates run-BOM hashes, terminal states, observation
  hashes, and artifact hashes.

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
verified BOM, local content-addressed shard paths, parameters, artifact
directory, and progress sink.

The deterministic fake adapter is the only enabled backend today. MLX,
streaming tokenization and packing, checkpoint/resume, evaluation, real
consumption observations, and Safetensors export are Phase 5 work.
