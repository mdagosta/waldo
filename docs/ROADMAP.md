# Roadmap

This roadmap records current status, not completed implementation history.
Git history and ADRs preserve the details of earlier design phases.

## Implemented

- Git-backed index inspection, verification, audit, ingestion, update, and
  export with YAML-primary metadata and legacy JSON reads.
- Canonical Parquet records, local and S3 lookaside storage, verified caching,
  object inventory, mirroring, and explicit removal.
- Corpus, run, origin, model, export, and EU disclosure BOMs.
- Model initialization, open-weight pulls, forecasting, training, resume,
  inspection, generation where supported, and export.
- MLX, PyTorch, and single-node TorchTitan training adapters, subject to local
  runtime and hardware availability.
- Native WALDO, Hugging Face, MLX, GGUF, and Ollama export formats, with
  optional Sigstore signing when configured.

## First public release

The immediate release work is:

1. Repair CI and stale end-to-end test commands, then establish a green
   cross-platform release gate.
2. Complete the repository policy, security, governance, and contribution
   decisions in [the release plan](RELEASING.md).
3. Audit source and Git history for secrets, ownership, redistributability,
   and unsupported public claims.
4. Define supported platforms and publish signed, checksummed, reproducible
   binaries with release provenance.
5. Reconcile the CLI, this documentation, the website, and adjacent WALDO
   repositories before tagging the first supported version.

## Deferred beyond the first release

- Multimodal ingestion and corpus-removal contribution workflows.
- Broader Hugging Face tokenizer and architecture compatibility.
- Supervised fine-tuning, preference training, and pinned chat templates.
- PyTorch generation, TensorFlow training, and multi-node orchestration.
- Hugging Face publication and exact editable EU template rendering.
- Sparse mixture-of-experts and router research features.

Deferred work should move to public issues after launch instead of expanding
this file into a speculative design backlog.
