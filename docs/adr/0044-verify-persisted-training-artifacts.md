# ADR 0044: Verify persisted training artifacts

Status: accepted

## Decision

Before a PyTorch run completes, the worker reloads `model.safetensors` through
the portable inference representation and evaluates the pinned held-out set.
The run fails when the artifact result is non-finite or differs from the live
result by more than the larger of 0.02 loss or one percent.

The final evaluation records `artifact_heldout_loss`,
`artifact_heldout_perplexity`, and `artifact_loss_delta`. WALDO rejects a
successful PyTorch observation without this evidence when held-out evaluation
was configured.

The current PyTorch worker identity is
`builtin-pytorch-worker-schema-1-r7`.
