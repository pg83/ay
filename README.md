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
./build -k validate_quick    # 12 small canonical graph cases for iteration
./build -j4 -Darcadia=/home/pg/1 ay_e2e
./build -Darcadia=/home/pg/1 ya_e2e
./build -j4 -Dcoverage -Darcadia=/home/pg/1 ay_e2e
./build -j4 -Dcoverage -Darcadia=/home/pg/1 ay_e2e_results
./build -j4 -Dcoverage -Darcadia=/home/pg/1 cli_coverage
```

The make e2e targets run the Python tests in `tst/make/`. Graph tests use
`make -j0`, usually with `-G`; explicitly marked ay-only tests also exercise
execution. The ay suite uses the test binary with chaos fault points;
the production binary compiles those points out. Both require `-Darcadia` pointing to an existing
Arcadia tree. `ay_e2e` runs test modules in parallel, with each test linking
the tree's top-level entries into its own temporary source root. `ya_e2e`
also runs modules in parallel: `ay dev overlay` gives each worker a private
mount namespace and writable OverlayFS layer over Arcadia at its original
path, preserving UID/GID. Tests create `__ay_e2e__` only in that layer; the
source tree stays unchanged. Linux must allow unprivileged user namespaces
and OverlayFS mounts. Each test gets its own `--build-dir`, including
configuration files and ymake caches, because the same source paths have
different contents across workers. Ya's optional whole-command lock is
disabled with `--no-locked`; downloaded tools stay shared and keep their
own locks. Both fixture roots
omit `.arc`, whose symlink refers to the live mount's shared database outside
the source tree. Ya also runs with `--no-prefetch`, so neither VCS status nor
prefetch opens that database from the overlay. Neither target
reuses cached test results, since Arcadia can change independently.

Tests can override `project_name` with a path inside a subtree that has specific
build policies. The ay fixture expands its parent directories privately before
creating the test project.

With `-Dcoverage`, `ay_e2e` uses an instrumented binary and merges each test
module's counters into `.build/coverage/make/cover.out` and `report.txt`.
This local report measures coverage of the whole binary by the make tests
alone and has no minimum threshold.
`ay_e2e_results` collects statuses and coverage even when assertions fail;
it is not a passing-test gate. The JSON statuses are in `.build/e2e/ay/`.
`ay_e2e` checks those statuses and fails on any red test.

`cli_coverage` combines the existing CLI tests and the make tests, writes
`.build/coverage/cli/cover.out` and `report.txt`, and requires at least 90%
statement coverage of the whole binary. It measures coverage independently
of make test failures; use `ay_e2e` for their pass/fail status. Every make
graph test must pass on upstream `ya`; confirmed ay failures stay red without
XFAIL or backend-specific assertions. Mark tests of the local executor or
ay-specific CLI commands with `@ay_only("reason")` from `tst.backends`.
The make runner skips these tests on `ya`, including their fixture setup,
and records the skip reasons in its JSON results.

See [dev/BUILD.md](dev/BUILD.md) for the validation cases and the gate.
