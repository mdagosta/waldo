# WALDO remaining work

This is the prioritized product backlog after the working index, ingestion,
training, model-export, and calibrated-quantization slices. Detailed completed
history remains in [the roadmap](docs/ROADMAP.md); this file is intentionally
about unfinished work.

## 1. Index maintenance

- Add explicit corpus-removal contribution overlays with optional guarded
  lookaside object deletion.
- Design non-text and multimodal ingestion as a separate bounded phase.

## 2. Distribution

- Add `waldo model push <name> <huggingface-target>`.
- Reuse the verified export pipeline and support an explicitly selected
  Hugging Face, MLX, GGUF, or Ollama representation.
- Carry `--quant` and index-backed `--calibration` into GGUF/Ollama pushes.
- Authenticate through standard Hugging Face credentials and publish one
  reviewable commit without silently overwriting unrelated remote files.

## 3. Model lineage and tuning

- Add explicit model fork and parent-lineage operations.
- Broaden Hugging Face tokenizer and architecture compatibility beyond the
  initial Llama plus OpenWALDO byte-tokenizer profile.
- Define supervised fine-tuning records, objectives, evaluations, and BOM
  evidence.
- Pin chat templates rather than inferring instruction behavior.
- Add preference training only after the SFT and evaluation contracts are
  stable.

## 4. Release readiness

- Render the exact official editable EU GPAI template from the existing
  version-pinned disclosure evidence.
- Produce installable binaries and packages.
- Write migration guidance for earlier WALDO experiments and index metadata.
- Reconcile the README, CLI help, and public website.
- Define and execute the supported public-release process.

## Later scale and compatibility work

- PyTorch generation.
- A TensorFlow training adapter.
- Multi-node TorchTitan rendezvous, scheduling, and orchestration.
- Broader cluster-management integrations.

These are valuable extensions, but they are not blockers for a dependable
first release built around MLX, PyTorch, and single-node TorchTitan training.
