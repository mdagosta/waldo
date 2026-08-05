#!/bin/sh
set -eu

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "testing: real MLX model lifecycle skipped (requires Apple Silicon)"
  exit 0
fi

mlx_python=""
for candidate in "$(command -v python3 2>/dev/null || true)" /opt/homebrew/bin/python3 /usr/local/bin/python3; do
  [ -n "$candidate" ] && [ -x "$candidate" ] || continue
  if "$candidate" -c 'import mlx.core as mx; mx.eval(mx.array([1]))' >/dev/null 2>&1; then
    mlx_python=$candidate
    break
  fi
done
if [ -z "$mlx_python" ]; then
  echo "testing: real MLX model lifecycle skipped (no Metal-capable MLX Python runtime)"
  exit 0
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-mlx-e2e.XXXXXX")

cleanup() {
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved MLX E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-mlx-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

binary="$work/waldo"
index_root="$work/waldo-index"
lookaside="$work/lookaside"
staging="$work/staging"
models="$work/models"
input="$work/training.txt"
compose="$work/model.yaml"
export WALDO_CONFIG="$work/config.json"

echo "testing: real MLX model lifecycle with $mlx_python"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
printf 'OpenWALDO trains real weights through MLX.\nThis tiny record exists only to validate the complete backend.\n' > "$input"

"$binary" index init "$index_root" >/dev/null
"$binary" config set lookaside "file://$lookaside" >/dev/null
"$binary" config set lookaside.cache "$work/cache" >/dev/null
"$binary" config set lookaside.scratch "$work/scratch" >/dev/null
"$binary" config set ingest.staging "$staging" >/dev/null
"$binary" config set model.root "$models" >/dev/null
"$binary" config set model.backend auto >/dev/null
"$binary" config set index "$index_root" >/dev/null

destination="$index_root/core/e2e/mlx"
"$binary" index ingest "$input" "$destination" \
  --title MLX-E2E-Corpus \
  --description Disposable-real-MLX-training-input \
  --license CC0-1.0 \
  --source https://example.invalid/mlx-e2e \
  --source-category public-dataset >/dev/null

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  contribution=$candidate
done
[ -n "$contribution" ] || { echo "MLX contribution overlay not found" >&2; exit 1; }
cp -R "$contribution"/. "$index_root"/

cat > "$compose" <<EOF
kind: waldo-model-compose
schema: 1
architecture:
  family: decoder-transformer
  context_tokens: 16
  vocabulary_size: 259
  hidden_size: 32
  intermediate_size: 64
  layers: 1
  attention_heads: 4
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: byte
    revision: builtin-byte-schema-1
stages:
  - name: pretrain
    type: pre-training
    objective: causal-language-modeling
    corpora:
      - core/e2e/mlx
    parameters:
      steps: 2
      batch_size: 1
      sequence_length: 16
      learning_rate: 0.001
      seed: 7
      checkpoint_every: 1
      evaluate_every: 1
EOF

output=$("$binary" model compose mlx-smoke "$compose")
printf '%s\n' "$output"
printf '%s\n' "$output" | grep -q 'backend       mlx@builtin-mlx-worker-schema-1'
summary=$("$binary" --json model summary mlx-smoke)
printf '%s\n' "$summary" | grep -Eq '"simulated"[[:space:]]*:[[:space:]]*false'
printf '%s\n' "$summary" | grep -Eq '"name"[[:space:]]*:[[:space:]]*"mlx"'
weights=$(find "$models/mlx-smoke/runs" -type f -name model.safetensors -print)
[ -n "$weights" ] && [ -s "$weights" ] || { echo "real MLX weights were not produced" >&2; exit 1; }
checkpoint_count=$(find "$models/mlx-smoke/runs" -type f -name 'step-*.safetensors' -print | wc -l | tr -d ' ')
[ "$checkpoint_count" -eq 2 ] || { echo "found $checkpoint_count MLX checkpoints, want 2" >&2; exit 1; }

train_output=$("$binary" model train mlx-smoke core/e2e/mlx)
printf '%s\n' "$train_output"
printf '%s\n' "$train_output" | grep -q 'backend       mlx@builtin-mlx-worker-schema-1'
summary=$("$binary" --json model summary mlx-smoke)
printf '%s\n' "$summary" | grep -Eq '"runs"[[:space:]]*:[[:space:]]*\['
printf '%s\n' "$summary" | grep -Eq '"initialization"[[:space:]]*:'
run_count=$(find "$models/mlx-smoke/runs" -type f -name RUN.json -print | wc -l | tr -d ' ')
[ "$run_count" -eq 2 ] || { echo "found $run_count MLX runs, want 2" >&2; exit 1; }
weights_count=$(find "$models/mlx-smoke/runs" -type f -name model.safetensors -print | wc -l | tr -d ' ')
[ "$weights_count" -eq 2 ] || { echo "found $weights_count terminal MLX weights, want 2" >&2; exit 1; }

echo "E2E MLX model passed: composed and directly trained real weights, checkpointed, evaluated, and persisted Safetensors"
