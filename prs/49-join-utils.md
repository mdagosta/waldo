Six small changes that make WALDO easier to run on a laptop and to share models from. Each stands on its own; distributed training (#48) uses all of them.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This sits directly on main and needs no other PR; #48 builds on it. Once #42 merges, this conflicts with it in `internal/model/pull.go`; #48 already carries the resolution, and I'll update this branch then.

## What changed

- **Run BOMs no longer record the Python interpreter's full path**, only its name, so a published BOM doesn't reveal the trainer's home directory. Earlier runs still resume.
- **`model chat --json --prompts`** reads one JSON prompt per line from stdin and answers each on its own line, opening the model once.
- **Trained byte-BPE tokenizers export.** Hugging Face and MLX packages now carry them, and `waldo model pull` restores them. Before, only the byte tokenizer could be exported.
- **`training.checkpoint-minutes`** makes MLX also save a checkpoint after that many minutes, so a slow run can stop without losing much.
- **`gpu.throttle`** changes the MLX GPU budget of a running training or chat, under the `WALDO_GPU_THROTTLE` ceiling, without restarting it. `0` pauses.
- **A compose can name a corpus by its path on disk** (absolute, or starting with `./`, `../` or `~/`), and per-corpus filters and weights apply to it as they do to its logical path. Re-running such a compose still retrains corpora an earlier run finished, as it does today.
- **Also:** fixes a race in `TestGracefulCancellationLetsLauncherReapWorker` that failed macOS CI.

## Verification

`gofmt -l .` is empty, and `./testing/all.sh` passes on macOS arm64 at 858e067; the real MLX and Linux-only suites skip.

Known limit: older WALDO versions can't pull a package with a trained byte-BPE tokenizer.

## Contracts

New config keys `training.checkpoint-minutes` and `gpu.throttle`, and `--prompts` on `waldo model chat`, documented in `docs/MODEL-LIFECYCLE.md`. Corpus paths on disk are documented in `docs/MODEL-COMPOSE.md`. Hugging Face and MLX packages may carry `openwaldo_tokenizer.json`, documented in `docs/MODEL-EXPORTS.md`. The runtime string in run BOMs names the interpreter, not its path. Only MLX workers read the new training settings.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).
