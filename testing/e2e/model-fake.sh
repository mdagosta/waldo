#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-model-e2e.XXXXXX")

cleanup() {
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved model E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-model-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

binary="$work/waldo"
index_root="$work/waldo-index"
lookaside="$work/lookaside"
cache="$work/cache"
scratch="$work/scratch"
staging="$work/staging"
models="$work/models"
model_export="$work/model-export"
input="$work/training.txt"
compose="$work/model.yaml"
disclosure="$work/eu-gpai.json"
export WALDO_CONFIG="$work/config.json"

echo "testing: complete fake-model lifecycle"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
printf 'Small deterministic training record.\nA second preserved line.\n' > "$input"

"$binary" index init "$index_root"
"$binary" config set lookaside "file://$lookaside"
"$binary" config set lookaside.cache "$cache"
"$binary" config set lookaside.cache.max-size 64MiB
"$binary" config set lookaside.scratch "$scratch"
"$binary" config set ingest.staging "$staging"
"$binary" config set model.root "$models"
"$binary" config set model.backend fake
"$binary" config set index "$index_root"

destination="$index_root/core/e2e/model-corpus"
"$binary" index ingest "$input" "$destination" \
  --title Model-E2E-Corpus \
  --description Disposable-fake-model-input \
  --license CC0-1.0 \
  --source https://example.invalid/model-e2e \
  --source-category public-dataset

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  [ -z "$contribution" ] || { echo "multiple contribution overlays found" >&2; exit 1; }
  contribution=$candidate
done
[ -n "$contribution" ] || { echo "contribution overlay not found" >&2; exit 1; }
cp -R "$contribution"/. "$index_root"/

"$binary" index audit "$destination"
cat > "$compose" <<EOF
kind: waldo-model-compose
schema: 1
architecture:
  family: decoder-transformer
  context_tokens: 128
  vocabulary_size: 256
  hidden_size: 64
  intermediate_size: 192
  layers: 2
  attention_heads: 4
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: float32
  tokenizer:
    name: byte
    revision: sha256:model-e2e
stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora:
      - core/e2e/model-corpus
    parameters:
      steps: 2
      batch_size: 1
      sequence_length: 64
      learning_rate: 0.001
      seed: 7
EOF

forecast_output=$("$binary" model forecast "$compose")
printf '%s\n' "$forecast_output"
printf '%s\n' "$forecast_output" | grep -q 'GPUS.*MFR.*ACCELERATOR.*MEMORY/GPU.*APPROX. TIME'
printf '%s\n' "$forecast_output" | grep -q 'Apple.*M4 Max 40-core GPU'
printf '%s\n' "$forecast_output" | grep -q 'NVIDIA.*H100 SXM'
if printf '%s\n' "$forecast_output" | grep -Eq 'BACKEND|FIT|~|unified'; then
  echo "forecast contains an unwanted display field" >&2
  exit 1
fi
[ ! -e "$models/smoke" ] || { echo "forecast created model state" >&2; exit 1; }
forecast_json=$("$binary" --json model forecast "$compose")
printf '%s\n' "$forecast_json" | grep -Eq '"catalog"[[:space:]]*:[[:space:]]*"openwaldo-training-hardware-'
printf '%s\n' "$forecast_json" | grep -Eq '"approximate_seconds"[[:space:]]*:'

build_output=$("$binary" model compose smoke "$compose")
printf '%s\n' "$build_output"
printf '%s\n' "$build_output" | grep -q 'composed model smoke'

inspect_output=$("$binary" model summary smoke)
printf '%s\n' "$inspect_output"
printf '%s\n' "$inspect_output" | grep -q 'complete'
printf '%s\n' "$inspect_output" | grep -q 'simulated'

json_inspection=$("$binary" --json model summary smoke)
printf '%s\n' "$json_inspection" | grep -Eq '"state"[[:space:]]*:[[:space:]]*"complete"'
printf '%s\n' "$json_inspection" | grep -Eq '"simulated"[[:space:]]*:[[:space:]]*true'
printf '%s\n' "$json_inspection" | grep -Eq '"profile"[[:space:]]*:[[:space:]]*"causal-pretrain-v1"'
printf '%s\n' "$json_inspection" | grep -Eq '"packing"[[:space:]]*:[[:space:]]*"continuous-eos-v1"'
printf '%s\n' "$json_inspection" | grep -Eq '"checkpoints"[[:space:]]*:'
printf '%s\n' "$json_inspection" | grep -Eq '"evaluations"[[:space:]]*:'

for required in PLAN.json MODEL.json MODEL-BOM.json; do
  [ -s "$models/smoke/$required" ] || { echo "missing model record $required" >&2; exit 1; }
done
run_count=$(find "$models/smoke/runs" -type f -name RUN.json -print | wc -l | tr -d ' ')
[ "$run_count" -eq 1 ] || { echo "found $run_count run records, want 1" >&2; exit 1; }
artifact_count=$(find "$models/smoke/runs" -type f -name fake-model.json -print | wc -l | tr -d ' ')
[ "$artifact_count" -eq 1 ] || { echo "found $artifact_count fake artifacts, want 1" >&2; exit 1; }
checkpoint_count=$(find "$models/smoke/runs" -type f -name 'step-*.json' -print | wc -l | tr -d ' ')
[ "$checkpoint_count" -eq 1 ] || { echo "found $checkpoint_count fake checkpoints, want 1" >&2; exit 1; }

if "$binary" model compose smoke "$compose" >/dev/null 2>&1; then
  echo "model compose replaced an existing model without --replace" >&2
  exit 1
fi
"$binary" model compose smoke "$compose" --replace >/dev/null

"$binary" model init manual --preset 10m >/dev/null
"$binary" model train manual core/e2e/model-corpus >/dev/null
"$binary" model summary manual | grep -q 'RUNS:.*1'
"$binary" model list 'm*' | grep -q 'manual'
"$binary" model bom manual | grep -q '"subject": "model"'
"$binary" model export manual "$model_export" >/dev/null
[ -s "$model_export/MODEL-BOM.json" ] || { echo "model export missing BOM" >&2; exit 1; }
if "$binary" model chat manual >/dev/null 2>&1; then
  echo "fake model unexpectedly supported chat" >&2
  exit 1
fi
"$binary" model rm manual >/dev/null
[ ! -e "$models/manual" ] || { echo "model rm left manual model" >&2; exit 1; }
if "$binary" bom export smoke "$disclosure" --format eu-gpai >/dev/null 2>&1; then
  echo "complete EU GPAI export unexpectedly passed without required facts" >&2
  exit 1
fi
"$binary" bom export smoke "$disclosure" --format eu-gpai --allow-incomplete
grep -q '"kind": "waldo-eu-gpai-training-content"' "$disclosure"
grep -q '"status": "incomplete-draft"' "$disclosure"

if find "$scratch" -type f -print 2>/dev/null | grep . >/dev/null 2>&1; then
  echo "model lifecycle left partial-download scratch files" >&2
  exit 1
fi

echo "E2E fake model passed: ingested, audited, forecasted, initialized, trained, composed, replaced, summarized, listed, exported, removed, and disclosed"
