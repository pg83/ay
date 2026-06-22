#!/usr/bin/env bash
# sg5: ydb ydb/apps/ydbd, native x86_64, sandboxing OS_SDK=local  (ref: ydb/sg5.json)
# --keep-going: a missing own ADDINCL dir is fatal in strict mode; the gate
# reproduces upstream graphs generated in warn-mode, so warn-and-continue here.
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing --keep-going \
    --source-root /home/pg/monorepo/ydb \
    -DOS_SDK=local --host-platform-flag OS_SDK=local \
    ydb/apps/ydbd > "${1}"
