# ADR 0044: Verify persisted training artifacts

Status: Accepted

## Context

Training evaluation previously measured only the live model in worker memory.
Serialization, reduced-precision conversion, or a loader mismatch could therefore
produce a persisted model that behaved differently while the run was still
recorded as successful.

## Decision

The PyTorch backend keeps FP32 master parameters and optimizer state and uses the
architecture's declared reduced precision for compute and portable weights.

Before a run completes, the worker reloads `model.safetensors` into a fresh model
through the portable inference representation and evaluates the pinned held-out
set. The run fails if the artifact result is non-finite or differs from the live
result by more than the larger of 0.02 loss or one percent. The final evaluation
records `artifact_heldout_loss`, `artifact_heldout_perplexity`, and
`artifact_loss_delta`. WALDO rejects a successful PyTorch observation that lacks
this evidence when held-out evaluation was configured.

## Consequences

- A complete run establishes that its persisted weights can be reloaded and
  retain measured quality.
- PyTorch checkpoints retain FP32 optimizer state, increasing checkpoint size
  but avoiding reduced-precision AdamW state.
- The final artifact reload adds one held-out evaluation to run completion time.
- Backend revision `builtin-pytorch-worker-schema-1-r5` identifies this contract
  together with explicit normal(0, 0.02) embedding and projection initialization.
