# ADR 0022: Export separate signed model release formats

- Status: accepted
- Date: 2026-08-05

## Context

WALDO models must move between WALDO installations and the Hugging Face,
vLLM, MLX, GGUF, Ollama, llama.cpp, and LM Studio ecosystems. Those consumers
do not share one executable model package: Safetensors and GGUF are weight
containers, while architecture code, configuration, tokenizer conventions,
tensor names, and quantization remain runtime-specific. Combining every weight
representation would also make ordinary exports unnecessarily large.

Every public distribution must retain the same technical provenance and EU
GPAI training-content disclosure. Provider identity is machine-level release
configuration, while model name, version, lineage, and training evidence are
model-specific facts. Artifact hashes prove integrity but do not establish who
issued a release.

## Decision

`waldo model export <name> <destination>` produces the native WALDO format.
The optional `--format` flag selects exactly one of `waldo`, `huggingface`,
`mlx`, `gguf`, or `ollama`; formats are never merged implicitly.

Every format contains exactly named `BOM.json` and `EU-BOM.json` documents.
`BOM.json` is the format-specific OpenWALDO release inventory and retains the
source model, run, corpus, backend, and artifact provenance. `EU-BOM.json` is a
version-pinned projection onto the supported Commission template. A normal
export requires a strictly validated provider profile from
`disclosure.provider` and fails before publication when required disclosure
facts are absent. An explicit incomplete-development path may emit only a
conspicuously marked draft.

Provider configuration contains organization-level facts only. Model-specific
release facts live with the model or its compose and are never hidden in a
global profile.

Signing is automatic when complete `signing.*` infrastructure is configured.
Failure of configured signing fails the export. An export without signing
configuration succeeds with an explicit unsigned warning. Detached Sigstore
bundles avoid self-referential JSON and are named `BOM.sigstore.json` and
`EU-BOM.sigstore.json`.

Format adapters map one verified, complete, non-simulated source run. They
stream or copy large tensors, write into a sibling temporary directory, verify
their result, and publish with one atomic rename. Hugging Face exports include
custom Python implementation when the architecture cannot honestly identify
as a native Transformers architecture. GGUF and Ollama exports include only
architectures and tokenizer metadata their target runtimes can execute.

## Consequences

- The native WALDO package remains the small, default, lossless export.
- Users choose storage-expensive derived representations deliberately.
- vLLM can consume the Hugging Face representation; Ollama and LM Studio can
  share the GGUF representation without pretending the packages are identical.
- Every exported representation has the same provenance and disclosure names.
- Release verification can distinguish integrity, signer identity, regulatory
  completeness, numerical conversion, and runtime compatibility.
- New runtime formats add adapters rather than conditionals to model training
  or the index/corpus domains.
