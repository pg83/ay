package main

// defaults.go — runtime-stack closure-membership tables and the
// defaultPeerdirsFor / defaultProgramPeerdirsFor derivations.
// Hand-translated subset of upstream ymake's _BUILTIN_PEERDIR /
// _BASE_PROGRAM logic — see each function's docstring for the
// reference-graph anchors that pin the empirical shape.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runtimeAncestorPaths is the set of module paths that are themselves
// part of the platform/runtime stack and therefore receive NO implicit
// default peers — matches the empirical reference-graph behaviour where
// every one of these modules has zero peer-archive deps in its AR.
//
// Upstream ymake achieves this via a special-case in `_BUILTIN_PEERDIR`
// (build/conf/) that we do not yet model from source; PR-27 hard-codes
// the closure-membership set instead. The list is the union of:
//
//   - C runtime stack: musl, libc_compat, linuxvdso(/original).
//   - C++ runtime stack: cxxsupp/{builtins, libcxx, libcxxrt,
//     libcxxabi, libcxxabi-parts}, libunwind.
//   - Allocator API: library/cpp/malloc/api.
//   - Sanitizer headers shim: library/cpp/sanitizer/include.
//   - The Yandex stdlib root: util.
//
// Membership of a path in this set causes `defaultPeerdirsFor` to
// return an empty slice for that instance, regardless of FlagSet.
// The set is intentionally narrow: a module not listed here that
// happens to declare a NO_* flag still goes through the normal
// per-flag suppression below. New entries land here only when the
// reference graph confirms the module has zero peer-archive deps
// AND the walker hits a cycle through it.
var runtimeAncestorPaths = map[string]bool{
	"contrib/libs/musl":                    true,
	"contrib/libs/libc_compat":             true,
	"contrib/libs/linuxvdso":               true,
	"contrib/libs/linuxvdso/original":      true,
	"contrib/libs/cxxsupp/builtins":        true,
	"contrib/libs/cxxsupp/libcxx":          true,
	"contrib/libs/cxxsupp/libcxxrt":        true,
	"contrib/libs/cxxsupp/libcxxabi":       true,
	"contrib/libs/cxxsupp/libcxxabi-parts": true,
	"contrib/libs/libunwind":               true,
	"library/cpp/malloc/api":               true,
	"library/cpp/sanitizer/include":        true,
	"util":                                 true,
}

// runtimeAncestorCxxConsumers is the subset of runtimeAncestorPaths
// whose C++ sources include libcxx headers (e.g. <atomic>, <cstddef>)
// and therefore need libcxx as an implicit GLOBAL header peer to
// supply `-I libcxx/include`, `-I libcxxrt/include` (libcxx's own
// GLOBAL ADDINCLs propagate the libcxxrt include via libcxx's
// `IF (CXX_RT == "libcxxrt")` branch — see
// `contrib/libs/cxxsupp/libcxx/ya.make:78-85`), and `-nostdinc++`
// (libcxx's GLOBAL CXXFLAG when CLANG=yes).
//
// PR-35c closes PR-33-A2_01: the C01 hoist was reorder-only — it
// rearranged `peerAddInclGlobal` entries already present, but never
// INJECTED the libcxx/libcxxrt slots when the runtime ancestor's
// `defaultPeerdirsFor` returned the empty set (the
// `library/cpp/malloc/api` case: NO_UTIL only, zero explicit
// PEERDIRs). For these modules, libcxx must be wired as a default
// peer so the existing two-phase peer-aggregation supplies the
// missing slots, and C01's hoist can lift them into the canonical
// order.
//
// The set is deliberately narrow:
//   - The C-runtime stack (musl, libc_compat, linuxvdso(/original),
//     builtins) compiles only C and would gain spurious -I libcxx
//     entries that the reference does not emit.
//   - The C++-runtime stack (libcxx, libcxxrt, libcxxabi, libcxxabi-
//     parts, libunwind) carries its own ADDINCL/CXXFLAGS declarations
//     in-tree and the reference emits a freestanding (`-nostdinc++`-
//     only) shape on those CC nodes; adding libcxx as a peer would
//     introduce flags they intentionally drop.
//   - util already pulls libcxx/libcxxrt headers via its existing
//     user-PEERDIRs (util/charset, zlib, double-conversion,
//     libc_compat) through the Phase 2 transitive walk; adding libcxx
//     here would duplicate work without fingerprint impact.
//   - sanitizer/include is header-only; consumers see its peer-GLOBAL
//     contributions via `walkPeersForGlobalAddIncl`, not through this
//     direct-peer mechanism.
//
// The single M2-closure member is `library/cpp/malloc/api`. New
// entries land here only when the reference graph confirms a runtime
// ancestor's CC nodes carry libcxx/libcxxrt -I + `-nostdinc++` and
// the existing peer-aggregation does not supply them.
var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

// isAncestorPath reports whether `srcDir` is an ancestor of
// `instancePath` (or equal to it). PR-30 D06 uses this to guard the
// SRCDIR full-rebase decision: the rebase fires only for the
// "include-from-parent" pattern (PROGRAM whose SRCDIR is an ancestor
// directory of the module path), where ymake's reference emits the
// PROGRAM's outputs under SRCDIR with module_dir = srcDir. LIBRARYs
// with SRCDIR pointing elsewhere (sibling, ancestor, or self) fall
// through to per-source SRCDIR routing in composeCCPaths, which keeps
// module_dir at instance.Path.
func isAncestorPath(srcDir, instancePath string) bool {
	if srcDir == instancePath {
		return true
	}

	return strings.HasPrefix(instancePath, srcDir+"/")
}

// isRuntimeAncestor reports whether instance.Path is a runtime
// ancestor (literal entry in `runtimeAncestorPaths`).
//
// PR-33 D01: dropped the `HasPrefix(prefix+"/")` subtree extension that
// also classified subtree members (e.g. `util/charset`,
// `contrib/libs/musl/full`, `libcxxabi-parts`) as runtime ancestors.
// The literal entries already self-suppress via the `instance.Path !=
// "..."` guards inside `defaultPeerdirsFor`, so the subtree extension
// was only blocking subtree members from auto-peering libcxx /
// libcxxrt / util / etc. Empirical cycle re-test (probe 2026-05-07):
// rc=0, cycle count = 7 (unchanged), L0/L1 unchanged at 98.77% /
// 98.74%; util/charset gains its libcxx/libcxxrt -I + -nostdinc++
// peer-GLOBAL contributions.
func isRuntimeAncestor(path string) bool {
	return runtimeAncestorPaths[path]
}

// runtimeStackAddInclPaths is the set of peer-GLOBAL ADDINCL `-I…`
// paths the upstream `_BUILTIN_PEERDIR` machinery hoists to the FRONT
// of a consumer's peer-GLOBAL include bundle, ahead of the musl/arch
// group and the user-PEERDIR contributions. These are the runtime-
// stack header roots: libcxx, libcxxrt, libcxxabi, libunwind. The
// reference graph emits these slots immediately after the linux-
// headers ccIncludesSuffix in every non-musl CC node — both for
// modules that declare these as direct peers (tools/archiver,
// util/charset) and for modules where they only reach the cmd_args
// transitively via a user PEERDIR's walk (util/_/digest/city.cpp.o,
// util's other CC nodes).
//
// PR-33 C01: declared explicitly here so `hoistRuntimeStackAddIncl`
// preserves the relative order across runtime ancestors when they
// appear as peer-GLOBAL contributions, regardless of which Phase
// (own-first vs transitive-second) actually picked them up.
// Paths are SOURCE_ROOT-relative — `appendAddIncl` (cc.go:867) adds
// the literal `-I$(S)/` prefix at emit time. Match the same
// representation here.
var runtimeStackAddInclPaths = map[string]int{
	"contrib/libs/cxxsupp/libcxx/include":    0,
	"contrib/libs/cxxsupp/libcxxrt/include":  1,
	"contrib/libs/cxxsupp/libcxxabi/include": 2,
	"contrib/libs/libunwind/include":         3,
}

// bundledAddInclPaths is the set of ADDINCL paths the cc bundle's
// `ccIncludesSuffix` (cc.go) injects directly into every non-musl CC
// node's cmd_args (slots between own AddIncl and peer AddInclGlobal).
// PR-35g: peer-propagated GLOBAL ADDINCL contributions whose path is
// already covered by the bundle MUST be deduped out of
// `peerAddInclGlobal` so they do not re-emit at a later slot.
//
// Empirical motivation: ragel6 host PIC walks musl/full → linux-headers
// (whose `ADDINCL(GLOBAL contrib/libs/linux-headers ...)` propagates),
// producing a duplicate emission at the tail of the peer-AddIncl block.
// The reference graph drops it because the cc bundle already supplies
// the same `-I$(S)/contrib/libs/linux-headers{,/_nf}` flags.
//
// Musl flavours bypass this filter: their composer drops
// PeerAddInclGlobal entirely (the `-nostdinc` + muslCcIncludes set
// defines the entire include search path explicitly).
var bundledAddInclPaths = map[string]bool{
	"contrib/libs/linux-headers":     true,
	"contrib/libs/linux-headers/_nf": true,
}

// suppressMallocAPIDefault drops `library/cpp/malloc/api` from a
// default-peer slice when the module declared `ALLOCATOR(FAKE)`.
// PR-35g: mirrors upstream `_BASE_UNIT`'s skip of the malloc/api
// auto-peer when ALLOCATOR=FAKE — yasm is the only M2-closure case.
// Returns the input unchanged when the gate is closed.
func suppressMallocAPIDefault(defaults []string, allocatorName string) []string {
	if allocatorName != "FAKE" {
		return defaults
	}

	out := make([]string, 0, len(defaults))

	for _, p := range defaults {
		if p == "library/cpp/malloc/api" {
			continue
		}

		out = append(out, p)
	}

	return out
}

// hoistRuntimeStackAddIncl returns `paths` with any entries from the
// runtime-stack ADDINCL set (libcxx/include, libcxxrt/include,
// libcxxabi/include, libunwind/include) moved to the front while
// preserving the canonical relative order between them. Non-runtime-
// stack entries keep their original relative order behind the
// hoisted prefix. The input is not mutated.
//
// PR-33 C01: util (a runtime-ancestor with empty default peer set
// other than musl/include) only picks up libcxx/libcxxrt -I via the
// transitive Phase 2 walk through user PEERDIRs (util/charset, zlib,
// double-conversion, libc_compat). Without hoisting, those slots
// land at the TAIL of util's peerAddInclGlobal — after musl-arch
// and the user paths — diverging from the reference. Modules that
// already declare libcxx/libcxxrt as direct peers (tools/archiver,
// util/charset) see no change because the hoist preserves the
// already-front ordering.
func hoistRuntimeStackAddIncl(paths []string) []string {
	if len(paths) == 0 {
		return paths
	}

	hoisted := make([]string, 0, len(paths))
	rest := make([]string, 0, len(paths))

	for _, p := range paths {
		if _, ok := runtimeStackAddInclPaths[p]; ok {
			hoisted = append(hoisted, p)
		} else {
			rest = append(rest, p)
		}
	}

	if len(hoisted) == 0 {
		return paths
	}

	// Sort hoisted by canonical relative order (libcxx < libcxxrt <
	// libcxxabi < libunwind). The dedup invariant in the caller keeps
	// each path at most once, so this is a stable selection sort.
	sort.SliceStable(hoisted, func(i, j int) bool {
		return runtimeStackAddInclPaths[hoisted[i]] < runtimeStackAddInclPaths[hoisted[j]]
	})

	out := make([]string, 0, len(paths))
	out = append(out, hoisted...)
	out = append(out, rest...)

	return out
}

// defaultPeerdirsFor returns the implicit DEFAULT_PEERDIRs ymake
// adds automatically based on language + module flavor. PR-26 hard-
// coded the upstream `_BUILTIN_PEERDIR` mechanism for CPP modules
// because reproducing it from `build/conf/` in a faithful evaluator
// is M5+ work and the gap between the explicit-only walker (50 nodes
// for tools/archiver) and the reference (3,730 nodes) is dominated
// by these implicit peers (musl alone is 2,656 nodes). PR-27
// completes the set — libcxx / libcxxrt / libunwind / util — once
// the parser learned `==` and `<` so those modules' ya.makes parse.
//
// Suppression model: ymake's `NO_PLATFORM` is the umbrella switch
// that disables every implicit peer below; the more granular flags
// (`NO_LIBC` / `NO_RUNTIME` / `NO_UTIL`) each disable one piece.
// A module that sets all three granular flags is effectively
// platform-less even if it does not type the `NO_PLATFORM` macro
// itself — that is how `build/cow/on` (the M1 leaf) ends up with
// zero peer deps in the reference graph despite never typing
// `NO_PLATFORM()`. The helper treats the combination as an
// effective `NO_PLATFORM` via `effectiveNoPlatform`.
//
// CPP modules implicitly PEERDIR (unless suppressed):
//
//   - contrib/libs/musl              — suppressed by NO_LIBC or
//     effective NO_PLATFORM
//   - contrib/libs/cxxsupp/builtins  — suppressed by NO_RUNTIME or
//     effective NO_PLATFORM
//   - library/cpp/malloc/api         — suppressed by effective
//     NO_PLATFORM
//   - contrib/libs/cxxsupp/libcxx    — suppressed by NO_RUNTIME or
//     effective NO_PLATFORM (PR-27)
//   - contrib/libs/cxxsupp/libcxxrt  — suppressed by NO_RUNTIME or
//     effective NO_PLATFORM (PR-27)
//   - contrib/libs/libunwind         — suppressed by NO_RUNTIME or
//     effective NO_PLATFORM (PR-27)
//   - util                           — suppressed by NO_UTIL or
//     effective NO_PLATFORM (PR-27)
//
// The libcxx/libcxxrt/libunwind suppression by NO_RUNTIME is the
// upstream behaviour: those three are runtime-support libraries,
// pulled in only when the consumer wants the C++ runtime
// scaffolding. util is separately gated by NO_UTIL because util is
// the Yandex stdlib analogue, conceptually distinct from the
// language runtime.
//
// Cycle prevention: the helper guards against adding a module as its
// own peer via the `instance.Path != "..."` checks (and a prefix
// match for musl, libcxx, util, which have sub-modules underneath).
// The walker's own `walking` stack catches deeper cycles.
//
// Returns empty for non-CPP languages — proto / go / py / java will
// get their own helpers in M5+.
//
// `ctx` is consulted only for the target-axis discriminator on `util`
// (PR-28-D08); a nil ctx falls back to the M2-canonical
// `DefaultLinuxConfig.Target.ID` so unit tests that exercise the
// helper directly do not have to thread a real context through.
func defaultPeerdirsFor(ctx *genCtx, instance ModuleInstance) []string {
	if instance.Language != LangCPP {
		return nil
	}

	// PR-27 + PR-32 D03: runtime-ancestor modules (libcxx, libcxxrt,
	// libunwind, musl, malloc/api, util, ...) get zero RUNTIME-stack
	// implicit peers AND the musl/include auto-PEERDIR (when MUSL=yes
	// and not LibcMusl-self). The two-phase peer-aggregation in the
	// walker (own-first, transitive-second) ensures the musl-arch
	// paths from these runtime-ancestors propagate AFTER the libcxx-
	// include / libcxxrt-include paths libcxx and libcxxrt themselves
	// declare, matching the reference cmd_args ordering.
	noPlatform := effectiveNoPlatform(instance.Flags)

	if isRuntimeAncestor(instance.Path) {
		var only []string

		if !noPlatform && !instance.Flags.LibcMusl && cliMuslOn(ctx) {
			only = append(only, "contrib/libs/musl/include")
		}

		return only
	}

	var peers []string

	// PR-42: contrib/libs/musl is reached transitively via contrib/libs/musl/full
	// (program-default); upstream conf does NOT add it as a direct peer of arbitrary
	// consumers (verified against build/ymake.core.conf:760-1255 and musl/full/ya.make).

	// PR-42: contrib/libs/cxxsupp/builtins is reached transitively via
	// contrib/libs/cxxsupp/libcxx → builtins (libcxx's ya.make PEERDIR);
	// upstream conf does NOT add builtins as a direct peer of arbitrary consumers.

	// PR-42: library/cpp/malloc/api is reached transitively via
	// library/cpp/malloc/tcmalloc → api (program-default allocator walk);
	// upstream conf does NOT add malloc/api as a direct peer of arbitrary consumers.

	// PR-27: complete the implicit-peer set. libcxx / libcxxrt /
	// libunwind are gated by NO_RUNTIME (same as builtins); util
	// is gated by NO_UTIL. Each is suppressed for the module's
	// own subtree to break the obvious self-cycle.
	if !instance.Flags.NoRuntime && !noPlatform {
		if instance.Path != "contrib/libs/cxxsupp/libcxx" && !strings.HasPrefix(instance.Path, "contrib/libs/cxxsupp/libcxx/") {
			peers = append(peers, "contrib/libs/cxxsupp/libcxx")
		}

		if instance.Path != "contrib/libs/cxxsupp/libcxxrt" {
			peers = append(peers, "contrib/libs/cxxsupp/libcxxrt")
		}

		if instance.Path != "contrib/libs/libunwind" {
			peers = append(peers, "contrib/libs/libunwind")
		}
	}

	// PR-M3-F-1: util is an implicit peer for ALL CPP modules (both target
	// and host) unless suppressed by NO_UTIL / effective NO_PLATFORM.
	// The reference graph (sg2.json) includes util on default-linux-x86_64
	// for host PROGRAM modules (tools/archiver, tools/rescompiler, etc.).
	// Prior code restricted util to target-platform only; this caused ~24
	// missing x86_64 util nodes in M3. M2 is unaffected because its host
	// tools (ragel6, yasm) both declare NO_UTIL / NO_PLATFORM.
	if !instance.Flags.NoUtil && !noPlatform {
		if instance.Path != "util" && !strings.HasPrefix(instance.Path, "util/") {
			peers = append(peers, "util")
		}
	}

	// PR-32 D03: mirror `build/ymake.core.conf:781`'s
	// `when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include }`.
	// Every TARGET LIBRARY/PROGRAM that is not NO_PLATFORM gets an
	// implicit peer on `contrib/libs/musl/include`. The peer is
	// header-only (PR-31 path) so its 4 GLOBAL ADDINCL paths
	// (musl/arch/{x86_64,aarch64,generic}, musl/include, musl/extra,
	// the linux-headers pair) propagate to consumers' CC cmd_args
	// AND scanner search paths, closing the L2-stagnation gap
	// PR-31-D12 identified.
	//
	// Suppression model:
	//   - musl-self subtree → caught by `isRuntimeAncestor` early-exit above.
	//   - Effective NO_PLATFORM → suppressed (matches `_BASE_UNIT`'s
	//     gate; build/cow/on is the M1 example).
	//   - MUSL != "yes" in cliDefines → suppressed entirely.
	if !noPlatform && cliMuslOn(ctx) {
		peers = append(peers, "contrib/libs/musl/include")
	}

	return peers
}

// cliMuslOn reports whether the CLI bound `MUSL` to `"yes"` (PR-32
// D01/D03). Centralises the check so the auto-PEERDIR rule and the
// `-D_musl_` peer-CFLAG injection consult the same predicate. A nil
// `ctx` (synthetic test path) defaults to the M2 canonical state
// (MUSL=yes) so existing direct-call tests of `defaultPeerdirsFor`
// see the same auto-peer set as the real walker.
func cliMuslOn(ctx *genCtx) bool {
	if ctx == nil {
		return true
	}

	return ctx.target.Flags["MUSL"] == "yes"
}

// defaultPeerCFlags returns the auto-injected peer-CFLAG set the
// walker contributes to ModuleCCInputs.AutoPeerCFlags (PR-32 D09).
// Mirrors `_BASE_UNIT`'s `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`
// (build/ymake.core.conf:781). The `-D_musl_` sentinel (no `=1`)
// applies to consumers; musl-self CC nodes get `-D_musl_=1` from
// `muslExtraDefines` instead and are gated off this auto-injection
// via the LibcMusl + effective-NO_PLATFORM checks. Returns nil when
// the gate is closed so the slot stays empty in cmd_args.
func defaultPeerCFlags(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	if !cliMuslOn(ctx) {
		return nil
	}

	if instance.Flags.LibcMusl {
		return nil
	}

	if effectiveNoPlatform(d.flags) {
		return nil
	}

	out := []string{muslConsumerSentinel}

	// PR-M3-python-addincl-cflags: `_PYTHON3_ADDINCL`'s
	// `CFLAGS+=-DUSE_PYTHON3` (python.conf:1019, gated on
	// $USE_ARCADIA_PYTHON == "yes") lands here. The reference places it
	// at the AutoPeerCFlags slot — right after `-D_musl_`, before the
	// second `noLibcUndebugBlock` copy — e.g. library/python/runtime_py3
	// /__res.cpp.o ref:93, library/cpp/pybind/cast.cpp.py3.o ref:83.
	// `contrib/libs/python` itself is skipped via the modulePath guard
	// in `applyPython3AddIncl`; the `usePython3` flag captures that decision.
	if d.usePython3 && instance.Path != "contrib/libs/python" {
		out = append(out, "-DUSE_PYTHON3")
	}

	return out
}

// muslConsumerSentinel is the `-D_musl_` flag that
// `_BASE_UNIT`'s `when ($MUSL == "yes")` rule auto-injects into every
// non-NO_PLATFORM module's CFLAGS. Distinct from `-D_musl_=1` (which
// is musl-self only and lives in `muslExtraDefines`). PR-32 D09.
const muslConsumerSentinel = "-D_musl_"

// defaultProgramPeerdirsFor returns the implicit DEFAULT_PEERDIRs that
// upstream `_BASE_PROGRAM` (`build/ymake.core.conf:1219-1253`) attaches
// to PROGRAM modules in our M2 environment (MUSL=yes, OS_LINUX=yes,
// CLANG=yes, no sanitizer). PR-30 D02 + D03:
//
//   - `MUSL=yes && !MUSL_LITE` → `contrib/libs/musl/full`. Drives the
//     host `musl/full → asmlib + asmglibc + linux-headers` cascade and
//     the asmlib host AS sources' yasm trigger (which then pulls
//     jemalloc + musl_extra via yasm's own PEERDIRs).
//   - PROGRAM with no explicit `ALLOCATOR(...)` macro AND `MUSL=yes`
//     AND `OS_LINUX=yes` → default ALLOCATOR=TCMALLOC_TC →
//     `library/cpp/malloc/tcmalloc` + `contrib/libs/tcmalloc/no_percpu_cache`
//     (which transitively peers `contrib/libs/tcmalloc/malloc_extension`
//     and `contrib/restricted/abseil-cpp` via its common.inc).
//
// The helper does NOT model the GCC, sanitizer, or non-Linux paths;
// future closures that hit those will need a richer environment-driven
// dispatch (R2 of the PR-30 plan flags this as a known gap).
//
// Suppression: when `instance` is itself a runtime-ancestor module
// (covered by `isRuntimeAncestor`), `defaultPeerdirsFor` already
// returns nil; the PROGRAM-default helper is only consulted from the
// non-ancestor branch in `genModule`. PROGRAMs that ARE runtime
// ancestors (none in the M2 closure) would still get the
// program-default peers from this helper — `genModule` callers can
// suppress by checking `isRuntimeAncestor` themselves if a future
// closure surfaces such a case.
//
// PR-43: split into pre-user and post-user halves via the `includeMusl`
// parameter.  When `includeMusl=false` the musl/full (or bare musl)
// tail is omitted; when `includeMusl=true` only the musl tail is
// returned.  `genModule` calls this twice so it can interleave the
// allocator explicit peers (kept as peerKindUserPeer for GLOBAL phase
// ordering) and the regular d.peerdirs between the two halves.
func defaultProgramPeerdirsFor(ctx *genCtx, instance ModuleInstance, hadAllocator bool, allocatorName string, muslLiteOverride bool, includeMusl bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	env := buildIfEnv(instance)
	// PR-32 D02: MUSL gate reads from cliDefines (CLI -DMUSL=yes/no);
	// fall back to env.Bool("MUSL") when ctx is nil so the unit-test
	// helper path keeps working. The default in `Gen` seeds MUSL=yes
	// so back-compat callers see no change.
	muslOn := env.Bool("MUSL")

	if ctx != nil {
		muslOn = ctx.target.Flags["MUSL"] == "yes"
	}

	muslLite := env.Bool("MUSL_LITE") || muslLiteOverride
	osLinux := env.Bool("OS_LINUX")

	var peers []string

	if !includeMusl {
		// PR-35c: USE_COW=yes M2 default — every PROGRAM gets `build/cow/on`
		// as an implicit peer. Mirrors `_BASE_PROGRAM`'s
		// `when ($USE_COW == "yes") { PEERDIR += build/cow/on }` at
		// `build/ymake.core.conf:946-948`. Declared BEFORE the allocator block
		// (conf line 946 precedes the allocator select at line 959) so post-order
		// DFS places build/cow/on before the tcmalloc subtree. PR-42: reordered
		// to match upstream conf declaration sequence.
		peers = append(peers, "build/cow/on")

		// PR-30 D03: default ALLOCATOR=TCMALLOC_TC for our M2 environment
		// (MUSL=yes, OS_LINUX=yes). PROGRAMs that explicitly declare
		// ALLOCATOR(NAME) go through allocatorPeers; this default fires
		// only when neither was declared.
		if !hadAllocator && muslOn && osLinux {
			// TCMALLOC_TC peer set; mirrors allocatorPeers["TCMALLOC_TC"].
			// Listed inline so the PROGRAM-default path remains
			// self-documenting alongside the M2 environment guards.
			peers = append(peers,
				"library/cpp/malloc/tcmalloc",
				"contrib/libs/tcmalloc/no_percpu_cache",
			)
		}
	} else {
		// PR-42: musl block declared AFTER the allocator block in upstream conf
		// (build/ymake.core.conf:1238-1244, after allocator select at :959-1036).
		// Post-order DFS places musl after the tcmalloc subtree, matching REF
		// slots 47-48 (musl, musl/full) vs slots 41-46 (cow + tcmalloc cluster).
		// PR-43: musl is in the post-user half so that explicit ALLOCATOR peers
		// (kept as peerKindUserPeer) land before musl/full in the archive walk.
		if muslOn && !muslLite {
			// Caller (defaultPeerdirsFor in gen.go:932) gates on !isRuntimeAncestor(instance.Path)
			// which already excludes contrib/libs/musl/* (incl. musl/full). No self-suppression needed here.
			const muslFullPath = "contrib/libs/musl/full"
			peers = append(peers, muslFullPath)
		}

		if muslOn && muslLite {
			// PR-42: upstream conf build/ymake.core.conf:1239-1240 adds bare contrib/libs/musl
			// (not musl/full) when MUSL_LITE=yes. Mirrors the MUSL_LITE branch of _BASE_PROGRAM.
			// Modules like contrib/tools/yasm declare ENABLE(MUSL_LITE) to get musl without
			// the full allocator+tcmalloc cascade.
			peers = append(peers, "contrib/libs/musl")
		}

		// PR-M3-cpuid-check-host-peerdir: upstream `_BASE_PROGRAM` at
		// build/ymake.core.conf:1247-1254 declares `DEFAULT(CPU_CHECK yes)`
		// gated off by `USE_SSE4 != yes || NOUTIL == yes || ALLOCATOR == FAKE`.
		// USE_SSE4 defaults yes only when `ARCH_X86_64 || ARCH_I386` (conf
		// :3057-3132); the `otherwise` branch at :3128-3132 forces
		// `USE_SSE4=no` AND `CPU_CHECK=no`, so the predicate collapses to
		// (ARCH_X86_64 && !NoUtil && ALLOCATOR != "FAKE") in our M2/M3
		// environment (i386 not in closure). Declared after musl/full to
		// mirror the upstream conf order (:1238-1244 musl, :1252-1254
		// cpuid_check). Closes the L0 reshuffle (logger@aarch64 + every
		// downstream EN/CC chain) cascading off host x86_64 PROGRAMs.
		if env.Bool("ARCH_X86_64") && !instance.Flags.NoUtil && allocatorName != "FAKE" {
			peers = append(peers, "library/cpp/cpuid_check")
		}
	}

	return peers
}

// effectiveNoPlatform reports true when the FlagSet's combination
// behaves as `NO_PLATFORM` in upstream ymake — i.e., NoLibc + NoUtil +
// NoRuntime all set. The M1 leaf `build/cow/on` exhibits this pattern
// via the `inferFlagsFromPath` heuristic (module.go:161-165), which
// seeds the triple from the path alone. Macro-driven examples (a real
// ya.make that types all three NO_* without typing NO_PLATFORM) await
// a future closure module.
func effectiveNoPlatform(f FlagSet) bool {
	if f.NoPlatform {
		return true
	}

	return f.NoLibc && f.NoRuntime && f.NoUtil
}

// peerYaMakeExists reports whether `<sourceRoot>/<peerPath>/ya.make`
// is a regular file. Used by the default-peer walk to skip implicit
// peers that are not present in the (possibly synthetic) source root,
// rather than throwing the parser's "no such file" error. Explicit
// PEERDIRs do not go through this filter — a missing explicit peer
// is a real bug.
func peerYaMakeExists(sourceRoot, peerPath string) bool {
	_, err := os.Stat(filepath.Join(sourceRoot, peerPath, "ya.make"))

	if err == nil {
		return true
	}

	if errors.Is(err, fs.ErrNotExist) {
		return false
	}

	ThrowFmt("gen: failed to stat default-peer ya.make %q: %v", filepath.Join(sourceRoot, peerPath, "ya.make"), err)

	return false // unreachable
}
