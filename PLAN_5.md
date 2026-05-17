# PLAN 5: Cache walkPeersForGlobalAddIncl results in module emission

## Goal

Avoid repeatedly walking peer closures to compute GLOBAL ADDINCL contribution.

Current profile:

- `walkPeersForGlobalAddIncl`: 4.50s cumulative CPU
- `walkPeersForGlobalAddIncl`: 387 MB cumulative allocations

This is outside scanner internals, but it expands scanner contexts and repeatedly computes the same peer-derived include path sets.

## Current Shape

`genModule` calls `walkPeersForGlobalAddIncl(ctx, instance, d)` to compose peer-global include context.

`genModule` already memoizes module emission by `ModuleInstance`. The peer-global result should be part of that memoized module result rather than recomputed by every consumer path.

## Proposed Model

Store peer-global ADDINCL contribution on `moduleEmitResult`.

Possible field:

```go
type moduleEmitResult struct {
	...
	PeerGlobalAddIncl peerGlobalContribs
}
```

Then:

- compute once during module emission;
- reuse when parent modules need peer contribution;
- avoid recursively walking the same peer graph just to rebuild include paths.

## Implementation Steps

1. Read `moduleEmitResult` definition and current fields.

2. Read `walkPeersForGlobalAddIncl` call sites.

3. Move the computed `peerGlobalContribs` into `moduleEmitResult`.

4. When processing a peer, use `peerResult.PeerGlobalAddIncl` if it already represents the needed transitive contribution.

5. Preserve allocator/runtime suppression rules currently inside `walkPeersForGlobalAddIncl`.

6. Add a targeted test around a module with:

- own GLOBAL ADDINCL
- transitive peer GLOBAL ADDINCL
- allocator/runtime suppression if there is an existing test fixture

7. Run `go test ./...` and `./validate.sh`.

## Correctness Checks

- `sg2.aarch64` exact
- `sg2.x86_64` exact
- `sg3` should not regress
- Inspect a known YA binary CC node's include inputs before/after for order stability

## Expected Impact

CPU/alloc reduction in:

- `walkPeersForGlobalAddIncl`
- `genModule`
- scanner context construction due to fewer rebuilt include slices

This also reduces pressure on PLAN 1 and PLAN 4 by shrinking repeated context construction work.

## Risks

- GLOBAL ADDINCL order matters. Reuse must preserve declaration and traversal order.
- Some contribution may depend on caller axis or module kind. Key the cached value by `ModuleInstance`, including platform/lang/kind, not just path.
- Suppression rules must remain at the same semantic boundary.
