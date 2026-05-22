#!/usr/bin/env bash
# sg4: ydb util/ut, native x86_64, -ttt sandboxing OS_SDK=local  (ref: ydb/sg4.json)
env -u CFLAGS -u CXXFLAGS \
    PYTHON='$(YMAKE_PYTHON3)/bin/python3' CC='$(CLANG)/bin/clang' \
    CXX='$(CLANG)/bin/clang++' OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
    ./ay make -j 0 -k -G -ttt --sandboxing \
    --source-root /home/pg/monorepo/ydb \
    -DOS_SDK=local --host-platform-flag OS_SDK=local \
    util/ut > "${1}"
