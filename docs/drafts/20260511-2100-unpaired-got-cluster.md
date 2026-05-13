# Unpaired-got cluster analysis — 223 nodes REF has and OUR misses

Date: 2026-05-11
Generator output: `./.out/m3.json` (regenerated; `./yatool gen --target devtools/ymake/bin --out ./.out/m3.json`, wall 4.5s).
Reference: `/home/pg/monorepo/yatool_orig/sg2.json` (8750 nodes).
Comparator: `./yatool compare --level=3 ./.out/m3.json /home/pg/monorepo/yatool_orig/sg2.json` →
`L0=88.45%, L1=97.30%, L2=93.95%, L3=92.79%; 8527 pairs / 0 unpaired-want / 223 unpaired-got`.

Probe scripts: `debug/20260511-2100-{cluster-unpaired,drill-clusters,aarch64-probe,source-link,objcopy-py,visit-check,cascade}.py`.

`unpaired-got` here = REF nodes whose `(outputs[0], platform)` pairing key has no match in OUR (`want=./.out/m3.json`, `got=sg2.json` per `main.go:265-266`; the comparator labels the second arg "got").

---

## 1. Histogram by `kv.p`

| kind | count | % of 223 |
|------|------:|---------:|
| **PY**   | **132** | **59.2%** |
| **CC**   | **60**  | **26.9%** |
| **AR**   | **26**  | **11.7%** |
| CP   | 4   | 1.8% |
| PR   | 1   | 0.4% |

Three kinds (PY+CC+AR) account for **218 of 223 nodes (97.8%)**. The CP and PR tails are concentrated in two specific upstream toolchains.

Platform breakdown (probe `aarch64-probe.py`):
- aarch64-only: 159 (71.3%) — `CC=45, AR=20, PY=89, CP=4, PR=1`.
- x86_64-only:   64 (28.7%) — `CC=15, AR=6,  PY=43`.

Aarch64 over-representation traces to (a) `library/python/runtime_py3` artefacts that REF builds for aarch64 only, and (b) ANTLR/CP outputs we lack entirely. The x86_64-only set is dominated by `contrib/libs/blake2/src` SIMD permutations (10/15) and `contrib/tools/python3/lib2/py` objcopy outputs (≥40).

---

## 2. Top clusters

### Cluster A — PY objcopy_*.o (RESOURCE embedding) — 127 nodes

**Size:** 127 of 132 PY-cluster nodes (57.0% of the 223 total).
**Representative:** `$(BUILD_ROOT)/certs/objcopy_c27c99b2d9d5eade92fd72d0aa.o`, `platform=default-linux-aarch64`.
**kv:** `{p: PY, pc: yellow, show_out: yes}`.
**target_properties:** `{module_dir: certs}`.
**cmd_args[0:8]:** `[/ix/realm/pg/bin/python3, $(SOURCE_ROOT)/build/scripts/objcopy.py, --compiler, /ix/realm/boot/bin/clang++, --objcopy, /ix/realm/boot/bin/llvm-objcopy, --compressor, $(BUILD_ROOT)/tools/rescompressor/rescompressor]`.

**Why we're not emitting it (hypothesis):** explicit deferral.
- `py.go:19-21` documents this exact shape: *"show_out / objcopy shape (127 nodes, show_out=yes): RESOURCE-based Python embedding; emitted by build/scripts/objcopy.py. Deferred to a later PR — not handled here."*
- `gen.go:3645-3650` walks `tools/rescompiler/bin`, `tools/rescompressor/bin`, `tools/archiver` as host tools, but the comment explicitly says: *"LD NodeRefs are not yet wired into the yapyc3 PY nodes emitted below (that wiring is deferred to a later PR when the full objcopy PY emitter lands)."*
- `gen.go:424` whitelists `RESOURCE` but the comment is: *"Embeds binary resources; PR-M3-A defers PY node emission."*
- `gen.go:425` likewise for `RESOURCE_FILES`.

**Evidence:** `grep -c "objcopy_" .out/m3.json /home/pg/monorepo/yatool_orig/sg2.json` → OUR=0 nodes anywhere, REF has 127. `grep "objcopy" *.go` returns only `py.go` and `gen.go` references in **comments and host-walks**, never an emitter.

**Owning module-dirs (REF):**
- `contrib/tools/python3/lib2/py` — 76 nodes (aarch64+x86_64)
- `contrib/tools/python3/Lib` — 40 nodes (aarch64 only)
- `library/python/runtime_py3` — 4
- `library/python/symbols/module` — 4
- `tools/py3cc/slow` — 3 (x86_64 only)
- `devtools/ymake/contrib/python-rapidjson` — 2 (aarch64 only)
- `certs`, `devtools/ymake`, `devtools/ymake/libs/ymakeyaml` — 1 each.

**Effort:** **M** (one new emitter that mirrors `emitPySrcs` shape but emits the objcopy invocation per `RESOURCE()`/`RESOURCE_FILES()` entry; cmd_args are stable, env is fixed, inputs come from `tools/rescompiler` + `tools/rescompressor` + the resource source files; we already host-walk both binaries at `gen.go:3671-3673`).

**Metric lift (upper bound, this cluster alone):**
- Adds 127 paired nodes to a denom of 8750 → L1 lift ≈ `127 / 8750 = +1.45 pp` (ceiling).
- Similar magnitude for L0 (these are unique fingerprints — none are present in OUR), L2, L3.
- Cascade analysis (`cascade.py`): only 2/223 unpaired-got outputs are already referenced from OUR inputs → adding emitters is **additive**, not corrective for existing L2 drift.

---

### Cluster B — CC of generated `.cpp` sources — 60 nodes

**Size:** 60 (26.9%).
**Representative:** `$(BUILD_ROOT)/library/cpp/cpuid_check/cpu_id_check.cpp.pic.o`, `platform=default-linux-x86_64`, `kv={p:CC, pc:green}`.

This cluster is **not homogeneous**. Sub-breakdown by source-input character (`source-link.py`):

**B.1 (31 nodes): downstream CC of generated sources we DO emit upstream.**
The CC's source-input (`inputs[0]`) is itself an output of some other node we emit, but we don't add the downstream `.cpp.o`. Examples:
- `$(BUILD_ROOT)/devtools/ymake/config/config.h_serialized.cpp.o` ← we emit the `.h_serialized.cpp` (EN node), but no follow-up CC.
- `$(BUILD_ROOT)/devtools/ymake/diag/common_msg/msg.ev.pb.cc.o` ← we emit the `.ev.pb.cc` (EV node), no follow-up CC.
- `$(BUILD_ROOT)/devtools/ymake/lang/CmdLexer.g4.cpp.o` ← depends on Cluster D's missing `.g4.cpp` (so this row chains downstream of Cluster D).

Root cause: the generator emits the codegen *.cpp* product into the CodegenRegistry (PR-M3-F-7) but does not enqueue it as a synthetic SRCS member of the owning module. The `emitOneSource` switch at `gen.go:4023-4046` only fires for sources discovered by the YA.MAKE walker, not for build-tree-rooted outputs.

**B.2 (10 nodes): SIMD-permutation CCs we don't fan out.** All `contrib/libs/blake2/src` (x86_64): `blake2[bs].c.{avx,sse2,sse41,ssse3,xop}.pic.o` — REF compiles each `.c` source 5 times with different ISA flags; OUR emits the single non-suffixed `.c.pic.o` only. Root cause: SRC_C_PIC + SIMD-variant macros (`SRC_C_AVX`, `SRC_C_SSE41`, …) handler not implemented. Find: `grep -n "SRC_C_AVX\|SSE41\|SIMD" *.go` returns nothing.

**B.3 (~14 nodes): CCs of sources we never produce.** `runtime_py3/__res.cpp.o`, `library/python/symbols/{libc,module,python,registry}/syms.cpp.{o,py3.o}`, `cpu_id_check.cpp.pic.o`, `malloc-info.cpp.pic.o`, `wide_sse41.cpp.sse41.pic.o`. These are real on-disk sources in dirs OUR walker doesn't visit (no `module_dir` for them in OUR per `visit-check.py`: 13 of 60 owning-dirs ABSENT from OUR).

**Effort:**
- B.1: **M** — extend the codegen-product → synthetic-SRCS loop in `gen.go:2692-2766` to enumerate codegen outputs and feed them through `emitOneSource`. 31 nodes.
- B.2: **M** — implement `SRC_C_{AVX,SSE2,SSE41,SSSE3,XOP}` macros + their per-source CFLAGS. 10 nodes.
- B.3: **L** — walker integration for several `library/python/symbols/*` modules; non-trivial because they multi-module on `py3` / `py3_global` / `py3_native_global` tags.

**Metric lift:** B.1+B.2 together = 41 paired nodes ≈ +0.47 pp on L1/L0/L2/L3. B.3 is downstream of Cluster A's resolution.

---

### Cluster C — AR of unwalked or codegen-tagged modules — 26 nodes

**Size:** 26 (11.7%).
**Representative:** `$(BUILD_ROOT)/certs/libcerts.global.a`, `platform=default-linux-aarch64`, `kv={p:AR, pc:light-red, show_out:yes}`, `target_properties.module_tag=global, module_dir=certs`.

The 26 AR nodes split by tag:
- **11× `lib*.global.a`** — `.global.a` archives for modules with GLOBAL_SRCS. OUR-AR-coverage for those module_dirs (`visit-check.py`): mostly absent (we never emit AR there).
- **15× `lib*.a`** (regular archives) — e.g. `library/cpp/protobuf/json/proto`, `library/cpp/protobuf/util/proto`, `library/cpp/retry/protos` (all aarch64-side; we emit them on x86_64 but not aarch64 for these PROTO_LIBRARYs).

Notable subset (5 nodes): `library/python/runtime_py3/{__res.pyc.inc, sitecustomize.pyc.inc}` × 2 platforms + the host PR + the LIBRARY .a. These are downstream of Cluster E (the `.pyc` → `.pyc.inc` chain).

Hypothesis cocktail:
- 11 of 26: missing module-walk for that `(module_dir, platform)` combination because the module is a downstream peer of Cluster A's objcopy-only modules — once Cluster A emits, these dirs will be reachable and AR will follow.
- ~5 of 26: PROTO_LIBRARY `cpp_proto` AR on aarch64 missing while x86_64 is present (`devtools/ymake/diag/common_msg`, 3 `library/cpp/*/proto`, 1 `library/cpp/retry/protos`). Root cause unclear — likely aarch64-side peer-walk pruning we can dig into separately.

**Effort:** **S** for the second group (one peer-walk fix); the first group resolves automatically when Cluster A lands.
**Metric lift:** ~+0.30 pp.

---

### Cluster D — CP (fs_tools.py copy) for ANTLR `.cpp` → `.g4.cpp` — 4 nodes

**Size:** 4 (all aarch64).
**Outputs:** `$(BUILD_ROOT)/devtools/ymake/lang/{CmdLexer,CmdParser,TConfLexer,TConfParser}.g4.cpp`.
**cmd_args[0:5]:** `[python3, $(SOURCE_ROOT)/build/scripts/fs_tools.py, copy, $(BUILD_ROOT)/.../CmdLexer.cpp, $(BUILD_ROOT)/.../CmdLexer.g4.cpp]`.

Hypothesis: post-ANTLR file-rename step. `EmitJVSplit` at `m3_misc.go:241-328` declares lexer/parser `.cpp` outputs but never emits the trailing CP that renames each `.cpp` → `.g4.cpp`. Evidence: `grep -c "g4.cpp" .out/m3.json` → 0, `grep -c "g4.cpp" sg2.json` → 32; no Go reference to `g4.cpp` exists at all.

Downstream cost: 4 CC nodes in Cluster B.1 (`CmdLexer.g4.cpp.o`, `CmdParser.g4.cpp.o`, `TConfLexer.g4.cpp.o`, `TConfParser.g4.cpp.o`) depend on these CP outputs → fixing Cluster D unlocks 4 CC pairs.

**Effort:** **S** — emit one CP per JVSplit output `.cpp` (reuse `cp.go`'s `EmitCP`).
**Metric lift:** +0.05 pp direct, +0.05 pp via unlocked B.1 follow-ups.

---

## 3. Long-tail (clusters < 10)

- **PY `.reg3.cpp` (4 nodes)**: `gen_py3_reg.py` invocations producing `rapidjson.reg3.cpp`, `ymakeyaml.reg3.cpp`, `ymake.reg3.cpp`, plus one `*.syms.reg3.cpp`. cmd_args[0:4] = `[python3, gen_py3_reg.py, <module-name>, <out-path>]`. No Go reference. Effort **S**; downstream of Cluster A modules.
- **PY `__init__.py.yapyc3` (1 node)** in `library/python/symbols/module`. Standard yapyc3 shape but in a multi-module dir we don't walk on aarch64.
- **PR `__res.pyc` via `stage0pycc` (1 node)**: `cmd_args[0:6]=[$(BUILD_ROOT)/library/python/runtime_py3/stage0pycc/stage0pycc, mod=..., $(SOURCE_ROOT)/...py, $(BUILD_ROOT)/...pyc, ...]`. Zero Go references to `stage0pycc`. Effort **M** (new host-tool walk + new `EmitPR` emitter). Singleton.

---

## 4. Recommended next PR

**PR-M3-resource-objcopy: implement the RESOURCE/objcopy PY emitter.**

**Scope:**
1. Parse `RESOURCE(name1 key1 ... nameN keyN)` and `RESOURCE_FILES([PREFIX p] [STRIP s] file1 ... fileN)` macros (already whitelisted at `gen.go:424-425`).
2. Per RESOURCE entry, emit one PY node with the `objcopy.py` invocation shape documented in `py.go:19-21` and observable in the representative drilled out by `drill-clusters.py`.
3. Wire `tools/rescompiler/bin` and `tools/rescompressor/bin` LD refs (already host-walked at `gen.go:3671-3673`) into each PY node's `DepRefs`.
4. Compute the deterministic content-addressed suffix on the output: `objcopy_<24-hex>.o`. Source of the hash is empirically the (key, content?) tuple — needs one experiment against REF to pin (probably keyed by sorted `name+key` pairs and one resource file path).

**Expected metric movement (upper bound, before cascade):**
- L0: +1.45 pp (88.45% → ~89.90%)
- L1: +1.45 pp (97.30% → ~98.75%)
- L2: +1.45 pp (93.95% → ~95.40%) — but probably less because new nodes have rich `inputs[]` that need to match REF byte-for-byte.
- L3: +1.45 pp (92.79% → ~94.24%) — same caveat: cmd_args byte-equality needed.

If we also wrap in Cluster D (4 CP nodes, Effort S, ~+0.10 pp combined direct+unlocked) the PR could deliver ~+1.50–1.55 pp across the four levels for ~one engineer-day of work.

**Why not start with Cluster B.1?** It looks larger by node-count (31) but the codegen-product → CC chain needs a per-source `IncludeInputs` rescan against a synthetic source path that doesn't exist on disk (`scanIncludesForSource` at `gen.go:4034` reads from disk). That's a bigger surface area than the objcopy emitter, which is mechanical.

---

## 5. Open questions

1. **objcopy_*.o hash derivation.** The 24-hex suffix in `objcopy_c27c99b2d9d5eade92fd72d0aa.o` must be deterministic from `(resource-key, content-hash)` or similar — need to inspect 3-5 REF examples and reverse the hash spec (read `build/scripts/objcopy.py` upstream). Without this, the emitter can produce nodes but pairing will miss on `outputs[0]`.
2. **RESOURCE macro syntax variance.** Does our YA.MAKE parser already preserve the (name, key) pair order, including the `--keys L2J1aWx0aW4vY2FjZXJ0`-style base64-encoded keys? Quick grep test required.
3. **Cluster C aarch64-only `cpp_proto` AR gap.** 5 PROTO_LIBRARYs emit their AR on x86_64 in OUR but not aarch64 — needs a small probe (which module_dirs and which peer-walk site prunes them). Not user-blocking but worth a one-paragraph triage before the next PR.
4. **Cluster B.2 SIMD-variant macro support.** `SRC_C_AVX`/`SSE41`/`XOP`/`SSE2`/`SSSE3` — are these already known statement kinds in our YA.MAKE parser, or are they generic SET/SRCS variants? Tested with `grep -n "SRC_C_AVX\|SSE41\|SIMD" *.go` returns zero. Worth determining before pricing B.2 effort precisely.
