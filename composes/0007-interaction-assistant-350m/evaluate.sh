#!/usr/bin/env bash
set -euo pipefail

if (($# != 3)); then
  echo "usage: $0 MODEL SUITE.json OUTPUT.jsonl" >&2
  exit 2
fi

model="$1"
suite="$2"
output="$3"
run_id="$(go run ./cmd/waldo/ --json model summary "$model" | jq -r '.bom.current_run_id')"

: >"$output"
while IFS= read -r prompt; do
  go run ./cmd/waldo/ --json model chat "$model" \
    --run-id "$run_id" --temperature 0 --seed 42 --max-tokens 128 \
    "$prompt" >>"$output"
done < <(jq -r '.[].prompt' "$suite")
