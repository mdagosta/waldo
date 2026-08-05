# Model lifecycle

Phase 4 proves model orchestration and provenance without coupling WALDO to an
ML framework. The enabled backend is deliberately fake: it writes a small
deterministic document that states it is not trained weights. A successful
Phase-4 build proves recipe resolution, corpus verification, state transitions,
and artifact hashing; it does not prove that training occurred.

All formats in this document use schema 1.

## Recipe

Recipes are strict YAML or JSON. Unknown fields, additional YAML documents,
incomplete architecture, unsupported objectives, and ambiguous backend
revisions are rejected.

```yaml
kind: waldo-model-recipe
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

backend:
  name: fake
  revision: builtin-fake-schema-1

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

`corpus` names a native corpus export directory or its `EXPORT.json`. It is
resolved relative to the recipe. WALDO verifies every exported file against
the export record before it creates the model and requires canonical Parquet
record schema 1 for the current causal-language-modeling objective.
`type` is one of `pre-training`, `fine-tuning`, `alignment`, or `other`; WALDO
carries it into the immutable plan and run OpenWALDO BOM for model-specific
training-content disclosure.

The local corpus path is not identity. The resolved corpus OpenWALDO BOM hash,
architecture, backend revision, parameters, and ordered stages form the
immutable build plan.

## Commands

Models default to `~/.waldo/models`. Override that durable location when a
different disk is more appropriate:

```bash
waldo config set model.root /fast-disk/waldo-models
waldo model build recipe.yaml
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

WALDO validates architectural divisibility and sequence length before any
write. It reports an approximate parameter and weight-byte count using
embeddings, decoder projections, gated MLP matrices, and normalization weights;
biases are excluded and the formula is recorded with the estimate. This is a
transparent size estimate, not a promise that a particular accelerator can
train the model. Real backend memory forecasting belongs with the backend that
knows its optimizer, activation, sharding, and precision behavior.
