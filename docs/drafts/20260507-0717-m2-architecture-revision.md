# M2 Architecture Revision — Module-instance addressing & cross-platform recursion

## 1. Architectural feedback summary

User reports a load-bearing structural error in the current plan. A module is **not** addressable by `path` alone. Its identity is the tuple **`(path, language, target, flag-set)`**. Three consequences:

1. **Polymorphic modules.** A `PROTO` module's emitted artefacts depend on the entering language: enter via a CPP peer, you get `.pb.cc`/`.pb.h`; via a GO peer, you get `.pb.go`. Not visible in M2 archiver subgraph (CPP-only), but addressing scheme must accommodate it from start.
2. **Cross-platform = re-instantiation, not joining.** Host-tool dependency must walk into the dependee module's path with `target` flipped to host platform. The walker recurses into a *different* `ModuleInstance` of the same path; the result is a real `NodeRef` in the same emitter. There is no separate "host pass" buffered then merged.
3. **Early execution.** Once a node's deps are resolved, an executor can dispatch it. Today's "buffer all → finalize → merge" is incompatible. Streaming emitter (M3) must be designed against new addressing now.

D8 (two-pass + merge) and D20 (stub-host UIDs) directly contradict this and are invalidated.

## 2. Verified reality (probes against `/home/pg/monorepo/yatool_orig/g.json`)

- **3,730 nodes total** in reference graph; **42 distinct `target_properties.module_dir`** values.
- **10 module_dirs instantiate on BOTH `default-linux-aarch64` (target) and `default-linux-x86_64` (host)** — `build/cow/on`, `contrib/libs/cxxsupp/{builtins,libcxx,libcxxabi-parts,libcxxrt}`, `contrib/libs/{libunwind,musl,musl/full}`, `contrib/tools/ragel6`, `library/cpp/malloc/api`. Even M1 leaf `build/cow/on` is dual-instantiated.
- **11 distinct `outputs[0]` strings appear in BOTH host and target sets** — disambiguated by `platform`, not output mangling.
- **CC commands diverge sharply between host/target.** For `build/cow/on/lib.c`: target = 101 args (`-g -O0`, `--target=aarch64-linux-gnu`, output `lib.c.o`); host = 105 args (`-O3 -fPIC -DNDEBUG`, `--target=x86_64-linux-gnu`, output `lib.c.pic.o`). 68 args shared, ~36 differ. **`.pic.o` vs `.o` extension** is the clearest tell.
- **Op-type distribution:** `CC: 3571, AR: 48, AS: 83, JS: 23, LD: 3, CP: 1, R6: 1`. **`module_lang` is `cpp` on 51 nodes (1.4%) and `""` on 3,679** — language polymorphism NOT exercised in reference. M5+ concern.
- **Cross-platform edges sparse.** From `tools/archiver/archiver` BFS via deps (host-clipped) = 1,926 target nodes; via deps + foreign_deps = 3,730 nodes. **1 explicit `deps` edge** crosses target → host (parser.rl6.cpp → ragel6 LD). **26 nodes carry `foreign_deps`** — 1 R6 target node, 25 host AS nodes (asmlib `.pic.o` outputs depending on YASM host LD UID).
- **Result roots:** 1 (the archiver binary).

Implication: cross-platform recursion is the right model. Post-merge buffered ~1,800 host nodes only to glue 27 cross-edges. With direct recursion, those 27 edges become real `NodeRef`s during the walk.

## 3. Revised cross-cutting decisions (D30–D40)

### D30 — Module instance is the address [LOCKED] · supersedes D8, D20, D21

`ModuleInstance` lives in new file `module.go`:

```go
type ModuleInstance struct {
    Path     string
    Language Language
    Target   PlatformID
    Flags    FlagSet
}

type Language string
const (
    LangCPP   Language = "cpp"
    LangProto Language = "proto"  // reserved
    LangGo    Language = "go"     // reserved
    LangPy    Language = "py"     // reserved
    LangJava  Language = "java"   // reserved
)

type PlatformID string
const (
    PlatformDefaultLinuxAArch64 PlatformID = "default-linux-aarch64"
    PlatformDefaultLinuxX8664   PlatformID = "default-linux-x86_64"
)

type FlagSet struct {
    NoLibc, NoUtil, NoRuntime, NoPlatform, NoCompilerWarnings bool
    IsCpp bool
    PIC   bool   // host = true, target = false (typically)
    Extra []string  // sorted; ADDINCL/CFLAGS digests for M5
}
```

Comparable by value. Memo key = `map[ModuleInstance]*moduleEmitResult`. `String()` for diagnostics. `NewFlagSet(...)` enforces sorted `Extra`.

### D31 — Cross-platform recursion replaces post-merge join [LOCKED] · supersedes D8

When rule emits node depending on host tool (e.g. ragel6), the rule does NOT inject stub UID. Instead:

```go
hostInstance := parentInstance.WithHost(ctx.cfg)
hostResult := genModule(ctx, hostInstance)
// → wire as ordinary DepRef or ForeignDepRefs["tool"]
```

`(ModuleInstance).WithHost(cfg) ModuleInstance` flips `Target`, sets `Flags.PIC=true`. Walker is platform-agnostic; only `cfg` parameter changes per recursion frame. Result graph holds host AND target nodes side-by-side via `node.Platform` and `node.HostPlatform=true`.

Q5 (`--filter-platform`) descoped — direct recursion emits both platforms naturally.

### D32 — One Emitter, one Graph, no merge step [LOCKED] · supersedes D8

`Gen(cfg PlatformConfig, sourceRoot, targetDir string) *Graph` builds a single `BufferedEmitter`. Both target and host nodes accumulate. `cfg` seeds target; host derived via `cfg.Host()`.

```go
type PlatformConfig struct {
    Target PlatformSpec
    Host   PlatformSpec
}

type PlatformSpec struct {
    ID     PlatformID
    Triple string
    March  string
    SDK    string
    PIC    bool
}
```

`TargetCfg` becomes `DefaultLinuxConfig`.

### D33 — Rule signatures take `ModuleInstance` [LOCKED] · supersedes D21

```go
EmitCC(instance ModuleInstance, srcRel string, emit Emitter) (NodeRef, string)
EmitAR(instance ModuleInstance, objRefs []NodeRef, objPaths []string, emit Emitter) NodeRef
EmitAS(instance ModuleInstance, srcRel string, yasmLD NodeRef, emit Emitter) (NodeRef, string)
EmitCP(instance ModuleInstance, srcRel, dstRel string, emit Emitter) (NodeRef, string)
EmitJS(instance ModuleInstance, name string, srcs []string, emit Emitter) (NodeRef, string)
EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, emit Emitter) (NodeRef, string)
EmitLD(instance ModuleInstance, mainObjRef NodeRef, peerLDRefs []NodeRef, peerLDPaths []string, ..., emit Emitter) NodeRef
```

Output path uses `instance.Flags.PIC`: `.o` vs `.pic.o`, `.a` vs `.pic.a`.

### D34 — `genCtx.memo` keyed on `ModuleInstance` [LOCKED]

```go
type genCtx struct {
    cfg        PlatformConfig
    sourceRoot string
    emit       Emitter
    memo       map[ModuleInstance]*moduleEmitResult
    walking    map[ModuleInstance]bool
}
```

Two distinct instances of `build/cow/on` (target and host) memoize separately; both emit. Memo deduplicates *within* an instance.

### D35 — Language-polymorphic macros via dispatch table [LOCKED structurally; M5 implementation]

```go
type LanguageEmitter func(instance ModuleInstance, srcRel string, emit Emitter) (NodeRef, string)

type ProtoEmitter struct {
    perLang map[Language]LanguageEmitter
}
```

Walker, when entering PROTO directory's ya.make, looks up dispatch table from entering instance's Language. M2 doesn't ship `proto.go`; M5+ does.

### D36 — Walker is post-order, recursion-driven, single emitter [LOCKED] · supersedes D19

Same DFS / post-order / cycle-detection, keyed by `ModuleInstance`. Visit order = PEERDIR declaration order (preserves R14 link-order). Host-tool recursion fires inline; host instance walked to completion before returning host root's `NodeRef` to target caller.

Cycle detection covers within-platform AND cross-platform cycles.

### D37 — Streaming emitter contract [LOCKED for design; M3 implementation]

Add to `Emitter` interface NOW (not in M3) so rules are written against it from M2-onward:

```go
type Emitter interface {
    Emit(n *Node) NodeRef
    Result(NodeRef)
    OnReady(NodeRef) <-chan struct{}  // closes when node's DepRefs all resolved
}
```

`BufferedEmitter` implements `OnReady` trivially (channel closes at Finalize). `StreamingEmitter` (M3) closes per-node as topo wave reaches it. Signature locked NOW; M3's impl swaps without rule-emitter rewrite.

### D38 — Comparator continues to receive a static `*Graph` [LOCKED]

Finalize still returns `*Graph`. Streaming emitter (M3) implements Finalize by waiting on internal pipeline + assembling same data structure. Comparator code unchanged.

### D39 — `flag.PIC` is the canonical host/target axis for M2 [LOCKED]

For M2 CPP-only world, `Flags.PIC=true` ⇔ host build. Drives:
- output path mangling (`.o` ↔ `.pic.o`, `.a` ↔ `.pic.a`)
- inclusion of `-fPIC` in CC bundle
- flag-bundle selection (release vs debug)

### D40 — `--filter-platform` descoped [LOCKED] · revises Q5

With direct recursion, generator emits BOTH platforms; comparator pairs by `(outputs[0], platform)`. No filter needed for M2 acceptance. Q5 deferred indefinitely.

## 4. Salvage decision per Wave 2 PR

### PR-13 — parser AST + macro evaluator (`yamake.go`, `macros.go`)
**Survives.** Parser orthogonal to addressing. EvalCond may gain `instance` parameter later. **Action: cherry-pick verbatim into PR-PIVOT.**

### PR-14 — `ModuleFlavor` + musl bundle (`flags.go`, `cc.go`)
**Partial.** `ModuleFlavor` is structurally a subset of `D33`'s `FlagSet`. Musl flag bundle valuable.
**Action:** cherry-pick **flag-bundle additions in `flags.go`** only. Discard `ModuleFlavor`/`inferModuleFlavor`/`EmitCCFlavor`. `inferModuleFlavor(targetDir)` heuristic survives as `inferFlagsFromPath(path) FlagSet`.

### PR-15 — `ar.go` real archive-naming + multi-source sort + GLOBAL_SRCS
**Survives — small signature update.** Real archive-naming function path-driven, orthogonal. **Action:** cherry-pick with `EmitAR(instance, ...)` signature update.

### PR-17 — `cp.go` + `js.go` + `r6.go` + Finalize relax
**REWORK.**
1. **`cp.go`/`js.go`** — survive with `instance` plumbing.
2. **`r6.go`** — INVALIDATED. EmitR6 now takes `ragel6LD NodeRef` parameter, wires via `ForeignDepRefs["tool"]` (real ref). Walker recurses into host instance of `contrib/tools/ragel6`.
3. **Finalize relaxation (`emitter.go`)** — INVALIDATED. With real refs everywhere, no rule pre-populates `node.ForeignDeps`. **REVERT.**

**Action:** cherry-pick `cp.go`/`js.go`; redo `r6.go`; revert `emitter.go`.

### Already-merged PR-12 (gen.go recursive walk)
**REWORK in-place via PR-PIVOT.** Memo keys change from `string` to `ModuleInstance`. Walker shape preserved.

### Already-merged PR-16 (`as.go`)
**REWORK in-place via PR-PIVOT.** Take `yasmLD NodeRef` parameter; wire via `ForeignDepRefs["tool"]`.

### Already-merged PR-18 (Emit/Result post-finalize guards)
**Survives unchanged.** Orthogonal.

## 5. Updated PR breakdown (M2 onward)

### Wave 2-PIVOT (single sequential PR; replaces in-flight PR-13/14/15/17)

- **PR-23 (PR-PIVOT)** [Opus] — Architectural pivot. Introduces `ModuleInstance`, `FlagSet`, `Language`, `PlatformID`, `PlatformConfig{Target, Host}`. Retrofits `gen.go`, `cc.go`, `ar.go`, `as.go`. Cherry-picks PR-13 macros, PR-14 musl bundles, PR-15 archive-naming, PR-17 cp/js. Rewrites r6.go. Reverts PR-17 emitter.go. Adds `Emitter.OnReady`.

### Wave 3
- **PR-24** [Opus] — `ld.go` (LD rule, 4-cmd Node) + Gen wiring for PROGRAM-with-LD. (= old PR-19 retargeted.)
- **PR-25** [Opus] — Walker integrates macro evaluator + per-instance flag derivation + host-tool recursion hooks for ragel6/yasm. **Wave-3 keystone.**

### Wave 4
- **PR-26** [Sonnet] — Acceptance: full `tools/archiver` subgraph (TARGET + HOST = 3,730 nodes). No `--filter-platform`. **Acceptance**: L0 ≥ 95%, L1 ≥ 80%, L2 ≥ 70%, L3 ≥ 50% on FULL graph.
- **PR-27** [Sonnet] — Sweep deferred cosmetics.

### Worktree disposition
- `agent-a28ac5af8195f09ad` (PR-13) — cherry-pick source, no merge.
- `agent-ab6afc527bc92b702` (PR-14) — cherry-pick `flags.go` musl bundles only.
- `agent-ac53615d2babab6e9` (PR-15) — cherry-pick `ar.go`.
- `agent-aba07bbc8afadda77` (PR-17) — cherry-pick `cp.go`/`js.go`. Discard `r6.go`/`emitter.go` deltas.

## 6. M3/M4 milestone revision

### M3 (was: streaming emitter + parallel parser)
- **StreamingEmitter** implements `OnReady` per-node. Topo-sort frontier + worker pool.
- **ParallelWalker** uses `errgroup`. Per-instance `sync.Once` for deduped concurrent visits.
- Comparator unchanged.
- Acceptance: peak RSS < 200 MB, wall ≤ 1.5× sequential.

### M4 (was: full host platform via post-merge)
**Reframe.** Cross-platform recursion already in M2. M4 = correctness check + full host bundle hand-translation.
- Tests that ragel6/yasm host chains + 1,800-node host CPP closure all emit through one Gen call byte-exact.
- Host-side flag bundles for cxxsupp/builtins etc.
- Acceptance: L0 = 100%, L1 ≥ 95% on FULL graph.

## 7. Open questions (resolved with planner defaults)

- **Q6**: type names locked.
- **Q7**: PR-PIVOT lands as ONE squash commit per CLAUDE.md "One PR = one commit" discipline. Body large but reviewable.
- **Q8**: M3 streaming impl deferred; `OnReady` interface lands in PR-PIVOT.
- **Q9**: confirmed by §2 probe — `foreign_deps.tool` semantics permit recursion model.
- **Q10**: PR-PIVOT acceptance pins target byte-exact; host structural (4 nodes total). Full host byte-exact = M4.

## 8. PR-PIVOT (PR-23) brief

### Scope — files to change

**New files:**
- `module.go` — `ModuleInstance`, `FlagSet`, `Language`, `PlatformID`, `inferFlagsFromPath`, `(ModuleInstance).WithHost`, `String()`.
- `cp.go`/`js.go` — cherry-pick from PR-17 worktree, signature update.
- `r6.go` — REWRITE: `EmitR6(instance, srcRel, ragel6LD NodeRef, emit) (NodeRef, string)`, wires via `ForeignDepRefs["tool"]`.
- `macros.go` — cherry-pick from PR-13 worktree verbatim.

**Modified files:**
- `toolchain.go` — `PlatformConfig{Target, Host}`. `DefaultLinuxConfig`.
- `gen.go` — `genCtx.memo: map[ModuleInstance]...`. PEERDIR walk passes parent instance forward. Tool deps trigger `instance.WithHost(ctx.cfg)`.
- `cc.go` — `EmitCC(instance, srcRel, emit) (NodeRef, string)`. Body composes bundles by `instance.Flags.IsMusl/NoLibc/PIC`. Output `.o` vs `.pic.o`. Cherry-pick PR-14 musl bundle composition.
- `ar.go` — `EmitAR(instance, ...)`; cherry-pick PR-15 archive-naming + sort.
- `as.go` — `EmitAS(instance, srcRel, yasmLD, emit)`. Wires YASM via `ForeignDepRefs["tool"]`.
- `yamake.go` — cherry-pick PR-13 verbatim.
- `flags.go` — cherry-pick PR-14 musl bundles + add host PIC bundle (`-O3 -fPIC -DNDEBUG -mcx16 ...`).
- `emitter.go` — REVERT PR-17 Finalize relax. Add `OnReady(NodeRef) <-chan struct{}` interface; BufferedEmitter no-op.

**Tests:**
- `TestModuleInstance_Equality_Hashing`
- `TestEmitCC_BuildCowOn_Target_ByteExact` — preserves M1
- `TestEmitCC_BuildCowOn_Host_ByteExact` — NEW: 105-arg host bundle for `lib.c.pic.o`
- `TestEmitAR_BuildCowOn_Target_ByteExact` — preserves M1
- `TestEmitAR_BuildCowOn_Host_ByteExact` — NEW
- `TestEmitR6_RagelHostRecursion_Synthetic` — NEW
- `TestGen_DualInstantiation_BuildCowOn` — Gen emits 4 nodes (2 target + 2 host) byte-exact against unfiltered reference
- `TestEmitter_OnReady_BufferedNoOp`
- `TestFinalize_RejectsPreSetForeignDeps` — restored

### Acceptance commands

```bash
go build ./... && go vet ./... && gofmt -l *.go && go test -count=1 ./...
./yatool make -j 0 -G build/cow/on > /tmp/m1pivot.json
./yatool compare --level=3 /tmp/m1pivot.json /home/pg/monorepo/yatool_orig/g.json
# Output must include: "4 matched / 4 pairs / 0 unpaired-want / 3726 unpaired-got" at all 4 levels.
```

### Out of scope (deferred)
- Full PEERDIR closure (PR-25).
- LD rule (PR-24).
- StreamingEmitter impl (M3).
- Macro evaluator integration (PR-25).
- Per-instance ADDINCL/CFLAGS (M5).

### Risks
- **R-PIVOT-1**: Cherry-picking from 4 worktrees onto rewritten base is conflict-prone. Mitigate: line-level cherry-picks, expect file rewrites.
- **R-PIVOT-2**: PR-13's macros.go may reference no-longer-extant call sites. Verify before cherry-pick.
- **R-PIVOT-3**: Reverting PR-17 Finalize relax means rules cannot pre-populate `node.ForeignDeps`. Test re-pins contract.
- **R-PIVOT-4**: Host CC bundle (105 args) hand-translated FIRST time. Reviewers should expect side-by-side cmd_args dump and iteration.
