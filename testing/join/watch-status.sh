#!/bin/sh
# While a waldito launch runs, shows its newest round and progress through that round as this tab's
# Herdr status ("train  r1/3  20%"); ends when the launcher finishes or its process is gone.
#   watch-status.sh <plan>        (LOG=<launcher.log> to read another file)
set -u
plan=${1:?usage: watch-status.sh <plan>}
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
log=${LOG:-$HOME/.waldo/join/logs/$plan/launcher.log}
start=$(wc -l < "$log" 2>/dev/null || echo 0)
gone=0
while :; do
  if tail -n +"$((start + 1))" "$log" 2>/dev/null | grep -q '^all identities finished'; then
    herdr-status "done: training"
    exit 0
  fi
  status=$(python3 -I - "$log" "$here/$plan/run.yaml" <<'PY'
import re, sys
log, plan = sys.argv[1:3]
lines = open(log, errors="replace").read().splitlines()
mark = max([i for i, line in enumerate(lines) if "of this launch starting" in line], default=-1)
# Units each identity trains in a round: units x replicas / identities, from the plan.
try:
    text = open(plan).read()
    keys = lambda name: len(re.findall(r"^    [\w-]+:", re.search(rf"^  {name}:.*\n((?:    .*\n|[ \t]*\n)*)", text, re.M).group(1), re.M))
    per_round = max(1, -(-keys("units") * int(re.search(r"^  replicas:\s*(\d+)", text, re.M).group(1)) // keys("identities")))
except (OSError, AttributeError, ValueError, ZeroDivisionError):
    per_round = 1
seen = {}
for line in lines[mark + 1:]:
    m = re.match(r"\S+ \S+ (\S+): r(\d+)/(\d+) (\S+) (\S+)(?:\s+(\d+)%)?", line)
    if not m:
        continue
    who, rnd, total, phase, unit, pct = m.groups()
    state = seen.setdefault(who, {"round": None, "units": []})
    if state["round"] != rnd:
        state.update(round=rnd, units=[])
    if pct is not None and unit not in state["units"]:
        state["units"].append(unit)
    state.update(total=total, unit=unit, pct=None if pct is None else int(pct))
if not seen:
    sys.exit()
low = min(int(s["round"]) for s in seen.values())
progress = []
for s in seen.values():
    if int(s["round"]) > low:
        progress.append(100)
    elif s["pct"] is not None:
        progress.append(min(100, (s["units"].index(s["unit"]) + s["pct"] / 100) / per_round * 100))
    else:
        progress.append(100 if s["units"] else None)
total = next(s["total"] for s in seen.values() if int(s["round"]) == low)
known = None if None in progress else int(min(progress))
print(f"train  r{low}/{total}  {'-' if known is None else known}%")
PY
)
  herdr-status "${status:-training starting}"
  if pgrep -f "waldito.* $plan" > /dev/null; then gone=0; else gone=$((gone + 1)); fi
  if [ "$gone" -ge 3 ]; then
    herdr-status "stopped: train"
    exit 0
  fi
  sleep 60
done
