#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# Copyright (c) 2026 CtrlIQ, Inc.
# Copyright (c) 2026 Gregory M. Kurtzer
# SPDX-License-Identifier: Apache-2.0

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
temporary_base=${TMPDIR:-/tmp}
work=$(mktemp -d "$temporary_base/waldo-conversation-e2e.XXXXXX")

cleanup() {
  if [ "${WALDO_E2E_KEEP:-0}" = "1" ]; then
    echo "preserved structured-conversation E2E workspace: $work"
    return
  fi
  case "$work" in
    "$temporary_base"/waldo-conversation-e2e.*) rm -rf -- "$work" ;;
    *) echo "refusing to remove unexpected workspace: $work" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

binary="$work/waldo"
index_root="$work/waldo-index"
lookaside="$work/lookaside"
cache="$work/cache"
scratch="$work/scratch"
staging="$work/staging"
models="$work/models"
recipe_root="$work/fetchers"
fixture="$work/hermes-shaped.jsonl"
recipe="$recipe_root/structured-conversation.yaml"
fetcher="$recipe_root/copy-fixture.sh"
export_root="$work/export"
compose="$work/model.yaml"
export WALDO_CONFIG="$work/config.json"

echo "testing: structured conversation ingestion and training lifecycle"
(cd "$repo_root" && GOCACHE="$work/go-cache" go build -o "$binary" ./cmd/waldo)
mkdir -p "$recipe_root"

cat > "$fixture" <<'EOF'
{"id":"one","tools":"[{\"name\":\"lookup\",\"description\":\"Look up a value\"}]","conversations":[{"from":"human","value":"Find account 42."},{"from":"gpt","value":"I will use the lookup tool."},{"from":"tool","value":"Account 42 is active."},{"from":"gpt","value":"The account is active."}]}
{"id":"two","conversations":[{"from":"human","value":"User: this prefix belongs to the user content."},{"from":"gpt","value":"The prefix was preserved as content."}]}
{"id":"three","conversations":[{"from":"human","value":"Demonstrate embedded role-looking text."},{"from":"gpt","value":"This remains one assistant message.\n\nUser: not a boundary."}]}
{"id":"four","conversations":[{"from":"human","value":"Give a concise test response with enough bytes for capacity validation."},{"from":"gpt","value":"This response supplies deterministic assistant targets for the smoke training run."}]}
EOF

cat > "$fetcher" <<'EOF'
#!/bin/sh
set -eu
[ "$#" -eq 1 ] || { echo "usage: copy-fixture SOURCE" >&2; exit 2; }
cp "$1" conversations.jsonl
EOF
chmod 755 "$fetcher"

cat > "$recipe" <<EOF
kind: waldo-ingest-recipe
schema: 1
title: Structured Conversation E2E
description: Disposable Hermes-shaped structured-conversation fixture.
license: Apache-2.0
source:
  name: structured-conversation-e2e
  version: fixture-1
  url: https://example.invalid/structured-conversation-e2e
  category: public-dataset
input:
  type: chat-messages
  fields:
    id: id
  messages:
    role: conversations[].from
    content: conversations[].value
    tools: tools
    role_aliases:
      human: user
      gpt: assistant
steps:
  - name: copy-fixture
    exec: ./copy-fixture.sh
    args:
      - $fixture
EOF

"$binary" index init "$index_root"
"$binary" config set index "$index_root"
"$binary" config set lookaside "file://$lookaside"
"$binary" config set lookaside.cache "$cache"
"$binary" config set lookaside.cache.max-size 64MiB
"$binary" config set lookaside.scratch "$scratch"
"$binary" config set ingest.staging "$staging"
"$binary" config set model.root "$models"
"$binary" config set model.backend fake

destination="$index_root/post-train/sft/structured-conversation-e2e"
"$binary" index ingest "$recipe" "$destination" --dry-run
"$binary" index ingest "$recipe" "$destination"

contribution=""
for candidate in "$staging"/*/contribution; do
  [ -d "$candidate" ] || continue
  [ -z "$contribution" ] || { echo "multiple contribution overlays found" >&2; exit 1; }
  contribution=$candidate
done
[ -n "$contribution" ] || { echo "contribution overlay not found" >&2; exit 1; }
cp -R "$contribution"/. "$index_root"/

manifest="$destination/structured-conversation-e2e.yaml"
grep -Eq '^record_kind:[[:space:]]+conversation$' "$manifest"
grep -Eq '^record_schema:[[:space:]]+1$' "$manifest"
grep -Eq 'recipe:[[:space:]]+parquet-go/0\.30\.1/zstd-6/page-1m/rg-64m/conversation-v1$' "$manifest"

"$binary" index verify post-train/sft/structured-conversation-e2e --objects
"$binary" index audit post-train/sft/structured-conversation-e2e --deep --show-boms
"$binary" index export post-train/sft/structured-conversation-e2e "$export_root" --format jsonl

jsonl=$(find "$export_root/data" -type f -name '*.jsonl' -print -quit)
[ -n "$jsonl" ] || { echo "conversation JSONL export not found" >&2; exit 1; }
(cd "$repo_root" && GOCACHE="$work/go-cache" go run ./testing/e2e/validate_conversation "$jsonl")

cat > "$compose" <<'EOF'
kind: waldo-model-compose
schema: 1
interaction:
  template: user-assistant-v1
architecture:
  family: decoder-transformer
  context_tokens: 64
  vocabulary_size: 256
  hidden_size: 64
  intermediate_size: 192
  layers: 2
  attention_heads: 4
  key_value_heads: 2
  tie_embeddings: true
  parameter_dtype: float32
  tokenizer:
    name: byte
    revision: sha256:structured-conversation-e2e
stages:
  - name: structured-sft
    type: fine-tuning
    objective: assistant-response-modeling
    conversation:
      template: user-assistant-v1
      supervised_roles: [assistant]
    corpora:
      - post-train/sft/structured-conversation-e2e
    parameters:
      steps: 1
      batch_size: 1
      sequence_length: 64
      learning_rate: 0.001
      seed: 7
      evaluation_max_records: 1
      evaluation_max_bytes: 1048576
EOF

"$binary" model train structured-conversation-smoke "$compose" --audit
"$binary" model summary structured-conversation-smoke

run_bom=$(find "$models/structured-conversation-smoke/runs" -type f -name RUN-BOM.json -print -quit)
[ -n "$run_bom" ] || { echo "structured conversation run BOM not found" >&2; exit 1; }
grep -Eq '"template"[[:space:]]*:[[:space:]]*"user-assistant-v1"' "$run_bom"
grep -Eq '"supervised_roles"[[:space:]]*:' "$run_bom"
grep -Eq '"assistant"' "$run_bom"

echo "E2E structured conversation passed: fetched, ingested, audited, exported, structurally validated, transformed, masked, and trained"
