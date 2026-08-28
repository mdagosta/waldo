# ADR 0019: Fail closed into MLX on Apple Silicon

Status: accepted

## Decision

Automatic training on macOS selects MLX. MLX requires Apple Silicon and a
usable native Python installation. Resolution probes the runtime and Metal
before any run is created. Failure never falls back to simulation.

The embedded MLX worker supports the portable decoder transformer, byte,
`tiktoken/r50k_base`, and `tiktoken/cl100k_base` tokenizers, causal-language
modeling, assistant-response modeling, held-out evaluation, complete
checkpoints, resume, and terminal Safetensors.

The fake adapter remains explicit machine-local test configuration and is
never selected automatically.
