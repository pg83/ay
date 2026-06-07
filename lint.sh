#!/usr/bin/env sh

go build .
./ay refac consts
./ay refac lint
gofmt .
