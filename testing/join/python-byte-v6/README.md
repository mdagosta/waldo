# python-byte-v6: pretraining to a save point

Pretraining only: [python-byte-v3](../python-byte-v3/README.md)'s round shape (6.5M bytes, about
200 steps per unit, learning rate 0.0003), from scratch, until the merge's held-out loss reaches 1.4.
v5's 24-step rounds barely moved; longer rounds than v3's drift apart before they merge. There's no
post-training: [python-byte-v7](../python-byte-v7/README.md) and later plans pin v6's last merge as
their `base:` and fine-tune it, so one long pretraining serves any number of post-training tries.
Architecture and tokenizer are fixed by that: a plan that changes them starts its own pretraining.

## Launch

Commit `run.yaml` to the local runs repository as `runs/python-byte-v6/`. The launcher splits
`WALDO_GPU_THROTTLE` (your 0.25) across the 3 identities, about 0.083 each: roughly 2 hours a round,
so 12 rounds plus the bootstrap is about a day; run it in chunks, stopping during a cooldown. Keep the
lid open while it trains.

    cd ~/.waldo/join/local/seed
    mkdir -p runs/python-byte-v6
    cp ~/src/openwaldo/waldo-join-b/testing/join/python-byte-v6/run.yaml runs/python-byte-v6/
    git add runs/python-byte-v6 && git commit -m "runs/python-byte-v6: pretrain to a save point" && git push

    caffeinate -s env ROUNDS=4 COOLDOWN=10 RUNS_REPO=$HOME/.waldo/join/local/runs.git \
      JOIN_IDENTITY_PREFIX=mdagosta-v6- IDENTITIES=3 \
      WALDO=$HOME/src/openwaldo/waldo-join-b/waldo \
      JOIN_EMAIL=346140425+mdagosta-bot@users.noreply.github.com \
      ~/src/openwaldo/waldo-waldito/testing/join/run-identities.sh runs/python-byte-v6

When the run completes, its last merge is the save point:

    ~/src/openwaldo/waldo-join-b/testing/join/pin-base.sh ~/.waldo/join/local/store/python-byte-v6/<closer>/<last merge>

prints the `base:` block for a post-training plan.

Results: stopped 2026-11-06 after the bootstrap and round 1 (held-out probes py-b 2.35 to 2.18,
py-c 2.36 to 2.03). Only ~44% of what it trained on was Python: py-a was 20% SVG, py-b 28% GeoJSON
(`waldito data mix`). [hello-python-v8](../hello-python-v8/README.md) restarts it on Python only.
