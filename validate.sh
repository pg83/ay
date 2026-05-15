#!/usr/bin/env bash
# validate.sh — M2 byte-exact regression check.
#
# Reproduces the L4 byte-exact pin for `tools/archiver`. The toolchain
# overrides match the absolute paths the saved reference graph embeds
# in cmd_args (mineTools() resolves to different store paths today, so
# the overrides are mandatory). `--target-platform` / `--host-platform`
# pin the cross-compile axes the reference was generated under.
#
# Usage: ./validate.sh [/path/to/sg.archiver.json]
# Default output path: ./.out/sg.archiver.json

set -euo pipefail

OUT="${1:-./.out/sg.archiver.json}"
EXPECTED_M2="c3078d562bb875e5f795198663836610b202f6f44db7de06787440ac6d06c0d9"

mkdir -p "$(dirname "$OUT")"

go build -o yatool .

env -u CFLAGS -u CXXFLAGS ./yatool gen \
    --target tools/archiver \
    --out "$OUT" \
    --target-platform default-linux-aarch64 \
    --host-platform default-linux-x86_64 \
    --python-bin /ix/realm/pg/bin/python3 \
    --c-compiler /ix/realm/boot/bin/clang \
    --cxx-compiler /ix/realm/boot/bin/clang++ \
    --objcopy /ix/realm/boot/bin/llvm-objcopy \
    --host-platform-flag MUSL=yes

GOT=$(sha256sum "$OUT" | awk '{print $1}')

if [[ "$GOT" != "$EXPECTED_M2" ]]; then
    echo "M2 byte-exact FAIL"
    echo "  expected: $EXPECTED_M2"
    echo "  got:      $GOT"
    exit 1
fi

echo "M2 byte-exact OK ($GOT)"
