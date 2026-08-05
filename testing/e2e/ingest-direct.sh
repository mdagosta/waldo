#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
transport=${1:-local}
[ "$#" -le 1 ] || {
  echo "usage: $0 [local|s3://bucket/waldo-e2e/prefix]" >&2
  exit 2
}

echo "testing: direct ingestion lifecycle over $transport"
exec "$script_dir/_ingest_lifecycle.sh" "$transport" direct
