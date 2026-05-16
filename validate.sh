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
EXPECTED_M2="769d40e0e5dc79ce04fb1c34bffe83b9170ce05c3eac22ac291cbef1718e0571"

mkdir -p "$(dirname "$OUT")"

go build -o yatool .

env -u CFLAGS -u CXXFLAGS \
    PYTHON=/ix/realm/pg/bin/python3 \
    CC=/ix/realm/boot/bin/clang \
    CXX=/ix/realm/boot/bin/clang++ \
    OBJCOPY=/ix/realm/boot/bin/llvm-objcopy \
    ./yatool make \
    -j 0 \
    -k \
    -G \
    --target-platform default-linux-aarch64 \
    --host-platform default-linux-x86_64 \
    --host-platform-flag MUSL=yes \
    --musl \
    tools/archiver > "$OUT"

GOT=$(sha256sum "$OUT" | awk '{print $1}')

if [[ "$GOT" != "$EXPECTED_M2" ]]; then
    echo "M2 byte-exact FAIL"
    echo "  expected: $EXPECTED_M2"
    echo "  got:      $GOT"
    exit 1
fi

echo "M2 byte-exact OK ($GOT)"
