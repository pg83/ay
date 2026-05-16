# M3 residue after PR-C (PR-M3-residue-B) — read-only probe

Date: 2026-05-12
Generator output: `./.out/m3.json` (freshly regenerated; `./yatool make -j 0 -G devtools/ymake/bin > ./.out/m3.json`, wall **4.5 s**).
Reference: `/home/pg/monorepo/yatool_orig/sg2.json` (8 750 nodes).
Comparator: `./yatool compare --level=3 ./.out/m3.json sg2.json` →
`L0 = 88.45%, L1 = 97.43%, L2 = 94.13%, L3 = 92.91%; pairs = 8 538, unpaired-want = 117, unpaired-got = 212`.

Probe scripts: `debug/20260512-0000-residue-probe.py`, `debug/20260512-0010-runtime-py3-peers.py`, `debug/20260512-0020-residue-detail.py`.

This document answers two questions:
(A) why the aarch64 peer-walk for `devtools/ymake/bin` does not reach `library/python/runtime_py3` and `library/python/symbols/module`;
(B) what the residual 212 unpaired-got and 117 unpaired-want decompose into after PR-M3-residue-B closed 11 over-emissions (the pre-PR-A baseline was 223 / 0).

---

## 1. Aarch64 peer-walk gap — root cause

### 1.1 Observation

OUR `library/python/runtime_py3` aarch64 instance: **0 nodes**. OUR `library/python/symbols/module` aarch64 instance: **0 nodes** (and zero on x86_64 too). REF has both modules fully built on aarch64 (10 runtime_py3 nodes + 8 symbols/module nodes). REF's `devtools/ymake/bin/ymake` aarch64 LD has two direct deps on the aarch64 `library/python/runtime_py3` archive pair (`libpy3library-python-runtime_py3.a` + `.global.a`), verified in `debug/20260512-0010-runtime-py3-peers.py`.

OUR x86_64 instance of runtime_py3 *does* exist (14 nodes) — it is reached through the host-tool walk into `tools/py3cc/slow/bin`, which explicitly PEERDIRs `library/python/runtime_py3` (verified: `/home/pg/monorepo/yatool_orig/tools/py3cc/slow/bin/ya.make:6-9`). So the walker can construct the module — it is simply not reached on the target axis.

### 1.2 Root cause

The implicit-peerdir hardcode for `USE_PYTHON3()` at `gen.go:1073-1083` is **wrong**. It adds:

```go
case "USE_PYTHON3":
    d.peerdirs = append(d.peerdirs, "contrib/tools/python3", "contrib/tools/python3/Lib")
```

The upstream macro at `/home/pg/monorepo/yatool_orig/build/conf/python.conf:1063-1071` is

```
macro USE_PYTHON3() {
    _ARCADIA_PYTHON3_ADDINCL()
    SET(PEERDIR_TAGS PY3 PY3_BIN_LIB ...)
    PEERDIR(contrib/libs/python)               # <-- not contrib/tools/python3
    when ($USE_ARCADIA_PYTHON == "yes") {
        PEERDIR+=library/python/runtime_py3    # <-- missed entirely
    }
}
```

`contrib/libs/python` is a `PY23_LIBRARY` (`/home/pg/monorepo/yatool_orig/contrib/libs/python/ya.make:1`) that PEERDIRs (PY3 branch):

```
library/python/symbols/module
library/python/symbols/libc
library/python/symbols/python
library/python/runtime_py3
contrib/tools/python3
contrib/tools/python3/Lib
```

OUR hardcode therefore omits four target-axis peers: `library/python/runtime_py3`, `library/python/symbols/{module,libc,python}`. On the host-tool sub-walk (py3cc/slow → runtime_py3 via that module's own PEERDIR) we still reach runtime_py3, but only as an x86_64 instance — devtools/ymake/bin is aarch64, and its PEERDIR closure never lists runtime_py3 because USE_PYTHON3 hardcodes the wrong set. No `DefaultIfEnv` gating is at fault; no module-instance keying is at fault; the walker is correct given an incomplete input.

### 1.3 Recommended fix

**Single-change fix (M effort).** Extend the hardcoded list at `gen.go:1083`:

```go
d.peerdirs = append(d.peerdirs,
    "contrib/tools/python3",
    "contrib/tools/python3/Lib",
    "library/python/runtime_py3",
    "library/python/symbols/module",
    "library/python/symbols/libc",
    "library/python/symbols/python",
)
```

This mirrors the empirical `contrib/libs/python` PY3-branch closure rather than going through one more layer of fake-conf indirection. The structural alternative (parse `contrib/libs/python/ya.make`, evaluate its IF/MODULE_TAG branches, walk transitively) is correctly factored but materially larger work and is not unique to USE_PYTHON3 — `contrib/libs/python` is the only PY23_LIBRARY in the M3 closure. Hard-coding the closure is exactly what `gen.go:1073-1083` already does for the simpler half; extending the same list is consistent with the existing pattern.

Caveat: `contrib/libs/python` and `library/python/symbols/*` are themselves `PY23_LIBRARY`/`PY3_LIBRARY`, which OUR walker already accepts via `isPyLibraryType`. They each emit small graphs and have no host-only quirks. Their objcopy PY nodes are already emitted on x86_64 instances of similar modules (verified by the existing emitter wiring), so the same emission path applies on aarch64 once they are reached.

---

## 2. Unpaired-want (117) triage

All 117 unpaired-want are `kv.p = PY` with output paths matching:

| sub-bucket | count |
|------------|------:|
| `contrib/tools/python3/lib2/py/objcopy_*.o` | 78 (39 × aarch64 + 39 × x86_64) |
| `contrib/tools/python3/Lib/objcopy_*.o`     | 39 (aarch64 only) |
| total | **117** |

Probe verification (`debug/20260512-0020-residue-detail.py`):
- 0 of 117 are *not* `objcopy_*.o`.
- 0 of 117 are *not* under `contrib/tools/python3`.

These are pure chunker over-emissions: OUR chunker produces 39 + 39 + 39 = 117 distinct hashes that REF does not emit, while simultaneously *under*-emitting on the same module (the unpaired-got side has the matching 112 = 75 aarch64 + 37 x86_64 hashes REF *does* emit). The PR-chunker-precision change tracked separately reconciles both sides — it is purely a chunk-boundary / hashing-input discrepancy, not a missing emitter or a missing peer.

Confidence: **high** that all 117 close when PR-chunker-precision lands. No other source identified.

---

## 3. Unpaired-got (212) cluster — re-histogram

### 3.1 Histogram by `kv.p`

| kind | count | % of 212 |
|------|------:|---------:|
| PY   | 121 | 57.1% |
| CC   |  60 | 28.3% |
| AR   |  26 | 12.3% |
| CP   |   4 |  1.9% |
| PR   |   1 |  0.5% |

Cross of (kind, arch):

| (kind, arch) | count |
|--------------|------:|
| (PY, aarch64) | 84 |
| (CC, aarch64) | 45 |
| (PY, x86_64)  | 37 |
| (AR, aarch64) | 20 |
| (CC, x86_64)  | 15 |
| (AR, x86_64)  |  6 |
| (CP, aarch64) |  4 |
| (PR, aarch64) |  1 |

### 3.2 Top module-path prefixes (top 5)

| count | prefix |
|------:|--------|
| 115 | `contrib/tools/python3` (Lib + lib2/py objcopy chunker counterparts) |
|  14 | `library/python/symbols` |
|  12 | `library/python/runtime_py3` |
|  11 | `devtools/ymake/lang`   (CP g4.cpp + downstream CC) |
|  10 | `contrib/libs/blake2`   (SIMD variants) |

### 3.3 Named clusters

| cluster | nodes | named by |
|---------|------:|----------|
| **A1 — chunker counterparts (contrib/tools/python3)** | 112 | `kv.p=PY`, output `objcopy_*.o` under Lib/lib2py; mirror of the 117 unpaired-want; closes with **PR-chunker-precision** |
| **A2 — runtime_py3 aarch64 closure**                 |  10 | AR + CC + PY + PR for runtime_py3@aarch64; closed by USE_PYTHON3 fix (§1.3) |
| **A3 — symbols/module aarch64 closure**              |   8 | PY + AR + CC for symbols/module@aarch64; closed by USE_PYTHON3 fix (§1.3) |
| **B1 — codegen-product downstream CC**               |  32 | EN/EV/PB/JV-emitted `.cpp` whose *.o is never emitted (deferred to codegen-cc-enqueue PR) |
| **B2 — SIMD-variant CC (blake2)**                    |  10 | `blake2[bs].c.{avx,sse2,sse41,ssse3,xop}.pic.o`; no `SRC_C_AVX/SSE41/...` macro support |
| **C  — AR (global + cpp_proto aarch64)**             |   9 | `.global.a` archives + 5 `cpp_proto` AR on aarch64 (PROTO_LIBRARY axis pruning) |
| **D  — CP g4.cpp rename (ANTLR)**                    |   4 | `fs_tools.py copy` step missing on JV outputs |
| **UNCLUSTERED tail**                                 |  27 | see §3.4 |

The 27 unclustered nodes split by module-dir:

| count | module_dir | content |
|------:|------------|---------|
|  6 | `library/cpp/eventlog/proto`         | both arches; `pb.cc.o` + AR — downstream of codegen-cc-enqueue + cpp_proto AR axis |
|  4 | `tools/enum_parser/enum_serialization_runtime` | aarch64-only `*.cpp.o` + AR; OUR misses on aarch64 |
|  2 | `library/python/runtime_py3` (x86_64 AR of `.pyc.inc`) | downstream of pyc.inc emitter |
|  2 | `library/cpp/protobuf/util/proto`    | aarch64 PB/AR — cpp_proto axis |
|  2 | `library/cpp/malloc/jemalloc` (x86_64) | `malloc-info.cpp.pic.o` + AR |
|  2 | `library/cpp/protobuf/json/proto`    | aarch64 PB/AR — cpp_proto axis |
|  2 | `library/cpp/retry/protos`           | aarch64 PB/AR — cpp_proto axis |
|  4 | misc singletons: `library/python/symbols/registry` AR, `devtools/ymake{,/libs/ymakeyaml,/contrib/python-rapidjson}` `.reg3.cpp`, `library/cpp/cpuid_check`, `util/charset/wide_sse41.cpp.sse41.pic.o`, `devtools/ymake/diag/common_msg` AR | per-row separate causes |

### 3.4 Effort estimates and metric impact (upper bounds; assumes byte-equality follows once a node is paired)

| cluster | nodes | effort | direct L1 lift (cap) | notes |
|---------|------:|:------:|--------------------:|-------|
| A1 chunker counterparts | 112 | tracked separately (PR-chunker-precision) | +1.28 pp | both sides close together with the 117 unpaired-want |
| A2 + A3 runtime_py3 + symbols/module aarch64 | 18 | **S** (one-line peer hardcode + sanity test) | +0.21 pp | unlocks two more dirs on aarch64; cascade may add 2-4 nodes |
| B1 codegen-cc-enqueue | 32 | M | +0.37 pp | already scoped: PR `codegen-cc-enqueue` |
| B2 SIMD-variant macros | 10 | M | +0.11 pp | needs `SRC_C_AVX/SSE2/SSE41/SSSE3/XOP` parser + per-variant CFLAGS table |
| C  AR aarch64 cpp_proto | 5 | S | +0.06 pp | one peer-walk axis fix in `emitProtoSrcs`; rest of C closes with A2 |
| D  CP g4.cpp | 4 | S | +0.05 pp | emit one `fs_tools.py copy` per JV `.cpp` output; unlocks 4 of B1's CC nodes |
| UNCLUSTERED tail | 27 | mixed S/M | +0.31 pp | per-row triage; largest sub-row is `library/cpp/eventlog/proto` (6) which falls under B1+C |

Upper-bound aggregate of (A1 + A2 + A3 + B1 + B2 + C + D + tail): `+2.39 pp` on L1 (213/8538). Note this is a pair-add ceiling; L2/L3 lift depends on byte-exactness of the new nodes' kv/inputs/cmd_args.

### 3.5 What shifted since the pre-PR-A 223 → 212 baseline

PR-M3-residue-B closed 11 over-emission OUR-only nodes (committed at `99ab1a4`). Net effect on unpaired-got was unchanged (-1 from clusters resolving, +0 added; the PR's primary diff was the matching unpaired-want side). The 212 number reflects:

- A1 chunker counterparts increased from 127 → 112 (PR-C reduced the chunker's REF-side spread; the residual 112 is what the PR-chunker-precision pass will reconcile alongside its mirror set on unpaired-want).
- A2/A3 unchanged (root cause is USE_PYTHON3 hardcode, not touched by any landed PR).
- D unchanged (4 g4.cpp CP).
- Modest movement in UNCLUSTERED — `library/cpp/eventlog/proto` and `tools/enum_parser/enum_serialization_runtime` now surface clearly as separate residue rows (they were previously folded into B1).

---

## 4. Recommended next 3 PRs — ranked by lift-per-effort

After PR-chunker-precision and PR-codegen-cc-enqueue (both already scoped) land, the highest yield-per-effort sequence is:

### 4.1 PR-M3-use-python3-closure (S effort, ~+0.21 pp L1)

**Scope.** Extend `gen.go:1083` USE_PYTHON3 hardcode to add four peers:
- `library/python/runtime_py3`
- `library/python/symbols/module`
- `library/python/symbols/libc`
- `library/python/symbols/python`

Update the function comment to point at `build/conf/python.conf:1063-1071` (the upstream `USE_PYTHON3` macro) and `contrib/libs/python/ya.make:23-26,47` (the transitively-included peers). Add a `genCtx` sanity test that runtime_py3 is reached on the aarch64 axis with one `genModule(seed=devtools/ymake/bin@aarch64)` call.

**Expected metric movement (upper bound).** +18 pairs direct → +0.21 pp on L0/L1; some L2/L3 lift contingent on cmd_args byte-equality of the new aarch64 instances (the x86_64 twins already pair, so the byte-equality risk is low).

**Risk.** Low. The four added peers are not new module types (already supported by `isPyLibraryType`). The only risk is a transitive cycle through `contrib/libs/python` if that module enters the walker — guarded by the existing `runtimeAncestorPaths` set and the host-tool walker memoisation. None of the four peers should themselves recurse into `contrib/libs/python` (verified by their ya.make: runtime_py3 PEERDIRs library/cpp/resource + contrib/tools/python3; symbols/module PEERDIRs only library/python/symbols/registry).

### 4.2 PR-M3-cpp-proto-aarch64-axis (S effort, ~+0.06 pp L1)

**Scope.** `emitProtoSrcs` currently emits the `cpp_proto` AR archive on x86_64 instances but not on aarch64 for five PROTO_LIBRARY modules (`library/cpp/protobuf/{json,util}/proto`, `library/cpp/retry/protos`, `library/cpp/eventlog/proto`, `devtools/ymake/diag/common_msg`). Trace the axis-guard in `emitProtoSrcs` — likely a `targetIsX8664(instance)` guard intended for host-only PB invocation that has been extended too far down the AR emission path.

**Expected metric movement.** +5 to +9 pair adds (5 AR + 0–4 downstream CC), +0.06 to +0.11 pp.

**Risk.** Low — one source file, behaviour change scoped to known PROTO_LIBRARY closures, byte-equality witness already in REF.

### 4.3 PR-M3-jv-cp-g4 (S effort, ~+0.05 pp direct, +0.05 pp via B1 unlock)

**Scope.** `m3_misc.go:241-328` `EmitJVSplit` declares the four lexer/parser `.cpp` outputs but never emits the post-step `fs_tools.py copy <name>.cpp <name>.g4.cpp` CP. Emit one CP per JVSplit output `.cpp` via `cp.go::EmitCP` with the established `fs_tools.py copy` cmd_args shape (no Go references to `g4.cpp` currently exist — `grep -c g4.cpp .out/m3.json` = 0, `grep -c g4.cpp sg2.json` = 32). Unlocks 4 downstream CC nodes that fall under B1 (`{CmdLexer,CmdParser,TConfLexer,TConfParser}.g4.cpp.o`).

**Expected metric movement.** +4 CP direct + +4 CC downstream after PR-codegen-cc-enqueue → +0.09 pp combined.

**Risk.** Low — the four files are visible in REF and the CP shape is mechanical.

---

## 5. Open questions

1. **`contrib/libs/python` as a real walker target.** The recommended fix (§1.3) hard-codes the four extra peers. If a fifth USE_PYTHON3 caller outside the current M3 closure ever lands, or if `contrib/libs/python` itself becomes reachable as a regular module, the hardcoded list will drift from the upstream macro. Question: is there a planned M4/M5 PR that parses build/conf macros at a higher fidelity (full conf-macro evaluation), or do we accept the hardcode as the long-term shape? The current code at gen.go:1083 already accepts the hardcode pattern, so this is a forward-compat question, not a blocker.

2. **`B2` SIMD macro support.** `grep -n 'SRC_C_AVX\|SSE41\|XOP' *.go` → zero matches. Are `SRC_C_AVX`/`SRC_C_SSE41`/etc. emitted by upstream as separate macro statements (parser change required), or are they desugared upstream into `SRC(filename -mavx)` style invocations (per-source CFLAGS table — already supported via `d.perSrcCFlags`)? An upstream-conf check would settle this before pricing B2 effort precisely.

3. **`UNCLUSTERED` tail rows.** Five module-dirs each contribute 2 nodes (one cpp_proto + one AR pair); they are unblocked by PR §4.2. The non-proto rows (`library/cpp/cpuid_check`, `library/cpp/malloc/jemalloc`, `util/charset/wide_sse41.cpp.sse41.pic.o`, `tools/enum_parser/enum_serialization_runtime`) need their own short triage — each looks like a one-module-dir-not-walked case, but the cause may be a missing PEERDIR injection (cpuid_check is similar in shape to malloc/api), an axis-pruning issue (enum_serialization_runtime), or per-source CFLAGS (wide_sse41 = B2). Not a blocker for the next 3 PRs; worth a one-page note before pricing them.
