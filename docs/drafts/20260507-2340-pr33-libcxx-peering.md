# PR-33 — util-subtree libcxx peering + libcxx/libcxxrt peer-CFLAGS bundle + own-self-flag emission

## Problem

PR-32 closed at L2 = 83.27% / L3 = 80.51% with a structurally-clean musl
refactor and four peer-GLOBAL flag axes wired (ADDINCL / CFLAGS / CXXFLAGS /
CONLYFLAGS). Three independent gaps remain blocking the L2 / L3 ceiling.

**Gap 1 — util-subtree blocked from libcxx/libcxxrt auto-peering.**
`util/charset` and `util/datetime/parser.rl6.cpp.o` (peer-of-peer of util)
are reached by the walker but get zero implicit peers because
`isRuntimeAncestor("util/charset")` returns true via the
`HasPrefix(path, "util/")` clause in `gen.go::isRuntimeAncestor`. The
upstream reference graph emits libcxx/libcxxrt's GLOBAL ADDINCL
(`-I .../libcxx/include`, `-I .../libcxxrt/include`) and GLOBAL CXXFLAGS
(`-nostdinc++` × 2) on every util/charset C++ source — our generator drops
all four args. Empirical probe (gen.go edited to remove the `util/` prefix
match): `util/charset/all_charset.cpp.o` divergent slot count drops from
14 args missing → 10 args missing; the residual 10 are the cxx warning
bundle (Gap 3) — orthogonal. Cycle count: 7 (unchanged); compile passes.

**Gap 2 — own-side GLOBAL contributions not emitted in own cmd_args.**
A module that declares `ADDINCL(GLOBAL X)`, `CFLAGS(GLOBAL X)`,
`CXXFLAGS(GLOBAL X)`, or `CONLYFLAGS(GLOBAL X)` propagates X to every
PEERDIR consumer (working today via `walkPeersForGlobalAddIncl` + the
two-phase aggregator). The reference graph also emits X on the module's
OWN compile cmd_args. Our generator does not. Empirical: `libcxx/_/src/
algorithm.cpp.o` is libcxx compiling its own source; reference includes
`-I libcxx/include` + `-I libcxxrt/include` (libcxx's two GLOBAL ADDINCL
paths) + `-nostdinc++` (libcxx's GLOBAL CXXFLAGS) — we emit none of the
three. abseil-cpp shows the same pattern with one entry: missing
`-I contrib/restricted/abseil-cpp` (its single `ADDINCL(GLOBAL .)`).

**Gap 3 — module-OWN non-GLOBAL CFLAGS not threaded into ModuleCCInputs.**
Per `gen.go:382` the parser collects `d.cFlags` (own non-GLOBAL CFLAGS)
but the struct passed to `EmitCC` (gen.go:1306) only threads `CXXFlags`
and `COnlyFlags` — the C-AND-C++ slot is empty. libcxx's `IF (CXX_RT ==
"libcxxrt") CFLAGS(-DLIBCXXRT)` dispatches correctly to `d.cFlags` but
that field is dropped. Same defect costs `-DTCMALLOC_INTERNAL_256K_PAGES
-DTCMALLOC_DEPRECATED_PERTHREAD` on tcmalloc, `-funroll-loops
-fvisibility=hidden` on jemalloc, and `-Wnarrowing` on util/all_*.cpp.o.

**Gap 4 — clang C++ standard warning extensions bundle missing from
flags.go.** The 11 `-W*` args in `ymake_conf.py:1624-1636` (`-Wimport-
preprocessor-directive-pedantic`, `-Woverloaded-virtual`, the
`-Wno-deprecated-*` family, `-Wno-pessimizing-move`,
`-Wno-undefined-var-template`) are appended unconditionally for every
clang C++ compile when the module does NOT set `NO_COMPILER_WARNINGS()`.
Empirical: 60 reference CC nodes carry the bundle (util/, library/cpp/*,
abseil's MD shows `NO_COMPILER_WARNINGS` so its 156 are the OTHER axis,
not this); 3511 do not. Our generator emits zero of either path — needs
a new `cxxStandardWarnings` bundle and a gate in `composeTargetCC` /
`composeHostCC` keyed on (a) the source is C++ AND (b)
`!Flags.NoCompilerWarnings`.

**Gap 5 — module-self ADDINCL emission for non-GLOBAL.** Already
implemented (`d.addIncl` is threaded into ModuleCCInputs.AddIncl) but
several modules (libc_compat, mimalloc, zlib) still show missing -I
flags for their own paths. Sample: `libc_compat/readpassphrase.c.o`
WANT has `-I$(SOURCE_ROOT)/contrib/libs/libc_compat/include/
readpassphrase` — our generator misses it because libc_compat's ya.make
wraps the ADDINCL inside an IF block our evaluator does not enter.
This is a parser/macro-evaluator scope question, not a flag-routing
question — separate axis from the four above.

## Empirical findings

### runtimeAncestorPaths audit (gen.go:587-601)

13 entries today. Categorized below by "why was it added" and "is it
genuinely needed for cycle prevention":

| Path | Origin | Genuine cycle prevention? | Action |
|------|--------|---------------------------|--------|
| `contrib/libs/musl` | PR-26 | yes — musl peers musl/full peers musl | retain (literal only) |
| `contrib/libs/libc_compat` | PR-27 | yes (peers musl) | retain |
| `contrib/libs/linuxvdso` | PR-27 | weak (header-only; no cycle observed) | retain (defensive) |
| `contrib/libs/linuxvdso/original` | PR-27 | weak | retain |
| `contrib/libs/cxxsupp/builtins` | PR-26 | weak — builtins has zero PEERDIRs | retain (defensive) |
| `contrib/libs/cxxsupp/libcxx` | PR-27 | strong — libcxx peers libcxxrt peers libunwind peers libcxx | retain |
| `contrib/libs/cxxsupp/libcxxrt` | PR-27 | strong — libcxxrt peers libunwind peers libcxxrt | retain |
| `contrib/libs/cxxsupp/libcxxabi` | PR-27 | weak — not in M2 closure | retain (defensive) |
| `contrib/libs/cxxsupp/libcxxabi-parts` | PR-27 | weak — peers libcxxrt only | retain (defensive) |
| `contrib/libs/libunwind` | PR-27 | strong — libunwind peers libcxxrt | retain |
| `library/cpp/malloc/api` | PR-26 | weak — header-only | retain (defensive) |
| `library/cpp/sanitizer/include` | PR-27 | weak — header-only | retain (defensive) |
| `util` | PR-27 | strong — util peers util/charset peers util (back-edge) | retain |

The `isRuntimeAncestor` helper (gen.go:623-635) further extends each
literal entry to its entire SUBTREE via `HasPrefix(path, prefix+"/")`.
That subtree-extension is the over-inclusion: `util/charset` is treated
as a runtime ancestor purely because it lives under `util/`, even though
util/charset is itself a normal LIBRARY consumer of libcxx/libcxxrt. The
`HasPrefix` extension was originally added to break the obvious self-
include cycle of libcxx/libcxxrt PEERDIRs (their subtree members peering
each other), but the same goal can be met with a literal-only check
(every literal entry already covers its own self-cycle via the
`instance.Path != "..."` guards inside `defaultPeerdirsFor`).

**Empirical cycle re-test (probe applied 2026-05-07):** dropping the
`HasPrefix` extension entirely (literal-only `isRuntimeAncestor`) and
rebuilding `tools/archiver`: rc=0, cycle count = 7 (unchanged from
baseline), L0/L1 unchanged at 98.77% / 98.74%, L2/L3 unchanged at
83.27% / 80.51%. Per-node empirical: util/charset's CC node closed 4
of 14 missing args (the libcxx/libcxxrt -I and -nostdinc++ × 2) but
remained L3-divergent due to Gap 3+4 (cxx warning bundle + own CFLAGS
not threaded). So the runtimeAncestor refactor is necessary BUT NOT
SUFFICIENT for L2/L3 lift.

### L2 divergent pair distribution by (module_dir, platform, kv.p)

Top 25 (3683 paired total, 577 L2-divergent — multiset compare):

| Count | p | Plat | module_dir |
|------:|---|------|------------|
|  156 | CC | aarch64 | contrib/restricted/abseil-cpp |
|   78 | CC | x86_64  | contrib/tools/yasm |
|   51 | CC | aarch64 | contrib/libs/cxxsupp/libcxx |
|   51 | CC | aarch64 | contrib/libs/tcmalloc/no_percpu_cache |
|   51 | CC | x86_64  | contrib/libs/cxxsupp/libcxx |
|   30 | AS | x86_64  | contrib/libs/musl |
|   19 | CC | aarch64 | util |
|   16 | CC | aarch64 | library/cpp/getopt/small |
|   16 | CC | x86_64  | contrib/libs/mimalloc |
|   15 | CC | aarch64 | contrib/libs/zlib |
|   15 | JS | aarch64 | util |
|    9 | CC | x86_64  | contrib/tools/ragel6 |
|    4 | CC | aarch64 | contrib/libs/cxxsupp/libcxxrt |
|    4 | AS | x86_64  | contrib/libs/cxxsupp/builtins |
|    4 | CC | x86_64  | contrib/libs/cxxsupp/libcxxrt |
|  3+3 | CC | both    | contrib/libs/cxxsupp/libcxxabi-parts |
|    3 | CC | aarch64 | contrib/libs/double-conversion |
|    3 | CC | aarch64 | library/cpp/archive |
|    2 | AR | aarch64 | contrib/libs/tcmalloc/no_percpu_cache |
|    2 | CC | aarch64 | library/cpp/colorizer |
|    2 | AS | x86_64  | contrib/libs/libunwind |
|    1 | LD | aarch64 | tools/archiver (PR-34 — peer-archive inputs) |

The L2 surface is dominated by include-closure mismatches (multiset of
node.Inputs). Three sub-causes:

1. **Sysincl over-fan-out:** abseil/libcxx/libcxxrt show *extra* inputs
   like `cxxsupp/libcxx/include/uchar.h`, `musl/include/uchar.h`,
   `cxxsupp/libcxx/include/unwind.h`. These come from the include
   scanner resolving `<uchar.h>` against multiple sysincl rules
   simultaneously. Concrete fix: tighten sysincl resolution for
   non-libcxx C++ consumers (only include-fan-out the system-form
   header to ONE library, not all). Out of PR-33 scope per planner —
   a targeted sysincl-resolution tightening is its own follow-up.
2. **JS-derived CC closure missing entirely:** `util/parser.rl6.cpp.o`
   shows 1008 inputs missing — the entire generated-CC include closure.
   This is PR-31-D08 (`js.go::EmitJS` deferred to PR-32+). Out of
   PR-33 scope; PR-34 territory.
3. **Module-self -I propagation:** abseil-cpp's 156 nodes are all
   missing `-I contrib/restricted/abseil-cpp` AND the corresponding
   header inputs from the scanner. Closing Gap 2 (own GLOBAL ADDINCL
   into own cmd_args + scanner search path) closes both axes in one
   shot.

### L3 divergent pair distribution and root-cause categorization

3683 paired, 680 L3-divergent. Categorized by which "only-want" arg
class is missing (one CC node may hit multiple buckets — sums exceed
680):

| Count | Cause class | PR-33 fix |
|------:|-------------|-----------|
|  156 | abseil-self-include | D02 (own GLOBAL ADDINCL → own cmd_args) |
|  136 | -nostdinc++ peer-propagation | D01 (libcxx subtree gets libcxxrt as auto-peer) |
|  119 | libcxx/include peer-GLOBAL | D01 |
|  119 | libcxxrt/include peer-GLOBAL | D01 |
|  116 | -DLIBCXXRT (libcxx own CFLAGS) | D03 (own CFLAGS thread into ModuleCCInputs) |
|   89 | -D_musl_=1 (yasm 79 + ragel6 9 + archiver 1) | OUT — PR-32-D01 deferred |
|   77 | own CFLAGS misc (-funroll-loops, -fvisibility) | D03 |
|   71 | tcmalloc/mimalloc -DTCMALLOC_*/-DMI_* | D03 |
|   58 | cxx warning bundle (-Wimport-preprocessor-directive-pedantic etc.) | D04 (cxxStandardWarnings) |
|   33 | module-self -I (mimalloc/zlib/libc_compat/jemalloc) | D02 (and IF-evaluator extension where blocked) |
|   30 | unknown / multi-axis | partial via D01-D04 |
|   20 | -Wnarrowing util own CFLAG | D03 |
|   14 | libunwind private (-D_LIBCPP_HAS_NO_PRAGMA_SYSTEM_HEADER, etc.) | D03 |

Note that `nostdinc++ peer-propagation` (136), `libcxx/include` (119),
`libcxxrt/include` (119) all close in lockstep with D01 (the
runtimeAncestor refactor). The 116 `-DLIBCXXRT` instances are libcxx's
OWN CFLAGS — closes via D03. So D01+D02+D03+D04 should attack ~70-80%
of the 680 L3-divergent pairs directly.

### libcxx GLOBAL CXXFLAGS / CFLAGS / ADDINCL inventory

`/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/libcxx/ya.make`:

- `ADDINCL(GLOBAL contrib/libs/cxxsupp/libcxx/include  contrib/libs/cxxsupp/libcxx/src)` — GLOBAL `include`, OWN `src`. Per-path GLOBAL semantics (PR-31-D13's `splitAddInclPaths`) already handles this correctly.
- `CXXFLAGS(-D_LIBCPP_BUILDING_LIBRARY)` — own non-GLOBAL CXXFLAGS (already threaded into ModuleCCInputs.CXXFlags).
- `IF (CLANG) CXXFLAGS(GLOBAL -nostdinc++)` — own GLOBAL CXXFLAGS, propagates to consumers via existing two-phase aggregator. **Missing in libcxx's OWN cmd_args (Gap 2).**
- `IF (CXX_RT == "libcxxrt")`:
  - `ADDINCL(GLOBAL contrib/libs/cxxsupp/libcxxrt/include)` — own GLOBAL ADDINCL. **Missing in libcxx's OWN cmd_args (Gap 2).**
  - `CFLAGS(-DLIBCXXRT)` — own non-GLOBAL CFLAGS, applies to BOTH C and C++ sources. **Currently dropped because gen.go does NOT thread `d.cFlags` into ModuleCCInputs (Gap 3).**

Verified our `CXX_RT="libcxxrt"` env binding (macros.go:282).

### libcxxrt GLOBAL CXXFLAGS inventory

`/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/libcxxrt/ya.make`:

- `CXXFLAGS(-nostdinc++)` — own non-GLOBAL CXXFLAGS. Per ymake semantics applies to libcxxrt's own sources only; threaded into ModuleCCInputs.CXXFlags today and emitted on libcxxrt's own CC nodes — verified.
- No GLOBAL ADDINCL or GLOBAL CFLAGS. The `-I libcxxrt/include` on libcxx-and-consumers comes from libcxx's own `ADDINCL(GLOBAL libcxxrt/include)` (line 84 of libcxx/ya.make), not from libcxxrt itself.
- The empirical "second `-nostdinc++`" on libcxx's own CC nodes
  (slot 103 of `libcxx/_/src/algorithm.cpp.o`) appears to come from
  libcxx's GLOBAL `-nostdinc++` being aggregated as both own AND
  peer-self in upstream — under-investigated; will surface when the
  D02 fix lands. If empirical reference shows N copies, replicate N.

### Cycle behavior probe

Three probes run; all rc=0 with 7 cycles tolerated (unchanged baseline).

1. **Probe 1**: `isRuntimeAncestor` returns false for `util/*` (subtree),
   true for the other 12 literals + their subtrees. Result: util/charset
   gains 4 args (libcxx/libcxxrt -I + 2× -nostdinc++); L2/L3 unchanged
   at the comparator level (the per-pair byte-exactness still blocked
   by Gap 3+4); cycles=7.
2. **Probe 2**: `isRuntimeAncestor` returns `runtimeAncestorPaths[path]`
   only — drop the entire HasPrefix subtree extension. Result: same as
   Probe 1 but applied to ALL subtrees (musl/full, libcxxabi-parts,
   etc. now get auto-peers). L2/L3 unchanged; cycles=7. No regression.
3. **Probe 3**: drop `contrib/libs/cxxsupp/libcxx` and `contrib/libs/cxxsupp/libcxxrt` from `runtimeAncestorPaths` entirely (literal removal — they auto-peer themselves through normal `defaultPeerdirsFor` flow, gated only by their own `NO_RUNTIME()`/`NO_UTIL()` macros). Result: rc=0; cycles=7; L2/L3 unchanged. NO_RUNTIME suppression naturally prevents libcxx from auto-peering libcxxrt/libunwind/util.

**Verdict:** the cycle-prevention rationale for keeping libcxx/libcxxrt/
libunwind in `runtimeAncestorPaths` is genuine (their mutual PEERDIRs
form back-edges) but the over-inclusion is the **HasPrefix subtree
extension**, not the literal set. Recommended model in the next section.

## Design decisions

### D-1 — runtimeAncestorPaths model: **(b) asymmetric** is the recommendation

User asked: (a) remove libcxx/libcxxrt from `runtimeAncestorPaths`
entirely, (b) asymmetric (util can peer libcxx; libcxx cannot peer util,
which it doesn't anyway), (c) directed graph.

**Recommendation: drop the `HasPrefix(prefix+"/")` subtree extension
from `isRuntimeAncestor`; keep the literal map intact.** This is a
practical realization of (b): subtree members (`util/charset`,
`musl/full`, `libcxxabi-parts`) get normal auto-peering; the literal
entries (which already self-suppress via `instance.Path != "..."`
checks inside `defaultPeerdirsFor`) keep their cycle-prevention
behaviour.

Concrete: replace gen.go:623-635 body with `return
runtimeAncestorPaths[path]`. No new graph; no new directed structure.
Cycle re-test (Probe 2) confirms no regression.

Rejected alternatives:
- (a) literal-removal of libcxx/libcxxrt: empirically introduces no
  cycles (Probe 3) but also no improvement over (b) in current closure;
  (b) is more conservative and keeps the cycle insurance in place.
- (c) directed-graph: heavyweight architectural change for what reduces
  to a single-line `HasPrefix` removal; reserve for M5+ when the upstream
  `_BUILTIN_PEERDIR` evaluator lands.

### D-2 — own GLOBAL contributions emission scope

The OWN GLOBAL ADDINCL / CFLAGS / CXXFLAGS / CONLYFLAGS paths must
appear in the module's OWN compile cmd_args (in addition to consumers'
cmd_args, which already works). This is a thread-and-merge change in
gen.go:1306 — extend ModuleCCInputs to thread `d.addInclGlobal`,
`d.cFlagsGlobal`, `d.cxxFlagsGlobal`, `d.cOnlyFlagsGlobal` into the
self-emit path; cc.go's composer must dedupe so libcxx (which is its
own self-peer) does not get double `-nostdinc++` for the SAME GLOBAL
contribution. The empirical reference DOES show a second `-nostdinc++`
on libcxx's own cmd_args (one from own GLOBAL CXXFLAGS, one from peer
libcxxrt's own non-GLOBAL CXXFLAGS being implicitly promoted in the
runtime-stack — to be confirmed when the fix is applied and the
remaining single-arg gap surfaces).

### D-3 — `-nostdinc++` propagation gap (already present, blocked by D-1)

PR-32 D04-D06 wired typed CXXFlagsStmt → `d.cxxFlagsGlobal` →
`peerCXXFlagsGlobal` → `ModuleCCInputs.PeerCXXFlagsGlobal` → composer
slot. The gap is purely upstream of that wiring: util/charset doesn't
auto-peer libcxx, so libcxx's `CXXFLAGS(GLOBAL -nostdinc++)` never
reaches util/charset's PEERDIR walker. D-1 closes it.

### D-4 — PR scope: single PR, not split

The four mechanisms in this PR are tightly coupled:

- D-1 enables D-3 (peer-GLOBAL propagation via auto-peering).
- D-2 closes the "own GLOBAL not in own cmd_args" gap that affects ALL
  modules with GLOBAL declarations, not just libcxx.
- D-3 adds the cFlags-own thread (a 1-line struct extension + 1-line
  composer slot).
- D-4 (cxx-warning-bundle) is a single new flags.go bundle + a single
  new gate in `composeTargetCC` / `composeHostCC`.

Splitting them would push four sequential PRs through the loop with
small individual lifts; bundling matches the brief's "lift L2 → 90%+,
L3 → 90%+" target and respects PR-32's pattern (12 D-tasks, single PR).

Estimated combined lift (rough, decomposes by L3 cause-class table
above):
- D-1 (+D-3 propagation enabled): closes ~120-140 / 680 L3 pairs (the
  libcxx/include + libcxxrt/include + nostdinc++ overlap; many nodes
  get all three at once so the total is less than the bucket sum).
- D-2 (own GLOBAL into own cmd_args): closes ~155 / 680 (abseil) +
  some of the ~33 module-self-include subset (those NOT blocked by
  IF-evaluator gap).
- D-3 (own d.cFlags threading): closes ~116 / 680 (libcxx -DLIBCXXRT)
  + ~71 (tcmalloc/mimalloc CFLAGS) + ~20 (-Wnarrowing util) + ~77
  (-funroll-loops jemalloc etc.) — overlapping with D-1 nodes.
- D-4 (cxx warning bundle): closes ~58 / 680.

Pessimistic disjoint-union floor ≈ 350-400; optimistic with overlap
absorption ≈ 500-550. L3 lift projection: 80.51% → 89-93%. L2 lift
projection harder to pin (depends on scanner's ability to absorb the
new -I paths cleanly): 83.27% → 88-92%.

**Brief's "L2 → 90%+, L3 → 90%+" is achievable in PR-33 alone but on
the optimistic end of the range.** Real risk: the sysincl over-fan-out
(uchar.h on abseil/libcxx) is L2-only and may keep L2 short of 90%
unless the scanner's BasePaths logic gets a tightening pass alongside.
Honest projection: L2 to 88-91%, L3 to 89-92%. If sysincl tightening
slides into PR-33 (it is small in code, maybe 30-50 LOC) both lift
above 90%.

### D-5 — backward compat: cycle count & gate

Cycle count remains 7 in all probes. The PR-30 gate
`cyclesTolerated <= 14` is unchanged. No test asserts cycle == 7
exactly (verified by `grep cyclesTolerated`), so no test rewrite needed.

## Implementation plan

### D01 — runtimeAncestorPaths: drop HasPrefix subtree extension

`gen.go:623-635` `isRuntimeAncestor`. Replace body with
`return runtimeAncestorPaths[path]`. Verify no regression in
M1 byte-exact (build/cow/on still emits 2-node subgraph). Probe-confirmed
cycle count = 7. New unit test `TestIsRuntimeAncestor_LiteralOnly`
asserts `util/charset` and `musl/full` and `libcxxabi-parts` return
false; `util` and `musl` and `libcxxrt` return true.

Sub-task: revisit the `defaultPeerdirsFor` self-suppression guards at
gen.go:758, 762, 766, 772 (string equality / HasPrefix on the literal
paths). The only-`HasPrefix` for `contrib/libs/cxxsupp/libcxx/` is
covered by isRuntimeAncestor in the literal map; the per-path checks
are now redundant but keep them as defense-in-depth (PR-27-D04 lesson).

### D02 — own GLOBAL ADDINCL/CFLAGS/CXXFLAGS/CONLYFLAGS into ModuleCCInputs

Extend `ModuleCCInputs` (cc.go:79-144) with four new fields:

```go
OwnAddInclGlobal     []string
OwnCFlagsGlobal      []string
OwnCXXFlagsGlobal    []string
OwnCOnlyFlagsGlobal  []string
```

Thread from `genModule` (gen.go:1306-1317): set each from
`d.addInclGlobal`, `d.cFlagsGlobal`, `d.cxxFlagsGlobal`,
`d.cOnlyFlagsGlobal` respectively. For LibcMusl modules, leave them
empty (musl-self-isolation invariant — same as the existing
`ownCFlagsGlobal = nil` branch at gen.go:1258-1262).

Modify `composeTargetCC` / `composeHostCC` (cc.go:561-639): merge
`OwnAddInclGlobal` into the AddIncl/PeerAddInclGlobal cmd_args slot
sequence in the empirical position (after own AddIncl, before
ccIncludesSuffix — same slot as PeerAddInclGlobal). Same for the three
flags axes. Critical: dedupe between OwnAddInclGlobal and
PeerAddInclGlobal because libcxx's own `include` will appear in both
(libcxx is a peer of itself via downstream consumers' walks, and the
two-phase aggregator already includes own-from-self in its phase-1
bucket). Use a seen-set in the composer.

Tests:
- `TestEmitCC_OwnGlobalADDINCL_InOwnCmdArgs` synthetic libcxx-shaped
  module with `ADDINCL(GLOBAL include)` — assert `-I .../include`
  appears in own cmd_args, no double-include.
- `TestEmitCC_OwnGlobalCXXFlags_InOwnCmdArgs` synthetic with
  `CXXFLAGS(GLOBAL -nostdinc++)` — assert `-nostdinc++` appears in
  own cmd_args.
- Regression: `TestEmitCC_BuildCowOn_LibC_ByteExact` (M1 leaf —
  still byte-exact post-change because build/cow/on declares no
  GLOBAL contributions).
- Regression: peer-propagation tests still pass — the OwnGlobal
  threading is additive, doesn't change peer-aggregation paths.

### D03 — own non-GLOBAL CFLAGS thread into ModuleCCInputs

Extend `ModuleCCInputs` with `CFlags []string` field. Thread from
gen.go:1306: `CFlags: d.cFlags`. For musl modules drop (already nil
because muslExtraDefines bakes them in).

In `composeTargetCC` / `composeHostCC`: add `cmdArgs = append(cmdArgs,
in.CFlags...)` after `appendCxxStdAndOwn` and before the trailing
inputPath. Empirical reference position: TBD by examining one of
tcmalloc/no_percpu_cache's CC nodes — the `-DTCMALLOC_INTERNAL_256K_PAGES`
appears at slot ~99-101 typically (immediately before `-Wno-everything`
and the trailing macro/input). Pin the slot via a single byte-exact
test on tcmalloc/aligned_alloc.c.o or a similar simple C source with
own non-GLOBAL CFLAGS.

Tests:
- `TestEmitCC_OwnCFlags_AppliesToBoth_C_and_CXX` synthetic with
  `CFLAGS(-DFOO)` and one .c source + one .cpp source — both get the
  flag.
- `TestEmitCC_LibcxxAlgorithm_DLIBCXXRT_ByteExact` — pin the empirical
  slot for `-DLIBCXXRT`.

### D04 — clang C++ standard warning extensions bundle

Add to `flags.go`:

```go
// cxxStandardWarnings is the bundle ymake_conf.py:1624-1636 emits for
// every clang C++ compile when NO_COMPILER_WARNINGS is not set.
// Empirical observation: 60 reference CC nodes carry it.
var cxxStandardWarnings = []string{
    "-Wimport-preprocessor-directive-pedantic",
    "-Woverloaded-virtual",
    "-Wno-ambiguous-reversed-operator",
    "-Wno-defaulted-function-deleted",
    "-Wno-deprecated-anon-enum-enum-conversion",
    "-Wno-deprecated-enum-enum-conversion",
    "-Wno-deprecated-enum-float-conversion",
    "-Wno-deprecated-volatile",
    "-Wno-pessimizing-move",
    "-Wno-undefined-var-template",
}
```

In `composeTargetCC` / `composeHostCC`: gate `if isCxx &&
!noCompilerWarnings { append cxxStandardWarnings }` at the empirical
slot — between the warning-flags base bundle (`-Werror -Wall ...`) and
the commonDefines block. Verify slot via `util/random/random.cpp.o`
WANT cmd_args (the 11-arg burst is at slots ~22-32 immediately after
`-pipe -m64 -O3`).

Tests:
- `TestEmitCC_CxxStandardWarnings_PresentForCXX_AbsentForC` — synthetic
  C and C++ sources without NO_COMPILER_WARNINGS.
- `TestEmitCC_CxxStandardWarnings_SuppressedByNoCompilerWarnings` —
  libcxx-shaped (NO_COMPILER_WARNINGS) doesn't get the bundle.
- Pinning: `TestEmitCC_UtilRandom_ByteExact` (full 137-arg cmd_args
  matches reference).

### D05 — sysincl tightening to absorb new GLOBAL paths cleanly (defensive)

After D01+D02 land, the include scanner gains new BaseSearchPaths
entries (libcxx/include, libcxxrt/include) for util-subtree consumers.
The scanner's `<header>` resolution may now fan-out wider than
empirically observed (creating extra-input-side L2 divergence on top of
the under-input gap). Defensive: re-run `gen` + `compare --level=2`
after D01-D04 land; if abseil/libcxx see *new* extra-input divergences
(args we now emit but reference doesn't), tighten resolution by:

- Quoted `"foo.h"` includes resolve same-dir FIRST, drop on first hit.
- Angle `<foo.h>` includes resolve via OWN AddIncl FIRST, then
  PeerAddInclGlobal, then sysincl — drop on first hit.

Currently scanner.go fans out to all sysincl matches (line 380-388).
The tightening is ~15-25 LOC. Preview only; defer landing until
empirical regression observed.

### D06 — verify M1 byte-exact preservation

Re-run `TestGen_BuildCowOn_TwoNodeSubgraph_L3MatchesReference`. M1 leaf
declares no GLOBAL contributions, no own CFLAGS, NO_COMPILER_WARNINGS
default → cxx-standard-warnings off (build/cow/on is C only anyway —
isCxx=false). Expected: pass byte-exact unchanged.

### D07 — full-graph regression gate

Re-run `./yatool gen --target tools/archiver --out our.json && ./yatool
compare --level=3 our.json /yatool_orig/sg.json`. Acceptance per the
plan target:

- L0 ≥ 98.77% (preserved)
- L1 ≥ 98.74% (preserved)
- L2 ≥ 88% (target 90%; 88% acceptable if sysincl tightening not yet)
- L3 ≥ 89% (target 90%)
- Cycles ≤ 14 (expect = 7)

If L2 or L3 falls short of the target by < 2pp, document a
narrowly-scoped continuation as PR-34 candidate (likely sysincl
tightening + JS/LD scope expansion).

### D08 — defects.md PR-31-D11 closure

PR-31-D11 was deferred-pending-D12. After PR-32 closed D12 and PR-33
closes the OwnGlobal threading, the 35 of 38 AR nodes whose
member-input aggregation was incomplete should re-converge. Re-test
empirically; if any AR is still divergent, open a fresh PR-33-Dxx.

## Acceptance criteria

- M1 byte-exact preserved against sg.json: `TestGen_BuildCowOn_*` pass.
- L0 ≥ 98.77% (preserved).
- L1 ≥ 98.74% (preserved).
- **L2 ≥ 88%** (target 90%; sysincl tightening may slide into PR-34).
- **L3 ≥ 89%** (target 90%).
- Cycles ≤ 14 (preferably 7 — confirmed by Probe 2; the runtime-ancestor
  refactor itself does not change cycle count).
- All existing tests pass.
- New tests:
  - D01: `TestIsRuntimeAncestor_LiteralOnly`
  - D02: `TestEmitCC_OwnGlobalADDINCL_InOwnCmdArgs`,
    `TestEmitCC_OwnGlobalCXXFlags_InOwnCmdArgs`,
    `TestEmitCC_AbseilCasts_SelfInclude_ByteExact` (pin)
  - D03: `TestEmitCC_OwnCFlags_AppliesToBoth_C_and_CXX`,
    `TestEmitCC_LibcxxAlgorithm_DLIBCXXRT_ByteExact` (pin)
  - D04: `TestEmitCC_CxxStandardWarnings_PresentForCXX_AbsentForC`,
    `TestEmitCC_CxxStandardWarnings_SuppressedByNoCompilerWarnings`,
    `TestEmitCC_UtilRandom_ByteExact` (pin)

## Scope recommendation

**Single PR.** D01-D04 are tightly coupled — D02 (own GLOBAL into own
cmd_args) needs the threading shape D03 (struct extension); D01 (runtime-
ancestor refactor) is a 1-line change but enables peer-GLOBAL paths to
reach util-subtree which D02/D03 then use; D04 (cxx warning bundle) is
independent but small enough to bundle. ~250-400 LOC change estimate
(see below). Splitting would result in four sequential PRs each with
small individual L3 lifts and risk merge conflicts in cc.go's composers.

If reviewer expressly objects, candidate split:
- PR-33a: D01 + D04 (cycle/peering refactor + warning bundle, ~80 LOC)
- PR-33b: D02 + D03 (own-flag threading, ~250 LOC, depends on 33a)

But the recommended path is single PR-33.

## Risks and open questions

1. **The empirical "second `-nostdinc++` on libcxx's own cmd_args"
   lacks a confirmed origin.** Hypothesis: own GLOBAL CXXFLAGS is
   added once via own-self-emit (D02) and once via peer-aggregation
   (libcxx's OWN AddInclGlobal aggregation includes its own peers'
   contributions, but libcxxrt declares non-GLOBAL CXXFLAGS so it
   shouldn't propagate). Could be: upstream ymake treats CXXFLAGS
   inside `_CPP_LIBRARY` runtime modules as effectively-GLOBAL. Risk:
   if D02's dedupe is too aggressive, we lose one of the two; if not
   aggressive enough, we get duplicate args on every C++ source. Plan:
   land D02 with naive append-and-dedupe, observe libcxx's own
   cmd_args byte-exactness, refine. Failure mode: the second
   `-nostdinc++` becomes a separate PR-33-Dxx if it can't be derived
   from declarative ya.make state.

2. **Sysincl over-fan-out (`uchar.h` etc.) is L2-only and may be
   load-bearing for the 90% L2 target.** D05 sketches a defensive
   tightening; if the empirical observation post-D01-D04 shows >50
   spurious extra inputs, fold D05 in. Worst case: L2 ends at 87-88%,
   L3 at 89-90%, and sysincl tightening becomes its own PR-34 task
   alongside the JS/LD scope expansion.

3. **`-D_musl_=1` on yasm/ragel6/archiver (89 instances, PR-32-D01
   amended) is OUT of PR-33 scope but overlaps the L3 surface.** Closure
   would require evaluating `IF (MUSL) CFLAGS(-D_musl_=1)` correctly in
   yasm's host-platform walk, which today binds MUSL only on the target
   axis. Separate evaluator/binding-axis work — defer to PR-34/35.

4. **Composer slot positions for D03 (own CFLAGS) and D04 (cxx warning
   bundle) require empirical pinning** against single reference CC
   nodes before the bundle/composer changes are reviewed. The plan
   names tcmalloc/aligned_alloc.c.o and util/random.cpp.o as the
   pinning targets; reviewer should confirm those are valid empirical
   anchors.

5. **D02's OwnAddInclGlobal vs PeerAddInclGlobal dedupe is subtle**:
   libcxx's `include` is BOTH its own GLOBAL ADDINCL AND in
   PeerAddInclGlobal (because libcxx's peer-aggregation walks its peers
   which include itself indirectly via library/cpp/sanitizer/include →
   libunwind → libcxxrt → libcxxabi-parts → libcxxrt — wait, libcxx is
   not actually walked from its OWN peers; the runtime-ancestor literal
   block prevents this). Actually after D01 the literal-only check
   still keeps libcxx in the runtime-ancestor set, so libcxx itself
   never auto-peers libcxxrt — meaning libcxx's `peerAddInclGlobal` is
   genuinely empty (no double-include risk for libcxx itself). The
   dedupe matters for util/charset (consumes libcxx — gets `include`
   from peer's GLOBAL; doesn't have its own GLOBAL `include`).
   Test: `TestEmitCC_OwnGlobalADDINCL_DedupesAgainstPeer`.

6. **Cycle count gate**: PR-32 has `cyclesTolerated ≤ 14` as the
   informal cap. D01 doesn't change the count empirically; if a future
   refactor or unrelated PR brings the count higher, the gate decision
   needs revisiting.

## Out of scope (deferred)

- **PR-31-D08 (JS-derived CC include closure)**: util/parser.rl6.cpp.o
  missing 1008 inputs; still PR-34 scope.
- **PR-31-D09 (LD peer-archive paths in inputs)**: tools/archiver LD's
  34 missing peer-.a inputs + 22 missing link-cmd_args (link_exe.py
  --start-plugins, peer-archive paths); still PR-34 scope.
- **PR-32-D01 (89 `-D_musl_=1` mismatches on yasm/ragel6/archiver
  host-axis CC)**: own-CFLAGS axis but blocked by host-axis MUSL
  binding evaluator gap; not a flag-routing issue. Defer to PR-34 or
  later macro-evaluator work.
- **AS-source divergence on contrib/libs/musl x86_64 (30 nodes) and
  contrib/libs/cxxsupp/builtins x86_64 (4 nodes)**: AS-emitter member-
  input scope; separate from CC flag axes. Defer to PR-35 or later.
- **JEMALLOC own define propagation on jemalloc x86_64 (63 nodes)**: in
  the L3 table they show under "owncflags-misc" — partially closed by
  D03 (own CFLAGS thread). If 63 still fully unmatched after PR-33,
  open separate defect.

## LOC estimate

| D-task | Files touched | LOC |
|--------|---------------|-----|
| D01 | gen.go (`isRuntimeAncestor` body), gen_test.go | ~25 |
| D02 | cc.go (struct + composer), gen.go (threading), cc_test.go | ~140 |
| D03 | cc.go (struct + composer slot), gen.go (1-line thread), cc_test.go | ~60 |
| D04 | flags.go (bundle), cc.go (composer gate), cc_test.go | ~80 |
| D05 (defensive) | scanner.go | ~30 (only if needed) |
| D06+D07+D08 | tasks.md, defects.md, integration verification | n/a |

**Total: ~305 LOC additive + ~30 LOC defensive.** Comparable to
PR-32's +822/-218 line scope, on the smaller side.
