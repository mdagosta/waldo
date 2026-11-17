# py-hello-waldo-v7: more wordings, and requests in two steps

[py-hello-waldo-v6](../py-hello-waldo-v6/README.md) with one change: python-programs-v4 in place of v3.
v6 wrote 15 of the 20 taught tasks in their trained words, but no paraphrase and no two-step request:
it ignored words it hadn't seen ("the product of 13 and 6" gave `13 + 6`) and did the first step of a
pair and stopped ("multiplies 12 and 7, then subtracts 5" gave `12 * 7`). python-programs-v4 keeps
v3's 8,101 records and adds 1,215 that ask for 27 taught tasks in 2-4 more wordings and lead-ins, and
1,426 in 20 families that chain two taught tasks ("multiplies 4 and 9, then adds 3").

The demo is v6's, unchanged, so the tiers compare. Its generator
(`post-training-data/scripts/generate_python_programs_v4.py --demo demo.jsonl`) holds the demo's
paraphrase words out of every training prompt and its compose and stretch shapes out of every training
program: training chains multiply-then-add, never the demo's multiply-then-subtract.

## Launch

`base:` pins hello-python-v10's last merge, as v4-v6 do. Publish `run.yaml`, `eval.bom.json`,
`eval.jsonl` and `evaluate_python.py` as `runs/py-hello-waldo-v7/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v7

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

Results: TBD.
