# PLAN 4: Optimize resolveSearchPath candidate generation

## Goal

Reduce CPU and allocation in include path resolution.

Current profile:

- `scanCtx.resolveSearchPath`: 6.60s cumulative CPU
- `resolveSearchPath`: 587 MB cumulative allocations
- `resolveSearchPath.func3`: 253 MB flat allocations
- `fileExistsByRel`: 1.87s cumulative CPU
- `normalisePath`: visible under candidate generation

The current implementation creates many candidate strings and normalizes every one.

## Current Shape

`resolveSearchPath` builds candidates like:

```go
prefix + "/" + target
normalisePath(rel)
s.fileExistsByRel(rel)
```

It also checks `strings.HasPrefix(prefix, "$(B)/")` inside the loop for every prefix.

## Proposed Model

Preprocess scan context search paths into typed prefixes:

```go
type includePrefixKind uint8

const (
	includePrefixSource includePrefixKind = iota
	includePrefixBuild
	includePrefixRoot
)

type includePrefix struct {
	kind includePrefixKind
	rel  string
}
```

Store this prepared form in `scanCtx` when it is constructed:

- own prefixes
- peer prefixes
- base prefixes

Then resolve candidates without per-iteration prefix classification.

## Implementation Steps

1. Add prepared prefix slices to `scanCtx`:

```go
ownPrefixes  []includePrefix
peerPrefixes []includePrefix
basePrefixes []includePrefix
```

2. Build them in `NewScanCtx`.

3. Add fast normalization:

```go
func needsNormalisePath(s string) bool {
	return strings.Contains(s, "..") || strings.Contains(s, "//") || strings.Contains(s, "/./")
}
```

Only call `normalisePath` when needed.

4. Replace `addInclPath(prefix, target)` with `addPrefixPath(prefix includePrefix, target string)`.

5. Avoid `strings.TrimPrefix` in hot loops by storing build prefix rel without `$(B)/`.

6. Profile again and confirm reductions in:

- `resolveSearchPath.func3`
- `normalisePath`
- `fileExistsByRel`
- string concat allocation

## Correctness Checks

- Add tests for:
  - empty base prefix
  - build-rooted prefix
  - path requiring normalization
  - path not requiring normalization
- `go test ./...`
- `./validate.sh`

## Expected Impact

This should reduce:

- candidate string churn
- repeated prefix classification
- unnecessary normalization
- map/file-exists pressure caused by duplicate normalized candidates

The expected CPU win is smaller than PLAN 1 but still meaningful because this function sits under every include resolution miss.

## Risks

- Search order is graph semantics. Preserve exact order: quoted directory, own, peer, base, fallback locators.
- Empty prefix must still mean source root, not `"/target"`.
- Build-rooted prefixes must still consult codegen registry, not filesystem.
