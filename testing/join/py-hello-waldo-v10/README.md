# py-hello-waldo-v10: small tasks said many ways, two steps said as the demo says them

[py-hello-waldo-v9](../py-hello-waldo-v9/README.md) with one change: python-programs-v7 in place of v6.
v9 scored 34/55 on the demo (trained 10, taught 16, paraphrase 2, compose 6, stretch 0) and wrote two
programs whose shape it never trained on (`12 * 7 - 5`, `"banana"[::-1].upper()`), but its gate fell
to 211/218: all six "prints the factorial of N" gave a number step (`17 * 9 / 5 + 32`), the phrasing's
28 records drowned by 1,936 steps on one number. It still stopped after one step of "the largest and
the smallest of 3, 17 and 8" and "sorts the list 5, 3, 9, 1 and prints it backwards".

python-programs-v7 keeps v6's records and adds 1,225:

- 152 factorial and 93 fibonacci, each worded several ways ("computes the factorial of 8", "prints 8
  factorial", "finds the factorial of 8"), on v1's training values only, beside 224 one-number
  neighbours of the same values: the square, cube, double and half of N, so the word picks the program;
- 154 two results of one source said as nouns: "prints the total and the average of 4, 9 and 2", "the
  sum and the length of the list 28, 45, 26";
- 602 chains joined by "and": "repeats empc twice and prints it in lowercase", "adds up the numbers
  from 4 to 53 and checks whether it is even".

It drops 4 records: v6 left a pair out at its first example shaped like a demo task, but kept that
pair's other list lengths, so sort then reverse (compose-06's shape at four numbers) trained at three,
five and six. v7 leaves out every pair with any example shaped like a demo task. The demo's largest and
smallest, and sort then reverse, stay untrained in any wording.

The demo is v6's, unchanged, so the tiers compare.

## Launch

`base:` pins hello-python-v10's last merge, as v4-v9 do. Publish `run.yaml`, `eval.bom.json`,
`eval.jsonl` and `evaluate_python.py` as `runs/py-hello-waldo-v10/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 caffeinate -s ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v10

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

Results: TBD.
