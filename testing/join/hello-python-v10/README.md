# hello-python-v10: hello-python-v8, saving every 150 steps

[hello-python-v8](../hello-python-v8/README.md)'s plan with `checkpoint_every: 150`. The default
saves only at step 500 and a stage's end, so stopping v8 mid-round lost the whole round. Now a
stop costs at most 150 steps: the bootstrap saves at 150, 300, 450, 600 and 611, a 199-step round at
150 and 199. Each checkpoint is 145 MB (weights and optimizer state), and all are kept. The launcher
marks a progress line `SAVED` when an identity wrote one since the last report.

## Launch

Commit `run.yaml` to the local runs repository as `runs/hello-python-v10/`, then launch it at 0.25,
which keeps the fan off. Keep the lid open while it trains.

    mkdir -p ~/.waldo/join/local/seed/runs/hello-python-v10
    cp ~/src/openwaldo/waldo-join-b/testing/join/hello-python-v10/run.yaml ~/.waldo/join/local/seed/runs/hello-python-v10/

    cd ~/.waldo/join/local/seed && git pull --rebase && git add runs/hello-python-v10 && git commit -m "runs/hello-python-v10: v8 saving every 150 steps" && git push

    WALDO_GPU_THROTTLE=0.25 ROUNDS=1 ~/src/openwaldo/waldo-waldito/waldito hello-python-v10

When the run completes, its last merge is the save point:

    ~/src/openwaldo/waldo-join-b/testing/join/pin-base.sh ~/.waldo/join/local/store/hello-python-v10/<closer>/<last merge>

Results: TBD.
