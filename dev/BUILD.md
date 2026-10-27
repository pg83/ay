# Build and validation

The repository vendors the canonical runner from `../build`. Do not edit the
local `build` copy independently; update the upstream runner, run its tests, and
copy the resulting executable here.

Common commands:

```sh
./build                 # build and publish ./ay -> .build/bin/ay
./build unit            # Python unit tests plus the binary-driven graph tests
./build vet             # go vet with the generated dense maps overlaid
./build -Drace unit     # unit against a -race build
./build -Dcoverage coverage  # unit tests against a -cover build; see below
./build validate        # all validation results, then the aggregate gate
./build -k validate_quick # 16 small canonical graph cases for iteration
./build test            # unit + complete validation gate
./build validation_report
./build validation_resources
./build validate_catboost_app
```

The canonical upstream graph gate is the source of truth for behavior. Python
end-to-end tests are fast checks for iteration; their fixtures and assertions
must follow behavior established by the gate. A failing fast assertion alone
does not prove a regression. When the two disagree, inspect the canonical graph
and upstream mechanism, then correct the fast test. Do not change canonical
references or normalization to satisfy a fast test. Report graph-gate results
separately from fast-test failures, including when running the combined `test`
target.

Use the existing producer dependency scheduler for command construction.
Order initial commands by source priority and declaration order. Postpone a
command with an unavailable direct input to the queue tail and append commands
for automatic outputs there. Append archive and link members when their commands
are constructed; do not sort the members afterward. Explicit SRC, SRCS and
PY_SRCS calls inside a macro belong to that declaration; they are not automatic
processing of its outputs. Preserve that distinction when expanding macros.
Do not add a second simulation of command readiness, virtual queue rounds, or
reconstructed producer-chain depth to reorder objects after graph construction.

With `-Dcoverage`, every binary test packs its `GOCOVERDIR` counters into
`$(B)/coverage/<test>.tar`, and the `coverage` node merges them into the
textfmt profile `$(B)/coverage/cover.out`. It prints a per-file statement table,
stores it as `$(B)/coverage/report.txt`, and fails below the statement floor
`COVERAGE_MINIMUM` in `build.py`.

CI in `.github/workflows/ci.yml` runs gofmt, `vet`, `unit`, `-Drace unit` and
`-Dcoverage coverage` on Linux x86_64, Linux aarch64 and macOS. The aggregate
job adds the per-platform profiles up with `dev/coverage.py combine`, checks the
floor on the sum, and uploads it to Codecov. The validation cases need the
internal Sandbox and run only locally.

The required aggregate statement coverage is **100%: zero uncovered statements**
across the supported CI platforms, without excluding production files or blocks.
Cover reachable behavior through Python end-to-end tests that invoke the CLI;
use the existing chaos build for deterministic failures at system boundaries.
Do not add Go unit tests (`*_test.go`). Remove unreachable code only after
establishing why no supported input can reach it. Per-platform floors may be
lower because some behavior is specific to an operating system or architecture.

Each entry in `dev/config.json` becomes an independent result node and a public
`validate_<case-id>` gate target. A result node consumes the `ay` binary plus
its immutable source-slice and reference-graph archives, executes the complete
generate/normalize/sort/compare pipeline in private scratch space, and stores a
small structured bundle below `$(B)/validation/cases/<case-id>`. Semantic
differences are data in `result.json`; the corresponding gate node turns that
status into a pass/fail stamp. `validation_summary` aggregates every result at
`$(B)/validation/summary.json` before the aggregate gate is evaluated.
`validate`, `test`, and `validation_report` publish both the structured report
and its human-readable `$(B)/validation/summary.txt`. The report always names
every XFAIL case, its matched/our-only/reference-only counts, and the final
OK/XFAIL/FAIL totals; a successful gate therefore cannot hide accepted
differences behind a bare green stamp.

Each case also retains the complete differing normalized nodes in
`ours.only.jsonl` and `ref.only.jsonl`, alongside the bounded `diff-sample.txt`.
These files are streamed during the parity count, so diagnosing a mismatch
does not require generating or normalizing the same graphs again.

For routine fixes, run the fixed subset `validate_quick`:

```sh
./build -j24 -k validate_quick
```

It runs the same complete generate/normalize/sort/compare pipeline and strict
per-case gates as `validate`, using the same pinned archives. It selects 17
existing cases with 90,979 reference nodes in total (including overlap between
cases). It does not generate or truncate new reference graphs.

| Cases | Regression checks | Reference nodes |
|---|---|---:|
| `feature_cpp_generated_header` | Generated C++ headers and source inputs | 2,926 |
| `feature_resource_order` | Resource objects and archive/link order | 8,491 |
| `feature_cython_py3_inputs`, `feature_cython_py2_inputs` | Both Cython/Python variants | 7,851 |
| `p12_python_contexts` | Python module contexts, including FlatBuffers | 3,057 |
| `p12_proto` | Protobuf generators | 4,832 |
| `p12_go` | Go graph generation | 18,043 |
| `feature_mlaunch_go` | Real Go consumer of MLAUNCH_COMPONENT, Go tool SBOM and SDC metadata order | 5,160 |
| `p12_java` | Java graph generation | 14,569 |
| `feature_java_sources_debug`, `feature_java_sbom` | Debug source JARs and Java SBOM | 13 |
| `feature_ts_proto` | TypeScript proto variants and non-recursive package dependencies | 29 |
| `feature_antlr4_cpp` | ANTLR C++ runtime includes and generated listeners/visitors | 6 |
| `p12_typescript` | TypeScript graph generation | 8,444 |
| `p12_cuda` | CUDA generation and platform resources | 2,651 |
| `p12_docs` | Documentation graphs | 18 |
| `devtools_ya_bin` | Real Python program with native dependencies, aarch64/musl target | 14,889 |

This subset catches regressions while fixing the shared ay/ya make tests; it
does not cover every failing fixture or replace the full corpus. Run the
affected make test against both backends as well. Add the relevant existing
case when changing a mechanism outside this subset, for example
`validate_p12_tests`, `validate_p12_codegen_tail`, `validate_p12_config`, or
`validate_p12_packaging`. Run `validate` before concluding that the complete
corpus still has parity.

`feature_mlaunch_go` pins
`sdg/sdc/ros/motion_safety/ml_postprocessing/tests/one_tick/golaunch` with
`USE_PREBUILT_TOOLS=no` on host and target, as in `p12_go`. The shared make
tests separately exercise component selection through `ONLY_TAGS`,
`INCLUDE_TAGS` and `EXCLUDE_TAGS`. ROS component variants are not covered by
this case.

Small source-feature cases provide a quicker loop for Cython inputs, resource
ordering, and late source-file edges. Run all four with:

```sh
./build -j1 -k validate_source_features
```

Each case also has its own `validate_<case-id>` target. The reference closures
use real targets from the pinned Arcadia revision, with independent source
slices verified by running upstream `ya` on them:

| Case ID | Target | Reference nodes |
|---|---|---:|
| `feature_cython_py3_inputs` | `search/split_bert` | 645 |
| `feature_cpp_generated_header` | `extsearch/geo/saas/common/ut` | 2,926 |
| `feature_cython_py2_inputs` | `contrib/python/scipy/py2` | 7,206 |
| `feature_resource_order` | `irt/bmgen/synonyms` | 8,491 |

The generated-header case also reproduces the lost-input regression from
reusing a closure after its file edges have changed. All four cases use
`xfail: false`; differences fail their gates normally. These focused cases
supplement the larger corpus cases.

The `feature_rust_cryptography` case uses
`contrib/python/cryptography/next/tests`, a real Python test binary linking
cryptography, Temporal, and librsvg with a shared Rust runtime. Its complete
release graph includes Rust libraries, host crates, procedural macros, and
build-script programs: 11,336 normalized nodes, including 554 Rust commands.
Run it independently when changing Rust support:

```sh
./build validate_feature_rust_cryptography
```

It is included in the full corpus with `xfail: false`.

Rust support includes `CRATE` target and host variants, `RUST_BUILD_SCRIPT`,
`CRATE_PROC_MACRO`, `RUST_RUNTIME`, and the `CRATE_*` configuration macros.
The Rust emitter uses the existing peer closures, tool bindings, and lazy
module publication. Native archives, metadata `.rlib` files, and build-script
`.build.tar` files belong to one producer; C++ links native archives and one
shared runtime. The same graph-only tests in `tst/make/test_rust.py` run on
both ay and upstream ya. The e2e revision d02375f8 predates Rust modules, so
each test installs the upstream Rust support of 3b36097f
(`tst/make/upstream_rust.py`) into its private view of the tree:

```sh
./build -Darcadia=/home/pg/2 ay_e2e_collect_rust ya_e2e_rust
```

The ay collection target stores assertions in `.build/e2e/ay/test_rust.json`;
use `ay_e2e` to enforce the complete make-test suite's pass/fail status.

Two small Java cases use real targets from `devtools/dummy_arcadia`:

| Case ID | Target below `devtools/dummy_arcadia/` | Build type | Reference nodes |
|---|---|---|---:|
| `feature_java_sources_debug` | `hello_java/hello` | Debug | 9 |
| `feature_java_sbom` | `sbom/java/lib` | Release | 4 |

The first covers source JARs for a Java program and its library, plus SBOM
dependencies. Upstream enables source JARs by default in Debug, so a Release
graph does not cover this default. The second covers explicit `EMBED_SBOM`,
dependency component metadata, and toolchain metadata. Each source slice is
verified by rerunning upstream `ya` and comparing all graph nodes and result
roots with the original tree. Both cases use `xfail: false` and belong to
`validate` and `validate_quick`.

```sh
./build -j2 -k validate_feature_java_sources_debug validate_feature_java_sbom
```

Two more strict cases use `devtools/dummy_arcadia/typescript/proto/non_recursive/consumer`
and `devtools/dummy_arcadia/antlr4/cppexpr`. Their complete Release graphs contain
29 and 6 nodes respectively. Both source slices reproduce upstream's complete
nodes and result roots. They belong to both `validate` and `validate_quick`:

```sh
./build -j2 -k validate_feature_ts_proto validate_feature_antlr4_cpp
```

Sandbox archives are ordinary CAS outputs and do not depend on the candidate
`ay` binary. New resources provisioned by `dev/provision.py` are uploaded as
locally created `tar.zst` archives (`zstd -3 -T2`, streaming) and pinned by SHA-256.
Provisioning requires the `zstd` executable on `PATH`. Older config entries rely on
the immutability of their Sandbox resource IDs until they are reprovisioned.

Use separate materialized build roots with a common cache when multiple
workspaces run concurrently:

```sh
./build -B .out/ticket-build --cache-dir /shared/ay-build-cache test
```

`BUILD_CACHE_DIR` provides the same cache setting as the CLI flag. Overseer and
`acceptance` can therefore share resource archives and successful node results
without racing over `summary.json` symlinks in one build root.

Validation sharding is deterministic and balanced over the configured cases:

```sh
./build -Dgroup=0 -Dgroup_count=4 validation_shard
```

`dev/validate.py` remains a compatibility wrapper. It invokes result groups in
the build graph and renders the historical text lines from structured JSON.
`acceptance` reads JSON directly for build-integrated revisions and falls back
to the legacy wrapper for revisions that predate `build.py`.
