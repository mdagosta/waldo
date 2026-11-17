# py-hello-waldo-v11: two-number tasks whose numbers don't give the verb away

[py-hello-waldo-v10](../py-hello-waldo-v10/README.md) with one change: python-programs-v8 in place of v7.
v10 scored 35/55 on the demo (trained 10, taught 18, paraphrase 3, compose 4, stretch 0) and 216/218 on
the gate. Two of its compose misses were the operator, not the structure: "checks whether the sum of 12
and 7 is even" gave `12 * 7 % 2 == 0`, and "multiplies 12 and 7, then subtracts 5" gave `12 * 7 / 5 + 5`.

v1-v7 draw each two-number task's numbers from its own range (multiply 2-30, add 1-99, subtract 50-999
less 1-49), so small numbers meant multiply and the model read the numbers, not the verb.
python-programs-v8 keeps v7's records and adds 643:

- add, subtract, multiply and divide share one pool of number pairs: 44 pairs show up under all four
  verbs, 121 under at least two;
- each verb is said a few ways ("finds the sum of 24 and 7", "takes 9 away from 27", "finds 55 times 10"),
  none a word the demo's paraphrase tasks hold out;
- each verb is chained with every number step, 423 chains, and stands alone beside them 220 times.

The demo and the gate are v10's, unchanged, so the scores compare. The generator keeps out any chain
shaped like a demo compose or stretch task (multiply then subtract, add then check even), so the demo
still asks whether the verb carries over to a pairing it never saw. After the demo, score
`../unseen.jsonl` too: every version since v5 has been tuned against the same 55 tasks.

## Launch

Commit python-programs-v8 in post-training-data and add the corpus to the index as v7 was. `base:` pins
hello-python-v10's last merge, as v4-v10 do. Publish `run.yaml`, `eval.bom.json`, `eval.jsonl` and
`evaluate_python.py` as `runs/py-hello-waldo-v11/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 caffeinate -s ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v11

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

## Results

Scored on the final merge, `waldo-py-hello-waldo-v11-r0002-merge-627cdf94bb60`. v10's scores are in
parentheses.

| | v11 | v10 |
|---|---|---|
| Demo | 35/55 | 35/55 |
| Gate | 215/218 | 216/218 |
| `../unseen.jsonl` | 17/20 | 18/20 |

Demo by tier: trained 10/10, taught 17/20 (18), paraphrase 3/10 (3), compose 5/10 (4), stretch 0/5 (0).

- v8's shared numbers fixed the operator slip on "checks whether the sum of 12 and 7 is even", the
  new compose pass.
- "multiplies 12 and 7, then subtracts 5" still comes out wrong, now `12 * 7 * 5`: "then subtracts"
  reads as another multiply.
- Taught lost `fizzbuzz`, which also fails on the unseen set. `repeat` and `words` fail in both versions.

A tie within a one-task margin on each set. v10 is the simpler model with the same or better scores, so
data-only changes look to have plateaued.
