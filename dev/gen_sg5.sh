#!/usr/bin/env bash
# sg5: ydb ydb/apps/ydbd, native x86_64, sandboxing OS_SDK=local  (ref: ydb/sg5.json)
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing \
    --source-root /home/pg/monorepo/ydb \
    -DOS_SDK=local --host-platform-flag OS_SDK=local \
    ydb/apps/ydbd > "${1}"
