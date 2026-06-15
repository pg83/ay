# ADDINCL ordering: ADDINCLSELF's `${MODDIR}` lands LAST, not first

## Symptom

sg6 divergence cascade. On the single-instance node
`$(B)/contrib/deprecated/bdb/_/src/rep/rep_record.c.o` the `cmds` token
multiset is identical between ours and ref, but the **order** of the `-I` flags
differs, producing a different `self_uid` that cascades to ~276 CC roots.

Concretely, the module's own dir `-I$(S)/contrib/deprecated/bdb` (added by
`ADDINCLSELF()`):

- **ours**: position 19 — **first** in the module's `-I` group
- **ref** : position 45 — **last** in the module's `-I` group

We emitted ADDINCLSELF's self-dir at its *declaration* position in
`d.addIncl`. Upstream puts it last. This doc proves *why* last, from ymake C++.

## Mechanism (proven in devtools/ymake)

### 1. Statements are processed in priority order, not declaration order

`TModuleDef::GetMakeFileMap()` returns a `TMakeFileMap`, a
`TMultiMap<TPrioStatement, ...>` where `TPrioStatement = pair<size_t prio, TStringBuf name>`
(module_loader.h). The map iterates ordered by `(prio, name)`.

`TMakeFileMap::Add` (module_loader.h:~40):

```cpp
prio = (prio << 24) + (multi ? size() : 0);
```

So for a given base priority, **non-multi** statements all share the same key
`prio = base<<24` (and merge — `find`+append args), while **multi** statements
get a strictly increasing insertion offset `+ size()`.

`StatementPriority` (module_loader.cpp:38): SRCS/PY_SRCS=4, `_ADD_PY_LINTER_CHECK`=5,
PEERDIR/SRCDIR/_LATE_GLOB=1, a few=3, **everything else (incl. ADDINCL and
ADDINCLSELF) = 2 (default)**.

### 2. ADDINCL is non-multi; ADDINCLSELF is multi

`TModuleDef::IsMulti` (module_loader.cpp:216):

```cpp
return name == "ALLOCATOR" || ... || name == _LATE_GLOB || ...
       || IsUserMacro(name) || Conf.FindPluginMacro(name) != nullptr;
```

- `ADDINCL` — builtin `DirStatement` (module_builder.cpp:625), **not** a user
  macro → `IsMulti=false`.
- `ADDINCLSELF` — defined in `build/ymake.core.conf:3177`
  `macro ADDINCLSELF(FOR="") { ... ADDINCL += ${MODDIR} ... }`.
  `IsUserMacro(name)` = `Conf.BlockData[name].IsUserMacro` (module_loader.cpp:209)
  → true → **`IsMulti=true`**.

Therefore:

```
prio(ADDINCL)     = (2<<24) + 0
prio(ADDINCLSELF) = (2<<24) + size()    // size() >= 1 at insert time
```

`prio(ADDINCLSELF) > prio(ADDINCL)` → **ADDINCL is processed before
ADDINCLSELF**, regardless of which was written first in `ya.make`. All the
explicit `ADDINCL(...)` dirs merge into the single non-multi entry and land
ahead of ADDINCLSELF's `${MODDIR}`.

### 3. The ADDINCL var flush adds dirs in processing order

`InterpretMakefile` (module_builder.cpp:508-523):

```cpp
ApplyVarAsMacro(ADDINCL);                       // L510, pre-loop: global defaults only
for (statement : GetMakeFileMap()) TryProcessStatement(...);  // L511, priority order
ApplyVarAsMacro(ADDINCL, true);                 // L523, post-loop force
```

and per-statement `ProcessStatement` ends with `ApplyVarAsMacro(ADDINCL)` (L558).

`ApplyVarAsMacro` (module_builder.cpp:565) flushes `Vars["ADDINCL"]` to
`DirStatement(ADDINCL, args)` once per distinct value-hash (`VarMacroApplied`
dedup; `force` bypasses it). `DirStatement` → `AddIncdir`, which pushes into
`TModuleIncDirs` buckets backed by `TDirs : TUniqVector` — **first-insertion
wins, order preserved** (uniq_vector.h:22,33; dirs.h:14).

Sequence for bdb:

1. L510 pre-loop flush: `Vars["ADDINCL"]` holds only inherited/global dirs.
2. Loop, priority 2, **non-multi `ADDINCL` first** (name merge): explicit
   `ADDINCL(__dirs_)` + `ADDINCL(src/os)` → `AddIncdir(__dirs_, src/os)`.
3. Loop, priority 2, **multi `ADDINCLSELF` after**: its body does
   `ADDINCL += ${MODDIR}`; the trailing `ApplyVarAsMacro(ADDINCL)` (L558)
   flushes the now-changed var → `AddIncdir(MODDIR)`.

`MODDIR` enters the uniq vector **last** → emitted last in the `-I` group.
This is the observed ref ordering (position 45). ∎

## Fix

`modules.go:1154` (`case tokAddInclSelf:`) currently appends the self-dir
(`source(modulePath)`, or the cython/asm variants under `FOR cython`/`FOR asm`)
to `d.addIncl` at the statement's *declaration* position. To match upstream,
ADDINCLSELF's self-dir must be ordered **after all explicit `ADDINCL(...)` dirs**
of the same module — i.e. deferred to the end of the module's addincl group,
the same way `modules.go:516` defers `d.cfAddIncl` after `collectStmts`.

Verification plan: apply the deferral, then `./dev/validate.py` (foreground) —
sg2/sg2_x86_64/sg3/sg4/sg5 must stay byte-exact, sg6 CC-root count must drop.

## References (devtools/ymake, read-only)

- `module_loader.h` — `TPrioStatement`, `TMakeFileMap`, `Add` (`prio<<24 + multi?size():0`)
- `module_loader.cpp:38` `StatementPriority`; `:209` `IsUserMacro`; `:216` `IsMulti`
- `module_builder.cpp:508-523` `InterpretMakefile` (3× `ApplyVarAsMacro(ADDINCL)`);
  `:558` per-statement flush; `:565` `ApplyVarAsMacro` (`VarMacroApplied` dedup);
  `:625` `ADDINCL` `DirStatement`
- `build/ymake.core.conf:3177` `macro ADDINCLSELF`
- `common/uniq_vector.h:22,33` (Push-to-end, dedup-keeps-first); `dirs.h:14` `TDirs : TUniqVector`
