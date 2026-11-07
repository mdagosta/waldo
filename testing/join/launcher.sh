#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# Burn-in launcher for `waldo model join`: runs a plan's identities on one run at the same time,
# each throttled, each logging to ~/.waldo/join/logs/<run>/<identity>.log, with the Mac kept awake
# and its heat logged beside them to thermal.log (thermal-log.sh, when macmon is installed). What
# the launcher prints also goes to launcher.log there.
#
#   WALDO_GPU_THROTTLE=0.36 ROUNDS=4 launcher.sh <run>
#
# ~/.waldo/join/launch.env holds this machine's settings as `: "${RUNS_REPO:=...}"` lines, so the
# command line still overrides them: RUNS_REPO, the runs repository; WALDO; JOIN_EMAIL, the record
# commits' author; COOLDOWN. The identities are the ones the plan lists; for a plan that lists none,
# JOIN_IDENTITY_PREFIX1..IDENTITIES (default 5).
# WALDO_GPU_THROTTLE (default 1) is the whole machine's GPU budget, split evenly: each identity gets
# WALDO_GPU_THROTTLE / identities, at least 0.01.
# Rounds run one at a time until the run ends, or ROUNDS=<n> of them. A thermostat watches the
# machine, whatever heats it, a video render included: each minute, above HOT_C degrees (default
# 42) or with a fan running it pauses training, below COOL_C (default 35) it steps the budget back
# up by 5%, never past WALDO_GPU_THROTTLE, and in between it holds. It sets each identity's
# `waldo config set gpu.throttle`, which training re-reads. Between rounds the launcher waits at
# least COOLDOWN minutes (default 20), then until the fans stop and the GPU is at HOT_C or below.
#   ROUNDS=1 launcher.sh <run>                       # run the next round, then stop
#   ROUNDS=3 COOLDOWN=30 HOT_C=45 launcher.sh <run>  # three rounds, cooling at least 30 minutes
# Keep a laptop's lid open while it trains: closed, it runs hotter.
# REPORT=<minutes> sets how often the launcher prints progress, with the GPU load and fan speed over
# those minutes from thermal.log and SAVED when an identity wrote a checkpoint (default 5).
# FAULTS runs identities through burnin.py, which injects protocol faults as they join:
#   FAULTS=random launcher.sh <run>                  # every identity, random faults
#   FAULTS="<identity>=close <identity>=lazy" ...           # these identities, these faults
# FAULT_RATE (default 0.15) tunes random faults; burnin.py report runs/<run> checks the outcome.
#
# Stop them all with Ctrl-C; each identity resumes where it left off when run again.
# WALDO is the binary with `waldo model join` (default: the md/join-local worktree's, beside this one).

set -eu

[ ! -f "$HOME/.waldo/join/launch.env" ] || . "$HOME/.waldo/join/launch.env"
run_dir=${1:?usage: launcher.sh <run>}
case $run_dir in runs/*) ;; *) run_dir=runs/$run_dir ;; esac
repo=${RUNS_REPO:?set RUNS_REPO to the runs repository}
# The plan's identities, when the runs repository is one on this disk.
names=$(git --git-dir="$repo" show "HEAD:$run_dir/run.yaml" 2>/dev/null | awk '
  /^  identities:/ { listed = 1; next }
  listed && /^    [^ #]/ { sub(/^ +/, ""); sub(/:.*/, ""); print; next }
  listed && /^  [^ ]/ { listed = 0 }')
if [ -z "$names" ]; then
  prefix=${JOIN_IDENTITY_PREFIX:?the plan lists no identities; set JOIN_IDENTITY_PREFIX, as in <account>-bot-}
  names=$(seq 1 "${IDENTITIES:-5}" | sed "s/^/$prefix/")
fi
identities=$(echo "$names" | wc -l | tr -d ' ')
# The plan's rounds after the bootstrap, so progress reads r2/12.
total=$(git --git-dir="$repo" show "HEAD:$run_dir/run.yaml" 2>/dev/null | awk '/^  rounds:/ {
  for (i = 1; i <= NF; i++) if ($i ~ /^(pretrain|posttrain):$/) { value = $(i + 1); gsub(/[^0-9]/, "", value); sum += value } }
  END { if (sum) print sum }')
throttle=$(awk -v budget="${WALDO_GPU_THROTTLE:-1}" -v n="$identities" 'BEGIN { t = budget / n; printf "%.4f", (t < 0.01 ? 0.01 : t) }')
rounds=${ROUNDS:-0}
cooldown=${COOLDOWN:-20}
hot=${HOT_C:-42}
cool=${COOL_C:-35}
budget=${WALDO_GPU_THROTTLE:-1}
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
waldo=${WALDO:-$(CDPATH='' cd -- "$script_dir/../../.." && pwd)/waldo-join-local/waldo}
[ -x "$waldo" ] || { echo "no waldo binary at $waldo; set WALDO" >&2; exit 1; }
logs=$HOME/.waldo/join/logs/$(basename "$run_dir")

mkdir -p "$logs"
# Everything printed here also goes to launcher.log beside the identities' logs.
if [ -z "${RUN_IDENTITIES_TEE:-}" ]; then
  RUN_IDENTITIES_TEE=1 "$0" "$@" 2>&1 | tee -a "$logs/launcher.log"
  exit
fi
trap 'kill 0' INT TERM
echo "keep the lid open while training"
caffeinate -s -w $$ &
thermal=""
thermostat_pid=""
if command -v macmon >/dev/null; then
  "$script_dir/thermal-log.sh" "$logs/thermal.log" > /dev/null &
  thermal=$!
else
  echo "macmon isn't installed, so heat isn't logged"
fi

# phase <log>: an identity's current phase from the end of its log, training in 10% steps, led by the
# join round it belongs to (r2), not the launcher's own count of rounds.
phase() {
unit=$(grep '^== Round .*, unit ' "$1" 2>/dev/null | tail -1 | sed 's/.*, unit \([^:]*\):.*/\1/')
round=$(grep '^== Round ' "$1" 2>/dev/null | tail -1 | sed 's/^== Round \([0-9]*\).*/\1/')
tail -c 4000 "$1" 2>/dev/null | tr '\r' '\n' | awk -v unit="$unit" -v round="$round" -v total="$total" '
  match($0, /step [0-9]+\/[0-9]+,/) {
    split(substr($0, RSTART + 5, RLENGTH - 6), s, "/")
    stage = $1; sub(/.*\//, "", stage)
    line = stage " " unit " " int(s[1] * 10 / s[2]) * 10 "%"; next }
  /"scoring / { m = $0; sub(/.*"scoring /, "", m); sub(/ on .*/, "", m); sub(/.*-r[0-9][0-9][0-9][0-9]-/, "", m)
    if (m ~ /^merge-/) m = "round start"
    line = "scoring " m; next }
  /unit [^ ]+: accepted/ { m = $0; sub(/.*unit /, "", m); sub(/:.*/, "", m); line = "unit " m " accepted"; next }
  /^merge  .*merging/ { m = $0; sub(/.*merging /, "", m); sub(/ \(.*/, "", m); line = "merging " m; next }
  /^-- Merge round/ { line = "merged, publishing"; next }
  /until others finish/ { line = "waiting for others"; next }
  /is merged; .* stops/ { line = "round done"; next }
  END { if (line != "") print (round != "" ? "r" round (total != "" ? "/" total : "") " " : "") line }'
}

# report: every REPORT minutes (default 5), prints each identity's phase when it changes.
# saves <log>: how many checkpoints an identity has written.
saves() {
cat "$1" 2>/dev/null | tr '\r' '\n' | grep -c 'checkpoint step .* persisted' || true
}

report() {
# Stopped between rounds, the reporter just exits; the launcher's kill-everything trap isn't its own.
trap - INT TERM
n=0
for identity in $names; do
  n=$((n + 1))
  now=$(phase "$logs/$identity.log")
  eval "phase_$n=\$now saves_$n=$(saves "$logs/$identity.log")"
done
while sleep $((${REPORT:-5} * 60)); do
  # thermal.log's minutes since the last report: mean GPU and CPU busy and chip watts, hottest GPU,
  # fastest fan. The CPU is everything else on the machine too.
  heat=$(tail -n "${REPORT:-5}" "$logs/thermal.log" 2>/dev/null | awk '$1 ~ /:/ && NF >= 8 {
    gpu += $2; cpu += $3; watts += $5; if ($7 > hot) hot = $7; if ($8 > fan) fan = $8; n++ }
    END { if (n) printf "gpu %3.0f%% cpu %3.0f%% %4.1fW %3.0fC fan %d", gpu / n, cpu / n, watts / n, hot, fan }')
  n=0
  printed=""
  for identity in $names; do
    n=$((n + 1))
    now=$(phase "$logs/$identity.log")
    count=$(saves "$logs/$identity.log")
    eval "was=\${phase_$n:-} saved=\${saves_$n:-0}"
    # SAVED: a checkpoint written since the last report, so stopping now loses little.
    mark=""
    [ "$count" -le "$saved" ] || mark=" SAVED"
    if [ -n "$now" ] && { [ "$now" != "$was" ] || [ -n "$mark" ]; }; then
      printf '%s %s: %-34s %s%s\n' "$(date '+%a %H:%M')" "$identity" "$now" "$heat" "$mark"
      eval "phase_$n=\$now saves_$n=$count"
      printed=1
    fi
  done
  # A fan is news even when no phase changed.
  [ -n "$printed" ] || ! echo "$heat" | awk '$NF + 0 > 0 { found = 1 } END { exit !found }' || echo "$(date '+%a %H:%M') $heat"
done
}

# launch <rounds>: starts every identity for that many rounds (0: until the run ends) and waits.
launch() {
pids=""
for identity in $names; do
  email=${JOIN_EMAIL:-$identity@waldo.invalid}
  echo "starting $identity (throttle $throttle) -> $logs/$identity.log"
  mode=""
  for fault in ${FAULTS:-}; do
    if [ "$fault" = random ]; then
      mode=random
    elif [ "${fault%%=*}" = "$identity" ]; then
      mode="$mode ${fault#*=}"
    fi
  done
  if [ -n "$mode" ]; then
    echo "  $identity injects faults: $mode"
    # shellcheck disable=SC2086 # one argument per fault
    WALDO_GPU_THROTTLE=$throttle WALDO=$waldo RUNS_REPO=$repo JOIN_EMAIL=$email JOIN_LOGS=$logs \
      uv run -q --with mlx --with huggingface_hub python3 -u "$script_dir/burnin.py" "$identity" "$run_dir" $mode \
      >> "$logs/$identity.log" 2>&1 &
    pids="$pids $!"
  else
    WALDO_GPU_THROTTLE=$throttle uv run -q --with mlx --with huggingface_hub \
      "$waldo" model join "$repo" "$run_dir" --identity "$identity" --email "$email" --rounds "$1" --wait 30 \
      >> "$logs/$identity.log" 2>&1 &
    pids="$pids $!"
  fi
  sleep 5
done
report &
reporter=$!
# shellcheck disable=SC2086 # one argument per identity
wait $pids || true
kill "$reporter" 2>/dev/null || true
}

# finish: what the run reached, its latest merge saying hello, and the plan's own checks.
finish() {
name=$(basename "$run_dir")
plan=$(git --git-dir="$repo" show "HEAD:$run_dir/run.yaml" 2>/dev/null)
gate=$(echo "$plan" | sed -n 's/.*heldout_loss_at_most: *\([0-9.]*\).*/\1/p' | head -1)
echo
echo "== $name: held-out loss on each unit's data, at the round's start -> after it"
# Closers record each unit as they accept it; every closer repeats the same numbers.
results=$(for identity in $names; do
  cat "$logs/$identity.log" 2>/dev/null | tr '\r' '\n' | awk '
    /^== Round / { round = $3 }
    /round start probe loss/ { start = $5 }
    /replicas 1 vs 2: probe loss/ { end = $7 }
    /unit [^ ]+: accepted/ { unit = $2; sub(/:$/, "", unit); print round, unit, start, end }'
done | sort -u -k1,1n -k2,2)
echo "$results" | awk 'NF == 4 { printf "   r%s %-5s %s -> %s\n", $1, $2, $3, $4 }'
last=$(echo "$results" | awk 'NF == 4 { round = $1 } END { print round }')
echo "$results" | awk -v round="$last" -v gate="$gate" 'NF == 4 && $1 == round { sum += $4; n++ }
  END { if (n) printf "   r%s average %.3f%s\n", round, sum / n, (gate != "" ? ", gate at most " gate : "") }'
cat "$logs"/*.log 2>/dev/null | tr '\r' '\n' | grep 'gate after' | sort -u | sed 's/^ */   /'
# The latest merge, from whichever identity holds it under its plain name, at temperature 0.
pick=$(for identity in $names; do
  for dir in "$HOME/.waldo/join/$identity/models/waldo-$name-r"*-merge-*; do
    [ -d "$dir" ] && echo "$(basename "$dir" | sed 's/.*-r\([0-9]*\)-merge-.*/\1/') ${#dir} $identity $(basename "$dir")"
  done
done | sort -k1,1r -k2,2n | head -1)
[ -n "$pick" ] || { echo "no merge yet to chat with"; return 0; }
set -- $pick
if echo "$plan" | grep -q 'type: fine-tuning'; then
  prompt="Write a Python program that prints Hello Waldo." raw=""
else
  # A pretrained base continues text, so it gets a comment to finish as code.
  prompt='# Print "Hello Waldo" in Python
' raw="--raw"
fi
echo
echo "== $4 (held by $3) says:"
echo "\$ waldo model chat $4 ${raw:+$raw }--temperature 0 --max-tokens 120 '$(echo "$prompt" | head -1)'"
WALDO_CONFIG="$HOME/.waldo/join/$3/waldo-config.json" uv run -q --with mlx --with huggingface_hub \
  "$waldo" model chat "$4" $raw --temperature 0 --max-tokens 120 "$prompt" 2>&1 || echo "(chat failed)"
measure="$(dirname "$waldo")/testing/join/$name/measure.sh"
if [ -x "$measure" ]; then
  echo
  echo "== $name/measure.sh"
  "$measure" || echo "(measure.sh failed)"
fi
}

# set_throttle <budget>: each identity's share of a machine budget, live in its own waldo config.
set_throttle() {
for identity in $names; do
  share=$(awk -v budget="$1" -v n="$identities" 'BEGIN { t = budget / n; printf "%.4f", (budget > 0 && t < 0.01 ? 0.01 : t) }')
  WALDO_CONFIG="$HOME/.waldo/join/$identity/waldo-config.json" "$waldo" config set gpu.throttle "$share" >/dev/null 2>&1 ||
    echo "can't set $identity's gpu.throttle; does $waldo know it?"
done
}

# thermostat: each minute, from thermal.log's last minute: above HOT_C or with a fan running,
# pause; below COOL_C, step up 5% of the budget; in between, hold.
thermostat() {
trap - INT TERM
level=$budget
while sleep 60; do
  next=$(tail -n 1 "$logs/thermal.log" 2>/dev/null | awk -v level="$level" -v budget="$budget" -v hot="$hot" -v cool="$cool" '
    $1 ~ /:/ && NF >= 8 {
      if ($8 > 0 || $7 > hot) level = 0
      else if ($7 < cool) { level += budget / 20; if (level > budget) level = budget }
      printf "%.4f %dC fan %d\n", level, $7, $8 }')
  [ -n "$next" ] || continue
  set -- $next
  if [ "$1" != "$(printf '%.4f' "$level")" ]; then
    echo "$(date '+%a %H:%M') thermostat: throttle $(printf '%.2f' "$level") -> $(printf '%.2f' "$1") at $2, $3 $4"
    level=$1
    set_throttle "$level"
  fi
done
}

# cool_down: at least COOLDOWN minutes, then until thermal.log's last minute shows the fans stopped
# and the GPU at COOL_C or below. Without macmon there's no heat log, only the minutes.
cool_down() {
echo "cooling down: at least $cooldown minutes, then until the fans stop and the GPU is at ${hot}C"
sleep $((cooldown * 60))
[ -n "$thermal" ] || return 0
waited=0
while heat=$(tail -n 1 "$logs/thermal.log" 2>/dev/null | awk -v hot="$hot" '$1 ~ /:/ && NF >= 8 && ($8 > 0 || $7 > hot) { printf "fan %d, %dC", $8, $7 }') && [ -n "$heat" ]; do
  [ $((waited % 15)) -ne 0 ] || echo "$(date '+%a %H:%M') waiting for the machine to cool: $heat"
  sleep 60
  waited=$((waited + 1))
done
}

# done_run: the run is complete, or stopped at a gate, by any identity's log.
done_run() {
for identity in $names; do
  grep -q "^run $(basename "$run_dir") \(is complete\|stopped\)" "$logs/$identity.log" 2>/dev/null && return 0
done
return 1
}

# Every launch starts at the full budget, so a pause left from the last one can't hold this one.
set_throttle "$budget"
[ -z "$thermal" ] || { thermostat & thermostat_pid=$!; }
round=0
while [ "$rounds" -eq 0 ] || [ "$round" -lt "$rounds" ]; do
  round=$((round + 1))
  if [ "$rounds" -eq 0 ]; then of="until the run ends"; else of="of $rounds"; fi
  echo "$(date '+%a %H:%M') round $round $of starting"
  started=$SECONDS
  launch 1
  elapsed=$((SECONDS - started))
  echo "$(date '+%a %H:%M') round $round $of done in $((elapsed / 3600))h$(printf %02d $((elapsed % 3600 / 60)))m"
  if done_run || { [ "$rounds" -ne 0 ] && [ "$round" -ge "$rounds" ]; }; then
    break
  fi
  cool_down
done
finish
[ -z "$thermostat_pid" ] || kill "$thermostat_pid" 2>/dev/null || true
[ -z "$thermal" ] || { pkill -P "$thermal"; kill "$thermal"; } 2>/dev/null || true
echo "all identities finished; logs in $logs"
