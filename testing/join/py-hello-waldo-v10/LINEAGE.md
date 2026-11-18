# py-hello-waldo-v10: how it was trained, from the beginning

py-hello-waldo is a small model that writes Python. It reads and writes raw bytes with no learned
tokenizer, and it was built from random weights on one Mac with `waldo model join`. Each round,
three identities split the work into three units; two replicas train each unit, two closers check
the replicas against each other on held-out data, and the agreed ones are merged into the next
round's starting weights. It took three runs, each starting from the previous run's final merge,
pinned by hash, so the released weights trace back through every round to the exact data and signed
records behind them.

1. [hello-python-v9](../hello-python-v9/README.md), pretraining from scratch:
   - Model: 8 layers, 512 wide, byte tokenizer.
   - Data: the `.py` files, docs and config from 40 permissively licensed GitHub repos (django,
     pandas, ray, langchain…), filtered to about 80% Python and split into three units balanced by
     tokens.
   - Plan: a 20M-byte bootstrap round, then 12 rounds of 6.5 MB per unit. Batch 32 × 1,024 bytes,
     learning rate 3e-4. Gate: held-out loss ≤ 1.4.
   - Final merge: `waldo-hello-python-v9-r0012-merge-301056e64bda`.
2. [hello-python-v10](../hello-python-v10/README.md), more pretraining: continues from that merge
   with 12 more rounds of 6.5 MB per unit on the same data, with the same settings. Final merge:
   `waldo-hello-python-v10-r0011-merge-f0398167086c`.
3. [py-hello-waldo-v10](README.md), fine-tuning:
   - Data: prompt→program pairs from our `python-basics-v4` and `python-programs-v7` sets, covering
     counting, string operations, factorial and fibonacci worded many ways, and two-step "do X and Y"
     requests. Loss is on the answer only.
   - Plan: 3 rounds of 2 epochs on each unit's third of the pairs. Batch 16, learning rate 1e-4.
     Gate: at least 90% of 120 held-out programs must run and print the right output.
   - The run took 8.9 hours wall clock and 26.4 training hours.
   - Final model: `waldo-py-hello-waldo-v10-r0002-merge-646a8d4ef638`.
4. Result:
   - 216/218 on the full gate.
   - 35/55 on the demo: 10/10 on trained tasks and 18/20 on taught ones, but weak on paraphrases
     (3/10) and compositions (4/10), and 0/5 on stretch tasks.
   - The prompts and answers are in [results/demo.txt](results/demo.txt).
