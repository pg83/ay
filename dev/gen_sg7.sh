#!/usr/bin/env bash
# sg7: Arcadia yabs/server/daemons/bs_static, native x86_64, sandboxing  (ref: /home/pg/monorepo/4/sg7.json)
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing \
    --source-root /home/pg/monorepo/4 \
    yabs/server/daemons/bs_static > "${1}"
