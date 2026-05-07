package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver. PR-25 wires the macro evaluator + per-instance flag
// derivation + dispatch by source extension + host-tool recursion
// into the walker that PR-23 retrofitted onto `ModuleInstance`. The
// shape stays depth-first, post-order, declaration-order over PEERDIR
// (R14 link order) — what changed is the per-module work performed
// once a ya.make has been parsed.
//
// Macro vocabulary the walker now understands:
//
//   - `IF (cond) ... [ELSE ...] ENDIF` — evaluated via macros.go's
//     EvalCond against a per-instance env (target/host platform +
//     ARCH_AARCH64 / ARCH_X86_64 / MUSL flags etc.). The taken branch
//     is inlined; unreached branches contribute nothing.
//   - `NO_LIBC`, `NO_UTIL`, `NO_RUNTIME`, `NO_PLATFORM`,
//     `NO_COMPILER_WARNINGS` — set the corresponding boolean on the
//     instance's FlagSet. Macro-derived flags take precedence over
//     `inferFlagsFromPath`'s heuristic.
//   - `ADDINCL([GLOBAL] paths...)`, `CFLAGS([GLOBAL] flags...)`,
//     `CXXFLAGS([GLOBAL] flags...)`, `CONLYFLAGS(flags...)`,
//     `LDFLAGS(flags...)`, `SRCDIR(dir)` — collected per-module.
//     PR-29-D02/D03 thread the per-module non-GLOBAL ADDINCL,
//     CXXFLAGS, and CONLYFLAGS into EmitCC via ModuleCCInputs.
//     Peer-propagated GLOBAL ADDINCL/CXXFLAGS routing is deferred to
//     PR-30 (D04 of the PR-29 plan).
//   - `JOIN_SRCS(name srcs...)` — emits a JS node + a CC node that
//     compiles the joined output. The CC node's own `.cpp` source is
//     the JS output relative to the module path.
//   - `GLOBAL_SRCS(srcs...)` — collected as separate sources; their
//     CC outputs flow into a SECOND AR node (`<lib>.global.a`) with
//     `module_tag=global` per `EmitARGlobal`.
//   - `INCLUDE(path)` — already inlined by the parser at parse time;
//     the walker never sees an IncludeStmt.
//
// Source dispatch by extension:
//
//   - `.c` / `.cpp` / `.cc` / `.cxx` → EmitCC.
//   - `.h` / `.hpp` — silently skipped (headers in SRCS are rule-
//     metadata only, not compiled).
//   - `.S` / `.s` → EmitAS. Caller may need a host yasm LD ref; PR-25
//     plumbs it conditionally (only when `instance.Flags.PIC` and
//     the module's path matches the asmlib heuristic).
//   - `.rl6` → EmitR6 (host ragel6 LD via `WithHost` recursion into
//     `contrib/tools/ragel6`), then EmitCC of the generated `.cpp`.
//
// Cross-platform recursion (D31):
//
// When a `.rl6` source is processed, the walker constructs the host
// ragel6 instance (`instance.WithHost(ctx.cfg)` with Path overridden
// to `contrib/tools/ragel6`) and recurses through `genModule`. The
// resulting host LD NodeRef threads into EmitR6's `ragel6LD`
// parameter. Same shape applies for yasm when an `.S` source needs
// it. If the host tool's ya.make does not parse cleanly (the
// upstream uses `IF (USE_PREBUILT_TOOLS) ... INCLUDE(...)` blocks the
// PR-25 evaluator does not bind), the recursion throws — that is the
// expected PR-26 escalation point and is documented in the PR-25
// Completed entry.
//
// PR-25 acceptance scope: the walker mechanism itself. Synthetic
// tests in `gen_test.go` exercise IF / JOIN_SRCS / GLOBAL_SRCS /
// `.rl6` host recursion. The full `tools/archiver` PEERDIR closure
// is PR-26's job — PR-25 only ensures `Gen(...)` against
// `tools/archiver` does NOT panic at the walker's call site (it may
// throw a parse error from a deep peer it cannot evaluate yet, which
// is the documented partial-coverage point).

// moduleEmitResult is the per-instance "what did we emit?" record
// kept by `genCtx.memo`. PR-24 distinguished ARRef/LDRef:
//
//   - LIBRARY modules populate ARRef (the .a archive); LDRef/LDPath
//     alias to ARRef/ARPath so PROGRAM modules peering this LIBRARY
//     can wire it as a peer-archive input through the AR fields.
//   - PROGRAM modules populate LDRef (the linked binary); ARRef/ARPath
//     alias to LDRef/LDPath defensively but in practice no LIBRARY
//     peers a PROGRAM, so the ARRef of a PROGRAM is never read.
//
// `isPROGRAM` records the module-shape so the caller (`Gen`) knows
// whether to mark `LDRef` or `ARRef` as the graph result.
//
// PR-27: `headerOnly` distinguishes header-only LIBRARY modules
// (e.g. `library/cpp/sanitizer/include`) that have no compilable
// sources and emit nothing. Such modules are walked (so their
// transitive PEERDIRs are still discovered) but contribute no
// AR/LD/Global node — callers that peer them must skip the
// archive-dep wiring rather than trip on a zero-valued NodeRef.
type moduleEmitResult struct {
	ARRef      NodeRef
	ARPath     string
	isPROGRAM  bool
	headerOnly bool
	LDRef      NodeRef
	LDPath     string
	GlobalRef  *NodeRef // non-nil when the module has GLOBAL_SRCS (EmitARGlobal was called)
	GlobalPath string   // BUILD_ROOT-relative path to the .global.a archive; empty when GlobalRef is nil
	// AddInclGlobal is the union of this module's own GLOBAL ADDINCL
	// paths PLUS the transitive peer-GLOBAL ADDINCL contributions
	// from every PEERDIR (PR-31 D05). Consumers use this set for both
	// (a) cmd_args -I emission (peer-propagated -I flags slotted
	// after the module's own ADDINCL) and (b) the include scanner's
	// resolution search path. SOURCE_ROOT-relative paths.
	AddInclGlobal []string
}

// genCtx threads state through the recursive walk. `emit`
// accumulates every node emitted in the closure; `memo`
// deduplicates per-instance emission; `walking` is the
// cycle-detection stack. PR-23 keys both maps on `ModuleInstance`
// (D34); PR-12 keyed them on the bare path string.
//
// `cyclesTolerated` counts back-edges suppressed by the headerOnly
// stub path (D02). Accessible to tests that need to assert a known
// cycle was hit exactly once.
//
// Host-tool walks fire eagerly from inside `emitOneSource`: a `.rl6`
// source recurses into ragel6/bin, an `.S`/`.s` source in a yasm-using
// host module recurses into yasm. The recursion happens at the trigger
// site so the resulting host LD's NodeRef and output path are
// available to wire into the per-source emitter (R6's cmd_args[0],
// AS's foreign_deps.tool). `genModule`'s memo prevents re-walking the
// same host instance twice. No post-walk drainer is needed — every
// host PROGRAM the target closure depends on is reached through one
// of the two source-extension dispatch sites.
type genCtx struct {
	cfg             PlatformConfig
	sourceRoot      string
	emit            Emitter
	memo            map[ModuleInstance]*moduleEmitResult
	walking         map[ModuleInstance]bool
	cyclesTolerated int
	// scannerTarget is the include-resolver for TARGET (aarch64) CC
	// nodes; scannerHost is the host (x86_64) variant. Each scanner
	// has its own parsed-includes cache (the OS page cache amortises
	// rereads). Each also has its own SysInclSet because
	// linux-musl-<arch>.yml mappings differ between platforms (e.g.
	// bits/alltypes.h resolves arch-specifically).
	scannerTarget *IncludeScanner
	scannerHost   *IncludeScanner
}

// asmlibYasmModules lists module paths whose host `.S`/`.s` sources
// invoke yasm via `foreign_deps.tool`. Per F2/F4 of the PR-28 plan, the
// reference graph wires yasm into the 25 host-asmlib AS nodes; no other
// host AS source reaches yasm (`cxxsupp/builtins/chkstk_aarch64.S` and
// libcxx/libcxxabi shims use clang's built-in assembler with no
// foreign_deps). Future host modules that adopt yasm get appended here.
var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

// whitelistedMetadataMacros is the whitelist of UnknownStmt names
// that the walker treats as no-ops (metadata only — they do not
// participate in node emission). The "real" effects (NO_LIBC etc.)
// are handled directly in `collectModule` and override the
// inferred-from-path FlagSet bag. Names that remain pure metadata
// (LICENSE, VERSION, ALLOCATOR_IMPL, ...) stay as no-ops.
// Whitelisted metadata macros (NO_BUILD effect, parser-permissive).
// Owners: PR-25 extended; new entries OK if confirmed metadata-only.
var whitelistedMetadataMacros = map[string]struct{}{
	"NO_UTIL":               {},
	"NO_LIBC":               {},
	"NO_RUNTIME":            {},
	"NO_PLATFORM":           {},
	"NO_LTO":                {},
	"NO_COMPILER_WARNINGS":  {},
	"LICENSE":               {},
	"LICENSE_TEXTS":         {},
	"WITHOUT_LICENSE_TEXTS": {},
	"VERSION":               {},
	"ORIGINAL_SOURCE":       {},
	"RECURSE":               {},
	"RECURSE_FOR_TESTS":     {},
	"ALLOCATOR_IMPL":        {},
	"NEED_CHECK":            {},
	"IDE_FOLDER":            {},
	"EXTRALIBS":             {},
	"HEADERS":               {},
	"DISABLE":               {}, // PR-30: ENABLE handled explicitly to track MUSL_LITE per module; DISABLE has no per-module side effect today.
	"NO_BUILD_IF":           {},
	"NO_SANITIZE":           {},
	"NO_SANITIZE_COVERAGE":  {},
	"SRC_C_NO_LTO":          {},
	"DEFAULT":               {},
	"PROVIDES":              {},
	"USE_CXX":               {},
	"DEFINE_VARIABLE":       {},
	"PYTHON3":               {},
	"BUILD_ONLY_IF":         {}, // PR-27: contrib/libs/cxxsupp/libcxxrt
	"MESSAGE":               {}, // PR-27: contrib/libs/cxxsupp/libcxx (FATAL_ERROR in dead branch)
	"SRC":                   {}, // PR-27: util/charset (per-source compile flag; treated as metadata until R3 lands)
	"SRC_C_SSE41":           {}, // PR-27: util/charset (arch-specific compile-flag wrapper)
	"NO_CLANG_COVERAGE":     {}, // PR-30: contrib/tools/yasm
	"NO_PROFILE_RUNTIME":    {}, // PR-30: contrib/tools/yasm
}

// Gen produces the build graph rooted at `targetDir`. PR-23 wraps
// the call into the new ModuleInstance addressing model: the seed
// instance is constructed from `cfg.Target`, language=cpp,
// flags=inferFlagsFromPath(targetDir, false). The walker
// (`genModule`) takes the ModuleInstance directly so future host-
// tool recursion (PR-25) can fork the walker into a host instance
// without changing this entry point.
//
// PR-28 model: host PROGRAM walks fire eagerly from the source-dispatch
// sites in `emitOneSource`. When `genModule`'s per-source loop hits
// `.rl6` (R6 generator) or a yasm-using `.S`/`.s`, it constructs the
// host ModuleInstance via `WithHost(cfg)` and calls `genModule`
// recursively right there — no separate post-walk drainer. The host
// walk may itself trigger further host walks (ragel6/bin → musl/full →
// asmlib's host AS → yasm), all reached through the same eager-recursion
// rule. `genCtx.memo` deduplicates so each host PROGRAM is walked at
// most once.
//
// Host LDs are emitted into the same Graph as the target walk but are
// NOT added to the result roots (per F3 of the PR-28 plan: reference
// graph's `result` is target-only).
func Gen(cfg PlatformConfig, sourceRoot string, targetDir string) *Graph {
	ctx := &genCtx{
		cfg:           cfg,
		sourceRoot:    sourceRoot,
		emit:          NewBufferedEmitter(),
		memo:          make(map[ModuleInstance]*moduleEmitResult),
		walking:       make(map[ModuleInstance]bool),
		scannerTarget: NewIncludeScanner(sourceRoot, LoadSysInclSetFor(sourceRoot, "aarch64")),
		scannerHost:   NewIncludeScanner(sourceRoot, LoadSysInclSetFor(sourceRoot, "x86_64")),
	}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Language: LangCPP,
		Target:   cfg.Target.ID,
		Flags:    inferFlagsFromPath(filepath.Clean(targetDir), false),
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	return Finalize(ctx.emit.(*BufferedEmitter))
}

// moduleData is the per-module accumulator populated by
// `collectModule`. It captures everything the rule-emission stage
// needs after macro evaluation has flattened IF branches and
// inlined macros. The `flags` field starts from the path-based
// heuristic and is overlaid with macro-derived bools (NO_LIBC etc.).
type moduleData struct {
	moduleStmt    *ModuleStmt
	srcs          []string
	globalSrcs    []string
	peerdirs      []string
	joinSrcs      []*JoinSrcsStmt
	addIncl       []string // collected non-GLOBAL ADDINCL paths
	addInclGlobal []string // PR-31 D04: collected ADDINCL(GLOBAL ...) paths; peer-propagated to consumers
	cFlags        []string // collected CFLAGS values, all variants
	cxxFlags      []string // collected CXXFLAGS values (C++ only); PR-29-D02 threads into ModuleCCInputs.CXXFlags
	cOnlyFlags    []string // collected CONLYFLAGS values (C only); PR-29-D02 threads into ModuleCCInputs.COnlyFlags
	ldFlags       []string // collected LDFLAGS values
	srcDir        string   // last SRCDIR setting (empty = module dir)
	flags         FlagSet  // overlay of inferFlagsFromPath + macro bools
	hadAllocator  bool     // PR-30 D03: set by applyAllocatorStmt; PROGRAM-default-allocator routing fires only when this is false
	muslLite      bool     // PR-30 D02: set by ENABLE(MUSL_LITE); flips the default-program-peers musl/full → musl gate
	conflictMod   *ModuleStmt
}

// collectModule walks `mf.Stmts` (after IF branches have been
// resolved against `env`) and returns a `moduleData` with all
// macros classified. IfStmts are recursively inlined; nested
// JOIN_SRCS / SRCS / PEERDIR / NO_*  inside an IF taken branch are
// processed as if they were top-level. INCLUDE never reaches this
// point (the parser already inlined includes).
//
// The `pathFlags` argument is the path-based heuristic seed; macro
// overlays mutate it in place on the returned moduleData so the
// caller does not need to compose two separate bags.
func collectModule(modulePath string, stmts []Stmt, env Environment, pathFlags FlagSet) *moduleData {
	d := &moduleData{flags: pathFlags}

	collectStmts(modulePath, stmts, env, d)

	return d
}

// collectStmts is the shared walker collectModule and IfStmt-branch
// expansion both use. It mutates `d` in place.
func collectStmts(modulePath string, stmts []Stmt, env Environment, d *moduleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			d.moduleStmt = v
		case *SrcsStmt:
			d.srcs = append(d.srcs, v.Sources...)
		case *PeerdirStmt:
			d.peerdirs = append(d.peerdirs, v.Paths...)
		case *SetStmt:
			// SET is parsed but PR-25 has no evaluator. The taken
			// IF branches above already flattened any conditional
			// SET; an unconditional SET that influences downstream
			// IFs would need a real macro evaluator (PR-26+).
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			d.joinSrcs = append(d.joinSrcs, v)
		case *AddInclStmt:
			// PR-31 D04/D13: route per-path GLOBAL ADDINCL into a
			// separate slot (peer-propagated to consumers via PEERDIR
			// walk); non-GLOBAL paths go into the per-module own-ADDINCL
			// slot that EmitCC's appendAddIncl emits as -I args.
			// D13 fix: GlobalPaths and OwnPaths are split by
			// splitAddInclPaths so a single ADDINCL call can carry both
			// GLOBAL and module-own paths (e.g. libcxx: GLOBAL include +
			// bare src — only include propagates to consumers).
			d.addInclGlobal = append(d.addInclGlobal, v.GlobalPaths...)
			d.addIncl = append(d.addIncl, v.OwnPaths...)
		case *CFlagsStmt:
			d.cFlags = append(d.cFlags, v.Flags...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, v.Flags...)
		case *SrcDirStmt:
			// SRCDIR shifts source resolution base. PR-28-D02 threads d.srcDir
			// into emitOneSource so per-source CC/AS/R6 nodes rebase to <srcDir>;
			// JOIN_SRCS / EmitJS gap was closed by PR-28-D11. LD/AR remain at
			// instance.Path (semantic difference: the binary/archive lives where
			// declared, even if its sources are elsewhere).
			d.srcDir = v.Dir
		case *GlobalSrcsStmt:
			d.globalSrcs = append(d.globalSrcs, v.Sources...)
		case *IfStmt:
			taken := v.Then

			if !EvalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(modulePath, taken, env, d)
		case *UnknownStmt:
			applyUnknownStmt(v, d)
		default:
			ThrowFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

// applyUnknownStmt routes an UnknownStmt by name. The five flag-
// flipping macros (NO_LIBC / NO_UTIL / NO_RUNTIME / NO_PLATFORM /
// NO_COMPILER_WARNINGS) override the inferFlagsFromPath heuristic.
// `ALLOCATOR(NAME)` is resolved to an implicit PEERDIR addition per
// `build/ymake.core.conf:961-1035` (PR-28 / D12). Anything else must
// be in the metadata whitelist; an unknown name throws so a new
// ya.make macro surfaces immediately rather than being silently
// dropped (D27 discipline extended to UnknownStmts).
func applyUnknownStmt(v *UnknownStmt, d *moduleData) {
	switch v.Name {
	case "NO_LIBC":
		d.flags.NoLibc = true
	case "NO_UTIL":
		d.flags.NoUtil = true
	case "NO_RUNTIME":
		d.flags.NoRuntime = true
	case "NO_PLATFORM":
		d.flags.NoPlatform = true
	case "NO_COMPILER_WARNINGS":
		d.flags.NoCompilerWarnings = true
	case "CXXFLAGS":
		mod, rest := splitGlobalModifier(v.Args)

		if mod == "GLOBAL" {
			// GLOBAL CXXFLAGS propagate to peers per ymake semantics; PR-30 D04 will route via cxxFlagsGlobal field. For now, dropped — applying to self would be semantically wrong.
			break
		}

		d.cxxFlags = append(d.cxxFlags, rest...)
	case "CONLYFLAGS":
		mod, rest := splitGlobalModifier(v.Args)

		if mod == "GLOBAL" {
			// GLOBAL CONLYFLAGS propagate to peers per ymake semantics; PR-30 D04 will route via cOnlyFlagsGlobal field. For now, dropped — applying to self would be semantically wrong.
			break
		}

		d.cOnlyFlags = append(d.cOnlyFlags, rest...)
	case "ALLOCATOR":
		applyAllocatorStmt(v, d)
	case "ENABLE":
		// PR-30 D02: track ENABLE(MUSL_LITE) so the
		// defaultProgramPeerdirsFor decision sees the per-module
		// flip. yasm declares ENABLE(MUSL_LITE) inside its IF(MUSL)
		// branch; without this hook yasm pulls musl/full and the
		// resulting cross-PROGRAM cycle (yasm → musl/full →
		// asmlib's .asm sources → yasm) blows the cycle counter.
		// All other ENABLE(...) names stay metadata-only.
		for _, a := range v.Args {
			if a == "MUSL_LITE" {
				d.muslLite = true
			}
		}
	default:
		if _, ok := whitelistedMetadataMacros[v.Name]; !ok {
			ThrowFmt("gen: PR-25 does not yet support macro %q (extend whitelistedMetadataMacros or add a typed Stmt)", v.Name)
		}
	}
}

// allocatorPeers maps `ALLOCATOR(<name>)` arguments to the implicit
// PEERDIR additions per `build/ymake.core.conf:961-1035`. Each name
// resolves to zero or more peer paths appended to the module's
// PEERDIR list. PR-28 ships the M2-relevant subset; entries with
// resolved == nil intentionally add no peer (FAKE /
// allocator-already-handled-elsewhere).
//
// ALLOCATOR(SYSTEM) unconditionally adds library/cpp/malloc/system per
// build/ymake.core.conf:1038-1040 (`when ($ALLOCATOR == "SYSTEM")`).
// The MUSL gate at lines 954-958 applies to the select($ALLOCATOR)
// block, NOT to this when-clause.
var allocatorPeers = map[string][]string{
	"MIM":                       {"library/cpp/malloc/mimalloc"},
	"MIM_SDC":                   {"library/cpp/malloc/mimalloc_sdc"},
	"HU":                        {"library/cpp/malloc/hu"},
	"PROFILED_HU":               {"library/cpp/malloc/profiled_hu"},
	"THREAD_PROFILED_HU":        {"library/cpp/malloc/thread_profiled_hu"},
	"TCMALLOC_256K":             {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc"},
	"TCMALLOC_SMALL_BUT_SLOW":   {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/small_but_slow"},
	"TCMALLOC_NUMA_256K":        {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/numa_256k"},
	"TCMALLOC_NUMA_LARGE_PAGES": {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/numa_large_pages"},
	"TCMALLOC":                  {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/default"},
	"TCMALLOC_TC":               {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/no_percpu_cache"},
	"GOOGLE":                    {"library/cpp/malloc/galloc"},
	"J":                         {"library/cpp/malloc/jemalloc"},
	"LF":                        {"library/cpp/lfalloc"},
	"LF_YT":                     {"library/cpp/lfalloc/yt"},
	"LF_DBG":                    {"library/cpp/lfalloc/dbg"},
	"B":                         {"library/cpp/balloc"},
	"BM":                        {"library/cpp/balloc_market"},
	"C":                         {"library/cpp/malloc/calloc"},
	"LOCKLESS":                  {"library/cpp/malloc/lockless"},
	"YT":                        {"library/cpp/ytalloc/impl"},
	// FAKE / DEFAULT add no peer; SYSTEM unconditionally peers
	// library/cpp/malloc/system per ymake.core.conf:1038-1040.
	"FAKE":    nil,
	"SYSTEM":  {"library/cpp/malloc/system"},
	"DEFAULT": nil,
}

// applyAllocatorStmt resolves `ALLOCATOR(<name>)` to a PEERDIR
// addition per `build/ymake.core.conf:961-1035`. The macro takes
// exactly one argument; multi-arg or unknown allocator names throw
// loudly per D27 discipline.
func applyAllocatorStmt(v *UnknownStmt, d *moduleData) {
	if len(v.Args) != 1 {
		ThrowFmt("gen: ALLOCATOR expects exactly 1 argument, got %d (line %d)", len(v.Args), v.Line)
	}

	name := v.Args[0]

	peers, ok := allocatorPeers[name]
	if !ok {
		ThrowFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name, v.Line)
	}

	d.peerdirs = append(d.peerdirs, peers...)
	d.hadAllocator = true
}

// buildIfEnv constructs the per-instance bound-variable environment
// for IF predicates. The base set is `DefaultIfEnv` (M2 default =
// aarch64 / linux / clang / musl). For host instances (Flags.PIC),
// flip ARCH_AARCH64↔ARCH_X86_64 so the same ya.make produces the
// other architecture's branches. The result is a fresh Environment;
// the caller is free to mutate it.
func buildIfEnv(instance ModuleInstance) Environment {
	env := DefaultIfEnv.Clone()

	if instance.Target == PlatformDefaultLinuxX8664 {
		env.SetBool("ARCH_AARCH64", false)
		env.SetBool("ARCH_X86_64", true)
	}

	if instance.Target == PlatformDefaultLinuxAArch64 {
		env.SetBool("ARCH_AARCH64", true)
		env.SetBool("ARCH_X86_64", false)
	}

	return env
}

// derivePeerInstance constructs the peer module's ModuleInstance.
// The peer inherits the parent's Language and Target and the PIC
// axis (host-tool peers stay on host); its FlagSet is seeded from
// `inferFlagsFromPath(peerPath, parent.PIC)` and macro-overlaid by
// `genModule` itself (so the peer's flag bag reflects its own
// ya.make's NO_LIBC / NO_UTIL declarations). Macro overlay happens
// inside `genModule` because that is where the peer's ya.make is
// parsed; this helper only builds the cycle-detection key.
func derivePeerInstance(parent ModuleInstance, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Language: parent.Language,
		Target:   parent.Target,
		Flags:    inferFlagsFromPath(peerPath, parent.Flags.PIC),
	}
}

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
// ancestor (see runtimeAncestorPaths) or sits inside a runtime
// ancestor's subtree (e.g. `contrib/libs/musl/full`, `util/charset`).
func isRuntimeAncestor(path string) bool {
	if runtimeAncestorPaths[path] {
		return true
	}

	for prefix := range runtimeAncestorPaths {
		if strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
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

	// PR-27: runtime ancestor modules (libcxx, libcxxrt, libunwind,
	// musl, malloc/api, util, ...) get zero implicit peers — matches
	// upstream ymake's `_BUILTIN_PEERDIR` exclusion list and breaks
	// the otherwise-unavoidable mutual cycle between malloc/api and
	// libcxxrt (each had the other as a default).
	//
	// INVARIANT: every path enumerated in the per-platform branches below
	// MUST appear in runtimeAncestorPaths so that the early-exit above
	// catches the self-cycle case before we recurse. The per-path
	// instance.Path != "..." guards on lines ~484-520 are redundant
	// defense-in-depth — kept to make the suppression intent visible at
	// the call site, not because they are reachable when runtimeAncestorPaths
	// is complete.
	if isRuntimeAncestor(instance.Path) {
		return nil
	}

	noPlatform := effectiveNoPlatform(instance.Flags)

	var peers []string

	if !instance.Flags.NoLibc && !noPlatform {
		// Don't peer musl from any contrib/libs/musl[/...] module —
		// musl provides itself, and the PR-13 path heuristic already
		// sets NoLibc for those, so this is belt-and-braces against
		// a future heuristic regression.
		if instance.Path != "contrib/libs/musl" && !strings.HasPrefix(instance.Path, "contrib/libs/musl/") {
			peers = append(peers, "contrib/libs/musl")
		}
	}

	if !instance.Flags.NoRuntime && !noPlatform {
		if instance.Path != "contrib/libs/cxxsupp/builtins" {
			peers = append(peers, "contrib/libs/cxxsupp/builtins")
		}
	}

	if !noPlatform {
		if instance.Path != "library/cpp/malloc/api" {
			peers = append(peers, "library/cpp/malloc/api")
		}
	}

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

	// util is a target-only implicit peer per the reference graph (zero
	// host nodes under util/). Suppressing here keeps the host walk from
	// pulling util in. The proper upstream rule lives in
	// build/ymake.core.conf's _BUILTIN_PEERDIR (USE_CXX/NO_UTIL gating);
	// the target-axis check approximates it for M2.
	targetPlatformID := DefaultLinuxConfig.Target.ID

	if ctx != nil {
		targetPlatformID = ctx.cfg.Target.ID
	}

	if !instance.Flags.NoUtil && !noPlatform && instance.Target == targetPlatformID {
		if instance.Path != "util" && !strings.HasPrefix(instance.Path, "util/") {
			peers = append(peers, "util")
		}
	}

	return peers
}

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
func defaultProgramPeerdirsFor(instance ModuleInstance, hadAllocator bool, muslLiteOverride bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	env := buildIfEnv(instance)
	muslOn := env.Bool("MUSL")
	muslLite := env.Bool("MUSL_LITE") || muslLiteOverride
	osLinux := env.Bool("OS_LINUX")

	var peers []string

	if muslOn && !muslLite {
		// Caller (defaultPeerdirsFor in gen.go:932) gates on !isRuntimeAncestor(instance.Path)
		// which already excludes contrib/libs/musl/* (incl. musl/full). No self-suppression needed here.
		const muslFullPath = "contrib/libs/musl/full"
		peers = append(peers, muslFullPath)
	}

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

// genModule emits the subgraph for `instance` and returns its
// `*moduleEmitResult`. Memoised: a second call for the same
// instance returns the cached result without re-emitting.
//
// Algorithm (PR-25):
//
//  1. Memo hit → return.
//  2. Cycle check: if `instance` is already on the walking stack,
//     throw.
//  3. Parse `<sourceRoot>/<instance.Path>/ya.make`.
//  4. Resolve IF branches and collect SRCS / PEERDIR / JOIN_SRCS /
//     GLOBAL_SRCS / NO_*  / ADDINCL / CFLAGS / SRCDIR via
//     `collectModule`. Apply macro-derived NO_*  flags as overrides
//     on the path-based seed FlagSet.
//  5. Validate: exactly one module, non-empty effective sources.
//  6. Recurse into each PEERDIR in declaration order (post-order —
//     peers emitted before parent) using the macro-aware
//     `genModule`.
//  7. For each source dispatch by extension to EmitCC / EmitAS /
//     EmitR6 (which itself recurses into the host ragel6 instance);
//     headers (`.h`/`.hpp`) are skipped silently.
//  8. For each JOIN_SRCS, EmitJS then EmitCC against the joined
//     output's module-relative path.
//  9. For LIBRARY: EmitAR over own CC outputs (regular srcs +
//     joined srcs); plus EmitARGlobal if GLOBAL_SRCS was non-empty.
//     For PROGRAM: EmitLD over own CC outputs and peer archives.
//
// 10. Memoise and return.
func genModule(ctx *genCtx, instance ModuleInstance) *moduleEmitResult {
	if existing, ok := ctx.memo[instance]; ok {
		return existing
	}

	// PR-27: a back-edge during the walk is no longer a hard error —
	// the implicit DEFAULT_PEERDIR set creates legitimate mutual
	// references between runtime-stack modules (libcxx ↔ libcxxrt,
	// libunwind ↔ libcxxrt via sanitizer/include's ancestor chain,
	// etc.) that the upstream reference handles by exclusion lists
	// we have not yet modelled. Returning a `headerOnly`-shaped
	// stub for the back-edge peer is sufficient: the peer's own
	// walk completes elsewhere on the stack, and the consumer
	// correctly skips an empty archive-ref instead of pinning a
	// zero-valued NodeRef into its AR/LD. The reference graph
	// emits no peer-archive deps in AR anyway (every LIBRARY's AR
	// has only its own .o files), so the loss-of-information here
	// is below the comparator's L1 surface.
	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path)

		return &moduleEmitResult{headerOnly: true}
	}

	ctx.walking[instance] = true
	defer delete(ctx.walking, instance)

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path, "ya.make")
	mf := Throw2(ParseFile(yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(instance.Path, mf.Stmts, env, instance.Flags)

	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

	if d.moduleStmt.Name != "LIBRARY" && d.moduleStmt.Name != "PROGRAM" {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path, d.moduleStmt.Name)
	}

	// Update the instance's flags from macro overlay so downstream
	// emitters see the post-macro view. The instance is value-typed
	// so this rebinds locally without affecting the caller.
	instance.Flags = d.flags

	// PR-27: a header-only LIBRARY (e.g. library/cpp/sanitizer/include)
	// has no compilable sources but still has a valid module
	// declaration; the upstream reference does not emit any AR for
	// these. Walk the peers so their transitive closure is
	// discovered, then return a `headerOnly: true` result that
	// callers handle by skipping the archive-dep wiring. PROGRAMs
	// with zero compilable sources remain a hard error.
	if !hasCompilableSource(d) {
		if d.moduleStmt.Name == "PROGRAM" {
			ThrowFmt("gen: %s has no compilable sources (after IF/header filter)", instance.Path)
		}

		// Header-only LIBRARYs may declare ADDINCL(GLOBAL ...) that
		// peer-propagates without emitting an AR. Walk peers (so
		// transitive sanitizer/include peerdirs reach genModule) and
		// aggregate own + peer GLOBAL ADDINCL so consumers see the
		// closure. PR-31 D05.
		peerGlobal := walkPeersForGlobalAddIncl(ctx, instance, d)

		seen := map[string]struct{}{}
		eff := make([]string, 0, len(d.addInclGlobal)+len(peerGlobal))

		for _, p := range d.addInclGlobal {
			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			eff = append(eff, p)
		}

		for _, p := range peerGlobal {
			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			eff = append(eff, p)
		}

		result := &moduleEmitResult{headerOnly: true, AddInclGlobal: eff}
		ctx.memo[instance] = result

		return result
	}

	// Recurse into peers. Implicit DEFAULT_PEERDIRs (PR-26) are
	// prepended to the explicit `PEERDIR(...)` list before the walk
	// so a module's transitive closure includes the runtime / libc /
	// allocator scaffolding ymake adds via `_BUILTIN_PEERDIR`. The
	// declaration-order R14 invariant for the explicit set is kept
	// — defaults sort first, then explicit in source order.
	//
	// Defaults are tolerant of a missing ya.make: synthetic test
	// fixtures populate only the modules they care about, and a
	// helper-supplied default (musl / builtins / malloc/api) will
	// not exist in those trees. A missing EXPLICIT peer is still a
	// hard error — the test author declared it, so its absence is a
	// fixture bug, not an "implicit ymake plumbing" no-op.
	defaults := defaultPeerdirsFor(ctx, instance)

	// PR-30 D02 + D03: PROGRAM-only implicit peerdirs. `_BASE_PROGRAM`
	// adds musl/full (when MUSL=yes && !MUSL_LITE) and the default
	// ALLOCATOR's peer set (TCMALLOC_TC for our environment) on top of
	// the language defaults. Threaded only for PROGRAM modules; the
	// `hadAllocator` flag suppresses the allocator-default when the
	// PROGRAM declared `ALLOCATOR(NAME)` itself.
	if d.moduleStmt.Name == "PROGRAM" && !isRuntimeAncestor(instance.Path) {
		programDefaults := defaultProgramPeerdirsFor(instance, d.hadAllocator, d.muslLite)
		defaults = append(defaults, programDefaults...)
	}

	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))
	allPeers := make([]string, 0, len(defaults)+len(d.peerdirs))

	for _, p := range defaults {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
	}

	for _, p := range d.peerdirs {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
	}

	peerArchiveRefs := make([]NodeRef, 0, len(allPeers))
	peerArchivePaths := make([]string, 0, len(allPeers))
	peerGlobalRefs := make([]NodeRef, 0, len(allPeers))
	peerGlobalPaths := make([]string, 0, len(allPeers))

	// PR-31 D05: aggregate peer-GLOBAL ADDINCL transitively. The
	// dedup map preserves DECLARATION order across the PEERDIR walk
	// (R14 — first peer's declarations come first in cmd_args).
	peerAddInclSeen := map[string]struct{}{}
	peerAddInclGlobal := make([]string, 0, 16)

	addPeerGlobal := func(paths []string) {
		for _, p := range paths {
			if _, dup := peerAddInclSeen[p]; dup {
				continue
			}

			peerAddInclSeen[p] = struct{}{}
			peerAddInclGlobal = append(peerAddInclGlobal, p)
		}
	}

	for i, p := range allPeers {
		peerPath := filepath.Clean(p)

		isDefault := i < len(defaults)

		if isDefault && !peerYaMakeExists(ctx.sourceRoot, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(instance, peerPath)
		peerResult := genModule(ctx, peerInstance)

		// PR-31 D05: every peer (including header-only) contributes
		// its accumulated GLOBAL ADDINCL to the consumer's effective
		// search path. The transitive aggregation means a peer's
		// peer-GLOBAL set is already folded into peerResult.AddInclGlobal,
		// so a single union here yields the full closure without a
		// separate BFS pass.
		addPeerGlobal(peerResult.AddInclGlobal)

		if peerResult.isPROGRAM {
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		// PR-27: a header-only peer (e.g. library/cpp/sanitizer/include)
		// emits no AR; skip the archive-dep wiring so the caller
		// does not pin a zero-valued NodeRef. The peer's transitive
		// closure was already walked by the recursive genModule call.
		if peerResult.headerOnly {
			continue
		}

		peerArchiveRefs = append(peerArchiveRefs, peerResult.ARRef)
		peerArchivePaths = append(peerArchivePaths, peerPath+"/"+ArchiveName(peerPath))

		if peerResult.GlobalRef != nil {
			peerGlobalRefs = append(peerGlobalRefs, *peerResult.GlobalRef)
			peerGlobalPaths = append(peerGlobalPaths, peerResult.GlobalPath)
		}
	}

	// PR-31 D05: this module's effective AddInclGlobal is its OWN
	// GLOBAL ADDINCL plus the union of every peer's transitive set.
	// Stored on the result so transitive consumers see the closure
	// in one shot.
	effectiveAddInclGlobal := make([]string, 0, len(d.addInclGlobal)+len(peerAddInclGlobal))
	effectiveSeen := map[string]struct{}{}

	for _, p := range d.addInclGlobal {
		if _, dup := effectiveSeen[p]; dup {
			continue
		}

		effectiveSeen[p] = struct{}{}
		effectiveAddInclGlobal = append(effectiveAddInclGlobal, p)
	}

	for _, p := range peerAddInclGlobal {
		if _, dup := effectiveSeen[p]; dup {
			continue
		}

		effectiveSeen[p] = struct{}{}
		effectiveAddInclGlobal = append(effectiveAddInclGlobal, p)
	}

	// Per-source dispatch. JoinSrcs entries become JS+CC pairs
	// folded in alongside regular SRCS. Header sources (`.h` /
	// `.hpp`) are skipped. PR-25 keeps own-source ordering
	// faithful: regular SRCS in declaration order, then each
	// JOIN_SRCS's compiled output appended, then global srcs are
	// processed as a separate AR step (so they don't pollute the
	// regular `.a`).
	ccRefs := make([]NodeRef, 0, len(d.srcs)+len(d.joinSrcs))
	ccOutputs := make([]string, 0, len(d.srcs)+len(d.joinSrcs))
	// PR-31 D11: accumulate the union of every CC member's inputs
	// (primary source + IncludeInputs, deduped, in DFS-discovery
	// order) so the downstream AR/LD step can fold these into its
	// own `inputs` slice per the sg.json shape (AR includes the
	// source files of its archived .o files, plus their resolved
	// header closures).
	memberInputs := make([]string, 0, 64)
	memberInputsSeen := map[string]struct{}{}

	addMemberInputs := func(paths []string) {
		for _, p := range paths {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}

			memberInputsSeen[p] = struct{}{}
			memberInputs = append(memberInputs, p)
		}
	}

	moduleInputs := ModuleCCInputs{
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddInclGlobal,
		CXXFlags:          d.cxxFlags,
		COnlyFlags:        d.cOnlyFlags,
		SrcDir:            d.srcDir,
		SourceRoot:        ctx.sourceRoot,
	}

	// PR-30 D06: ancestor-only SRCDIR rebase. The "PROGRAM with SRCDIR
	// pointing at an ancestor of instance.Path" pattern (typified by
	// `contrib/tools/ragel6/bin` whose SRCDIR is `contrib/tools/ragel6`)
	// is the only shape where the reference rebases module_dir to
	// SRCDIR. LIBRARYs with SRCDIR keep module_dir at instance.Path
	// and route per-source via composeCCPaths' SRCDIR-aware composer.
	ancestorRebase := d.srcDir != "" && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(d.srcDir, instance.Path)

	for _, src := range d.srcs {
		ref, outPath, ccIns, ok := emitOneSource(ctx, instance, d.srcDir, src, moduleInputs, ancestorRebase)

		if !ok {
			continue
		}

		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		addMemberInputs(ccIns)
	}

	for _, js := range d.joinSrcs {
		// PR-28-D11 + PR-30 D06: rebase onto SRCDIR only when
		// `ancestorRebase` is set (PROGRAM with ancestor SRCDIR; the
		// ragel6/bin pattern). Otherwise keep srcInstance at
		// instance.Path — JOIN_SRCS in LIBRARY-with-sibling-SRCDIR
		// modules (none in M2 closure today, but defended for future)
		// emit at the LIBRARY's own dir.
		srcInstance := instance

		if ancestorRebase {
			srcInstance.Path = d.srcDir
		}

		jsRef, joinOut := EmitJS(srcInstance, js.OutputName, js.Sources, ctx.emit)

		// EmitJS returns a $(BUILD_ROOT)/<srcInstance.Path>/<name>
		// absolute path; convert to srcInstance-relative for the
		// downstream EmitCC. PR-29-D07: the JS output lives under
		// $(BUILD_ROOT) — pass IsGenerated so EmitCC composes the
		// inputPath under $(BUILD_ROOT) instead of $(SOURCE_ROOT).
		// PR-30 D04: thread the JS NodeRef as the downstream CC's
		// `Generator` so the CC node carries an explicit dep on its
		// source-generating JS node, matching the reference shape
		// (every JS-derived CC in the reference has DepRefs=[js UID]).
		// PR-29 deferred this because the wider closure had not yet
		// landed; PR-30's musl/full + ALLOCATOR_IMPL closure widening
		// absorbs the 2-multiset cost many times over.
		jsRel := strings.TrimPrefix(joinOut, "$(BUILD_ROOT)/"+srcInstance.Path+"/")

		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = jsRef
		ccIn.HasGenerator = true

		ref, outPath := EmitCC(srcInstance, jsRel, ccIn, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		// JS-derived CC: input is the BUILD_ROOT-rooted joined .cpp.
		jsGenInput := "$(BUILD_ROOT)/" + srcInstance.Path + "/" + jsRel
		addMemberInputs([]string{jsGenInput})
	}

	// GLOBAL_SRCS get their own CC nodes and a separate AR pass
	// (see below). Filter headers here too.
	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]string, 0, len(d.globalSrcs))

	// PR-31 D11: GLOBAL_SRCS contribute their own member-inputs slice
	// to the .global.a archive (separate accumulator from regular AR).
	globalMemberInputs := make([]string, 0, 16)
	globalMemberInputsSeen := map[string]struct{}{}

	for _, src := range d.globalSrcs {
		ref, outPath, ccIns, ok := emitOneSource(ctx, instance, d.srcDir, src, moduleInputs, ancestorRebase)

		if !ok {
			continue
		}

		globalRefs = append(globalRefs, ref)
		globalOutputs = append(globalOutputs, outPath)

		for _, p := range ccIns {
			if _, dup := globalMemberInputsSeen[p]; dup {
				continue
			}

			globalMemberInputsSeen[p] = struct{}{}
			globalMemberInputs = append(globalMemberInputs, p)
		}
	}

	if d.moduleStmt.Name == "PROGRAM" {
		// PR-28-D01: PROGRAM(name) declares the linker output basename
		// directly. Most ya.makes elide the argument (PROGRAM() →
		// binary inherits the directory's last component) but
		// `contrib/tools/ragel6/bin/ya.make` declares
		// `PROGRAM(ragel6)` so the binary is `bin/ragel6`, not
		// `bin/bin`. Pass through to EmitLD; the emitter's empty-fallback
		// matches the elided-argument case.
		var binaryName string

		if len(d.moduleStmt.Args) > 0 {
			binaryName = d.moduleStmt.Args[0]
		}

		ldRef := EmitLD(
			instance,
			binaryName,
			ccRefs, ccOutputs,
			peerArchiveRefs, peerArchivePaths,
			nil, nil,
			peerGlobalRefs, peerGlobalPaths,
			memberInputs,
			ctx.emit,
		)
		ldPath := LDOutputPath(instance, binaryName)

		result := &moduleEmitResult{
			ARRef:         ldRef,
			ARPath:        ldPath,
			isPROGRAM:     true,
			LDRef:         ldRef,
			LDPath:        ldPath,
			AddInclGlobal: effectiveAddInclGlobal,
		}
		ctx.memo[instance] = result

		return result
	}

	// LIBRARY: regular AR over own CCs. Peer-archive DepRefs are
	// intentionally NOT threaded — PR-30 D05: empirical reference probe
	// confirmed every reference AR has zero AR-on-AR deps. Threading
	// peer-archive refs into AR.DepRefs (PR-15 → PR-29 behaviour)
	// shifted the parent AR's L0 fingerprint away from the reference
	// shape on 24 paired AR pairs. Peer archives correctly flow into
	// the consumer's downstream LD via the `peerArchiveRefs` slot in
	// `EmitLD`'s call site below — only the LIBRARY AR drops them.
	arRef := EmitAR(instance, ccRefs, ccOutputs, nil, memberInputs, ctx.emit)
	_ = peerArchiveRefs // retained as a loop accumulator for the PROGRAM LD branch above; intentionally unused for the LIBRARY AR.
	arPath := "$(BUILD_ROOT)/" + instance.Path + "/" + ArchiveName(instance.Path)

	result := &moduleEmitResult{
		ARRef:         arRef,
		ARPath:        arPath,
		isPROGRAM:     false,
		LDRef:         arRef,
		LDPath:        arPath,
		AddInclGlobal: effectiveAddInclGlobal,
	}

	if len(globalRefs) > 0 {
		globalRef := EmitARGlobal(instance, globalRefs, globalOutputs, globalMemberInputs, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = instance.Path + "/" + globalArchiveName(instance.Path)
	}

	ctx.memo[instance] = result

	return result
}

// walkPeersForGlobalAddIncl walks the peers of a header-only LIBRARY
// (PR-27) to ensure their transitive closure is discovered (genModule
// memoises so other consumers can pick them up later) AND returns the
// union of every peer's transitive AddInclGlobal contribution
// (PR-31 D05). The header-only module emits no AR, so the per-peer
// archive refs are intentionally dropped; only the GLOBAL ADDINCL
// peer-propagation is preserved.
func walkPeersForGlobalAddIncl(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	defaults := defaultPeerdirsFor(ctx, instance)

	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))
	pathSeen := map[string]struct{}{}
	out := make([]string, 0, 16)

	add := func(paths []string) {
		for _, p := range paths {
			if _, dup := pathSeen[p]; dup {
				continue
			}

			pathSeen[p] = struct{}{}
			out = append(out, p)
		}
	}

	for _, p := range defaults {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerPath := filepath.Clean(p)

		if !peerYaMakeExists(ctx.sourceRoot, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(instance, peerPath)
		add(genModule(ctx, peerInstance).AddInclGlobal)
	}

	for _, p := range d.peerdirs {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}

		peerPath := filepath.Clean(p)
		peerInstance := derivePeerInstance(instance, peerPath)
		add(genModule(ctx, peerInstance).AddInclGlobal)
	}

	return out
}

// hasCompilableSource reports whether the module has at least one
// source the rule emitter would actually compile (excluding pure
// headers in SRCS, which the upstream uses as IDE / dependency-
// tracking metadata). Modules that contain only JOIN_SRCS / globals
// also count.
func hasCompilableSource(d *moduleData) bool {
	for _, s := range d.srcs {
		if !isHeaderSource(s) {
			return true
		}
	}

	if len(d.joinSrcs) > 0 {
		return true
	}

	for _, s := range d.globalSrcs {
		if !isHeaderSource(s) {
			return true
		}
	}

	return false
}

// isHeaderSource reports whether `srcRel` is a header file the
// emitter should skip.
func isHeaderSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".h") || strings.HasSuffix(srcRel, ".hpp")
}

// emitOneSource dispatches a single source by extension. Returns
// `(ref, outputPath, ccInputs, true)` when a node was emitted (the
// 3rd return is the CC node's input list — primary source path
// followed by IncludeInputs — so the caller's downstream AR/LD step
// can fold these into its own `inputs` aggregate per the sg.json
// AR/LD shape, PR-31 D11). For headers (silently skipped) returns
// `(_, _, nil, false)`. Throws on unknown extensions so a new source
// kind surfaces during integration rather than being silently
// dropped.
//
// `srcDir` is the module's `SRCDIR(...)` setting (empty when none).
// Per PR-28-D02, when non-empty it relocates the per-source emitter's
// view of the module: SRCS resolve to `$(SOURCE_ROOT)/<srcDir>/<rel>`
// and the emitted node's `module_dir` becomes `<srcDir>` instead of
// `instance.Path`. The LD/AR/Global archives that wrap these sources
// remain at `instance.Path` (the walker called from genModule keeps
// instance unchanged for those). For ragel6/bin: `instance.Path =
// contrib/tools/ragel6/bin`, `srcDir = contrib/tools/ragel6` →
// per-source CC nodes show `module_dir = contrib/tools/ragel6` and
// inputs `$(SOURCE_ROOT)/contrib/tools/ragel6/<src>`, while the
// containing LD lands at `bin/ragel6`.
//
// `in` carries the module's per-source-language compile knobs (D02
// CXXFLAGS / CONLYFLAGS, D03 ADDINCL). Per PR-29 the walker collects
// ADDINCL/CXXFLAGS/CONLYFLAGS into moduleData and threads them into
// EmitCC via this struct.
func emitOneSource(ctx *genCtx, instance ModuleInstance, srcDir string, srcRel string, in ModuleCCInputs, ancestorRebase bool) (NodeRef, string, []string, bool) {
	if isHeaderSource(srcRel) {
		return NodeRef{}, "", nil, false
	}

	// PR-30 D06: SRCDIR rebase is now ancestor-only and only fires when
	// the caller has decided this is the "include-from-parent" pattern
	// (PROGRAM whose SRCDIR is an ancestor of instance.Path; ragel6/bin
	// is the canonical case). LIBRARYs with SRCDIR (libcxxabi-parts,
	// musl_extra, tcmalloc/no_percpu_cache) keep
	// `srcInstance.Path == instance.Path`; the per-source SRCDIR
	// resolution happens inside EmitCC via `in.SrcDir`/`in.SourceRoot`
	// (composeCCPaths).
	srcInstance := instance

	if ancestorRebase {
		srcInstance.Path = srcDir
	}

	// When the instance is rebased to SRCDIR (ragel6/bin pattern), the
	// composer should NOT additionally apply SRCDIR routing — clear
	// SrcDir on the per-source input bag. When NOT rebased (LIBRARY
	// shape), keep SrcDir so the composer can decide local-vs-SRCDIR
	// resolution per source.
	srcIn := in
	if ancestorRebase {
		srcIn.SrcDir = ""
	}

	switch {
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		// PR-31 D08: resolve the transitive include closure for
		// non-generated sources. Generated sources (handled in the
		// JS / R6 branches below — NOT this site) skip the scanner:
		// their primary input lives under $(BUILD_ROOT) and doesn't
		// exist on disk at scan time. The walker passes the
		// scanner-aware srcIn down to EmitCC.
		srcIn.IncludeInputs = scanIncludesForSource(ctx, srcInstance, srcRel, srcIn)

		ref, outPath := EmitCC(srcInstance, srcRel, srcIn, ctx.emit)

		// AR/LD aggregate the per-CC inputs (primary source +
		// resolved headers) into their own inputs slice per the
		// sg.json shape (PR-31 D11). Compose the input list here
		// (matching what EmitCC itself does internally).
		inputPath := emittedSourceInputPath(srcInstance, srcRel, srcIn, ctx.sourceRoot)
		ccInputs := append([]string{inputPath}, srcIn.IncludeInputs...)

		return ref, outPath, ccInputs, true
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		// PR-28: when a host (`Flags.PIC`) `.S`/`.s` source belongs
		// to a module known to use yasm (`asmlibYasmModules`), recurse
		// into the host yasm PROGRAM and wire its LDRef into the AS
		// node's `ForeignDepRefs["tool"]` (matches reference: 25
		// host-asmlib AS nodes have foreign_deps.tool=yasm). Other
		// `.S` sources (target-side AS, host chkstk, host
		// libcxx/libcxxabi shims) pass nil — they assemble via
		// clang's built-in assembler with no foreign_deps.
		//
		// asmlib host walk is wired but not reached in the M2 archiver
		// closure because we peer contrib/libs/musl, not
		// contrib/libs/musl/full (the upstream PEERDIR rule
		// MUSL=yes && !MUSL_LITE → musl/full lives at
		// build/ymake.core.conf:1238-1245 and is not modelled here).
		// Closing the musl/full closure path is deferred to a follow-up
		// PR. The trigger code here remains as forward-scaffolding so
		// that PR will not need to re-derive the wiring; the existing
		// synthetic test pins it.
		var yasmRef *NodeRef

		if instance.Flags.PIC && asmlibYasmModules[instance.Path] {
			const yasmPath = "contrib/tools/yasm"

			yasmInstance := instance.WithHost(ctx.cfg)
			yasmInstance.Path = yasmPath
			yasmInstance.Flags = inferFlagsFromPath(yasmPath, true)

			yasmResult := genModule(ctx, yasmInstance)
			ldRef := yasmResult.LDRef
			yasmRef = &ldRef
		}

		// PR-31 D11: scan transitive headers for AS sources too. A
		// small subset of `.S` sources include `.h`/`.inc` headers
		// (e.g. cxxsupp/builtins/chkstk.S → assembly.h +
		// int_endianness.h); the scanner populates the AS node's
		// inputs and feeds the downstream AR's memberInputs aggregator.
		asIncludeInputs := scanIncludesForSource(ctx, srcInstance, srcRel, srcIn)
		ref, outPath := EmitAS(srcInstance, srcRel, nil, yasmRef, asIncludeInputs, ctx.emit)

		asInputPath := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		asInputs := append([]string{asInputPath}, asIncludeInputs...)

		return ref, outPath, asInputs, true
	case strings.HasSuffix(srcRel, ".rl6"):
		// Host-ragel6 recursion (D31, eager per PR-28). The recursion
		// happens here so the resulting LD's outputs[0] can be
		// threaded into EmitR6's cmd_args[0] (PR-28-D01 — internal
		// consistency between R6 invocation path and our own host LD).
		//
		// `contrib/tools/ragel6/bin` is the real host-PROGRAM
		// directory; the parent `contrib/tools/ragel6/ya.make` uses
		// INCLUDE(${ARCADIA_ROOT}/...) which our parser does not yet
		// expand (M5+ variable substitution work).
		const ragelBinPath = "contrib/tools/ragel6/bin"

		// Fallback ragel6 path: used when the host walk fails its
		// parse. The literal matches the reference graph's invocation
		// path, so a zero-host-LD codepath at least produces a
		// meaningful argv even though the host LD node is missing.
		const ragelFallbackPath = "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6"

		var (
			ragelLDRef     NodeRef
			ragelBinaryStr = ragelFallbackPath
		)

		ragelInstance := instance.WithHost(ctx.cfg)
		ragelInstance.Path = ragelBinPath
		ragelInstance.Flags = inferFlagsFromPath(ragelInstance.Path, true)

		if exc := Try(func() {
			ragelResult := genModule(ctx, ragelInstance)
			ragelLDRef = ragelResult.LDRef
			ragelBinaryStr = ragelResult.LDPath
		}); exc != nil {
			// Only swallow *ParseError — the documented gap when
			// ragel6's ya.make contains INCLUDE(${ARCADIA_ROOT}/...)
			// that our parser cannot yet expand (M5+ variable
			// substitution). Any other exception is unexpected and
			// must propagate loudly rather than silently produce a
			// zero ragel6LD ref.
			var pe *ParseError

			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}

			// Leave ragelLDRef zero-valued and ragelBinaryStr at the
			// reference-shaped fallback; document the host-tool gap
			// rather than re-throwing. The R6 node will not dep-link
			// to a host ragel6, but its cmd_args[0] still names a
			// plausible binary path.
		}

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryStr, ctx.emit)
		// PR-29-D07: same shape as the JS branch above. Pass
		// IsGenerated so the downstream CC composes inputPath under
		// $(BUILD_ROOT)/<srcInstance.Path>/<rel> rather than the
		// stale $(SOURCE_ROOT) shape. PR-30 D04: thread r6Ref as the
		// downstream CC's `Generator` so the CC node carries an
		// explicit dep on its R6 source-generator node, matching the
		// reference shape.
		ccSrcRel := strings.TrimPrefix(r6Out, "$(BUILD_ROOT)/"+srcInstance.Path+"/")

		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.Generator = r6Ref
		ccIn.HasGenerator = true

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// R6-derived CC: primary input is the BUILD_ROOT-rooted .cpp
		// generated by ragel6. No scanner pass (the .cpp doesn't exist
		// on disk at scan time). Inputs are the .cpp path only.
		genInputPath := "$(BUILD_ROOT)/" + srcInstance.Path + "/" + ccSrcRel

		return ccRef, ccOut, []string{genInputPath}, true
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)

	return NodeRef{}, "", nil, false
}

// emittedSourceInputPath mirrors composeCCPaths' inputPath logic so
// the walker can compose the AR/LD inputs aggregator without having
// to round-trip through EmitCC's emitted node. Returns the
// `$(SOURCE_ROOT)/...` (or `$(BUILD_ROOT)/...` for IsGenerated)
// path the CC node will use as its primary input.
func emittedSourceInputPath(instance ModuleInstance, srcRel string, in ModuleCCInputs, sourceRoot string) string {
	if in.IsGenerated {
		return "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel
	}

	if in.SrcDir != "" && in.SrcDir != instance.Path {
		localCandidate := filepath.Join(sourceRoot, instance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			return "$(SOURCE_ROOT)/" + in.SrcDir + "/" + srcRel
		}
	}

	return "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
}

// scanIncludesForSource resolves the source's actual on-disk path
// (matching composeCCPaths' SRCDIR-aware semantics) and invokes the
// include scanner. Returns the SOURCE_ROOT-relative include set the
// scanner produces, or nil when the scanner is unavailable, the
// source has no on-disk file, or the scanner produces an empty
// closure.
//
// PR-31 D08 — the source-rel and ScanContext that drives the
// scanner per CC node. The own-AddIncl + peer-GLOBAL-AddIncl
// search path mirrors what cmd_args -I uses, plus a baseline set
// for the linux-headers / musl-arch include paths the cc bundle
// includes implicitly.
func scanIncludesForSource(ctx *genCtx, srcInstance ModuleInstance, srcRel string, in ModuleCCInputs) []string {
	scanner := ctx.scannerTarget

	if srcInstance.Flags.PIC {
		scanner = ctx.scannerHost
	}

	if scanner == nil {
		return nil
	}

	// Mirror composeCCPaths' source-resolution logic so the scanner
	// hashes the same on-disk file as the cc compiler will read.
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if in.SrcDir != "" && in.SrcDir != srcInstance.Path {
		// SRCDIR override: the source resolves under SRCDIR when no
		// local file at instance.Path/<srcRel> exists.
		localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			srcRelOnDisk = in.SrcDir + "/" + srcRel
		}
	}

	scanCtx := ScanContext{
		SourceRel:       srcRelOnDisk,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(srcInstance),
	}

	return scanner.WalkClosure(scanCtx)
}

// includeScannerBasePaths returns the implicit include search path
// that the cc bundle adds via cmd_args (SOURCE_ROOT + linux-headers +
// musl arch when applicable). The scanner uses these as fallback
// resolution candidates so headers like `<util/folder/path.h>` (repo-
// rooted system-form includes) and `<linux/types.h>` (linux-headers)
// resolve in the same way the compiler would.
//
// Non-musl flavours: an empty-string entry is prepended first,
// representing the SOURCE_ROOT itself. The resolver treats an empty
// prefix as "resolve directly against SOURCE_ROOT" — so `<util/foo.h>`
// tries $(SOURCE_ROOT)/util/foo.h before the linux-headers subtree.
// This mirrors the `-I$(SOURCE_ROOT)` flag the compiler receives via
// cmd_args for every non-musl CC node.
//
// Musl flavours (composeMuslCC / composeMuslHostCC paths) MUST NOT get
// the empty prefix — they use `-nostdinc` and have a fully explicit
// include search path via muslCcIncludes. Adding SOURCE_ROOT there
// would cause false resolution of system-form includes against the
// repo root, silently expanding the musl CC input sets incorrectly.
func includeScannerBasePaths(instance ModuleInstance) []string {
	base := []string{
		"contrib/libs/linux-headers",
		"contrib/libs/linux-headers/_nf",
	}

	isMusl := instance.Path == "contrib/libs/musl" || strings.HasPrefix(instance.Path, "contrib/libs/musl/")

	if isMusl {
		// Mirror muslCcIncludes / muslCcIncludesX8664: arch + generic
		// + src/include + src/internal + include + extra. Use the
		// instance's PIC flag to pick aarch64 vs x86_64 (the same
		// switch composeMuslCC vs composeMuslHostCC uses).
		var arch string

		if instance.Flags.PIC {
			arch = "x86_64"
		} else {
			arch = "aarch64"
		}

		muslPaths := []string{
			"contrib/libs/musl/arch/" + arch,
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/src/include",
			"contrib/libs/musl/src/internal",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
		}

		// Musl paths come BEFORE linux-headers in the cmd_args ordering.
		out := make([]string, 0, len(muslPaths)+len(base))
		out = append(out, muslPaths...)
		out = append(out, base...)

		return out
	}

	// Non-musl: prepend the empty-prefix entry (SOURCE_ROOT itself) so
	// repo-rooted system-form includes like `<util/folder/path.h>`
	// resolve against $(SOURCE_ROOT)/util/folder/path.h.
	out := make([]string, 0, 1+len(base))
	out = append(out, "")
	out = append(out, base...)

	return out
}
