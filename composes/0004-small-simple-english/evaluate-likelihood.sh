#!/usr/bin/env bash
# Compare rung 0004 with the preserved rung-0002 foundation checkpoint.
set -euo pipefail

if (($# < 3 || $# > 4)); then
  echo "usage: $0 CANDIDATE_MODEL BASELINE_MODEL BASELINE_RUN_ID [OUTPUT_DIR]" >&2
  exit 2
fi

candidate_model="$1"
baseline_model="$2"
baseline_run_id="$3"
output_dir="${4:-/tmp/rung0004-likelihood}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
suite="$root/composes/0004-small-simple-english/suite.json"
worker="$root/internal/inference/workers/pytorch.py"
mkdir -p "$output_dir"

candidate_summary="$output_dir/candidate-summary.json"
baseline_summary="$output_dir/baseline-summary.json"
go run ./cmd/waldo/ --json model summary "$candidate_model" >"$candidate_summary"
go run ./cmd/waldo/ --json model summary "$baseline_model" >"$baseline_summary"
candidate_run_id="$(jq -r '.bom.current_run_id' "$candidate_summary")"

artifact_path() {
  local summary="$1" run_id="$2" role="$3"
  local model_path relative
  model_path="$(jq -r '.path' "$summary")"
  relative="$(jq -r --arg run "$run_id" --arg role "$role" \
    '.bom.runs[] | select(.id == $run) | .artifacts[] | select(.role == $role) | .path' "$summary")"
  if [[ -z "$relative" || "$relative" == null ]]; then
    echo "missing $role artifact for run $run_id" >&2
    return 1
  fi
  printf '%s/%s\n' "$model_path" "$relative"
}

candidate_config="$(artifact_path "$candidate_summary" "$candidate_run_id" configuration)"
candidate_weights="$(artifact_path "$candidate_summary" "$candidate_run_id" weights)"
candidate_tokenizer="$(artifact_path "$candidate_summary" "$candidate_run_id" tokenizer)"
baseline_config="$(artifact_path "$baseline_summary" "$baseline_run_id" configuration)"
baseline_weights="$(artifact_path "$baseline_summary" "$baseline_run_id" weights)"
baseline_tokenizer="$(artifact_path "$baseline_summary" "$baseline_run_id" tokenizer)"

jq 'group_by(.form) | map(. as $group | [range(0; length) as $index |
  $group[$index] + {continuation: $group[(($index + 1) % length)].continuation}]) | add' \
  "$suite" >"$output_dir/mismatched-suite.json"

go run ./composes/0004-small-simple-english/tools/encode-suite \
  "$candidate_tokenizer" "$suite" >"$output_dir/candidate-matched-encoded.json"
go run ./composes/0004-small-simple-english/tools/encode-suite \
  "$candidate_tokenizer" "$output_dir/mismatched-suite.json" >"$output_dir/candidate-mismatched-encoded.json"
go run ./composes/0004-small-simple-english/tools/encode-suite \
  "$baseline_tokenizer" "$suite" >"$output_dir/baseline-matched-encoded.json"
go run ./composes/0004-small-simple-english/tools/encode-suite \
  "$baseline_tokenizer" "$output_dir/mismatched-suite.json" >"$output_dir/baseline-mismatched-encoded.json"

evaluate() {
  python3 "$root/composes/0004-small-simple-english/likelihood.py" --worker "$worker" "$@"
}

evaluate --config "$candidate_config" --weights "$candidate_weights" \
  --encoded "$output_dir/candidate-matched-encoded.json" >"$output_dir/candidate-matched.json"
evaluate --config "$candidate_config" --weights "$candidate_weights" \
  --encoded "$output_dir/candidate-mismatched-encoded.json" >"$output_dir/candidate-mismatched.json"
evaluate --config "$baseline_config" --weights "$baseline_weights" \
  --encoded "$output_dir/baseline-matched-encoded.json" >"$output_dir/baseline-matched.json"
evaluate --config "$baseline_config" --weights "$baseline_weights" \
  --encoded "$output_dir/baseline-mismatched-encoded.json" >"$output_dir/baseline-mismatched.json"
evaluate --config "$candidate_config" --untrained \
  --encoded "$output_dir/candidate-matched-encoded.json" >"$output_dir/untrained-matched.json"
evaluate --config "$candidate_config" --untrained \
  --encoded "$output_dir/candidate-mismatched-encoded.json" >"$output_dir/untrained-mismatched.json"

python3 "$root/composes/0004-small-simple-english/compare.py" \
  "$output_dir/candidate-matched.json" "$output_dir/candidate-mismatched.json" \
  "$output_dir/baseline-matched.json" "$output_dir/baseline-mismatched.json" \
  "$output_dir/untrained-matched.json" "$output_dir/untrained-mismatched.json" \
  | tee "$output_dir/comparison.json"
