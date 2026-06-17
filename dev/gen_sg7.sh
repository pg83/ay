#!/usr/bin/env bash
# sg7: Arcadia yabs/server/daemons/bs_static, native x86_64, sandboxing  (ref: /home/pg/monorepo/4/sg7.json)
# --keep-going: sg7 is an in-progress xfail case; unresolved includes (sysincl /
# codegen gaps still being closed) warn instead of aborting so node-count and
# node-divergence convergence can proceed. Drop it once resolution is complete.
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing --keep-going \
    --source-root /home/pg/monorepo/4 \
    yabs/server/daemons/bs_static > "${1}"
