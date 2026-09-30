#!/bin/sh
# End-to-end `waldito join`: a local bare runs repo, a tiny plan, alice and bob
# alternating --once until the run is merged, with bob free-riding in round 1.
# Real MLX training on the code/permissive index; not part of all.sh.
# POSTTRAIN_DATA and POSTTRAIN_FETCHER override the post-training shard source.

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
data=${POSTTRAIN_DATA:-$HOME/src/openwaldo/post-training-data/data/sft/python-basics-v4/train/part-00000.jsonl}
fetcher=${POSTTRAIN_FETCHER:-$HOME/.waldo/waldito/fetchers/python-basics-v4.ini}
run=join-e2e
work=${TMPDIR:-/tmp}/waldito-join-e2e
waldito="$repo_root/waldito"

# Fresh state: this run's own models, clones, and the scratch repo and store.
cd "$repo_root"
./waldo --json model list | python3 -c '
import json, sys
names = [entry["name"] for entry in json.load(sys.stdin) if entry["name"].startswith("waldito-'$run'-")]
print("\n".join(names))' | xargs -r ./waldo model rm
rm -rf "$work" "$HOME/.waldo/waldito/join/e2e-alice" "$HOME/.waldo/waldito/join/e2e-bob"
mkdir -p "$work/keys" "$work/store"
for identity in e2e-alice e2e-bob; do
  ssh-keygen -q -t ed25519 -N "" -C "waldito $identity" -f "$work/keys/$identity"
done

git init -q --bare -b main "$work/runs.git"
git clone -q "$work/runs.git" "$work/seed"
mkdir -p "$work/seed/runs/$run"
cat > "$work/seed/runs/$run/plan.yaml" <<EOF
name: $run
units: [u0, u1]
replicas: 2
merge: mean
store: file://$work/store
identities:
  e2e-alice: $(cut -d' ' -f1,2 "$work/keys/e2e-alice.pub")
  e2e-bob: $(cut -d' ' -f1,2 "$work/keys/e2e-bob.pub")
tamper: {round: 1, unit: u1, identity: e2e-bob, kind: lazy}

pretrain:
  compose: pretrain.yaml.tmpl
  bootstrap_tokens: 2000000
  rounds: 1
  tokens_per_round: 4000000   # ~244 steps: 61 made the round worse, so honest replicas looked like disagreement
  shards:
    u0: [TheAlgorithms/Python]
    u1: [pallets/flask, httpie/cli]

posttrain:
  compose: sft.yaml.tmpl
  data: $data
  shard_by: record-hash
  fetcher: $fetcher
  rounds: 1
  epochs_per_round: 1

replica_tolerance: 0.25
EOF
architecture='architecture:
  family: decoder-transformer
  context_tokens: 512
  vocabulary_size: 259
  hidden_size: 128
  intermediate_size: 384
  layers: 4
  attention_heads: 4
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: bfloat16
  tokenizer:
    name: byte
    revision: builtin-byte-schema-1'
cat > "$work/seed/runs/$run/pretrain.yaml.tmpl" <<EOF
kind: waldo-model-compose
schema: 1
\$base_block
interaction:
  template: user-assistant-v1
$architecture
stages:
  - name: python
    type: pre-training
    objective: causal-language-modeling
    filter:
      main_content: true
      sources:
        include: \$sources
    corpora:
      - path: code/permissive
        weight: 1
    parameters:
      profile: causal-pretrain-weighted
      \$budget
      batch_size: 32
      sequence_length: 512
      learning_rate: 0.0006
      seed: \$seed
      shuffle_buffer_records: 1000000
      shuffle_buffer_bytes: 4294967296
\$extra
EOF
cat > "$work/seed/runs/$run/sft.yaml.tmpl" <<EOF
kind: waldo-model-compose
schema: 1
\$base_block
interaction:
  template: user-assistant-v1
$architecture
stages:
  - name: sft
    type: fine-tuning
    objective: assistant-response-modeling
    conversation:
      template: user-assistant-v1
      supervised_roles: [assistant]
    corpora:
      - path: \$corpus
    parameters:
      profile: causal-pretrain-shuffled
      \$budget
      batch_size: 8
      sequence_length: 512
      learning_rate: \$learning_rate
      seed: \$seed
      weight_decay: \$weight_decay
      evaluation_fraction: 0.05
\$extra
EOF
git -C "$work/seed" add -A
git -C "$work/seed" -c user.name=e2e -c user.email=e2e@waldito.invalid commit -q -m "runs/$run: plan"
git -C "$work/seed" push -q origin main

last=$(sed -n 's/^  rounds: //p' "$work/seed/runs/$run/plan.yaml" | awk '{total += $1} END {print total}')
final="runs/$run/rounds/round-$(printf %04d "$last")/merge.yaml"
passes=0
until git --git-dir="$work/runs.git" cat-file -e "main:$final" 2>/dev/null; do
  passes=$((passes + 1))
  [ "$passes" -le 20 ] || { echo "waldito-join: run not merged after 20 passes" >&2; exit 1; }
  for identity in e2e-alice e2e-bob; do
    "$waldito" join "$work/runs.git" "runs/$run" --identity "$identity" --key "$work/keys/$identity" \
      --email "$identity@waldito.invalid" --once
  done
done

# Round 1: bob's lazy u1 replica disagrees with alice's, so u1 is dropped and u0 alone is merged.
git --git-dir="$work/runs.git" show "main:runs/$run/rounds/round-0001/merge.yaml" | python3 -c '
import json, sys
merge = json.load(sys.stdin)
assert [unit["unit"] for unit in merge["accepted"]] == ["u0"], merge["accepted"]
assert [unit["unit"] for unit in merge["dropped"]] == ["u1"], merge["dropped"]'
git --git-dir="$work/runs.git" log --format='%an %s' main
echo "waldito-join: ok"
