# py-hello-waldo-v6: taught tasks, measured in other words and in pairs

[py-hello-waldo-v5](../py-hello-waldo-v5/README.md) with one change: python-programs-v3 in place of v2.
v5 used its building blocks but chose among them by keyword (`17 // 5` for "the remainder of 17 divided
by 5") and still failed the old demo's 20 unseen tasks: tasks as simple as multiplying were held out, so
"multiplies" never appeared beside `*`. python-programs-v3 keeps v2's 5,550 records and adds 2,551 in 20
families that teach those tasks directly.

The demo moves up a level to measure what's still held out. Its 55 requests, by tier:

- trained (10) and taught (20): v1's tasks and the old demo's 20 tasks, in the words training uses.
- paraphrase (10): a taught task in words no training prompt uses ("the product of 13 and 6").
- compose (10): two taught tasks in one request ("multiplies 12 and 7, then subtracts 5").
- stretch (5): tasks nothing taught builds, like a leap-year check; expected to fail.

Its generator (`post-training-data/scripts/generate_python_programs_v3.py --demo demo.jsonl`) drops
records with a demo prompt and refuses any training prompt with a paraphrase's held-out phrase or any
training program shaped like a compose or stretch task. The gate is v1-v5's, so it still compares.

## Launch

`base:` pins hello-python-v10's last merge, as v4 and v5 do. Publish `run.yaml`, `eval.bom.json`,
`eval.jsonl` and `evaluate_python.py` as `runs/py-hello-waldo-v6/`, then:

    UV_OFFLINE=1 SAVE_MINUTES=10 ~/src/openwaldo/waldo-waldito/waldito py-hello-waldo-v6

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`, with
the demo's score per tier in `results/tiers.txt`.

Results: TBD.
