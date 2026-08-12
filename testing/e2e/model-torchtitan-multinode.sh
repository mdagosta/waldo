#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "testing: multi-node TorchTitan lifecycle skipped (requires Linux)"
  exit 0
fi
if [ "${WALDO_E2E_MULTINODE:-0}" != "1" ]; then
  echo "testing: multi-node TorchTitan lifecycle skipped (set WALDO_E2E_MULTINODE=1 to opt in)"
  exit 0
fi

titan_python=""
for candidate in "$(command -v python3 2>/dev/null || true)" "$(command -v python 2>/dev/null || true)"; do
  [ -n "$candidate" ] && [ -x "$candidate" ] || continue
  if "$candidate" -c 'import torch,torchtitan; from torchtitan.distributed import ParallelDims; assert torch.cuda.is_available() and torch.cuda.device_count() >= 2' >/dev/null 2>&1; then
    titan_python=$candidate
    break
  fi
done
if [ -z "$titan_python" ]; then
  echo "testing: multi-node TorchTitan lifecycle skipped (needs 2+ visible CUDA GPUs and a TorchTitan runtime)"
  exit 0
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-torchtitan-mn-e2e.XXXXXX")

secondary_pid=""
cleanup() {
  if [ -n "$secondary_pid" ]; then
    kill "$secondary_pid" 2>/dev/null || true
    wait "$secondary_pid" 2>/dev/null || true
  fi
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved multi-node TorchTitan E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-torchtitan-mn-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

binary="$work/waldo"
index_root="$work/waldo-index"
staging="$work/staging"
models="$work/models"
input="$work/training.txt"
export WALDO_CONFIG="$work/config.json"

rendezvous="127.0.0.1:29500"
rendezvous_id="waldo-mn-smoke"

echo "testing: multi-node (2-node) TorchTitan lifecycle with $titan_python"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
printf 'OpenWALDO validates its multi-node TorchTitan rendezvous.\nThis corpus is disposable.\n' > "$input"

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
  --title TorchTitan-MN-E2E-Corpus \
  --description Disposable-multi-node-TorchTitan-input \
  --license CC0-1.0 \
  --source https://example.invalid/torchtitan-mn-e2e \
  --source-category public-dataset >/dev/null

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  contribution=$candidate
done
[ -n "$contribution" ] || { echo "contribution overlay not found" >&2; exit 1; }
cp -R "$contribution"/. "$index_root"/

"$binary" model init smoke --preset 10m >/dev/null

# Two nodes on one host, one visible GPU each. The secondary joins the
# rendezvous the primary hosts; only the primary (rank 0) authors model records.
CUDA_VISIBLE_DEVICES=1 "$binary" model train-worker \
  --nodes 2 --node-rank 1 --rendezvous "$rendezvous" --rendezvous-id "$rendezvous_id" &
secondary_pid=$!

CUDA_VISIBLE_DEVICES=0 "$binary" model train smoke core/e2e/torchtitan \
  --epochs 1 --nodes 2 --rendezvous "$rendezvous" --rendezvous-id "$rendezvous_id"

secondary_status=0
wait "$secondary_pid" || secondary_status=$?
secondary_pid=""
[ "$secondary_status" -eq 0 ] || { echo "secondary node exited $secondary_status" >&2; exit 1; }

summary=$("$binary" --json model summary smoke)
printf '%s\n' "$summary" | grep -Eq '"simulated"[[:space:]]*:[[:space:]]*false'
printf '%s\n' "$summary" | grep -Eq '"name"[[:space:]]*:[[:space:]]*"torchtitan"'

# The run BOM must pin the aggregate cluster: two nodes, world size two.
grep -ERq '"world_size"[[:space:]]*:[[:space:]]*2' "$models/smoke/runs" || { echo "run did not record world_size 2" >&2; exit 1; }
grep -ERq '"nodes"[[:space:]]*:[[:space:]]*2' "$models/smoke/runs" || { echo "run did not record nodes 2" >&2; exit 1; }

# Only the primary authors weights; the secondary writes no model records.
weights=$(find "$models/smoke/runs" -type f -name model.safetensors ! -path '*/checkpoints/*' -print)
[ -n "$weights" ] && [ -s "$weights" ] || { echo "primary did not produce terminal weights" >&2; exit 1; }

CUDA_VISIBLE_DEVICES=1 "$binary" model train-worker \
  --nodes 2 --node-rank 1 --rendezvous "$rendezvous" --rendezvous-id "$rendezvous_id-r2" &
secondary_pid=$!

CUDA_VISIBLE_DEVICES=0 "$binary" model train smoke core/e2e/torchtitan \
  --epochs 1 --nodes 2 --rendezvous "$rendezvous" --rendezvous-id "$rendezvous_id-r2"

secondary_status=0
wait "$secondary_pid" || secondary_status=$?
secondary_pid=""
[ "$secondary_status" -eq 0 ] || { echo "secondary node exited $secondary_status on initialized run" >&2; exit 1; }
runs=$(find "$models/smoke/runs" -mindepth 1 -maxdepth 1 -type d | wc -l)
[ "$runs" -eq 2 ] || { echo "expected 2 runs after initialized rerun, found $runs" >&2; exit 1; }

echo "E2E multi-node TorchTitan passed: 2-node rendezvous, aggregate world size 2, primary-authored weights, initialized rerun"
