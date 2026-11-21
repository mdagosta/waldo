A held-out loss only compares models scored on the same records, and until now only a training run guaranteed that. This lets held-out scoring stand on its own, so different models can be compared fairly. Distributed training (#48) uses it to compare contributors' work and to decide when a merge is good enough.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This sits directly on main and needs no other PR; #48 builds on it. Once #42 merges, this conflicts with it in `internal/cli/model_train_test.go`; #48 already carries the resolution, and I'll update this branch then.

## What changed

- `evaluation_seed` fixes which records are held out, so runs that shuffle their training data differently are still scored on the same records.
- `artifact_selection: final` publishes a stage's last step instead of its best-scoring one, for weights that will be trained further or merged.
- `waldo model train <name> <compose> --evaluate-only` scores a model's current weights on the compose's held-out records. It trains nothing and writes nothing.

ADR 0030 and ADR 0079 each get a short addendum.

## Verification

`gofmt -l .` is empty, and `./testing/all.sh` passes on macOS arm64 at 2dcc4c6; the real MLX and Linux-only suites skip, so `--evaluate-only` runs against a stub backend here.

Known limit: older WALDO versions reject a compose that sets either new field, and can't read a model trained with one.

## Contracts

New optional compose fields `evaluation_seed` and `artifact_selection`, documented in `docs/MODEL-COMPOSE.md`; leaving them unset changes no existing hash. New `--evaluate-only` flag on `waldo model train`, documented in `docs/MODEL-LIFECYCLE.md`. Addenda to ADR 0030 and ADR 0079.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).
