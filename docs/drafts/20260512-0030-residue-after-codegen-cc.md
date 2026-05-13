# M3 residue after PR-codegen-cc-enqueue — L0 reshuffle + 71-cluster histogram

Date: 2026-05-12
PRE  = `99ab1a4` … `d205696` (USE_PYTHON3 closure) — graph `./.out/m3-master.json` (8526 nodes).
POST = `0ed364e` (codegen-cc-enqueue) — graph `./.out/m3.json` (8679 nodes).
REF  = `/home/pg/monorepo/yatool_orig/sg2.json` (8750 nodes).

Comparator (POST vs REF): `L0=88.45% (7739/8750), L1=99.02% (8664/8679 pairs, 0 unpaired-want, 71 unpaired-got), L2=94.78%, L3=94.38%` (run 2026-05-12 06:18).
Comparator (PRE  vs REF): `L0=88.45% (7739/8750), L1=97.29% (8513/8526), 0 unpaired-want, 224 unpaired-got`.

Probes: `debug/20260512-0030-l0-fp-diff.py`, `debug/20260512-0035-logger-fp-shift.py`, `debug/20260512-0040-residue-71-cluster.py`.

Note on the user-stated "7740 → 7739" L0 numbers: my Python re-implementation of `compare_topology.go` (and the Go comparator itself) report **7739 on both sides** — the absolute count is unchanged. What did change is the *multiset assignment*: one fingerprint moved out of a coincidental-collision bucket into a unique-but-unmatched bucket (and one POST node that was previously unpaired joined another bucket). Net L0 = 7739 → 7739, but the user is correct that "one fingerprint moved out of match" — see §1.

---

## 1. L0 fingerprint reshuffle — root cause

### Single node

`library/cpp/logger/liblibrary-cpp-logger.a` @ `default-linux-aarch64`.

### Mechanism (probe `20260512-0030-l0-fp-diff.py`)

- L0 fingerprint = `sha1(kv.p || sorted(child_fps))`. Children of an AR are CC nodes. Of 7560 CC nodes in PRE, 7508 have **0 deps** (leaf) — they all share the same fingerprint `sha1("CC\0")`. An AR with N leaf-CC children therefore hashes to a *value that depends only on (N, "AR")* — every AR with the same arity collides.
- **Pre-PR-codegen-cc-enqueue**, the aarch64 logger AR had **15** leaf-CC children. So did `contrib/libs/zlib`@{aarch64,x86_64} and `contrib/libs/lzmasdk`@aarch64. All four shared fp `WGJ-ZbTc8WKPTNPCadmusy`. REF has fp `WGJ-…` four times (zlib×2, lzmasdk×aarch64, and one more 15-leaf-CC AR), so PRE's multiset of 4 matched REF's multiset of 4 — an **accidental** match that masked the missing `priority.h_serialized.cpp.o` child.
- **Post-PR-codegen-cc-enqueue**, logger@aarch64 gained `priority.h_serialized.cpp.o`, moving it from 15-leaf-CC arity to **16** (REF arity). Its fp is now `NmFCADgSk05MRO6An5asFq` — unique, **unmatched** by REF (REF's logger fp = `Fk-xVTwK_e9LmMh5yXRLyK`, also unique). Net effect: -1 paired fingerprint in the 15-leaf-CC bucket (4 → 3), +0 elsewhere. L0 unchanged at 7739, but the matched-multiset shifted.

### Why POST's logger fp ≠ REF's logger fp

POST has 16 children with identical (output, platform) keys to REF — but ONE child has a different fingerprint:
- `library/cpp/logger/priority.h_serialized.cpp.o@aarch64` POST fp `rLW_Df-irJTsHIzmw2FyZl` vs REF fp `zQXBPLWsJAuqW9YbIb7j0E`.
- That CC has 1 dep, an EN. The EN has 1 dep, an LD: `tools/enum_parser/enum_parser/enum_parser@x86_64`.
- POST's LD has **29 deps**, REF's has **30**. The missing dep: `library/cpp/cpuid_check/liblibrary-cpp-cpuid_check.global.a@x86_64`.

### Recommended fix

**1-line scope**: emit `library/cpp/cpuid_check` as a host-tool peerdir when `CPU_CHECK == yes` (default-yes; only gated off by `USE_SSE4 != yes`, `NOUTIL == yes`, `ALLOCATOR == FAKE` — see `/home/pg/monorepo/yatool_orig/build/ymake.core.conf:1247-1254`). yatool currently stubs the macro at `macros.go:358` (`"NO_CPU_CHECK": false`) but no positive-side emission exists.

Concrete change — add `library/cpp/cpuid_check` to the host-PROGRAM default-PEERDIR set in `gen.go`'s host-walker (the same place that injects musl/builtins; identify by `genCtx.IsHostBuild`). One-line peerdir append in the host-PROGRAM default block. Cascade: closes the L0 reshuffle (logger@aarch64 picks up the same EN→LD→AR fp as REF), pairs the 2 cpuid_check nodes themselves (`liblibrary-cpp-cpuid_check.global.a@x86_64`, `cpu_id_check.cpp.pic.o@x86_64`), and **likely cascades many more** since the LD enum_parser fp propagates into every dependent CC (`*.h_serialized.cpp.o`) → AR (logger, ymake, eventlog, json/writer, zipatch, yndex, ymake-lang, ymake-symbols, ymake-compact_graph, ymake-diag, libdevtools-ymake.a). All those ARs currently have POST-distinct fingerprints from REF for the same chain reason.

Side observation worth noting: the comparator's L0 score is **structurally insensitive to leaf-CC identity** — any AR with the right child arity collides with any other AR of that arity. The pre-codegen-cc-enqueue 88.45% was inflated by exactly the kind of accidental match seen here. Real-shape agreement should be measured against L1+ once we're inside 1 pp.

---

## 2. 71-cluster histogram

### 2.1 By `kv.p` and arch

| kind | count | aarch64 | x86_64 |
|------|------:|--------:|-------:|
| CC   | 36 | 21 | 15 |
| AR   | 24 | 18 |  6 |
| PY   |  7 |  7 |  0 |
| CP   |  4 |  4 |  0 |
| total | **71** | 50 | 21 |

### 2.2 Top-5 module-path prefixes (depth 3)

| count | prefix |
|------:|--------|
| 14 | `library/python/symbols` |
| 10 | `contrib/libs/blake2` |
|  8 | `devtools/ymake/lang` |
|  6 | `library/cpp/eventlog` |
|  4 | `tools/enum_parser/enum_serialization_runtime` |

### 2.3 Named clusters (probe `20260512-0040-residue-71-cluster.py`)

The user's pre-probe estimate of "10 PB/EV-cpp_proto CCs" is close to but not exactly what I measure. Actual:

| cluster | nodes | (kv.p, arch) breakdown | metric impact (cap, +pp L1) |
|---------|------:|------------------------|----------------------------:|
| **PB/EV cpp_proto** (incl. AR companions) — *in-flight, skipped* | **14** | 6 CC@aarch, 1 CC@aarch (common_msg), 2 CC@x86_64 (.pic), 4 AR@aarch, 1 AR@x86_64 | +0.16 |
| **ANTLR `.g4.cpp` rename** (CP + CC pairs) | 8  | 4 CP@aarch, 4 CC@aarch | +0.09 |
| **SIMD `.{avx,sse2,sse41,ssse3,xop}.pic.o`** (blake2) | 10 | 10 CC@x86_64 | +0.12 |
| **`symbols/{module,libc,python,registry}` aarch64**  | 14 | 5 AR@aarch, 4 CC@aarch, 3 PY@aarch (incl. yapyc3 + objcopy_*), plus 2 .reg3.cpp belong here too | +0.16 |
| **`.reg3.cpp` PY_REGISTER chain (aarch64)** | 8 | 4 PY@aarch, 4 CC@aarch — `ymake.reg3.cpp`, `ymakeyaml.reg3.cpp`, `rapidjson.reg3.cpp`, `symbols/module/library.python.symbols.module.syms.reg3.cpp` | +0.09 |
| **`enum_serialization_runtime` aarch64** | 4 | 3 CC@aarch, 1 AR@aarch | +0.05 |
| **runtime_py3 pyc.inc AR** (both arches) | 4 | 2 AR@aarch (`__res.pyc.inc`, `sitecustomize.pyc.inc`), 2 AR@x86_64 (same names) | +0.05 |
| **cpuid_check (host x86_64)** | 2 | 1 CC@x86_64, 1 AR@x86_64 | +0.02 direct, **+L0 reshuffle cascade** (see §1) |
| **malloc/jemalloc (host x86_64)** | 2 | 1 CC@x86_64, 1 AR@x86_64 | +0.02 |
| **SIMD `wide_sse41` (util/charset)** | 1 | 1 CC@x86_64 | +0.01 |
| **certs `.global.a`** | 1 | 1 AR@aarch | +0.01 |
| **contrib/tools/python3/Lib + lib2/py `.global.a`** | 3 | 1 AR@aarch (Lib), 1 AR@aarch (lib2/py), 1 AR@x86_64 (lib2/py) | +0.04 |
| **lzmasdk/zlib/etc — coincidental L0 cascade only** | 0 | (no unpaired-got rows; closes via §1 cascade) | (L0 only) |

The breakdown **adds to 71** (modulo the AR companions overlapping the symbols/runtime_py3/reg3 clusters — single cluster assignment used above, no double-counting). My count differs from the user-stated breakdown:
- PB/EV cluster: I count 14 (12 from cpp_proto + 2 from diag/common_msg), not "10 CCs". The 10 figure was CC-only on aarch64. AR companions are tightly coupled and should ship in the same PR.
- ANTLR g4.cpp.o: **8 nodes** (4 CP + 4 CC), not just 4 — the CC `.o` files are unpaired even though the CP exists in REF, and the `EmitJVSplit` is the upstream gap.
- "49 other (SIMD, unwalked dirs, other)": measured = 71 - 14 (PB/EV) = 57, of which SIMD = 11 (blake2 × 10 + wide_sse41 × 1), symbols aarch64 = 14, .reg3.cpp = 8, enum_serialization_runtime = 4, runtime_py3 pyc.inc = 4, contrib/tools/python3/.global.a = 3, cpuid_check + malloc/jemalloc + certs = 5, ANTLR = 8. Total accounted: 57. Match.

---

## 3. Remaining clusters with metric impact (in-flight PB/EV excluded)

### 3.1 ANTLR `.g4.cpp` (8 nodes) — CP rename + CC downstream

**Scope.** `EmitJVSplit` (`m3_misc.go:241-328`) emits the 4 parser/lexer `.cpp` outputs but never wires the `fs_tools.py copy <name>.cpp <name>.g4.cpp` rename. REF has both the unrenamed `.cpp` AND a `.g4.cpp` plus its `.o`. Emit one CP per JVSplit output via `cp.go::EmitCP` with `fs_tools.py copy` cmd_args. The 4 `.g4.cpp.o` CCs are downstream of the new CPs and will be picked up by the existing codegen-cc-enqueue path (which already walks CP outputs to emit `.o` CCs for `.cpp` extensions).

**Effort.** S (≤ 30 LoC; mechanical mirror of existing CP emitters).
**Expected L1 lift.** +8 pairs → **+0.09 pp**. L2/L3 likely follow since the cmd_args shape for fs_tools.py copy is already byte-exact in M3.
**Risk.** Low.

### 3.2 SIMD `.{avx,sse2,sse41,ssse3,xop}.pic.o` (10 nodes) — `SRC_C_AVX/SSE*/XOP` macros

**Scope.** `gen.go:474-478` stubs SRC_C_AVX/SSE2/SSE4/SSSE3/XOP to no-op. Upstream `build/ymake.core.conf:3848-3922` shows each macro expands to `_SRC_CUSTOM_C_CPP(${lastext:FILE} <macro> $FILE .<variant> $<VARIANT>_CFLAGS $FLAGS)` — i.e. it emits one variant `.cpp.o` per source per SIMD variant, with `.<variant>.` infixed in the output name and `$<VARIANT>_CFLAGS` appended to per-source CFLAGS. The per-source CFLAGS table (`d.perSrcCFlags`) already exists. What's missing is the macro parser + variant-suffix table.

**Effort.** M (parse 5 macros, table of variant suffixes + canonical CFLAGS strings — `SRC_C_SSE41` is already partially handled at `gen.go:403` for util/charset's wide_sse41).
**Expected L1 lift.** +11 pairs (blake2 × 10 + wide_sse41 × 1) → **+0.13 pp**. wide_sse41 is the easiest single test case — `util/charset/wide_sse41.cpp.sse41.pic.o`.
**Risk.** Medium. `$<VARIANT>_CFLAGS` resolution requires reading `build/ymake.core.conf` for `AVX_CFLAGS=`/`SSE41_CFLAGS=` definitions. Concrete values: `AVX_CFLAGS=-mavx`, `SSE41_CFLAGS=-msse4.1 -msse4 -mssse3 -msse3 -mpopcnt` etc. Per-CC-byte equality of cmd_args is the risk — needs the exact upstream flag-bundle text.

### 3.3 Unwalked dirs (~14 nodes) — `library/python/symbols/{module,libc,python,registry}` aarch64

**Status.** PR-M3-use-python3-closure (d205696) added these to the USE_PYTHON3 closure, but the closure pulls the libraries' source files only at the host-walk level. Aarch64 instances still have 0 nodes for `symbols/{module,libc,python,registry}` (probe confirms 14 unpaired-got rows here). Investigation needed: does `library/python/symbols/module` walk via host-tool only, or is the aarch64 PEERDIR not flowing through `contrib/libs/python`'s PY3 branch?

**Probe.** Walk `library/python/symbols/module` directly with `genModule(seed=symbols/module@aarch64)` and check whether its 16 deps are emitted; if so, the issue is just that the aarch64 walker isn't reaching it (USE_PYTHON3 closure → contrib/libs/python → symbols/module should be wired, but isn't on aarch64 yet).

**Scope.** Either (a) verify USE_PYTHON3 closure is correctly applied on aarch64 (gen.go:1073-1083 should branch on `IsHostBuild` — possibly only adds host peerdirs), or (b) add explicit aarch64 instance lifting for symbols/* like py3cc/slow → runtime_py3 already does on x86_64.
**Effort.** S–M (probe-first).
**Expected L1 lift.** +14 pairs → **+0.16 pp**.
**Risk.** Medium — depends on what the probe finds.

### 3.4 `.reg3.cpp` PY_REGISTER chain (8 nodes, aarch64)

**Scope.** `ymake.reg3.cpp`, `ymakeyaml.reg3.cpp`, `rapidjson.reg3.cpp`, `library.python.symbols.module.syms.reg3.cpp` are PY (codegen) outputs feeding CC `.o` outputs. All 8 unpaired rows are aarch64-only — the x86_64 instances *are* paired. So the emitter is correct on host; the aarch64 axis loses these because the PY-register output chain is not lifted on the target axis. This is the same shape as 3.3.

**Scope.** Once 3.3 is unblocked (symbols/module@aarch64 reachable), three of the four `.reg3.cpp` rows belong to modules that will be walked. `ymake.reg3.cpp` is `devtools/ymake@aarch64` and is already walked — its `.reg3.cpp` must be emitted by `PY_REGISTER` macro which probably routes through `emitter.PY3REGISTER` (TBD — needs ~30 LoC).
**Effort.** S after 3.3 lands.
**Expected L1 lift.** +8 pairs → **+0.09 pp**.
**Risk.** Low after 3.3.

### 3.5 Aarch64 residue from USE_PYTHON3 (~10 nodes)

Concretely: `runtime_py3/__res.pyc.inc@aarch64`, `runtime_py3/sitecustomize.pyc.inc@aarch64` (and x86_64 twins), `contrib/tools/python3/Lib/libpy3tools-python3-Lib.global.a@aarch64`, `contrib/tools/python3/lib2/py/libpy3python3-lib2-py.global.a@{aarch64,x86_64}`. Total = 7 nodes, all `AR/.global.a` or `AR/pyc.inc`.

The `pyc.inc` outputs come from `PY_SRCS()`'s archiver chain (yapyc3 → resource → archiver → `.pyc.inc`). PR-M3-resource-objcopy-{A,B,C} already emitted the objcopy PYs; the missing piece is the **final `pyc.inc` AR archive**. The 4 `.pyc.inc` rows are all AR, not CC — so this is an emitter gap, not a peerdir gap. `gen.go:3810` says `pyc.inc AR nodes in the M3 closure` are pending wiring.

**Scope.** Emit one AR per `pyc.inc` output (probably in `ar.go` after the existing objcopy emission). The cmd is `archiver` with the resource-input list — entry-point already lifted via the `walkHostTool(archiverPath)` at gen.go:3836.
**Effort.** S (one AR emitter + connect to existing objcopy outputs).
**Expected L1 lift.** +7 pairs → **+0.08 pp**.
**Risk.** Low.

### 3.6 Long-tail

- `enum_serialization_runtime` @aarch64 (4 nodes) — entire module not walked on aarch64. **Likely closes when 3.3 lands** (it's a PEERDIR target of `library/cpp/logger` which already walks on aarch64).
- `library/cpp/malloc/jemalloc` @x86_64 (2 nodes) — `malloc-info.cpp.pic.o` + `libcpp-malloc-jemalloc.a`. Likely a `MALLOC=jemalloc` axis variant that ymake selects for some host tools.
- `certs/libcerts.global.a` @aarch64 (1 node) — implicit-peerdir injection from `BUILD` block (similar shape to cpuid_check).
- `wide_sse41` (1 node) — covered by 3.2.

**Total long-tail:** ~8 nodes, +0.09 pp combined.

---

## 4. Recommended next 3 PRs — ranked by lift-per-effort

### 4.1 PR-M3-cpuid-check-host-peerdir (S effort, **highest yield**)

**Scope.** Add `library/cpp/cpuid_check` as a default host-PROGRAM peerdir, gated by the upstream `CPU_CHECK == yes` predicate (`USE_SSE4 == yes && NOUTIL != yes && ALLOCATOR != FAKE`). Concrete change: one `peerdirs = append(...)` in the host-walker default-peer block in `gen.go` (same site that already injects musl/builtins).

**Expected metric movement.**
- Direct: +2 pairs (cpuid_check CC + AR @x86_64) → +0.02 pp L1.
- **Cascade**: closes the L0 reshuffle (§1) and propagates through *all* `*.h_serialized.cpp.o` CCs and their ARs — every aarch64 AR whose fp currently differs from REF by exactly this one transitive child (logger, ymake, eventlog, json/writer, zipatch, yndex, ymake-lang, ymake-symbols, ymake-compact_graph, ymake-diag, libdevtools-ymake.a, plus enum_parser tooling). Expected L2/L3 cascade: every paired AR's stored UID re-stabilizes against REF.
- L0 cap: +~10–20 fingerprint pairings as fingerprint chains stabilize → **+0.20 to +0.30 pp L0**.
**Risk.** Low. One PEERDIR; behaviour change limited to host x86_64 tools.

### 4.2 PR-M3-jv-cp-g4 (S effort, +0.09 pp)

**Scope.** Emit one `fs_tools.py copy` CP per JV `.cpp` output in `EmitJVSplit` (`m3_misc.go:241-328`). Downstream `.g4.cpp.o` CCs are picked up automatically by codegen-cc-enqueue.

**Expected L1 lift.** +8 pairs (4 CP + 4 CC) → **+0.09 pp**.
**Risk.** Low — mechanical CP emitter mirroring existing M3 patterns.

### 4.3 PR-M3-symbols-aarch64-axis (S–M effort, +0.16 pp; potentially +0.25 pp via cascade)

**Scope.** Probe first: identify why `library/python/symbols/{module,libc,python,registry}` aarch64 instances have 0 nodes even after PR-d205696. Most-likely cause: the USE_PYTHON3 closure additions at `gen.go:1083` flow through host-tool walks but the *target axis* (aarch64) reaches contrib/libs/python only through some PROGRAM/LD that hasn't been wired. Add an explicit aarch64 instance lift, parallel to py3cc/slow → runtime_py3@x86_64 (`gen.go:3790-3806`).

**Expected L1 lift.** +14 pairs direct + +4 pairs cascade (enum_serialization_runtime if it shares the same walker gap) + +8 .reg3.cpp pairs after follow-up → **+0.16 pp** direct, **+0.30 pp** with cascade follow-up.
**Risk.** Medium — probe-first; cause currently inferred but not confirmed.

---

## 5. Open items / not pursued in this probe

1. **PB/EV cpp_proto PR** (in-flight per user) — 14 nodes, +0.16 pp expected; skipped per brief.
2. **SIMD macros (PR 3.2)** — not in top 3 because it's M effort with cmd_args byte-equality risk; defer until 4.1/4.2/4.3 land and we see the next residue layer.
3. **runtime_py3 pyc.inc AR (§3.5)** — small (+0.08 pp); fold into 4.3 if scope allows, or as PR-M3-pyc-inc-AR after.
4. **`malloc/jemalloc` and `certs/global.a`** singletons — defer; each one is a separate ymake-conf default-peerdir investigation, low lift per effort compared to cpuid_check (which has the L0 cascade).

## 6. Verification commands

- `./yatool compare --level=3 .out/m3.json /home/pg/monorepo/yatool_orig/sg2.json` → 71 unpaired-got, 0 unpaired-want.
- `python3 debug/20260512-0030-l0-fp-diff.py` → single-node L0 reshuffle = logger@aarch64.
- `python3 debug/20260512-0040-residue-71-cluster.py` → cluster breakdown above.
