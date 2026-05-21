package main

// defaults.go — runtime-stack closure-membership tables and the
// defaultPeerdirsFor / defaultProgramPeerdirsFor derivations.
// Hand-translated subset of upstream ymake's _BUILTIN_PEERDIR /
// _BASE_PROGRAM logic — see each function's docstring for the
// reference-graph anchors that pin the empirical shape.

import (
	"sort"
	"strings"
)

// runtimeAncestorPaths is the platform/runtime closure: modules that
// receive NO implicit default peers because they ARE the runtime stack
// (C runtime, C++ runtime, allocator API, sanitizer shim, util).
// Membership causes `defaultPeerdirsFor` to return empty regardless of
// FlagSet. New entries require zero peer-archive deps in the reference
// graph AND a walker cycle through the module.
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
// wired in as an implicit GLOBAL header peer.
//
// Deliberately narrow — C-only runtime, self-configuring C++ runtime,
// and modules already reaching libcxx via existing user-PEERDIRs are
// excluded so we don't emit spurious or duplicate -I flags.
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
// (libcxx, libcxxrt, libcxxabi, libunwind) that `_BUILTIN_PEERDIR`
// hoists to the FRONT of a consumer's peer-GLOBAL include bundle,
// ahead of musl/arch and user-PEERDIRs. `hoistRuntimeStackAddIncl`
// preserves canonical relative order regardless of aggregation phase.
// Paths are SOURCE_ROOT-relative; `appendAddIncl` prefixes `-I$(S)/`.
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
// not re-emit at a later slot (e.g. ragel6 host PIC walking
// musl/full → linux-headers would otherwise emit a duplicate -I).
//
// Musl flavours bypass this filter: their composer drops
// PeerAddInclGlobal entirely.
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
// `_BUILTIN_PEERDIR`; implicit peers dominate the closure.
//
// Suppression: `NO_PLATFORM` is the umbrella switch; granular flags
// (`NO_LIBC`, `NO_RUNTIME`, `NO_UTIL`) each disable one piece. A module
// setting all three granular flags is effectively platform-less even
// without `NO_PLATFORM` (captured by `effectiveNoPlatform`).
//
// CPP modules implicitly PEERDIR (unless suppressed):
//   - contrib/libs/cxxsupp/libcxx    — NO_RUNTIME / NO_PLATFORM
//   - contrib/libs/cxxsupp/libcxxrt  — NO_RUNTIME / NO_PLATFORM
//   - contrib/libs/libunwind         — NO_RUNTIME / NO_PLATFORM
//   - util                           — NO_UTIL / NO_PLATFORM
//   - contrib/libs/musl/include      — NO_PLATFORM (gated on MUSL=yes)
//
// Cycle prevention: path-equality + prefix matches for musl/libcxx/util
// subtrees. The walker's `walking` stack catches deeper cycles.
// Returns empty for non-CPP languages.
func defaultPeerdirsFor(ctx *genCtx, instance ModuleInstance, flags FlagSet, muslOn bool) []string {
	return defaultPeerdirsForWithState(ctx, instance, flags, muslOn)
}

func defaultPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	return defaultPeerdirsForWithState(ctx, instance, d.flags, d.muslEnabled)
}

func defaultPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, flags FlagSet, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	// Runtime-ancestor modules (libcxx, libcxxrt, libunwind, musl,
	// malloc/api, util, ...) get zero RUNTIME-stack implicit peers
	// AND the musl/include auto-PEERDIR (when MUSL=yes and not
	// no-stdinc itself). Two-phase peer-aggregation ensures musl-arch
	// paths propagate AFTER libcxx/libcxxrt include paths, matching
	// the reference cmd_args ordering.
	noPlatform := effectiveNoPlatform(flags)

	rc := implicitPeerCtx{
		flags:             flags,
		noPlatform:        noPlatform,
		isRuntimeAncestor: isRuntimeAncestor(instance.Path),
		muslOn:            muslOn,
	}

	if rc.isRuntimeAncestor {
		var only []string

		only = appendImplicitPeers(only, unitImplicitPeers, rc)

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
	if !flags.NoRuntime && !noPlatform {
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
	if !flags.NoUtil && !noPlatform {
		if instance.Path != "util" && !strings.HasPrefix(instance.Path, "util/") {
			peers = append(peers, "util")
		}
	}

	peers = appendImplicitPeers(peers, unitImplicitPeers, rc)

	if !flags.NoRuntime && !noPlatform && useArcadiaCompilerRuntime(ctx, instance) && instance.Path != "library/cpp/sanitizer/include" {
		peers = append(peers, "library/cpp/sanitizer/include")
	}

	return peers
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

// defaultPeerCFlags returns the auto-injected peer-CFLAG set for
// ModuleCCInputs.AutoPeerCFlags. Mirrors `_BASE_UNIT`'s
// `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }` (ymake.core.conf:781).
// `-D_musl_` (no `=1`) is consumer-side; musl-self CC nodes get
// `-D_musl_=1` separately, gated off this injection via the
// effective-NO_PLATFORM check.
func defaultPeerCFlags(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	if !d.muslEnabled {
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
// Distinct from `-D_musl_=1` (musl-self only).
const muslConsumerSentinel = "-D_musl_"

// implicitPeerCtx is the read-only view of a module instance that
// implicitPeerRule predicates evaluate against. Engine-agnostic — same
// shape for unit-level, program-level, and allocator-default rule sets.
type implicitPeerCtx struct {
	flags             FlagSet
	noPlatform        bool
	isRuntimeAncestor bool
	muslOn            bool
	muslLite          bool
	osLinux           bool
	archX8664         bool
	hadAllocator      bool
	allocatorName     string
}

// implicitPeerRule maps a predicate to an implicit PEERDIR injection.
// Engine: appendImplicitPeers iterates rules in declaration order and
// appends `peer` whenever `predicate` matches. Self-suppression for
// runtime-stack subtrees lives in the predicate, not in the engine.
type implicitPeerRule struct {
	name      string
	peer      string
	predicate func(implicitPeerCtx) bool
}

func appendImplicitPeers(dst []string, rules []implicitPeerRule, rc implicitPeerCtx) []string {
	for _, r := range rules {
		if r.predicate(rc) {
			dst = append(dst, r.peer)
		}
	}
	return dst
}

// unitImplicitPeers mirrors `_BASE_UNIT`'s `when ($MUSL == "yes") {
// PEERDIR += contrib/libs/musl/include }` at ymake.core.conf:781.
// Other unit-level peers (libcxx/libcxxrt/libunwind, util,
// sanitizer/include) remain inline pending the broader rule lift.
var unitImplicitPeers = []implicitPeerRule{
	{
		name: "musl/include",
		peer: "contrib/libs/musl/include",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn && !rc.noPlatform
		},
	},
}

// programImplicitPeers mirrors `_BASE_PROGRAM`'s musl PEERDIR block at
// ymake.core.conf:1238-1244. Fires in the post-user half
// (`includeMusl=true`) so explicit ALLOCATOR peers land before
// musl/full in the archive walk. MUSL_LITE selects bare musl (used by
// contrib/tools/yasm); the default branch selects musl/full.
var programImplicitPeers = []implicitPeerRule{
	{
		name: "musl/full",
		peer: "contrib/libs/musl/full",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn && !rc.muslLite
		},
	},
	{
		name: "musl",
		peer: "contrib/libs/musl",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn && rc.muslLite
		},
	},
}

// archDependentPeerAddInclPrefixes lists PEER GLOBAL ADDINCL path
// prefixes whose final segment is the consumer's target ISA. These
// paths come from `IF (ARCH_<ISA>) ADDINCL(...)` blocks in upstream
// ya.make and therefore differ between host-axis and target-axis
// walks. Each prefix MUST end in `/`; the matcher appends the literal
// ISA string.
var archDependentPeerAddInclPrefixes = []string{
	"contrib/libs/musl/arch/",
}

// rebasePerArchPeerAddIncl returns a copy of `hostPeerAddIncl` with
// any path matching `<prefix><from>` replaced by `<prefix><to>`, where
// prefix iterates `archDependentPeerAddInclPrefixes`. Used by the JS
// closure scan to re-anchor a host-axis PeerAddInclGlobal slice on
// the target ISA without re-walking the dep tree.
func rebasePerArchPeerAddIncl(hostPeerAddIncl []VFS, from, to ISA) []VFS {
	out := make([]VFS, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		out[i] = p

		for _, prefix := range archDependentPeerAddInclPrefixes {
			if p == Source(prefix+string(from)) {
				out[i] = Source(prefix + string(to))
				break
			}
		}
	}

	return out
}

// programAllocatorDefaults mirrors the upstream
// `DEFAULT_ALLOCATOR=TCMALLOC_TC` branches for `MUSL=yes` and
// non-musl `OS_LINUX=yes && ARCH_X86_64=yes`. Fires in the pre-user
// half (`includeMusl=false`) only when the module declared no
// explicit ALLOCATOR(NAME). Mirrors `allocatorPeers["TCMALLOC_TC"]`
// peer set. GCC, sanitizer, Android, Windows, arch32, and FAKE
// allocator branches remain out of scope here.
var programAllocatorDefaults = []implicitPeerRule{
	{
		name: "tcmalloc (TCMALLOC_TC default)",
		peer: "library/cpp/malloc/tcmalloc",
		predicate: func(rc implicitPeerCtx) bool {
			return !rc.hadAllocator && rc.osLinux && (rc.muslOn || rc.archX8664)
		},
	},
	{
		name: "tcmalloc/no_percpu_cache (TCMALLOC_TC default)",
		peer: "contrib/libs/tcmalloc/no_percpu_cache",
		predicate: func(rc implicitPeerCtx) bool {
			return !rc.hadAllocator && rc.osLinux && (rc.muslOn || rc.archX8664)
		},
	},
}

// defaultProgramPeerdirsFor returns the implicit DEFAULT_PEERDIRs that
// upstream `_BASE_PROGRAM` attaches to the validated Linux PROGRAM
// branches we model here: the MUSL path plus the non-musl x86_64
// `TCMALLOC_TC` default. GCC, sanitizer, Android, Windows, arch32,
// and other allocator-default branches remain out of scope.
//
// `includeMusl` splits the output: false → pre-user (cow/on + tcmalloc
// default); true → post-user (musl + cpuid_check). genModule calls
// twice to interleave allocator explicit peers and d.peerdirs between
// halves so explicit ALLOCATOR peers land before musl/full in the
// archive walk.
func defaultProgramPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData, includeMusl bool) []string {
	return defaultProgramPeerdirsForWithState(ctx, instance, d.flags, d.hadAllocator, d.allocatorName, d.muslLite, includeMusl, d.muslEnabled)
}

func defaultProgramPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, flags FlagSet, hadAllocator bool, allocatorName string, muslLiteOverride bool, includeMusl bool, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	env := buildIfEnv(instance)
	muslLite := env.Bool("MUSL_LITE") || muslLiteOverride

	rc := implicitPeerCtx{
		flags:         flags,
		muslOn:        muslOn,
		muslLite:      muslLite,
		osLinux:       env.Bool("OS_LINUX"),
		archX8664:     env.Bool("ARCH_X86_64"),
		hadAllocator:  hadAllocator,
		allocatorName: allocatorName,
	}

	var peers []string

	if !includeMusl {
		// USE_COW=yes default: every PROGRAM gets `build/cow/on`.
		// Mirrors `_BASE_PROGRAM` `when ($USE_COW == "yes")` at
		// ymake.core.conf:946-948. Declared BEFORE the allocator block
		// (conf:946 precedes allocator select at conf:959) so post-order
		// DFS places build/cow/on before tcmalloc.
		peers = append(peers, "build/cow/on")

		// Default ALLOCATOR=TCMALLOC_TC for MUSL=yes or non-musl
		// x86_64 Linux. PROGRAMs declaring ALLOCATOR(NAME) go through
		// allocatorPeers; this default fires only when neither was
		// declared.
		peers = appendImplicitPeers(peers, programAllocatorDefaults, rc)
	} else {
		// musl block declared AFTER allocator in upstream conf
		// (ymake.core.conf:1238-1244 vs allocator select :959-1036).
		// Post-order DFS places musl after tcmalloc, matching REF
		// slots 47-48 (musl, musl/full) vs 41-46 (cow + tcmalloc).
		// In the post-user half so explicit ALLOCATOR peers land
		// before musl/full in the archive walk.
		peers = appendImplicitPeers(peers, programImplicitPeers, rc)

		// `_BASE_PROGRAM` at ymake.core.conf:1247-1254 declares
		// `DEFAULT(CPU_CHECK yes)` gated off by
		// `USE_SSE4 != yes || NOUTIL == yes || ALLOCATOR == FAKE`.
		// USE_SSE4 defaults yes only when ARCH_X86_64 || ARCH_I386
		// (conf:3057-3132); the predicate collapses to
		// (ARCH_X86_64 && !NoUtil && ALLOCATOR != "FAKE") in our env.
		// Declared after musl/full to mirror conf order.
		if env.Bool("ARCH_X86_64") && !flags.NoUtil && allocatorName != "FAKE" {
			peers = append(peers, "library/cpp/cpuid_check")
		}
	}

	return peers
}

// effectiveNoPlatform reports true when the FlagSet combination behaves
// as `NO_PLATFORM` in upstream ymake — NoLibc + NoUtil + NoRuntime all
// set. `build/cow/on` exhibits this pattern via its NO_LIBC + NO_UTIL +
// NO_RUNTIME ya.make declarations parsed by collectModule.
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
func peerYaMakeExists(fs *FS, peerPath string) bool {
	return fs.IsFile(peerPath + "/ya.make")
}
