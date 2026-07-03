# bucketHash — order-independent multiset hash exploration (2026-07-03)

Status: **explored and validated, then reverted; production keeps `Σ mix64(v)`.**
The faster variant + the `ay dev perf buckethash` tool live in commit `1118dcd`
(reverted by `d6793a2`) to pick up later. This note records the design space and
the measurements.

## What bucketHash is and why collisions are fatal

`bucketHash([]VFS) uint64` keys the content-addressed bucket intern:
`c.intern.cell(bucketHash(elems))`. `IntMap` trusts the 64-bit key and does **not
compare bucket contents** on a hit. So a hash collision = two different buckets
share one slot = one closure silently gets another's slice = wrong graph. The
hash must be near-collision-free, order-independent (a bucket is a set), and fast
(it is on the closure-intern hot path).

## Literature (multiset hashing)

- **MSet-Add-Hash**: `Σ H(v)` — efficient, multiset-collision-resistant. This is
  exactly the original `Σ mix64(v)`.
- **MSet-XOR-Hash**: `⊕ H(v)` — set-only, needs a secret key.
- **MSet-Mu-Hash / ECMH**: finite-field product / elliptic curve — key-free but
  slower.
- Universal/polynomial hashing gives provable bounds via field arithmetic (per-
  element field multiply).

The load-bearing fact: **each element must be avalanched before the commutative
combine.** Cheap linear pre-combines (raw sum, xor) lose the entropy and collide.

## Failed cheap attempts (gate `matched` drops)

| hash | matched | why |
|---|---|---|
| `Σ mix64(v)` (original) | 71689 | baseline, MSet-Add-Hash |
| `sum + Πv` | 37591 | product saturates to 0 (VFS often even → factors of 2) |
| `splitMix64(sum^xor, prod^xor)` | 13432 | product garbage + lossy fold |
| `splitMix64(sum, xor)` | 61425 | both linear (Σ mod 2³², ⊕ over GF(2)) collide |

`10284`-node diffs are **not** 10k collisions — one collision on a widely-shared
bucket cascades to many nodes. Actual collisions were a handful with wide blast
radius.

## The correct fast hash: `Σv + Σv² + ⊕v`

Three independent invariants, folded once at the end:
`splitMix64(sum, sq) ^ mix64(uint64(xor))`.

- `Σv²` (the square) is the **nonlinearity** that plain sum/xor lack — it breaks
  their linear collisions.
- `⊕v` breaks the residual *moment* collisions that `sum+sq` alone admits:
  `{1,5,6}` and `{2,3,7}` share Σ (=12) and Σ² (=62) but differ in xor (2≠6). Two
  power-sum moments provably cannot distinguish such sets; the third invariant
  (xor, a different algebra) is mathematically required.

Gate: byte-exact, `matched=71689`. Collision stress (`ay dev perf buckethash`,
random sequences len 0..10000, values 0..100000): no genuine collision — 64-bit
birthday is ~4·10⁹ sequences (unreachable), and no *early* collision means the
effective entropy is near the full 64 bits (no structural weakness).

## Speed (clean-field microbench, ns/element vs `Σ mix64(v)`)

Key findings:
- **`.strID()` (a `>>1` shift) cost ~0.2 ns/elem** — hash `v` directly (it is
  1:1 with strID, collision-equivalent). This alone was 1.54× → 1.94×.
- **Winning structure = independent per-element ops summed into separate
  accumulators (max ILP).** `v*v` is one independent, pipelined multiply.
- Everything with a **dependency chain lost**, latency-bound not throughput-bound:
  `mix64` (2 chained muls) 1.0×, `Πodd` product chain 1.37×, xorshift avalanche
  chain 1.13×. Shifts have more ports but the chain serializes them.
- **2-wide unroll (independent accumulator pairs s0/s1, q0/q1, x0/x1) + unsafe
  pointer walk.** The Go compiler emits **3 bounds checks per 2 elements** for
  manual `elems[i]`/`elems[i+1]` indexing (a range loop has none); `unsafe`
  removes them. This took the *safe 3-invariant* hash to **2.03–2.04×**, matching
  the 2-moment lower bound — i.e. the xor becomes free once unrolled.

Real build (yabs bs_static): `storeBuckets` cum **6.21s → 5.14s (−1.07s)**,
byte-exact. (Note: an earlier profiler flat-attribution suggested 3.8×; the
clean-field bench corrected it to ~2× — inlining/attribution inflated the flat.)

## If revisited

The production hash is `Σ mix64(v)`. To ship the 2× version, restore `1118dcd`:
`bucketHash` = 2-wide-unsafe `sum+sq+xor`, plus `ay dev perf buckethash`
(throughput bench + collision stress). It is validated byte-exact; it was
reverted only to keep production unchanged for now.
