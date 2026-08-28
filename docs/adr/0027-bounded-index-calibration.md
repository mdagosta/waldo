# ADR 0027: Use bounded corpus calibration for quantization

Status: accepted

## Decision

WALDO writes a verified high-precision GGUF in private export staging and uses
installed upstream `llama-quantize` for requested quantization. The public
profiles are `2`, `3`, `4`, `5`, `6`, and `8`.

Optional `--calibration <index-path>` resolves a normal corpus BOM and selects
a deterministic, bounded sample. `llama-imatrix` measures the sample before
quantization. Calibration does not change model weights and is not a training
run.

The release BOM pins the corpus sample and exact tool identities and hashes.
Intermediate GGUF and importance-matrix files are removed before publication
or during failure cleanup.
