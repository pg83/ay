#!/usr/bin/env bash
# sg2: devtools/ymake/bin, aarch64 musl  (ref: yatool/sg2.json)
env -u CFLAGS -u CXXFLAGS \
    PYTHON='$(YMAKE_PYTHON3)/bin/python3' CC='$(CLANG)/bin/clang' \
    CXX='$(CLANG)/bin/clang++' OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
    ./ay make -j 0 -k -G \
    --target-platform default-linux-aarch64 --host-platform default-linux-x86_64 \
    --host-platform-flag MUSL=yes --musl \
    --source-root /home/pg/monorepo/yatool \
    devtools/ymake/bin > "${1}"
