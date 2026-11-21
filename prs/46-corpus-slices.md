Distributed training gives different pieces of one corpus to different trainers. This adds a way to pick a non-overlapping piece of a corpus, so each model traces back to a known part of the published corpus rather than an unspecified one. Distributed training (#48) uses it to give each contributor their own piece. It replaces #45, which carried only the first commit.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This sits directly on main and needs no other PR; #48 builds on it. Once #42 merges, this conflicts with it in `docs/adr/README.md`; #48 already carries the resolution, and I'll update this branch then.

## What changed

```yaml
filter:
  slice: {index: 2, count: 8}
```

- A compose filter can take `slice`, for the whole stage or one corpus. This one keeps piece 2 of 8.
- The pieces don't overlap, are about the same size, and together cover the corpus.
- A record's piece comes from a hash of its ID, so anyone can check which records a piece holds without running WALDO.
- The piece is recorded in the run's BOM, so each model shows which part of the corpus it trained on.

ADR 0086 has the details.

## Verification

`gofmt -l .` is empty, and `./testing/all.sh` passes on macOS arm64 at 272a059; the real MLX and Linux-only suites skip.

Known limits: train each piece into its own model, since a model that has finished one piece skips the corpus as already trained. Older WALDO versions reject a compose with a slice, and can't read a model trained on one.

## Contracts

ADR 0086. New optional filter field `slice`, documented in `docs/MODEL-COMPOSE.md`. Without a slice, existing hashes are unchanged.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).
