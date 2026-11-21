Models fine-tuned from the same starting weights can be averaged into one, a simple way to combine what each learned. This lets a compose do that, and keeps the merged model traceable to the models that went into it. Distributed training (#48) uses it to combine each round's work.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This sits on #41, whose one commit shows here until it merges; please review the other 15. #48 builds on this.

## What changed

```yaml
base:
  merge:
    - {model: python-ft, weight: 1}
    - {model: docs-ft, weight: 3}
```

- A compose can list two or more local models under `base.merge`, optionally weighted. The result is a new model, or the starting point for more training.
- Merging the same parents always gives the same weights, so anyone holding them can check a merge.
- WALDO merges only models whose history shows a common ancestor, and refuses the rest.
- The merged model carries its parents' history, so the parents can be deleted, and its disclosure files still list all the data behind it.

ADR 0085 has the details. Start reading at `internal/model/merge.go`.

## Verification

`gofmt -l .` is empty, and `./testing/all.sh` passes on macOS arm64 at f52349a; the real MLX and Linux-only suites skip.

Known limit: as with #41, older WALDO versions can't read a merged model or anything built on one.

## Contracts

ADR 0085. New compose field `base.merge`, and a `MERGE.json` record (`waldo-model-merge`, schema 1) in each merged model. `model.Builder.ResolveCompose` takes the destination model name.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).
