#!/usr/bin/env sh

set -xue

go build -o ay .
./ay dev refac consts
./ay dev refac lint
gofmt -w .
go build -o ay .
