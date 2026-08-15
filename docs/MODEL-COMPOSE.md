# Model compose guide

A model compose is a strict, portable YAML or JSON document that declares a
model architecture and one or more ordered training stages. Use one when WALDO
must create a model, train several stages as one resumable transaction, or
preserve an exact experiment for later review.

The compose does not select MLX, PyTorch, TorchTitan, GPUs, paths, credentials,
or other machine-local policy. WALDO selects a compatible training backend on
the machine that runs the compose.

```bash
waldo model forecast composes/0001-babble.yaml
waldo model train babble composes/0001-babble.yaml
```

`forecast` is read-only. `train` creates the named model when it does not
exist, or appends the declared stages when the existing model has exactly the
same architecture.

## Why the profile is called causal pretraining

The name is **causal**, not casual. In causal language modeling, the model
predicts each next token using only the tokens that precede it. Future tokens
are masked. This left-to-right objective is what makes the resulting base model
able to continue a prompt.

`pretrain` in the profile name describes the general data-ordering and training
contract. It does not make the stage label `type: pre-training` mandatory; the
currently executable objective is the same for every accepted stage type.

## Complete annotated compose

This example shows every schema-1 field. The optional `base` block is commented
out because it is valid only when the named model has compatible pulled origin
weights.

```yaml
kind: waldo-model-compose
schema: 1

# Optional: initialize from either a managed pulled model or a pinned source.
# base:
#   model: llama-base
#   origin_sha256: <expected-origin-bom-sha256> # optional assertion
# base:
#   source: huggingface://organization/model@<commit>

architecture:
  family: decoder-transformer
  context_tokens: 2048
  vocabulary_size: 50259
  hidden_size: 768
  intermediate_size: 2048
  layers: 12
  attention_heads: 12
  key_value_heads: 4
  dropout: 0.1
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: tiktoken/r50k_base
    revision: tiktoken-r50k-base

stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    filter:
      licenses:
        include: [CC-BY-*, CC0-*]
        exclude: [CC-BY-NC-*]
    corpora:
      - path: core/books/gutenberg
        weight: 1
        filter:
          languages:
            include: [en]
          date:
            from: "1900"
      - path: core/common-pile/wikimedia
        weight: 2
        filter:
          sources:
            exclude: [deprecated-*]
    parameters:
      profile: causal-pretrain-weighted
      epochs: 1
      steps: 60000
      batch_size: 32
      sequence_length: 1024
      learning_rate: 0.0002
      seed: 42
      weight_decay: 0.15
      warmup_steps: 600
      checkpoint_every: 6000
      evaluate_every: 6000
      shuffle_buffer_records: 32768
      shuffle_buffer_bytes: 1073741824
      evaluation_fraction: 0.01
      evaluation_max_records: 512
      evaluation_max_bytes: 16777216
```

Unknown fields and additional YAML documents are rejected. JSON uses the same
field names and structure.

## Top-level fields

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `kind` | yes | `waldo-model-compose` | Identifies the document as a model compose. |
| `schema` | yes | `1` | Selects the compose schema. |
| `base` | no | object | Optionally initializes a new model from pulled origin weights. |
| `architecture` | normally | object | Defines immutable model structure and tokenizer identity. It may be omitted with `base.source`, in which case WALDO inherits the verified source architecture. |
| `stages` | yes | non-empty list | Ordered training stages. Stage names must be unique. |

### Base fields

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `base.model` | exactly one of `model` or `source` | `^[a-z0-9][a-z0-9._-]{0,63}$` | Names a managed model whose current weights are still its pulled origin. |
| `base.source` | exactly one of `model` or `source` | pinned model source | Acquires a supported external model through the same verified importer as `model pull`. Schema 1 accepts `huggingface://organization/model@<commit>`. |
| `base.origin_sha256` | no | SHA-256 | Asserts the expected origin BOM. WALDO always resolves and pins the actual value. |

With `base.model`, the complete compose architecture must exactly equal the
managed base architecture. With `base.source`, architecture may be omitted and
is inherited from the verified source; when supplied, it is an exact assertion.
A base initializes a new model and is never mutated by training.

## Architecture fields

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `family` | yes | `decoder-transformer` | The only schema-1 architecture family. |
| `context_tokens` | yes | positive integer | Maximum token context. Every stage sequence length must fit within it. |
| `vocabulary_size` | yes | positive integer | Must match the selected tokenizer contract. |
| `hidden_size` | yes | positive integer | Transformer residual width. Must be divisible by `attention_heads`. |
| `intermediate_size` | yes | positive integer | Gated feed-forward intermediate width. |
| `layers` | yes | positive integer | Number of decoder blocks. |
| `attention_heads` | yes | positive integer | Query-head count. Must divide `hidden_size`. |
| `key_value_heads` | yes | positive integer | Key/value-head count. Must divide `attention_heads`. |
| `dropout` | no | `0 <= value < 1`; default `0` | Residual dropout applied during training and disabled during evaluation and inference. |
| `tie_embeddings` | no | boolean; default `false` | Reuses input embeddings as the output projection when true. False adds a separate output matrix. Reference composes set it explicitly. |
| `parameter_dtype` | yes | `float32`, `float16`, or `bfloat16` | Portable parameter and mixed-precision artifact declaration. Backend support is checked before training. |
| `tokenizer.name` | yes | supported name | Selects WALDO's offline tokenizer implementation. |
| `tokenizer.revision` | yes | immutable revision | Pins exact tokenizer behavior. |

The architecture determines the model parameter count. WALDO derives and
reports that count in forecasts and model summaries; it is not an independent
compose field.

### Supported tokenizer contracts

The tokenizer name, revision, and vocabulary size are one exact contract:

| Name | Revision | `vocabulary_size` | Intended use |
| --- | --- | ---: | --- |
| `byte` | `builtin-byte-schema-1` | 259 | Legacy and very small byte-token models. |
| `tiktoken/r50k_base` | `tiktoken-r50k-base` | 50259 | Compact English-oriented subword models. |
| `tiktoken/cl100k_base` | `tiktoken-cl100k-base` | 100259 | Larger multilingual and code-capable subword vocabulary. |

WALDO performs tokenization before the framework worker, ensuring supported
backends receive identical token IDs.

## Stage fields

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `name` | yes | `^[a-z0-9][a-z0-9._-]{0,63}$` | Unique durable stage and run label. |
| `type` | yes | `pre-training`, `fine-tuning`, `alignment`, or `other` | Records the stage's intended role in provenance. |
| `objective` | yes | `causal-language-modeling` | The only currently executable objective. |
| `filter` | no | record filter | Applies one record-level condition to every selected corpus. |
| `corpora` | yes | non-empty list of unique scalar paths or configured corpus objects | Selects canonical corpus records for the stage. |
| `parameters` | yes | object | Declares the portable training budget and controls. |

Relative corpus values are logical paths beneath the selected WALDO index.
Absolute paths may identify another index checkout. Values select indexed
corpora, never raw source directories or exported corpus files.

Stages execute in listed order. Each completed stage produces the current
weights used to initialize the next stage. If a stage fails, later stages do
not run. Repeating the exact command after interruption resumes the durable
transaction and its latest verified checkpoint.

Stage `type` currently records intent; it does not select a different loss or
framework algorithm. `objective` selects executable behavior, and schema 1
supports only causal language modeling.

## Corpus selection and record filters

A `corpora` entry may remain a path string, preserving every existing
schema-1 compose:

```yaml
corpora:
  - core/books/gutenberg
```

Use the object form when a corpus needs configuration:

```yaml
filter:                         # stage-wide
  licenses:
    include: [CC-BY-*, CC0-*]
corpora:
  - path: core/books/gutenberg
    weight: 2
    filter:                     # only this corpus
      languages:
        include: [en]
      sources:
        exclude: [deprecated-*]
      date:
        from: "1900"
        to: "2025-06-30"
```

| Field | Required | Meaning |
| --- | --- | --- |
| `path` | yes in object form | Logical index path. |
| `weight` | only for `causal-pretrain-weighted` | Positive integer relative token exposure. It replaces the legacy map entry for this corpus. |
| `filter` | no | Record filter local to this corpus. |
| `licenses` | no | Matches the canonical row's normalized license. |
| `languages` | no | Matches the canonical row's language. |
| `sources` | no | Matches either the canonical source identifier or source name. |
| `date` | no | Selects canonical dates that overlap the inclusive `from`/`to` interval. |
| `include` | no | At least one shell-style, case-sensitive pattern must match. |
| `exclude` | no | Any matching pattern rejects the record and takes precedence over `include`. |
| `from` | no | Inclusive lower date bound: `YYYY`, `YYYY-MM`, `YYYY-MM-DD`, or RFC 3339. |
| `to` | no | Inclusive upper date bound in the same formats. |

Every declared filter must contain at least one condition. A stage-wide
`filter` and a corpus-local `filter` are combined with AND; the local filter
cannot loosen the global one. Conditions within one filter are also ANDed.
Missing or malformed row values do not satisfy an include or date condition.

Filtering happens while WALDO streams canonical rows, before deterministic
held-out selection and training shuffle. The versioned effective policy is
pinned in the corpus OpenWALDO BOM, so a resume or distributed node cannot
silently train on a different subset. The BOM's manifest totals remain the
indexed reference totals; run and evaluation evidence describe actual training
consumption.

For `causal-pretrain-weighted`, prefer inline `weight` fields. Existing
`parameters.corpus_weights` maps remain valid for compatibility, but a stage
must use one representation or the other, never both.

## Training parameter fields

| Field | Required | Default or range | Meaning |
| --- | --- | --- | --- |
| `profile` | no | `causal-pretrain-shuffled` | Selects versioned record ordering, corpus exposure, and held-out selection. |
| `epochs` | no | default `1`; `1..1000000` | Maximum deterministic passes over the selected canonical records. |
| `steps` | yes | positive integer | Required optimizer steps and learning-rate schedule length. |
| `batch_size` | yes | positive integer | Number of packed sequences in each optimizer step. |
| `sequence_length` | yes | positive integer, at most `context_tokens` | Number of predicted token targets per packed sequence. |
| `learning_rate` | yes | finite positive number | Peak AdamW learning rate. |
| `seed` | no | default `0` | Controls deterministic shuffling, evaluation selection, initialization, and training randomness. Reference composes set it explicitly. |
| `weight_decay` | no | default `0.1`; `0..1` | AdamW weight decay. Explicit zero disables it. |
| `warmup_steps` | no | `min(100, steps/10)`; `0..steps` | Linear warmup duration. For runs longer than one step, the default is at least one. Explicit zero disables warmup. |
| `checkpoint_every` | no | `min(500, steps)`; `0..steps` | Checkpoint interval. Explicit zero disables periodic checkpoints. |
| `evaluate_every` | no | `min(500, steps)`; `0..steps` | Held-out evaluation interval. Explicit zero disables periodic evaluation. |
| `shuffle_buffer_records` | no | default `1024`; `1..1000000` | Maximum records retained by deterministic bounded shuffle. |
| `shuffle_buffer_bytes` | no | default 64 MiB; `1 B..16 GiB` | Maximum record text retained by deterministic bounded shuffle. |
| `corpus_weights` | only for `causal-pretrain-weighted`; legacy form | each weight `1..1000000` | Integer relative token exposure keyed by every selected corpus path. Configured corpus `weight` fields are preferred. |
| `evaluation_fraction` | no | default `0.01`; `0 <= value < 1` | Candidate fraction for deterministic held-out selection. |
| `evaluation_max_records` | no | default `256`; `0..1000000` | Held-out record cap. |
| `evaluation_max_bytes` | no | default 1 MiB; `0 B..16 GiB` | Held-out text-byte cap. |

The planned token capacity is:

```text
steps * batch_size * sequence_length
```

The canonical stream must contain enough packed targets across the declared
epochs to reach every requested step. A run fails rather than silently
shortening its budget. Records are continuously packed with an EOS token
between records; document boundaries do not force padding to a new sequence.

Setting any one of `evaluation_fraction`, `evaluation_max_records`, or
`evaluation_max_bytes` to zero disables the held-out set and resolves all
three values to zero.

### Fixed profile behavior

All profiles resolve to AdamW with betas `0.9` and `0.95`, epsilon `1e-8`, and
a cosine schedule ending at 10% of the peak learning rate. Those values and
continuous EOS packing are versioned profile facts, not compose fields.

A schema-1 compose also has no fields for a chat template, optimizer choice,
gradient accumulation, activation checkpointing, mixture-of-experts routing,
or distributed topology. The current objective produces a raw causal base
model. Hardware and backend topology remain machine-local policy; other
training behaviors require a separately versioned portable contract rather
than an ignored compose field.

| Profile | Training record order | Held-out selection | Corpus weights |
| --- | --- | --- | --- |
| `causal-pretrain-shuffled` | One bounded deterministic shuffle across the selection. | Deterministic lowest SHA-256 candidates across the selection. | Not accepted. |
| `causal-pretrain-balanced` | Balances emitted tokenizer targets equally across logical corpus paths, with bounded shuffle within each. | Deterministically stratified across corpus paths. | Not accepted. |
| `causal-pretrain-weighted` | Selects the corpus with the lowest emitted-target-to-declared-weight ratio, with bounded shuffle within each. | Deterministically stratified across corpus paths. | Required for every selected corpus. |

Each named profile currently has `profile_schema: 1` in the resolved run BOM.
That field versions the behavior contract independently; it is not a model
architecture version. The former `causal-pretrain-v1`, `-v2`, and `-v3` names
remain accepted as deprecated input aliases and resolve respectively to
`shuffled`, `balanced`, and `weighted`. New resolved run BOMs use the
behavior-named identities.

Use `causal-pretrain-balanced` when every selected corpus should receive equal
token exposure. Use `causal-pretrain-weighted` when the intended mixture is
unequal. Weights are relative—for example, `2` and `1` target approximately
twice as many emitted training tokens from the first corpus while it remains
available. They do not duplicate canonical records or alter corpus provenance.

## Common compose patterns

### Minimal model from scratch

```yaml
kind: waldo-model-compose
schema: 1
architecture:
  family: decoder-transformer
  context_tokens: 512
  vocabulary_size: 50259
  hidden_size: 512
  intermediate_size: 1536
  layers: 8
  attention_heads: 8
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: tiktoken/r50k_base
    revision: tiktoken-r50k-base
stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora: [core/books/gutenberg, science/plos]
    parameters:
      profile: causal-pretrain-balanced
      steps: 32000
      batch_size: 64
      sequence_length: 512
      learning_rate: 0.0003
      seed: 42
```

### Initialize from pulled weights

```yaml
base:
  model: llama-base
  origin_sha256: <expected-origin-bom-sha256>
```

Add this block to a complete compose whose architecture exactly matches
`llama-base`. The base must have pulled origin weights as its current weights.

To acquire the origin directly from a supported external source, pin its
immutable commit and omit `architecture` to inherit the verified definition:

```yaml
base:
  source: huggingface://organization/model@0123456789abcdef0123456789abcdef01234567
```

This uses the same importer and compatibility checks as `waldo model pull`.
WALDO caches the verified origin beneath the managed model root, initializes
the new model using hard links when possible, and records the resolved origin
BOM in the compose, plan, and model BOM. Branches and tags are rejected because
they can move. Schema 1 currently supports the Hugging Face model subset
documented in the model lifecycle guide.

### Multiple ordered stages

```yaml
stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora: [core/books/gutenberg, science/plos]
    parameters:
      profile: causal-pretrain-balanced
      steps: 32000
      batch_size: 64
      sequence_length: 512
      learning_rate: 0.0003
      seed: 42

  - name: domain-tune
    type: fine-tuning
    objective: causal-language-modeling
    corpora: [core/common-pile/python-enhancement-proposals/peps]
    parameters:
      profile: causal-pretrain-shuffled
      steps: 1000
      batch_size: 32
      sequence_length: 512
      learning_rate: 0.00005
      seed: 43
```

The second stage starts from the first stage's completed weights. The
`fine-tuning` type is provenance; both stages currently use the causal
language-modeling objective.

## Validation and failure behavior

WALDO fails before training when a compose has:

- an unknown field, extra YAML document, unsupported kind, or schema;
- an unsupported architecture, tokenizer contract, dtype, or objective;
- zero architecture dimensions or incompatible attention-head divisibility;
- dropout outside `0..<1` or a sequence longer than the architecture context;
- no stages, duplicate stage names, no corpus selection, or duplicate corpora;
- invalid parameter ranges or an overflowing planned token capacity;
- corpus weights outside `causal-pretrain-weighted`, missing weighted-profile
  weights, or weights for unselected corpora;
- a base whose source is mutable or whose origin, architecture, or current weights do not match; or
- an existing destination model with a different immutable architecture.

During training, WALDO fails rather than accepting incomplete steps,
unaccounted corpus exposure, corrupt checkpoints, or artifacts that do not
match their recorded hashes.

## Portability and identity

Architecture, tokenizer, resolved profile, corpus BOMs, backend identity,
evaluation selection, checkpoints, telemetry, and output artifacts are
persisted in model and run records. Changing architecture fields creates a
different architecture identity. Changing stage parameters or corpus weights
creates a different run contract and prevents an incompatible checkpoint from
being resumed.

The compose remains portable because backend selection and hardware are not
part of it. Portability means WALDO can select any backend that implements the
declared architecture and objective; it does not promise that every compose
fits every machine. Run `waldo model forecast` before allocating substantial
compute.

See the measured examples in [`composes/`](../composes/), the broader
[model lifecycle](MODEL-LIFECYCLE.md), and [training and calibration](TRAINING-AND-CALIBRATION.md).
