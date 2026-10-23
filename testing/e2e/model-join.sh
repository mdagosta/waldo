#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# End-to-end `waldo model join`: a local bare runs repo, a tiny plan, alice,
# bob, and carol taking turns --once until the run is merged, with bob
# free-riding on one unit in round 1 and carol closing round 1 dishonestly, to
# be outvoted by the other two closers. It trains for real with MLX on the configured index's
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

identities="e2e-alice e2e-bob e2e-carol"
rm -rf "$work" "$HOME/.waldo/join/e2e-alice" "$HOME/.waldo/join/e2e-bob" "$HOME/.waldo/join/e2e-carol"
mkdir -p "$work/keys" "$work/store"
for identity in $identities; do
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
closers: 3
merge: mean
store: file://$work/store
identities:
  e2e-alice: $(cut -d' ' -f1,2 "$work/keys/e2e-alice.pub")
  e2e-bob: $(cut -d' ' -f1,2 "$work/keys/e2e-bob.pub")
  e2e-carol: $(cut -d' ' -f1,2 "$work/keys/e2e-carol.pub")
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

# rounds.py reads the runs repo's main branch as data: whether a round is decided,
# its majority merge, and what the cheaters need to forge records that are well formed.
cat > "$work/rounds.py" <<'PYTHON'
import collections, hashlib, json, pathlib, shutil, subprocess, sys
git, run, command, number = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
rounds = f"runs/{run}/rounds"

def files(round_number, kind):
    listing = subprocess.run(["git", f"--git-dir={git}", "ls-tree", "--name-only", f"main:{rounds}/round-{round_number:04d}/{kind}/"],
                             capture_output=True, text=True).stdout.split()
    return {name: json.loads(subprocess.run(["git", f"--git-dir={git}", "show", f"main:{rounds}/round-{round_number:04d}/{kind}/{name}"],
                                            capture_output=True, text=True, check=True).stdout) for name in listing if name.endswith(".yaml")}

def decided(round_number):
    merges = list(files(round_number, "merges").values())
    votes = collections.Counter((m["model_sha256"], json.dumps(m["accepted"], sort_keys=True)) for m in merges)
    best = votes.most_common(1)
    if not best or best[0][1] < 2:
        return None, merges
    return next(m for m in merges if (m["model_sha256"], json.dumps(m["accepted"], sort_keys=True)) == best[0][0]), merges

def free_slot(round_number, unit, count):
    claims = files(round_number, "claims")
    return next(f"{unit}-{n}" for n in range(1, count + 1) if f"{unit}-{n}.yaml" not in claims)

if command == "decided":
    merge, _ = decided(number)
    sys.exit(0 if merge else 1)
if command == "merge":
    merge, merges = decided(number)
    print(json.dumps({"merge": merge, "merges": merges}))
if command in ("lazy-u2", "cheat-close"):
    seed, store, signer = pathlib.Path(sys.argv[5]), pathlib.Path(sys.argv[6]), sys.argv[7]
    base, _ = decided(number - 1)
    submissions = files(number, "submissions")
    claims = files(number, "claims")
    if command == "lazy-u2":
        # Not yet: round 1 has started, and bob has not taken part in u2.
        if not claims or any(c["identity"] == signer and c["slot"].startswith("u2-") for c in claims.values()):
            sys.exit(1)
        model = f"waldo-{run}-r{number:04d}-u2-{signer}"
        shutil.copytree(store / base["model"], store / model)
        pinned = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted((store / model).iterdir())}
        # Every unit trains the same fixed steps, so any honest record has the right tokens; one exists by now
        # or the free-rider waits a turn.
        honest = next(iter(submissions.values()), None)
        if honest is None:
            sys.exit(1)
        slot = free_slot(number, "u2", 2)
        out = {f"claims/{slot}.yaml": {"kind": "waldo-join-claim", "schema": 1, "run": run, "round": number, "stage": "pretrain",
                                      "slot": slot, "attempt": 1, "identity": signer, "created_at": honest["created_at"]},
               "submissions/u2-" + signer + ".yaml": {
                   "kind": "waldo-join-submission", "schema": 1, "run": run, "round": number, "stage": "pretrain", "unit": "u2",
                   "identity": signer, "base": base["model"], "base_sha256": base["model_sha256"], "model": model,
                   "model_sha256": pinned["model.safetensors"], "bom_sha256": pinned.get("BOM.json"), "files": pinned,
                   "tokens": honest["tokens"], "url": f"file://{store / model}", "created_at": honest["created_at"]}}
    else:
        # Not yet: every unit of the round has both replicas, and carol has not closed it.
        units = collections.Counter(s["unit"] for s in submissions.values())
        if sorted(units.values()) != [2, 2, 2] or any(c["identity"] == signer and c["slot"].startswith("close-") for c in claims.values()):
            sys.exit(1)
        # The cheat: a merge that accepts every unit, the free-rider's included, and publishes the free-rider's weights.
        lazy = submissions["u2-e2e-bob.yaml"]
        slot = free_slot(number, "close", 3)
        accepted = [{"unit": s["unit"], "identity": s["identity"], "model": s["model"], "model_sha256": s["model_sha256"], "tokens": s["tokens"]}
                    for name, s in sorted(submissions.items()) if s["identity"] == "e2e-bob"]
        out = {f"claims/{slot}.yaml": {"kind": "waldo-join-claim", "schema": 1, "run": run, "round": number, "stage": "pretrain",
                                      "slot": slot, "attempt": 1, "identity": signer, "created_at": lazy["created_at"]},
               f"merges/{signer}.yaml": {
                   "kind": "waldo-join-merge", "schema": 1, "run": run, "round": number, "stage": "pretrain", "identity": signer,
                   "method": "weighted-mean-f32-v1", "base": base["model"], "base_sha256": base["model_sha256"], "accepted": accepted,
                   "dropped": [], "model": lazy["model"], "model_sha256": lazy["model_sha256"], "files": lazy["files"], "url": lazy["url"],
                   "closed_at": lazy["created_at"]}}
    directory = seed / rounds / f"round-{number:04d}"
    for name, record in out.items():
        (directory / name).parent.mkdir(parents=True, exist_ok=True)
        (directory / name).write_text(json.dumps(record, indent=2) + "\n")
        print(directory / name)
PYTHON

rounds() {
  python3 "$work/rounds.py" "$work/runs.git" "$run" "$@"
}

# The cheaters write their records by hand and sign them with their own keys, so only
# the closers' measurements and the closers' majority can catch them.
cheat() {
  identity=$1 command=$2
  git -C "$work/seed" pull -q
  records=$(rounds "$command" 1 "$work/seed" "$work/store/$run" "$identity") || return 1
  for record in $records; do
    ssh-keygen -Y sign -f "$work/keys/$identity" -n waldo-join "$record" 2>/dev/null
  done
  git -C "$work/seed" add "runs/$run/rounds"
  git -C "$work/seed" -c user.name="$identity" -c user.email="$identity@waldo.invalid" commit -q -s -m "$run round 1: $command by $identity"
  git -C "$work/seed" push -q origin main
  echo "!! $identity: $command"
}

last=$(grep -c '^  rounds: 1' "$work/seed/runs/$run/plan.yaml")
passes=0
until rounds decided "$last"; do
  passes=$((passes + 1))
  [ "$passes" -le 20 ] || { echo "model-join: run not merged after 20 passes" >&2; exit 1; }
  for identity in $identities; do
    if rounds decided 0 && ! rounds decided 1; then
      [ "$identity" = e2e-bob ] && cheat e2e-bob lazy-u2 && continue
      [ "$identity" = e2e-carol ] && cheat e2e-carol cheat-close && continue
    fi
    "$waldo" model join "$work/runs.git" "runs/$run" --identity "$identity" --key "$work/keys/$identity" --once
  done
done

# Round 1 refused the free-rider's unit on its provenance and merged the honest units,
# outvoting carol's merge that took it; later rounds merged every unit. Each merge pins
# its BOM.json and records its evidence.
for round in $(seq 1 "$last"); do
  rounds merge "$round" | python3 -c '
import json, sys
decided, round_number = json.load(sys.stdin), int(sys.argv[1])
merge = decided["merge"]
honest = ["u0", "u1"] if round_number == 1 else ["u0", "u1", "u2"]
assert sorted(unit["unit"] for unit in merge["accepted"]) == honest, merge["accepted"]
assert merge["method"] == "weighted-mean-f32-v1" and "BOM.json" in merge["files"], merge
if round_number == 1:
    dissent = [m for m in decided["merges"] if m["model_sha256"] != merge["model_sha256"]]
    assert [m["identity"] for m in dissent] == ["e2e-carol"], [m["identity"] for m in decided["merges"]]
    print(f"round 1: e2e-carol'"'"'s merge was outvoted by", sorted(m["identity"] for m in decided["merges"] if m not in dissent))
assert sorted(unit["unit"] for unit in merge["evidence"]) == honest, merge.get("evidence")
for unit in merge["evidence"]:
    assert len(unit["replicas"]) == 2 and len(unit["evaluation_set_sha256"]) == 64 and len(unit["corpus_bom_sha256"]) == 64, unit
    print(f"round {round_number}", unit["unit"], "start", round(unit["start_loss"], 4), "replicas", [(r["identity"], round(r["heldout_loss"], 4)) for r in unit["replicas"]])
if round_number == 1:
    # The free-rider republished the starting package of the round, which records no training run of its
    # own, so the closers refuse it on provenance before measuring it, and u2 has too few replicas left.
    assert [unit["unit"] for unit in merge["dropped"]] == ["u2"], merge["dropped"]
    refused = [(r["unit"], r["identities"], r["reason"]) for r in merge.get("rejected", [])]
    assert [(u, i) for u, i, _ in refused] == [("u2", ["e2e-bob"])] and "provenance" in refused[0][2], refused
    print("round 1: e2e-bob u2 refused:", refused[0][2])
# The merge publishes its round'"'"'s run BOMs and its whole training history, and discloses it.
package = merge["url"][len("file://"):]
provenance = [name for name in merge["files"] if name.startswith("provenance-")]
assert len(provenance) == len(merge["accepted"]), provenance
closure = json.load(open(package + "/TRAINING-CLOSURE.json"))
assert closure["complete"] and len(closure["corpora"]) >= 3, [(c["corpus_bom_sha256"][:12], c["uses"]) for c in closure["corpora"]]
disclosure = json.load(open(package + "/EU-BOM.json"))
inherited = [c for c in disclosure["training"]["unique_corpora"] if "inherited" in c["stages"]]
assert len(inherited) >= 3, disclosure["training"]
print(f"round {round_number} merge publishes {len(provenance)} run BOMs and a complete history of {len(closure['"'"'corpora'"'"'])} corpora")' "$round"
done
git --git-dir="$work/runs.git" log --format='%an %s' main
echo "model-join: ok"
