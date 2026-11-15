#!/bin/sh
# Waits for the py-hello-waldo-v7 launcher to finish, then scores the final merge on the full held-out gate
# set and on the 55-request demo, and totals the GPU time, into results/. The demo's tiers: trained and
# taught tasks, paraphrases of taught tasks, two taught tasks chained (compose), and stretch tasks.
set -eu
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
waldo=$here/../../../waldo
mkdir -p "$here/results"
while pgrep -f "waldo model join" >/dev/null; do sleep 300; done
# The newest merge in any identity's models: only the last round's closers hold the final one.
latest=$(for dir in "$HOME"/.waldo/join/mdagosta-v6-*; do ls "$dir/models" | grep -E '^waldo-py-hello-waldo-v7-r[0-9]{4}-merge-[0-9a-f]{12}/?$' | tr -d / | sed "s|$| $dir|"; done | sort | tail -1)
model=${latest% *} bot=${latest#* }
echo "$model" > "$here/results/model.txt"
score() {
  WALDO=$waldo WALDO_CONFIG=$bot/waldo-config.json WALDO_GPU_THROTTLE=0.10 uv run -q --with mlx \
    python3 "$here/evaluate_python.py" --show --records "$here/$1" "$model" "$here/eval.bom.json" "$here/results/$2.json" \
    > "$here/results/$2.txt" 2>&1
}
score demo.jsonl demo
score eval.jsonl gate-full
awk '/^(pass|FAIL)  /{split($2, id, "-"); n[id[1]]++; if ($1 == "pass") p[id[1]]++} END{for (t in n) printf "%-10s %d/%d\n", t, p[t], n[t]}' "$here/results/demo.txt" | sort > "$here/results/tiers.txt"
python3 -I "$HOME/src/openwaldo/waldo-waldito/testing/join/timings.py" py-hello-waldo-v7 > "$here/results/timings.txt"
echo "done: $(tail -1 "$here/results/demo.txt")"
cat "$here/results/tiers.txt"
