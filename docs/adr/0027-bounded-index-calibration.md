# ADR 0027: Quantize with upstream tools and bounded index calibration

## Context

WALDO can write a precision-preserving GGUF itself, but implementing and
maintaining llama.cpp's changing K-quant algorithms would duplicate specialist
runtime code. Calibration must consume auditable corpus data without turning a
large index selection into an unbounded download or being mistaken for model
training.

## Decision

- WALDO writes its verified source weights to a private high-precision GGUF in
  the atomic export staging directory.
- An installed upstream `llama-quantize` performs the requested conversion.
- The public CLI accepts only `2`, `3`, `4`, `5`, `6`, or `8`; WALDO resolves
  and persists the exact llama.cpp profile.
- Optional `--calibration <index-path>` resolves a normal WALDO corpus BOM.
  WALDO deterministically selects unique hash-pinned shards and streams audited
  records until a versioned bounded byte-token budget is full.
- `llama-imatrix` measures that sample before quantization. It does not update
  weights and is not represented as a training run.
- Release evidence hashes and identifies both executables and embeds the
  compact corpus and sample selection evidence in `BOM.json`.
- Intermediate high-precision GGUF and importance-matrix files are removed
  before atomic publication or during failure cleanup.

## Consequences

Quantization behavior stays compatible with the target runtime and can be
audited to a specific executable hash. Calibrated export downloads only a
bounded subset of a large corpus, although the selected Parquet shards must be
read completely enough to audit them. Quantized export temporarily needs disk
for the high-precision GGUF plus the derived output. The managed training
weights remain unchanged and are still required for future training-quality
exports.
