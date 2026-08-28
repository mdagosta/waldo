#!/bin/sh
# Copyright (c) 2026 OpenWALDO Project contributors
# SPDX-License-Identifier: Apache-2.0

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/.." && pwd)

echo "testing: documentation"

if rg -n \
    'waldo index update|--rebuild-shards|waldo index ingest .*recipe\.ya?ml|--replace' \
    "$repo_root/README.md" "$repo_root/AGENTS.md" "$repo_root/docs" \
    --glob '*.md' --glob '!adr/**'; then
    echo "obsolete command or flag found in maintained documentation" >&2
    exit 1
fi

for target in \
    docs/README.md \
    docs/INGESTION.md \
    docs/INGESTION-MANIFEST.md \
    docs/INGESTION-DESIGN.md \
    docs/MODEL-COMPOSE.md \
    docs/MODEL-LIFECYCLE.md \
    docs/MODEL-EXPORTS.md \
    docs/COMPATIBILITY.md \
    docs/TESTING.md
do
    test -f "$repo_root/$target" || {
        echo "required documentation is missing: $target" >&2
        exit 1
    }
done
