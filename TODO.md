# TODO

Work found while running joins on one Mac, newest first. WALDITO.md is the network plan; this is
the backlog. Each item says where it would land.

## Look inside the training data (waldito first, then upstream)

python-byte-v6 round 1, 2026-11-06: py-a's held-out loss didn't move (3.408 to 3.410) while its
training loss fell. Two throwaway Go programs built against `internal/shard` found why:

- Held-out records: a run's `PREFLIGHT.json` lists them as `<shard sha256>:<row>`; the shards sit
  in the lookaside cache (`waldo lookaside status`), and `shard.ReadRecordsAt` reads those rows.
  One file, ray's `map-of-ray.svg` (785K characters), was 57% of py-a's held-out bytes. Each round
  and unit draws a new sample (`pieceSeed`), so one huge record can swing a round's probe.
- Training mix: `shard.WalkRecords` over the six shards, by unit and file extension. Only ~41% of
  the bytes are `.py`; py-a is 29% `.svg`, 11% `.lock`, 10% `.json`; py-b 12% `.geojson`; records
  over 100 KB hold 27-61% of each unit's bytes. (Before the plan's content filters.)

Now `waldito data heldout <model or run>` and `waldito data mix <run.yaml>` (pyarrow over the cached
shards; mix applies the stage's content filters and matches waldo's included-record counts). After
those filters v6 is ~44% `.py`: py-a 34% (`.svg` 20%), py-b 29% (`.geojson` 28%, one 11.6 MB file
9%), py-c 64%. Upstream what proves useful. A plan can't filter by path or size today: record filters
cover licenses, languages, sources, exact metadata values, dates, and slices. A `source_path`
glob filter, or a record-size cap, would let a plan train on code; decide before restarting v6.

## Survive a network blip while closing (join-local)

python-byte-v5 round 4, 2026-11-06 02:42: Wi-Fi dropped while v5-2 and v5-3 were closing. Each
probe's `--evaluate-only` refreshes the managed index from GitHub, the fetch failed (DNS / i/o
timeout), and both identities exited. v5-1 had already closed and published, so it logged "nothing
until others finish" for 5 hours: the round needs 2 closers and the only one left had closed.
Relaunching resumes it.

- A failed index refresh could warn and use the checkout already on disk; a local run doesn't need
  GitHub to score held-out records.
- Done 2026-11-06 on join-local-b: `waldo model join` retries a failed step up to 6 times,
  doubling the wait, instead of exiting.

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

## Upstream crash window

- A crash between `initializeModel`'s rename and copying `base/model.safetensors` (or origin artifacts) leaves a model nothing can inspect; copy them in the `prepare` callback before the rename (found in lineage vet round 6).
