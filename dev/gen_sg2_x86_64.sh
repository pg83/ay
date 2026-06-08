#!/usr/bin/env bash
# sg2_x86_64: devtools/ymake/bin, x86_64 musl  (ref: yatool/sg2_x86_64.json)
env -u CFLAGS -u CXXFLAGS \
    ./ay make -j 0 -G \
    --target-platform default-linux-x86_64 --host-platform default-linux-x86_64 \
    --host-platform-flag MUSL=yes --musl \
    --source-root /home/pg/monorepo/yatool \
    devtools/ymake/bin > "${1}"
