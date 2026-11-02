#!/usr/bin/env bash
# Run the complete frozen rung-0007 interaction and retention suites.
set -euo pipefail

if (($# != 2)); then
  echo "usage: $0 MODEL OUTPUT-DIRECTORY" >&2
  exit 2
fi

model="$1"
output="$2"
here="$(cd "$(dirname "$0")" && pwd)"
rung6="$(cd "$here/../0006-general-base-350m" && pwd)"
mkdir -p "$output"

"$here/evaluate.sh" "$model" "$here/suite.json" \
  "$output/interaction.jsonl"
python3 "$here/score.py" "$here/suite.json" \
  "$output/interaction.jsonl" >"$output/interaction-score.json"
"$rung6/evaluate.sh" "$model" "$output/foundation-sampled.jsonl" \
  0.8 42 512 >"$output/foundation-sampled-summary.json"
"$rung6/evaluate.sh" "$model" "$output/foundation-greedy.jsonl" \
  0 42 512 >"$output/foundation-greedy-summary.json"
python3 "$rung6/score.py" "$output/foundation-sampled.jsonl" \
  "$output/foundation-greedy.jsonl" >"$output/foundation-score.json"
