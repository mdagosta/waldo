# Rung 0001: Tiny Shakespeare reference

This is the small end-to-end systems reference. It proves corpus resolution,
byte tokenization, training, held-out checkpoint selection, serialization,
reload, and raw generation with a workload that is practical on consumer
hardware.

## Promoted result

- Model: `tiny-shakespeare-control-01`
- Model ID: `b4f8477a55ad`
- Capacity: 10.7M parameters
- Exposure: 81.92M byte tokens
- Held-out loss: 5.3814 initially; 1.5117 selected
- Selected checkpoint: step 1,250, before later overtraining
- Behavior: locally plausible play continuation and preserved play formatting
- Reference runtime: under 15 minutes on two H200 GPUs

## Promotion gates

- Training metrics remain finite.
- The selected artifact reloads at the selected held-out loss.
- Checkpoint selection rejects the degraded final optimizer step.
- Sampled continuations preserve character/dialogue formatting.
- Output is locally plausible Shakespeare-like text rather than corruption.

## Run

From the repository root:

```console
go run ./cmd/waldo/ model forecast \
  composes/0001-tiny-shakespeare/compose.yaml
go run ./cmd/waldo/ model train tiny-shakespeare-control-01 \
  composes/0001-tiny-shakespeare/compose.yaml
./composes/0001-tiny-shakespeare/evaluate.sh \
  tiny-shakespeare-control-01 /tmp/tiny-shakespeare-control-01-eval.jsonl
```

## Contents

- `compose.yaml`: immutable promoted training recipe
- `compose_test.go`: architecture, data, budget, and evaluation-contract guards
- `evaluate.sh`: fixed generation probes
- `README.md`: purpose, evidence, gates, and commands
