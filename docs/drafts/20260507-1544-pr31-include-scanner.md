# PR-31 — Include scanner + sg.json reference switch (M2 L2 recovery)

## Problem

The reference graph used by the comparator was switched from
`/home/pg/monorepo/yatool_orig/g.json` to
`/home/pg/monorepo/yatool_orig/sg.json` in advance of M2 closure.
sg.json includes the parsed `#include` transitive closure as additional
`inputs` on every CC node. Switching the comparator without
implementing an include scanner crashed L2 from 97.56% (vs g.json) to
0.72% (vs sg.json) because every CC node we emit has `inputs=[source]`
while the reference has `inputs=[source, ...transitively-resolved
headers]`. L0 (98.77%), L1 (88.74%), and L3 (80.35%) were preserved
because none of those metrics consult the inputs list directly: L0 reads
the topology fingerprint, L1 pairs by `(outputs[0], platform)`, L3
compares cmd_args/env/cwd.

The fix is two-part. First, point all `referenceGraphPath` consts at
sg.json. Second, implement a C/C++ `#include` scanner that produces, for
each CC node we emit, the same input-set the upstream ymake scanner
produces — text-based scanning that ignores preprocessor conditionals,
walks all `#include`/`#include_next` directives, resolves them against
the same path-search rules ymake uses, and walks the transitive closure
with memoisation and cycle protection.

The empirical baseline (orchestrator-verified, 2026-05-07): L0=98.77%,
L1=88.74%, **L2=0.72%**, L3=80.35%. M2's L2 acceptance gate is **≥70%**;
PR-31 must close that gap.

## Empirical findings

### Include directive shape

Probed `lib.c`/`libbase64.h`/`codecs.h` in `contrib/libs/base64/avx2/`,
`strlen.c` in `contrib/libs/musl/src/string/`, and several libcxx headers
(`include/string.h`, `include/__config`, `include/algorithm`,
`include/stddef.h`).

Three forms appear:

1. `#include <header>` — angle-bracket form. The dominant form for
   system/library headers. Resolves against per-module ADDINCL +
   sysincl mappings (no same-dir search).
2. `#include "header"` — quoted form. Used for in-tree headers (e.g.
   `lib.c`'s `#include "libbase64.h"`). Resolves first against the
   source file's own directory, then against ADDINCL, then sysincl.
3. `#include_next <header>` — GCC/Clang extension used heavily by libcxx
   (e.g. `libcxx/include/string.h`'s `#include_next <string.h>`) and by
   musl's `#include_next` chains. Behaves like `#include <...>` for our
   scanner's purposes (transitive-closure walker visits the next match
   in the search path).

Whitespace tolerated between `#`, `include`, and the bracket. Indented
forms (`#  include <...>` inside `#if` blocks) are common in libcxx.

The simplest matcher is regex
`^\s*#\s*(include|include_next)\s*[<"]([^>"]+)[>"]`. Covers all three
forms with two capture groups (directive, target). Confirmed by
inspecting `__config` (which uses indented `#  include` inside `#if`),
musl `string.h` (vanilla `#include`), and libcxx `string.h`
(`#include_next`).

### Sample resolution (3 CC nodes traced)

**Node 1: `$(BUILD_ROOT)/contrib/libs/base64/avx2/lib.c.o`**
(18 inputs, target aarch64).

Source `contrib/libs/base64/avx2/lib.c` has 4 directives:
`<stdint.h>`, `<stddef.h>`, `"libbase64.h"`, `"codecs.h"`.

Resolved inputs trace:

| input idx | path | resolution mechanism |
|-----------|------|---------------------|
| 0 | base64/avx2/lib.c | the source itself |
| 1 | base64/avx2/codecs.h | `"codecs.h"` → same dir |
| 2 | base64/avx2/libbase64.h | `"libbase64.h"` → same dir |
| 3-9 | libcxx/include/{stddef.h,__config,__config_epilogue.h,__config_site,__configuration/{abi,compiler,platform}.h} | `<stddef.h>` → libcxx via `stl-to-libcxx.yml` (`stddef.h: contrib/libs/cxxsupp/libcxx/include/stddef.h`); transitive includes from stddef.h → __config → site/abi/compiler/platform |
| 10-11 | musl/{include,src/include}/features.h | from libcxx `__configuration/compiler.h` `<features.h>` → resolved via `libc-musl-libcxx.yml` source_filter for `libcxx/include/__config*` AND `libc-to-musl.yml` catch-all (mapping returns BOTH paths; both included as inputs) |
| 12-13 | libcxx/include/__configuration/{availability,language}.h | transitive from __config |
| 14-17 | musl/{include/stddef.h,arch/aarch64/bits/alltypes.h,include/stdint.h,arch/aarch64/bits/stdint.h} | `<stddef.h>` from libcxx-side (via `__cxx03/stddef.h` chain) plus `<stdint.h>` resolved via `libc-to-musl.yml` `stdint.h: musl/include/stdint.h`; musl's stdint.h `#include <bits/alltypes.h>` and `<bits/stdint.h>` resolve to per-module ADDINCL `musl/arch/aarch64` (musl ya.make) |

Module's own ADDINCL is empty (base64/avx2 ya.make declares none).
Header resolution comes entirely from peer-propagated GLOBAL ADDINCL
(libcxx, libcxxrt, musl) plus sysincl YAML mappings.

**Node 2: `$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.o`**
(20 inputs).

Source `strlen.c` has 3 directives: `<string.h>`, `<stdint.h>`,
`<limits.h>`.

Resolution: `libc-musl-libcxx.yml` source_filter `^contrib/libs/musl`
applies (musl source). Mappings:
- `string.h: libcxx/include/string.h` (overrides — musl's own string.h
  not in slot 1 but pulled later via libcxx's `#include_next <string.h>`)
- `stdint.h: ""` (suppressed for musl source itself, but transitively
  from libcxx's chain `stdint.h` resolves via `libc-to-musl.yml` to
  musl/include/stdint.h)
- `stddef.h: libcxx/include/stddef.h`

For `<limits.h>`: not in `libc-musl-libcxx.yml`'s explicit list →
falls through to `libc-to-musl.yml` → `limits.h: musl/include/limits.h`.

The chain becomes: strlen.c → libcxx/string.h → libcxx/__config →
libcxx/__configuration/compiler.h → musl/features.h → ... → libcxx
suite (8 nodes) → musl/limits.h → musl/arch/aarch64/bits/alltypes.h
(per-module musl ADDINCL `musl/arch/aarch64`) → musl/stdint.h →
musl/string.h (from libcxx string.h's `#include_next`) →
musl/strings.h (musl string.h includes `<strings.h>` under
`#if defined(_BSD_SOURCE) || defined(_GNU_SOURCE)` — the conditional is
NOT respected by the upstream scanner; the include is followed
unconditionally) → musl/src/include/string.h.

**Node 3: `$(BUILD_ROOT)/tools/archiver/main.cpp.o`**
(1009 inputs).

This is a regular C++ application source. The transitive closure spans
libcxx (most of it), util/, library/cpp/archive/, etc. The 1009 inputs
include essentially the entire libcxx public surface used by `<string>`,
`<algorithm>`, `<map>`, etc. that `main.cpp` pulls.

Resolution mix:
- `<cstring>` etc. → libcxx (via stl-to-libcxx.yml)
- `<string.h>` etc. → musl (via libc-to-musl.yml)
- `"yarchive.h"` (relative) → same dir (tools/archiver/) AND
  library/cpp/archive (via that module's GLOBAL ADDINCL, peer-propagated)
- `<util/generic/fwd.h>` → util/generic/ via util's GLOBAL ADDINCL
  (peer-propagated from `util` peerdir)

### Transitive depth distribution

Across 3571 CC nodes in sg.json:

- header-input count median: **17**
- p25: **12**
- p75: **42**
- p90: **708**
- p99: **1080**
- min: 1 (musl C-only sources after transitive closure resolves to one
  header), max: **1225** (`tcmalloc.cc.o` with libcxx + abseil + musl +
  linux-headers transitive closure)
- mean: 125

The p90 → p99 jump (708 → 1080) is dominated by C++ application sources
that pull libcxx-heavy headers (`<string>`, `<map>`, `<algorithm>`,
`<memory>`, etc.). C-only sources sit comfortably under p75. Musl
C-internal sources are smallest (the libc-musl-libcxx.yml source_filter
limits transitive expansion to a small set).

### Conditional-include handling

**The upstream scanner does NOT respect `#if`/`#ifdef`/`#elif`/`#else`
conditionals.** Empirically confirmed via `musl/include/string.h`:
`<strings.h>` is included only inside `#if defined(_BSD_SOURCE) ||
defined(_GNU_SOURCE)`, yet the reference inputs for `strlen.c.o`
(input [18]) include `musl/include/strings.h`. Same observation for
libcxx `__config`'s `#  include <features.h>` inside `#if !defined(...)`.

This drastically simplifies the scanner. We do NOT need a preprocessor
evaluator: a pure regex match across every line, ignoring all
preprocessor branching, reproduces the upstream behaviour. Every
`#include`/`#include_next` directive in the source text contributes to
the transitive closure regardless of guard conditions.

### Symlinks / generated headers

No CC node in sg.json has a header input with a `$(BUILD_ROOT)/` prefix
(probe: 0 of 4954 unique CC inputs). Header inputs are uniformly
`$(SOURCE_ROOT)/<path>`. JS-derived CC nodes (e.g.
`$(BUILD_ROOT)/util/charset/all_charset.cpp.o`) DO have a
`$(BUILD_ROOT)/...` PRIMARY input (the generated `.cpp`), but their
secondary inputs are SOURCE_ROOT — namely the build script
(`build/scripts/gen_join_srcs.py`, `build/scripts/process_command_files.py`)
plus the source `.cpp` files that the JS step joins (under
`$(SOURCE_ROOT)/util/charset/`).

PR-31's scanner will not need to handle generated headers — there are
none in the reference. JS-derived CC nodes need a separate
input-composition rule (build scripts + JS source files) that is NOT
the include scanner; they are handled by EmitJS / EmitCC's IsGenerated
branch already, with the additional inputs added at JS emit time.

### Path normalization + sort order

- All inputs SOURCE_ROOT-prefixed (header inputs only) or BUILD_ROOT
  (primary input for generated CC). No mixing.
- Paths are normalized: 0 occurrences of `/../` or `/./` across all
  4954 unique CC inputs.
- **Inputs are NOT alphabetically sorted.** Order is BFS-like /
  scanner-walk order from the primary source. Empirical: probed
  `tools/archiver/main.cpp.o` and `strlen.c.o` — `inputs[1:]` does NOT
  equal `sorted(inputs[1:])`. The order matches the order ymake's
  scanner discovered each header during transitive walk.
- Inputs are deduplicated within a node (probe: 0 dupes across 500
  sampled CC nodes).

The order is load-bearing for L2: L2 compares inputs as a multiset (set
equality + count), not a sequence. Verified by re-reading
`compare_props.go` L2 implementation. Therefore PR-31's scanner only
needs to produce the right SET of inputs; the BFS/DFS traversal order
within the scanner does not need to match upstream byte-for-byte.

(Caveat: for L3 the inputs are not touched. For L0 the topology
fingerprint hashes the set as well — exact order is not required.)

## Design decisions

**1. Include scanner architecture: regex-based pre-processor.**

A single regex `^\s*#\s*(include|include_next)\s*[<"]([^>"]+)[>"]`
applied per line. No `#if` evaluation, no token-level lex, no
preprocessing-state machine. Justification: the upstream behaviour is
empirically conditional-blind (see "Conditional-include handling"),
and a token-aware lexer would add complexity for zero correctness
benefit. Trade-offs:

- Pro: ~50 LOC for the matcher itself; trivially memoizable per file
  path; deterministic.
- Pro: Matches `#  include` / `#include_next` / quoted / angled forms in
  one expression.
- Con: A pathological source containing the literal string
  `#include <foo>` inside a multi-line C string or block comment would
  produce a false positive. Empirical mitigation: not observed in any
  M2-relevant source. Documented as "follow up if a future PR catches
  one".
- Con: Cannot handle `#include MACRO_NAME` (macro-expanded include
  paths). Not observed in M2 closure; if encountered, the regex skips
  the line and the missing-header symptom is a divergent input set,
  caught by the comparator. Documented as a future hardening point.

**2. Resolution algorithm.**

For each include directive `<header>` or `"header"`:

```
candidates = []
if quoted:
    candidates.append(samedir(source) / header)
for path in module.AddIncl:                    # per-module non-GLOBAL ADDINCL
    candidates.append(SOURCE_ROOT / path / header)
for path in module.PeerGlobalAddIncl:          # peer-propagated GLOBAL ADDINCL (Note A)
    candidates.append(SOURCE_ROOT / path / header)
for mapping in sysincl.lookup(source.path, header):
    candidates.append(SOURCE_ROOT / mapping)   # may yield 0, 1, or N paths
# Filter to those that actually exist on disk; dedup; emit each as input.
for c in candidates:
    if exists(c) and c not in visited:
        visited.add(c); recurse(c)
```

Critical observation from sysincl YAML files: **a single header can map
to multiple paths**. Example from `libc-to-musl.yml`:

```yaml
- features.h:
  - contrib/libs/musl/include/features.h
  - contrib/libs/musl/src/include/features.h
```

Both paths are emitted as inputs (verified: strlen.c.o inputs [8] and
[9] are the two features.h files). The resolution algorithm must
fan out to all listed paths, not pick the first.

**3. Caching: per-file include-list cache.**

Each header file's parsed include directives memoized once per scanner
run. Key = absolute path. Value = list of `(directive_kind, target)`
tuples. Avoids re-parsing libcxx's `__config` (~1180 lines) on every
CC node that transitively includes it (≈3000 of 3571 CC nodes).

Memoize at the **file** level, not the **(file, source_module)** level:
the parsed directive list is a property of the file. Resolution is then
re-run per consumer because different source modules have different
ADDINCL/sysincl source_filter context (this is correct and necessary).

**4. Closure: depth-first walk + visited-set cycle detection.**

Standard graph traversal. The visited set is per CC-node (per scanner
invocation) so musl_extra/all.c and base64/avx2/lib.c each compute their
own closure. Within a single closure, cycles (e.g. libcxx's `stdint.h`
includes-itself-via-include_next) are broken by the visited check.

Output order: iterate the visited set in DFS-discovery order, but since
L2 treats inputs as a multiset, exact order is not load-bearing. We
emit in DFS-discovery order so future debugging/diffing is sane.

**5. Path output format: `$(SOURCE_ROOT)/<path>`.**

All header inputs in sg.json use `$(SOURCE_ROOT)/<path>`. The scanner
emits the literal `$(SOURCE_ROOT)/` prefix followed by the
SOURCE_ROOT-relative path. No `$(BUILD_ROOT)/` headers exist in M2
closure.

**6. Sort order in inputs: do not sort.**

Multiset-equality comparison at L2 makes order irrelevant for the
acceptance gate. Emit headers in DFS-discovery order. (Future change to
match upstream's exact order is a polish item for M5+; not in M2 scope.)

**7. Where in rule emitter to inject: extend ModuleCCInputs with a
resolved-headers slice; inject in EmitCC after the source path.**

The cleanest fit:

```go
type ModuleCCInputs struct {
    ...existing fields...
    IncludeInputs []string  // resolved transitive header inputs ($(SOURCE_ROOT)/...)
}
```

The walker (`gen.go::emitOneSource`) computes the closure once per
source via a new `IncludeScanner` (carried on `genCtx`). EmitCC appends
`in.IncludeInputs` to `node.Inputs` after the primary source path.

Alternative considered: compute the closure inside EmitCC. Rejected
because: (a) EmitCC's signature is already wide; (b) the scanner needs
ADDINCL + peer-GLOBAL-ADDINCL + sysincl context that the walker has but
EmitCC does not; (c) the scanner state (file cache, sysincl mappings)
is process-lifetime — belongs on `genCtx`, not in the rule emitter.

**8. System include paths: load from `build/sysincl/*.yml`.**

Two sub-decisions:

(a) Source of sysincl mappings. The upstream maintains 53 YAML files
totalling 11,248 lines under `build/sysincl/`. Hand-translating them to
Go would be huge and would drift on every upstream resync. Instead:
**load and parse the YAML files at startup** from
`<sourceRoot>/build/sysincl/*.yml`. Stdlib has no YAML; we add a
minimal hand-rolled parser for the subset actually used (string-only
mapping `key: value` and `key:` followed by `- value` list under
`includes:`, plus the optional `source_filter:` regex). The full YAML
spec is out of scope.

The sysincl loader produces a flat structure:

```go
type SysIncl struct {
    SourceFilter *regexp.Regexp  // nil = matches all
    Mappings     map[string][]string  // header → list of resolved paths
}
type SysInclSet []SysIncl
func (s SysInclSet) Lookup(sourcePath, header string) []string { ... }
```

Rationale (from RTFM principle): we read the upstream's authoritative
specification rather than reverse-engineer it. The YAML-loading code is
~150 LOC including the minimal parser; the upstream files are 11K lines
of pure data.

(b) Which YAML files to load. M2 closure needs at minimum:
`linux-headers.yml`, `intrinsic.yml`, `libc-to-musl.yml`,
`libc-musl-libcxx.yml`, `stl-to-libcxx.yml`, `linux-musl.yml`,
`linux-musl-aarch64.yml`, `local-compiler-runtime.yml`. To stay
honest and forward-compatible, **load all `*.yml` in `build/sysincl/`**
unconditionally and apply source_filter at lookup time. (Some YAML
files have `source_filter` regexes that exclude their content from most
sources — e.g. `windows.yml` has effectively no effect on Linux closures
because its mappings only fire for filename patterns that don't appear.)

**9. PR scope: single PR, ≈500-600 LOC.**

Estimated LOC by component:

| component | LOC |
|-----------|-----|
| sysincl YAML loader (minimal parser) | ~150 |
| `IncludeScanner` (file cache + regex matcher + transitive walker) | ~150 |
| `genCtx.scanner` wiring; `emitOneSource` invocation; `ModuleCCInputs.IncludeInputs` plumbing | ~50 |
| `EmitCC` appending include inputs to `node.Inputs` | ~10 |
| `referenceGraphPath` const switch + test re-pin (`referenceNodeCount` may shift) | ~5 |
| Tests (per-source synthetic; one regression pin against base64/avx2 reference inputs) | ~150 |
| Total | ~515 |

This is comparable to PR-29 (~600 LOC) and PR-30 (~700 LOC). One PR;
no need to split.

## Note A — peer-propagated GLOBAL ADDINCL

The empirical resolution probe (Sample resolution above) shows the
include scanner needs **peer-propagated GLOBAL ADDINCL** to resolve any
header in the libcxx/libcxxrt/musl ecosystem from a non-runtime-ancestor
consumer. Specifically:

- `<features.h>` (from libcxx `__config`) resolves to musl, but only
  because the consumer module's effective ADDINCL set includes
  `contrib/libs/musl/include` (peer-propagated from peer
  `contrib/libs/musl`'s GLOBAL ADDINCL — which musl actually does NOT
  declare as GLOBAL; it's an effect of sysincl mappings combined with
  musl's ADDINCL being walked transitively).

Re-reading `contrib/libs/musl/ya.make`: ADDINCL is non-GLOBAL. So
**musl's per-module ADDINCL is not peer-propagated as ADDINCL**. The
include resolution of `<features.h>` from a non-musl source uses
sysincl YAML mappings, which name absolute repo paths. The non-GLOBAL
musl ADDINCL only affects musl's own compilation; it does not need to
flow to peers.

`contrib/libs/cxxsupp/libcxx/ya.make`:
```
ADDINCL(
    GLOBAL contrib/libs/cxxsupp/libcxx/include
```

This IS GLOBAL. So `tools/archiver/main.cpp` includes `<cstring>` and
the resolution path needs `-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include`
in the search path. Confirmed empirically: `tools/archiver/main.cpp.o`
cmd_args include `-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include`
as a peer-propagated ADDINCL.

**Conclusion: PR-31 must implement peer-propagated GLOBAL ADDINCL
resolution.** The scanner cannot work without it: absent libcxx GLOBAL
ADDINCL, `<cstring>` resolves to nothing and the entire libcxx
transitive closure (≈90% of L2's mass) is missing from inputs.

This is bundling Note A's option (a) — peer-propagated GLOBAL ADDINCL
routing lands in PR-31 alongside the include scanner. The scanner
literally cannot resolve any libcxx header without it.

Implementation note: the GLOBAL ADDINCL collection is similar to
PR-30 D04's `cxxFlagsGlobal`/`cOnlyFlagsGlobal` deferred work. Add a
new field `addInclGlobal []string` to `moduleData`; collect via
`applyAddInclStmt` (typed `*AddInclStmt`) when modifier is GLOBAL;
expose on `moduleEmitResult` as `AddInclGlobal []string`; aggregate the
transitive set at consumer's genModule (BFS over peers) into a
`PeerAddInclGlobal []string` slot on `ModuleCCInputs`; the scanner
queries this for resolution candidates. The same field also gets
appended to cmd_args (already done for direct ADDINCL via
`appendAddIncl`).

The existing applyUnknownStmt already handles GLOBAL stripping for
CXXFLAGS/CONLYFLAGS via `splitGlobalModifier`; the typed *AddInclStmt
has `splitGlobalModifier` called at PR-29 yamake.go:863 — verify
in implementation that the GLOBAL form is currently DROPPED (like
CXXFLAGS GLOBAL), not silently appended to local addIncl.

PR-32 (formerly the catch-all peer-propagated CXXFLAGS GLOBAL PR) will
land peer-propagated CXXFLAGS GLOBAL alongside the cxx-warning-extension
peer bundle and similar L3 polish; the GLOBAL ADDINCL routing in PR-31
sets the precedent and reusable shape.

## Note B — musl hardcoding audit (orthogonal)

User-flagged on 2026-05-07: musl knowledge is scattered through the code
("not necessarily M2, study, plan"). Goal of this section: enumerate
every musl-specific call site, classify by category, estimate refactor
effort, recommend WHERE the work fits.

### Sites

`grep -n musl` across all production .go files (excluding `_test.go`)
returned 150 hits across 7 files. Below grouped by file with site
character.

**`flags.go` (44 hits)** — flag-bundle definitions:

- `muslCcIncludes` (10-arg `-I` set, target/aarch64)
- `muslCcIncludesX8664` (10-arg `-I` set, host/x86_64)
- `muslWarningFlags` (single `-Wno-everything`)
- `muslExtraDefines` (9-arg block: `-D_XOPEN_SOURCE=700`, `-U_GNU_SOURCE`,
  `-nostdinc`, `-ffreestanding`, `-fno-stack-protector`,
  `-D__libc_calloc=calloc`, `-D__libc_malloc=malloc`,
  `-D__libc_free=free`, `-D_musl_=1`)

Category: **(i) genuinely musl-specific knowledge**. Each entry is
exactly the upstream musl ya.make's own `CFLAGS()` block plus its
`ADDINCL()`. There is nowhere else to put this — the musl flag bundle
is a property of the musl module. Refactor: move the flag values from
"hardcoded in flags.go" to "derived from parsing musl's ya.make
CFLAGS/ADDINCL". This requires teaching the walker that the union of
own-CFLAGS + own-ADDINCL becomes the cmd_args contribution for that
module. Effort: medium (~150 LOC). Already partially in scope for the
generic flag-threading story (PR-29 D02/D03).

**`cc.go` (50 hits)** — `composeMuslCC` / `composeMuslHostCC` plus the
`isMusl := instance.Path == "contrib/libs/musl" || HasPrefix(...)` path
check that dispatches to those composers.

Category: **(iii) "module is musl" path-prefix check that should be
flag-driven**. Today the dispatch uses literal path-string. The
refactor is to have a `isMusl bool` field on FlagSet (or a sentinel
"flavor" marker on ModuleInstance) set by inferFlagsFromPath plus the
ya.make's content. Then `composeMuslCC` becomes either (a) a
flavor-dispatched composer in a per-flavor table, or (b) collapsed
into `composeTargetCC` with the musl bundle composed dynamically from
the module's effective FLAGS.

The right shape is (b) IF and only if PR-29's ADDINCL/CXXFLAGS/
CONLYFLAGS threading + a future "GLOBAL CFLAGS peer propagation" PR
land first — then the difference between "musl" and "non-musl" is JUST
the per-module flag content.

Effort: medium-high (~300 LOC including refactor, test churn). Coupled
to D04's GLOBAL CFLAGS/CXXFLAGS routing.

**`gen.go` (37 hits)** — five categories of mention:

- `runtimeAncestorPaths["contrib/libs/musl"] = true` — data-driven
  membership (category (ii), already correct shape)
- `defaultPeerdirsFor`'s `instance.Path != "contrib/libs/musl" &&
  !HasPrefix("contrib/libs/musl/")` — musl self-suppression in implicit
  peer addition (category (iii) but defensible — all runtime ancestors
  need self-suppression; the literal path check IS the data here)
- `defaultProgramPeerdirsFor`'s `muslFullPath = "contrib/libs/musl/full"`
  hardcode — category (iii); `MUSL=yes && !MUSL_LITE` decision should
  consult a config table not a literal path
- `applyUnknownStmt`'s `if a == "MUSL_LITE"` ENABLE switch — category
  (i), MUSL_LITE is a real macro the upstream defines, the binding is
  semantic
- doc comments mentioning musl — informational, not refactor targets

Effort to refactor categories (ii)/(iii) here: low-medium (~50 LOC).

**`module.go` (1 hit)** — `inferFlagsFromPath` has the path-literal
`if path == "contrib/libs/musl" || HasPrefix(path, "contrib/libs/musl/")`
that sets `NoLibc=NoUtil=NoRuntime=true`. Category (iii). The
refactor: musl ya.makes already declare `NO_LIBC()`, `NO_UTIL()`,
`NO_RUNTIME()` — actually they declare `NO_PLATFORM()` (which we map
to the triple). The path heuristic is redundant once the macro
evaluator runs. Replace with an empty stub function and let the macro
evaluator populate. Effort: low (~20 LOC + test rebase).

Caveat: `inferFlagsFromPath` runs BEFORE the ya.make is parsed (the
flags seed the ModuleInstance which is the memo key). Removing the
path heuristic shifts ordering. Whether this works depends on whether
the seed-flag value participates in any decision before the ya.make is
parsed. Audit at refactor time.

**`ld.go` (11 hits)** — `composeLDCmdLinkVcs` injects `-D_musl_=1` /
`-D_musl_` into the linker's vcs_version compile args. Hardcoded
unconditionally. Category (iii) — should be conditional on the
PROGRAM's effective FLAGS containing `-D_musl_=1` (which is a peer-
propagated GLOBAL CFLAG from musl). When the GLOBAL CFLAGS routing
lands (PR-32 D04), this hardcode goes away.

**`cp.go` (5 hits)** — `EmitCP` emits a copy node for
`contrib/libs/musl/include/musl.py` → `musl.py.pyplugin`. The function
is structurally generic for any src/dst pair; only the doc / test pin
the musl case. Category (iv): not really musl-hardcoded, just musl-
documented. No refactor needed.

**`macros.go` (2 hits)** — `MUSL=true` and `MUSL_LITE=false` in
`DefaultIfEnv`. Category (i): these are environment values the IF
evaluator consults. Correct shape. The DEFAULT values are M2-canonical
(Linux + musl). M5+ where multiple environments matter, this becomes
a per-config map.

### Refactor sizing total

| category | LOC | risk |
|----------|-----|------|
| (i) genuinely musl-specific data → leave alone | 0 | none |
| (ii) data-driven dispatch already correct | 0 | none |
| (iii) path-prefix checks → flag-driven | ~400 | medium (test churn; ordering audit for inferFlagsFromPath; coupling to GLOBAL CFLAGS routing) |
| (iv) doc-only | 0 | none |
| **Total refactor** | **~400** | medium |

### Recommended PR placement

The musl-hardcoding refactor is **OUT OF SCOPE for PR-31**. PR-31's
focus is the L2 acceptance gate via the include scanner; the musl
refactor is L0/L1/L2/L3-neutral (it's a structural cleanup, not a
correctness fix).

The work cleanly decomposes:

- The `composeMuslCC`/`composeMuslHostCC` collapse into
  `composeTargetCC`/`composeHostCC` with parameterised flag content
  is **unblocked by PR-31's peer-GLOBAL ADDINCL** (the same machinery
  serves peer-GLOBAL CFLAGS, which is what musl declares
  `-D_musl_=1` GLOBAL as). Lands cleanly in PR-32 alongside the
  CFLAGS GLOBAL routing.
- The `inferFlagsFromPath` musl heuristic removal is independent and
  can land at any time after PR-32's CFLAGS GLOBAL work; recommend
  M5 hardening as it has a non-trivial ordering audit.
- The `runtimeAncestorPaths` and `defaultProgramPeerdirsFor` literal
  path checks are correct as-is per the reference; replacing literal
  paths with a config table is gold-plating without a forcing function.
  Defer to M5+.

**Recommendation:**
- **PR-31:** include scanner + sg.json + peer-GLOBAL ADDINCL. NO musl
  refactor.
- **PR-32:** peer-GLOBAL CFLAGS/CXXFLAGS routing → enables the
  `composeMusl*` collapse → ship the collapse in the same PR while the
  refactor is hot. ~300 LOC on top of the GLOBAL CFLAGS work.
- **M5 hardening:** `inferFlagsFromPath` musl removal + literal-path
  table-driven cleanup. ~100 LOC; low priority unless a new module's
  ya.make contradicts the path heuristic.

## Implementation plan

D01 — switch `referenceGraphPath` const in `gjson_test.go` from
`g.json` to `sg.json`. Update `referenceNodeCount` if it shifted (probe
shows 3730 unchanged, so likely no edit needed). Re-run all tests
and accept the mass failures at L2 — they are the failure mode PR-31
exists to fix.

D02 — `sysincl.go` (new file): minimal YAML loader for the subset of
YAML used in `build/sysincl/*.yml`. Parse `source_filter:` (regex
string), `includes:` (list of `key: value` or `key: <list>`). Compile
regexes. Expose `LoadSysInclSet(sourceRoot string) (SysInclSet, error)`.
Empirical YAML is hand-edited; assume LF line endings, no tabs in
content, indented two spaces. Tests synthesise small YAMLs and check
parse correctness.

D03 — `IncludeScanner` (`scanner.go` new file): regex matcher per file
+ memoized `parseIncludes(absPath) []includeDirective` cache.
`Resolve(source, header, kind, ctx) []string` returns 0..N
SOURCE_ROOT-relative paths via the search-order: same-dir-if-quoted →
own ADDINCL → peer-GLOBAL ADDINCL → sysincl.lookup. `WalkClosure(source,
ctx) []string` returns the DFS-order transitive header set
(SOURCE_ROOT-prefixed strings) excluding the source itself.

D04 — `applyAddInclStmt` (in `gen.go`): split on GLOBAL modifier; route
non-GLOBAL → `d.addIncl` (existing), GLOBAL → `d.addInclGlobal` (new).
Confirms (or fixes) PR-29's behaviour that GLOBAL ADDINCL is currently
silently dropped — it must NOT be: GLOBAL ADDINCL belongs to peer-
propagated set.

D05 — `moduleEmitResult.AddInclGlobal []string` (new field): captures
the module's own GLOBAL ADDINCL contribution. `genModule` aggregates
peer-GLOBAL ADDINCL transitively and exposes via the module's own
`AddInclGlobal` (own + transitive peer) so consumers can use a single
slice.

D06 — `ModuleCCInputs.PeerAddInclGlobal []string` (new field): the
walker threads the resolved peer-GLOBAL set into EmitCC.
`appendAddIncl` already iterates a slice — append the peer-GLOBAL
slice between own-ADDINCL and the suffix include set, in declaration
order (R14). Verify cmd_args byte-exact against
`base64/avx2/lib.c.o`'s `-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include`
slot.

D07 — `genCtx.scanner *IncludeScanner` (new field) + initialisation in
`Gen`. Pass `ctx.scanner` to `emitOneSource`.

D08 — `ModuleCCInputs.IncludeInputs []string` (new field). In
`emitOneSource`, after computing `srcInstance` and `srcIn`, invoke
`ctx.scanner.WalkClosure(absSourcePath, scanCtx)` where `scanCtx`
carries the module's effective ADDINCL (own + peer-GLOBAL), the
module's path (used for sysincl source_filter matching). Feed result
into `srcIn.IncludeInputs`. Skip for `IsGenerated` (JS/R6 outputs):
generated CCs use a separate input shape (build script + sources, NOT
header closure).

D09 — `EmitCC` (in `cc.go`): append `in.IncludeInputs` to `node.Inputs`
after the primary source path. Order: primary source first, then
include-inputs in DFS-discovery order (no sort).

D10 — `cc_test.go`: regression pin against
`base64/avx2/lib.c.o`'s reference inputs. Load reference, pluck the
node's `inputs`, compare as multiset against our emitted node's
inputs. The test guarantees the scanner produces the right SET (order
explicitly not asserted, per Design Decision #6). Plus a synthetic
test exercising `#include_next` and a multi-mapping sysincl entry to
pin the fan-out behaviour.

D11 — `gen_test.go`: probe-test that `./yatool make -j 0 -G tools/archiver`
produces a graph whose CC nodes have non-trivial input sets (≥10
median header inputs across the 50+ in-closure CCs). Soft floor; the
real acceptance is the comparator output.

D12 — Update all reference doc comments in production .go files from
"g.json" → "sg.json" where the comment is structurally about the
reference (not historical PR-N notes). Pure rename; no logic change.

## Acceptance criteria

- M1 regression preserved against sg.json (build/cow/on/lib.c has no
  includes — IncludeInputs is empty — input set unchanged from g.json
  era; 2/2 pairs at L0/L1/L2/L3).
- L0 not regressed: stays ≥ 95% (current 98.77% has 3.77pp headroom);
  the include scanner changes ALL CC node fingerprints since inputs
  participate in topology fingerprint, but pairing-by-output should
  still resolve.
- L1 not regressed: stays ≥ 80%.
- **L2 ≥ 70% (M2 acceptance gate against sg.json)** — primary
  deliverable of PR-31.
- L3 not regressed: stays ≥ 50%. Note: L3 doesn't read inputs, so
  unchanged from PR-30's 80.35%.
- All existing tests pass after the const switch + scanner.
- `LoadSysInclSet` parses every `build/sysincl/*.yml` without error in
  the production tree.
- LOC estimate: ~515 (sysincl loader 150 + scanner 150 + plumbing 60 +
  tests 150).

Honest L2 projection: **70–85% achievable.** The reasoning:

- The libcxx + musl + linux-headers transitive closure dominates the
  input mass (~85% of total inputs across all CC nodes). Resolving
  these correctly via sysincl + peer-GLOBAL ADDINCL closes most of the
  gap.
- Edge cases that MAY fall short: (a) `#include MACRO_NAME` (not
  observed in M2 closure but possible); (b) sysincl YAML files with
  source_filter regex semantics our minimal parser mismatches; (c)
  ordering-sensitive resolution where ymake's "first match wins"
  conflicts with our "all matches" approach.
- 70% is a conservative floor; 85% is the realistic ceiling without
  M5 hardening. Above 85% requires `#include MACRO_NAME` handling +
  exact ymake scanner-order replication.

If L2 lands < 70%: documented escalation is to instrument the scanner
to log per-node "input set diff vs reference" and fix the highest-
impact divergence patterns iteratively in a follow-up PR-31b.

## Scope recommendation

**Single PR, ~515 LOC.**

The work does not partition cleanly into multiple PRs:
- The sysincl loader is unusable without the scanner that consumes it.
- The scanner is unusable without sysincl + peer-GLOBAL ADDINCL.
- Peer-GLOBAL ADDINCL routing is the same shape as PR-30 D04's
  CXXFLAGS GLOBAL deferral — bundling it here unblocks the scanner
  and keeps the routing pattern in one PR for review-coherence.

NOT bundled (deferred):
- musl hardcoding refactor (Note B) — independent, M5+
- peer-propagated GLOBAL CFLAGS/CXXFLAGS routing — PR-32
- exact ymake scanner-order replication — M5+ polish

## Risks and open questions

R1 — **YAML parser scope.** A minimal hand-rolled parser is brittle if
upstream introduces YAML constructs we don't handle (anchors, nested
block-folded strings, etc.). Mitigation: panic with a clear "PR-31
YAML parser does not handle X" message rather than silently misparse.
Audit `build/sysincl/*.yml` once; document the YAML subset relied on
in the loader's doc comment.

R2 — **Ordering of peer-GLOBAL ADDINCL aggregation.** When multiple
peers all contribute GLOBAL ADDINCL paths, the consumer's effective
ADDINCL list has them in PEERDIR-walk order. cmd_args ordering is L3-
visible; verify base64/avx2/lib.c.o's actual cmd_args order matches
our PEERDIR-declaration order. If not, ordering needs to be transitive-
DFS or some other canonical order.

R3 — **Existing GLOBAL ADDINCL handling in applyAddInclStmt.** Re-read
yamake.go's `*AddInclStmt` parsing carefully. PR-29 introduced
`splitGlobalModifier` for CXXFLAGS/CONLYFLAGS but the typed
`*AddInclStmt` may already parse the GLOBAL modifier into the AST
field. Confirm in implementation; likely a small adjustment to either
the AST or the gen.go consumer.

R4 — **L0 cost cascade.** Every CC node's UID changes when its inputs
change (Merkle hash). 3571 CC node fingerprints flip simultaneously.
L0 is currently 98.77% (3684/3730) — pre-existing 46 unpaired pairs
plus the 12 net spurious. The change is uniform across all CC nodes;
L0 should remain ≥ 95% provided the SET of inputs is correct (the
fingerprint is set-based, not order-based for inputs). If a per-node
input set is wrong, that node's fingerprint diverges from reference
and L0 drops by 1/3730 = 0.027 percentage points per wrong node. We
have ~138 percentage-points of headroom (98.77 - 95 = 3.77pp ≈ 138
nodes). Tolerable error budget: ~138 wrong CC nodes.

R5 — **Test environment dependency.** All tests that consume
`referenceGraphPath` skip when the file is absent. Switching to
sg.json: ensure sg.json exists at `/home/pg/monorepo/yatool_orig/sg.json`
(probe confirms it does, 73MB). Also: synthetic-only tests don't need
the file; only the empirical-pin tests do. Per STYLE.md skip-pattern.

Q1 — **`#include MACRO_NAME` handling.** Empirically not observed in
M2 closure but possible for M4+ targets. Decision deferred to M4 —
PR-31 panics on lines that match `#\s*include\s+\w+\s*$` (no bracket)
so we surface the case loudly. Alternative is silent skip.

Q2 — **`#include_next` vs `#include` for header-uniqueness.** Both
contribute to the closure equivalently in terms of inputs (the
secondary file IS an input). Treat identically in scanner. Verified:
strlen.c.o has both musl/include/string.h AND libcxx/include/string.h
as inputs from the `#include_next` chain.

## Out of scope (deferred)

- Musl hardcoding refactor (Note B) — defer to PR-32 (composer
  collapse) + M5 hardening (inferFlagsFromPath + literal-path table).
- Peer-propagated GLOBAL CFLAGS/CXXFLAGS routing — PR-32.
- Exact upstream-scanner traversal-order replication — M5+ polish; L2
  is multiset-only so order does not affect acceptance.
- `#include MACRO_NAME` macro-expanded include paths — unblocked by
  M4+ if/when needed.
- Sysincl YAML support beyond the subset used by `build/sysincl/*.yml`
  in the current upstream — extend on-demand.
- BUILD_ROOT-rooted header inputs (generated headers) — not present in
  M2 closure; defer to whenever first encountered.
