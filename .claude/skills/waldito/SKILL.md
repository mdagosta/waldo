---
name: waldito
description: waldito's ritual for a join plan: check, commit, publish, and hand over the launch. waldito check-plan's fast checks, three one-line readings (mission, openness, hygiene), then each commit and publish step shown and run exactly as shown, and the launch line for the user to type.
disable-model-invocation: true
argument-hint: "<plan name or directory, e.g. hello-python-v9> [throttle, default 0.25] [rounds, default 1]"
---

# waldito: launch a plan

The user publishes this work as open source and reads every step before it happens. Show each
command, then run exactly that command: what is on the screen is what lands in the repository.

## 1. Check (seconds)

- A bare name means `~/src/openwaldo/waldo-join-b/testing/join/<name>`.
- Run `~/src/openwaldo/waldo-waldito/waldito check-plan <plan directory>` and show its lines as
  they are. On any ✗, stop: say what to fix, and run nothing.

## 2. Three readings, one line each

From the plan's diff against the plan it follows, its README, and `docs/VISION.md`:

- **mission**: what this run changes, and whether it moves toward the goal.
- **openness**: provenance (filters live in the plan, shards and pins unchanged) and licenses.
- **hygiene**: names, pointers (a post-training plan's base, the previous plan's Results), and
  anything to cut. "Nothing to simplify" is a fine answer.

## 3. Steps

Find what belongs to this launch and isn't committed: `git status` in waldo-join-b (the plan
directory and READMEs it touched) and waldo-waldito (launcher and tooling), and check-plan's
`name` line for whether `runs/<name>` is already published. List the steps, numbered, one command
line each, exactly as they will run. Skip a step with nothing to do, and say so.

1. Commit the plan: `cd ~/src/openwaldo/waldo-join-b && git add <files> && git commit -s -m "<one line, 72 characters at most, in the user's voice>"`
2. Commit tooling, if any changed: the same form in `~/src/openwaldo/waldo-waldito`.
3. Copy: `mkdir -p <SEED>/runs/<name> && cp <plan directory>/run.yaml <files> <SEED>/runs/<name>/`, where
   `<files>` is everything join reads from the run directory: each gate's `evaluation` BOM and every
   `evaluator` argument that is a file in the plan directory (e.g. eval.bom.json, eval.jsonl,
   evaluate_python.py). A plan with no such gates copies run.yaml alone. README.md, measure.sh and
   demo.jsonl stay in the repo.
4. Publish: `cd <SEED> && git pull --rebase && git add runs/<name> && git commit -m "runs/<name>: <one line>" && git push`
5. Launch, for the user to type: `~/src/openwaldo/waldo-waldito/waldito <name>` (WALDO_GPU_THROTTLE from the shell is the budget; add ROUNDS=<n> to stop early)

`SEED` and `RUNS_REPO` come from `~/.waldo/join/launch.env`.

Under each commit, show what it holds: its files with `git diff --stat` and a line on what each
change does. Then stop. The user reads the steps to understand what is about to be published;
run nothing until they say go.

## 4. Run steps 1-4, after the user's go

- One Bash call per step, the command byte for byte as shown: no added flags, trailers or
  co-author lines.
- Stop at the first failure and show it. Don't improvise a fix; offer one.
- Never amend, force-push, or rebase anything but the seed's own pull.
- After publishing, confirm the runs repository holds the plan as committed:
  `git --git-dir=<RUNS_REPO> show HEAD:runs/<name>/run.yaml | cmp - <plan directory>/run.yaml`
- The launch is the user's: it runs for hours in their terminal. Print it last in its own block,
  with what the first lines should say (each identity's throttle) and when the first `SAVED` should
  appear.
