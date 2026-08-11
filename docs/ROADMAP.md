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

## Opening development to the public

The immediate release work is:

1. Keep CI and portable end-to-end tests green.
2. Audit source and Git history for secrets, private material, and
   redistributability concerns.
3. Add lightweight issue, pull-request, and security-reporting paths.
4. Discuss ownership, licensing, and contribution-policy questions with the
   project owner before changing legal or governance files.
5. Publish the source as early development and iterate in public.

## Deferred beyond the first release

- Multimodal ingestion and corpus-removal contribution workflows.
- Broader Hugging Face tokenizer and architecture compatibility.
- Supervised fine-tuning, preference training, and pinned chat templates.
- PyTorch generation, TensorFlow training, and multi-node orchestration.
- Hugging Face publication and exact editable EU template rendering.
- Sparse mixture-of-experts and router research features.

Deferred work should move to public issues after launch instead of expanding
this file into a speculative design backlog.
