#!/bin/sh
# End-to-end `waldo model join`: a local bare runs repo, a tiny plan, alice and
# bob alternating --once until the run is merged. Real MLX training on the
# code/permissive index; not part of all.sh. Set JOIN_POSTTRAIN_CORPUS to an
# index path to add a post-training round that splits it by filter.shard.

set -eu

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
name: $run
units: [u0, u1]
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

last=$(grep -c '^  rounds: 1' "$work/seed/runs/$run/plan.yaml")
final="runs/$run/rounds/round-$(printf %04d "$last")/merge.yaml"
passes=0
until git --git-dir="$work/runs.git" cat-file -e "main:$final" 2>/dev/null; do
  passes=$((passes + 1))
  [ "$passes" -le 20 ] || { echo "model-join: run not merged after 20 passes" >&2; exit 1; }
  for identity in e2e-alice e2e-bob; do
    "$waldo" model join "$work/runs.git" "runs/$run" --identity "$identity" --key "$work/keys/$identity" --once
  done
done

# Every round merged both units, pins its BOM.json, and records the probe evidence behind each verdict.
for round in $(seq 1 "$last"); do
  git --git-dir="$work/runs.git" show "main:runs/$run/rounds/round-$(printf %04d "$round")/merge.yaml" | python3 -c '
import json, sys
merge = json.load(sys.stdin)
assert sorted(unit["unit"] for unit in merge["accepted"]) == ["u0", "u1"], merge["accepted"]
assert merge["method"] == "token-weighted-mean" and "BOM.json" in merge["files"], merge
assert sorted(unit["unit"] for unit in merge["evidence"]) == ["u0", "u1"], merge.get("evidence")
for unit in merge["evidence"]:
    assert len(unit["replicas"]) == 2 and len(unit["evaluation_set_sha256"]) == 64, unit
    print(unit["unit"], "start", round(unit["start_loss"], 4), "replicas", [round(r["heldout_loss"], 4) for r in unit["replicas"]], "on", unit["evaluation_set_sha256"][:12])'
done
git --git-dir="$work/runs.git" log --format='%an %s' main
echo "model-join: ok"
