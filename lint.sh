#!/usr/bin/env sh

set -xue

./build ay
.build/bin/ay dev refac consts
.build/bin/ay dev refac lint
gofmt -w .
./build ay
