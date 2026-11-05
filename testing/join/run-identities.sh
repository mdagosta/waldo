#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# Burn-in launcher for `waldo model join`: runs identities <prefix>1..N on one run at the
# same time, each throttled, each logging to ~/.waldo/join/logs/<run>/<identity>.log.
#
#   RUNS_REPO=https://github.com/<owner>/<runs> JOIN_IDENTITY_PREFIX=<account>-bot- \
#     run-identities.sh runs/<run>
#
# IDENTITIES (default 5) and JOIN_THROTTLE (default 0.2, each identity's WALDO_GPU_THROTTLE
# regardless of any global setting) size the run; JOIN_EMAIL is the record commits' author.
# ROUNDS=<n> runs the next n rounds one at a time, cooling down COOLDOWN minutes (default 20)
# between them so the machine isn't hot all night:
#   ROUNDS=1 run-identities.sh runs/<run>                  # run the next round, then stop
#   ROUNDS=3 COOLDOWN=30 run-identities.sh runs/<run>      # three rounds, 30 minutes apart
# Keep a laptop's lid open while it trains: closed, it runs hotter.
# REPORT=<minutes> sets how often the launcher prints progress (default 5).
# FAULTS runs identities through burnin.py, which injects protocol faults as they join:
#   FAULTS=random run-identities.sh runs/<run>             # every identity, random faults
#   FAULTS="<identity>=close <identity>=lazy" ...           # these identities, these faults
# FAULT_RATE (default 0.15) tunes random faults; burnin.py report runs/<run> checks the outcome.
#
# Stop them all with Ctrl-C; each identity resumes where it left off when run again.
# WALDO is the binary with `waldo model join` (default: the md/join-local worktree's, beside this one).

set -eu

run_dir=${1:?usage: run-identities.sh runs/<run>}
repo=${RUNS_REPO:?set RUNS_REPO to the runs repository}
prefix=${JOIN_IDENTITY_PREFIX:?set JOIN_IDENTITY_PREFIX, as in <account>-bot-}
identities=${IDENTITIES:-5}
throttle=${JOIN_THROTTLE:-0.2}
rounds=${ROUNDS:-0}
cooldown=${COOLDOWN:-20}
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
waldo=${WALDO:-$(CDPATH='' cd -- "$script_dir/../../.." && pwd)/waldo-join-local/waldo}
[ -x "$waldo" ] || { echo "no waldo binary at $waldo; set WALDO" >&2; exit 1; }
logs=$HOME/.waldo/join/logs/$(basename "$run_dir")

mkdir -p "$logs"
trap 'kill 0' INT TERM
echo "keep the lid open while training"

# phase <log>: an identity's current phase from the end of its log, training in 10% steps.
phase() {
tail -c 4000 "$1" 2>/dev/null | tr '\r' '\n' | awk '
  match($0, /step [0-9]+\/[0-9]+,/) {
    split(substr($0, RSTART + 5, RLENGTH - 6), s, "/")
    stage = $1; sub(/.*\//, "", stage)
    line = stage " " int(s[1] * 10 / s[2]) * 10 "%"; next }
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
report() {
for n in $(seq 1 "$identities"); do
  now=$(phase "$logs/$prefix$n.log")
  eval "phase_$n=\$now"
done
while sleep $((${REPORT:-5} * 60)); do
  for n in $(seq 1 "$identities"); do
    now=$(phase "$logs/$prefix$n.log")
    eval "was=\${phase_$n:-}"
    if [ -n "$now" ] && [ "$now" != "$was" ]; then
      echo "$(date '+%a %H:%M') $prefix$n: $now"
      eval "phase_$n=\$now"
    fi
  done
done
}

# launch <rounds>: starts every identity for that many rounds (0: until the run ends) and waits.
launch() {
pids=""
for n in $(seq 1 "$identities"); do
  identity=$prefix$n
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
echo "all identities finished; logs in $logs"
