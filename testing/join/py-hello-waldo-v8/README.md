# py-hello-waldo-v8: minimal pairs

[py-hello-waldo-v7](../py-hello-waldo-v7/README.md) with one change: python-programs-v5 in place of v4.
v7 chained two steps for the first time (compose 1/10 to 3/10, two of them real: "apple in uppercase
and then its length" printed both), but chained them unasked: taught fell from 15/20 to 11/20 as
"prints apple in uppercase" printed it twice and "multiplies 12 and 7" gave `12 * 7 / 5 + 7`. Its gate
still lost 9 greetings with made-up words ("prints ylou world" gave `"ylou".count("y")`): training
showed such words only in string tasks.

python-programs-v5 keeps v4's 10,742 records and adds 2,225 minimal pairs whose prompts differ only in
the words that choose the program: 1,361 give each two-step request's first step alone over the same
values ("multiplies 46 and 39" beside "multiplies 46 and 39, then adds 5"), and 864 put made-up words
both in greetings and in string tasks ("prints tquu world", "prints the string tlxpoi in uppercase").
No greeting uses a gate word, and no pair repeats a prompt from either split of v4.

The demo is v6's, unchanged, so the tiers compare.

## Launch

`base:` pins hello-python-v10's last merge, as v4-v7 do. Publish `run.yaml`, `eval.bom.json`,
`eval.jsonl` and `evaluate_python.py` as `runs/py-hello-waldo-v8/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v8

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

Results: TBD.
