# 2026-05-13 — Absolute-final probe: residual mismatch vs sg2.json

## Ground truth

`./yatool make -j 0 -G devtools/ymake/bin > ./.out/m3.json` vs
`/home/pg/monorepo/yatool_orig/sg2.json`:

| Layer | Ours | Ref  | Match               |
|-------|------|------|---------------------|
| L0    | 8750 | 8750 | 100.0000 %          |
| L1    | 8750 | 8750 | 100.0000 % (common) |
| L2    | —    | —    |  99.9886 % (1 miss) |
| L3    | —    | —    |  99.9771 % (2 miss) |

Total residue: **3 nodes** (not 17 — the prompt referenced an older run).
The three nodes form **2 distinct defect clusters**.

## Defect 1 — ragel-5 intermediate as CC input  (L2)

**Node:** `$(BUILD_ROOT)/tools/struct2fieldcalc/parsestruct.rl5.cpp.pic.o`
(`default-linux-x86_64`, p=CC).

Diff:

```
-REF: $(BUILD_ROOT)/tools/struct2fieldcalc/parsestruct.rl.tmp
```

REF lists the `.rl.tmp` *intermediate* output of the ragel step as an
input to the downstream CC node. OURS does not. The R5 producer node
itself (`p=R5`, two outputs `.rl.tmp` + `.rl5.cpp`) is byte-identical
between OURS and REF (same inputs, same cmd_args). The defect is in the
CC node's `inputs` propagation: when a producer emits two outputs, REF
adds **both** to the consumer's `inputs`; OURS adds only the one named
by the consumer's command line (`parsestruct.rl5.cpp`).

`cmd_args` for this CC node are identical between OURS and REF — `.rl.tmp`
is not referenced on the command line, only listed in `inputs`. This is
purely an "inputs-listing" gap.

**Upstream behaviour** (`build/conf/ragel.conf` & `tools/struct2fieldcalc/ya.make`
use `SRCS(parsestruct.rl5)`, expanded via `BUILDWITH_RAGEL5`). ymake's
graph emitter declares every output of the producer as an "uid-side"
dependency of any consumer that reaches it, even when only one is
file-referenced.

**Proposed fix.** When threading `inputs` for a CC node, walk all
declared outputs of every producer node that lands in the closure of the
CC node, not just the file whose name appears in the command. Concretely:
in `gen.go` (CC composer), after collecting "source files", merge in
sibling outputs of any node that produces one of those sources.

**Severity:** minor. Single-node, no command-line consequence, no
build-correctness consequence; cosmetic for graph equivalence.

## Defect 2 — Missing `-I…/abseil-cpp` via back-peer cycle  (L3, x2)

**Nodes** (both on `default-linux-aarch64`, p=CC):

- `$(BUILD_ROOT)/library/python/symbols/module/module.cpp.py3.o`
- `$(BUILD_ROOT)/library/python/symbols/module/library.python.symbols.module.syms.reg3.cpp.py3.o`

Diff (identical for both):

```
-REF: -I$(SOURCE_ROOT)/contrib/restricted/abseil-cpp
```

`inputs` are identical between OURS and REF; the only difference is the
missing `-I` slot. All other 1179 CC nodes that include this `-I` in
REF also include it in OURS.

### Root cause — back-peer cycle

`library/python/symbols/module/ya.make` declares `PY23_LIBRARY()`. The
PY3 variant of this multimodule is **peered FROM**
`contrib/libs/python/ya.make`:

```ya.make
# contrib/libs/python/ya.make, lines 22–26
PEERDIR(
    library/python/symbols/module     # ← cycle leg 1
    library/python/symbols/libc
    library/python/symbols/python
)
```

`contrib/libs/python` is itself a `PY23_LIBRARY`. Its PY3 variant
recursively peers:

```
contrib/libs/python (PY3)
  → library/python/runtime_py3          [PY3_LIBRARY]
      → library/cpp/resource            [LIBRARY]
          → library/cpp/containers/absl_flat_hash   [LIBRARY]
              → contrib/restricted/abseil-cpp        ← GLOBAL ADDINCL
```

`contrib/restricted/abseil-cpp/ya.make:20–22` declares
`ADDINCL(GLOBAL contrib/restricted/abseil-cpp)`.

ymake resolves the cycle and propagates the GLOBAL ADDINCL **back into
the module that started the cycle** (`symbols/module`). Our scanner
does not: it computes the peer-closure of `symbols/module` strictly
forward (`symbols/module` → `symbols/registry` only), missing the
back-edge from `contrib/libs/python`.

`library/cpp/pybind` (9 sibling `.py3.o` nodes that lack this `-I`) is
declared `PY23_NATIVE_LIBRARY`, and `contrib/libs/python` does **not**
PEERDIR it back — no cycle, no abseil. This matches REF exactly,
confirming the rule.

**Proposed fix.** In the peer-closure computation (composers in
`gen.go`, peer walker in scanner), when computing the GLOBAL-ADDINCL
contribution for a CC node belonging to module M, also include the
GLOBAL ADDINCLs of every module that PEERDIRs M (i.e. follow reverse
peer-edges one hop and re-walk forward closure from there). Equivalent
upstream semantics: ymake treats the strongly-connected component of
peer-edges as a single unit for GLOBAL propagation.

A safer narrower variant: limit reverse-walk to multimodule self-loops
(PY23_LIBRARY peered by `contrib/libs/python` and similar small
white-listed cases). This avoids invalidating the cache for the 1179
non-cycle nodes that already match.

**Severity:** minor. Two nodes; missing `-I` to a header directory that
their sources do not actually `#include` (this is "transitive default
ADDINCL leakage" — semantically present in REF, semantically harmless
in practice). The fix is real but its blast radius needs care.

## Cluster summary & minimal PR list

Two clusters → two PRs (independent, can land in either order):

1. **PR-M3-residue-A: emit sibling outputs of multi-output producers as
   consumer `inputs`.** Fixes Defect 1 (the `.rl.tmp` slot). Touches the
   CC-composer's input-list assembly only; no command-line change.

2. **PR-M3-residue-B: propagate GLOBAL ADDINCL through reverse peer
   edges (back-peer cycles).** Fixes Defect 2 (both abseil-cpp slots).
   Touches the peer-closure walker. Risk: must not regress the 1179
   nodes that currently match. Recommend implementing as a separate
   reverse-edge pass merged before flag assembly, gated by a unit test
   pinning the 1179-vs-2 invariant.

After both: **L0 = L1 = L2 = L3 = 100.0000 % vs sg2.json.**
