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

// runtimeAncestorPaths is the set of module paths that are part of the
// platform/runtime stack and therefore receive NO implicit default
// peers. Matches the reference: each has zero peer-archive deps in AR.
//
// Upstream ymake special-cases these in `_BUILTIN_PEERDIR`; we
// hard-code the closure-membership set instead. The list is the union
// of C runtime (musl, libc_compat, linuxvdso[/original]), C++ runtime
// (cxxsupp/{builtins, libcxx, libcxxrt, libcxxabi, libcxxabi-parts},
// libunwind), allocator API (library/cpp/malloc/api), sanitizer shim
// (library/cpp/sanitizer/include), and the Yandex stdlib root (util).
//
// Membership causes `defaultPeerdirsFor` to return empty regardless
// of FlagSet. New entries require the reference graph to confirm
// zero peer-archive deps AND a walker cycle through the module.
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
// whose C++ sources include libcxx headers and therefore need libcxx
// as an implicit GLOBAL header peer to supply `-I libcxx/include`,
// `-I libcxxrt/include`, and `-nostdinc++`.
//
// Without this set, runtime ancestors whose `defaultPeerdirsFor`
// returns empty (`library/cpp/malloc/api`: NO_UTIL only, zero explicit
// PEERDIRs) lack libcxx wiring — the C01 hoist reorders only, it
// never injects.
//
// Deliberately narrow:
//   - C-runtime (musl, libc_compat, linuxvdso, builtins) compiles
//     only C; adding libcxx would emit spurious -I.
//   - C++-runtime (libcxx, libcxxrt, libcxxabi, libcxxabi-parts,
//     libunwind) carries its own ADDINCL/CXXFLAGS; adding peers
//     would introduce flags it intentionally drops.
//   - util reaches libcxx/libcxxrt via existing user-PEERDIRs.
//   - sanitizer/include consumers see GLOBAL via walkPeersForGlobalAddIncl.
//
// Currently a single member: `library/cpp/malloc/api`.
var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

// isAncestorPath reports whether `srcDir` is an ancestor of (or equal
// to) `instancePath`. Guards the SRCDIR full-rebase decision: the
// rebase fires only for the "include-from-parent" pattern (PROGRAM
// with SRCDIR an ancestor of its module path), where ymake emits
// outputs under SRCDIR with module_dir = srcDir. LIBRARYs with SRCDIR
// elsewhere fall through to per-source SRCDIR routing in composeCCPaths.
func isAncestorPath(srcDir, instancePath string) bool {
	if srcDir == instancePath {
		return true
	}

	return strings.HasPrefix(instancePath, srcDir+"/")
}

// isRuntimeAncestor reports whether instance.Path is a literal entry
// in runtimeAncestorPaths. Subtree members (util/charset, musl/full,
// libcxxabi-parts) are NOT classified here — they need to auto-peer
// libcxx/libcxxrt/util themselves. The literal entries self-suppress
// via path-equality guards in `defaultPeerdirsFor`.
func isRuntimeAncestor(path string) bool {
	return runtimeAncestorPaths[path]
}

// runtimeStackAddInclPaths is the set of peer-GLOBAL ADDINCL paths
// upstream `_BUILTIN_PEERDIR` hoists to the FRONT of a consumer's
// peer-GLOBAL include bundle, ahead of musl/arch and user-PEERDIRs.
// The runtime-stack header roots: libcxx, libcxxrt, libcxxabi, libunwind.
// Reference emits these immediately after linux-headers ccIncludesSuffix
// in every non-musl CC node — both for direct peers and for transitive
// reach through user PEERDIRs.
//
// `hoistRuntimeStackAddIncl` preserves the relative order across
// runtime ancestors regardless of which aggregation phase picked them
// up. Paths are SOURCE_ROOT-relative; `appendAddIncl` adds the
// literal `-I$(S)/` prefix at emit time.
var runtimeStackAddInclPaths = map[VFS]int{
	Source("contrib/libs/cxxsupp/libcxx/include"):    0,
	Source("contrib/libs/cxxsupp/libcxxrt/include"):  1,
	Source("contrib/libs/cxxsupp/libcxxabi/include"): 2,
	Source("contrib/libs/libunwind/include"):         3,
}

// bundledAddInclPaths is the set of ADDINCL paths the cc bundle's
// `ccIncludesSuffix` injects directly into every non-musl CC node's
// cmd_args. Peer-propagated GLOBAL ADDINCL contributions covered by
// the bundle MUST be deduped out of `peerAddInclGlobal` so they do
// not re-emit at a later slot.
//
// Empirically: ragel6 host PIC walks musl/full → linux-headers,
// producing a duplicate `-I$(S)/contrib/libs/linux-headers{,/_nf}`
// at the peer-AddIncl tail; reference drops it because the cc bundle
// already supplies these flags.
//
// Musl flavours bypass this filter: their composer drops
// PeerAddInclGlobal entirely (`-nostdinc` + muslCcIncludes define
// the entire include search path).
var bundledAddInclPaths = map[VFS]bool{
	Source("contrib/libs/linux-headers"):     true,
	Source("contrib/libs/linux-headers/_nf"): true,
}

// suppressMallocAPIDefault drops `library/cpp/malloc/api` from a
// default-peer slice when the module declared `ALLOCATOR(FAKE)`.
// Mirrors upstream `_BASE_UNIT`'s skip of the malloc/api auto-peer.
// Returns input unchanged when the gate is closed.
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

// hoistRuntimeStackAddIncl returns `paths` with runtime-stack ADDINCL
// entries (libcxx, libcxxrt, libcxxabi, libunwind /include) moved to
// the front in canonical order. Non-runtime-stack entries keep their
// original relative order. Input is not mutated.
//
// Without hoisting, util (whose only default peer is musl/include)
// picks up libcxx/libcxxrt -I via transitive Phase 2 walks through
// user PEERDIRs, landing them at the peerAddInclGlobal TAIL — after
// musl-arch and user paths — diverging from the reference. Modules
// that already declare libcxx/libcxxrt as direct peers see no change.
func hoistRuntimeStackAddIncl(paths []VFS) []VFS {
	if len(paths) == 0 {
		return paths
	}

	hoisted := make([]VFS, 0, len(paths))
	rest := make([]VFS, 0, len(paths))

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

	out := make([]VFS, 0, len(paths))
	out = append(out, hoisted...)
	out = append(out, rest...)

	return out
}

// defaultPeerdirsFor returns the implicit DEFAULT_PEERDIRs ymake adds
// based on language + module flavor. Hand-coded mirror of upstream
// `_BUILTIN_PEERDIR`; the implicit peers dominate the closure
// (musl alone is 2,656 of 3,730 nodes for tools/archiver).
//
// Suppression model: ymake's `NO_PLATFORM` is the umbrella switch
// disabling every implicit peer; granular flags (`NO_LIBC`,
// `NO_RUNTIME`, `NO_UTIL`) each disable one piece. A module setting
// all three granular flags is effectively platform-less even without
// typing `NO_PLATFORM` (e.g. `build/cow/on`). `effectiveNoPlatform`
// captures this.
//
// CPP modules implicitly PEERDIR (unless suppressed):
//   - contrib/libs/cxxsupp/libcxx    — NO_RUNTIME / NO_PLATFORM
//   - contrib/libs/cxxsupp/libcxxrt  — NO_RUNTIME / NO_PLATFORM
//   - contrib/libs/libunwind         — NO_RUNTIME / NO_PLATFORM
//   - util                           — NO_UTIL / NO_PLATFORM
//   - contrib/libs/musl/include      — NO_PLATFORM (gated on MUSL=yes)
//
// libcxx/libcxxrt/libunwind are runtime-support libs gated by
// NO_RUNTIME; util is the Yandex stdlib analogue gated by NO_UTIL.
//
// Cycle prevention: path-equality guards against adding a module as
// its own peer, plus prefix matches for musl/libcxx/util subtrees.
// The walker's `walking` stack catches deeper cycles.
//
// Returns empty for non-CPP languages.
func defaultPeerdirsFor(ctx *genCtx, instance ModuleInstance) []string {
	return defaultPeerdirsForWithState(ctx, instance, effectiveMuslOn(ctx, nil))
}

func defaultPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	return defaultPeerdirsForWithState(ctx, instance, effectiveMuslOn(ctx, d))
}

func defaultPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	// Runtime-ancestor modules (libcxx, libcxxrt, libunwind, musl,
	// malloc/api, util, ...) get zero RUNTIME-stack implicit peers
	// AND the musl/include auto-PEERDIR (when MUSL=yes and not
	// no-stdinc itself). Two-phase peer-aggregation ensures musl-arch
	// paths propagate AFTER libcxx/libcxxrt include paths, matching
	// the reference cmd_args ordering.
	noPlatform := effectiveNoPlatform(instance.Flags)

	if isRuntimeAncestor(instance.Path) {
		var only []string

		if !noPlatform && !instance.Flags.NoStdInc && muslOn {
			only = append(only, "contrib/libs/musl/include")
		}

		if runtimeAncestorCxxConsumers[instance.Path] && useArcadiaCompilerRuntime(ctx, instance) && instance.Path != "library/cpp/sanitizer/include" {
			only = append(only, "library/cpp/sanitizer/include")
		}

		return only
	}

	var peers []string

	// musl, builtins, malloc/api are reached TRANSITIVELY (via
	// musl/full, libcxx, tcmalloc respectively); upstream conf does
	// NOT add them as direct peers of arbitrary consumers.

	// libcxx / libcxxrt / libunwind: gated by NO_RUNTIME; util gated
	// by NO_UTIL. Each suppressed for its own subtree (self-cycle).
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

	// util is an implicit peer for ALL CPP modules (target and host)
	// unless suppressed by NO_UTIL / effective NO_PLATFORM. The
	// reference includes util on default-linux-x86_64 for host PROGRAM
	// modules (tools/archiver, tools/rescompiler, etc.).
	if !instance.Flags.NoUtil && !noPlatform {
		if instance.Path != "util" && !strings.HasPrefix(instance.Path, "util/") {
			peers = append(peers, "util")
		}
	}

	// Mirrors `build/ymake.core.conf:781`:
	// `when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include }`.
	// Every LIBRARY/PROGRAM that is not NO_PLATFORM gets an implicit
	// peer on `contrib/libs/musl/include`. Header-only so its 4 GLOBAL
	// ADDINCL paths propagate to consumers' CC cmd_args + scanner.
	//
	// Suppression: musl-self subtree (caught by isRuntimeAncestor),
	// effective NO_PLATFORM, MUSL != "yes" in cliDefines.
	if !noPlatform && muslOn {
		peers = append(peers, "contrib/libs/musl/include")
	}

	if !instance.Flags.NoRuntime && !noPlatform && useArcadiaCompilerRuntime(ctx, instance) && instance.Path != "library/cpp/sanitizer/include" {
		peers = append(peers, "library/cpp/sanitizer/include")
	}

	return peers
}

func effectiveMuslOn(ctx *genCtx, d *moduleData) bool {
	if d != nil {
		return d.muslEnabled
	}

	return cliMuslEnabled(ctx)
}

func useArcadiaCompilerRuntime(ctx *genCtx, instance ModuleInstance) bool {
	if instance.Platform != nil {
		if v := instance.Platform.Flags["USE_ARCADIA_COMPILER_RUNTIME"]; v != "" {
			return v != "no"
		}
	}

	if ctx == nil {
		return false
	}

	if v := ctx.target.Flags["USE_ARCADIA_COMPILER_RUNTIME"]; v != "" {
		return v != "no"
	}

	return true
}

// cliMuslEnabled reports whether the CLI bound `MUSL` to `"yes"`.
// Centralised so auto-PEERDIR and `-D_musl_` peer-CFLAG injection
// share the same predicate. nil `ctx` defaults to MUSL=yes for
// direct-call tests.
func cliMuslEnabled(ctx *genCtx) bool {
	if ctx == nil {
		return true
	}

	return ctx.target.Flags["MUSL"] == "yes"
}

// defaultPeerCFlags returns the auto-injected peer-CFLAG set for
// ModuleCCInputs.AutoPeerCFlags. Mirrors `_BASE_UNIT`'s
// `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }` (ymake.core.conf:781).
// `-D_musl_` (no `=1`) is consumer-side; musl-self CC nodes get
// `-D_musl_=1` from `muslExtraDefines` instead, gated off this
// injection via NoStdInc + effective-NO_PLATFORM checks.
func defaultPeerCFlags(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	if !effectiveMuslOn(ctx, d) {
		return nil
	}

	if instance.Flags.NoStdInc {
		return nil
	}

	if effectiveNoPlatform(d.flags) {
		return nil
	}

	// `_PYTHON3_ADDINCL`'s `CFLAGS+=-DUSE_PYTHON3` (python.conf:1019,
	// gated on $USE_ARCADIA_PYTHON == "yes"). Reference places it at
	// the AutoPeerCFlags slot — right after `-D_musl_`, before the
	// second `noLibcUndebugBlock`. `contrib/libs/python` is skipped via
	// the modulePath guard in `applyPython3AddIncl`.
	usePython3 := d.usePython3 && instance.Path != "contrib/libs/python"

	return consumerAutoPeerCFlags(true, usePython3)
}

// consumerAutoPeerCFlags is the single source of literal strings for
// the consumer-side auto-peer CFLAG slice. Predicate evaluation stays
// at the caller; this helper just centralises flag names + ordering so
// emitter composers cannot drift. Order: musl sentinel, then USE_PYTHON3.
func consumerAutoPeerCFlags(muslOn, usePython3 bool) []string {
	var out []string
	if muslOn {
		out = append(out, muslConsumerSentinel)
	}
	if usePython3 {
		out = append(out, "-DUSE_PYTHON3")
	}
	return out
}

// muslConsumerSentinel is `-D_musl_`, auto-injected by `_BASE_UNIT`'s
// `when ($MUSL == "yes")` into every non-NO_PLATFORM module's CFLAGS.
// Distinct from `-D_musl_=1` (musl-self only, in `muslExtraDefines`).
const muslConsumerSentinel = "-D_musl_"

// defaultProgramPeerdirsFor returns the implicit DEFAULT_PEERDIRs that
// upstream `_BASE_PROGRAM` (build/ymake.core.conf:1219-1253) attaches
// to PROGRAM modules in our environment (MUSL=yes, OS_LINUX=yes,
// CLANG=yes, no sanitizer):
//
//   - `MUSL=yes && !MUSL_LITE` → `contrib/libs/musl/full`, driving
//     the musl/full → asmlib + asmglibc + linux-headers cascade and
//     the asmlib AS sources' yasm trigger.
//   - PROGRAM with no explicit ALLOCATOR(...) AND MUSL=yes AND
//     OS_LINUX=yes → default ALLOCATOR=TCMALLOC_TC →
//     `library/cpp/malloc/tcmalloc` + `contrib/libs/tcmalloc/no_percpu_cache`
//     (which transitively peers malloc_extension + abseil-cpp).
//
// GCC, sanitizer, non-Linux paths not modelled.
//
// `includeMusl` splits into pre-user (false: cow/on + tcmalloc) and
// post-user (true: musl + cpuid_check) halves. genModule calls twice
// to interleave allocator explicit peers and d.peerdirs between halves.
func defaultProgramPeerdirsFor(ctx *genCtx, instance ModuleInstance, hadAllocator bool, allocatorName string, muslLiteOverride bool, includeMusl bool) []string {
	return defaultProgramPeerdirsForWithState(ctx, instance, hadAllocator, allocatorName, muslLiteOverride, includeMusl, effectiveMuslOn(ctx, nil))
}

func defaultProgramPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData, includeMusl bool) []string {
	return defaultProgramPeerdirsForWithState(ctx, instance, d.hadAllocator, d.allocatorName, d.muslLite, includeMusl, effectiveMuslOn(ctx, d))
}

func defaultProgramPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, hadAllocator bool, allocatorName string, muslLiteOverride bool, includeMusl bool, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	env := buildIfEnv(instance)
	muslLite := env.Bool("MUSL_LITE") || muslLiteOverride
	osLinux := env.Bool("OS_LINUX")

	var peers []string

	if !includeMusl {
		// USE_COW=yes default: every PROGRAM gets `build/cow/on`.
		// Mirrors `_BASE_PROGRAM` `when ($USE_COW == "yes")` at
		// ymake.core.conf:946-948. Declared BEFORE the allocator block
		// (conf:946 precedes allocator select at conf:959) so post-order
		// DFS places build/cow/on before tcmalloc.
		peers = append(peers, "build/cow/on")

		// Default ALLOCATOR=TCMALLOC_TC for MUSL=yes + OS_LINUX=yes.
		// PROGRAMs declaring ALLOCATOR(NAME) go through allocatorPeers;
		// this default fires only when neither was declared.
		if !hadAllocator && muslOn && osLinux {
			// TCMALLOC_TC peer set; mirrors allocatorPeers["TCMALLOC_TC"].
			peers = append(peers,
				"library/cpp/malloc/tcmalloc",
				"contrib/libs/tcmalloc/no_percpu_cache",
			)
		}
	} else {
		// musl block declared AFTER allocator in upstream conf
		// (ymake.core.conf:1238-1244 vs allocator select :959-1036).
		// Post-order DFS places musl after tcmalloc, matching REF
		// slots 47-48 (musl, musl/full) vs 41-46 (cow + tcmalloc).
		// In the post-user half so explicit ALLOCATOR peers land
		// before musl/full in the archive walk.
		if muslOn && !muslLite {
			const muslFullPath = "contrib/libs/musl/full"
			peers = append(peers, muslFullPath)
		}

		if muslOn && muslLite {
			// MUSL_LITE branch: bare contrib/libs/musl (not /full),
			// per ymake.core.conf:1239-1240. e.g. contrib/tools/yasm
			// declares ENABLE(MUSL_LITE) to get musl without the
			// allocator+tcmalloc cascade.
			peers = append(peers, "contrib/libs/musl")
		}

		// `_BASE_PROGRAM` at ymake.core.conf:1247-1254 declares
		// `DEFAULT(CPU_CHECK yes)` gated off by
		// `USE_SSE4 != yes || NOUTIL == yes || ALLOCATOR == FAKE`.
		// USE_SSE4 defaults yes only when ARCH_X86_64 || ARCH_I386
		// (conf:3057-3132); the predicate collapses to
		// (ARCH_X86_64 && !NoUtil && ALLOCATOR != "FAKE") in our env.
		// Declared after musl/full to mirror conf order.
		if env.Bool("ARCH_X86_64") && !instance.Flags.NoUtil && allocatorName != "FAKE" {
			peers = append(peers, "library/cpp/cpuid_check")
		}
	}

	return peers
}

// effectiveNoPlatform reports true when the FlagSet combination behaves
// as `NO_PLATFORM` in upstream ymake — NoLibc + NoUtil + NoRuntime all
// set. `build/cow/on` exhibits this pattern via `inferFlagsFromPath`
// (module.go:161-165) which seeds the triple from the path alone.
func effectiveNoPlatform(f FlagSet) bool {
	if f.NoPlatform {
		return true
	}

	return f.NoLibc && f.NoRuntime && f.NoUtil
}

// peerYaMakeExists reports whether `<sourceRoot>/<peerPath>/ya.make`
// is a regular file. Used by the default-peer walk to skip implicit
// peers not present in (possibly synthetic) source roots, rather than
// throwing the parser's "no such file" error. Explicit PEERDIRs do
// not go through this filter — a missing explicit peer is a defect.
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
