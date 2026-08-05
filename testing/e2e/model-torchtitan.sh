#!/bin/sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "testing: real TorchTitan model lifecycle skipped (requires Linux)"
  exit 0
fi

titan_python=""
for candidate in "$(command -v python3 2>/dev/null || true)" "$(command -v python 2>/dev/null || true)"; do
  [ -n "$candidate" ] && [ -x "$candidate" ] || continue
  if "$candidate" -c 'import torch,torchtitan; from torchtitan.distributed import ParallelDims; assert torch.cuda.is_available() and torch.cuda.device_count() > 0' >/dev/null 2>&1; then
    titan_python=$candidate
    break
  fi
done
if [ -z "$titan_python" ]; then
  echo "testing: real TorchTitan model lifecycle skipped (no usable GPU TorchTitan runtime)"
  exit 0
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-torchtitan-e2e.XXXXXX")

cleanup() {
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved TorchTitan E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-torchtitan-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

binary="$work/waldo"
index_root="$work/waldo-index"
staging="$work/staging"
models="$work/models"
input="$work/training.txt"
compose="$work/model.yaml"
export WALDO_CONFIG="$work/config.json"

echo "testing: real TorchTitan model lifecycle with $titan_python"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
printf 'OpenWALDO validates its distributed TorchTitan adapter.\nThis corpus is disposable.\n' > "$input"

"$binary" index init "$index_root" >/dev/null
"$binary" config set lookaside "file://$work/lookaside" >/dev/null
"$binary" config set lookaside.cache "$work/cache" >/dev/null
"$binary" config set lookaside.scratch "$work/scratch" >/dev/null
"$binary" config set ingest.staging "$staging" >/dev/null
"$binary" config set model.root "$models" >/dev/null
"$binary" config set model.backend torchtitan >/dev/null
"$binary" config set index "$index_root" >/dev/null

destination="$index_root/core/e2e/torchtitan"
"$binary" index ingest "$input" "$destination" \
  --title TorchTitan-E2E-Corpus \
  --description Disposable-real-TorchTitan-training-input \
  --license CC0-1.0 \
  --source https://example.invalid/torchtitan-e2e \
  --source-category public-dataset >/dev/null

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  contribution=$candidate
done
[ -n "$contribution" ] || { echo "TorchTitan contribution overlay not found" >&2; exit 1; }
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
      - core/e2e/torchtitan
    parameters:
      steps: 2
      batch_size: 1
      sequence_length: 16
      learning_rate: 0.001
      seed: 7
      checkpoint_every: 1
      evaluate_every: 1
EOF

output=$("$binary" model compose torchtitan-smoke "$compose")
printf '%s\n' "$output"
printf '%s\n' "$output" | grep -q 'backend       torchtitan@builtin-torchtitan-worker-schema-1'
summary=$("$binary" --json model summary torchtitan-smoke)
printf '%s\n' "$summary" | grep -Eq '"simulated"[[:space:]]*:[[:space:]]*false'
printf '%s\n' "$summary" | grep -Eq '"name"[[:space:]]*:[[:space:]]*"torchtitan"'
weights=$(find "$models/torchtitan-smoke/runs" -type f -name model.safetensors -print)
[ -n "$weights" ] && [ -s "$weights" ] || { echo "real TorchTitan weights were not produced" >&2; exit 1; }
checkpoint_count=$(find "$models/torchtitan-smoke/runs" -type f -name 'step-*.safetensors' -print | wc -l | tr -d ' ')
[ "$checkpoint_count" -eq 2 ] || { echo "found $checkpoint_count TorchTitan checkpoints, want 2" >&2; exit 1; }
grep -ERq '"world_size"[[:space:]]*:[[:space:]]*[1-9]' "$models/torchtitan-smoke/runs" || { echo "TorchTitan run did not persist world size" >&2; exit 1; }

echo "E2E TorchTitan model passed: distributed mesh, optimization, checkpoints, and portable weights verified"
