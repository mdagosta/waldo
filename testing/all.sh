#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

"$script_dir/unit.sh"
"$script_dir/vet.sh"
"$script_dir/e2e/ingest-direct.sh"
"$script_dir/e2e/ingest-recipe.sh"
"$script_dir/e2e/model-fake.sh"
"$script_dir/e2e/model-mlx.sh"
"$script_dir/e2e/model-pytorch.sh"
"$script_dir/e2e/model-torchtitan.sh"
