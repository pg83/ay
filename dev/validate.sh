#!/usr/bin/env bash
# validate.sh — M3 normalized L4 regression check.
#
# Builds `yatool`, generates the `devtools/ymake/bin` graph for both
# supported target platforms, and checks normalized L4 equality against
# the checked-in upstream references:
#   - sg2.json          → default-linux-aarch64 target
#   - sg2_x86_64.json   → default-linux-x86_64 target
#
# The toolchain env overrides match the absolute paths embedded in the
# saved reference graphs. On mismatch, the script prints a ready-to-run
# `diff.py` command for the failing case.
#
# Usage: ./dev/validate.sh [out-dir]
# Default output dir: <repo-root>/.out/validate

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

OUT_DIR="${1:-./.out/validate}"
REF_DIR="/home/pg/monorepo/yatool_orig"

mkdir -p "$OUT_DIR"

go build -o yatool .

run_case() {
    local case_name="$1"
    local target_path="$2"
    local target_platform="$3"
    local ref_json="$4"
    local root_output="$5"

    local make_json="$OUT_DIR/${case_name}.make.json"
    local our_norm="$OUT_DIR/${case_name}.our.norm.json"
    local ref_norm="$OUT_DIR/${case_name}.ref.norm.json"

    echo "[$case_name] generating graph"

    env -u CFLAGS -u CXXFLAGS \
        PYTHON='$(YMAKE_PYTHON3)/bin/python3' \
        CC='$(CLANG)/bin/clang' \
        CXX='$(CLANG)/bin/clang++' \
        OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
        ./yatool make \
        -j 0 \
        -k \
        -G \
        --target-platform "$target_platform" \
        --host-platform default-linux-x86_64 \
        --host-platform-flag MUSL=yes \
        --musl \
        "$target_path" > "$make_json"

    echo "[$case_name] normalize + L4 compare"

    if ./dev/normalize.py \
        --our "$make_json" \
        --ref "$ref_json" \
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

if ! run_case "sg2.aarch64" "devtools/ymake/bin" "default-linux-aarch64" "$REF_DIR/sg2.json" "/devtools/ymake/bin/ymake"; then
    status=1
fi

if ! run_case "sg2.x86_64" "devtools/ymake/bin" "default-linux-x86_64" "$REF_DIR/sg2_x86_64.json" "/devtools/ymake/bin/ymake"; then
    status=1
fi

if ! run_case "sg3.aarch64" "devtools/ya/bin" "default-linux-aarch64" "$REF_DIR/sg3.json" "/devtools/ya/bin/ya-bin"; then
    status=1
fi

if [[ "$status" -eq 0 ]]; then
    echo "validate.sh: all M3 L4 checks passed"
else
    echo "validate.sh: one or more M3 L4 checks failed"
fi

exit "$status"
