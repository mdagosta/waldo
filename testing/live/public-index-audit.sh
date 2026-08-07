#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

set -eu

[ "${WALDO_LIVE_ALLOW_PUBLIC_INDEX:-0}" = "1" ] || {
  echo "set WALDO_LIVE_ALLOW_PUBLIC_INDEX=1 to authorize network reads and local cache use" >&2
  exit 2
}
[ "$#" -le 1 ] || {
  echo "usage: $0 [small-index-corpus-path]" >&2
  exit 2
}

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
target=${1:-"$repo_root/../waldo-index/core/common-pile/foodista"}
[ -e "$target" ] || { echo "index target does not exist: $target" >&2; exit 2; }

temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-live-index.XXXXXX")
cleanup() {
  case "$work" in "$temporary_base"/waldo-live-index.*) rm -rf -- "$work" ;; esac
}
trap cleanup EXIT HUP INT TERM

(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$work/waldo" ./cmd/waldo)
"$work/waldo" index verify "$target"
"$work/waldo" index audit "$target"
echo "live public-index audit passed: $target"
