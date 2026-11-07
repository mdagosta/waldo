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
# ROUNDS=<n> runs the next n rounds one at a time, cooling down COOLDOWN minutes (default 20)
# between them so the machine isn't hot all night:
#   ROUNDS=1 launcher.sh <run>                       # run the next round, then stop
#   ROUNDS=3 COOLDOWN=30 launcher.sh <run>           # three rounds, 30 minutes apart
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
throttle=$(awk -v budget="${WALDO_GPU_THROTTLE:-1}" -v n="$identities" 'BEGIN { t = budget / n; printf "%.4f", (t < 0.01 ? 0.01 : t) }')
rounds=${ROUNDS:-0}
cooldown=${COOLDOWN:-20}
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
if command -v macmon >/dev/null; then
  "$script_dir/thermal-log.sh" "$logs/thermal.log" > /dev/null &
  thermal=$!
else
  echo "macmon isn't installed, so heat isn't logged"
fi

# phase <log>: an identity's current phase from the end of its log, training in 10% steps.
phase() {
unit=$(grep '^== Round .*, unit ' "$1" 2>/dev/null | tail -1 | sed 's/.*, unit \([^:]*\):.*/\1/')
tail -c 4000 "$1" 2>/dev/null | tr '\r' '\n' | awk -v unit="$unit" '
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
  END { print line }'
}

# report: every REPORT minutes (default 5), prints each identity's phase when it changes.
# saves <log>: how many checkpoints an identity has written.
saves() {
cat "$1" 2>/dev/null | tr '\r' '\n' | grep -c 'checkpoint step .* persisted' || true
}

report() {
n=0
for identity in $names; do
  n=$((n + 1))
  now=$(phase "$logs/$identity.log")
  eval "phase_$n=\$now saves_$n=$(saves "$logs/$identity.log")"
done
while sleep $((${REPORT:-5} * 60)); do
  # thermal.log's minutes since the last report: mean GPU busy and chip watts, hottest GPU, fastest fan.
  heat=$(tail -n "${REPORT:-5}" "$logs/thermal.log" 2>/dev/null | awk '$1 ~ /:/ {
    gpu += $2; watts += $4; if ($6 > hot) hot = $6; if ($7 > fan) fan = $7; n++ }
    END { if (n) printf "gpu %3.0f%% %4.1fW %3.0fC fan %d", gpu / n, watts / n, hot, fan }')
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
      printf '%s %s: %-30s %s%s\n' "$(date '+%a %H:%M')" "$identity" "$now" "$heat" "$mark"
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

if [ "$rounds" -eq 0 ]; then
  launch 0
else
  for round in $(seq 1 "$rounds"); do
    echo "$(date '+%a %H:%M') round $round of $rounds starting"
    started=$SECONDS
    launch 1
    elapsed=$((SECONDS - started))
    echo "$(date '+%a %H:%M') round $round of $rounds done in $((elapsed / 3600))h$(printf %02d $((elapsed % 3600 / 60)))m"
    if [ "$round" -lt "$rounds" ]; then
      echo "cooling down $cooldown minutes"
      sleep $((cooldown * 60))
    fi
  done
fi
[ -z "$thermal" ] || { pkill -P "$thermal"; kill "$thermal"; } 2>/dev/null || true
echo "all identities finished; logs in $logs"
