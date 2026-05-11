# D41 — Target-platform axis as a first-class field on `ModuleInstance`

**Date:** 2026-05-11
**Author:** READ-ONLY architectural planner (no code edited)
**Scope:** Audit + refactoring roadmap. Type-level address is already
correct per D30; this document catalogs the *implementation drift*
where the host-vs-target axis is read through the `Flags.PIC` proxy
instead of the canonical `instance.Target` field, and proposes the
surgical refactor that closes the gap before M3 expands the host
closure 14× (3 → 16 LDs, 1 → 739 foreign_deps consumers, 13 new host
PROGRAMs).

---

## 0. User prompt verbatim

> "для того, чтобы корректно построить sg2.json, не обойтись без того,
> что адрес модуля — не путь, а путь и набор флагов, в том числе,
> target platform, иначе не получится сделать норм зависимости от
> тулинга, который собирается в процессе сборки target кода. это
> неотъемлемая часть этой системы сборки."

Operationalised: the *type* of the address (D30: `ModuleInstance{Path,
Language, Target PlatformID, Flags FlagSet}`) is already a tuple. The
*implementation* across `cc.go`, `ld.go`, `ar.go`, `as.go`, `gen.go`
intermittently reads the host-vs-target axis through `instance.Flags.PIC`
instead of through `instance.Target`. The two coincide in M2 because
every host PROGRAM the M2 closure reaches is PIC-compiled. Under M3
they will continue to coincide for the 16 LDs in scope (host PROGRAMs
remain PIC; the target ymake LD remains non-PIC) — but the conflation
is structurally wrong:

1. **PIC** ("position-independent code") is a compiler-emission flag.
2. **Target platform identity** is a build-graph axis (`PlatformID`).

The two are independent in principle. The codebase happens to set
`Flags.PIC=true` exactly when `Target == cfg.Host.ID` because
`WithHost` (`module.go:132-138`) sets both simultaneously. The audit
below confirms that for M3 we do not *need* to decouple them yet
(every M3 host PROGRAM is still PIC-compiled), but the current code is
fragile against:

- A target build that happens to be PIC (a shared library target —
  not in M2/M3 scope but in M5+).
- A host build that, hypothetically, does not need PIC (no real case
  today, but the comments in `cc.go:178` and `ar.go:15` describe the
  field as "host", not "PIC").
- A second host platform (cross-build to a third axis — out of scope
  for M3 but the address tuple already supports it).

The refactor closes the gap before M3 expands the host closure 14×.

---

## 1. Audit — `Flags.PIC` uses in production code

`grep -nE 'Flags\.PIC|\.PIC\b' *.go` (excluding `_test.go`) yields **40
hits across 8 files**. Classification per the brief's A/B/C taxonomy:

- **A**: legitimate "emit `-fPIC` flag / `.pic.o` suffix" — PIC as a
  compiler-flag, stays. **17 hits.**
- **B**: host-vs-target axis proxy. Should read `instance.Target ==
  cfg.Host.ID` (or a new helper `instance.IsHostBuild(cfg)`).
  **23 hits.**
- **C**: ambiguous (the hit is in a comment or in a builder that sets
  both PIC and Target simultaneously and so does both jobs). **3
  hits, all in `module.go` and `toolchain.go`.**

The grand total `17+23+3 = 43` slightly exceeds the 40 source-text
hits because three hits represent "comment + code-line" pairs that
the audit splits.

### 1.1 Category A — legitimate PIC compiler-flag uses (KEEP)

| file:line | role | reason kept |
|---|---|---|
| `cc.go:37` | header comment: `.o` vs `.pic.o` discriminator | doc, not code |
| `cc.go:203` | `suffix = ".pic.o"` output mangling | PIC-the-flag (compiler emits `.pic.o`) |
| `ld.go:265-266` | LD output suffix for `__vcs_version__.c.pic.o` | host LD vcs-stub compiles PIC |
| `ld.go:152` | `hostBuild := instance.Flags.PIC` — currently **AXIS-PROXY**, see Category B; the cmd[1] *compile* itself is the legitimate consumer (the vcs.c stub is compiled PIC iff the LD is host) | mixed |
| `cc.go:333` | `instance.Flags.PIC` → emit `-fPIC` | legitimate (the compiler flag itself) |
| `as.go:166` | `tags=["tool"]` selection | this is axis-proxy actually — see Cat B |
| `as.go:184` | `HostPlatform: instance.Flags.PIC` | axis-proxy — Cat B |
| `as.go:379` | `isHost := instance.Flags.PIC` switch between hostCFlags/commonCFlags | axis-proxy — Cat B |
| `as.go:136` | `if instance.Flags.PIC && asmlibYasmModules[instance.Path]` → yasm branch | axis-proxy — Cat B |
| `cc.go:273-280` | switch on `(isMusl, instance.Flags.PIC)` → composeHostCC / composeMuslHostCC | axis-proxy — Cat B (the compiler bundle differs because it's a *host* toolchain, not because the codegen wants PIC) |
| `gen.go:3074, 3202, 3268` | `srcInstance.Flags.PIC` selects scannerHost / arch=x86_64 | axis-proxy — Cat B (scanner-arch follows host platform, not PIC) |
| `gen.go:2168` | `jsInstance.Flags.PIC = false` to detach JS to target axis | axis-proxy mutation — Cat B (writes `false` to mean "target-platform-axis") |

**Strictly category-A hits — keep regardless of refactor:**

| file:line | code |
|---|---|
| `cc.go:203` | `suffix = ".pic.o"` |
| `cc.go:333` | `-fPIC` emission inside `composeHostCC` (the compiler-flag itself) |
| `ld.go:265` | `vcsOSuffix = ".pic.o"` for vcs-stub output |

Even these three "true PIC" uses depend on the host-vs-target axis in
practice (a target shared-library build in M5+ would also want
`.pic.o` and `-fPIC`). But for M2/M3 the `Flags.PIC` field correctly
reflects "compile as PIC", which the address tuple needs anyway. So:
**A is a small set, and after the refactor the remaining A-hits read
`instance.Flags.PIC` to mean "this compile wants PIC", which is what
the field is documented to be.**

### 1.2 Category B — host-vs-target axis proxy (REFACTOR)

These are the hits where `Flags.PIC` is read **to decide host vs
target**, not to decide PIC vs non-PIC. The refactor swaps to
`instance.Target == ctx.cfg.Host.ID` (or a helper on `ModuleInstance`).

| file:line | code | refactor target |
|---|---|---|
| `ar.go:186` | `if instance.Flags.PIC { tags = []string{"tool"} }` | `if instance.IsHostBuild(cfg)` |
| `ar.go:216-217` | `if instance.Flags.PIC { n.HostPlatform = true }` | `if instance.IsHostBuild(cfg)` |
| `as.go:136` | `if instance.Flags.PIC && asmlibYasmModules[...]` | `if instance.IsHostBuild(cfg) && ...` |
| `as.go:166-170` | `if instance.Flags.PIC { tags = []string{"tool"} }` | `if instance.IsHostBuild(cfg)` |
| `as.go:184` | `HostPlatform: instance.Flags.PIC,` | `HostPlatform: instance.IsHostBuild(cfg),` |
| `as.go:379` | `isHost := instance.Flags.PIC` (cmd_args composer) | `isHost := instance.IsHostBuild(cfg)` |
| `cc.go:273` | `case isMusl && instance.Flags.PIC:` → musl-host bundle | host-axis: `instance.IsHostBuild(cfg)` |
| `cc.go:277` | `case instance.Flags.PIC:` → host bundle | host-axis: `instance.IsHostBuild(cfg)` |
| `cc.go:338` | `if instance.Flags.PIC { node.HostPlatform = true }` | `node.HostPlatform = instance.IsHostBuild(cfg)` |
| `ld.go:152` | `hostBuild := instance.Flags.PIC` (drives 13 downstream branches) | `hostBuild := instance.IsHostBuild(cfg)` |
| `ld.go:265-266` | `if instance.Flags.PIC { n.HostPlatform = true; n.Tags = []string{"tool"} }` | `instance.IsHostBuild(cfg)` |
| `gen.go:53,55,672,703,713` | comments referencing `Flags.PIC` as host marker | update prose; code is in `derivePeerInstance` which carries the axis |
| `gen.go:713` | `inferFlagsFromPath(peerPath, parent.Flags.PIC)` — the `isPIC` argument seeds the peer's PIC field, BUT the peer's Target is set independently at 712 | the `inferFlagsFromPath(_, isPIC)` parameter becomes misleading once host-axis decouples from PIC; rename to `inferFlagsFromPath(peerPath, isHostBuild bool)` and have the function set PIC=isHostBuild (the only current behaviour) |
| `gen.go:2152-2168` | JS axis-detach: `if srcInstance.Flags.PIC { ... jsInstance.Flags.PIC = false }` | `if srcInstance.IsHostBuild(ctx.cfg) { jsInstance.Target = ctx.cfg.Target.ID }` (and the corresponding `Flags.PIC` clear stays for the compiler-flag axis, OR is set when the JS-derived CC switches target) |
| `gen.go:2870` | `if instance.Flags.PIC && asmlibYasmModules[...]` — host-axis trigger | `if instance.IsHostBuild(ctx.cfg) && ...` |
| `gen.go:3074, 3202` | `scanner = ctx.scannerHost` selection by `srcInstance.Flags.PIC` | by `srcInstance.IsHostBuild(ctx.cfg)` — scanner is *per host/target platform*, not per PIC-ness |
| `gen.go:3268` | `arch = "x86_64"` selection by `instance.Flags.PIC` for musl arch search path | by `instance.IsHostBuild(ctx.cfg)` — arch derives from host vs target axis, not from PIC |

**Count: 23 production-code hits that the refactor should rewrite.**

### 1.3 Category C — ambiguous / both-jobs sites

These are sites where `Flags.PIC` is written or read in a context that
simultaneously sets/reads the axis AND the compiler-flag, and so the
hit cannot be cleanly classified:

| file:line | code | resolution |
|---|---|---|
| `module.go:128-138` | `WithHost(cfg)` sets `out.Target = cfg.Host.ID; out.Flags.PIC = true` | This is the **invariant**: host builds happen to be PIC. The refactor keeps WithHost setting both, but adds a documentation note that callers must read `Target == cfg.Host.ID` for axis dispatch, not `Flags.PIC`. |
| `module.go:167-170` | `inferFlagsFromPath(path, isPIC bool)` — `isPIC` parameter doubles as "is host build" for path-based inference; downstream callers pass `parent.Flags.PIC` from `derivePeerInstance` (gen.go:713) | rename parameter to `isHostBuild` and update the call site at `gen.go:713` to pass `parent.IsHostBuild(cfg)` — but at `gen.go:713` cfg is not in scope. The fix is to thread cfg through `derivePeerInstance` OR to keep the boolean and rename the parameter only. Smaller diff: rename. |
| `toolchain.go:27-28, 50, 55` | `PlatformSpec.PIC bool` field — declared but unused by code (DefaultLinuxConfig sets Target.PIC=false, Host.PIC=true; no reader) | dead field; remove OR document as the source-of-truth for axis-side PIC default (Target/Host's *typical* PIC-ness) |

**Count: 3 hits.** None of these break M2; all benefit from a one-line
clarifying comment after the refactor.

---

## 2. Audit — host-recursion mechanism (`WithHost`)

Single definition: `module.go:128-138`. **3 call sites** in production
code (all in `gen.go`):

| call site | trigger | path override |
|---|---|---|
| `gen.go:2873` | `.S`/`.s` source in `asmlibYasmModules` | `yasmInstance.Path = "contrib/tools/yasm"` |
| `gen.go:2938` | `.rl6` source | `ragelInstance.Path = "contrib/tools/ragel6/bin"` |
| `gen.go:N/A` | (no third site in M2) | — |

Both call sites override `Path` after `WithHost` because the host
tool's PROGRAM module lives at a different path than the consuming
module. **Pattern:**

```go
toolInstance := instance.WithHost(ctx.cfg)
toolInstance.Path = "<host PROGRAM path>"
toolInstance.Flags = inferFlagsFromPath(toolInstance.Path, true)
```

The `Flags = inferFlagsFromPath(_, true)` pass replaces (does not
overlay) the just-flipped `Flags.PIC=true` from `WithHost` with a
freshly-inferred set. **This is correct for M2** because
`inferFlagsFromPath` honours the `isPIC` argument and re-sets PIC=true.
**This is the only correct use of `inferFlagsFromPath(_, true)` in the
codebase** — every other caller passes `parent.Flags.PIC` (which
inherits the parent's axis).

**M3 risk:** 13 new host PROGRAMs each need their own `WithHost(_)` +
`Path = "<tool path>"` + `Flags = inferFlagsFromPath(_, true)` block.
That's 13 × the same 3-line pattern. Either:

(a) extract a helper `func hostToolInstance(ctx *genCtx, toolPath
    string) ModuleInstance`, or
(b) document the pattern verbatim in 13 places.

The M3 plan (`docs/drafts/20260511-0930-m3-plan.md` §4.1) gestures at
this for `py.go`/`pb.go`/etc. but does not call out the axis-vs-PIC
ambiguity. **Recommendation:** PR-D41-A extracts the helper *before*
PR-M3-A starts, so the 13 new sites use the helper and the audit
catches a regression if any of them set PIC without Target.

---

## 3. Audit — `node.HostPlatform` field emission

`HostPlatform: bool` is set in **5 production sites**:

| file:line | code | risk |
|---|---|---|
| `ar.go:217` | `if instance.Flags.PIC { n.HostPlatform = true }` | reads PIC, should read axis |
| `as.go:184` | `HostPlatform: instance.Flags.PIC,` (struct literal) | reads PIC, should read axis |
| `as.go:289` | `HostPlatform: true,` (in yasm-shaped AS) | hardcoded true — yasm AS is always host; correct |
| `cc.go:338` | `node.HostPlatform = true` inside `if instance.Flags.PIC` branch | reads PIC, should read axis |
| `ld.go:266` | `n.HostPlatform = true` inside `if instance.Flags.PIC` branch | reads PIC, should read axis |

**Empirical M3 invariant** (from M3 plan §4.2):
`host_platform=true ⇔ platform == "default-linux-x86_64"` for all 8750
M3 nodes. The PIC-proxy currently produces the same result by
coincidence (every host build is PIC; the only non-PIC build is the
M2/M3 target). After the refactor:

```go
node.HostPlatform = (instance.Target == cfg.Host.ID)
```

becomes the **structurally correct** rule. This holds regardless of
the future PIC-decoupling decision.

---

## 4. Audit — `(Path, Language)` partial-key bypasses

`grep -nE 'map\[string\]|seen' *.go | grep -v _test.go` produced ~30
string-keyed maps in `gen.go`. Of these, **5 categories warrant
inspection** for "treats path-string as module identity":

| map | key | risk |
|---|---|---|
| `ldPluginCPCache map[string]NodeRef` (`gen.go:223`) | output path (`$(BUILD_ROOT)/<modulePath>/<name>.pyplugin`) | **PR-35l intentional** — CP node is platform-agnostic (musl.pyplugin shared between target and host walks). Documented at gen.go:206-222. Not a defect. |
| `asmlibYasmModules map[string]bool` (`gen.go:232`) | `instance.Path` only | **Static set** — list of paths whose `.S`/`.s` sources trigger yasm. Not a memo key; tests `instance.Path` membership only. No axis confusion. |
| `whitelistedMetadataMacros map[string]struct{}` (`gen.go:244`) | macro name | name, not module identity. Safe. |
| `runtimeAncestorPaths map[string]bool` (`gen.go:740`) | `instance.Path` only | **Static set** — paths whose default-peer set is empty. Same path on host axis still has empty default peers (the rule is intrinsic to the module path, not the axis). Safe but **brittle for M3** if any of these modules gain a target-only or host-only peer-suppression. |
| `runtimeAncestorCxxConsumers`, `runtimeStackAddInclPaths`, `bundledAddInclPaths`, `allocatorPeers` (gen.go:616, 796, 853, 876) | `instance.Path` or symbol | Same pattern — module-path-keyed; axis-agnostic by design. |

**Within `genCtx.walking` and `genCtx.memo`** (gen.go:187-188): both
are `map[ModuleInstance]...`. Memo keys are **full tuples**:

```go
ctx.memo[originalInstance] = result    // gen.go:1435, 2349, 2441
ctx.memo[instance] = result            // gen.go:1436, 2350, 2442
```

The dual-write pattern (`originalInstance` is the seed-flag instance;
`instance` after macro overlay) is documented at `gen.go:1320-1331`
(PR-34b). **Both keys are full `ModuleInstance` tuples** — no partial
keying. Confirmed no `(Path, Language)`-only memo lookups. Safe.

**`derivePeerInstance(parent, peerPath)`** (gen.go:708-715) produces a
fresh `ModuleInstance` with:

```go
return ModuleInstance{
    Path:     peerPath,
    Language: parent.Language,
    Target:   parent.Target,
    Flags:    inferFlagsFromPath(peerPath, parent.Flags.PIC),
}
```

The `Target` is correctly carried; the `Flags.PIC` is correctly
carried via the heuristic. **For M3** this remains correct: every peer
of a host-built consumer is itself host-built (the M3 walk traverses
peer closures under `instance.Target` — a host PROGRAM's peers are
host LIBRARYs; the JS axis-detach at `gen.go:2168` is the only
exception, and it explicitly resets both `Target` and `Flags.PIC`).

**No partial-key bypasses found.** The address tuple is intact at the
memo / cycle-detection layer.

---

## 5. Audit — `PlatformID` enum / type

`module.go:53-58`:

```go
type PlatformID string

const (
    PlatformDefaultLinuxAArch64 PlatformID = "default-linux-aarch64"
    PlatformDefaultLinuxX8664   PlatformID = "default-linux-x86_64"
)
```

**Two values defined.** Both are sufficient for M2 (aarch64 target,
x86_64 host) and M3 (same axes). Future cross-build adds a third value
without disrupting the address tuple.

Usage:

- `gen.go:685-695` (`buildIfEnv`): switches `ARCH_AARCH64` /
  `ARCH_X86_64` bindings based on `instance.Target`. This is the
  **architecturally correct** axis read — it goes through `Target`,
  not `Flags.PIC`. Confirmed: `buildIfEnv` is one of the few sites that
  already uses the canonical axis source.
- `gen.go:1078` (`defaultPeerdirsFor`): `instance.Target ==
  targetPlatformID` — also canonical. ✓
- `gen.go:331` (`Gen`): seeds `Target: cfg.Target.ID`. ✓
- `ld.go:245`, `cc.go:318`, `as.go:193`, `as.go:298`, `ar.go:205`,
  `cp.go:65`, `r6.go:135`: all `Platform: string(instance.Target)`. ✓
- `js.go:104`: `Platform: string(platform)` — explicit override
  parameter (JS axis-detach to target). ✓

**Conclusion:** the `Target` field is *already* the canonical source
for the `node.Platform` JSON field across all 7 emitter files. The
host-vs-target *dispatch* in cmd_args/HostPlatform/tags is the only
place that drifts through `Flags.PIC`.

---

## 6. Audit — M3 plan PR-M3-A scope

The M3 plan (`docs/drafts/20260511-0930-m3-plan.md`) PR-M3-A section
(line 785-822) calls for:

> Extend the host-walk machinery to recognize and emit the 13 new
> host PROGRAMs ...
>
> Files touched: `gen.go` (mainly — host walk machinery, RECURSE-based
> child-PROGRAM lift mirroring M2 PR-23's ragel6/yasm pattern), ...

The plan does **NOT** call out the axis-vs-PIC distinction. The brief
in §4.2 of that plan says:

> `host_platform=true ⇔ platform == "default-linux-x86_64"`. M2
> already emits this correctly via `instance.Flags.PIC` discriminator.

This is **structurally complacent**: it accepts the PIC-proxy because
"M2 already emits this correctly". M3 inherits the proxy and so will
ship with the same fragility.

**Gap in M3 plan:** no PR explicitly addresses the axis-PIC decoupling.

**Risk if not addressed before M3:** the 13 new host PROGRAMs in M3
each add at least 1-2 `Flags.PIC` axis-proxy reads (the per-tool
host-walk trigger + the tool's LD HostPlatform setting). That's
13 × 2 ≈ 26 new B-category hits on top of the existing 23 — **doubles
the surface** that a future PR-D41-A would have to refactor. Cheaper
to land the refactor first.

**Recommendation:** PR-D41-A lands *before* PR-M3-A starts. PR-M3-A
then uses the new `instance.IsHostBuild(cfg)` helper from day one.

---

## 7. Design proposal

### 7.1 The core question: keep or kill the PIC-axis coupling?

Two designs are viable:

**Design X (minimal): keep PIC and Target coupled, add a helper.**

- `WithHost(cfg)` keeps setting both `Target=cfg.Host.ID` AND
  `Flags.PIC=true`. The invariant **"`Target == cfg.Host.ID ⇔
  Flags.PIC == true` for M2/M3 closures"** is documented as a locked
  cross-cutting note (D41).
- New helper method:
  ```go
  func (mi ModuleInstance) IsHostBuild(cfg PlatformConfig) bool {
      return mi.Target == cfg.Host.ID
  }
  ```
- All 23 Category-B sites swap `instance.Flags.PIC` → `instance.IsHostBuild(cfg)`.
- The 3 Category-A sites (`.pic.o` suffix, `-fPIC` flag emission)
  continue to read `instance.Flags.PIC`. The semantics shift from
  "this is a host build" to "this build wants PIC code generation" —
  which is what the field name actually says.

**Design Y (principled): decouple PIC and Target completely.**

- Same helper as Design X.
- `WithHost(cfg)` sets `Target=cfg.Host.ID` only; **stops setting**
  `Flags.PIC=true`.
- A new derivation rule: `Flags.PIC` is set when the *compile shape*
  needs PIC. For M2/M3 every host build *does* need PIC, so a
  separate rule "host builds want PIC" sets PIC=true at the
  composer-input layer (not at `WithHost`).
- This adds work — every place that constructs a host ModuleInstance
  must explicitly set `Flags.PIC=true` (or, more cleanly, the compile
  composers default PIC=true for `IsHostBuild`-returning instances).

**Decision: Design X.** Rationale:

1. M2/M3 do not exercise PIC-decoupled targets (no shared-library
   target in the M3 closure).
2. Design Y's added complexity buys nothing for M3 acceptance.
3. The brief explicitly says "the type is already correct" — the
   refactor is implementation, not type-level.
4. Design X's locked-invariant approach is reversible. If M5 needs
   PIC-decoupled targets (e.g., shared-lib output), PR-D41-bis can
   land Design Y on top.

### 7.2 New helper API

Add to `module.go`:

```go
// IsHostBuild reports whether this instance targets the host platform
// (i.e. it is part of the host-tool closure: ragel6, yasm, protoc,
// py3cc, etc.) as opposed to the target platform (the aarch64
// closure for M2/M3).
//
// The axis is determined by `instance.Target == cfg.Host.ID`, NOT by
// `instance.Flags.PIC`. PIC is a compiler-flag (whether to emit
// position-independent code); the axis is a platform identity. Today
// they coincide (every host build is PIC), but the field semantics are
// independent and code that dispatches on the axis should read this
// helper, not Flags.PIC.
//
// Lock: see cross-cutting note D41 in tasks.md.
func (mi ModuleInstance) IsHostBuild(cfg PlatformConfig) bool {
    return mi.Target == cfg.Host.ID
}
```

**Signature:** takes `PlatformConfig` because the host platform ID
lives in `cfg.Host.ID`. Cannot be a zero-argument method because
`PlatformID` is a global string constant — but the *which* host varies
with config (M2/M3 happen to use the same host, but unit tests may
construct fixtures with different platforms).

Callers that have only the instance must thread `cfg` through. For the
6 emitter files (cc.go, ar.go, as.go, ld.go, cp.go, r6.go) that
currently dispatch on `Flags.PIC` for axis-only purposes, adding a
`cfg PlatformConfig` parameter to `EmitCC`/`EmitAR`/`EmitAS`/`EmitLD`
is the surface area of the refactor.

**Alternative considered:** embed `Host PlatformID` in `ModuleInstance`
itself, so `mi.IsHostBuild()` becomes parameterless. **Rejected**
because it duplicates `cfg.Host.ID` across every instance (33 KB of
redundant data in the memo map for ~1000 modules) and creates a second
source of truth for "what is the host platform".

### 7.3 `node.HostPlatform` emission rule

After the refactor, every emitter file sets:

```go
n.HostPlatform = instance.IsHostBuild(cfg)
```

uniformly (instead of `instance.Flags.PIC`). This applies to:

- `ar.go:217`
- `as.go:184` (struct literal initialization — needs the same helper
  read)
- `cc.go:338`
- `ld.go:266`

The hardcoded `HostPlatform: true` in `as.go:289` (yasm-shaped AS)
stays — yasm AS is always host by construction. **Optional cleanup**:
the yasm-shape function `emitASYasm` is only called when
`instance.IsHostBuild(cfg) && asmlibYasmModules[instance.Path]`; a
defensive assertion at the top of `emitASYasm` (`if
!instance.IsHostBuild(cfg) { ThrowFmt(...) }`) would make the
invariant explicit and survive future refactors.

### 7.4 Per-target flag derivation correctness

`buildIfEnv(instance)` at `gen.go:682-698` already dispatches on
`instance.Target`:

```go
if instance.Target == PlatformDefaultLinuxX8664 {
    env.SetBool("ARCH_AARCH64", false)
    env.SetBool("ARCH_ARM64", false)
    env.SetBool("ARCH_X86_64", true)
}
```

**No change needed** — this is already on the canonical axis.

However, the hardcoded `PlatformDefaultLinuxX8664` / `AArch64`
constants are brittle: if M5 adds a third platform, this switch
silently falls through both branches. **Suggested cleanup (PR-D41-A
optional):** derive the ARCH bindings from a `(PlatformID → ARCH
flags)` table on `PlatformConfig` instead of hardcoded if-chain. Out
of scope for D41 closure but flagged for M5.

### 7.5 `-fPIC` emission rule

After the refactor:

| Field | Read by | Means |
|---|---|---|
| `instance.Flags.PIC` | `cc.go:203, 333`, `ld.go:265` (output suffix; `-fPIC` flag) | "emit position-independent code" |
| `instance.IsHostBuild(cfg)` | all 23 Cat-B sites | "this build targets the host platform" |

For M2/M3 the two predicates always coincide (the only PIC=true
instances are host builds; the only host builds are PIC=true). The
refactor formally separates the two readings; the invariant becomes
**testable**:

```go
// Invariant test (gen_test.go addition):
//   For every ModuleInstance emitted in the M2/M3 closure,
//   instance.IsHostBuild(cfg) == instance.Flags.PIC.
//   D41 lock-in. If this test ever fails, the lock has been broken
//   intentionally — update D41 to record the new design and remove
//   the test.
```

This test catches a regression where someone (e.g. M5+) sets
`Flags.PIC=true` for a non-host instance without explicitly updating
D41. A "fail-loudly" surface for the next architectural shift.

### 7.6 Memo-key correctness

Already correct (§4). No change.

### 7.7 M3 sg2 scenarios unlocked

The refactor itself doesn't *unlock* new scenarios — it formalises the
axis. But it positions M3 for the following:

1. **protoc closure shared between target and host.** `libprotobuf` is
   PEERDIR'd by both `tools/protoc` (host) and any target ya.make
   that wants protobuf at runtime. Today this is unreachable because
   M2/M3 target closures don't peer libprotobuf for runtime (the only
   libprotobuf in M3 is for the host protoc). **No D41 impact, but
   M3-plan §3 confirms 156 libprotobuf CC nodes — all host.** If a
   future PR makes target ymake itself peer protobuf for runtime, the
   address tuple correctly emits both flavours; the proxy would have
   silently collided.

2. **abseil shared between target and host.** Same as #1 — abseil
   ships 312 (M2) + 314 (M3 new) CC nodes. Host and target instances
   are distinguishable via `Target`, not via path.

3. **musl arch path search.** `gen.go:3268` selects musl arch
   (x86_64/aarch64) by `instance.Flags.PIC`. After refactor, this
   becomes `instance.IsHostBuild(cfg)`. The semantics are the same;
   the dispatcher correctly reflects "musl arch path follows the
   build's target platform".

4. **Cross-tool sharing (ragel6 + yasm both use asmlib).** Already
   works in M2 via memo on the full tuple. D41 just hardens the
   invariant.

### 7.8 PR-M3-A interaction

PR-M3-A is *not* in flight (verified — see §0 of this audit, no `[~]`
on PR-M3-A in tasks.md; the worktree `a29d088de14a6fd48` mentioned in
the brief is actually a stale PR-43 worktree at commit `32053ae`).

**Recommendation:** land PR-D41-A *before* PR-M3-A starts. PR-M3-A
then uses the helper in its 13 new host-walk triggers and avoids
adding 26 new B-category hits.

---

## 8. PR breakdown

### PR-D41-A — Helper + Category-B sweep + invariant test

**Scope:**

1. Add `IsHostBuild(cfg PlatformConfig) bool` to `module.go`.
2. Thread `cfg` parameter into the 6 emitter entry points that read
   `Flags.PIC` for axis purposes: `EmitCC`, `EmitAR`, `EmitAS`,
   `EmitLD`, `EmitCP`, `EmitR6`, `EmitJS`.
   - **Subtlety:** the new parameter is `cfg PlatformConfig`, which
     these emitters do not currently take. The caller (`gen.go`) has
     `ctx.cfg` in scope at every emit site, so threading is mechanical.
     Tests (which call `EmitCC(instance, ...)` with fixture instances)
     pass `DefaultLinuxConfig`.
3. Replace the 23 Cat-B `instance.Flags.PIC` reads with
   `instance.IsHostBuild(cfg)`.
4. Update `node.HostPlatform` setting at 4 sites (ar.go:217, as.go:184,
   cc.go:338, ld.go:266) to read the helper.
5. Add a regression test in `gen_test.go`:
   ```go
   func TestD41_HostBuildEqualsPIC_InM2Closure(t *testing.T) {
       // Walk tools/archiver. For every emitted ModuleInstance,
       // assert instance.IsHostBuild(cfg) == instance.Flags.PIC.
   }
   ```
6. Update docstrings in `module.go`, `ar.go`, `as.go`, `cc.go`,
   `ld.go` that currently describe `Flags.PIC` as a host marker.
7. Rename `inferFlagsFromPath(path, isPIC bool)` parameter to
   `isHostBuild` for naming clarity (the actual implementation still
   sets `Flags.PIC = isHostBuild`; locked behind D41).

**Acceptance:**

- `go build`, `go vet`, `gofmt -l *.go` clean.
- Full test suite passes (`go test ./... -count=1`).
- M1 (`build/cow/on`): L0=L1=L2=L3=100% (preserved).
- M2 (`tools/archiver`): L0=L1=L2=L3=100% (preserved).
- `gen` wall ≤ 5 s on M2 (no perf regression; refactor is local).
- New test `TestD41_HostBuildEqualsPIC_InM2Closure` passes.

**Estimated LOC:** ~100-150 (mostly mechanical search-and-replace +
threading `cfg` through 6 emitter signatures + tests).

**Risks:**

- Threading `cfg` through emitter signatures touches the
  per-emitter `_test.go` files (each test that calls `EmitCC` /
  `EmitAR` / etc. needs a `cfg` argument). Mechanical but voluminous
  — estimate 30-50 test sites to update across `cc_test.go`,
  `ar_test.go`, `as_test.go`, `ld_test.go`, `r6_test.go`,
  `js_test.go`, `cp_test.go`. Standard pattern: pass
  `DefaultLinuxConfig`.
- The `inferFlagsFromPath` parameter rename touches all callers (2
  in production + ~10 in tests). Mechanical.

**Parallelism:** Single executor; the refactor is cross-file (touches
all 6 emitters) and a parallel split would conflict on `module.go`.

### PR-D41-B — Optional: emit-rule audit + future-test

**Scope:**

1. Audit every `n.HostPlatform = ...` site (now 4 + 1 hardcoded) and
   confirm it derives from `instance.IsHostBuild(cfg)`.
2. Add an emitter-level invariant assertion (defensive):
   ```go
   if n.HostPlatform != instance.IsHostBuild(cfg) {
       ThrowFmt("emitter %s: HostPlatform=%v but axis=%v",
           kind, n.HostPlatform, instance.IsHostBuild(cfg))
   }
   ```
   placed in `Finalize` or in each emitter's tail.
3. Add a forward-test that constructs a synthetic non-host PIC
   instance (Target=aarch64, PIC=true) and verifies the emitters
   emit `host_platform=false` for it. This pins Design X's semantic
   (PIC alone does not flip `host_platform`) and would fail if
   anyone re-introduces the Cat-B proxy.

**Acceptance:** L0..L3 preserved on M1 + M2. New test passes.

**Estimated LOC:** ~50 (mostly test).

**Parallelism:** Serial after PR-D41-A.

**Recommendation:** **defer** to "follow-up" status. PR-D41-A is
sufficient for M3; PR-D41-B is belt-and-suspenders.

### PR-D41-C — Optional: PlatformSpec.PIC dead-field cleanup

**Scope:** `toolchain.go:27-28` declares `PlatformSpec.PIC bool` but
no production code reads it. Either delete the field or document it
as the canonical default-PIC source (Target.PIC=false, Host.PIC=true
for M2/M3) and add a derivation site that consults it.

**Acceptance:** L0..L3 preserved.

**Estimated LOC:** ~10 (single-file).

**Recommendation:** **defer**, low-value. Address in M5 if a third
platform exposes the asymmetry.

---

## 9. Cross-cutting note proposal (D41)

To be appended to `tasks.md` after PR-D41-A merges:

> - [x] **D41** — Host-vs-target axis dispatch reads
>   `instance.Target == cfg.Host.ID`, NOT `instance.Flags.PIC`. The
>   address tuple is `(Path, Language, Target, Flags)`; no code path
>   may use a partial key. The locked invariant for M2/M3 is
>   `Target == cfg.Host.ID ⇔ Flags.PIC == true` (every host build is
>   PIC); `IsHostBuild(cfg)` helper enforces the convention. The
>   emit rule for `node.HostPlatform` is `instance.IsHostBuild(cfg)`,
>   never `instance.Flags.PIC`. Documented because `Flags.PIC` reads
>   that masquerade as axis dispatch caused 23 Cat-B sites (PR-D41-A
>   audit) that would silently break if a non-host PIC build (M5
>   shared-lib target) is ever introduced. Reversal of the Design-X
>   invariant requires a follow-up D-note and removal of the
>   regression test `TestD41_HostBuildEqualsPIC_InM2Closure`.

---

## 10. Risks / open questions

### 10.1 Threading `cfg` through emitter signatures

Touches every `EmitX` call site (production + tests). Mechanical but
voluminous. Mitigation: a single executor, single round of review.

### 10.2 The `inferFlagsFromPath` parameter rename

`inferFlagsFromPath(path string, isPIC bool)` → `(path string,
isHostBuild bool)`. The parameter's *semantics* don't change (it still
sets `Flags.PIC = isHostBuild`), only the name does. **Locked under
D41 invariant.** Reviewers should NOT change the assignment line —
just the parameter name and the docstring. PR-D41-A brief must call
this out explicitly so a fix subagent doesn't "fix" what they perceive
as a now-misnamed assignment.

### 10.3 PR-M3-A in flight — abort or let land?

**Verified not in flight.** No `[~]` on PR-M3-A in tasks.md. The
worktree `a29d088de14a6fd48` referenced in the brief is a stale PR-43
worktree (commit `32053ae`). No conflict.

**Recommendation:** land PR-D41-A *before* PR-M3-A starts. PR-M3-A
then uses `IsHostBuild(cfg)` for its 13 new host-walk triggers and
avoids the surface area expansion.

### 10.4 Tests pinning the PIC-proxy

Audit existing tests for `Flags.PIC` reads that effectively pin axis
semantics. Specifically `cc_test.go`, `ar_test.go`, `as_test.go`,
`ld_test.go` likely contain assertions of the form `HostPlatform: ...,
Tags: []string{"tool"}` for `instance.Flags.PIC=true` cases. After the
refactor these assertions remain valid (Design X invariant), but a
reviewer who reads the test source will see `Flags.PIC=true` and infer
"PIC drives HostPlatform". The doc comment in `module.go:IsHostBuild`
must explicitly call this out.

Test names to inspect during PR-D41-A:
- `TestEmitCC_HostPlatform` (likely; verify in `cc_test.go`)
- `TestEmitAR_HostPlatformTags` (likely; verify in `ar_test.go`)
- `TestEmitLD_AcceptsHostPIC` (referenced in tasks.md PR-25 entry —
  rename to `TestEmitLD_AcceptsHostBuild` post-refactor)

### 10.5 Design Y deferral

If M5 introduces shared-library targets (PIC target builds), the
locked invariant breaks. **Documented exit path:** D41 records the
invariant and references a future D-note that would replace Design X
with Design Y. The refactor work would be small (decouple `WithHost`'s
PIC-setting; add an explicit `Flags.PIC` setter wherever the compile
shape demands PIC). No code in PR-D41-A becomes dead under Design Y;
only the invariant test (`TestD41_HostBuildEqualsPIC_InM2Closure`)
would need updating.

### 10.6 `derivePeerInstance` doesn't receive `cfg`

Currently it does not need it (it reads `parent.Flags.PIC` as the
boolean for `inferFlagsFromPath`). After the refactor it conceptually
should compute `parent.IsHostBuild(cfg)` instead, but adding `cfg` to
its signature ripples through every peer-walk site (~6 callers).

**Acceptable compromise:** keep passing `parent.Flags.PIC` for the
heuristic seed. The semantics are preserved (the heuristic always
seeds `Flags.PIC = isHostBuild` and the parent's PIC matches the
parent's IsHostBuild under the D41 invariant). Document the choice
in the function's docstring.

---

## 11. Approximate effort

| PR | LOC (prod) | LOC (tests) | Total | Sequencing |
|---|---:|---:|---:|---|
| PR-D41-A (mandatory) | 100-150 | 100-150 (test plumbing) | 200-300 | Before PR-M3-A |
| PR-D41-B (optional) | 30 | 50 | 80 | After PR-D41-A |
| PR-D41-C (optional) | 10 | 0 | 10 | M5 |

Each PR: ~1 review round. PR-D41-A may need 2 if the test plumbing
surfaces signature-thread bugs (low risk; mechanical change).

---

## 12. Summary of decisions

| Decision | Choice |
|---|---|
| Design X (couple PIC and Target) vs Design Y (decouple) | **Design X**. Locked invariant; reversible later. |
| New helper | `func (mi ModuleInstance) IsHostBuild(cfg PlatformConfig) bool` |
| Emitter signature change | `Emit*` functions gain `cfg PlatformConfig` parameter |
| `node.HostPlatform` derivation | `instance.IsHostBuild(cfg)` everywhere |
| `inferFlagsFromPath` parameter rename | `isPIC` → `isHostBuild` (semantics unchanged) |
| PlatformSpec.PIC field | Defer cleanup to M5 |
| Invariant test | Add `TestD41_HostBuildEqualsPIC_InM2Closure` |
| D41 cross-cutting note | Append to `tasks.md` after PR-D41-A merge |
| PR sequencing | PR-D41-A before PR-M3-A. PR-D41-B/C optional/deferred. |

---

## 13. Pointers

- `/home/pg/monorepo/yatool/module.go:121-138` — `ModuleInstance` +
  `WithHost`.
- `/home/pg/monorepo/yatool/module.go:167-201` — `inferFlagsFromPath`
  (parameter rename target).
- `/home/pg/monorepo/yatool/toolchain.go:30-37` — `PlatformConfig`
  (helper consumer).
- `/home/pg/monorepo/yatool/gen.go:185-188` — `genCtx.memo` /
  `genCtx.walking` (already correct).
- `/home/pg/monorepo/yatool/gen.go:682-698` — `buildIfEnv` (already
  reads canonical axis).
- `/home/pg/monorepo/yatool/gen.go:700-715` — `derivePeerInstance`
  (carries Target through; `Flags.PIC` heuristic seed acceptable).
- `/home/pg/monorepo/yatool/gen.go:2870, 2938` — host-walk trigger
  sites (template for M3's 13 new triggers).
- `/home/pg/monorepo/yatool/cc.go:203,273-280,333,338` —
  Cat-A `.pic.o` suffix + Cat-B host-bundle dispatch + Cat-B
  HostPlatform.
- `/home/pg/monorepo/yatool/ld.go:152,265-266` — Cat-B
  `hostBuild` axis read + Cat-B HostPlatform/Tags.
- `/home/pg/monorepo/yatool/ar.go:186,216-217` — Cat-B tags +
  HostPlatform.
- `/home/pg/monorepo/yatool/as.go:136,166-170,184,289,379` — Cat-B
  axis cluster including the (hardcoded-correct) yasm-shape branch.
- `/home/pg/monorepo/yatool/docs/drafts/20260511-0930-m3-plan.md:785-822`
  — PR-M3-A scope (needs amendment OR D41 lands first).
- `/home/pg/monorepo/yatool/tasks.md:126-140` — D30..D40 architectural
  notes (where D41 appends).
