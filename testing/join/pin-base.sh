#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

# Prints the join plan `base:` block that pins a published merge by every file's SHA-256:
#   pin-base.sh ~/.waldo/join/local/store/<run>/<identity>/<merge>
set -eu
dir=$(CDPATH='' cd -- "${1:?usage: pin-base.sh <published merge directory>}" && pwd)
[ -f "$dir/model.safetensors" ] || { echo "no model.safetensors in $dir" >&2; exit 1; }
hash() { shasum -a 256 "$1" | cut -d' ' -f1; }
echo "  base:"
echo "    model: $(basename "$dir")"
echo "    url: file://$dir"
echo "    model_sha256: $(hash "$dir/model.safetensors")"
echo "    files:"
for file in "$dir"/*; do
  [ -f "$file" ] && echo "      $(basename "$file"): $(hash "$file")"
done
