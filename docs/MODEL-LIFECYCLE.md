# Model lifecycle

Phase 4 proves model orchestration and provenance without coupling WALDO to an
ML framework. The enabled backend is deliberately fake: it writes a small
deterministic document that states it is not trained weights. A successful
Phase-4 build proves compose resolution, corpus verification, state transitions,
and artifact hashing; it does not prove that training occurred.

All formats in this document use schema 1.

## Model compose

Model compose files are strict YAML or JSON. Unknown fields, additional YAML documents,
incomplete architecture, unsupported objectives, and ambiguous backend
revisions are rejected.

```yaml
kind: waldo-model-compose
schema: 1
name: smoke

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
    corpus: ../exports/core/EXPORT.json
    parameters:
      steps: 10
      batch_size: 2
      sequence_length: 1024
      learning_rate: 0.0003
      seed: 7
```

`corpus` currently names a native corpus export directory or its `EXPORT.json`. It is
resolved relative to the compose. WALDO verifies every exported file against
the export record before it creates the model and requires canonical Parquet
record schema 1 for the current causal-language-modeling objective.
`type` is one of `pre-training`, `fine-tuning`, `alignment`, or `other`; WALDO
carries it into the immutable plan and run OpenWALDO BOM for model-specific
training-content disclosure.

The local corpus path is not identity. The resolved corpus OpenWALDO BOM hash,
architecture, automatically resolved execution backend, parameters, and
ordered stages form the immutable build plan. Composes do not select a backend;
the same compose remains portable across supported execution environments.

## Commands

Models default to `~/.waldo/models`. Override that durable location when a
different disk is more appropriate:

```bash
waldo config set model.root /fast-disk/waldo-models
waldo model build model.yaml
waldo model inspect smoke
waldo model inspect /fast-disk/waldo-models/smoke
```

`model build` refuses to reuse an existing model name. Additional training and
replacement will have separate explicit workflows; there is no overwrite or
continuation flag hidden in the initial command.

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
            └── fake-model.json
```

- `PLAN.json` is immutable and content-identifies the initial model build.
- `RUN-BOM.json` is written before backend launch. It embeds the corpus
  OpenWALDO BOM and pins the exported files, architecture, backend, objective,
  and parameters without machine-local paths.
- `RUN.json` moves atomically through `planned`, `running`, and exactly one of
  `complete`, `failed`, or `interrupted`. Observations never replace planned
  totals.
- `MODEL-BOM.json` aggregates run-BOM hashes, terminal state, observation
  hashes, and output artifact hashes.

Timestamps and local paths are operational observations rather than inputs to
model identity.

## Resource forecast

Before allocating hardware, run:

```bash
waldo model forecast model.yaml
waldo model forecast /path/to/waldo-index/core/books
waldo model forecast core/books science/papers
```

A model compose supplies its architecture and complete training budget. Direct
index paths instead resolve one deduplicated selection, recommend the largest
model rung supported by roughly 20 tokens per parameter, and forecast one pass
over that data. Multiple paths must belong to the same checkout. Logical paths
use the current checkout or the checkout configured with:

```bash
waldo config set index /path/to/waldo-index
```

WALDO creates no model or run state. It lists only accelerator configurations
that have enough memory, sorted from slowest to fastest:

```text
GPUS  MFR     ACCELERATOR                    MEMORY/GPU  APPROX. TIME
   1  Apple   M4 Max 40-core GPU                 128 GB       48 days
   8  NVIDIA  H100 SXM                           80 GB       44 hours
```

The actual rows and times depend on the model compose. The estimate uses planned
tokens, approximate model parameters, optimizer and activation memory, device
headroom, and conservative effective throughput from a versioned hardware
catalog. Approximate time covers the complete planned workload, including a
small allowance for checkpoints, evaluation, and final artifacts. JSON output
includes the catalog revision, formula, effective throughput, required memory,
and unrounded duration used to produce the compact table.
