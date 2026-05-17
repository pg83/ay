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
