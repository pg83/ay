# sg3.json Generation Performance Plan

This plan is for speeding up `yatool make -j 0 -G ... devtools/ya/bin`
generation of `sg3.json`.

Do not start with the JSON writer optimization here. The separate low-risk
`appendVFS` fix is intentionally excluded from this plan.

## Baseline

Measured on the current tree with:

```bash
go build -o yatool .

/usr/bin/time -v env -u CFLAGS -u CXXFLAGS \
    YATOOL_CPUPROFILE=.out/perf-sg3.cpu.pprof \
    YATOOL_MEMPROFILE=.out/perf-sg3.mem.pprof \
    PYTHON='$(YMAKE_PYTHON3)/bin/python3' \
    CC='$(CLANG)/bin/clang' \
    CXX='$(CLANG)/bin/clang++' \
    OBJCOPY='$(CLANG)/bin/llvm-objcopy' \
    ./yatool make \
    -j 0 \
    -k \
    -G \
    --target-platform default-linux-aarch64 \
    --host-platform default-linux-x86_64 \
    --host-platform-flag MUSL=yes \
    --musl \
    devtools/ya/bin > /dev/null
```

Observed numbers:

- `8.4s` wall.
- `10.6s` CPU.
- `126%` CPU.
- `~1.34 GiB` peak RSS.
- generated JSON is `~462 MiB`.
- graph has `14649` nodes by `"uid"` count.

Control run without `-G`:

- `7.8s` wall.
- `~1.07 GiB` peak RSS.

Interpretation:

- Main bottleneck is include scanning, not JSON output.
- `-G` adds roughly `0.4-0.6s` and `~270 MiB` RSS from materializing and
  serializing the graph.
- Scanner work and allocation churn dominate CPU and memory.

Useful pprof commands:

```bash
go tool pprof -top -nodecount=40 ./yatool .out/perf-sg3.cpu.pprof
go tool pprof -top -cum -nodecount=60 ./yatool .out/perf-sg3.cpu.pprof
go tool pprof -top -nodecount=40 -sample_index=alloc_space ./yatool .out/perf-sg3.mem.pprof
go tool pprof -top -nodecount=40 -sample_index=alloc_objects ./yatool .out/perf-sg3.mem.pprof
```

Current hot spots:

- [scanner.go](./scanner.go) `(*scanCtx).forEachResolvedChild`: about `67%`
  cumulative CPU.
- [scanner.go](./scanner.go) `(*scanCtx).dfs` / `WalkClosure`: about `62%`
  cumulative CPU.
- [scanner.go](./scanner.go) `(*scanCtx).subgraph`: about `52-58%`
  cumulative CPU.
- [scanner.go](./scanner.go) `(*scanCtx).resolveSearchPath`: about `19%`
  cumulative CPU.
- Go runtime map lookups and hashing: `runtime.mapaccess2_faststr` about
  `31%` cumulative CPU.
- GC marking: about `2.1s` CPU.
- `alloc_space`: about `2.25 GiB`.

Current allocation sources to keep in mind:

- `(*scanCtx).subgraph`: hundreds of MiB, especially the cached `[]VFS`
  copies.
- `(*scanCtx).resolveSearchPath`: hundreds of MiB, especially repeated
  `prefix.Rel + "/" + target` string construction.
- `VFS.String`: many allocations; excluded from this plan because it is the
  JSON-writer low-risk fix.
- `os.ReadFile`: about `174 MiB`; not the first target.

## Always Verify

After every change:

```bash
go test ./...
```

For behavior parity, use the validate flow. The full script currently checks
`sg2.aarch64`, `sg2.x86_64`, and `sg3.aarch64`:

```bash
./validate.sh
```

If only `sg3.aarch64` needs inspection, reuse the command from
`validate.sh` and normalize against `/home/pg/monorepo/yatool_orig/sg3.json`.

When validate fails, inspect with:

```bash
./diff.py \
    --our ./.out/validate/sg3.aarch64.our.norm.json \
    --ref ./.out/validate/sg3.aarch64.ref.norm.json \
    --root-output /devtools/ya/bin/ya-bin \
    --show-cmd-diff
```

For perf comparison, always collect:

- wall time from `/usr/bin/time -v`;
- peak RSS from `/usr/bin/time -v`;
- CPU top;
- alloc space top;
- alloc objects top;
- scanner counters from the instrumentation in step 1.

Keep a short before/after table in commit messages or notes.

## Step 1: Add Final Perf Counters

Goal: make scanner performance measurable without reading pprof every time.
This is instrumentation only.

Current state:

- `SCANNER_STATS=1` prints periodic lines from
  [scanner.go](./scanner.go), but it is hard to compare runs because lines
  are emitted per `WalkClosure` call and not as a final summary.
- [gen.go](./gen.go) already tracks `scanCtxAllocs` and `scanCtxPeak`, but
  nothing prints them.

Add an env-gated final summary, for example `YATOOL_PERF_STATS=1`.

Implementation points:

- In [parser_manager.go](./parser_manager.go), add counters to
  `sharedParseCache` or `includeParserManager`:
  - parsed source cache hits;
  - parsed source cache misses;
  - existence cache hits;
  - existence cache misses;
  - buildParsed count at the end.
- In [scanner.go](./scanner.go), add counters to `IncludeScanner`:
  - `walkClosureCalls`;
  - `dfsCalls`;
  - `plainDfsCalls`;
  - `subgraphCacheEntries` by summing active `scanCtx` maps later;
  - `resolveCacheHits`;
  - `resolveCacheMisses`;
  - `resolveSearchPathCalls`;
  - `sysinclSourceHits/Misses`;
  - `sysinclIncluderHits/Misses`.
- In [gen.go](./gen.go), after `genModule` returns and before returning from
  `runGenIntoWithResources`, print a final summary if
  `YATOOL_PERF_STATS=1`.
- The summary should distinguish target and host scanner values. Print one
  line per scanner plus one line for `genCtx`.
- Keep output on stderr.

Suggested output shape:

```text
perf: gen scanCtxAllocs=... scanCtxPeak=... internedScanCtx=...
perf: parser parsedHits=... parsedMisses=... existsHits=... existsMisses=... buildParsed=...
perf: scanner target walkClosure=... dfs=... subgraphHits=... subgraphMisses=... tainted=... resolveHits=... resolveMisses=... sysinclSourceHits=... sysinclSourceMisses=... sysinclIncluderHits=... sysinclIncluderMisses=...
perf: scanner host ...
```

Care points:

- The code is single-goroutine in generation mode. Plain integer counters are
  fine.
- Do not print by default.
- Do not change graph output on stdout.
- Do not let stats output depend on map iteration order.

Tests:

- Add focused unit tests only if the instrumentation needs new public helper
  functions.
- Otherwise rely on `go test ./...` and a manual
  `YATOOL_PERF_STATS=1 ... > /dev/null` smoke run.

Done criteria:

- `YATOOL_PERF_STATS=1` produces stable final summary lines on stderr.
- Normal `yatool make -G` stdout remains graph-only.
- `go test ./...` passes.
- `./validate.sh` behavior is unchanged.

## Step 3: Cache Search-Path Tier by Target

Goal: cut repeated search-path probing and string construction in
`resolveSearchPath`.

Current problem:

- [scanner.go](./scanner.go) `resolveSearchPath` checks same-dir, own
  `ADDINCL`, peer `GLOBAL ADDINCL`, and base paths for every
  `(includer, target, kind)`.
- The expensive tier is `OwnAddIncl + PeerAddInclSet + BaseSearchPaths`.
- For a fixed `scanCtx` and include `target`, the ADDINCL/peer/base result
  does not depend on the includer.
- Current code repeatedly constructs strings like
  `prefix.Rel + "/" + target`, does `fileExistsByRel`, and stores many tiny
  `[]VFS` entries in `resolveCache`.

Add a per-`scanCtx` cache for the source-independent search tier.

Suggested structures in [scanner.go](./scanner.go):

```go
type searchTierKey struct {
    target uint32
}

type searchTierResult struct {
    paths []VFS
    found bool
}

type scanCtx struct {
    ...
    searchTierCache map[searchTierKey]searchTierResult
}
```

`found` is necessary because `nil` paths can mean either "cache miss" or
"searched and found nothing".

Algorithm:

1. Keep same-dir quoted lookup in `resolveSearchPath`; it depends on
   `includerAbs`.
2. Keep build-root direct handling for `includerAbs.IsBuild()` in
   `resolveSearchPath`; it depends on includer/provenance.
3. Move only this part into a helper:
   - loop over `ctx.OwnAddIncl`;
   - loop over `ctx.PeerAddInclSet`;
   - loop over `ctx.BaseSearchPaths`.
4. Key that helper only by interned `target`.
5. Preserve first-match-wins behavior: the helper must stop at the first
   path that resolves.
6. Return a cache-owned `[]VFS`; callers must treat it as read-only.

Pseudo-code:

```go
func (sc *scanCtx) resolveContextSearchTier(target string) searchTierResult {
    s := sc.scanner
    key := searchTierKey{target: s.interner.internString(target)}
    if cached, ok := sc.searchTierCache[key]; ok {
        return cached
    }

    var out []VFS
    found := false

    try := func(prefix VFS) bool {
        // Same semantics as old addInclPath.
        // Avoid touching same-dir logic here.
    }

    for _, p := range sc.cfg.OwnAddIncl {
        if try(p) { found = true; break }
    }
    if !found {
        for _, p := range sc.cfg.PeerAddInclSet {
            if try(p) { found = true; break }
        }
    }
    if !found {
        for _, p := range sc.cfg.BaseSearchPaths {
            if try(p) { found = true; break }
        }
    }

    res := searchTierResult{paths: out, found: found}
    sc.searchTierCache[key] = res
    return res
}
```

Expected effect:

- Large reduction in `resolveSearchPath.func3` allocation.
- Fewer `fileExistsByRel` calls.
- Lower `runtime.mapaccess2_faststr` CPU.
- Lower GC pressure.

Risk:

- Same-dir quoted includes must still dominate sysincl gating exactly as
  before.
- `#include_next` must still return nil before this path.
- Build-root generated include handling must remain unchanged.
- `resolveCache` still exists and should continue to cover full
  `(includer,target,kind,next)` result. The new cache is an inner helper, not
  a replacement yet.

Tests to add:

- A scanner test where two different includers in the same `scanCtx` include
  the same target that resolves via `OwnAddIncl`; verify output is unchanged.
- A scanner test where same-dir quoted include shadows an `ADDINCL` hit;
  verify same-dir still wins.
- A scanner test where target is absent; call twice and verify no behavioral
  difference. If stats are present, verify second call hits the new tier cache.

Validation:

- `go test ./...`.
- `./validate.sh`.
- Perf run with `YATOOL_PERF_STATS=1` and pprof.

Done criteria:

- Normalized graph parity is unchanged.
- `resolveSearchPath.func3` and `fileExistsByRel` allocation/CPU decrease.
- `existsMisses` should not grow; ideally it drops.

## Step 4: Store Subgraph Cache as Interned IDs

Goal: reduce memory held by `subgraphCache` and reduce copying cost.

Current problem:

- [scanner.go](./scanner.go) `subgraphCache` stores `map[subgraphInnerKey][]VFS`.
- On every clean miss, [scanner.go](./scanner.go) copies `order` into a
  cache-owned `[]VFS`.
- pprof attributes hundreds of MiB to this cache/copy path.
- `subgraphInnerKey` already uses interned path IDs, but the value does not.

Change the cache value from `[]VFS` to `[]uint32`.

Suggested changes:

```go
type scanCtx struct {
    ...
    subgraphCache map[subgraphInnerKey][]uint32
}
```

Add reverse lookup to `scannerInterner`:

```go
type scannerInterner struct {
    stringIDs map[string]uint32
    strings []string // id-1 -> rel
    nextStr uint32
}

func (si *scannerInterner) vfsByID(id uint32) VFS
```

Rules:

- Keep `scannerInternerBuildBit` semantics.
- `internString` must append the new string to `strings` exactly when it
  allocates a new ID.
- `vfsByID` should reconstruct `VFS{Root: ..., Rel: ...}` without allocating.
  Returning a `VFS` with an existing string is fine.

Migration plan:

1. Add `strings []string` and `vfsByID`.
2. Change `subgraphCache` type only.
3. In `subgraph`, after `walkSubgraph`, convert `order []VFS` to
   `out []uint32` using `internVFS`.
4. In callers that merge child subgraphs, iterate IDs and call `vfsByID`.
5. Keep the external `WalkClosure` return type as `[]VFS`.

Pseudo-code for merge:

```go
for _, id := range sg {
    p := s.interner.vfsByID(id)
    if !visited.AddIfAbsent(p) {
        continue
    }
    *order = append(*order, p)
}
```

Expected effect:

- `subgraphCache` value memory drops because each cached entry element goes
  from a `VFS` struct to a `uint32`.
- Less allocation from `out := make([]VFS, len(order))`.
- Some CPU still remains because `visited` is still VFS/string-keyed. That is
  addressed in step 5.

Risk:

- Intern IDs are scanner-local. Do not share cached ID slices across scanners.
- Keep target and host scanners separate.
- Make sure `vfsByID` handles build bit correctly.
- Do not change closure order.

Tests to add:

- Unit test for `scannerInterner` round trip:
  - `Source("a/b.h")`;
  - `Build("x/y.pb.h")`;
  - repeated calls return same ID;
  - `vfsByID(internVFS(v)) == v`.
- Existing scanner subgraph cache tests should still pass.

Validation:

- `go test ./...`.
- `./validate.sh`.
- Perf run.

Done criteria:

- Graph parity unchanged.
- `alloc_space` at `(*scanCtx).subgraph` drops materially.
- Peak RSS drops.
- CPU should not regress by more than noise.

## Step 5: Move Hot DFS Visited/Order to Interned IDs

Goal: reduce CPU spent in string-keyed maps during include DFS.

Current problem:

- `visited` is `VFSSet`, internally map buckets keyed by `Rel string`.
- CPU profile shows heavy `runtime.mapaccess2_faststr`, string hashing, and
  map access in the scanner.
- After step 4, subgraph cache values are IDs, but merging still converts back
  to `VFS` for `visited`.

Introduce scanner-local ID sets for DFS internals.

Suggested structures:

```go
type idSet map[uint32]struct{}
```

or a reusable two-state structure if profiling says map overhead is still too
high. Start simple with `map[uint32]struct{}` because `mapaccess2_fast64` is
already cheaper than string hashing.

Plan:

1. Add `idSet` pool to `IncludeScanner`:
   - `visitedIDPool sync.Pool`;
   - `orderIDPool sync.Pool`.
2. Add ID-based internal methods:
   - `dfsID(absID uint32, visited idSet, order *[]uint32)`;
   - `plainDfsID(...)`;
   - `walkSubgraphID(...)`.
3. Keep public APIs unchanged:
   - `WalkClosure(vfsPath VFS) []VFS`;
   - `WalkSource(sourceRel string) []VFS`.
4. Public entry converts root `VFS` to `rootID` once.
5. Internal child resolution still returns `[]VFS` initially. Convert each
   child to ID at the boundary in `forEachResolvedChild` or a new
   `forEachResolvedChildID`.
6. At the final public boundary, convert `order []uint32` to `[]VFS` and skip
   the root.

Suggested helper:

```go
func (sc *scanCtx) forEachResolvedChildID(absID uint32, fn func(uint32)) {
    vfsPath := sc.scanner.interner.vfsByID(absID)
    sc.forEachResolvedChild(vfsPath, func(child VFS) {
        fn(sc.scanner.interner.internVFS(child))
    })
}
```

Keep step 5 separate from step 4. Step 4 is mostly memory; step 5 is CPU.

Risk:

- This touches the scanner traversal core. It can silently perturb include
  order if done carelessly.
- `isSourceLike` currently takes `VFS`; preserve exact behavior by converting
  `absID` to `VFS` there or by adding an ID-aware helper that reads `Rel`.
- `sourceClassID(sc.cfg.SourceRel)` behavior must remain unchanged.
- Build-root generated parsed includes must still dispatch through
  `parser_manager.buildParsed`.

Tests:

- Existing scanner cache/order tests are mandatory.
- Add a dedicated order test with:
  - two headers including a shared child;
  - one generated `$(B)` include registered via `RegisterBuildParsedIncludes`;
  - verify closure order and dedup match previous expected output.

Validation:

- `go test ./...`.
- `./validate.sh`.
- Perf run.

Done criteria:

- Graph parity unchanged.
- CPU in `runtime.mapaccess2_faststr` and `VFSMap.AddIfAbsent` drops.
- CPU may move to `mapaccess2_fast64`; total scanner CPU should still drop.
- Peak RSS should not increase.

## Step 6: Rework File Existence Checks

Goal: reduce filesystem stat overhead after search-path cache has removed
obvious repeated probes.

Current problem:

- [parser_manager.go](./parser_manager.go) `fileExistsByRel` uses `os.Stat`
  on cache miss.
- pprof shows `os.statNolog` and `syscall.ByteSliceFromString`, but they are
  secondary hot spots.
- Do not optimize this before step 3, because repeated probes are the larger
  problem.

Preferred approach: per-directory `readdir` cache.

Add to `sharedParseCache`:

```go
dirEntries map[string]map[string]bool
dirExists map[string]bool
```

Algorithm:

1. Split `rel` into `dir` and `base`.
2. If directory was already read:
   - answer `entries[base]`.
3. Otherwise read directory once with `os.ReadDir(sourceRootSlash + dir)`.
4. Store only non-directory file names as true.
5. If `ReadDir` fails, store `dirExists[dir] = false` and return false.

Care points:

- `fileExistsByRel` currently answers false for directories. Preserve that.
- Normalize path before calling this function, as current callers already do.
- Source root file case: `dir == ""` must read `sourceRoot`.
- The cache depends only on the source tree, so it belongs in
  `sharedParseCache`, same as the current `exists` map.
- Keep the old `exists map[string]bool` during initial implementation if it
  simplifies migration. Later remove it only after perf confirms the directory
  cache wins.

Risk:

- `os.ReadDir` can allocate more than `os.Stat` for one-off directories.
  This is why it must come after step 3 and be measured.
- Some include probes may touch many unique directories with one file each.
  If RSS grows, revert or add a heuristic.

Possible heuristic:

- Keep `exists map[string]bool` as a first-level cache.
- Use `ReadDir` only after the same directory has seen N misses/probes.
- Start with N=4 if naive `ReadDir` regresses memory.

Tests:

- Unit test for `fileExistsByRel`:
  - existing file returns true;
  - directory returns false;
  - missing file returns false;
  - repeated calls hit cache.
- Add counters from step 1 to verify fewer syscalls or fewer misses.

Validation:

- `go test ./...`.
- `./validate.sh`.
- Perf run.

Done criteria:

- Graph parity unchanged.
- `os.statNolog` and `syscall.ByteSliceFromString` drop in CPU/alloc profile.
- Wall time improves or stays neutral.
- Peak RSS does not materially regress.

## Suggested Order

1. Add final perf counters.
2. Cache search-path tier by target.
3. Store `subgraphCache` values as interned IDs.
4. Move hot DFS visited/order to interned IDs.
5. Rework file existence checks.

Commit each step separately. Each commit should include:

- what changed;
- before/after wall time and RSS for `sg3.aarch64`;
- relevant pprof delta;
- `go test ./...` result;
- validate result.

## Stop Conditions

Stop and reassess if:

- `./validate.sh` loses normalized parity;
- scanner order changes in unit tests;
- peak RSS grows by more than `5%`;
- a step improves CPU but makes wall time worse by more than noise;
- implementation starts requiring broad changes outside scanner/parser/gen
  plumbing.

