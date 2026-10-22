#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# End-to-end `waldo model join`: a local bare runs repo, a tiny plan, alice and
# bob alternating --once until the run is merged, with bob free-riding on one
# unit in round 1. It trains for real with MLX on the configured index's
# code/permissive corpus and keeps its identities in ~/.waldo/join/e2e-*, so it
# is opt-in: WALDO_E2E_JOIN=1. Set JOIN_POSTTRAIN_CORPUS to an index path to add
# a post-training round whose units each train a filter.slice of it.

set -eu

if [ "${WALDO_E2E_JOIN:-0}" != "1" ]; then
  echo "testing: model join skipped (opt-in: WALDO_E2E_JOIN=1)"
  exit 0
fi
if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "testing: model join skipped (requires Apple Silicon)"
  exit 0
fi
if ! python3 -c 'import mlx.core as mx; mx.eval(mx.array([1]))' >/dev/null 2>&1; then
  echo "testing: model join skipped (no Metal-capable MLX Python runtime on PATH; try uv run --with mlx)"
  exit 0
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
waldo="$repo_root/waldo"
run=join-e2e
work=${TMPDIR:-/tmp}/waldo-model-join-e2e
posttrain_corpus=${JOIN_POSTTRAIN_CORPUS:-}

rm -rf "$work" "$HOME/.waldo/join/e2e-alice" "$HOME/.waldo/join/e2e-bob"
mkdir -p "$work/keys" "$work/store"
for identity in e2e-alice e2e-bob; do
  ssh-keygen -q -t ed25519 -N "" -C "$identity" -f "$work/keys/$identity"
done

git init -q --bare -b main "$work/runs.git"
git clone -q "$work/runs.git" "$work/seed"
mkdir -p "$work/seed/runs/$run"
cp "$repo_root/testing/join/pretrain.yaml.tmpl" "$repo_root/testing/join/sft.yaml.tmpl" "$work/seed/runs/$run/"
cat > "$work/seed/runs/$run/plan.yaml" <<EOF
kind: waldo-join-plan
schema: 1
name: $run
units: [u0, u1, u2]
replicas: 2
merge: mean
store: file://$work/store
identities:
  e2e-alice: $(cut -d' ' -f1,2 "$work/keys/e2e-alice.pub")
  e2e-bob: $(cut -d' ' -f1,2 "$work/keys/e2e-bob.pub")
pretrain:
  compose: pretrain.yaml.tmpl
  bootstrap_tokens: 2000000
  rounds: 1
  tokens_per_round: 4000000
  shards:
    u0: [TheAlgorithms/Python]
    u1: [pallets/flask, httpie/cli]
    u2: [nvbn/thefuck, psf/requests]
replica_tolerance: 0.25
EOF
if [ -n "$posttrain_corpus" ]; then
  cat >> "$work/seed/runs/$run/plan.yaml" <<EOF
posttrain:
  compose: sft.yaml.tmpl
  corpus: $posttrain_corpus
  rounds: 1
  epochs_per_round: 4
EOF
fi
git -C "$work/seed" add -A
git -C "$work/seed" -c user.name=e2e -c user.email=e2e@waldo.invalid commit -q -m "runs/$run: plan"
git -C "$work/seed" push -q origin main

# A free-rider: bob submits round 1's starting weights for u2 without training, with
# the round's token count, so only the closer's held-out measurement can catch it.
submit_lazy_u2() {
  rounds="runs/$run/rounds"
  git -C "$work/seed" pull -q
  python3 - "$work/seed" "$work/store/$run" "$rounds" <<'PYTHON'
import hashlib, json, pathlib, shutil, sys
seed, store, rounds = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
base = json.loads((seed / rounds / "round-0000" / "merge.yaml").read_text())
# Every unit trains the same fixed steps, so any honest round-1 record has the right tokens.
honest = json.loads(sorted((seed / rounds / "round-0001" / "submissions").glob("*.yaml"))[0].read_text())
model = "waldo-join-e2e-r0001-u2-e2e-bob"
shutil.copytree(store / base["model"], store / model)
files = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted((store / model).iterdir())}
record = {
    "kind": "waldo-join-submission", "schema": 1, "run": honest["run"], "round": 1, "stage": "pretrain",
    "unit": "u2", "identity": "e2e-bob", "base": base["model"], "base_sha256": base["model_sha256"],
    "model": model, "model_sha256": files["model.safetensors"], "bom_sha256": files.get("BOM.json"),
    "files": files, "tokens": honest["tokens"], "url": f"file://{store / model}", "created_at": honest["created_at"],
}
(seed / rounds / "round-0001" / "submissions" / "u2-e2e-bob.yaml").write_text(json.dumps(record, indent=2) + "\n")
PYTHON
  record="$work/seed/$rounds/round-0001/submissions/u2-e2e-bob.yaml"
  ssh-keygen -Y sign -f "$work/keys/e2e-bob" -n waldo-join "$record" 2>/dev/null
  git -C "$work/seed" add "$rounds"
  git -C "$work/seed" -c user.name=e2e-bob -c user.email=e2e-bob@waldo.invalid commit -q -s -m "$run round 1: u2 by e2e-bob (free-rider)"
  git -C "$work/seed" push -q origin main
  echo "!! e2e-bob submitted round 1's starting weights for u2 without training"
}

on_main() {
  git --git-dir="$work/runs.git" cat-file -e "main:runs/$run/rounds/$1" 2>/dev/null
}

# Join takes the lowest unit this identity has not submitted, so with two identities u2
# is bob's next unit in round 1 once he has submitted u0 and u1.
bob_turns_to_u2() {
  on_main round-0000/merge.yaml && ! on_main round-0001/submissions/u2-e2e-bob.yaml &&
    on_main round-0001/submissions/u0-e2e-bob.yaml && on_main round-0001/submissions/u1-e2e-bob.yaml
}

last=$(grep -c '^  rounds: 1' "$work/seed/runs/$run/plan.yaml")
final="round-$(printf %04d "$last")/merge.yaml"
passes=0
until on_main "$final"; do
  passes=$((passes + 1))
  [ "$passes" -le 20 ] || { echo "model-join: run not merged after 20 passes" >&2; exit 1; }
  for identity in e2e-alice e2e-bob; do
    if [ "$identity" = e2e-bob ] && bob_turns_to_u2; then
      submit_lazy_u2
      continue
    fi
    "$waldo" model join "$work/runs.git" "runs/$run" --identity "$identity" --key "$work/keys/$identity" --once
  done
done

# Round 1 dropped the free-rider's unit on measured evidence and merged the honest units;
# later rounds merged every unit. Each merge pins its BOM.json and records its evidence.
for round in $(seq 1 "$last"); do
  git --git-dir="$work/runs.git" show "main:runs/$run/rounds/round-$(printf %04d "$round")/merge.yaml" | python3 -c '
import json, sys
merge, round_number = json.load(sys.stdin), int(sys.argv[1])
honest = ["u0", "u1"] if round_number == 1 else ["u0", "u1", "u2"]
assert sorted(unit["unit"] for unit in merge["accepted"]) == honest, merge["accepted"]
assert merge["method"] == "weighted-mean-f32-v1" and "BOM.json" in merge["files"], merge
assert sorted(unit["unit"] for unit in merge["evidence"]) == ["u0", "u1", "u2"], merge.get("evidence")
for unit in merge["evidence"]:
    assert len(unit["replicas"]) == 2 and len(unit["evaluation_set_sha256"]) == 64, unit
    print(f"round {round_number}", unit["unit"], "start", round(unit["start_loss"], 4), "replicas", [(r["identity"], round(r["heldout_loss"], 4)) for r in unit["replicas"]])
if round_number == 1:
    assert [(unit["unit"], unit["reason"]) for unit in merge["dropped"]] == [("u2", "replicas disagree")], merge["dropped"]
    lazy = next(unit for unit in merge["evidence"] if unit["unit"] == "u2")
    losses = {r["identity"]: r["heldout_loss"] for r in lazy["replicas"]}
    # The free-rider scores the round start; honest training improved on it.
    assert abs(losses["e2e-bob"] - lazy["start_loss"]) < 1e-6 and losses["e2e-alice"] < lazy["start_loss"], lazy' "$round"
done
git --git-dir="$work/runs.git" log --format='%an %s' main
echo "model-join: ok"
