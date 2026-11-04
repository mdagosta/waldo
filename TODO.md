# TODO

Work found while running joins on one Mac, newest first. WALDITO.md is the network plan; this is
the backlog. Each item says where it would land.

## Prune old models (local, decide first)

Each bot's model store holds ~800 models (58 GB per bot, 3 bots); the join store holds 19 GB. Every
merge reads the whole store to find ancestors, so size costs heat and time every round.

Bot 1's store by run: python-local-v3 140, python-bpe-v1 122, python-local-v5 115, code-m0 83,
python-local-v1 59, python-local-v2 55, python-byte-v1 51, bot-test-* 97, python-byte-v2 13.

Keep:
- anything a live plan pins as `base:` (python-byte-v2 pins python-byte-v1's round-3 merge)
- the final merge of each finished run whose README reports results, and code-m0's round 5
  (python-bpe-v1's base)
- the current round's start model of any live run (closers score it)

Candidates: bot-test-*, python-local-v1..v5, and every per-unit replica of a finished run.

To decide:
- Do deleted models need their small records kept (ADR 0084: the chain stays walkable only where
  node and origin records remain)? If so, prune needs a "drop weights, keep records" mode.
- Prune by run name, by age, or by "no live plan reaches it"?
- Join store (published copies) and bot stores together or separately?

## Throttle held-out scoring (join-local, done 2026-11-04)

Closers scored 9 models each per round at full GPU speed; WALDO_GPU_THROTTLE now applies to
`evaluate_model` too. Rebuild `./waldo` between launches.

## Cache weight hashes in the ancestor search (PR after #42)

`lineageIndex` inspects every model in the store per merge, and Inspect re-hashes each model's base
weights. Cost grows with the store; it's #42's listed limit, now felt every round. Cache verified
hashes by file size and mtime, or skip verification when only lineage is needed.

## Gradient clipping in the MLX worker (own PR)

python-byte-v1 at learning rate 0.0006 diverged in 3 of 13 units and never recovered (round 4 py-c:
1.6 to 5.1 at step 140). v2 at 0.0003 still spikes (py-a 2.3 to 4.8 at step 52; py-c flat at 4.15)
but recovers. The MLX worker has no clipping. Add a `max_grad_norm` stage parameter, recorded in the
run BOM, so a recipe states it.

## Closers merge units that got worse (join policy)

Replicas that diverge identically agree, so a unit whose probe loss ends above its start is merged.
Consider dropping a unit that made no progress even when its replicas agree.

## Resume the PR stack after #41 review

#41 answered (schema check, `model list` skips unreadable models). Then rebase #42 and join-local
onto it.
