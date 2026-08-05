#!/bin/sh
set -eu

[ "${WALDO_LIVE_ALLOW_S3:-0}" = "1" ] || {
  echo "set WALDO_LIVE_ALLOW_S3=1 to authorize writes to a disposable S3 prefix" >&2
  exit 2
}
[ "$#" -eq 1 ] || {
  echo "usage: $0 s3://bucket/waldo-e2e[/prefix]" >&2
  exit 2
}

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
export WALDO_E2E_ALLOW_S3=1
export WALDO_E2E_S3_PUBLIC=1
exec "$script_dir/../e2e/ingest-recipe.sh" "$1"
