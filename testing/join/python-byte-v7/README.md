# python-byte-v7: post-training on hello-python-v9's save point

Fine-tunes [hello-python-v9](../hello-python-v9/README.md)'s last merge, pinned as `base:`, on our
python-basics-v4 and python-programs-v1 SFT sets: 3 rounds of 2 epochs, 6 in all. python-basics-v1
passed its gate after about 80 epochs and answered every request with a program it had learned; this
fine-tunes lightly so pretraining still shows. The gate is 90% of 120 held-out python-programs-v1
programs, the 10 tasks it trained on; `measure.sh` also scores the 30-request demo, 20 of whose tasks
it never saw, to show whether it writes those or answers with what it learned.

`evaluate_python.py` asks for up to 512 tokens, not 256: a byte model's longest reference answers
(`read-file`) run 270 bytes, and a cut-off closing fence fails them, capping the gate at 0.85.

## Launch

When hello-python-v9 is complete, paste its last merge's pin over `base:` in `run.yaml`:

    ~/src/openwaldo/waldo-join-b/testing/join/pin-base.sh ~/.waldo/join/local/store/hello-python-v9/<closer>/<last merge>

then commit `run.yaml`, `eval.bom.json`, `eval.jsonl` and `evaluate_python.py` to the local runs
repository as `runs/python-byte-v7/`. It runs as v9's identities, v6's, whose stores already hold
the base.
A different fine-tune is a copy of this directory with a new name; the base stays the same.

    cd ~/.waldo/join/local/seed
    mkdir -p runs/python-byte-v7
    cp ~/src/openwaldo/waldo-join-b/testing/join/python-byte-v7/{run.yaml,eval.bom.json,eval.jsonl,evaluate_python.py} runs/python-byte-v7/
    git add runs/python-byte-v7 && git commit -m "runs/python-byte-v7: fine-tune hello-python-v9's save point" && git push

    WALDO_GPU_THROTTLE=0.30 ROUNDS=6 ~/src/openwaldo/waldo-waldito/testing/join/run-identities.sh python-byte-v7

    ~/src/openwaldo/waldo-join-b/testing/join/python-byte-v7/measure.sh

Results: TBD.
