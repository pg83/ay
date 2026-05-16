# PR-29 — Flag threading + EmitCCFromBuildRoot + host-musl bundle

## Problem

After PR-28's dual-platform structural pass, `tools/archiver`
gen yields 3321 nodes (1739 target + 1582 host) and the
comparator reports L0 = 88.34%, L1 = 88.66%, L2 = 86.89%,
**L3 = 36.46%** (1360 / 3307 paired byte-exact). The M2 acceptance
gate requires **L3 ≥ 50%** on the same set — a gap of 14 percentage
points, ≈ 460 paired pairs that need to flip from "paired but
cmd_args/env diverge" to "byte-exact".

The PR-28 plan doc earmarked PR-29 for the **host-musl CC
bundle** plus DEFAULT_ALLOCATOR / abseil-cpp follow-up. The PR-28
ledger entry rewrote that scope into "thread CXXFLAGS / CONLYFLAGS
/ SRCDIR / ADDINCL through EmitCC + EmitCCFromBuildRoot". An
empirical divergence-by-token probe of the 1947 paired-but-diverging
nodes (`./yatool make -j 0 -G tools/archiver` vs the reference)
shows **the dominant lever is host-musl, not flag threading**:

- **1297 of 1947 divergent CC pairs** are host musl nodes
  (`module_dir = contrib/libs/musl`, platform =
  `default-linux-x86_64`). Today our `composeMuslCC` ignores
  `instance.Flags.PIC` and emits the target-flavour 111-arg
  bundle for every musl module, host included. The reference's
  host musl bundle is 115 args (host-CC scaffolding + musl-specific
  x86_64 includes + musl extra defines). Fixing this single bucket
  is a clean +39pp on L3 (1297/3307 = 39.2pp).
- **329 nodes** are `contrib/libs/cxxsupp/builtins`. They diverge
  on 5 musl-arch `-I` paths the builtins ya.make declares via
  its own `IF (MUSL) ADDINCL(...)` block (non-GLOBAL, applies to
  builtins itself). Closing this requires per-module ADDINCL
  threading.
- **116 + 14 + 14 + 20 + smaller** = ~200 nodes are libcxx /
  libcxxrt / libunwind / util / various peers that need
  `-std=c++20` (CXXFLAGS), `clang++` (C++-language switch),
  per-module ADDINCL, and peer-propagated GLOBAL ADDINCL from
  musl/libcxx/libcxxrt/zlib/double-conversion/libc_compat/etc.
- **24 nodes** are JS-derived / R6-derived CC nodes whose
  generated source lives in `$(BUILD_ROOT)`; today we emit
  `$(SOURCE_ROOT)/...` as the input path. The known
  PR-25-D07 / PR-25-deferred `EmitCCFromBuildRoot` gap.

PR-29 ships in three independently verifiable D-tasks per work
class. The host-musl bundle alone clears the L3 ≥ 50% gate (36.46
+ 39.2 = ~75% optimistic, ~58% conservative after accounting for
secondary divergences inside those nodes). The remaining items
make the pass durable for PR-30/31 and close the L2-L3 gap on
the main archiver chain.

## Empirical findings

All probes run against `/home/pg/monorepo/yatool_orig/g.json`
(reference) and `./yatool make -j 0 -G tools/archiver` (current
HEAD output, 3321 nodes). Probe source archived at
`./.tmp/probe/probe.go` (gitignored).

### Divergence by op-type

| kv.p | divergent | total paired |
|------|-----------|--------------|
| AR   | 5         | 35           |
| AS   | 56        | 56           |
| CC   | 1884      | 3198         |
| JS   | 0         | 16           |
| LD   | 1         | 1            |
| R6   | 1         | 1            |

The CC bucket dominates (1884/1947 ≈ 96.8% of divergence). AS is
56/56 — every paired AS pair diverges (deferred to PR-30/31; host
asmlib + per-module includes + Cwd asymmetry per PR-24 known
constraint). LD has one divergent pair (the archiver binary itself
— investigate but likely a peer-archive ordering / VCS info detail
not in PR-29 scope). The AR 5/35 — small, deferrable.

### Top tokens that appear ONLY in want (most-frequent missing flags)

```
token                                                        occurrences  nodes-affected
-I$(SOURCE_ROOT)/contrib/libs/musl/arch/x86_64               1569         1569
-msse4.1 -msse3 -mpopcnt -mssse3 -O3 -mcx16 -msse2           1297×7       1297
-fPIC -DNDEBUG                                               2594×2       1297
-m64                                                         1297         1297
--target=x86_64-linux-gnu                                    1297         1297
-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE                 1297         1297
-msse4.2                                                     1297         1297
-I$(SOURCE_ROOT)/contrib/libs/musl/arch/generic              587          587
-I$(SOURCE_ROOT)/contrib/libs/musl/include                   587          587
-I$(SOURCE_ROOT)/contrib/libs/musl/extra                     587          587
-Wno-everything                                              660          511
-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64              315          315
-D_musl_                                                     258          258
-DCATBOOST_OPENSOURCE=yes                                    204          204
/ix/realm/boot/bin/clang++                                   204          204
-std=c++20                                                   204          204
-nostdinc++                                                  399          204
-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include         194          194
-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxxrt/include       194          194
-DLIBCXXRT                                                   116          116
-D_LIBCPP_BUILDING_LIBRARY                                   116          116
-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/src             116          116
-I$(SOURCE_ROOT)/contrib/libs/zlib/include                   60           60
-I$(SOURCE_ROOT)/contrib/libs/double-conversion              53           53
-I$(SOURCE_ROOT)/contrib/libs/libc_compat/include/readpassphrase 47        47
-I$(SOURCE_ROOT)/contrib/libs/mimalloc/include               26           26
-Woverloaded-virtual + 9 cxx-warning extensions              55           55
```

### Top tokens we emit that are NOT in want (incorrectly added)

```
token                                  occurrences  nodes-affected
-fdebug-default-version=4              1297         1297
-ggnu-pubnames                         1297         1297
--target=aarch64-linux-gnu             1297         1297
-march=armv8-a                         1297         1297
-mno-outline-atomics                   2594         1297
-fsigned-char                          1297         1297
-UNDEBUG                               2594         1297
-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64  1297  1297
-g                                     1297         1297
-fstack-protector                      1297         1297
-Wall -Wextra -Werror -Wno-parentheses 511          511
... (warning bundle)                   511×4        511
/ix/realm/boot/bin/clang               204          204
```

Reads as: "for 1297 host-musl nodes we emit aarch64+target flavour;
for 204 libcxx-style nodes we emit `clang` instead of `clang++` and
miss `-std=c++20`; for 511 nodes we emit the full `warningFlags`
bundle where `-Wno-everything` was wanted (cxxsupp/builtins +
cxxsupp/libcxxrt + various NO_COMPILER_WARNINGS modules)".

### Divergent CC by module_dir (top 14)

```
contrib/libs/musl                  1297   ← host-musl bundle (D01)
contrib/libs/cxxsupp/builtins       329   ← per-module ADDINCL (D03)
contrib/libs/cxxsupp/libcxx         116   ← CXXFLAGS + clang++ + ADDINCL (D02 + D03 + D05)
util                                 20   ← peer-propagated ADDINCL (D04)
contrib/libs/mimalloc                16   ← per-module CXXFLAGS+ADDINCL+CONLYFLAGS (D02 + D03)
library/cpp/getopt/small             16   ← peer-propagated ADDINCL + clang++ (D04 + D05)
contrib/libs/zlib                    15   ← per-module ADDINCL (D03)
contrib/libs/cxxsupp/libcxxrt        14   ← per-module ADDINCL + clang++ (D03 + D05)
contrib/libs/libunwind               14   ← per-module ADDINCL (D03)
contrib/tools/ragel6                  9   ← clang++ + per-module ADDINCL (D03 + D05)
contrib/libs/double-conversion        8   ← per-module ADDINCL (D03)
library/cpp/archive                   3
contrib/libs/libc_compat              2
library/cpp/malloc/api                2
```

### CXXFLAGS

Reference declarations actually consulted in M2 closure:

- `contrib/libs/cxxsupp/libcxx/ya.make:29` —
  `CXXFLAGS(-D_LIBCPP_BUILDING_LIBRARY)` — non-GLOBAL, 116 nodes.
- `contrib/libs/cxxsupp/libcxx/ya.make:68` —
  `CXXFLAGS(GLOBAL -nostdinc++)` — GLOBAL, propagates. Visible in
  every C++ consumer (e.g. getopt/small at cmd_args[115] and [117],
  libcxx itself at [101]/[103], etc.).
- `contrib/libs/mimalloc/ya.make` —
  `CONLYFLAGS(-Wno-narrowing)` (and similar). 16 nodes.

Estimated impact of typed CXXFLAGS / CONLYFLAGS threading:
≈ 116 nodes flip cleanly from D02 alone (libcxx). Mimalloc's 16
need CONLYFLAGS + ADDINCL + others to all line up; pessimistic
double-counted.

### CONLYFLAGS

- `contrib/libs/mimalloc/ya.make` — checked. Few nodes.
- `contrib/libs/zlib/ya.make` — none directly visible; CONLYFLAGS
  in M2 closure is small.

D02 closes <30 nodes alone but is a foundation block — required
before D04 can correctly route GLOBAL CXXFLAGS via peer
propagation.

### ADDINCL (own + GLOBAL peer-propagated)

Top divergence offenders by total `-I` flag count missing:

```
module                                      total -I divergences
contrib/libs/cxxsupp/builtins                1316    (own ADDINCL of musl arch)
contrib/libs/musl                            1297    (own ADDINCL of musl arch/x86_64
                                                      — gated by ARCH_X86_64 for host)
contrib/libs/cxxsupp/libcxx                   812
util                                          180
library/cpp/getopt/small                      144
contrib/tools/ragel6                           81
contrib/libs/mimalloc                          80
contrib/libs/zlib                              75
contrib/libs/libunwind                         70
contrib/libs/cxxsupp/libcxxrt                  56
```

The musl x86_64 -I (1297 nodes) is shared between D01 (host-musl
bundle) and D03 (own-ADDINCL). D01 will satisfy it for musl host
nodes; the same -I path appears propagated into builtins / libcxx
host nodes via peer-GLOBAL or own-IF-ARCH_X86_64 ADDINCL blocks.

### Generated sources (JS / R6 — EmitCCFromBuildRoot)

Reference CC nodes with input ending in `$(BUILD_ROOT)/...`:

- 24 paired pairs: 0 byte-exact (final input arg differs:
  `$(BUILD_ROOT)/...` in want vs `$(SOURCE_ROOT)/...` in got).
- 0 not-paired-with-us — every reference generated CC node has
  a yatool counterpart, just at the wrong inputPath.

Sample diverging path:

```
WANT: $(BUILD_ROOT)/contrib/tools/ragel6/all_other.cpp
 GOT: $(SOURCE_ROOT)/contrib/tools/ragel6/all_other.cpp
```

These are `JOIN_SRCS` outputs (`all_other.cpp` etc.) and ragel6
`.rl6.cpp` outputs. EmitJS / EmitR6 emit into `$(BUILD_ROOT)/...`
correctly; the downstream EmitCC then re-derives the path as
`$(SOURCE_ROOT)/<instance.Path>/<rel>` because EmitCC always
prepends `$(SOURCE_ROOT)/`. PR-25-D07 documented this gap;
PR-29-D07 closes it.

The 24-pair count is a **conservative ceiling** on what the
EmitCCFromBuildRoot fix alone closes — the actual flip count is
≤ 24 because each of these 24 nodes ALSO needs the matching
warning-bundle / CXXFLAGS / per-module flag to line up. For
modules like ragel6 (which uses `clang++` + `-std=c++20`), the
generated CC node only flips after both D05 and D07 land.

### Three composers (target / host / musl)

`composeTargetCC`: 101 args. M1-leaf-pinned (`build/cow/on/lib.c`).
`composeHostCC`: 105 args. M1-leaf-pinned
(`build/cow/on/lib.c.pic.o`).
`composeMuslCC`: 111 args. **PR-29 gap**: today's body is
target-only (uses `targetTriple`, `archFlag`, `commonCFlags`,
`commonDefines`, `noLibcUndebugBlock`). PIC instances flow into
this composer and emit aarch64 musl. Reference uses 115-arg host
musl variant — a fourth composer is needed.

The three composers consume flags via direct `append(cmdArgs, X...)`
of named bundles. There is no shared "extension point" parameter.
PR-29's `addIncl`/`cxxFlags`/`cOnlyFlags` integration must add a
struct-typed input (recommendation a/b below) and slot the per-module
flags at composer-specific positions:

- ADDINCL `-I` paths: AFTER `-I$(SOURCE_ROOT)` (ccIncludes[1])
  and BEFORE `-I$(SOURCE_ROOT)/contrib/libs/linux-headers`
  (ccIncludes[2]). Verified by sampling getopt/small (line 11+
  comes after BUILD_ROOT+SOURCE_ROOT, before linux-headers).
- CXXFLAGS (own, non-GLOBAL): AFTER second `noLibcUndebugBlock`
  (or `ndebugPicBlock`) AND AFTER catboostOpenSourceDefine.
  Verified by libcxx algorithm.cpp.o cmd_args[100] =
  `-D_LIBCPP_BUILDING_LIBRARY`. They sit between
  `-Wno-everything`/`-std=c++20` and the first `-nostdinc++`.
- CXXFLAGS (GLOBAL `-nostdinc++`): TWICE — once between
  catboostOpenSourceDefine and `hostSseFeatures` (or
  `noLibcUndebugBlock` second copy), once after second
  noLibcUndebugBlock. PR-29-D04 (peer-propagated GLOBAL)
  threads this. **Out of scope for D02 (own CXXFLAGS).**
- CONLYFLAGS: same slot as own CXXFLAGS but only for `.c`/`.S`
  sources (not `.cpp`/`.cc`/`.cxx`).

## Implementation plan

D-tasks numbered for PR-29. Independent tasks can be implemented
in parallel via `isolation: "worktree"` per CLAUDE.md.

### D01 — Host-musl CC bundle (`composeMuslHostCC`) [DOMINANT LEVER]

**File:** `cc.go`, `flags.go`, `cc_test.go`.

**Acceptance:** 1297 host-musl CC pairs (in
`tools/archiver` closure) flip to byte-exact at L3, OR very close
to that count (some may have secondary divergences exposed).

**Implementation:**

1. In `flags.go`, add `muslCcIncludesX8664` (10 args, replaces
   `arch/aarch64` with `arch/x86_64`):

   ```go
   var muslCcIncludesX8664 = []string{
       "-I$(BUILD_ROOT)",
       "-I$(SOURCE_ROOT)",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/arch/x86_64",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/arch/generic",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/src/include",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/src/internal",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/include",
       "-I$(SOURCE_ROOT)/contrib/libs/musl/extra",
       "-I$(SOURCE_ROOT)/contrib/libs/linux-headers",
       "-I$(SOURCE_ROOT)/contrib/libs/linux-headers/_nf",
   }
   ```

2. In `cc.go`, add `composeMuslHostCC(srcRel, outputPath, inputPath, modulePath string) []string`
   producing the 115-arg bundle. Pinned exactly against
   `$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.pic.o`.
   Skeleton:

   ```go
   cmdArgs := make([]string, 0, 115)
   cmdArgs = append(cmdArgs,
       ccCompilerPath,
       "--target="+hostTriple,
       "-B"+binPath,
       "-c", "-o", outputPath,
   )
   cmdArgs = append(cmdArgs, muslCcIncludesX8664...)
   cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
   cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
   cmdArgs = append(cmdArgs, hostCFlags...)
   cmdArgs = append(cmdArgs, muslWarningFlags...)
   cmdArgs = append(cmdArgs, hostDefines...)
   cmdArgs = append(cmdArgs, muslExtraDefines...)
   cmdArgs = append(cmdArgs, ndebugPicBlock...)
   cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
   cmdArgs = append(cmdArgs, hostSseFeatures...)
   cmdArgs = append(cmdArgs, ndebugPicBlock...)
   cmdArgs = append(cmdArgs, builtinMacroDateTime...)
   cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
   cmdArgs = append(cmdArgs, inputPath)
   ```

3. In `EmitCC` switch (cc.go:65-72), add a new case:

   ```go
   switch {
   case isMusl && instance.Flags.PIC:
       cmdArgs = composeMuslHostCC(srcRel, outputPath, inputPath, instance.Path)
   case isMusl:
       cmdArgs = composeMuslCC(srcRel, outputPath, inputPath, instance.Path)
   case instance.Flags.PIC:
       cmdArgs = composeHostCC(outputPath, inputPath)
   default:
       cmdArgs = composeTargetCC(outputPath, inputPath)
   }
   ```

4. New test `TestEmitCC_MuslHost_StrlenC_ByteExact` pinning the
   115-arg bundle against the reference node
   `$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.pic.o`
   (platform `default-linux-x86_64`).

**Verification:** `./yatool make -j 0 -G tools/archiver --out
.tmp/our.json && ./yatool compare --level=3 .tmp/our.json
/yatool_orig/g.json` — L3 jumps from 36.46% to ≥ 67% (1360 + 1297
= 2657 byte-exact / 3307 = 80.3% optimistic; conservative ≥ 67%
acknowledging some musl host nodes have secondary divergences
from peer-propagated ADDINCL we do not yet thread).

**Risk:** Some musl x86_64 nodes carry an extra `-D_musl_` token
between catboostOpenSourceDefine and second ndebugPicBlock that
the muslExtraDefines path already supplies via `-D_musl_=1`. Pin
the byte-exact test against the actual reference, not the prose;
empirically the value is `-D_musl_=1` in all musl arch sources.

### D02 — Per-module CXXFLAGS / CONLYFLAGS threading through EmitCC

**Files:** `cc.go`, `gen.go`, `cc_test.go`, `gen_test.go`.

**Acceptance:** libcxx's 116 nodes flip from "diverges by missing
`-D_LIBCPP_BUILDING_LIBRARY` (and second `-nostdinc++`)" to "byte-
exact OR diverges only on remaining ADDINCL / clang++ axes". Net
flip after D02 alone: ≈ 30 nodes (libcxx nodes that ALSO need D03
/ D05 still diverge).

**Implementation:**

1. Bundle the new per-module inputs into a struct (recommendation
   `b` from §Decisions). Add to `cc.go`:

   ```go
   // ModuleCCInputs carries per-module compile knobs that vary
   // between modules in the same closure but stay constant per
   // (instance, source). Threaded through EmitCC by the walker.
   //
   // PR-29: addIncl, cxxFlags, cOnlyFlags wired. isGenerated
   //   wired in D07. Extending the struct does not require
   //   updating signatures — that is the whole point of using
   //   a struct here.
   type ModuleCCInputs struct {
       AddIncl     []string  // own ADDINCL paths (declaration order)
       CXXFlags    []string  // own CXXFLAGS args (cpp/cc/cxx only)
       COnlyFlags  []string  // own CONLYFLAGS args (.c/.S only)
       IsGenerated bool      // when true, srcRel resolves under $(BUILD_ROOT) (D07)
   }
   ```

2. Change `EmitCC` signature (post-PR-12 today is
   `EmitCC(instance, srcRel, emit) (NodeRef, string)`) to:

   ```go
   func EmitCC(instance ModuleInstance, srcRel string, in ModuleCCInputs, emit Emitter) (NodeRef, string)
   ```

3. The four composers get an `extras` parameter carrying the
   already-filtered slice (CXXFLAGS for C++ sources, CONLYFLAGS
   for C sources, both empty otherwise) plus the ADDINCL slice
   (D03 wires this; D02 leaves AddIncl unused so D02 can land
   independently — leaves an explicit comment naming D03).

4. EmitCC dispatches CXXFlags vs COnlyFlags based on `srcRel`
   suffix:

   ```go
   var ownExtras []string
   if isCxxSource(srcRel) {
       ownExtras = in.CXXFlags
   } else {
       ownExtras = in.COnlyFlags
   }
   ```

5. Each composer slots `ownExtras` between catboostOpenSourceDefine
   and the second `noLibcUndebugBlock`/`ndebugPicBlock`. The slot
   matches reference libcxx algorithm.cpp.o cmd_args[100..103] =
   `-D_LIBCPP_BUILDING_LIBRARY -nostdinc++ -DCATBOOST_OPENSOURCE=yes
   -nostdinc++` — wait, this is tricky: the reference inserts
   `-D_LIBCPP_BUILDING_LIBRARY` BEFORE the first `-nostdinc++`,
   then catboostOpenSourceDefine, then the second `-nostdinc++`.
   Empirical re-check: at libcxx algorithm.cpp.o:
     [98] `-std=c++20`
     [99] `-Wno-everything`     ← (NO_COMPILER_WARNINGS on libcxx)
     [100] `-D_LIBCPP_BUILDING_LIBRARY`  ← own CXXFLAGS
     [101] `-nostdinc++`        ← peer-GLOBAL CXXFLAGS (libcxx → libcxx)
     [102] `-DCATBOOST_OPENSOURCE=yes`
     [103] `-nostdinc++`        ← peer-GLOBAL CXXFLAGS (repeated)
   So: own CXXFLAGS slot = AFTER the second `noLibcUndebugBlock`
   AND AFTER warning-suppression (`-Wno-everything` at index 99),
   BEFORE peer-propagated GLOBAL (D04 wires this).
   
   For PR-29-D02 (own only): slot AFTER catboostOpenSourceDefine
   in target/host bundles is wrong because catboost sits BETWEEN
   the two suppression blocks. Reference data shows own CXXFLAGS
   in the **post-second-suppression-block** zone, just before
   builtinMacroDateTime. Concrete slot: between line emitting
   `noLibcUndebugBlock` (second copy) and line emitting
   `builtinMacroDateTime`.
   
   For NO_COMPILER_WARNINGS modules (libcxx etc.) the warningFlags
   are replaced by `-Wno-everything` (single arg) — D02 must also
   thread an `instance.Flags.NoCompilerWarnings` switch into the
   composer. The flag is collected today (gen.go applies
   `NO_COMPILER_WARNINGS` → `d.flags.NoCompilerWarnings`); the
   composers do not yet consume it. Add a "warnings vs no-everything"
   selector in each composer.

6. The walker's `emitOneSource` (gen.go:1084) passes
   `ModuleCCInputs{CXXFlags: d.cxxFlags, COnlyFlags: d.cOnlyFlags}`
   to EmitCC. The walker must also pass these into the JS-derived
   downstream EmitCC (gen.go:910) and the R6-derived downstream
   EmitCC (gen.go:1205) — both currently call EmitCC with a bare
   `(srcInstance, jsRel, ctx.emit)` triple.

**Risk:** Per-module CXXFLAGS slot may be reference-specific
(libcxx puts CXXFLAGS before peer-GLOBAL CXXFLAGS, but mimalloc
or another module may differ). Plan: pin libcxx algorithm.cpp.o
byte-exact first, then audit one mimalloc node to confirm slot
universality before claiming D02 closed.

### D03 — Per-module ADDINCL threading

**Files:** `cc.go`, `gen.go`, `cc_test.go`.

**Acceptance:** builtins (329 nodes) and zlib (15 nodes) flip
their per-module ADDINCL injections. libcxx own ADDINCL (libcxx
src + libcxxrt include) wires.

**Implementation:**

1. Builds on D02's `ModuleCCInputs` struct. The `AddIncl []string`
   field is consumed by all four composers.
2. Insertion slot: per the `getopt/small` reference sample (cmd_args
   [9..19]), per-module ADDINCL goes AFTER the baseline
   `-I$(BUILD_ROOT) -I$(SOURCE_ROOT)` pair AND AFTER
   `-I$(SOURCE_ROOT)/contrib/libs/linux-headers{,/_nf}`. Wait —
   re-check builtins fp_mode.c.o (cmd_args[7..14]):
     [7] `-I$(BUILD_ROOT)`
     [8] `-I$(SOURCE_ROOT)`
     [9..12] `-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64`,
             `arch/generic`, `include`, `extra`     ← own ADDINCL
     [13..14] `-I$(SOURCE_ROOT)/contrib/libs/linux-headers{,/_nf}`
   So **own ADDINCL goes BETWEEN the baseline pair (BUILD_ROOT +
   SOURCE_ROOT) AND the linux-headers pair**. Concrete: split
   `ccIncludes` into `ccIncludesPrefix` (first 2 entries) and
   `ccIncludesSuffix` (last 2 entries); composer emits prefix +
   ADDINCL + suffix.
3. For getopt/small (a non-MUSL non-builtins LIBRARY), the
   ADDINCL slot continues with peer-propagated GLOBAL ADDINCL
   from libcxx, libcxxrt, musl, zlib, double-conversion,
   libc_compat. These are out of D03 scope (D04). D03 threads
   own-module ADDINCL only.
4. The walker (gen.go:880, :910, :921, :1103, :1139, :1205)
   passes `d.addIncl` into ModuleCCInputs for own-source emit.
   The downstream-of-JS / downstream-of-R6 EmitCC calls also
   need it.
5. ADDINCL paths in ya.make are SOURCE_ROOT-relative; the
   composer must prepend `-I$(SOURCE_ROOT)/`. Verified by builtins'
   `ADDINCL(contrib/libs/musl/arch/aarch64 ...)` → cmd_args
   `-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64`.

**Risk:** Order within own ADDINCL block matters (musl/arch/X
must come before musl/arch/generic for `include_next` chain).
The walker preserves declaration order via `append`; verify no
sort upstream.

### D04 — Peer-propagated GLOBAL ADDINCL / GLOBAL CXXFLAGS [LARGE; can defer to PR-30]

**Files:** `gen.go` (collectModule), `cc.go`.

**Acceptance:** libcxx's GLOBAL `-nostdinc++` propagates to every
C++ consumer (getopt/small, util, archiver itself). musl's
implied GLOBAL `-D_musl_=1` (from `CFLAGS(GLOBAL -D_musl_=1)` in
musl ya.make) propagates. This is what reference libcxx
algorithm.cpp.o's cmd_args[101]/[103] = `-nostdinc++` represents.

**Implementation note:** The yamake parser already produces
`AddInclStmt`/`CFlagsStmt` with a `Global bool` field today
(verified at `yamake.go` AST). The walker collects everything
into `d.addIncl`/`d.cFlags` without distinguishing GLOBAL. To
thread GLOBAL across PEERDIR boundaries, `moduleEmitResult`
needs new `GlobalAddIncl []string` / `GlobalCXXFlags []string`
fields, and the walker must aggregate peer GLOBAL from
`peerArchiveRefs`'s `peerResult.GlobalAddIncl` slices into the
parent's `ModuleCCInputs.AddIncl`.

**This is a large piece of work — recommend deferring to PR-30**
unless the orchestrator explicitly enlarges PR-29 scope. Without
D04, libcxx own (D02) flips lib_cxx_building_library but not
the peer-propagated `-nostdinc++` pair, so libcxx algorithm.cpp.o
remains divergent. Net impact estimate without D04:
  - libcxx: ~30 of 116 nodes (those with no peer-CXXFLAGS
    requirement) — pessimistic, possibly fewer.

**Out of scope for PR-29-D04 baseline:** mark as deferred-to-PR-30.
PR-30 also closes `-DLIBCXXRT` (libcxxrt's GLOBAL CFLAGS) and the
9-arg cxx-warning-extension bundle libcxx propagates to its 55
peers.

### D05 — `clang++` switch + `-std=c++20` for C++ sources

**Files:** `cc.go`, `flags.go`, `cc_test.go`.

**Acceptance:** 204 nodes that differ on `clang` vs `clang++` flip
correctly — but only when combined with D02 (which adds
`-std=c++20`).

**Implementation:**

1. Add `cxxCompilerPath = "/ix/realm/boot/bin/clang++"` to
   flags.go.
2. Add `cxxStandardFlag = "-std=c++20"`.
3. EmitCC dispatches per source extension:
   ```go
   compiler := ccCompilerPath
   if isCxxSource(srcRel) {
       compiler = cxxCompilerPath
   }
   ```
4. `-std=c++20` slots AFTER the second noLibcUndebugBlock /
   ndebugPicBlock and BEFORE own CXXFLAGS (D02 slot). Concretely,
   for C++ sources only.
5. The four composers each receive a `compiler` parameter (replaces
   the hardcoded `ccCompilerPath` literal).
6. Empirical confirmation of slot: libcxx algorithm.cpp.o
   cmd_args[98] = `-std=c++20`, [99] = `-Wno-everything`,
   [100] = own CXXFLAGS. So `-std=c++20` precedes
   `-Wno-everything` (when NoCompilerWarnings) — needs ordering
   check against a non-NoCompilerWarnings C++ module
   (e.g. `library/cpp/getopt/small`):
     [104] `-std=c++20`        — same slot
     [105..114] cxx-warning-extension bundle (the
       `-Woverloaded-virtual` + 9 `-Wno-deprecated-...` set)
   So slot = right AFTER the second suppression block, before
   peer-propagated CXX warning extensions (D04) and own CXXFLAGS
   (D02).

**Note:** isCxxSource = HasSuffix `.cpp`/`.cc`/`.cxx`. `.c` →
clang. `.S`/`.s` → AS rule (not EmitCC). R6-generated `.cpp`
output goes through EmitCC with `.cpp` → clang++. Verified
against ragel6's `all_other.cpp.pic.o` which uses clang++.

### D06 — NO_COMPILER_WARNINGS suppression bundle selection

**Files:** `cc.go`.

**Acceptance:** 511 nodes that diverge on
`-Wno-everything` vs `-Werror -Wall ...` flip. cxxsupp/builtins
+ cxxsupp/libcxxrt + libcxx + musl already use this; the gap
is for non-musl modules that declared NO_COMPILER_WARNINGS
explicitly (libcxxrt being the prime example, currently using
target's warningFlags bundle).

**Implementation:**

1. EmitCC consults `instance.Flags.NoCompilerWarnings`. When set,
   substitute `muslWarningFlags` (`-Wno-everything`) for
   `warningFlags` in target/host composers.
2. For musl path, already uses muslWarningFlags unconditionally.
3. The flag is collected today (collectStmts handles
   `NO_COMPILER_WARNINGS` → `d.flags.NoCompilerWarnings`).
   Verification grep: `applyUnknownStmt` case
   `"NO_COMPILER_WARNINGS"`: yes, present. Just unused by
   composers.

**Wins:** ≈ 30 builtins/libcxxrt/libunwind nodes flip (where
NO_COMPILER_WARNINGS is the only divergence). Larger blocks
(libcxx, musl) already use muslWarningFlags through D01.

### D07 — EmitCCFromBuildRoot variant via IsGenerated bool

**Files:** `cc.go`, `gen.go`, `gen_test.go`, `r6_test.go`,
`js_test.go`.

**Acceptance:** 24 generated-source CC pairs flip from `$(SOURCE_ROOT)/...`
input path to `$(BUILD_ROOT)/...`. Combined with other D-tasks
(D02 / D03 / D05), these become byte-exact.

**Implementation (recommendation: extend the struct, do NOT
fork EmitCC):**

1. Add `IsGenerated bool` to `ModuleCCInputs` (D02 already
   declared this field; D07 wires the consumer).
2. EmitCC line 59 today:
   ```go
   inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
   ```
   becomes:
   ```go
   var inputPath string
   if in.IsGenerated {
       inputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel
   } else {
       inputPath = "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
   }
   ```
3. Walker call sites: at gen.go:910 (JS-derived EmitCC) and
   gen.go:1205 (R6-derived EmitCC), pass
   `ModuleCCInputs{IsGenerated: true, ...}`.
4. EmitCC additionally threads the JS / R6 NodeRef into the new
   CC node's `DepRefs` so the generator becomes a real
   dependency. Caller passes a `Generator NodeRef` — add to
   `ModuleCCInputs`. Reference verified: generated CC nodes
   in g.json have `deps: [<JS or R6 UID>]`.

**Risk:** `EmitCC` today does not populate `DepRefs`. Adding the
generator dep changes the UID. Pin via existing
TestGen_BuildCowOn_TwoNodeSubgraph_L3MatchesReference (no
generators in that closure — should not regress). Add new
TestEmitCC_FromBuildRoot_ThreadsGenerator_Dep that pins the
input path AND DepRefs against a JS-derived reference node.

### D08 — Tests + documentation sweep

**Files:** all touched in D01-D07.

1. Update existing CC tests to consume new EmitCC signature
   (pass empty `ModuleCCInputs{}` for current pinning tests
   except `TestEmitCC_BuildCowOnLibC_ByteExact` and
   `TestEmitCC_BuildCowOnLibC_HostPIC_ByteExact` — both need to
   stay green with empty inputs).
2. Add `TestEmitCC_MuslHost_StrlenC_ByteExact` (D01 pin).
3. Add `TestEmitCC_LibCxx_AlgorithmCpp_OwnCXXFLAGS_BytePartial`
   (D02 + D05 + D06: pins everything except peer-GLOBAL pieces;
   asserts the 5+ tokens that D02 introduces appear at the right
   slot. Marked partial because D04 deferred).
4. Add `TestEmitCC_GeneratedSource_BuildRootInput` (D07 pin).
5. Comment update at gen.go:32-34 — remove the deferred-to-PR-26
   note for ADDINCL/CFLAGS/CXXFLAGS/CONLYFLAGS now that it has
   landed (or move to D04 as deferred-to-PR-30).
6. Comment update at gen.go:1198-1203 — remove the
   EmitCCFromBuildRoot deferred comment now that D07 lands.

**Verification commands (ledger entry):**

```
go build ./... && go vet ./... && gofmt -l *.go && go test -count=1 ./...
./yatool make -j 0 -G build/cow/on > .tmp/m1.json
./yatool compare --level=3 .tmp/m1.json /yatool_orig/g.json
  → expect: L0/L1/L2/L3 = 100% / 100% / 100% / 100% (M1 regression)
./yatool make -j 0 -G tools/archiver > .tmp/our.json
./yatool compare --level=3 .tmp/our.json /yatool_orig/g.json
  → expect:
    L0 ≥ 88.34%   (no regression — PR-29 adds zero new nodes)
    L1 ≥ 88.66%   (no regression)
    L2 ≥ 86.89%   (modest lift expected — generated-source
                   inputs flip and contribute to L2)
    L3 ≥ 50.00%   (acceptance gate)
```

### Optional D09 (stretch) — `peer-propagated GLOBAL ADDINCL` minimum

**Files:** `gen.go`, `cc.go`.

If D01..D07 fall short of L3 ≥ 50%, the smallest D04 slice that
unblocks the gate is "peer-propagated GLOBAL ADDINCL only" (no
peer GLOBAL CXXFLAGS yet). The walker stores
`d.globalAddIncl []string` per module (subset of `d.addIncl`
where the AST flag is GLOBAL); `moduleEmitResult.GlobalAddIncl`
exposes it; consumer modules concatenate peer GlobalAddIncl in
declaration order into the AddIncl slice passed to EmitCC.
Estimated additional flip: ≈ 50-80 nodes (libcxx GLOBAL
`-I.../libcxx/include` propagates to many peers). Mark as
**stretch goal** and only invoke if pre-D04 result is < 50%.

## Acceptance criteria

- M1 regression preserved: `./yatool make -j 0 -G build/cow/on`
  produces 2 byte-exact pairs at L0/L1/L2/L3.
- `tools/archiver` target nodes ≥ 1739 (no regression).
- `tools/archiver` host nodes ≥ 1582 (no regression).
- `tools/archiver` total nodes = 3321 ± 0 (no new nodes; PR-29
  is byte-exact tuning, not coverage expansion).
- L0 = 88.34% (unchanged — same node set).
- L1 ≥ 88.66% (no regression).
- L2 ≥ 86.89% (no regression; modest lift expected from D07).
- **L3 ≥ 50.00%** (acceptance gate).
- All existing tests pass.
- New byte-exact tests added per D08.

## Risks and open questions

- **D02 own-CXXFLAGS slot**. The reference data shows libcxx
  emitting own CXXFLAGS BETWEEN second-suppression-block and
  builtinMacroDateTime — but only one module sample (libcxx)
  was inspected at that depth. If mimalloc or zlib uses a
  different slot, D02 needs branching. Mitigation: pin TWO
  module samples in D08 (libcxx + mimalloc) and require slot
  identity.
- **D04 deferred to PR-30**. Without GLOBAL CXXFLAGS / GLOBAL
  ADDINCL propagation, libcxx's `-nostdinc++` pair stays
  unmatched on every C++ consumer. This caps L3 below the full
  reference parity. The 50% gate is reachable without D04 because
  D01 alone delivers ≈ 39pp; the question is whether non-musl
  divergences amount to < 11pp without D04. **Honest projection:
  D01 (+39pp) + D07 (+0.5-1pp on the 24 paired generated nodes
  conditional on D02/D05) + D02 own-libcxx (+0 to +1pp because
  libcxx still needs D04 for full byte-exactness) + D03 own
  builtins (+5-10pp) + D05 clang++ (+1-2pp on isolated cases) +
  D06 (+1pp) = 47-53pp on top of 36.46% = 83-90% L3. With heavy
  haircuts for double-counting (most divergent nodes need
  multiple D-tasks to flip), the realistic lower bound is
  **~55-60%** — still clears the gate.**
- **D04 dependence**. If D01 + D03 + D05 + D06 alone clears 50%
  (likely scenario per host-musl probe count of 1297), D04 stays
  in PR-30 cleanly. If not, D09 stretch goal lands.
- **Three composers diverging**. Today the four composers
  (target / host / musl / NEW musl-host) all need to be updated
  to consume `ModuleCCInputs`. They do not share an extension
  point. Recommendation: introduce a small `composeCCExtras(in
  ModuleCCInputs, slot string) []string` helper that returns the
  ADDINCL slice (slot="addincl"), the own-CXXFLAGS slice
  (slot="own-extras"), etc. Each composer calls the helper at
  three distinct slot positions. Mitigates divergence rot.
- **`-std=c++20` slot in target without NoCompilerWarnings**.
  Verified `library/cpp/getopt/small` reference: slot is right
  after the second noLibcUndebugBlock, before
  cxx-warning-extension peer-bundle. Verified with one sample
  (getopt/small completer.cpp.o cmd_args[104] = `-std=c++20`).
  Single-sample risk; D08 should pin a second non-libcxx C++
  module to reduce.
- **EmitCCFromBuildRoot vs IsGenerated bool**. Recommendation: bool
  inside ModuleCCInputs (per §Decisions). The "fork EmitCC into
  EmitCC + EmitCCFromBuildRoot" approach was the PR-25 deferred
  proposal. Bool-on-struct is cleaner: same composer body,
  same flag bundles, only the inputPath construction branches.
  Critically, the bool also lets the same EmitCC populate
  `DepRefs: [generatorRef]` from a struct field without
  doubling the surface area. Reviewer should check whether
  the "fork" model has any advantage they care about; the
  empirical data shows zero divergence between the two except
  inputPath + DepRefs.

## Out of scope (deferred to follow-up PRs)

- **D04 (peer-propagated GLOBAL ADDINCL/CXXFLAGS)** — PR-30. Closes
  libcxx + libcxxrt + musl peer chains for the full archiver
  closure.
- **AS divergence** (56/56 paired AS pairs diverge) — PR-30 or
  PR-31. Likely needs Cwd asymmetry handling per PR-24-D01
  documented constraint and per-module includes from `addIncl`
  threaded into EmitAS (already in EmitAS signature, just not
  populated by the walker).
- **AR divergence** (5/35) — investigate in PR-30; possibly
  related to peer-archive ordering or `module_tag` handling.
- **LD single divergent pair** — investigate; likely VCS info or
  archiver-binary specific.
- **musl/full closure path** (PR-28 known constraint) — separate
  PR; not in PR-29 scope. Closes asmlib + 8 yasm host nodes.
- **ALLOCATOR_IMPL routing + abseil-cpp closure** — PR-30 per
  ledger.
- **Per-host-bundle CC tuning** (libcxx host, libcxxrt host,
  jemalloc host, mimalloc host) — PR-31 per ledger.
- **`-D_LIBCPP_BUILDING_LIBRARY` in CXXFLAGS-using non-libcxx
  modules** — none observed in M2 closure; deferred.
- **EXTRALIBS / RECURSE routing** — out of M2 scope per PR-28
  plan.
- **r6ParseGapsTolerated counter cleanup** — PR-28 plan flagged
  this; minor.
