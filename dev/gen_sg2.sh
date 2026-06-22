#!/usr/bin/env bash
# sg2: devtools/ymake/bin, aarch64 musl  (ref: yatool/sg2.json)
# --keep-going: a missing own ADDINCL dir is fatal in strict mode; the gate
# reproduces upstream graphs generated in warn-mode, so warn-and-continue here.
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G --sandboxing --keep-going \
    --target-platform default-linux-aarch64 --host-platform default-linux-x86_64 \
    --host-platform-flag MUSL=yes --musl \
    --source-root /home/pg/monorepo/yatool \
    devtools/ymake/bin > "${1}"
