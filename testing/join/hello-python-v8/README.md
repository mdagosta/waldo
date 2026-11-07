# hello-python-v8: pretraining on Python to a save point

[python-byte-v6](../python-byte-v6/README.md)'s plan, trained only on Python and the docs and config
beside it: `.py`, `.pyi`, `.pyx`, `.md`, `.rst`, `.yaml`, `.yml`, `.toml`, `.cfg`, `.ini`, and
`requirements*.txt`. v6 stopped after round 1 because only ~44% of what it trained on was Python;
py-a was 20% SVG and py-b 28% GeoJSON. This filter keeps 213 of v6's 394 MB, about 80% of it
Python. Like v6 it pretrains only, and [python-byte-v7](../python-byte-v7/README.md) fine-tunes its
last merge.

The filter is a `source_path` metadata filter in the plan, so the run still pins the same shards and
the plan's hash records which files it read. WALDO's glob never crosses `/`, so each name pattern is
written once per directory depth. `waldito data mix run.yaml --keep '<globs>' --emit-filter`
regenerates it and shows what it keeps; WALDO's own preflight selected exactly the files it
predicted (py-a 10,567, py-b 9,380, py-c 22,349).

Each unit holds 52-105 MB and trains 6.5 MB a round, so over 12 rounds py-a and py-b see some files
about 1.5 times.

## Launch

Commit `run.yaml` to the local runs repository as `runs/hello-python-v8/`, then launch it at 0.30
(0.10 per identity): 0.25 kept the fan off, 0.36 didn't. Keep the lid open while it trains.

    cd ~/.waldo/join/local/seed
    mkdir -p runs/hello-python-v8
    cp ~/src/openwaldo/waldo-join-b/testing/join/hello-python-v8/run.yaml runs/hello-python-v8/

    git add runs/hello-python-v8 && git commit -m "runs/hello-python-v8: pretrain on Python to a save point" && git push

    WALDO_GPU_THROTTLE=0.30 ROUNDS=4 ~/src/openwaldo/waldo-waldito/testing/join/launcher.sh hello-python-v8

When the run completes, its last merge is the save point:

    ~/src/openwaldo/waldo-join-b/testing/join/pin-base.sh ~/.waldo/join/local/store/hello-python-v8/<closer>/<last merge>

Results: stopped 2026-11-06 a third of the way into the bootstrap; it saved only at step 500, so
[hello-python-v9](../hello-python-v9/README.md) restarts it saving every 150 steps.
