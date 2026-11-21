# join-utils vet, 2026-11-20 (origin/main...c986bbf)

Three agents (reviewer, adversary, compatibility), findings checked against the
code. Decision (858e067): eddd12e kept without its corpus_history.go change, so
the completed-corpus check compares logical paths exactly as on main (finding 1);
2 fixed in 75d5ded, 4 in 939a212; 3 dropped, since every improving evaluation
already gets its own checkpoint (mlx.py record_evaluation); minor items left
alone for now.

## 1. HIGH: a corpus named by its path on disk can be recorded as trained when it wasn't

From eddd12e. `selectedPath` (internal/model/recipe.go:418) maps a disk path to
a logical corpus by longest suffix against whatever path list it's given, not
the index's own resolution.

- Untrained corpus skipped: a run trained `python`; a compose naming
  `/home/me/idx/code/python` ends in `/python`, so it's skipped as trained
  (corpus_history.go:52) or the compose counts as complete (:128). Reproduced.
- Finished corpus retrained: two completed runs both trained `core/books`, the
  duplicate counts as ambiguous, the corpus trains again (:72). Join hits this
  every round. Reproduced.
- `./x` after a stage's first corpus resolves from the current directory here,
  from the index root in the index; a filter or weight can land on the wrong
  corpus. Reproduced.
- Absolute-path composes that used to retrain now skip.

Fix: carry the index's resolved `Target.Rel` per selection and compare logical
paths exactly. Or drop eddd12e: nothing in model-join depends on it.

## 2. MEDIUM: a pulled package can carry two different tokenizers

pull.go:428 decodes `openwaldo_tokenizer.json` with Go's JSON (case-insensitive
keys, last duplicate wins); the package's Python tokenizer is case-sensitive.
`"artifact"` plus `"Artifact"` → WALDO records one, HF/MLX code runs the other.
Reproduced. Fix: refuse case-variant or duplicate keys.

## 3. MEDIUM: an unrecorded clock setting can change the published weights

A `training.checkpoint-minutes` checkpoint can become the best-held-out
candidate (workers/mlx.py ~818), and the setting isn't in the run BOM. Read, not
run. Fix: keep clock checkpoints out of artifact selection.

## 4. LOW: legacy `waldo/bytepiece` models can't export to HF/MLX

huggingface.go:248 checks only `TrainedName`; pull, training and the package
code accept the legacy name. Reproduced. Fix: `waldotokenizer.IsTrainedName`.

## 5. LOW: workers accept hand-edited config values `config set` refuses

`gpu.throttle` 1e-12 in the file → worker sleeps for years or crashes; JSON
`true` passes as a number. `checkpoint-minutes 0.0001` saves every step. Read.
Fix: Go's range check in Python; floor checkpoint-minutes at ~1 minute.

## Minor

- Ambiguous `corpus_weights` now says "does not declare" (preflight.go:160).
- Chat warns "MLX chat waits" on PyTorch too; `gpu.throttle 0` pauses chat
  silently though the docs say it "says so" (cli/model.go:1863).
- ADR 0026 needs an addendum for byte-BPE pull; ADR 0029 and
  MODEL-LIFECYCLE.md:171 need "the interpreter's directory isn't part of the
  environment".
- Downgrade notes for the PR: an older `config set` drops the new keys; an older
  binary won't resume a run the new one started.
- `runtimeWithoutDirectory` (build.go:651) cuts at the first "; ", so a path
  containing "; " won't resume. Cut at "; Python ".
- Config path logic in three places (Go + two Python workers); dead fallbacks in
  `selectedPath`; no test for the `--prompts` flag guard.

## Clean

`--prompts` input handling; `sameExecution` still refuses other Python/MLX
versions; interrupted runs resume (MLX worker revision unchanged); tampered
tokenizer merges refused; atomic checkpoints; byte-package origins unchanged;
older binaries refuse BPE packages.
