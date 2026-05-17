# PLAN 3: Reduce materialized subgraph cache cost

## Goal

Reduce allocation and retained memory from scanner subgraph materialization.

Current allocation profile:

- `main.(*scanCtx).subgraph`: 435.93 MB flat, 1237.99 MB cumulative
- `main.(*scanCtx).WalkClosure`: 1730.44 MB cumulative
- `main.(*scanCtx).dfs`: 1650.72 MB cumulative

The cache helps avoid repeated DFS, but it also copies and retains many `[]VFS` closures.

## Current Shape

`subgraph()` computes and stores a full closure slice:

```go
out := make([]VFS, len(order))
copy(out, order)
sc.subgraphCache[key] = out
```

The key is `(absPath, srcClassHash)` and the value is root-included DFS order.

This is correct but expensive for large one-off closures.

## Proposed Model

Add an admission policy for subgraph caching:

1. Always cache small subgraphs.
2. Do not cache very large subgraphs on first sight.
3. Optionally cache large subgraphs only after seeing the same key requested more than once.

Initial conservative thresholds:

- `smallSubgraphLimit = 256`
- `largeSubgraphLimit = 2048`

Behavior:

- `len(order) <= smallSubgraphLimit`: cache immediately.
- `len(order) > largeSubgraphLimit`: return uncached.
- middle range: cache if the key has already missed once.

## Implementation Steps

1. Add a miss-count/admission map to `scanCtx`:

```go
subgraphSeen map[subgraphInnerKey]uint8
```

2. In `subgraph()` after a clean walk, decide whether to cache.

3. If not caching, still return a fresh `[]VFS` to the caller because caller iterates after scratch buffers are returned.

4. Make sure uncached clean subgraphs do not get marked tainted.

5. Add scanner stats counters:

- cached subgraphs
- bypassed large subgraphs
- cache bytes or total cached element count

6. Reprofile CPU and heap. If CPU regresses sharply, threshold is too low or repeated large subgraphs are important.

## Correctness Checks

- `go test ./...`
- `./validate.sh`
- Normalized graph output must be byte-equivalent for existing exact cases.

This plan should not change graph semantics. It only changes cache admission.

## Expected Impact

Heap alloc and retained cache memory should drop in:

- `scanCtx.subgraph`
- `scanCtx.walkSubgraph`
- `scanCtx.WalkClosure`

CPU may improve due to less GC. CPU may regress if too many useful subgraphs stop being cached. Treat this as a measured tuning step.

## Risks

- The previous children-cache experiment worsened performance. This plan must be guarded by profile numbers after each threshold change.
- Returning scratch-backed slices would be a use-after-pool bug. Always copy when returning an uncached result.
- If most large subgraphs are hot, delayed caching may hurt. Use counters before locking in thresholds.
