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
# ROUNDS=<n> stops every identity once n rounds, from the first still open, are merged:
#   ROUNDS=1 run-identities.sh runs/<run>                  # run the next round, then stop
# FAULTS runs identities through burnin.py, which injects protocol faults as they join:
#   FAULTS=random run-identities.sh runs/<run>             # every identity, random faults
#   FAULTS="<identity>=close <identity>=lazy" ...           # these identities, these faults
# FAULT_RATE (default 0.15) tunes random faults; burnin.py report runs/<run> checks the outcome.
#
# Stop them all with Ctrl-C; each identity resumes where it left off when run again.

set -eu

run_dir=${1:?usage: run-identities.sh runs/<run>}
repo=${RUNS_REPO:?set RUNS_REPO to the runs repository}
prefix=${JOIN_IDENTITY_PREFIX:?set JOIN_IDENTITY_PREFIX, as in <account>-bot-}
identities=${IDENTITIES:-5}
throttle=${JOIN_THROTTLE:-0.2}
rounds=${ROUNDS:-0}
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
waldo=$(CDPATH='' cd -- "$script_dir/../.." && pwd)/waldo
logs=$HOME/.waldo/join/logs/$(basename "$run_dir")

mkdir -p "$logs"
trap 'kill 0' INT TERM
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
  else
    WALDO_GPU_THROTTLE=$throttle uv run -q --with mlx --with huggingface_hub \
      "$waldo" model join "$repo" "$run_dir" --identity "$identity" --email "$email" --rounds "$rounds" --wait 30 \
      >> "$logs/$identity.log" 2>&1 &
  fi
  sleep 5
done
wait
echo "all identities finished; logs in $logs"
