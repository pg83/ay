#!/usr/bin/env bash
# sg5: ydb ydb/apps/ydbd, native x86_64, sandboxing OS_SDK=local  (ref: ydb/sg5.json)
env -u CFLAGS -u CXXFLAGS \
    PYTHON='$(YMAKE_PYTHON3)/bin/python3' CC='$(CLANG)/bin/clang' \
    CXX='$(CLANG)/bin/clang++' OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
    ./ay make -j 0 -k -G --sandboxing \
    --source-root /home/pg/monorepo/ydb \
    -DOS_SDK=local --host-platform-flag OS_SDK=local \
    ydb/apps/ydbd > "${1}"
