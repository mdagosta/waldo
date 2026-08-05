#!/bin/sh
set -eu

usage() {
  echo "usage: $0 local | s3://bucket/waldo-e2e[/prefix]" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
transport=$1

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-e2e.XXXXXX")

cleanup() {
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

case "$transport" in
  local)
    lookaside_url="file://$work/lookaside"
    ;;
  s3://*/waldo-e2e|s3://*/waldo-e2e/*)
    if [ "${WALDO_E2E_ALLOW_S3:-0}" != "1" ]; then
      echo "refusing S3 writes without WALDO_E2E_ALLOW_S3=1" >&2
      exit 2
    fi
    if [ "${WALDO_E2E_S3_PUBLIC:-0}" != "1" ]; then
      echo "a full S3 E2E run requires a publicly readable test prefix; set WALDO_E2E_S3_PUBLIC=1 to confirm" >&2
      exit 2
    fi
    lookaside_url=$transport
    ;;
  s3://*)
    echo "S3 E2E targets must use an explicit waldo-e2e prefix, not a bucket root" >&2
    exit 2
    ;;
  *) usage ;;
esac

binary="$work/waldo"
index_root="$work/waldo-index"
staging="$work/staging"
scratch="$work/scratch"
export_root="$work/export"
fixture="$repo_root/testdata/e2e/tiny"
export WALDO_CONFIG="$work/config.json"

echo "building WALDO"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)

echo "initializing empty index"
"$binary" index init "$index_root"

echo "configuring isolated machine state"
"$binary" config set lookaside "$lookaside_url"
"$binary" config set lookaside.workers 2
"$binary" config set lookaside.scratch "$scratch"
"$binary" config set ingest.staging "$staging"
if [ -n "${WALDO_E2E_AWS_REGION:-}" ]; then
  "$binary" config set lookaside.region "$WALDO_E2E_AWS_REGION"
fi

destination="$index_root/core/e2e/tiny"
common_arguments="--title Tiny-E2E-Corpus --description Disposable-ingestion-smoke-test --license CC0-1.0 --source https://example.invalid/waldo-e2e --source-category public-dataset"

echo "preflighting ingestion"
# The arguments are fixed test data rather than user input; intentional word
# splitting keeps this POSIX script dependency-free.
# shellcheck disable=SC2086
"$binary" index ingest "$fixture" "$destination" $common_arguments --dry-run

echo "running ingestion"
# shellcheck disable=SC2086
"$binary" index ingest "$fixture" "$destination" $common_arguments

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  if [ -n "$contribution" ]; then
    echo "found more than one contribution overlay" >&2
    exit 1
  fi
  contribution=$candidate
done
if [ -z "$contribution" ]; then
  echo "ingestion did not produce a contribution overlay" >&2
  exit 1
fi

echo "applying review overlay to disposable index"
cp -R "$contribution"/. "$index_root"/

echo "verifying new corpus recursively"
"$binary" index verify "$destination" --offline
"$binary" index verify "$destination"
"$binary" index verify "$destination" --objects

echo "exporting and verifying canonical JSONL"
"$binary" index export "$destination" "$export_root" --format jsonl
"$binary" bom verify "$export_root/EXPORT.json"

jsonl_count=$(find "$export_root/data" -type f -name '*.jsonl' -print | wc -l | tr -d ' ')
if [ "$jsonl_count" -ne 1 ]; then
  echo "JSONL export contains $jsonl_count shards, want 1" >&2
  exit 1
fi
jsonl=$(find "$export_root/data" -type f -name '*.jsonl' -print)
if [ -z "$jsonl" ] || [ ! -s "$jsonl" ]; then
  echo "JSONL export is missing or empty" >&2
  exit 1
fi
line_count=$(wc -l < "$jsonl" | tr -d ' ')
if [ "$line_count" -ne 2 ]; then
  echo "JSONL export contains $line_count records, want 2" >&2
  exit 1
fi

if find "$staging" -path '*/objects/*' -type f -print | grep . >/dev/null 2>&1; then
  echo "successful ingestion left staged object files behind" >&2
  exit 1
fi
if find "$scratch" -type f -print 2>/dev/null | grep . >/dev/null 2>&1; then
  echo "successful verification/export left scratch object files behind" >&2
  exit 1
fi

echo "E2E ingest passed: initialized, published, applied, verified, exported, and purged"
echo "  index:      $index_root"
echo "  lookaside:  $lookaside_url"
echo "  records:    $line_count"
