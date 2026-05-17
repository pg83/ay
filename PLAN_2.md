# PLAN 2: Key sysincl source cache by source class, not source path

## Goal

Reduce retained heap in sysincl caches by replacing path-specific source cache entries with equivalence-class entries.

Current in-use heap profile:

- `main.(*IncludeScanner).sysinclSourceLookup`: 150.07 MB flat, 236.95 MB cumulative
- `main.SysInclSet.PreparePerSource`: 80.30 MB cumulative
- `main.PerSourceView.computeActiveIncluderRecords`: 9 MB flat

This means many source paths keep their own sysincl cache entries even when they activate the same source-filter records.

## Current Shape

`sysinclSourceCache` is keyed by:

```go
type sysinclSourceKey struct {
	sourceRel string
	target    string
}
```

But `scanner.go` already has `sourceClassHash(sourceRel)`:

```go
func (s *IncludeScanner) sourceClassHash(sourceRel string) uint64
```

That hash is intended to represent the active source-keyed sysincl record set. Two sources with the same class should have identical source-keyed mappings for a given target.

## Proposed Model

Introduce a stable source-class ID and key source sysincl lookup by:

```go
type sysinclSourceKey struct {
	sourceClassID uint32
	target        uint32
}
```

If PLAN 1 is already done, `target` is the interned include target ID. If not, use:

```go
type sysinclSourceKey struct {
	sourceClassHash uint64
	target          string
}
```

The preferred order is PLAN 1 first, then this plan.

## Implemented Design

PLAN 1 already moved `sysinclSourceCache` itself to `(sourceClass, target)`.
The remaining retained heap turned out to be in the source-path view layer:

- `viewCache[sourceRel] -> PerSourceView`
- `PreparePerSource(sourceRel)` allocating full per-source views
- each view carrying `includerKeyed` and `includerFilterCache` even though
  scanner-side source lookups only need `activeSourceKeyed`

The implemented fix therefore finishes PLAN 2 at the view layer:

- keep `sourceClassCache map[string]uint32` only as `sourceRel -> classID`
- store one shared source-only view per class:
  `sourceClassViews map[uint32]PerSourceView`
- keep `sourceClassBuckets map[uint64][]uint32` and compare active record
  pointer lists before reusing a class ID, so `sourceClassSignature`
  collisions do not silently merge distinct classes
- build source-only views with just `activeSourceKeyed`
- leave includer-side state only in `anySrcView`

That means:

- source-keyed resolution results are shared per class
- source-specific filter evaluation still happens correctly
- per-path retention of full `PerSourceView` objects is gone

## Implementation Steps

1. Add `sourceClassID(sourceRel string) uint32` on `IncludeScanner`.

2. Intern source classes by the identity of active source-keyed records:

- reuse `sourceClassHash(sourceRel)` initially;
- keep `map[uint64]uint32` for class IDs;
- if collision risk is unacceptable, store and compare the active record pointer list before reusing ID.

3. Change `sysinclSourceKey`.

4. Change `sysinclSourceLookup` so it computes:

```go
classID := s.sourceClassID(sourceRel)
key := sysinclSourceKey{sourceClassID: classID, target: targetID}
```

5. Ensure `PerSourceView` is still used where source-specific filters need to be evaluated. This plan changes cache identity, not filter semantics.

6. Run `go test ./...`.

7. Reprofile heap in-use:

```bash
go tool pprof -top -inuse_space ./yatool .out/perf/sg3.plan2.heap.pprof
```

## Correctness Checks

- `go test ./...`
- `./validate.sh`
- Compare normalized sg3 before/after this plan. Expected behavior change: none.

## Measured Result

Correctness on the final tree:

- `go test ./...` = `ok`
- `validate.sh`
- `sg2.aarch64` = byte-exact
- `sg2.x86_64` = byte-exact
- `sg3.aarch64` unchanged from before:
- ours `e7dfcb960dcfec5ae8ca1a4388897bc00f56535a2705fd90bc797b951815bc87`
- ref  `ab7bbc788651077896a68e2c5fbdb647ed72af23791bb4744f4c635bebc342cc`

Perf on `devtools/ya/bin` (`make -j 0 -k -G`, final variant):

- wall time: `8.05s`
- user CPU: `10.03s`
- max RSS: `1081440 kB`
- `go test ./...`: `4.077s`

Compared to the PLAN 1 state before this work:

- in-use heap: about `187.49MB -> 100.37MB`
- max RSS: `1209140 kB -> 1081440 kB`

Key heap reductions:

- `main.(*IncludeScanner).sysinclSourceLookup` no longer dominates retained
  heap; it drops to `0.51MB` cumulative-flat scale
- `PreparePerSource` disappears from the heap top because scanner no longer
  stores one full `PerSourceView` per source path
- retained heap is now dominated by the includer side:
  `newIncludeScannerWith`, `resolveSearchPath.func3`,
  `PerSourceView.computeActiveIncluderRecords`,
  `sysinclIncluderLookup`

CPU notes:

- this plan is primarily a memory optimization
- CPU stayed in the same broad range as PLAN 1, while memory improved
  materially
- the remaining hot scanner costs are still generic DFS/subgraph and
  string-map hashing, not source-view retention

## Expected Impact

Primary memory reduction:

- `sysinclSourceLookup` in-use should fall sharply from ~150 MB flat.
- `SysInclSet.PreparePerSource` pressure should also drop if fewer source-specific views are retained or recomputed.

Secondary CPU reduction:

- fewer sysincl source cache entries means fewer map hashes and less GC scanning.

## Risks

- `sourceClassHash` collision would merge two different source-filter sets. Use an equality guard if this becomes more than a temporary implementation.
- Some source-filter records might depend on path text in a way not captured by active-record identity. Verify by reading `sysincl.go` before implementation.
- If `PerSourceView` stores source-specific state beyond active record lists, do not share the view itself; share only the resolved source lookup result.
