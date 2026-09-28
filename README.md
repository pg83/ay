# ay

[![CI](https://github.com/pg83/ay/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/pg83/ay/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/pg83/ay/branch/master/graph/badge.svg)](https://app.codecov.io/gh/pg83/ay/tree/master)
[![Go version](https://img.shields.io/github/go-mod/go-version/pg83/ay)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A Go reimplementation of the ya/ymake build graph generator, with a local
executor that starts running nodes while the graph is still being built.
The generated graphs are compared byte for byte against reference ymake
graphs on fixed source slices.

## Build and test

```sh
./build                      # build and publish ./ay
./build unit                 # Python unit tests and the binary-driven graph tests
./build vet                  # go vet over the sources and the generated dense maps
./build -Drace unit          # unit against a -race build
./build -Dcoverage coverage  # unit against a -cover build, per-file table and cover.out
./build test                 # unit plus the validation gate; needs the internal Sandbox
```

See [dev/BUILD.md](dev/BUILD.md) for the validation cases and the gate.
