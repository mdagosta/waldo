Many people have a GPU that sits idle, and one machine is too small to train a useful model. This adds `waldo model join`, so people who don't know each other can train one model together. They coordinate only through signed records in a git repository, and that history is the record of how the model was built.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This is the top: it sits on #42, #46, #47 and #49, which make up the first 46 of its 62 commits. **Please review the last 16, `eba901d..60f7c63`.** The diff shrinks as the lower PRs merge.

## What changed

- One run file describes the whole run: the units of work, who takes part (their SSH keys), the rounds, and how each stage is judged. The same file trains a single model, for comparison.
- Contributors claim units, train them, and submit the results as signed records. A record counts only if its signature checks out and it comes from whoever claimed that unit.
- Each round, the closers named in the run file check the submissions, score them with `--evaluate-only` (#47), and merge the good ones with `base.merge` (#42). A majority of closers decides.
- A stage can require its merged model to pass a test before the run moves on. If it fails, the run adds rounds.
- A released model lists all the data in its history, not only its last training step, and a pull refuses a release whose summary disagrees with its history (ADR 0089).
- WALDO can import an exported model package from a local directory (ADR 0087).
- Also fixes a race that made `TestHostfileSessionLaunchesAndPublishesPlans` flaky on macOS CI.

ADR 0088 has the design, including what join doesn't defend against: a colluding majority, a copied replica, and the plan's author. `docs/MODEL-JOIN.md` is the guide.

## Verification

`gofmt -l .` is empty, and `./testing/all.sh` passes on macOS arm64 at 60f7c63; the real MLX and Linux-only suites skip. `WALDO_E2E_JOIN=1 testing/e2e/model-join.sh` passes with real MLX at 60f7c63: three contributors train a run to the end, one cheating on a unit and one closer outvoted.

## Contracts

- New command `waldo model join` and its `join` block in the run file.
- New signed records for claims, submissions and merges (schema 1).
- Exported packages gain `provenance-<sha256>.json` and `TRAINING-CLOSURE.json`. Run BOMs record the Python runtime by version, not path, since they are now published; this amends ADR 0022.
- New ADRs 0087, 0088 and 0089.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).
