#!/bin/sh
set -eu

usage() {
  echo "usage: $0 local|s3://bucket/waldo-e2e[/prefix] [direct|compose]" >&2
  exit 2
}

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || usage
transport=$1
mode=${2:-direct}
case "$mode" in direct|compose) ;; *) usage ;; esac

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
fixture="$work/generated-input"
validator="$work/validate-jsonl"
export WALDO_CONFIG="$work/config.json"

echo "building WALDO and the E2E validator"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$validator" ./scripts/e2e/validate_jsonl.go)

echo "generating UTF-8, multiline, and duplicate source content"
mkdir -p "$fixture"
printf 'Plain UTF-8: café, 東京, and 🚀.\nSecond line preserved exactly.\n' > "$fixture/01-plain.txt"
printf '# Markdown title\n\nA paragraph with "quotes", a backslash \\, and trailing punctuation!\n\n- one\n- two\n' > "$fixture/02-markdown.md"
cp "$fixture/01-plain.txt" "$fixture/03-duplicate.txt"

ingest_input=$fixture
if [ "$mode" = "compose" ]; then
  compose_root="$work/waldo-fetchers"
  mkdir -p "$compose_root"
  fetcher="$compose_root/fetch-fixture.sh"
  compose="$compose_root/tiny.yaml"
  printf '%s\n' '#!/bin/sh' 'set -eu' 'cp "$1"/* "$WALDO_FETCH_DIR"/' > "$fetcher"
  chmod 755 "$fetcher"
  printf '%s\n' \
    'kind: waldo-ingest-compose' \
    'schema: 1' \
    'title: Tiny-E2E-Corpus' \
    'description: Disposable-ingestion-smoke-test' \
    'license: CC0-1.0' \
    'source:' \
    '  name: tiny' \
    '  url: https://example.invalid/waldo-e2e' \
    '  category: public-dataset' \
    'steps:' \
    '  - name: fetch-fixture' \
    '    run: fetch-fixture.sh' \
    '    args:' \
    "      - $fixture" > "$compose"
  ingest_input=$compose
fi

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
if [ "$mode" = "compose" ]; then
  "$binary" index ingest "$ingest_input" "$destination" --dry-run
else
  # shellcheck disable=SC2086
  "$binary" index ingest "$ingest_input" "$destination" $common_arguments --dry-run
fi

echo "running ingestion"
if [ "$mode" = "compose" ]; then
  "$binary" index ingest "$ingest_input" "$destination"
else
  # shellcheck disable=SC2086
  "$binary" index ingest "$ingest_input" "$destination" $common_arguments
fi

if [ "$transport" = "local" ]; then
  published_count=$(find "$work/lookaside" -type f -print | wc -l | tr -d ' ')
  if [ "$published_count" -ne 1 ]; then
    echo "local lookaside contains $published_count files, want exactly one Parquet object" >&2
    exit 1
  fi
  published_object=$(find "$work/lookaside" -type f -print)
  if [ "$(dd if="$published_object" bs=1 count=4 2>/dev/null)" != "PAR1" ] || [ "$(tail -c 4 "$published_object")" != "PAR1" ]; then
    echo "local lookaside object is not a complete Parquet file" >&2
    exit 1
  fi
fi

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
"$validator" "$jsonl" "$destination/tiny.json" \
  https://example.invalid/waldo-e2e tiny CC0-1.0 "$fixture" \
  "$fixture/01-plain.txt" "$fixture/02-markdown.md"

if find "$staging" -path '*/objects/*' -type f -print | grep . >/dev/null 2>&1; then
  echo "successful ingestion left staged object files behind" >&2
  exit 1
fi
if find "$staging/composes" -mindepth 1 -print 2>/dev/null | grep . >/dev/null 2>&1; then
  echo "successful composed ingestion left prepared source files behind" >&2
  exit 1
fi
if find "$scratch" -type f -print 2>/dev/null | grep . >/dev/null 2>&1; then
  echo "successful verification/export left scratch object files behind" >&2
  exit 1
fi

echo "E2E ingest passed: generated, initialized, published, applied, verified, exported, compared, and purged"
echo "  index:      $index_root"
echo "  lookaside:  $lookaside_url"
echo "  records:    $line_count"
echo "  mode:       $mode"
