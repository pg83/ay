#!/usr/bin/env bash
# sg7: Arcadia yabs/server/daemons/bs_static, native x86_64, sandboxing  (ref: /home/pg/monorepo/4/sg7.json)
# --keep-going: /home/pg/monorepo/4 is a partial slice of the full monorepo; some
# modules' own ADDINCL dirs (e.g. contrib/libs/jansson/src) were dropped in the
# slice. ymake/ay report a missing own ADDINCL as fatal in strict mode and as a
# warning under keep-going; the slice needs the latter to still emit the graph.
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing --keep-going \
    --source-root /home/pg/monorepo/4 \
    yabs/server/daemons/bs_static > "${1}"
