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

- A bare name means `~/src/openwaldo/waldo-waldito/testing/join/<name>`.
- Run `~/src/openwaldo/waldo-waldito/waldito check-plan <plan directory>` and show its lines as
  they are. On any ✗, stop: say what to fix, and run nothing.

## 2. Three readings, one line each

From the plan's diff against the plan it follows, its README, and `docs/VISION.md`:

- **mission**: what this run changes, and whether it moves toward the goal.
- **openness**: provenance (filters live in the plan, shards and pins unchanged) and licenses.
- **hygiene**: names, pointers (a post-training plan's base, the previous plan's Results), and
  anything to cut. "Nothing to simplify" is a fine answer.

## 3. Steps

Find what belongs to this launch and isn't committed: `git status` in waldo-waldito (the plan
directory and READMEs it touched, launcher and tooling), and check-plan's
`name` line for whether `runs/<name>` is already published. List the steps, numbered, one command
line each, exactly as they will run. Skip a step with nothing to do, and say so.

1. Commit the plan: `cd ~/src/openwaldo/waldo-waldito && git add <files> && git commit -s -m "<one line, 72 characters at most, in the user's voice>"`
2. Commit tooling, if any changed: the same form in `~/src/openwaldo/waldo-waldito`.
3. Copy: `mkdir -p <SEED>/runs/<name> && cp <plan directory>/run.yaml <files> <SEED>/runs/<name>/`, where
   `<files>` is everything join reads from the run directory: each gate's `evaluation` BOM and every
   `evaluator` argument that is a file in the plan directory (e.g. eval.bom.json, eval.jsonl,
   evaluate_python.py). A plan with no such gates copies run.yaml alone. README.md, measure.sh and
   demo.jsonl stay in the repo.
4. Publish: `cd <SEED> && git pull --rebase && git add runs/<name> && git commit -m "runs/<name>: <one line>" && git push`
5. Launch in this space's shell tab, then watch it: `~/src/openwaldo/waldo-waldito/testing/join/launch-in-shell.sh WALDO_GPU_THROTTLE=<budget> UV_OFFLINE=1 SAVE_MINUTES=10 caffeinate -s ~/src/openwaldo/waldo-waldito/waldito <name>` (add ROUNDS=<n> to stop early), and `~/src/openwaldo/waldo-waldito/testing/join/watch-status.sh <name>` in the background, which keeps this tab's status at `train  r1/3  20%` until the launcher finishes

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
- The user's go on the steps is the go for the launch too: step 5 types it into their shell tab, where
  they can read it, press keys, and stop it. Say what the first lines should say (each identity's
  throttle) and when the first `SAVED` should appear. Read progress from
  `~/.waldo/join/logs/<name>/launcher.log` (`hc read <shell terminal>` for the screen), and when the
  watcher ends, read the results: `measure.sh` runs by itself after the launcher. Don't poll: the
  watcher is the polling, and it costs no tokens. Look at the run when the user asks or the watcher ends.
