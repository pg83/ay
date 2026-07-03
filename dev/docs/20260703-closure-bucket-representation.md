# Closure bucket representation & merge — exploration log (2026-07-03)

Status: **explored, reverted to the `81f5ecb` design.** Production keeps compacted
buckets + flat-block splice + copy-on-miss intern. This note records what was
tried, the measurements, and the constraints that decided it, so the direction
can be picked up later without repeating the dead ends.

## The subsystem

Include-closure build lives in `scanner.go` `dfs` → Tarjan SCC, feeding
`bucket_cache.go`. A closure is `Closure{self VFS; buckets [][]VFS}` — a root
file plus its transitive residue, partitioned into 16 buckets by `strID & 15`.
Each bucket is hash-consed through `BucketCache` (`internBucket`), so identical
buckets across closures share one backing slice (the −82% memory win). The
per-file closure index is a per-scanner `DenseMap[STR, Closure]` — **per-scanner
is load-bearing**: the same header resolves to a different closure on target vs
host (musl arch headers), so a shared closure map corrupts multi-platform graphs.

Acceptance: `dev/validate.py` — 7 gating cases byte-exact, yabs XFAIL
`matched=71689` must not drop.

Perf recipe: `yabs/server/daemons/bs_static`, `min of 10`, **no `-G`**,
`YATOOL_CPUPROFILE`, merged pprof; the reliable isolated metric for this work is
`storeBuckets` cum (distribute + intern), not the noisy whole-program total.

## The `81f5ecb` baseline (what production runs)

- `buckets` is **compacted** — only the non-empty buckets, variable length.
- `dfs` builds the closure as one **flat `[]VFS` block** via `Closure.spliceInto`
  (`IdSet.spliceNew` dedup), then `storeBuckets` re-buckets that block by
  `strID & 15` into a 16-slot scratch and **hash-conses each bucket, copying
  into the pool only on a miss** (copy-on-miss).

## What was tried (all regressed; all reverted)

| direction | commit(s) | result |
|---|---|---|
| positional-16 buckets + direct-to-buckets fold in `dfs` | 3421f5f, b3028c4 | **+2.7% CPU** (always-16 slice headers, index-aligned merge) |
| 16-pool zero-copy intern (one bump pool per nibble) | — | **+4.3% CPU**, +RSS (scattered writes to 16 cold chunks, fragmentation) |
| buckets-outer, no window saved | — | **BROKEN**, matched 19383 (see windowSubsumed below) |
| buckets-outer, 2-pass with saved window | — | correct but **+21% CPU** (extra full pass forced by windowSubsumed) |
| zero-copy `storeBuckets` (16 nibble-filter passes over `rest`) | — | **+42% CPU** (16× iteration over the residue) |

## Constraints discovered (these are the reusable lessons)

1. **`windowSubsumed` forces children-outer.** It skips a child whose closure is
   already covered by an earlier child; correctness relies on each accepted
   child's *full* closure being marked in `gen` **before** the next child is
   decided. That is by definition "iterate children, accumulate as you go." A
   buckets-outer loop only has partial (per-nibble) state mid-pass, so:
   - Naive buckets-outer **self-trips**: once a child's `self` enters `gen` at
     its nibble bucket, `windowSubsumed(child)` returns true for later buckets and
     the child's higher buckets are dropped → matched 19383.
   - Doing it correctly needs a children-outer pre-pass to freeze the window,
     then a buckets-outer write pass = two full passes over the residue → +21%.

2. **`windowSubsumed` is not pure perf.** Removing it entirely changes the output
   (`matched` 71689 → 71687) — it must stay for correctness, not just speed.

3. **copy-on-miss beats zero-copy** for this intern. The hash-cons hit rate is
   high (that is the whole point of the sharing). Copy-on-miss touches the cold
   pool *only for unique buckets*; any build-in-place zero-copy scheme writes the
   cold pool for **every** bucket including hits, then rolls back — strictly more
   cold-pool traffic. Avoiding the copy costs either 16× residue iteration
   (single pool) or 16-pool fragmentation. The copy is R sequential writes into a
   hot scratch; eliminating it is more expensive than paying it.

4. **The one byte-safe win kept (`7b88687`, −2.2% CPU):** the build-leaf frontier
   only needs to expand **`abs`'s own** ClosureLeaves, transitively. A child
   build node already carries its leaves inside the child's buckets (expanded
   when the child closure was built), so re-scanning the whole closure block for
   build nodes is redundant. Seed the frontier with `abs` only.

## Conclusion

`81f5ecb` (compacted + flat block + copy-on-miss) is the CPU floor for this
workload; the closure merge is memory-latency-bound on scattered `gen[]` and
child-bucket reads and is genuinely incompressible by representation changes. If
revisited, the only untried lever is algorithmic (whole-set closure memo / more
subsumption), or SIMD on the splice — both flagged as low-probability. Do **not**
re-try positional buckets, buckets-outer, or zero-copy intern without a new idea
that dodges constraints 1 and 3 above.
