# Build and validation

The repository vendors the canonical runner from `../build`. Do not edit the
local `build` copy independently; update the upstream runner, run its tests, and
copy the resulting executable here.

Common commands:

```sh
./build                 # build and publish ./ay -> .build/bin/ay
./build unit            # one Go test node plus one Python test node
./build validate        # all validation results, then the aggregate gate
./build test            # unit + complete validation gate
./build validation_resources
./build validate_catboost_app
```

Each entry in `dev/config.json` becomes an independent result node and a public
`validate_<case-id>` gate target. A result node consumes the `ay` binary plus
its immutable source-slice and reference-graph archives, executes the complete
generate/normalize/sort/compare pipeline in private scratch space, and stores a
small structured bundle below `$(B)/validation/cases/<case-id>`. Semantic
differences are data in `result.json`; the corresponding gate node turns that
status into a pass/fail stamp. `validation_summary` aggregates every result at
`$(B)/validation/summary.json` before the aggregate gate is evaluated.

Sandbox archives are ordinary CAS outputs and do not depend on the candidate
`ay` binary. New resources provisioned by `dev/provision.py` are uploaded as
locally created archives and pinned by SHA-256. Older config entries rely on
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
