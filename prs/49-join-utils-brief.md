# Brief: join-utils (#49) vet fixes

Branch `join-utils` in the waldo repo, at c986bbf. It is PR #49 upstream
(openwaldo/waldo), a set of small independent utilities that `model-join` (#48)
merges in. A three-agent vet found the problems below; each was checked against
the code. Make the changes on `join-utils` only. Don't touch `model-join`; it
gets restacked on the Mac afterwards.

Each step is its own commit, proposed through `/commit` (I approve each one).
Keep diffs minimal and match the surrounding code. Run `gofmt -l .` and
`./testing/all.sh < /dev/null` at the end and tell me the final commit hash.

## 0. Drop eddd12e ("Resolve a corpus named by its path on disk...")

Its suffix matching can record an untrained corpus as trained and retrain a
finished one; nothing in model-join needs it, so it leaves this PR entirely.

    git rebase --onto eddd12e~1 eddd12e join-utils

No conflicts are expected: the only later commit touching the same files is
86eb0be, in a different part of docs/MODEL-COMPOSE.md. If one appears, stop and
show it. Afterwards `git grep -n 'selectedPath\|declaredCorpusPath'` should
find nothing, and `internal/model/selected_path_test.go` should be gone.

## 2. A pulled package must not carry two different tokenizers

`internal/model/pull.go` (~line 424) decodes `openwaldo_tokenizer.json` with
`encoding/json`, which matches keys case-insensitively and keeps the last
duplicate. The package's `tokenization_openwaldo.py` reads it with Python's
case-sensitive `json`. A file holding both `"artifact"` and `"Artifact"` (or a
duplicated key) makes WALDO validate and record one tokenizer while the
package's Hugging Face/MLX code runs the other.

Fix: refuse the file unless every key, at the top level and inside the
artifact, appears once and with its exact case. A token-stream check with
`json.Decoder` is one way. Test: two `TrainByteBPE` artifacts of the same
vocabulary in one file under `artifact` and `Artifact`; pull must refuse it.
Also cover a plain duplicate `"artifact"` key.

## 3. Clock-saved checkpoints must not choose the published weights

In `internal/training/workers/mlx.py` (artifact selection, ~line 818), a
checkpoint saved by `training.checkpoint-minutes` can become the
lowest-held-out candidate. That setting isn't in the run BOM, so two runs with
identical BOMs can publish different weights depending on GPU speed.

Fix: selection candidates are only the steps `checkpoint_every` schedules and
the final step; clock-saved checkpoints are for stopping and resuming only.
Update `docs/MODEL-LIFECYCLE.md` (the checkpoint-minutes paragraph: "not
artifact-selection candidates unless held-out scoring lands on the same step"
becomes "never artifact-selection candidates"). Test in
`testing/python/test_mlx_checkpoint_minutes.py`: a clock checkpoint on an
evaluated step with the lowest loss is not selected.

## 4. Legacy `waldo/bytepiece` models must still export

`validatePortableTokenizerExport` in `internal/modelexport/huggingface.go`
(~line 248) accepts only `waldotokenizer.TrainedName`. Pull, training,
`exportTokenizerArtifact` (~232) and the package's tokenizer code all accept
the legacy name too. Use `waldotokenizer.IsTrainedName`, fix the comment above
it, and add a test exporting a `waldo/bytepiece` model to Hugging Face.

## Not in scope

Leave the vet's minor items (throttle range in Python, chat warning wording,
ADR 0026/0029 notes, `runtimeWithoutDirectory`) alone unless I ask.
