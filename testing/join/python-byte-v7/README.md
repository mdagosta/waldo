# python-byte-v7: post-training on v6's save point

Fine-tunes [python-byte-v6](../python-byte-v6/README.md)'s last merge, pinned as `base:`, on our
python-basics-v4 and python-programs-v1 SFT sets: 3 rounds of 2 epochs, 6 in all. python-basics-v1
passed its gate after about 80 epochs and answered every request with a program it had learned; this
fine-tunes lightly so pretraining still shows. The gate is 90% of 120 held-out python-programs-v1
programs, the 10 tasks it trained on; `measure.sh` also scores the 30-request demo, 20 of whose tasks
it never saw, to show whether it writes those or answers with what it learned.

`evaluate_python.py` asks for up to 512 tokens, not 256: a byte model's longest reference answers
(`read-file`) run 270 bytes, and a cut-off closing fence fails them, capping the gate at 0.85.

## Launch

When v6 is complete, paste its last merge's pin over `base:` in `run.yaml`:

    ~/src/openwaldo/waldo-join-b/testing/join/pin-base.sh ~/.waldo/join/local/store/python-byte-v6/<closer>/<last merge>

then commit `run.yaml`, `eval.bom.json`, `eval.jsonl` and `evaluate_python.py` to the local runs
repository as `runs/python-byte-v7/`. It runs as v6's identities, whose stores already hold the base.
A different fine-tune is a copy of this directory with a new name; the base stays the same.

    cd ~/.waldo/join/local/seed
    mkdir -p runs/python-byte-v7
    cp ~/src/openwaldo/waldo-join-b/testing/join/python-byte-v7/{run.yaml,eval.bom.json,eval.jsonl,evaluate_python.py} runs/python-byte-v7/
    git add runs/python-byte-v7 && git commit -m "runs/python-byte-v7: fine-tune v6's save point" && git push

    caffeinate -s env ROUNDS=6 COOLDOWN=10 RUNS_REPO=$HOME/.waldo/join/local/runs.git \
      JOIN_IDENTITY_PREFIX=mdagosta-v6- IDENTITIES=3 \
      WALDO=$HOME/src/openwaldo/waldo-join-b/waldo \
      JOIN_EMAIL=346140425+mdagosta-bot@users.noreply.github.com \
      ~/src/openwaldo/waldo-waldito/testing/join/run-identities.sh runs/python-byte-v7

    ~/src/openwaldo/waldo-join-b/testing/join/python-byte-v7/measure.sh

Results: TBD.
