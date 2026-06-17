# AR member ordering — ymake mechanism (findings)

Investigation of how ymake orders the object files (`.o`) inside a static
library archive command, to replace `reorderARMembers`'s special-case buckets
with the real upstream mechanism. All citations are to the yatool checkout at
`/home/pg/monorepo/yatool`. Ground-truth order taken from the raw ymake graph
`/home/pg/monorepo/3/sg6.json` (NOT the normalized dump — see pitfall below).

## TL;DR

- The AR command lists objects from `$AUTO_INPUT`
  (`build/conf/linkers/ld.conf:372` `_LD_TAIL_LINK_LIB=$AUTO_INPUT …`,
  `LINK_LIB=$_LD_LIB_GENERATE_MF $_LD_ARCHIVER $TARGET $_LD_TAIL_LINK_LIB`).
- `$AUTO_INPUT` = the module node's `node.Edges()` in **insertion order**
  (`devtools/ymake/mkcmd_inputs_outputs.h` `ProcessInputsAndOutputs`: iterates
  `node.Edges()`, pushes each `EDT_BuildFrom` File dep to `VAR_AUTO_INPUT`;
  `mkcmd.cpp:223`). Inputs are **not** sorted — only `peers` are
  (`mkcmd.cpp:284 Sort(peers)`).
- Edge insertion order = the order compile commands complete `Process()` in
  `TModuleBuilder::RecursiveAddInputs` (`devtools/ymake/module_builder.cpp:80`):
  a `CmdAddQueue` FIFO, where a command whose inputs are not yet available
  (`CheckInputs` → `FAILED`) is **re-queued to the back** (deferral), with a
  `firstFail`/`lastTryMode` guard to break cycles.
- Net rule: **declaration order, with deferral** — an object whose compile input
  is an in-module *generated* file (codegen `.cpp`/`.pb.cc`, JOIN amalgamation,
  copy, …) is pushed after the objects whose inputs are ready.

So the codegen special-case buckets in `reorderARMembers`
(`gen.go` — `cfSrcs`, `g4Srcs`, `hSerSrcs`, `evPbSrcs`, `pbCCSrcs`, `rl6Srcs`,
`reg3Srcs`, `legacyR6`) are all the same phenomenon: *generated source →
deferred to the tail*. They should collapse into one general "deferred" bucket
driven by the readiness/deferral model, not by `.suffix.o` string matching.

## The unresolved piece (left for later, per owner)

For `contrib/libs/glibcasm` (no codegen at all) the raw command still places the
flagged `SRC(file flags)` objects (ya.make lines 236–243) **before** the plain
`SRCS` objects declared earlier (lines 216–234):

```
0   glibc/sysdeps/x86_64/multiarch/strstr.c.o          SRC(…strstr.c -fgnu89-inline)        line 236
1   glibc/sysdeps/x86_64/strcspn-generic.c.o           SRC(…strcspn-generic.c -DHAVE…)      line 240
2   glibc/sysdeps/x86_64/strpbrk-generic.c.o           SRC(…)                               line 241
3   glibc/sysdeps/x86_64/strspn-generic.c.o            SRC(…)                               line 242
4   glibc/sysdeps/x86_64/multiarch/strstr-avx512.c.avx512.o   SRC_C_AVX512(…)               line 243
5-145  _/glibc/…/*.S.o, *.c.o                          plain SRCS subdir                    lines 216-233
146 startup.c.o                                        plain SRCS root                      line 234
```

Pure declaration-order FIFO would put the SRCS block (216–234) first. It does
not. So flagged `SRC(...)` commands enter `CmdAddQueue` (or get their edge
added) **before** bulk `SRCS`, and there is no codegen dependency to explain a
deferral of the SRCS block. The hoisting site was not located in
`SrcStatement` / `AddByExt` / `AddSource` / `RecursiveAddInputs`
(`devtools/ymake/module_builder.cpp:246,723,80`). Open question:

> Why is `SRC(file flags)` in `$AUTO_INPUT` ahead of an earlier-declared `SRCS`?
> (separate processing phase for flagged sources? `_SRC` `.CMD` macro path vs
> bulk? the `notransformbuilddir` non-`_/` output?)

Current code reproduces the flagged order with a stable sort of the hoisted
("noLto") bucket by ya.make line (`gen.go reorderARMembers`, `arDeclLine`
populated in three emission sites: the `d.srcs` loop, `srcExtraFlat`, SIMD). It
is a local heuristic, kept until the hoist mechanism is pinned.

## The `_/` flat output

`_/` is NOT a "flagged source" marker and NOT collision disambiguation. It is the
**module-subdir transform** (`devtools/ymake/macro_processor.cpp:828`
`BuildDirStr = NPath::Join(BuildDirStr, transform ? "_" : ".", relative)` inside
`TCommandInfo::InitDirs`). A source under a subdir of the module gets
`module/_/subdir/file.o` when `transform` is on; `transform =
!NoTransformRelativeBuildDir`. The `notransformbuilddir` modifier
(`devtools/ymake/commands/mods/io.cpp:556`, `macro.cpp:52`) turns it off →
`module/subdir/file.o` (no `_/`). So flagged `SRC()` outputs in glibcasm are
non-`_/` (notransform) while plain SRCS-subdir are `_/`.

## Measurements / pitfalls

- **The normalized dump sorts `inputs`** — comparing `inputs` arrays shows them
  "identical" while the real `cmds` order differs. Always read the **command**
  (`cmd_args`) for AR member order, ideally from the raw `sg6.json`, not the
  normalized jsonl.
- Proto libraries build their archive through their **own** `emitARNode` in
  `emit_proto.go` (≈ line 650), bypassing `reorderARMembers`. So a proto AR's
  `pb.cc` → `h_serialized` order (observed in `libarc-api-proto.a`: all `.pb.cc`
  then all `.h_serialized`) is set there, not by the buckets. Don't use proto
  libraries to infer `reorderARMembers` behavior.
- `reorderARMembers`'s codegen buckets are only exercised by **non-proto**
  modules that pull `.pb.cc`/`.rl6`/… objects via `SRCS`; those are comparatively
  rare, so the inter-type bucket order is weakly constrained by the gate.

## Implication for the rewrite

Target form of `reorderARMembers` (the general mechanism):

1. **hoisted (flagged)** — `SRC`/`SRC_C_NO_LTO`/`SRC_C_AVX*`, declaration order
   (the *why-first* is the open question above; for now: line-sorted).
2. **ready (non-generated)** — plain `SRCS` whose compile input is a real source
   file, declaration order.
3. **deferred (generated)** — objects whose compile input is an in-module
   generated file (codegen, JOIN amalgamation, copy), in the order they become
   ready (≈ declaration order of the generator). This single bucket replaces
   `cfSrcs`/`g4Srcs`/`hSerSrcs`/`evPbSrcs`/`pbCCSrcs`/`rl6Srcs`/`reg3Srcs`/
   `legacyR6`.

The faithful way to produce groups 2+3 is a **`CmdAddQueue` simulation**:
declaration-ordered queue, defer (re-queue) any object whose source is an
in-module generated output until that output's producer has been emitted. The
type-grouping seen today is emergent from deferral depth, so a flat
declaration-sort will NOT reproduce multi-type modules — the simulation will.

Plan: introduce a per-object declaration sequence for every src-derived object
(generalize `arDeclLine`/`srcLine` beyond flagged sources — `SrcsStmt.Line`,
`JoinSrcsStmt.Line`, `SimdSrc.Line`, `SrcFlatEntry.Line` all exist), and an
`isGenerated` predicate (from `isCFGenerated`/`isProtoGenerated` + the codegen
emission paths, not `.suffix.o` strings). Then replace the buckets group by
group, running `./dev/validate.py` after each (gating sg2–sg5 must stay
byte-exact; sg6 is the xfail being reduced).

## Session progress context

`matched` 15459 → 15485 / 15508 this session by mechanism-grounded fixes:
py-proto auxcpp grpc.py sibling `_pb2.py`; RUN_PROGRAM `path:modifier` resolution
(CFFI); COPY_FILE `.a` source as AR input (pydantic-core); cython transpile
inputs source-only (tvmauth); AR hoisted-bucket declaration order (glibcasm).
Remaining sg6 divergences: LD ya-bin (missing second `-lrt` — OBJADDE cross-peer
dedup), LD swig/cffigen (SBOM `.component.sbom` order = `SRCS_GLOBAL` transitive
merge order, `devtools/ymake/module_restorer.cpp:61,87,497`).
