#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
transport=${1:-local}
[ "$#" -le 1 ] || {
  echo "usage: $0 [local|s3://bucket/waldo-e2e/prefix]" >&2
  exit 2
}

echo "testing: ingest-recipe lifecycle over $transport"
exec "$script_dir/_ingest_lifecycle.sh" "$transport" recipe
