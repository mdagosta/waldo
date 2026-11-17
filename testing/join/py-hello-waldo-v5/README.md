# py-hello-waldo-v5: building blocks for unseen tasks

[py-hello-waldo-v4](../py-hello-waldo-v4/README.md) with one change: python-programs-v2 in place of v1.
v1-v4 pass about 95% of the gate's 10 trained families and none of the demo's 20 unseen tasks: they
copy the request's values and pick the nearest operation they were taught (`print(9 + 50)` for
"subtracts 9 from 50"). python-programs-v2 keeps v1's 2,270 records and adds 3,280 in 25 families that
each teach a building block through a task of its own: `*` as a rectangle's area, `-` as years
elapsed, `%` as an even check, `.lower()`, `min`, `len` and `sum` of a list, range steps of 3 or more.
Its generator (`post-training-data/scripts/generate_python_programs_v2.py --forbid demo.jsonl`) refuses
any training program shaped like a demo task, so the demo still measures tasks it never saw.

The gate and `measure.sh` are v4's, so the results compare directly.

## Launch

`base:` pins hello-python-v10's last merge, as v4 does. Publish `run.yaml`, `eval.bom.json`, `eval.jsonl`
and `evaluate_python.py` as `runs/py-hello-waldo-v5/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v5

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`.

Results: TBD.
