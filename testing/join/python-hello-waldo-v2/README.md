# python-hello-waldo-v2: post-training on hello-python-v9's save point

Same as v1 without python-basics-v4: only python-programs-v1, whose prompts vary the value to
print, so more of the training is copying the request. 3 rounds of 4 epochs, 12 in all. Compare its `results/` with
[python-hello-waldo-v1](../python-hello-waldo-v1/README.md)'s.

Fine-tunes [hello-python-v9](../hello-python-v9/README.md)'s last merge, pinned as `base:`, on our
python-basics-v4 and python-programs-v1 SFT sets: 3 rounds of 2 epochs, 6 in all. python-basics-v1
passed its gate after about 80 epochs and answered every request with a program it had learned; this
fine-tunes lightly so pretraining still shows. The gate is 90% of 120 held-out python-programs-v1
programs, the 10 tasks it trained on; `measure.sh` also scores the 30-request demo, 20 of whose tasks
it never saw, to show whether it writes those or answers with what it learned.

`evaluate_python.py` asks for up to 512 tokens, not 256: a byte model's longest reference answers
(`read-file`) run 270 bytes, and a cut-off closing fence fails them, capping the gate at 0.85.

## Launch

`base:` in `run.yaml` pins hello-python-v9's last merge, round 12. It runs as v9's identities,
v6's, whose stores already hold the base. A different fine-tune is a copy of this directory with a
new name; the base stays the same. Publish `run.yaml`, `eval.bom.json`, `eval.jsonl` and
`evaluate_python.py` as `runs/python-hello-waldo-v2/`, then:

    ~/src/openwaldo/waldo-waldito/waldito python-hello-waldo-v2

When it finishes, the launcher runs `measure.sh`, which scores the final merge into `results/`.

Results: TBD.
