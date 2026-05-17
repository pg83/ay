# PLAN 1: Replace string-heavy scanner cache keys with numeric IDs

## Goal

Reduce CPU spent in Go map hashing/equality on scanner hot-path caches:

- `resolveInnerKey`
- `subgraphInnerKey`
- `sysinclSourceKey`
- `sysinclIncluderKey`

Current profile shows the dominant flat CPU is not parser logic, but map key work:

- `internal/runtime/maps.ctrlGroup.matchH2`: 21.21%
- `aeshashbody`: 12.82%
- `runtime.mapaccess2_faststr`: 25.99% cumulative
- `type:.eq.main.resolveInnerKey`: 4.16%
- `type:.eq.main.sysinclIncluderKey`: 3.75%

The scanner caches are doing too much hashing of strings and struct keys.

## Current Shape

`scanner.go` uses struct keys that include `VFS` and strings:

```go
type resolveInnerKey struct {
	includer VFS
	target   string
	kind     includeKind
	next     bool
}

type subgraphInnerKey struct {
	abs          VFS
	srcClassHash uint64
}

type sysinclSourceKey struct {
	sourceRel string
	target    string
}

type sysinclIncluderKey struct {
	includerRel string
	target      string
}
```

Even though `VFS` is already better than raw path strings, the map keys still trigger expensive hash/equality paths across millions of lookups.

## Proposed Model

Add a scanner-local interner for stable numeric IDs:

- `VFS -> uint32`
- include target string -> `uint32`
- source/includer rel string -> `uint32`

Then replace hot keys with compact numeric structs:

```go
type resolveInnerKey struct {
	includer uint32
	target   uint32
	flags    uint8
}

type subgraphInnerKey struct {
	abs          uint32
	srcClassID   uint32
}

type sysinclSourceKey struct {
	sourceClassID uint32
	target        uint32
}

type sysinclIncluderKey struct {
	includer uint32
	target   uint32
}
```

The first implementation can keep maps typed as structs. Packing into a single `uint64` is a later micro-optimization if profile still shows key equality/hashing.

## Implementation Steps

1. Add an `intern.go` or scanner-local type in `scanner.go`:

```go
type scannerInterner struct {
	vfsIDs    VFSMap[uint32]
	stringIDs map[string]uint32
	nextVFS   uint32
	nextStr   uint32
}
```

2. Store it on `IncludeScanner`.

3. Add methods:

- `internVFS(v VFS) uint32`
- `internString(s string) uint32`
- optionally `internTarget(target string) uint32`, just a named wrapper for readability.

4. Convert cache key construction sites:

- `resolveSearchPath`
- `subgraph`
- `sysinclSourceLookup`
- `sysinclIncluderLookup`

5. Keep existing cache value types unchanged.

6. Run `go test ./...`.

7. Run sg3 profile again:

```bash
env -u CFLAGS -u CXXFLAGS \
  YATOOL_CPUPROFILE=.out/perf/sg3.plan1.cpu.pprof \
  YATOOL_MEMPROFILE=.out/perf/sg3.plan1.heap.pprof \
  PYTHON='$(YMAKE_PYTHON3)/bin/python3' \
  CC='$(CLANG)/bin/clang' \
  CXX='$(CLANG)/bin/clang++' \
  OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
  ./yatool make -j 0 -k -G \
  --target-platform default-linux-aarch64 \
  --host-platform default-linux-x86_64 \
  --host-platform-flag MUSL=yes \
  --musl \
  devtools/ya/bin > /dev/null
```

## Correctness Checks

- `go test ./...`
- `./validate.sh` must keep `sg2.aarch64` and `sg2.x86_64` exact.
- `sg3` node/cmd diff must not change except for intentionally accepted graph fixes. This plan should be behavior-preserving.

## Expected Impact

CPU should drop in:

- `aeshashbody`
- `runtime.mapaccess2_faststr`
- `type:.eq.main.resolveInnerKey`
- `type:.eq.main.sysinclIncluderKey`
- `type:.hash.main.*Key`

This plan is the first one to implement because it attacks the top flat CPU without changing scanner semantics.

## Risks

- Incorrect ID reuse across scanners would be a correctness bug. Keep interner per `IncludeScanner`.
- `scanCtx` caches are per scanner/context already, so numeric IDs do not need to be globally stable.
- If ID maps grow too large, memory can shift from cache keys to interner maps. Profile after implementation.
