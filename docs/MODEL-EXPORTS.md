# Exporting WALDO models

`waldo model export` turns a managed model into one portable release package.
The default preserves the complete WALDO model. `--format` selects one derived
runtime representation. WALDO never bundles several large weight formats into
one export.

```bash
waldo model export <name> <new-directory> \
  [--format waldo|huggingface|mlx|gguf|ollama] \
  [--allow-incomplete] [--json]
```

Every package contains two consistently named public documents:

- `BOM.json` identifies the model or release and binds its artifacts by path,
  byte length, and SHA-256.
- `EU-BOM.json` is WALDO's version-pinned, machine-readable projection onto the
  EU GPAI public training-content disclosure template.

When signing is configured, both documents also receive detached Sigstore
bundles. An export contains one weight representation, so choosing GGUF does
not also copy Safetensors and choosing Hugging Face does not also create GGUF.

## What WALDO exports

| Format | Intended consumer | Weight representation | Additional runtime files |
| --- | --- | --- | --- |
| `waldo` | WALDO provenance, transfer, and archival | Original managed artifacts | Complete plan, model, run BOMs, observations, checkpoints, and artifacts |
| `huggingface` | Transformers and compatible consumers such as vLLM | Safetensors with standard Llama tensor names | Llama configuration, generation configuration, byte tokenizer, architecture binding, and README |
| `mlx` | MLX and MLX-LM | Safetensors with standard Llama tensor names and MLX container metadata | MLX-LM architecture binding, configuration, byte tokenizer, and README |
| `gguf` | llama.cpp-compatible consumers, including LM Studio | One GGUF v3 file | No second weight representation |
| `ollama` | Ollama | The same GGUF v3 representation | A relative `Modelfile` containing the context length |

`waldo` is the default because it is lossless, retains every historical run,
and preserves the most provenance. A derived runtime package contains only the
newest complete, non-simulated real-weight run selected by
`current_run_id` in the managed model BOM.

## Where the architecture lives

There is no single Python architecture file shared by every runtime:

- the native WALDO package keeps the canonical architecture in `PLAN.json`,
  `MODEL.json`, and the model/run BOMs;
- Hugging Face writes the standard Llama dimensions and behavior to
  `config.json` and includes an auditable `architecture.py` binding to
  Transformers;
- MLX writes the same portable dimensions and includes an `architecture.py`
  binding to MLX-LM;
- GGUF embeds the architecture, tokenizer, tensor types, and tensor layout in
  `model.gguf`; and
- Ollama uses that embedded GGUF architecture, while `Modelfile` supplies the
  relative model reference and runtime context setting.

The generated Python file is a runtime binding, not a copy of WALDO's training
worker. GGUF and Ollama do not need or include Python architecture code.

## Prerequisites and selection

The model name resolves beneath the configured `model.root`, which defaults to
`~/.waldo/models`. Before writing a derived runtime package, WALDO:

1. loads and validates the managed model;
2. resolves `current_run_id` from `MODEL-BOM.json`;
3. requires that run to be complete and non-simulated;
4. verifies the selected configuration, tokenizer, weights, sizes, and hashes;
5. verifies that the artifacts match the model pin; and
6. converts only that verified source.

Earlier simulated, interrupted, failed, and superseded runs remain in the
managed model and native WALDO export, but they are never silently selected as
runtime weights.

The destination must not already exist and must not be inside the managed
model. WALDO prepares a sibling temporary directory, removes it after a
failure, and publishes the completed package with one atomic rename. It does
not modify the managed model.

## Configure disclosure information

Model export always creates `EU-BOM.json`, so a provider profile is required:

```bash
waldo config set disclosure.provider docs/examples/eu-gpai-provider.json
waldo config get disclosure.provider
```

The schema-1 provider profile contains reusable organization-level facts such
as provider identity, EU representative, Code of Practice status, copyright
policy, and provider-level measures. It must not contain a local model's name,
architecture, training data, or release lineage; those facts come from the
model and its BOMs.

A normal export fails before publication if a required disclosure fact is
missing. During development, `--allow-incomplete` emits a conspicuously marked
draft whose gap list states what remains unavailable:

```bash
waldo model export small ./small-dev \
  --format gguf \
  --allow-incomplete
```

`--allow-incomplete` applies only to regulatory disclosure completeness. It
does not bypass model validation, artifact hashes, real-run selection, format
compatibility, or signing failures.

For disclosure-only output to standard output or a single JSON file, use
`waldo bom export`, not `model export`:

```bash
waldo bom export small --format eu-gpai
waldo bom export small training-content.json --format eu-gpai
```

## BOM layers

WALDO uses several related BOMs because they answer different questions.

### Corpus OpenWALDO BOM

This is the immutable training-data selection boundary. It carries the
selected manifests, shards, licenses, source evidence, object hashes, and
index Git identity. A run embeds the resolved corpus BOM so later audit does
not depend on reopening a mutable checkout.

### `RUN-BOM.json`

The run BOM is written before training starts. It binds the corpus selection,
architecture, backend, objective, resolved training parameters, environment,
and planned work. Runtime observations are recorded separately so the plan
cannot be rewritten after execution.

### Managed `MODEL-BOM.json`

The managed model BOM aggregates all runs. It retains each run-BOM hash,
terminal state, backend and simulation identity, observations, and artifact
hashes. `current_run_id` selects the newest complete real-weight run. Artifact
paths are relative to the model root rather than one developer's absolute
filesystem path.

### Exported `BOM.json`

In a native `waldo` package, `BOM.json` is the normalized managed model BOM;
the private filename `MODEL-BOM.json` is removed. The package retains the
complete model tree, so its run and artifact paths still resolve from the
directory containing `BOM.json`.

In a derived package, `BOM.json` is a compact format-specific release
inventory. Its schema-1 shape is:

```json
{
  "kind": "openwaldo-bom",
  "schema": 1,
  "subject": "model-release",
  "format": "gguf",
  "model_id": "<immutable model identity>",
  "name": "small",
  "run_id": "<selected real run>",
  "source_bom_sha256": "<managed model BOM hash>",
  "artifacts": [
    {
      "role": "weights",
      "path": "model.gguf",
      "sha256": "<artifact hash>",
      "bytes": 123456
    },
    {
      "role": "regulatory-disclosure",
      "path": "EU-BOM.json",
      "sha256": "<disclosure hash>",
      "bytes": 12345
    }
  ],
  "generated": "<model lifecycle timestamp>"
}
```

`source_bom_sha256` connects the derived representation to the managed model
BOM from which it was produced. Each artifact path is relative to the package
root. This makes a release relocatable without embedding a machine-specific
model directory.

The derived package currently records the source model BOM by hash rather than
embedding the full `MODEL-BOM.json` and run tree. The hash makes substitution
detectable when the source BOM is retained or published, but it cannot recreate
that missing document by itself. Keep or publish the native WALDO package when
a self-contained, inspectable provenance archive is required. `EU-BOM.json`
contains the regulatory projection of training evidence, but is not a
replacement for the full technical run provenance.

### `EU-BOM.json`

`EU-BOM.json` is not a second weight inventory. It is the model-specific
regulatory disclosure mapping assembled from provider configuration, model and
run lineage, and the training corpus BOMs. It pins the supported Commission
template and reports missing facts. WALDO does not claim that generating it
alone establishes legal compliance, and official editable Word-template
rendering remains separate work.

## Signing

Unsigned export is the default and prints a warning. Signing becomes automatic
when `signing.method` is configured.

Keyless Sigstore signing:

```bash
waldo config set signing.method sigstore-keyless
```

Key-based Sigstore signing:

```bash
waldo config set signing.method sigstore-key
waldo config set signing.key /secure/path/cosign.key
```

WALDO requires `cosign` on `PATH` and signs after all package files and both
BOMs have been generated, but before the atomic publication step. Configured
signing is fail-closed: a missing key, missing `cosign`, authentication error,
or empty signature bundle aborts the export and removes the temporary package.

A signed package adds:

```text
BOM.sigstore.json
EU-BOM.sigstore.json
```

These are detached bundles, avoiding a self-referential BOM hash. A consumer
must still apply an appropriate Sigstore certificate-identity and issuer
policy when verifying keyless signatures. Artifact hashes prove integrity;
the signature establishes who issued the signed BOM under the verifier's
trust policy. Neither is by itself a regulatory compliance finding.

## Native WALDO package

```bash
waldo model export small ./small-waldo
```

The native export copies the complete managed model without symbolic links,
renames the aggregate BOM to `BOM.json`, adds `EU-BOM.json`, and verifies all
terminal and checkpoint artifact sizes and hashes before publication.

Representative layout:

```text
small-waldo/
├── BOM.json
├── EU-BOM.json
├── MODEL.json
├── PLAN.json
└── runs/
    └── 0001-<stage>-<run-id>/
        ├── RUN-BOM.json
        ├── RUN.json
        └── artifacts/
```

This is the authoritative archival and WALDO-to-WALDO representation. A
dedicated `model download` command has not been implemented yet; export does
not pretend otherwise.

## Hugging Face package

```bash
waldo model export small ./small-huggingface --format huggingface
```

Representative layout:

```text
small-huggingface/
├── BOM.json
├── EU-BOM.json
├── README.md
├── architecture.py
├── config.json
├── generation_config.json
├── model.safetensors
├── special_tokens_map.json
├── tokenization_openwaldo.py
└── tokenizer_config.json
```

WALDO rewrites the Safetensors header from internal names to standard Llama
names and marks the container for PyTorch. The large tensor data section is
copied byte-for-byte, with no numerical conversion. `config.json` declares the
standard Llama causal-language-model architecture. The schema-1 WALDO byte
tokenizer is explicit custom code, so Transformers consumers must allow its
reviewed package-local tokenizer implementation with
`trust_remote_code=True`.

The package includes `architecture.py` even though the model maps to the
standard Transformers Llama implementation. This makes the architecture
binding explicit and auditable rather than requiring a consumer to infer it
from the weight names alone.

## MLX package

```bash
waldo model export small ./small-mlx --format mlx
```

The MLX package has the same high-level layout as the Hugging Face package.
Its tensor payload is also copied byte-for-byte after name conversion, while
Safetensors metadata identifies MLX and `architecture.py` binds the model to
MLX-LM's Llama implementation. The byte tokenizer remains explicit.

MLX and Hugging Face are separate packages even when their tensor payloads are
identical. Keeping them separate avoids implying that their runtime metadata
and executable bindings are interchangeable.

## GGUF package

```bash
waldo model export small ./small-gguf --format gguf
```

Layout:

```text
small-gguf/
├── BOM.json
├── EU-BOM.json
└── model.gguf
```

The converter writes GGUF v3 directly from the verified Safetensors source. It
does not load the whole model into memory and does not create an intermediate
weight file. It:

- maps WALDO tensors to standard GGUF Llama names;
- writes dimensions in GGML order;
- applies the Llama Q/K per-head permutation required by llama.cpp;
- preserves F32 matrices and BF16 or F16 matrix precision;
- promotes one-dimensional normalization weights to F32;
- supports tied and untied output embeddings according to the architecture;
- embeds the schema-1 byte tokenizer as a GPT-2-style byte vocabulary with no
  merges; and
- disables implicit BOS and EOS insertion so prompt tokenization matches
  WALDO's byte-token stream.

This export is a container conversion, not quantization. The resulting GGUF is
normally close to the source weight size. Quantized variants would be distinct
derived releases and are not currently produced by `model export`.

The current GGUF adapter intentionally fails closed unless the model uses the
supported decoder-transformer architecture and
`byte@builtin-byte-schema-1` tokenizer with vocabulary size 259. Unsupported
tensors, data types, tokenizer metadata, inconsistent tied embeddings, or
invalid Safetensors offsets stop the export.

The generated GGUF was validated by importing it into Ollama and comparing a
deterministic first token with WALDO's native MLX generation for the same raw
prompt. The regular real-MLX E2E test also verifies GGUF identity, version, BOM
inventory, and hashes.

## Ollama package

```bash
waldo model export small ./small-ollama --format ollama
ollama create small -f ./small-ollama/Modelfile
ollama run small
```

Layout:

```text
small-ollama/
├── BOM.json
├── EU-BOM.json
├── Modelfile
└── model.gguf
```

The GGUF bytes are produced by the same converter as `--format gguf`. The
additional `Modelfile` contains a relative `FROM ./model.gguf` reference and
the model's context length. It remains portable when the package directory is
moved.

WALDO's current pretrained models are raw causal continuation models. The
Ollama package therefore does not invent a system prompt or chat template.
Instruction-style chat behavior requires a later, explicitly recorded
fine-tuning stage and pinned chat-template contract.

## Machine-readable command output

`--json` changes the command result, not the package contents:

```bash
waldo --json model export small ./small-gguf --format gguf
```

```json
{
  "name": "small",
  "format": "gguf",
  "output": "/absolute/path/small-gguf",
  "signed": false
}
```

Progress, warnings, and signing interaction remain on standard error. Scripts
should use the JSON result rather than parse the human sentence.

## Common failures

| Failure | Meaning and correction |
| --- | --- |
| Provider information is required | Configure a valid schema-1 `disclosure.provider`. |
| Required disclosure facts are missing | Complete the provider/model provenance, or use `--allow-incomplete` only for a clearly marked development draft. |
| Destination already exists | Choose a new directory; export never overwrites a package implicitly. |
| Destination is inside the source model | Export beside or outside the managed model so source and release cannot overlap. |
| No current real run | Train with a real backend and complete the run; simulated artifacts cannot become runtime releases. |
| Artifact hash, size, or model pin mismatch | Treat the managed model as corrupted or inconsistent and audit it; WALDO will not convert unverified bytes. |
| Unsupported GGUF tensor, tokenizer, or dtype | Use a supported schema-1 decoder model or add a reviewed format adapter rather than guessing metadata. |
| Signing is configured but fails | Install/configure `cosign`, fix authentication or the configured key, and rerun. WALDO will not fall back to unsigned output. |

## Verification boundaries

The export pipeline and tests establish:

- source model and artifact identity;
- deterministic format conversion rules;
- package-relative artifact paths;
- artifact byte lengths and SHA-256 hashes;
- disclosure generation and gap reporting;
- fail-closed signing when configured;
- structural validation of every supported package; and
- live Ollama loading and deterministic token parity for the GGUF adapter.

They do not establish that a model is useful, safe, instruction-tuned, legally
compliant, or compatible with every version of every third-party runtime.
Those claims require separate evaluation and an explicit release policy.

The architectural rationale is recorded in
[ADR 0022](adr/0022-model-release-exports.md). Model training and managed state
are documented in [the model lifecycle guide](MODEL-LIFECYCLE.md), and the
regulatory mapping is documented in
[the EU GPAI disclosure guide](EU-GPAI-DISCLOSURE.md).
