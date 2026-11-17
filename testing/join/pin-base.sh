#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# Prints the join plan `base:` block that pins a published merge by every file's SHA-256:
#   pin-base.sh ~/.waldo/join/local/store/<run>/<identity>/<merge> [run.yaml to pin in place]
set -eu
dir=$(CDPATH='' cd -- "${1:?usage: pin-base.sh <published merge directory>}" && pwd)
[ -f "$dir/model.safetensors" ] || { echo "no model.safetensors in $dir" >&2; exit 1; }
hash() { shasum -a 256 "$1" | cut -d' ' -f1; }
pin() {
  echo "  base:"
  echo "    model: $(basename "$dir")"
  echo "    url: file://$dir"
  echo "    model_sha256: $(hash "$dir/model.safetensors")"
  echo "    files:"
  for file in "$dir"/*; do
    [ -f "$file" ] && echo "      $(basename "$file"): $(hash "$file")"
  done
}
if [ $# -lt 2 ]; then pin; exit; fi
# Replaces the plan's base: block, its indented lines included, with this pin.
pin > "$2.pin"
awk -v pin="$2.pin" '/^  base:/ { while ((getline line < pin) > 0) print line; skip = 1; next } skip && /^    / { next } { skip = 0; print }' "$2" > "$2.new"
rm "$2.pin" && mv "$2.new" "$2" && echo "pinned $(basename "$dir") in $2"
