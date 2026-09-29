# waldito network plan

Goal: pretrain open code models on waldo-indexed data with many small machines, every
contribution verified and attributed, and no service of waldito's own.

## What exists (2026-09-28)

`waldito build <plan>` simulates the network on one Mac. The `waldito-python-basics` build:
3 contributors x 3 replicas, 4 pretrain + 5 post-train rounds, merged every round, a
free-rider rejected in round 2 (28% worse on the verifier's probe). Result: 349/374 held-out
and 184/220 out-of-distribution, versus 367/374 and 211/220 for one model trained on all the data.

## Three kinds of work

| unit | input | output | checked by |
| --- | --- | --- | --- |
| train | round base + data unit + seed | weights, run BOM | replicas' probe losses agree |
| probe | a submission + fixed probe set | held-out loss | several verifiers agree |
| merge | accepted submissions (or a tensor slice) | merged weights | replica hashes match exactly (CPU merge is deterministic) |

Everyone computes everything; the coordinator only applies rules.

## A round, from the plan alone

1. Plan (a static file, from anywhere) fixes the data mix, units, round schedule, seeds, rules.
2. Each contributor derives its unit: hash(identity, round) mod units. Overlaps become replicas.
3. Base = previous round's merge, recomputable by anyone from the log.
4. Train, upload weights anywhere (addressed by sha256), append a signed record to the log.
5. At the round deadline, the accepted set is every signed record passing the probe rule.
   Anyone can run the merge and get the same hash.

Shared state is three replaceable things: the plan, an append-only log of signed records
(git today), and blob storage (Hugging Face today). All mirrorable, all hash-verified.

## Rules learned

- Shard the data, not the skills. Split-by-skill failed with every merge (0-7/334).
- Merge every round. Merge-once lost to every-round in pretraining (3.55 vs 3.39).
- Enough steps per round: 72 failed, ~200+ worked. More contributors need more data or
  more rounds, not finer splits.
- Units are equal work (fixed steps), not whole repos: split big repos (vscode 3-of-12),
  group small ones, weight merges by tokens.
- Verify behavior, not weights: GPU training is not bit-reproducible. Judge replica gaps
  against the round's improvement (lazy = zero improvement). Loses power as training converges.
- Plain mean beat DiLoCo momentum at 5 rounds; revisit with tuning.

## Composing models

- One shared pretrain run per mix (e.g. Python + TypeScript + Go): vscode chunks, django,
  flask, thefuck are all units in the same rounds. Result: the general base.
- Separately finished pretrains are not averaged together; they drift apart.
- Specialists (vscode, your company's code) are fine-tunes of the shared base, run locally.
- Combining specialists = routing (mixture of experts, FlexOlmo-style), a later step.

## Milestones

1. Done: at equal sequential steps (10 post-train rounds, ~2250 steps per contributor) the
   network-built model scored 371/374 and 217/220, matching the single model (367/374, 211/220).
   Lower learning rate and post-merge consolidation did not help; the gap was under-training.
2. Hand-run one real round with 3-5 people: git ledger, Hugging Face uploads, a README.
3. `waldito join <plan>`: install, log in once, leave it running at WALDO_GPU_THROTTLE=0.25.
4. Python + TypeScript + Go, ~200M parameters, ~4B tokens (~2 days on 50 Macs, ~6 h on one H100).
5. All of code/permissive, ~400M parameters, with vscode as ~12 units.

## Needed in waldo (proposal to maintainers)

- Merge stage in compose (N pinned parents, mean or DiLoCo).
- Deterministic record sharding in compose filters.
- Chat on pulled models; publish weights by hash.
- Deterministic training (makes verification exact).

## Open problems

- Identity: one person posing as five. Signed identities + an allow-list in the plan.
- Backdoors: changes that keep probe loss intact are not caught by replicas.
- Cost of redundancy: 3 replicas = 3x compute; spot-checking may be enough.
