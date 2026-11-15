# py-hello-waldo-v9: two-step requests built from parts

[py-hello-waldo-v8](../py-hello-waldo-v8/README.md) with one change: python-programs-v6 in place of v5.
v8 scored 31/55 on the demo (trained 10, taught 16, paraphrase 2, compose 3, stretch 0) and 213/218
on the gate: its minimal pairs stopped v7 chaining steps unasked, but it chained only the 20 pairs v4
taught and stopped after the first step of any other ("the largest and the smallest of 3, 17 and 8"
printed only the max).

python-programs-v6 keeps v5's records but 7 and adds 1,936 two-step requests over 356 pairs, about 6
examples each, where v4 had 20 pairs of 80: 15 number sources (multiply, celsius to fahrenheit, the sum
of a list ...) by 13 steps on their result (adds 7, halves it, checks whether it is odd ...), string and
list steps chained ("reverses lyrf, then counts its vowels"), two prints of one source ("prints the list
3, 8, 1 sorted and then its sum"), and loops in sequence. 300 more give a pair's first step alone. The
8 pairs shaped like a demo compose task are left out (add then even, odd or divisible all blank to the
demo's sum-is-even), and so is every training record as near a gate record as the gate's
contamination check flags: v5's one, which failed v8's gate, and 6 v5 training prompts that were also
v5 evaluation prompts.

The demo is v6's, unchanged, so the tiers compare. `evaluate_python.py` opens the model once
(`waldo model chat --prompts`), so `measure.sh` takes minutes, not most of an hour.

## Launch

`base:` pins hello-python-v10's last merge, as v4-v8 do. Publish `run.yaml`, `eval.bom.json`,
`eval.jsonl` and `evaluate_python.py` as `runs/py-hello-waldo-v9/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 caffeinate -s ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v9

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

## Results

Final merge `waldo-py-hello-waldo-v9-r0002-merge-52f72babffe4`, 3 rounds, 5.9 h wall, 17.5 GPU-h.

Demo 34/55 (v8 31): trained 10/10, taught 16/20, paraphrase 2/10, compose 6/10 (v8 3), stretch 0/5.
No item fell from v8; three compose items rose, two of them pairs left out of training, so new program
shapes: "multiplies 12 and 7, then subtracts 5" gave `12 * 7 - 5`, and "banana backwards in uppercase"
gave `"banana"[::-1].upper()`. "Counts down from 3 to 1 and then prints Hello world" did both steps but
printed `Hello World!`. Still one step only: the largest and the smallest (max alone), sort then
reverse (reversed alone). Two compose passes are luck: `12 + 7 % 2 == 0` and `100 * 9 > 200` print
the right answer from the wrong program.

Gate 211/218 (v8 213): the greetings v8 lost now pass, but all six "prints the factorial of N" fail,
each answered with a v6 number step on N (`2 ** 2`, `5 ** 2 / 2`, `77 * 2 + 32`, `17 * 9 / 5 + 32`).
v6 kept that phrasing's 28 training records but added 1,936 compose records whose steps square,
double, halve or convert one number; "computes N factorial" still passes.
