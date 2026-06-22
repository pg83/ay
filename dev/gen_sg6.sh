#!/usr/bin/env bash
# sg6: full Arcadia devtools/ya/bin, native x86_64, sandboxing  (ref: /home/pg/monorepo/3/sg6.json)
# --keep-going: a missing own ADDINCL dir is fatal in strict mode; the gate
# reproduces upstream graphs generated in warn-mode, so warn-and-continue here.
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing --keep-going \
    --source-root /home/pg/monorepo/3 \
    devtools/ya/bin > "${1}"
