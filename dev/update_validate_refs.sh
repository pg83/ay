#!/usr/bin/env bash
# Regenerate the compressed canonical validate refs from the local upstream
# raw graphs used during development.

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

REF_DIR="/home/pg/monorepo/yatool"
YDB_ROOT="/home/pg/monorepo/ydb"
OUT_DIR="$SCRIPT_DIR/validate_refs"

mkdir -p "$OUT_DIR"

python3 "$SCRIPT_DIR/validate_ref.py" pack \
    --raw "$REF_DIR/sg2.json" \
    --target "devtools/ymake/bin" \
    --out "$OUT_DIR/sg2.aarch64.norm.json.xz"

python3 "$SCRIPT_DIR/validate_ref.py" pack \
    --raw "$REF_DIR/sg2_x86_64.json" \
    --target "devtools/ymake/bin" \
    --out "$OUT_DIR/sg2.x86_64.norm.json.xz"

python3 "$SCRIPT_DIR/validate_ref.py" pack \
    --raw "$REF_DIR/sg3.json" \
    --target "devtools/ya/bin" \
    --out "$OUT_DIR/sg3.aarch64.norm.json.xz"

python3 "$SCRIPT_DIR/validate_ref.py" pack \
    --raw "$YDB_ROOT/sg4.json" \
    --target "util/ut" \
    --out "$OUT_DIR/sg4.ydb.norm.json.xz"
