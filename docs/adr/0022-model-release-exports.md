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

The schema-1 Hugging Face adapter uses the standard Llama configuration because
WALDO's decoder architecture has the same RMSNorm, rotary attention, grouped
query attention, and SwiGLU tensor shapes. It rewrites Safetensors header names
and metadata while copying the tensor data section byte-for-byte. WALDO's byte
tokenizer remains explicit custom tokenizer code; it is not mislabeled as a
different pretrained tokenizer.

The MLX adapter uses the same verified Llama name map, marks the Safetensors
container for MLX, and binds `architecture.py` to MLX-LM's Llama
implementation. It remains a separate release package even though its tensor
payload can be identical to the Hugging Face package.

The GGUF adapter writes GGUF v3 directly and incrementally from the verified
Safetensors artifact. It uses standard Llama tensor names, reverses tensor
dimensions for GGML's ordering, applies the required Q/K per-head permutation,
preserves BF16 or F16 matrices, and promotes one-dimensional normalization
weights to F32. WALDO's schema-1 byte tokenizer is encoded as an explicit
GPT-2-style byte vocabulary with no merges and no implicit BOS or EOS token.
The Ollama adapter adds only a relative `Modelfile`; it does not create a
second weight representation. Every adapter translates the immutable
model-level interaction declaration automatically: Hugging Face and MLX write
tokenizer chat-template metadata, GGUF writes `tokenizer.chat_template`, and
Ollama writes the equivalent `TEMPLATE`. `interaction.tools: true` adds full
tool schemas, assistant calls and arguments, and tool results. Raw and
non-tool conversational models do not advertise tool capability.
