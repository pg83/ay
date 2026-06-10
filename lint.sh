#!/usr/bin/env sh

set -xue

go build -o ay .
./ay refac consts
./ay refac lint
gofmt -w .
go build -o ay .
