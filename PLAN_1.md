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

## Implemented Design

The implementation took the narrower scanner-local route, not a repo-wide
`VFS` representation change.

Chosen shape:

- one scanner-local `string -> uint32` pool
- VFS encoded as `(interned rel-string-id) | root-bit`
- one scanner-local `sourceClassSignature -> uint32` pool

Why this shape:

- changing global `VFS` to `(root, string-id)` is a wider migration than
  PLAN 1 and would touch serializer/emitter/codegen boundaries
- interning plain strings without replacing the hot cache keys would not
  help enough, because Go would still hash string bytes inside those keys
- scanner-local IDs preserve correctness boundaries: cache identity stays
  local to one `IncludeScanner`
- VFS does not need its own second lookup table: SOURCE vs BUILD fits in
  one reserved high bit over the interned rel-string ID

Resulting hot keys:

```go
type resolveInnerKey struct {
	includer uint32
	target   uint32
	flags    uint8
}

type subgraphInnerKey struct {
	abs         uint32
	sourceClass uint32
}

type sysinclSourceKey struct {
	sourceClass uint32
	target      uint32
}

type sysinclIncluderKey struct {
	includer uint32
	target   uint32
}
```

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

## Measured Result

Correctness:

- `go test ./...` = `ok`
- `validate.sh`
- `sg2.aarch64` = byte-exact
- `sg2.x86_64` = byte-exact
- `sg3.aarch64` keeps the same non-zero state as before:
- ours `e7dfcb960dcfec5ae8ca1a4388897bc00f56535a2705fd90bc797b951815bc87`
- ref  `ab7bbc788651077896a68e2c5fbdb647ed72af23791bb4744f4c635bebc342cc`

Perf on `devtools/ya/bin` (`make -j 0 -k -G`, same profile setup as baseline):

- wall time: `8.16s -> 8.03s`
- user CPU: `10.71s -> 10.47s`
- max RSS: `1457860 kB -> 1209140 kB`
- `go test ./...`: `4.313s -> 4.179s`

CPU profile deltas:

- `internal/runtime/maps.ctrlGroup.matchH2`: `21.21% -> 10.09%`
- `aeshashbody`: `12.82% -> 7.08%`
- `type:.eq.main.resolveInnerKey`: `4.16% -> 0.85%`
- `type:.eq.main.sysinclIncluderKey`: dropped out of the top report
- new interner overhead exists but is smaller than the saved key-hash cost:
- `main.(*scannerInterner).internString`: `0.69s` cumulative
- `main.(*scannerInterner).internVFS`: `0.50s` cumulative

Memory profile deltas:

- in-use heap after GC: about `300MB -> 187.49MB`
- previous `sysinclSourceLookup` retained about `150MB`; after switching
  to `(sourceClass, target)` keys that retention no longer dominates the
  heap profile

Conclusion:

- the numeric-key conversion is worth keeping
- biggest remaining scanner costs are now `subgraph`, `resolveSearchPath`,
  and generic DFS/materialization, not string-heavy cache keys

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
