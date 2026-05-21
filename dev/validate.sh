#!/usr/bin/env bash
# validate.sh — M3 canonical byte-exact regression check.
#
# Builds `ay`, generates the target graph, canonicalizes the target
# closure, and compares those bytes against the checked-in compressed
# canonical refs:
#   - sg2.aarch64.norm.json.xz
#   - sg2.x86_64.norm.json.xz
#   - sg3.aarch64.norm.json.xz
#   - sg4.ydb.norm.json.xz
#
# Raw upstream sg*.json carry non-semantic ordering, UID, and metadata
# differences. The packed refs are generated from those raw graphs via
# `dev/update_validate_refs.sh` and are stable for direct byte-wise cmp.
#
# Usage: ./dev/validate.sh [out-dir]
# Default output dir: <repo-root>/.out/validate

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

OUT_DIR="${1:-./.out/validate}"
YDB_ROOT="/home/pg/monorepo/ydb"
CANON_REF_DIR="$SCRIPT_DIR/validate_refs"

mkdir -p "$OUT_DIR"

go build -o ay .

run_case() {
    local case_name="$1"
    local target_path="$2"
    local target_platform="$3"
    local ref_xz="$4"
    local root_output="$5"

    local raw_json="$OUT_DIR/${case_name}.make.json"
    local our_norm="$OUT_DIR/${case_name}.our.norm.json"
    local ref_norm="$OUT_DIR/${case_name}.ref.norm.json"

    echo "[$case_name] generating graph"

    env -u CFLAGS -u CXXFLAGS \
        PYTHON='$(YMAKE_PYTHON3)/bin/python3' \
        CC='$(CLANG)/bin/clang' \
        CXX='$(CLANG)/bin/clang++' \
        OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
        ./ay make \
        -j 0 \
        -k \
        -G \
        --target-platform "$target_platform" \
        --host-platform default-linux-x86_64 \
        --host-platform-flag MUSL=yes \
        --musl \
        "$target_path" > "$raw_json"

    echo "[$case_name] canonicalize + byte compare"

    if ./dev/validate_ref.py compare \
        --our "$raw_json" \
        --ref "$ref_xz" \
        --target "$target_path" \
        --our-out "$our_norm" \
        --ref-out "$ref_norm"; then
        echo "[$case_name] OK"
        return 0
    fi

    echo "[$case_name] FAIL"
    if [[ "$case_name" == "sg3.aarch64" ]]; then
        echo "  self_uid mismatch breakdown:"
        ./dev/mismatch.py --our "$our_norm" --ref "$ref_norm" || true
    fi
    echo "  inspect with:"
    echo "  ./dev/diff.py --our $our_norm --ref $ref_norm --root-output $root_output --show-cmd-diff"
    return 1
}

run_ydb_sg4() {
    local case_name="sg4.ydb"
    local target_path="util/ut"
    local ref_xz="$CANON_REF_DIR/sg4.ydb.norm.json.xz"
    local root_output="/util/ut/util-ut"

    local raw_json="$OUT_DIR/${case_name}.make.json"
    local our_norm="$OUT_DIR/${case_name}.our.norm.json"
    local ref_norm="$OUT_DIR/${case_name}.ref.norm.json"

    echo "[$case_name] generating graph (native x86_64, non-musl, -ttt --sandboxing, OS_SDK=local)"

    env -u CFLAGS -u CXXFLAGS \
        PYTHON='$(YMAKE_PYTHON3)/bin/python3' \
        CC='$(CLANG)/bin/clang' \
        CXX='$(CLANG)/bin/clang++' \
        OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
        ./ay make \
        -j 0 \
        -k \
        -G \
        -ttt \
        --sandboxing \
        --source-root "$YDB_ROOT" \
        -DOS_SDK=local \
        --host-platform-flag OS_SDK=local \
        "$target_path" > "$raw_json"

    echo "[$case_name] canonicalize + byte compare"

    if ./dev/validate_ref.py compare \
        --our "$raw_json" \
        --ref "$ref_xz" \
        --target "$target_path" \
        --our-out "$our_norm" \
        --ref-out "$ref_norm"; then
        echo "[$case_name] OK"
        return 0
    fi

    echo "[$case_name] FAIL"
    echo "  inspect with:"
    echo "  ./dev/diff.py --our $our_norm --ref $ref_norm --root-output $root_output --show-cmd-diff"
    return 1
}

status=0

if ! run_case "sg2.aarch64" "devtools/ymake/bin" "default-linux-aarch64" "$CANON_REF_DIR/sg2.aarch64.norm.json.xz" "/devtools/ymake/bin/ymake"; then
    status=1
fi

if ! run_case "sg2.x86_64" "devtools/ymake/bin" "default-linux-x86_64" "$CANON_REF_DIR/sg2.x86_64.norm.json.xz" "/devtools/ymake/bin/ymake"; then
    status=1
fi

if ! run_case "sg3.aarch64" "devtools/ya/bin" "default-linux-aarch64" "$CANON_REF_DIR/sg3.aarch64.norm.json.xz" "/devtools/ya/bin/ya-bin"; then
    status=1
fi

if ! run_ydb_sg4; then
    status=1
fi

if [[ "$status" -eq 0 ]]; then
    echo "validate.sh: all M3 L4 checks passed"
else
    echo "validate.sh: one or more M3 L4 checks failed"
fi

exit "$status"
