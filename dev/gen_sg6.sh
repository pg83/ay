#!/usr/bin/env bash
# sg6: full Arcadia devtools/ya/bin, native x86_64, sandboxing  (ref: /home/pg/3/sg6.json)
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing \
    --source-root /home/pg/3 \
    devtools/ya/bin > "${1}"
