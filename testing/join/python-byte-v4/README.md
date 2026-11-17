# python-byte-v4: python-byte-v3's recipe in 20-minute rounds

[python-byte-v3](../python-byte-v3/README.md)'s model, data and learning rate, in rounds short enough
to test the mechanism end to end on the lineage design B' stack: about 24 steps per unit (800K bytes),
2 units per identity per round, scoring on 64 held-out records. v3 trained at about 2,280 tokens/s
per identity at throttle 0.08, so a round's training is about 12 minutes; scoring and merging take
the rest. v4 uses new identities: v3's model stores hold models the design-A binary made.

## Launch

The plan is this directory's `run.yaml`, `eval.bom.json`, `eval.jsonl` and `evaluate_python.py`,
committed to the local runs repository as `runs/python-byte-v4/`. ROUNDS is however many fit the day; it
resumes at the next round (stop during a cooldown). Keep the lid open while it trains.

    cd ~/.waldo/join/local/seed
    caffeinate -s env ROUNDS=3 COOLDOWN=10 RUNS_REPO=$HOME/.waldo/join/local/runs.git \
      JOIN_IDENTITY_PREFIX=mdagosta-v4- IDENTITIES=3 JOIN_THROTTLE=0.08 \
      WALDO=$HOME/src/openwaldo/waldo-join-b/waldo \
      JOIN_EMAIL=346140425+mdagosta-bot@users.noreply.github.com \
      ~/src/openwaldo/waldo-waldito/waldito runs/python-byte-v4

If round 1 runs well past 20 minutes, lower `tokens_per_round`; post-training round time is not yet
measured (`epochs_per_round: 1`).

Results: TBD.
