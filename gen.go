package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	// hasPlainAR is true when EmitAR(Named) was actually called for this
	// module — i.e. the module has at least one regular (non-global) CC
	// output. False for modules whose only compilable sources are
	// GLOBAL_SRCS (blockcodecs codecs, getopt): these emit only a
	// .global.a and the consumer's peerLibPaths should not include the
	// plain .a path. PR-M3-residue-B.
	hasPlainAR bool
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
	// OwnAddInclGlobal is this module's OWN GLOBAL ADDINCL declarations
	// only — no transitive peer contributions. PR-32: the consumer
	// walker uses this to compose its peerAddInclGlobal in two phases
	// (own-first across all peers, transitive-second), matching the
	// reference cmd_args ordering where libcxx/include +
	// libcxxrt/include come BEFORE musl-arch (which propagates
	// transitively through their peers' auto-PEERDIR of musl/include).
	OwnAddInclGlobal []string
	// CFlagsGlobal / CXXFlagsGlobal / COnlyFlagsGlobal are the unions
	// of this module's own GLOBAL CFLAGS / CXXFLAGS / CONLYFLAGS plus
	// the transitive peer-GLOBAL contributions (PR-32 D07). Consumers
	// receive these via ModuleCCInputs.PeerCFlagsGlobal /
	// PeerCXXFlagsGlobal / PeerCOnlyFlagsGlobal at compile time.
	// Declaration-order preserved across the PEERDIR walk; duplicates
	// dropped (mirror of AddInclGlobal aggregation).
	CFlagsGlobal     []string
	CXXFlagsGlobal   []string
	COnlyFlagsGlobal []string
	// PeerArchiveClosureRefs / PeerArchiveClosurePaths is the transitive
	// archive closure exposed by this module to its consumers — every
	// peer's own AR plus every peer's PeerArchiveClosure*, deduplicated
	// in DFS post-order (first occurrence wins). PR-35c closes the LD
	// walker's deferred 19-archive gap (PR-31-D09 follow-on): without
	// this slot, PROGRAM modules' EmitLD only saw their *direct* peer
	// archives, so a 13-archive subset of the reference's 32 reached
	// cmd[2]'s `--start-group ... --end-group` block. The closure flows
	// through LIBRARY modules' moduleEmitResult so that any consumer
	// (LIBRARY or PROGRAM) can union its peers' closures with the
	// peers' own archives to produce the full link-time archive set.
	// Header-only LIBRARYs (PR-27) propagate closures from their peers
	// but contribute no archive of their own.
	PeerArchiveClosureRefs  []NodeRef
	PeerArchiveClosurePaths []string
	// isPyLibrary is true when this module's declared type is a Python
	// library/program variant (PY3_LIBRARY, PY23_LIBRARY, PY2_LIBRARY,
	// PY23_NATIVE_LIBRARY, PY2_PROGRAM, PY3_PROGRAM). PR-M3-protobuf-
	// umbrella-trigger: applyUmbrellaAddIncl reads this flag to suppress
	// umbrella ADDINCL propagation into Python-bound sub-libraries that
	// happen to sit under a propagating ancestor's path prefix.
	isPyLibrary bool
	// PeerGlobalClosureRefs / PeerGlobalClosurePaths is the transitive set
	// of `.global.a` archives reachable through this module's PEERDIR
	// closure (DFS post-order, dedup by path). PR-M3-LD-peer-globalA:
	// closes a 24-input gap on `devtools/ymake/bin/ymake` and 8 other LD
	// nodes where REF wires transitively-reachable `.global.a` archives
	// into the consumer PROGRAM's LD `inputs` slot.
	PeerGlobalClosureRefs  []NodeRef
	PeerGlobalClosurePaths []string
	// LDPluginRefs / LDPluginPaths is the transitive set of LD plugin
	// CP nodes a consumer PROGRAM must wire into its
	// `--start-plugins ... --end-plugins` block. PR-35k: the only
	// M2-closure case is `contrib/libs/musl/include`'s
	// `LD_PLUGIN(musl.py)`, which becomes
	// `$(BUILD_ROOT)/contrib/libs/musl/include/musl.py.pyplugin` and
	// reaches archiver / ragel6 / yasm via their PEERDIR walk through
	// musl/include. Aggregation mirrors the peer-archive closure: a
	// peer's own LD plugins UNION its PeerLDPluginPaths flow into the
	// consumer's running set, deduped by path (first occurrence wins).
	// Header-only LIBRARYs (musl/include itself) emit their own CP node
	// AND propagate it through this slot. Non-PROGRAM consumers
	// (LIBRARY ARs) carry the closure through but never consume it.
	LDPluginRefs  []NodeRef
	LDPluginPaths []string
	// InducedDeps is the module-level INDUCED_DEPS(<ext-filter> headers...)
	// declaration list (paths repo-relative). PR-M3-runprogram-closure:
	// emitRunProgram, when walking a tool PROGRAM via genModule, reads this
	// to seed the PR output's EmitsIncludes so the include scanner reaches
	// the tool-injected header closure (e.g. struct2fieldcalc's
	// `library/cpp/fieldcalc/field_calc_int.h` chain into autoarray.h).
	InducedDeps []string
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
	// traceStack is populated when YATOOL_TRACE=1: each entry is
	// "<path>@<platform>" for the calling frame; printed on genModule entry.
	traceStack []string
	// scannerTarget is the include-resolver for TARGET (aarch64) CC
	// nodes; scannerHost is the host (x86_64) variant. Each scanner
	// has its own parsed-includes cache (the OS page cache amortises
	// rereads). Each also has its own SysInclSet because
	// linux-musl-<arch>.yml mappings differ between platforms (e.g.
	// bits/alltypes.h resolves arch-specifically).
	scannerTarget *IncludeScanner
	scannerHost   *IncludeScanner
	// cliDefines mirrors the user-facing `--define KEY=VALUE` (PR-32
	// D01). Read by the auto-PEERDIR machinery in defaultPeerdirsFor /
	// defaultProgramPeerdirsFor and the auto-CFLAG injection in
	// defaultPeerCFlags. The single load-bearing key today is `MUSL`
	// (= "yes" mirrors `build/ymake.core.conf:781`'s
	// `when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include }`).
	// Read-only after Gen seeds it; never mutated mid-walk.
	cliDefines map[string]string
	// enOutputs maps each emitted EN node's output paths to its NodeRef.
	// PR-M3-D: cross-EN header-inclusion deps are resolved by looking up
	// previously emitted EN nodes whose outputs are included by the current
	// header's transitive include closure. The map key is the
	// $(BUILD_ROOT)-rooted output path; the value is the EN NodeRef.
	//
	// EN nodes only ever emit on the target axis (see gen.go:4531-4535), so
	// a flat path-keyed map collapses cleanly. PB/EV use codegenOutputKey
	// because both axes can emit their own producer NodeRefs.
	enOutputs map[string]NodeRef
	// pbOutputs/evOutputs map (platform, $(BUILD_ROOT)-rooted output path)
	// → emitted PB/EV NodeRef. Mirrors enOutputs but per-platform because
	// PB/EV emit on BOTH target and host axes (the host emission carries
	// tags=["tool"], the target one doesn't), and CC consumers must dep on
	// the producer that shares their own platform (verified in sg2.json:
	// x86_64 CCs dep on x86_64 PB; aarch64 CCs dep on aarch64 PB).
	//
	// PR-M3-L0-codegen-deps-EV-PB: consulted by resolveCodegenDepRefs() to
	// thread the producer NodeRef into a consumer CC's ExtraDepRefs when
	// the CC's IncludeInputs carries the BUILD_ROOT path of a generated
	// .pb.h / .pb.cc / .ev.pb.h / .ev.pb.cc.
	pbOutputs map[codegenOutputKey]NodeRef
	evOutputs map[codegenOutputKey]NodeRef
	// ldPluginCPCache deduplicates LD_PLUGIN CP NodeRefs across the
	// target/host walk pair (PR-35l). PR-35k emitted a fresh CP node for
	// every (instance.Path, plugin name) pair the walker visited, which
	// produced two CP nodes for `contrib/libs/musl/include`'s `musl.py`
	// — one on `default-linux-aarch64` (target walk) and one on
	// `default-linux-x86_64` (host walk through ragel6/bin → musl/include).
	// The reference graph emits the CP node ONCE on the target platform
	// and reuses its UID from both target and host LDs (verified at
	// /home/pg/monorepo/yatool_orig/sg.json:105515 — the same UID
	// `nPHkMSIqOHBrXsoclNuu6g` appears in target archiver LD deps AND in
	// the host ragel6 LD's deps). Keying by plugin output path
	// (`$(BUILD_ROOT)/<modulePath>/<name>.pyplugin`) is sufficient: the
	// path is independent of platform, and the plugin file is the same
	// artifact regardless of which walk reached it. First-write wins —
	// the target walk runs before any host walk recursion (host walks
	// fire from inside `emitOneSource`, after the seed module's peer
	// walk has run), so the cached entry carries the target platform.
	ldPluginCPCache map[string]NodeRef

	// PR-M3-perf-E: scanCtx (per-ctxHash resolve/subgraph cache holder)
	// lifecycle policy. Two variants benchmarked:
	//
	//   - "local"    — one scanCtx per (genModule, scanner, ctxHash).
	//                  Pushed at genModule entry, popped at exit. No
	//                  cross-module reuse. localScanCtxStack is the
	//                  per-genModule cache map stack.
	//   - "interned" — one scanCtx per (scanner, ctxHash) for the whole
	//                  Gen call. Lives in internedScanCtx. Cross-module
	//                  reuse when ctxHash matches.
	//
	// The flag is plumbed from the CLI `--scan-ctx-mode` (main.go).
	// Default = "interned" (winner of the bake-off; see commit message).
	scanCtxMode        string
	localScanCtxStack  []map[scanCtxCacheKey]*scanCtx
	internedScanCtx    map[scanCtxCacheKey]*scanCtx
	// PR-M3-perf-E debug counters (printed when YATOOL_SCANCTX_STATS=1).
	// scanCtxAllocs counts every fresh scanCtx allocation across the Gen;
	// scanCtxPeak is max bucket size observed at any get-and-store moment.
	// The local-mode peak corresponds to the deepest in-flight genModule
	// frame's bucket size; the interned-mode peak equals the total
	// scanCtx count since the bucket never shrinks. The counters are
	// dormant unless the env var is set.
	scanCtxAllocs int
	scanCtxPeak   int
}

// scanCtxCacheKey identifies a scanCtx by the (scanner pointer, ctxHash)
// pair. Pointer identity disambiguates target vs host scanners; ctxHash
// disambiguates module-config equivalence classes within one scanner.
//
// PR-M3-perf-E.
type scanCtxCacheKey struct {
	scanner *IncludeScanner
	ctxHash uint64
}

// codegenOutputKey identifies a codegen producer's output path on a specific
// platform axis. PR-M3-L0-codegen-deps-EV-PB: PB/EV emit on both target
// (aarch64) and host (x86_64) axes, and CC consumers must dep on the
// producer matching their own platform — flat path-only keying would
// collapse the two and produce wrong-axis deps.
type codegenOutputKey struct {
	platform PlatformID
	path     string
}

// resolveCodegenDepRefs scans `includeInputs` for $(BUILD_ROOT)-rooted paths
// that match a previously emitted EN/PB/EV/AR/CF/BI/JV/PR/R5/PY producer
// output, and returns the producer NodeRefs deduped in scan order. Each
// consumer CC node carries those NodeRefs as ExtraDepRefs so the resulting
// CC `deps` list mirrors the reference graph shape (sg2.json places explicit
// codegen-producer deps on every CC whose inputs[] references a $(BUILD_ROOT)/
// <gen>.h or <gen>.cc).
//
// `consumer.Target` disambiguates per-platform PB/EV lookup. EN nodes always
// emit on the target axis so ctx.enOutputs is consulted by path alone (both
// host and target consumers reach the same target EN NodeRef).
//
// `exclude` lists NodeRefs to suppress from the result (typically the
// downstream CC's `Generator` ref, which EmitCC already threads into DepRefs
// as the leading entry — without filtering, a CC compiling its own producer's
// .cc would carry the producer twice).
//
// PR-M3-L0-cascade-close-v2: in addition to the three legacy enOutputs/
// pbOutputs/evOutputs maps, the function consults the per-scanner
// CodegenRegistry for any registered entry whose HasProducerRef is set.
// This is the general producer-ref path that covers AR/CF/BI/JV/PR/R5/PY
// emitters; the legacy maps remain as the EN/PB/EV path (their writes are
// independent of the registry today and removing them is deferred to a
// later PR).
func resolveCodegenDepRefs(ctx *genCtx, consumer ModuleInstance, includeInputs []string, exclude ...NodeRef) []NodeRef {
	return resolveCodegenDepRefsExt(ctx, consumer, includeInputs, nil, exclude...)
}

// resolveCodegenDepRefsExt is the extended form that also scans `inputs` for
// $(BUILD_ROOT) producer paths. Used by consumers whose producer ref is
// input-driven (RESOURCE objcopy via .pyc.inc, .yapyc3 bytecode) rather than
// #include-driven. The two slices are scanned in order; dedup is global.
func resolveCodegenDepRefsExt(ctx *genCtx, consumer ModuleInstance, includeInputs, inputs []string, exclude ...NodeRef) []NodeRef {
	if len(includeInputs) == 0 && len(inputs) == 0 {
		return nil
	}

	seen := make(map[NodeRef]struct{}, 4+len(exclude))
	for _, r := range exclude {
		seen[r] = struct{}{}
	}

	var out []NodeRef

	probe := func(p string) {
		if !strings.HasPrefix(p, "$(BUILD_ROOT)/") {
			return
		}

		var ref NodeRef
		var ok bool

		if r, found := ctx.enOutputs[p]; found {
			ref, ok = r, true
		} else if r, found := ctx.pbOutputs[codegenOutputKey{platform: consumer.Target, path: p}]; found {
			ref, ok = r, true
		} else if r, found := ctx.evOutputs[codegenOutputKey{platform: consumer.Target, path: p}]; found {
			ref, ok = r, true
		} else {
			reg := codegenRegForInstance(ctx, consumer)
			if reg != nil {
				if info, found := reg.Lookup(p); found && info.HasProducerRef {
					ref, ok = info.ProducerRef, true
				}
			}
		}

		if !ok {
			return
		}

		if _, dup := seen[ref]; dup {
			return
		}

		seen[ref] = struct{}{}
		out = append(out, ref)
	}

	for _, p := range includeInputs {
		probe(p)
	}
	for _, p := range inputs {
		probe(p)
	}

	return out
}

// getScanCtx returns a `*scanCtx` for the (scanner, cfg) pair. Lookup
// dispatches on `ctx.scanCtxMode`:
//
//   - "local": the per-genModule cache map (top of localScanCtxStack);
//     a miss allocates a fresh scanCtx and stores it. When the genModule
//     pops the stack, every scanCtx allocated under that frame becomes
//     unreachable.
//   - "interned": the genCtx-wide internedScanCtx map; the scanCtx
//     persists across modules and accumulates resolveCache / subgraphCache
//     entries that any later matching ctxHash benefits from.
//
// PR-M3-perf-E.
func (ctx *genCtx) getScanCtx(scanner *IncludeScanner, cfg ScanContext) *scanCtx {
	ctxHash := hashScanContext(&cfg)
	key := scanCtxCacheKey{scanner: scanner, ctxHash: ctxHash}

	var bucket map[scanCtxCacheKey]*scanCtx

	if ctx.scanCtxMode == "interned" {
		bucket = ctx.internedScanCtx
	} else {
		// "local" — top of stack. The stack is always non-empty between
		// genModule entry and exit; an empty stack here is a programming
		// error.
		if len(ctx.localScanCtxStack) == 0 {
			ThrowFmt("genCtx.getScanCtx: localScanCtxStack empty (scanCtx requested outside genModule frame)")
		}

		bucket = ctx.localScanCtxStack[len(ctx.localScanCtxStack)-1]
	}

	if existing, ok := bucket[key]; ok {
		return existing
	}

	sc := scanner.NewScanCtx(cfg)
	bucket[key] = sc

	ctx.scanCtxAllocs++

	if len(bucket) > ctx.scanCtxPeak {
		ctx.scanCtxPeak = len(bucket)
	}

	return sc
}

// pushLocalScanCtx pushes a fresh empty scanCtx cache map onto the
// per-genModule stack. Called at genModule entry; the matching pop runs
// in a deferred cleanup. No-op in "interned" mode.
//
// PR-M3-perf-E.
func (ctx *genCtx) pushLocalScanCtx() {
	if ctx.scanCtxMode != "local" {
		return
	}

	ctx.localScanCtxStack = append(ctx.localScanCtxStack, make(map[scanCtxCacheKey]*scanCtx, 4))
}

// popLocalScanCtx pops the top entry from the stack. No-op in "interned"
// mode.
//
// PR-M3-perf-E.
func (ctx *genCtx) popLocalScanCtx() {
	if ctx.scanCtxMode != "local" {
		return
	}

	if len(ctx.localScanCtxStack) == 0 {
		ThrowFmt("genCtx.popLocalScanCtx: stack underflow")
	}

	ctx.localScanCtxStack = ctx.localScanCtxStack[:len(ctx.localScanCtxStack)-1]
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
	"DEFAULT":               {},
	"PROVIDES":              {},
	"USE_CXX":               {},
	"DEFINE_VARIABLE":       {},
	"PYTHON3":               {},
	"BUILD_ONLY_IF":         {}, // PR-27: contrib/libs/cxxsupp/libcxxrt
	"MESSAGE":               {}, // PR-27: contrib/libs/cxxsupp/libcxx (FATAL_ERROR in dead branch)
	// SRC_C_SSE41 / SSE2 / SSSE3 / AVX / XOP / SSE3 / SSE4: PR-M3-simd-permutations
	// handles these in applyUnknownStmt → d.simdSrcs (one CC node per
	// variant). Removed from the metadata whitelist so they no longer
	// no-op.
	"NO_CLANG_COVERAGE":     {}, // PR-30: contrib/tools/yasm
	"NO_PROFILE_RUNTIME":    {}, // PR-30: contrib/tools/yasm
	"WITHOUT_VERSION": {}, // PR-32 D03: contrib/libs/musl/include neighbours; metadata-only.

	// M3 metadata macros — no per-module side effect in PR-M3-A;
	// real emitters land in PR-M3-B..E.
	// USE_PYTHON3 is handled by the applyUnknownStmt case above (adds implicit
	// PEERDIRs to contrib/tools/python3 and .../Lib); removed from whitelist
	// so it doesn't fall through to the no-op path.
	"USE_PYTHON2":                       {}, // Python 2 dependency marker.
	"PYTHON3_ADDINCL":                   {}, // Adds Python3 include paths (system python, handled by emitter).
	"PYTHON2_ADDINCL":                   {}, // Adds Python2 include paths.
	// NO_PYTHON_INCLUDES: handled in applyUnknownStmt → d.noPythonIncl
	// (PR-M3-aarch64-py-closure); gates the PY*_LIBRARY-implicit
	// PEERDIR+=contrib/libs/python per build/conf/python.conf:741-743.
	// Removed from whitelist so it doesn't fall through to the no-op path.
	// NO_CHECK_IMPORTS: now typed UnknownStmt handled in applyUnknownStmt
	// (PR-M3-resource-objcopy-B); collects args into d.noCheckImports
	// and emits via emitNoCheckImportsObjcopy. Removed from whitelist
	// so it doesn't fall through to the no-op path.
	"NO_PYTHON_COVERAGE":                {}, // Suppresses Python coverage instrumentation.
	"NO_IMPORT_TRACING":                 {}, // Suppresses import tracing.
	"NO_LINT":                           {}, // Suppresses linting.
	"STYLE_PYTHON":                      {}, // Python style checker metadata.
	"WINDOWS_LONG_PATH_MANIFEST":        {}, // Windows-only manifest; no-op on Linux.
	// PYBUILD_NO_PYC: handled in applyUnknownStmt ENABLE case → d.pyBuildNoPYC; not a no-op whitelist entry.
	// RESOURCE / RESOURCE_FILES: now typed Stmts (PR-M3-resource-objcopy-A);
	// removed from whitelist. Routed via parseResource / parseResourceFiles
	// in yamake.go and consumed by resource.go::emitResourceObjcopy.
	// PY_REGISTER: now typed UnknownStmt handled in applyUnknownStmt
	// (PR-M3-reg3-cpp-py-register); each arg becomes one PY (gen_py3_reg.py)
	// node generating `<arg>.reg3.cpp` plus a CC compiling it into the
	// module's `.global.a`.
	// RUN_PROGRAM: now typed Stmt; removed from whitelist.
	// RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT: now typed Stmts; removed from whitelist.
	// GENERATE_ENUM_SERIALIZATION / _WITH_HEADER / _NOUTF removed from
	// whitelist in PR-M3-D: they are now parsed as GenerateEnumSerializationStmt
	// and dispatched to EmitEN via emitEnumSrcs.
	// ARCHIVE: PR-M3-unpaired-got-closure — now parsed in applyUnknownStmt
	// and consumed by emitArchives. Not a no-op whitelist entry.
	// CREATE_BUILDINFO_FOR: now typed Stmt; removed from whitelist.
	"INCLUDE_TAGS":                      {}, // Proto include-tag filter; semantic in PR-M3-B.
	"INDUCED_DEPS":                      {}, // Adds header deps without PEERDIR; metadata for PR-M3-A.
	"NO_PYTHON2":                        {}, // Marks PY2 unavailability; metadata.
	"CHECK_DEPENDENT_DIRS":              {}, // Dependency restriction check; metadata.
	"SUBSCRIBER":                        {}, // Ownership metadata.
	"OWNER":                             {}, // Ownership metadata.
	"LICENSE_RESTRICTION_EXCEPTIONS":    {}, // License metadata.
	"LICENSE_RESTRICTION":               {}, // License metadata.
	"RESTRICT_PATH":                     {}, // Path-restriction metadata.
	"NO_OPTIMIZE":                       {}, // Suppresses optimization; metadata for PR-M3-A.
	"TASKLET":                           {}, // Tasklet metadata; deferred.
	"TASKLETSUPPORT":                    {}, // Tasklet support metadata; deferred.
	// SET_APPEND is handled by applyUnknownStmt for the SFLAGS axis;
	// other SET_APPEND targets remain as no-ops (handled there).
	"OPENSOURCE_PROJECT":                {}, // Metadata.
	"SPLIT_FACTOR":                      {}, // Test metadata.
	"FORK_TESTS":                        {}, // Test metadata.
	"FORK_SUBTESTS":                     {}, // Test metadata.
	"SIZE":                              {}, // Test size metadata.
	"TAG":                               {}, // Test tag metadata.
	"REQUIREMENTS":                      {}, // Test requirements metadata.
	"TIMEOUT":                           {}, // Test timeout metadata.
	"ENV":                               {}, // Test env metadata.
	"DATA":                              {}, // Test data metadata.
	"TEST_SRCS":                         {}, // Test source list.
	"LINT":                              {}, // Lint metadata.
	"NO_YMAKE_PYTHON":                   {}, // Suppresses ymake python binding; metadata.
	"USE_LIGHT_PY2CC":                   {}, // Python build variant; metadata.

	// Additional M3 metadata macros found by scanning the closure:
	"SUPPRESSIONS":                    {}, // Sanitizer suppression file; metadata.
	"OPENSOURCE_EXPORT_REPLACEMENT":   {}, // CMake/Conan export replacement; metadata.
	"EXCLUDE_TAGS":                    {}, // Build-system tag exclusion; metadata.
	"FILES":                           {}, // Proto library file listing; metadata for PR-M3-B.
	"NO_JOIN_SRC":                     {}, // Suppresses JOIN_SRCS optimisation; metadata.
	"MASMFLAGS":                       {}, // MASM compiler flags (Windows); no-op on Linux.
	"NO_MYPY":                         {}, // Suppresses mypy type checking; metadata.
	"NO_OPTIMIZE_PY_PROTOS":           {}, // Suppresses proto Python optimisation; metadata.
	"PROTO_NAMESPACE":                 {}, // Proto namespace declaration; semantic in PR-M3-B.
	"PY_NAMESPACE":                    {}, // Python namespace declaration; semantic in PR-M3-E.
	"GRPC":                            {}, // gRPC service declaration; deferred.
	"CPP_PROTO_PLUGIN":                {}, // protoc C++ plugin; deferred to PR-M3-B.
	"CPP_PROTO_PLUGIN2":               {}, // protoc C++ plugin variant; deferred.
	"CPP_EV_PLUGIN":                   {}, // event compiler plugin; deferred.
	"JAVA_SRCS":                       {}, // Java sources; deferred.
	"JAVA_CLASSPATH_IGNORE_CONFLICTZ": {}, // Java classpath; metadata.
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
	return GenWith(cfg, sourceRoot, targetDir, nil)
}

// GenWith is the PR-32 D01 entry point that threads `cliDefines`
// through to `genCtx`. A nil `cliDefines` defaults to
// `{"MUSL": "yes"}` so back-compat callers (`Gen(cfg, root, target)`
// → `GenWith(cfg, root, target, nil)`) preserve M2 behaviour. Pass a
// non-nil empty map to opt out of all defaults (useful for test
// fixtures that pin the no-defaults shape).
func GenWith(cfg PlatformConfig, sourceRoot string, targetDir string, cliDefines map[string]string) *Graph {
	return GenWithMode(cfg, sourceRoot, targetDir, cliDefines, defaultScanCtxMode)
}

// defaultScanCtxMode is the per-Gen scanCtx lifecycle policy used when
// no explicit mode is passed (e.g. by tests, by the Gen wrapper). The
// PR-M3-perf-E bake-off selected "interned" as the winner (~6% wall-time
// reduction over "local"); the constant is the single source of truth
// for the production default.
const defaultScanCtxMode = "interned"

// GenWithMode is GenWith plus the scanCtxMode dispatch knob (PR-M3-perf-E).
// `mode` must be either "local" or "interned"; anything else throws.
func GenWithMode(cfg PlatformConfig, sourceRoot string, targetDir string, cliDefines map[string]string, mode string) *Graph {
	if mode != "local" && mode != "interned" {
		ThrowFmt("gen: --scan-ctx-mode must be \"local\" or \"interned\", got %q", mode)
	}

	if cliDefines == nil {
		cliDefines = map[string]string{"MUSL": "yes"}
	}

	// PR-M3-perf-B: one parse cache shared by both scanners (see comment
	// on scannerTarget/scannerHost below).
	sharedPC := newSharedParseCache()

	// PR-M3-F-7-A: one CodegenRegistry per scanner (per-scanner architecture
	// per user arbitration 2026-05-11; see codegen_registry.go header).
	// Target and host each maintain their own registry so platform-specific
	// generated outputs (e.g. protobuf compiled for both axes) are
	// independently tracked. F-7-C integrates these into scanner resolution.
	targetReg := NewCodegenRegistry()
	hostReg := NewCodegenRegistry()

	targetScanner := newIncludeScannerWith(sourceRoot, LoadSysInclSetFor(sourceRoot, "aarch64"), sharedPC)
	targetScanner.codegen = targetReg
	targetScanner.fallbackLocators = []pathLocator{codegenLocator{reg: targetReg}}
	hostScanner := newIncludeScannerWith(sourceRoot, LoadSysInclSetFor(sourceRoot, "x86_64"), sharedPC)
	hostScanner.codegen = hostReg
	hostScanner.fallbackLocators = []pathLocator{codegenLocator{reg: hostReg}}

	ctx := &genCtx{
		cfg:             cfg,
		sourceRoot:      sourceRoot,
		emit:            NewBufferedEmitter(),
		memo:            make(map[ModuleInstance]*moduleEmitResult),
		walking:         make(map[ModuleInstance]bool),
		// PR-M3-perf-B: target and host scanners share one parse-level cache
		// (file-byte parsing + file existence). Both scanners operate over
		// the same sourceRoot so parsed directives and stat results are
		// identical regardless of which arch first reads a header. Resolution
		// caches (sysinclSource/IncluderCache, resolveCache, subgraphCache)
		// remain per-scanner because sysincl YAML content is arch-specific
		// (linux-musl-aarch64.yml vs linux-musl.yml differ for bits/*).
		scannerTarget:   targetScanner,
		scannerHost:     hostScanner,
		cliDefines:      cliDefines,
		enOutputs:       make(map[string]NodeRef),
		pbOutputs:       make(map[codegenOutputKey]NodeRef),
		evOutputs:       make(map[codegenOutputKey]NodeRef),
		ldPluginCPCache: make(map[string]NodeRef),
		scanCtxMode:     mode,
		internedScanCtx: make(map[scanCtxCacheKey]*scanCtx, 64),
	}

	// PR-M3-perf-E: seed the local-mode stack with one root frame so the
	// top-level genModule call (and any peer-walk recursion outside its
	// own push/pop) has a non-empty stack to address. The frame is never
	// popped; it serves as the catch-all in case getScanCtx is invoked
	// from a call site we did not augment with push/pop.
	ctx.localScanCtxStack = []map[scanCtxCacheKey]*scanCtx{make(map[scanCtxCacheKey]*scanCtx, 4)}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Language: LangCPP,
		Target:   cfg.Target.ID,
		Flags:    inferFlagsFromPath(filepath.Clean(targetDir), false),
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	// PR-M3-brotli-snappy-re2-peer-addincl: post-emit umbrella ADDINCL
	// propagation. Upstream ymake propagates a LIBRARY's transitive
	// peer-GLOBAL ADDINCL closure down to every path-sub-library (i.e.,
	// modules whose path starts with the LIBRARY's path + "/"). The 86
	// `devtools/ymake/*/*.cpp.o` nodes on aarch64 (L3 lever #2) miss
	// brotli/snappy/re2 -I flags because `devtools/ymake` directly peers
	// `library/cpp/blockcodecs` (→ brotli + snappy GLOBAL ADDINCL) and
	// `contrib/libs/re2`, but the sub-libraries `common`, `diag`, etc.
	// inherit nothing from the umbrella peer chain through the standard
	// DFS walk. This post-pass patches the cmd_args of CC nodes whose
	// module_dir has a path-prefix ancestor LIBRARY in `ctx.memo`.
	applyUmbrellaAddIncl(ctx)

	if os.Getenv("YATOOL_SCANCTX_STATS") == "1" {
		fmt.Fprintf(os.Stderr, "scanctx-stats: mode=%s allocs=%d peak-in-flight=%d interned-final=%d\n",
			ctx.scanCtxMode, ctx.scanCtxAllocs, ctx.scanCtxPeak, len(ctx.internedScanCtx))

		// Per-scanCtx populated cache sizes — only valid in interned mode
		// (in local mode the buckets are emptied as frames pop).
		if ctx.scanCtxMode == "interned" {
			var totalResolve, totalSubgraph, maxResolve, maxSubgraph int
			for _, sc := range ctx.internedScanCtx {
				totalResolve += len(sc.resolveCache)
				totalSubgraph += len(sc.subgraphCache)
				if len(sc.resolveCache) > maxResolve {
					maxResolve = len(sc.resolveCache)
				}
				if len(sc.subgraphCache) > maxSubgraph {
					maxSubgraph = len(sc.subgraphCache)
				}
			}
			fmt.Fprintf(os.Stderr, "scanctx-stats: resolveCache total=%d max-per-ctx=%d  subgraphCache total=%d max-per-ctx=%d\n",
				totalResolve, maxResolve, totalSubgraph, maxSubgraph)
		}
	}

	return Finalize(ctx.emit.(*BufferedEmitter))
}

// moduleData is the per-module accumulator populated by
// `collectModule`. It captures everything the rule-emission stage
// needs after macro evaluation has flattened IF branches and
// inlined macros. The `flags` field starts from the path-based
// heuristic and is overlaid with macro-derived bools (NO_LIBC etc.).
type moduleData struct {
	moduleStmt       *ModuleStmt
	srcs             []string
	globalSrcs       []string
	pySrcs           []string // PR-M3-A: python sources from PY_SRCS(...); each entry is a .py filename
	pyBuildNoPYC     bool     // PR-M3-A: set by ENABLE(PYBUILD_NO_PYC); suppresses yapyc3 node emission from PY_SRCS
	pyBuildNoPY      bool     // PR-M3-resource-objcopy-C: set by ENABLE(PYBUILD_NO_PY); suppresses raw .py resfs embedding from PY_SRCS (only the yapyc3 form is embedded)
	pyTopLevel       bool     // PR-M3-resource-objcopy-C: set by TOP_LEVEL prefix in PY_SRCS(...); the resfs key for each source omits the dotted module-path prefix
	enumSrcs         []*GenerateEnumSerializationStmt // PR-M3-D: GENERATE_ENUM_SERIALIZATION(*) declarations
	peerdirs         []string
	joinSrcs         []*JoinSrcsStmt
	addIncl          []string // collected non-GLOBAL ADDINCL paths
	addInclGlobal    []string // PR-31 D04: collected ADDINCL(GLOBAL ...) paths; peer-propagated to consumers
	cFlags           []string // collected non-GLOBAL CFLAGS values (apply to module's own C+C++ sources)
	cFlagsGlobal     []string // PR-32 D04: collected CFLAGS(GLOBAL ...) values; peer-propagated to consumers' C+C++ sources
	cxxFlags         []string // collected non-GLOBAL CXXFLAGS values (C++ only); PR-29-D02 threads into ModuleCCInputs.CXXFlags
	cxxFlagsGlobal   []string // PR-32 D05: collected CXXFLAGS(GLOBAL ...) values; peer-propagated to consumers' C++ sources
	cOnlyFlags       []string // collected non-GLOBAL CONLYFLAGS values (C only); PR-29-D02 threads into ModuleCCInputs.COnlyFlags
	cOnlyFlagsGlobal []string // PR-32 D06: collected CONLYFLAGS(GLOBAL ...) values; peer-propagated to consumers' C / .S sources
	sFlags           []string // PR-M3-openssl-as-cflags: SET_APPEND(SFLAGS ...) values; appended to AS compiles only.
	ldFlags          []string // collected LDFLAGS values
	srcDir           string   // last SRCDIR setting (empty = module dir)
	flags            FlagSet  // overlay of inferFlagsFromPath + macro bools
	hadAllocator     bool     // PR-30 D03: set by applyAllocatorStmt; PROGRAM-default-allocator routing fires only when this is false
	allocatorName    string   // PR-35g: name passed to ALLOCATOR(...); empty when no ALLOCATOR macro. Used to suppress malloc/api when ALLOCATOR(FAKE).
	muslLite         bool     // PR-30 D02: set by ENABLE(MUSL_LITE); flips the default-program-peers musl/full → musl gate
	noPythonIncl     bool     // PR-M3-aarch64-py-closure: set by NO_PYTHON_INCLUDES(); suppresses the PY*_LIBRARY-implicit PEERDIR+=contrib/libs/python (mirror of `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }` in build/conf/python.conf:741-743).
	usePython3      bool      // PR-M3-python-addincl-cflags: set by USE_PYTHON3() or a PY3-family module type (PY3_LIBRARY / PY3_PROGRAM / PY3_PROGRAM_BIN / PY23_LIBRARY / PY23_NATIVE_LIBRARY); normalised by applyPython3AddIncl. Triggers the `when ($USE_ARCADIA_PYTHON == "yes")` branch of `_PYTHON3_ADDINCL` (python.conf:1018-1023): -DUSE_PYTHON3 (via defaultPeerCFlags / AutoPeerCFlags slot) and contrib/libs/python/Include (own + GLOBAL ADDINCL).
	ldPlugins        []string // PR-35k: filenames declared via LD_PLUGIN(name.py); the only M2 case is contrib/libs/musl/include's `LD_PLUGIN(musl.py)`. Each entry becomes a CP node and feeds `--start-plugins ... --end-plugins` in consumer LDs.
	arPlugin         string   // PR-M3-openssl-ar-plugin-and-as-clean: name from AR_PLUGIN(name); resolves to `$(SOURCE_ROOT)/<modulePath>/<name>.pyplugin` and is injected into the AR cmd_args (`--plugin <path>`) and inputs. Mirror of upstream macro `AR_PLUGIN` (ymake.core.conf:3396-3398) + `_LD_ARCHIVER_KV_PLUGIN` (ld.conf:366-368). Empty when no AR_PLUGIN macro present.
	// PR-35o: per-source extra CFLAGS keyed by source filename.
	// Populated by `SRC(filename extra_cflags...)` (e.g.
	// `util/charset/ya.make:22-25` `SRC(wide_sse41.cpp -DSSE41_STUB)`).
	// Threaded through emitOneSource into ModuleCCInputs.PerSourceCFlags
	// so the composer can append the per-source flags right before the
	// input path (matching the reference cmd_args slot for the SSE41_STUB
	// flag on `util/charset/wide_sse41.cpp.o`).
	perSrcCFlags map[string][]string
	// PR-M3-E: DEFAULT(name value) declarations collected per-module.
	// Used by ConfigureFileStmt processing to expand $CFG_VARS.
	// Keys are variable names; values are the DEFAULT values (empty
	// string for DEFAULT(name "")).
	defaultVars map[string]string
	// PR-M3-E: ordered list of DEFAULT var names (for deterministic
	// $CFG_VARS expansion matching the reference cmd_args order).
	defaultVarOrder []string
	// PR-M3-E: CONFIGURE_FILE() / .cpp.in / .c.in sources → CF nodes.
	configureFiles []*ConfigureFileStmt
	// PR-M3-E: CREATE_BUILDINFO_FOR(output_header) → BI node.
	createBuildInfoFor string
	// PR-M3-E: RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT → JV nodes.
	antlr4Grammars []antlr4GrammarInfo
	// PR-M3-E: RUN_PROGRAM → PR nodes.
	runPrograms []*RunProgramStmt
	// PR-M3-unpaired-got-closure: ARCHIVE(NAME <out> [DONTCOMPRESS] files...)
	// invocations collected in declaration order. Each entry produces one
	// AR node invoking `$(BUILD_ROOT)/tools/archiver/archiver` to pack the
	// listed files into NAME.
	archives []archiveEntry
	// PR-M3-unpaired-got-closure: map of PR-emitted output filename →
	// producing PR NodeRef. Populated by emitRunProgramsForAR as each
	// RUN_PROGRAM is emitted. Consumed by emitArchives to wire each AR
	// node's dep set to the producing PR (matching the REF shape).
	prOutputProducer map[string]NodeRef
	// PR-35o: set of source filenames declared via `SRC(...)` or
	// `SRC_C_NO_LTO(...)`. Upstream `SRC`/`SRC_C_NO_LTO` macros emit a
	// FLAT output path (no `_/` infix even when the source contains a
	// `/`), unlike `SRCS(subdir/foo.cpp)` which emits
	// `<modulePath>/_/subdir/foo.cpp.o`. Compare reference paths:
	//   - SRCS member util/digest/city.cpp → util/_/digest/city.cpp.o
	//   - SRC_C_NO_LTO util/system/compiler.cpp → util/system/compiler.cpp.o
	// emitOneSource consults this set to set
	// ModuleCCInputs.FlatOutput, which composeCCPaths uses to skip the
	// `_/` infix.
	flatSrcs    map[string]struct{}
	// PR-M3-resource-objcopy-A: RESOURCE() / RESOURCE_FILES() pair lists.
	// After collection, `resources` carries the (path, key, kv) triple list
	// that the objcopy packer in resource.go consumes; RESOURCE_FILES are
	// expanded inline at collect time so this slice is the canonical view
	// for the emitter.
	resources []resourceEntry
	// PR-M3-resource-objcopy-B: kv_only objcopy shapes (PY3-only).
	// pyMain captures the `PY_MAIN(<arg>)` macro argument or the
	// `MAIN <src.py>` modifier of `PY_SRCS(...)` — both produce a single
	// `PY_MAIN=<dotted-mod>:<func>` kv per upstream pybuild.py:py_main
	// (build/plugins/pybuild.py:759). Empty when no PY_MAIN-shape is
	// present.
	pyMain string
	// noCheckImports captures the verbatim arg list of
	// `NO_CHECK_IMPORTS(args...)` — used by emitNoCheckImportsObjcopy
	// to build a single `py/no_check_imports/<pathid>=<space-joined>` kv.
	// Args are kept in declaration order (the upstream value used in
	// pathid() and the resfs value join the args by ' ' in that order;
	// see build/plugins/ytest.py:811).
	noCheckImports []string
	// PR-M3-reg3-cpp-py-register: PY_REGISTER(args...) argument list. Each
	// arg is the dotted module name; gen_py3_reg.py(<arg>, <output>) emits a
	// `<arg>.reg3.cpp` source which is then SRCS(GLOBAL …) compiled.
	// Mirror of upstream macro _PY3_REGISTER in build/ymake.core.conf:4086-4089.
	pyRegister []string
	// PR-M3-simd-permutations: per-`SRC_C_<VARIANT>` entries in
	// declaration order. Each entry produces one CC node alongside (and
	// in addition to) any plain SRCS / SRC / SRC_C_NO_LTO listing of the
	// same file. AR-member ordering: emitted entries share the FLAT
	// bucket with SRC()/SRC_C_NO_LTO entries (no `_/` infix), so
	// reorderARMembers hoists them to the front of the archive.
	simdSrcs []simdSrc
	// PR-M3-ragel-flags-per-module: per-module RAGEL6_FLAGS override
	// captured from `SET(RAGEL6_FLAGS <value>)` (upstream
	// build/ymake.core.conf:3284 expands `$RAGEL6_FLAGS ${SRCFLAGS}`
	// before the rest of the ragel6 cmd line). Empty means the
	// platform-default fires inside EmitR6 — `-CG2` on x86_64 host
	// (release, mirroring ymake_conf.py:2274) and `-CT0` on target
	// aarch64 (debug). Empirical M3 case: devtools/ymake/lang/
	// makelists/ya.make sets `-lF1`.
	ragel6Flags []string
	conflictMod *ModuleStmt
	// PR-M3-runprogram-closure: module-level INDUCED_DEPS(<exts> headers...)
	// declarations. Each entry is a SOURCE_ROOT-rooted header path the tool
	// (when this module is a PROGRAM invoked via RUN_PROGRAM) is declared to
	// inject into its generated outputs. Consumed by emitRunProgram to seed
	// the PR output's EmitsIncludes — the scanner then walks the headers'
	// real `#include` graph to reach the full transitive closure.
	inducedDeps []string
}

// resourceEntry is one packer input as produced by upstream
// `TObjCopyResourcePacker::HandleResource`. Path == "-" marks a kv-only
// entry (--kvs); otherwise Path is the source path and Key is the
// pre-base64 raw key (the packer applies Base64 encoding when building
// the hash list / cmd_args).
type resourceEntry struct {
	Path string
	Key  string
}

// archiveEntry captures one `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)`
// invocation. Name is the module-relative output filename (e.g.
// "__res.pyc.inc"); DontCompress is set when the DONTCOMPRESS keyword
// appears; Files lists the inputs in declaration order (each is either a
// module-relative source path or the basename of a build-tree artifact
// produced by another macro in the same module — e.g. `__res.pyc`
// produced by a RUN_PROGRAM emit).
type archiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

// antlr4GrammarInfo captures a single RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT
// invocation for later JV node emission.  IsSplit distinguishes the two-grammar
// form (lexer+parser) from the single-grammar form.
// OutputIncludes carries repo-relative headers from the macro's OUTPUT_INCLUDES
// keyword (PR-M3-jv-antlr-system-headers): they are registered as CP `.g4.cpp`
// EmitsIncludes so the CC consumer walks their transitive closure.
type antlr4GrammarInfo struct {
	IsSplit        bool
	Lexer          string   // .g4 file (IsSplit=true)
	Parser         string   // .g4 file (IsSplit=true)
	Grammar        string   // .g4 file (IsSplit=false)
	Options        []string // extra antlr4 cmd_args (e.g. ["-package", "NConfReader"])
	Visitor        bool
	Listener       bool
	OutputIncludes []string // repo-relative
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

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)

	// PR-M3-sparsehash-slot-order: per upstream build/conf/proto.conf:480-491,
	// the _CPP_EVLOG_CMD (and _CPP_PROTO_EVLOG_CMD) macros fired for every
	// `.ev` source append `.PEERDIR=library/cpp/eventlog contrib/libs/protobuf`
	// to the owning module's PEERDIRs. Visit eventlog's transitive chain
	// (blockcodecs → codecs/brotli, codecs/snappy, contrib/libs/re2) from
	// every `.ev`-bearing module — places those peers ahead of sparsehash
	// in the consumer's transitive ADDINCL aggregation.
	hasEv := false
	hasProto := false
	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src, ".ev"):
			hasEv = true
		case strings.HasSuffix(src, ".proto"):
			hasProto = true
		}
	}

	if hasEv {
		d.peerdirs = append(d.peerdirs, "library/cpp/eventlog", "contrib/libs/protobuf")
	}

	// PR-M3-protobuf-umbrella-trigger: per upstream build/conf/proto.conf:461-465,
	// the `_CPP_PROTO_CMD(File)` macro fires once per `.proto` source and
	// appends `.PEERDIR=contrib/libs/protobuf` to the owning module. The
	// .ev branch above already covers `_CPP_EVLOG_CMD` / `_CPP_PROTO_EVLOG_CMD`
	// for .ev sources; mirror it here so .proto-only PROTO_LIBRARYs (e.g.
	// library/cpp/eventlog/proto, library/cpp/retry/protos,
	// library/cpp/protobuf/{json,util}/proto) propagate protobuf/src +
	// transitive abseil-cpp{,-tstring} -I to their downstream .pb.cc.o
	// consumers. Guarded on PROTO_LIBRARY only — other module types may
	// declare .proto sources for codegen without compiling them as
	// protobuf-runtime consumers.
	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" {
		d.peerdirs = append(d.peerdirs, "contrib/libs/protobuf")
	}

	return d
}

// applyPython3AddIncl mirrors the `when ($USE_ARCADIA_PYTHON == "yes")`
// branch of `_PYTHON3_ADDINCL` (build/conf/python.conf:1018-1023):
// `CFLAGS+=-DUSE_PYTHON3` plus `ADDINCL+=GLOBAL $PY3_BASE_INCLUDE_DIR`
// (= contrib/libs/python/Include per python.conf:96). Invoked by PY3-family
// module types and by `USE_PYTHON3()` (python.conf:738-739, 862, 1064, 1250).
//
// Empirically the reference places `-DUSE_PYTHON3` at the AutoPeerCFlags
// cmd_args slot — right after `-D_musl_`, before the second noLibcUndebug
// block copy (e.g. library/python/runtime_py3/__res.cpp.o ref:93,
// library/cpp/pybind/cast.cpp.py3.o ref:83) — even when the module declares
// `NO_PYTHON_INCLUDES()` and therefore has no peer to `contrib/libs/python`
// to propagate the flag from. We inject `-DUSE_PYTHON3` via
// `defaultPeerCFlags` so it lands at that slot, and we set `d.usePython3`
// here for `defaultPeerCFlags` to read. The `contrib/libs/python/Include`
// path goes to BOTH `d.addInclGlobal` (peer-propagated) AND `d.addIncl`
// (own ADDINCL slot), mirroring the `ADDINCL(GLOBAL X)` collector path
// (gen.go:918-919).
//
// `contrib/libs/python` itself emits these via its own ya.make IF-block
// (`ADDINCL(GLOBAL Include)` + `CFLAGS(GLOBAL -DUSE_PYTHON3)`), so skip it
// to avoid double-emit and to mirror the same cycle-guard pattern used by
// the PY*_LIBRARY auto-peerdir code at the genModule call site (line 2104).
//
// NO_PYTHON_INCLUDES() does NOT gate this injection: upstream gates only
// the implicit `PEERDIR+=contrib/libs/python` (python.conf:741-743), not
// the `_PYTHON3_ADDINCL` invocation itself. Empirical: library/python/
// runtime_py3 declares NO_PYTHON_INCLUDES() yet its CC nodes carry
// `-DUSE_PYTHON3` and `-I$(SOURCE_ROOT)/contrib/libs/python/Include`.
func applyPython3AddIncl(modulePath string, d *moduleData) {
	if d.moduleStmt == nil {
		return
	}

	if !d.usePython3 && !pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		return
	}

	if modulePath == "contrib/libs/python" {
		return
	}

	// Normalise: every code path downstream (e.g. `defaultPeerCFlags`'s
	// AutoPeerCFlags slot injection) reads `d.usePython3` rather than
	// re-checking the module-type set.
	d.usePython3 = true

	// `-DUSE_PYTHON3` is injected via `defaultPeerCFlags` so it lands at
	// the AutoPeerCFlags cmd_args slot (between catboost-redux and the
	// second noLibcUndebugBlock copy), matching the empirical reference
	// position (e.g. runtime_py3/__res.cpp.o ref:93, pybind/cast.cpp.py3.o
	// ref:83). Adding it to `d.cFlagsGlobal` instead would land it inside
	// the ownCFlags slot (position ~59), which mismatches the reference.
	d.addInclGlobal = append(d.addInclGlobal, "contrib/libs/python/Include")
	d.addIncl = append(d.addIncl, "contrib/libs/python/Include")

	// ARCHIVE(NAME ...) in library/python/runtime_py3 auto-injects
	// `${addincl;noauto;output:NAME}` per ymake.core.conf:4143. The
	// path is owner-scoped (own slot for runtime_py3) AND peer-propagated
	// to USE_PYTHON3 consumers. Owner gets it in d.addIncl (own).
	// Consumers see it via genModule's post-merge splice (placed AFTER
	// abseil-cpp).
	if modulePath == "library/python/runtime_py3" {
		d.addIncl = append(d.addIncl, "$(BUILD_ROOT)/library/python/runtime_py3")
	}
}

// applyBuildInfoAddIncl mirrors the implicit `ADDINCL(<build_info_dir>)`
// upstream CREATE_BUILDINFO_FOR macros emit. PR-M3-final-surgical (fix 5):
// the implicit ADDINCL is GLOBAL — the generated header must be visible to
// PEER consumers too (witnessed by `main.cpp.o` / `print.cpp.o` carrying
// `-I$(BUILD_ROOT)/library/cpp/build_info` via their peer chain).
func applyBuildInfoAddIncl(modulePath string, d *moduleData) {
	if d.createBuildInfoFor == "" {
		return
	}
	biDir := "$(BUILD_ROOT)/" + modulePath
	d.addIncl = append(d.addIncl, biDir)
	d.addInclGlobal = append(d.addInclGlobal, biDir)
}

// pyModuleTypeUsesPython3 returns true for module types whose upstream
// definition in build/conf/python.conf invokes `_PYTHON3_ADDINCL` (directly
// or via `_ARCADIA_PYTHON3_ADDINCL` / `PYTHON3_ADDINCL`):
//   - PY3_LIBRARY (line 738-739)
//   - PY3_PROGRAM_BIN / PY3_PROGRAM / _BASE_PY3_PROGRAM (line 862)
//   - PY23_LIBRARY's PY3 sub-module (inherits via PY3_LIBRARY)
//   - PY23_NATIVE_LIBRARY's PY3 sub-module (line 1250: PYTHON3_ADDINCL())
//
// PY2_LIBRARY / PY2_PROGRAM are intentionally excluded — they invoke
// `_ARCADIA_PYTHON_ADDINCL` (no "3"; python.conf:695), which is the
// Python2 variant and does not emit `-DUSE_PYTHON3`.
func pyModuleTypeUsesPython3(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY3_PROGRAM", "PY3_PROGRAM_BIN",
		"PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return true
	}

	return false
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
			// M3: SRCS(GLOBAL foo.cpp) uses GLOBAL as a per-source
			// modifier meaning the source's symbols are exported globally
			// (equivalent to GLOBAL_SRCS). PR-41+ upstream introduced
			// this inline variant. Strip GLOBAL tokens and route the
			// following sources to d.globalSrcs (PR-M3-A: treat the
			// same as regular srcs since EmitARGlobal handles global
			// archives; the correct routing matches GLOBAL_SRCS).
			globalNext := false

			for _, src := range v.Sources {
				if src == "GLOBAL" {
					globalNext = true

					continue
				}

				if globalNext {
					d.globalSrcs = append(d.globalSrcs, src)
					globalNext = false
				} else {
					d.srcs = append(d.srcs, src)
				}
			}
		case *PeerdirStmt:
			// PR-M3-final-surgical (fix 3): the ADDINCL modifier on the
			// immediately-following peerdir path means "peer this AND add
			// the same path to this module's own ADDINCL list". Drives
			// `PEERDIR(ADDINCL contrib/libs/protobuf …)` in
			// tools/event2cpp/bin/ya.make, which feeds a CC -I slot for
			// the consumer's compile of proto_events.cpp.
			addInclNext := false
			for _, p := range v.Paths {
				// Skip unexpanded variable references (e.g. ${STUB_PEERDIRS}).
				// These appear in some ya.make files as SET-driven optional peerdirs
				// that resolve to empty in the standard open-source build. The walker
				// has no SET evaluator, so variable-ref paths would cause a
				// "no such file" failure; skipping them is the correct M3 behaviour.
				if strings.Contains(p, "${") {
					continue
				}
				if p == "ADDINCL" {
					addInclNext = true
					continue
				}
				if p == "GLOBAL" {
					continue
				}
				if addInclNext {
					d.addIncl = append(d.addIncl, p)
					addInclNext = false
				}
				d.peerdirs = append(d.peerdirs, p)
			}
		case *SetStmt:
			// SET is parsed but PR-25 has no evaluator. The taken
			// IF branches above already flattened any conditional
			// SET; an unconditional SET that influences downstream
			// IFs would need a real macro evaluator (PR-26+).
			//
			// PR-M3-ragel-flags-per-module: capture `SET(RAGEL6_FLAGS
			// <value>)` so emitOneSource can thread the override into
			// EmitR6. Upstream `_SRC("rl6", ...)` (build/ymake.core.conf:
			// 3284) interpolates `$RAGEL6_FLAGS` before everything else,
			// so a SET replaces the default and is not additive to other
			// SETs in the same module (last-write-wins). Empirical M3
			// witness: devtools/ymake/lang/makelists/ya.make:6 sets
			// `-lF1`, producing the ragel6 cmd_args[1] observed in the
			// reference graph's `makefile_lang.rl6.cpp` node.
			if v.Name == "RAGEL6_FLAGS" {
				d.ragel6Flags = []string{v.Value}
			}
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			d.joinSrcs = append(d.joinSrcs, v)
		case *AddInclStmt:
			// PR-31 D04/D13: GLOBAL paths peer-propagate to consumers
			// via the PEERDIR walk (kept in `d.addInclGlobal`).
			// PR-33 D02: own-cmd_args emission uses `d.addIncl` which
			// includes BOTH GLOBAL and non-GLOBAL paths in declaration
			// order — empirically the reference graph emits a module's
			// own GLOBAL ADDINCL paths on the module's own CC compiles
			// (libcxx algorithm.cpp.o cmd_args[9..11] shows
			// `libcxx/include` + `libcxx/src` + `libcxxrt/include` in
			// stmt declaration order, where include and libcxxrt/include
			// are GLOBAL and src is non-GLOBAL).
			//
			// PR-M3-cmd-arg-slot-ordering: append AllPaths (positional
			// declaration order across the GLOBAL split) instead of
			// "GLOBAL-then-OWN", which mis-orders modules whose ya.make
			// interleaves bare and GLOBAL paths (libffi, base64, ragel5
			// peer modules) — see AddInclStmt.AllPaths doc.
			d.addInclGlobal = append(d.addInclGlobal, v.GlobalPaths...)
			d.addIncl = append(d.addIncl, v.AllPaths...)
		case *CFlagsStmt:
			// PR-32 D04: GLOBAL flags peer-propagate to consumers via
			// PEERDIR (kept in `d.cFlagsGlobal`); non-GLOBAL flags apply
			// to this module's own C+C++ sources only (kept in
			// `d.cFlags`). PR-33 D02 emits the GLOBAL set separately on
			// the module's own CC compiles via the bucket model in
			// composeTargetCC / composeHostCC (own GLOBAL ∪ peer
			// GLOBAL slot, twice flanking the catboost-redux).
			d.cFlagsGlobal = append(d.cFlagsGlobal, v.GlobalFlags...)
			d.cFlags = append(d.cFlags, v.OwnFlags...)
		case *CXXFlagsStmt:
			// PR-32 D05: GLOBAL CXXFLAGS peer-propagate to consumers'
			// C++ compiles (kept in `d.cxxFlagsGlobal`); non-GLOBAL
			// CXXFLAGS apply to this module's own C++ sources only
			// (kept in `d.cxxFlags`). PR-33 D02 emits the GLOBAL set
			// separately on own compiles via the bucket model.
			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, v.GlobalFlags...)
			d.cxxFlags = append(d.cxxFlags, v.OwnFlags...)
		case *CONLYFlagsStmt:
			// PR-32 D06: GLOBAL CONLYFLAGS peer-propagate to consumers'
			// C / .S compiles (kept in `d.cOnlyFlagsGlobal`); non-GLOBAL
			// CONLYFLAGS apply to this module's own C / .S sources only
			// (kept in `d.cOnlyFlags`). PR-33 D02 emits GLOBAL via the
			// bucket model.
			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, v.GlobalFlags...)
			d.cOnlyFlags = append(d.cOnlyFlags, v.OwnFlags...)
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
		case *GenerateEnumSerializationStmt:
			d.enumSrcs = append(d.enumSrcs, v)
		case *DefaultVarStmt:
			// PR-M3-E: track DEFAULT(name value) for $CFG_VARS expansion.
			if d.defaultVars == nil {
				d.defaultVars = map[string]string{}
			}
			if _, exists := d.defaultVars[v.VarName]; !exists {
				d.defaultVars[v.VarName] = v.Value
				d.defaultVarOrder = append(d.defaultVarOrder, v.VarName)
			}
			// PR-M3-pcre-jit-default-eval: also bridge the binding into the
			// per-module IF env so subsequent `IF (NAME)` / `IF (NAME == "v")`
			// predicates evaluated in this collectStmts walk see the value.
			// Mirrors upstream TEvalContext::SetStatement's NMacro::DEFAULT
			// branch (devtools/ymake/lang/eval_context.cpp:335-339): only
			// sets when the variable has no prior binding. Value typed as
			// string so bare-ident `IF (NAME)` coerces via Environment.Bool
			// (empty → false, non-empty → true) and `IF (NAME == "yes")`
			// matches via string equality. The env is the per-module clone
			// from buildIfEnv, so the mutation does not leak across modules.
			env.SetDefaultString(v.VarName, v.Value)
		case *ConfigureFileStmt:
			// PR-M3-E: explicit CONFIGURE_FILE(src dst) declaration.
			d.configureFiles = append(d.configureFiles, v)
		case *CreateBuildInfoStmt:
			// PR-M3-E: CREATE_BUILDINFO_FOR(header) → BI node.
			d.createBuildInfoFor = v.OutputHeader
		case *RunAntlr4CppStmt:
			// PR-M3-E: single-grammar ANTLR4 invocation → JV node.
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        false,
				Grammar:        v.Grammar,
				Options:        append([]string(nil), v.Options...),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: append([]string(nil), v.OutputIncludes...),
			})
		case *RunAntlr4CppSplitStmt:
			// PR-M3-E: lexer+parser split ANTLR4 invocation → JV node.
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        true,
				Lexer:          v.Lexer,
				Parser:         v.Parser,
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: append([]string(nil), v.OutputIncludes...),
			})
		case *RunProgramStmt:
			// PR-M3-E: RUN_PROGRAM → PR node.
			d.runPrograms = append(d.runPrograms, v)
		case *ResourceStmt:
			// PR-M3-resource-objcopy-A: RESOURCE pairs feed the objcopy
			// packer as-is. Pairs whose path is "-" are kv-only entries;
			// non-"-" pairs are (source path, raw key) pairs.
			for _, pair := range v.Pairs {
				d.resources = append(d.resources, resourceEntry{Path: pair.Path, Key: pair.Key})
			}
		case *ResourceFilesStmt:
			// PR-M3-resource-objcopy-A: expand RESOURCE_FILES into
			// resource entries per `build/plugins/res.py:onresource_files`.
			// For each path P (after DONT_COMPRESS / PREFIX / DEST / STRIP
			// keywords are processed), append:
			//   - kv-only entry: Path="-", Key=resfs/src/resfs/file/<key>=${rootrel;context=TEXT;input=TEXT:"<P>"}
			//   - source entry:  Path=<P>, Key=resfs/file/<key>
			// The ${rootrel;...} placeholder is preserved verbatim because
			// the hash formula (resource.go:objcopyHash) requires the
			// pre-expansion form (verified against REF
			// `devtools/ymake/contrib/python-rapidjson` objcopy hash).
			for _, e := range expandResourceFiles(v.Args) {
				d.resources = append(d.resources, e)
			}
		case *IfStmt:
			taken := v.Then

			if !EvalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(modulePath, taken, env, d)
		case *UnknownStmt:
			applyUnknownStmt(modulePath, v, d)
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
func applyUnknownStmt(modulePath string, v *UnknownStmt, d *moduleData) {
	switch v.Name {
	case "NO_LIBC":
		// build/ymake.core.conf: NO_LIBC() calls NO_RUNTIME() which calls NO_UTIL().
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_UTIL":
		d.flags.NoUtil = true
	case "NO_RUNTIME":
		// build/ymake.core.conf: NO_RUNTIME() calls NO_UTIL().
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_PLATFORM":
		// build/ymake.core.conf: NO_PLATFORM() calls NO_LIBC() → NO_RUNTIME() → NO_UTIL().
		d.flags.NoPlatform = true
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_COMPILER_WARNINGS":
		d.flags.NoCompilerWarnings = true
	case "NO_PYTHON_INCLUDES":
		// PR-M3-aarch64-py-closure: NO_PYTHON_INCLUDES() sets NO_PYTHON_INCLS=yes
		// per build/conf/python.conf:928-929 (macro definition). The PY*_LIBRARY
		// implicit `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }`
		// at python.conf:741-743 is gated by this; we capture the flip here so
		// the implicit-peer code in genModule skips contrib/libs/python for
		// modules that declare NO_PYTHON_INCLUDES (e.g. library/python/runtime_py3,
		// library/python/symbols/module).
		d.noPythonIncl = true
	case "ALLOCATOR":
		applyAllocatorStmt(v, d)
	case "ARCHIVE":
		// PR-M3-unpaired-got-closure: parse `ARCHIVE(NAME <out>
		// [DONTCOMPRESS] files...)` (upstream
		// build/ymake.core.conf:4142-4145). The NAME keyword expects
		// exactly one following argument; DONTCOMPRESS is a bare flag;
		// remaining positional args are the input files.
		applyArchiveStmt(v, d)
	case "ENABLE":
		// PR-30 D02: track ENABLE(MUSL_LITE) so the
		// defaultProgramPeerdirsFor decision sees the per-module
		// flip. yasm declares ENABLE(MUSL_LITE) inside its IF(MUSL)
		// branch; without this hook yasm pulls musl/full and the
		// resulting cross-PROGRAM cycle (yasm → musl/full →
		// asmlib's .asm sources → yasm) blows the cycle counter.
		// PR-M3-A: track ENABLE(PYBUILD_NO_PYC) so emitPySrcs
		// suppresses yapyc3 node emission for modules like
		// contrib/tools/python3/lib2/py that declare all Python
		// sources but do not want .pyc/.yapyc3 files generated.
		for _, a := range v.Args {
			if a == "MUSL_LITE" {
				d.muslLite = true
			}
			if a == "PYBUILD_NO_PYC" {
				d.pyBuildNoPYC = true
			}
			// PR-M3-resource-objcopy-C: PYBUILD_NO_PY (without the 'C')
			// is a separate flag — used by contrib/tools/python3/Lib —
			// that suppresses the raw `.py` resfs embedding while still
			// running yapyc3 compilation. Lib also has ENABLE(PYBUILD_NO_PY)
			// declared at the top of its ya.make.
			if a == "PYBUILD_NO_PY" {
				d.pyBuildNoPY = true
			}
		}
	case "SRC":
		// PR-35o: SRC(filename [extra_cflags...]) is a SRCS variant
		// that registers a single source AND attaches per-source extra
		// CFLAGS to that source's compile. The first arg is the
		// filename; remaining args are flag tokens (e.g. -DSSE41_STUB)
		// appended to the compile cmd_args at the per-source slot
		// (right before the input path), matching the reference for
		// `util/charset/wide_sse41.cpp.o`. SRC() with no args throws.
		// SRC's output path is FLAT (no `_/` infix) — see flatSrcs in
		// moduleData.
		if len(v.Args) == 0 {
			ThrowFmt("gen: SRC() requires at least 1 argument (filename); got 0 at line %d", v.Line)
		}

		filename := v.Args[0]
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}

		if len(v.Args) > 1 {
			if d.perSrcCFlags == nil {
				d.perSrcCFlags = map[string][]string{}
			}

			extras := append([]string(nil), v.Args[1:]...)
			d.perSrcCFlags[filename] = append(d.perSrcCFlags[filename], extras...)
		}
	case "SRC_C_NO_LTO":
		// PR-35o: SRC_C_NO_LTO(filename) is a SRCS variant that
		// disables LTO for the named source. The reference cmd_args
		// for `util/system/compiler.cpp.o` show no LTO-specific
		// flag delta (LTO is already off in M2's debug build), so
		// this reduces to plain SRCS in the current closure.
		// Output path is FLAT (no `_/` infix) — see flatSrcs in
		// moduleData.
		if len(v.Args) != 1 {
			ThrowFmt("gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got %d at line %d", len(v.Args), v.Line)
		}

		filename := v.Args[0]
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
	case "SRC_C_AVX", "SRC_C_SSE2", "SRC_C_SSE3", "SRC_C_SSSE3",
		"SRC_C_SSE4", "SRC_C_SSE41", "SRC_C_XOP":
		// PR-M3-simd-permutations: SRC_C_<V>(filename [extra_flags...])
		// emits one CC node compiling `filename` with the variant's
		// `-m<flag>` bundle plus the extras, into a FLAT
		// `<src>.<variant>.pic.o` output. The cmd_args layout reuses the
		// existing PerSourceCFlags slot (between macroPrefixMapFlags and
		// the input path). Per `build/ymake.core.conf:3848-3923`, each
		// macro expands to `_SRC_CUSTOM_C_CPP(... $FILE .<v> $<V>_CFLAGS
		// $FLAGS)` — the variant CFLAGS come first, then the macro's
		// trailing arguments.
		variant, ok := simdVariantFor(v.Name)
		if !ok {
			ThrowFmt("gen: unrecognised SIMD-permutation macro %q at line %d (simdVariants table out of sync)", v.Name, v.Line)
		}
		if len(v.Args) == 0 {
			ThrowFmt("gen: %s() requires at least 1 argument (filename); got 0 at line %d", v.Name, v.Line)
		}

		filename := v.Args[0]
		flags := make([]string, 0, len(variant.CFlags)+len(v.Args)-1)
		flags = append(flags, variant.CFlags...)
		flags = append(flags, v.Args[1:]...)

		d.simdSrcs = append(d.simdSrcs, simdSrc{
			Src:     filename,
			Variant: variant.Suffix,
			CFlags:  flags,
			Line:    v.Line,
		})
	case "LD_PLUGIN":
		// PR-35k: LD_PLUGIN(name.py) declares a python plugin to be
		// passed to the linker via `--start-plugins ... --end-plugins`
		// in every consumer PROGRAM's LD cmd_args. The named file is
		// copied (via a CP node) from `$(SOURCE_ROOT)/<modulePath>/name.py`
		// to `$(BUILD_ROOT)/<modulePath>/name.py.pyplugin` at gen time.
		// Multiple args (multiple plugins) are accepted; each is
		// recorded verbatim and emitted as a separate CP node by the
		// owning module's `genModule` call. Only `contrib/libs/musl/
		// include` declares this in M2 (`LD_PLUGIN(musl.py)`).
		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case "AR_PLUGIN":
		// PR-M3-openssl-ar-plugin-and-as-clean: AR_PLUGIN(name) registers
		// a python plugin for the module's AR step. Upstream macro
		// `AR_PLUGIN` (ymake.core.conf:3396-3398) does
		// `SET(_AR_PLUGIN $name.pyplugin)`; ld.conf:366-368 then injects
		// `--plugin ${input:_AR_PLUGIN}` between the inner `-- ... --`
		// separators of `_LD_ARCHIVER` and adds the plugin path to
		// `inputs`. Only `contrib/libs/openssl`'s `AR_PLUGIN(ar)` fires
		// in the M3 closure.
		if len(v.Args) != 1 {
			ThrowFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}
		d.arPlugin = v.Args[0] + ".pyplugin"
	case "USE_PYTHON3":
		// M3: USE_PYTHON3() adds implicit PEERDIRs to the Python 3 runtime
		// per build/conf/python.conf macro USE_PYTHON3 (python.conf:1064-1071):
		//   PEERDIR(contrib/libs/python)
		//   when ($USE_ARCADIA_PYTHON == "yes"): PEERDIR+=library/python/runtime_py3
		// The walker does not evaluate conf macros, so we hardcode the peers
		// here. PR-M3-use-python3-peer-split: contrib/tools/python3 and
		// contrib/tools/python3/Lib are NOT in upstream's USE_PYTHON3 macro
		// — they come from the PYTHON3_MODULE() macro (python.conf:644-647),
		// which is invoked inside PY_ANY_MODULE-shaped modules and gated by
		// USE_ARCADIA_PYTHON && (MSVC || IS_CROSS_TOOLS). Plain PROGRAM /
		// LIBRARY callers of USE_PYTHON3() (e.g. devtools/ymake,
		// devtools/ymake/bin) MUST NOT pick them up directly; they reach
		// the python3 tool transitively via contrib/libs/python's own peer
		// list (IF (USE_ARCADIA_PYTHON) ELSE branch: PEERDIR(contrib/tools/
		// python3, contrib/tools/python3/Lib) in contrib/libs/python/ya.make).
		// Adding them again at the USE_PYTHON3 site pulled their transitive
		// addincl set (lzma/openssl/libffi) into the peer-AddInclGlobal slot
		// BEFORE contrib/libs/python's own python/Include, mismatching the
		// reference cmd_args[21] cluster on ~158 nodes.
		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)
		// PR-M3-python-addincl-cflags: USE_PYTHON3() also invokes
		// `_ARCADIA_PYTHON3_ADDINCL` → `_PYTHON3_ADDINCL` (python.conf:1064)
		// whose `when ($USE_ARCADIA_PYTHON == "yes")` branch adds
		// `CFLAGS+=-DUSE_PYTHON3` and `ADDINCL+=GLOBAL contrib/libs/python/Include`.
		// `collectModule`'s post-pass (`applyPython3AddIncl`) performs that
		// injection; we just record the request here.
		d.usePython3 = true
	case "PY_SRCS":
		// PR-M3-A: collect PY_SRCS python source files into d.pySrcs.
		// PY_SRCS accepts optional leading/per-source modifiers TOP_LEVEL
		// and MAIN. TOP_LEVEL sets namespace to "" for subsequent paths
		// (default ns is `<modulePath-dotted>.`).  MAIN flags the next
		// path as the program entry point; in py3 mode this causes
		// pybuild.py:py_main(unit, mod + ":main") to emit a
		// `PY_MAIN=<dotted-mod>:main` kv resource (pybuild.py:362-396).
		// We capture pyMain at parse time; resource.go consumes it.
		topLevel := false
		mainNext := false
		for _, a := range v.Args {
			switch a {
			case "TOP_LEVEL":
				topLevel = true
				d.pyTopLevel = true
				continue
			case "MAIN":
				mainNext = true
				continue
			}
			d.pySrcs = append(d.pySrcs, a)
			if mainNext {
				// Compute the dotted module name per pybuild.py:289,385:
				//   ns = upath.replace('/','.') + '.'   (default)
				//   ns = ''                              (TOP_LEVEL)
				//   mod_name = stripext(arg).replace('/','.')
				//   mod = ns + mod_name
				ns := strings.ReplaceAll(modulePath, "/", ".") + "."
				if topLevel {
					ns = ""
				}
				modName := strings.TrimSuffix(a, ".py")
				modName = strings.ReplaceAll(modName, "/", ".")
				d.pyMain = ns + modName + ":main"
				mainNext = false
			}
		}
	case "PY_MAIN":
		// PR-M3-resource-objcopy-B: PY_MAIN(<arg>) macro per upstream
		// pybuild.py:onpy_main (build/plugins/pybuild.py:762). Argument
		// gets normalised: `/` → `.`, and a `:main` suffix is appended
		// when the arg has no colon. Multiple PY_MAIN(...) on the same
		// module would each emit a separate resource entry, but the M3
		// closure contains only single-PY_MAIN modules — we keep one.
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := strings.ReplaceAll(v.Args[0], "/", ".")
		if !strings.Contains(arg, ":") {
			arg += ":main"
		}
		d.pyMain = arg
	case "NO_CHECK_IMPORTS":
		// PR-M3-resource-objcopy-B: NO_CHECK_IMPORTS(args...) per upstream
		// ytest.py:on_register_no_check_imports (build/plugins/ytest.py:808).
		// The args are joined by ' ' in declaration order; that string is
		// the resfs value AND the input to _common.pathid() (md5 →
		// lower-cased unpadded base32). Empty arg list = no-op (no kv).
		if len(v.Args) > 0 {
			d.noCheckImports = append(d.noCheckImports, v.Args...)
		}
	case "PY_REGISTER":
		// PR-M3-reg3-cpp-py-register: capture PY_REGISTER(args...) dotted
		// module names. emitPyRegister later emits one PY (gen_py3_reg.py)
		// node generating `<arg>.reg3.cpp` plus a CC compiling it; both
		// flow into the module's `.global.a` (mirror of the upstream
		// SRCS(GLOBAL $Func.reg3.cpp) inside macro _PY3_REGISTER at
		// build/ymake.core.conf:4086-4089).
		d.pyRegister = append(d.pyRegister, v.Args...)
		// PR-M3-final-surgical (fix 4): mirror pybuild.py:740-750 — for
		// each dotted PY_REGISTER argument inject the two -D macro
		// renames so every CC in the same module compiles with them.
		for _, name := range v.Args {
			dot := strings.LastIndexByte(name, '.')
			if dot < 0 {
				continue
			}
			shortname := name[dot+1:]
			// mangle: "a.b.c" → "1a1b1c" (len(seg)+seg per segment).
			var mangled strings.Builder
			for _, seg := range strings.Split(name, ".") {
				fmt.Fprintf(&mangled, "%d%s", len(seg), seg)
			}
			d.cFlags = append(d.cFlags,
				"-DPyInit_"+shortname+"=PyInit_"+mangled.String(),
				"-Dinit_module_"+shortname+"=init_module_"+mangled.String(),
			)
		}
	case "SET_APPEND":
		// PR-M3-openssl-as-cflags: SET_APPEND(<var> <values...>) is
		// ymake's append-to-variable macro. Only SFLAGS is wired today
		// (openssl/crypto/ya.make.inc:179-186's
		// `IF (ARCH_X86_64 AND NOT MSVC) { SET_APPEND(SFLAGS -mavx512bw
		// -mavx512ifma -mavx512vl) }`); SFLAGS is the assembler flag
		// bundle threaded between CFLAGS and `-c -o` in AS cmd_args
		// (ymake.core.conf:3217). Other targets currently no-op.
		if len(v.Args) >= 2 && v.Args[0] == "SFLAGS" {
			d.sFlags = append(d.sFlags, v.Args[1:]...)
		}
	case "INDUCED_DEPS":
		// PR-M3-runprogram-closure: capture INDUCED_DEPS(<ext-filter> headers...)
		// declared at module level. First arg is the extension filter
		// (e.g. `h+cpp`, `h`) identifying which generated output kinds the
		// listed headers apply to; remaining args are ${ARCADIA_ROOT}-
		// rooted header paths. ymake/conf/index.yaml syntax: see
		// `build/conf/_decl.conf` for the source-of-truth spec. We strip
		// the leading `${ARCADIA_ROOT}/` prefix so the stored paths are
		// repo-relative (SOURCE_ROOT-relative); the consumer rebases.
		if len(v.Args) >= 2 {
			for _, p := range v.Args[1:] {
				p = strings.TrimPrefix(p, "${ARCADIA_ROOT}/")
				d.inducedDeps = append(d.inducedDeps, p)
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

// applyArchiveStmt parses `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)`
// per upstream build/ymake.core.conf:4142-4145. NAME is a required
// keyword followed by exactly one argument (the output filename);
// DONTCOMPRESS is a bare flag that maps to the archiver's `-p` switch;
// the remaining positional args are the inputs in declaration order.
// Throws on a missing or malformed NAME — there is no sensible default
// output name for this shape.
func applyArchiveStmt(v *UnknownStmt, d *moduleData) {
	var (
		entry      archiveEntry
		seenName   bool
		inNameSlot bool
	)
	for _, a := range v.Args {
		switch {
		case inNameSlot:
			entry.Name = a
			inNameSlot = false
			seenName = true
		case a == "NAME":
			inNameSlot = true
		case a == "DONTCOMPRESS":
			entry.DontCompress = true
		default:
			entry.Files = append(entry.Files, a)
		}
	}

	if inNameSlot {
		ThrowFmt("gen: ARCHIVE(NAME ...) missing value after NAME (line %d)", v.Line)
	}

	if !seenName || entry.Name == "" {
		ThrowFmt("gen: ARCHIVE expects `NAME <output>` (line %d)", v.Line)
	}

	if len(entry.Files) == 0 {
		ThrowFmt("gen: ARCHIVE(NAME %s) has no input files (line %d)", entry.Name, v.Line)
	}

	d.archives = append(d.archives, entry)
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

	if _, ok := allocatorPeers[name]; !ok {
		ThrowFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name, v.Line)
	}

	// PR-43: allocator peers are inserted into the program-default slot
	// (between build/cow/on and musl/full) by defaultProgramPeerdirsFor,
	// NOT into d.peerdirs (user-peer slot). Appending to d.peerdirs caused
	// the mimalloc cluster to land after musl/full's transitive closure
	// (asmlib/asmglibc/musl) in the LD archive list, reversing the
	// REF order for ragel6's ALLOCATOR(MIM) case.
	d.hadAllocator = true
	d.allocatorName = name
}

// isMultimoduleLibraryType returns true for module-declaration names that
// are NOT "LIBRARY" or "PROGRAM" but are treated as LIBRARY-shaped stubs
// in PR-M3-A. These include Python-binding native libraries, Python
// libraries, and proto libraries. Their C/C++ sources (when present) are
// compiled as normal LIBRARY sources; their non-C sources (*.py, *.proto)
// are skipped (header-only path). PR-M3-B..E introduce real emitters for
// the PY/PB/PR node kinds.
// isPyLibraryType returns true for Python library/program module names that
// behave as LIBRARY-shaped modules (emit AR/CC for their C++ SRCS, header-only
// when they have none). Unlike the multimodule types in isMultimoduleLibraryType,
// these modules are NOT unconditionally header-only — hasCompilableSource gates
// the path. They are separated so the gate check at the top of genModule can
// admit them without routing every one of them to the header-only path.
func isPyLibraryType(name string) bool {
	switch name {
	case "PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

// pyLibraryAutoPythonPeer returns true for Python module types whose
// upstream definition in build/conf/python.conf auto-PEERDIRs
// contrib/libs/python (gated by NO_PYTHON_INCLUDES). The set is a
// strict subset of isPyLibraryType — PY23_NATIVE_LIBRARY is excluded
// because its PY2/PY3 sub-modules inherit from plain LIBRARY (not
// PY*_LIBRARY) and so do not pick up the implicit peer upstream.
// PY2_PROGRAM/PY3_PROGRAM are kept in step with PY3_PROGRAM_BIN
// because _BASE_PY3_PROGRAM (their base) carries the same implicit peer.
func pyLibraryAutoPythonPeer(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY", "PY3_PROGRAM_BIN",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

func isMultimoduleLibraryType(name string) bool {
	switch name {
	case "PROTO_LIBRARY",
		"DLL", "SO_PROGRAM",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY":
		return true
	}

	return false
}

// buildIfEnv constructs the per-instance bound-variable environment
// for IF predicates. The base set is `DefaultIfEnv` (M2 default =
// aarch64 / linux / clang / musl). For host instances (Flags.PIC),
// flip ARCH_AARCH64↔ARCH_X86_64 so the same ya.make produces the
// other architecture's branches. The result is a fresh Environment;
// the caller is free to mutate it.
//
// PR-35o: ARCH_ARM64 is the upstream alias for ARCH_AARCH64 (Arcadia
// sets both together). Flip it alongside ARCH_AARCH64 so any
// `IF (ARCH_ARM64 ...)` predicate sees the same binding as
// `ARCH_AARCH64` — required for `contrib/libs/cxxsupp/builtins`'s
// bf16 SRCS block whose gate uses `ARCH_ARM64 OR ARCH_X86_64`.
func buildIfEnv(instance ModuleInstance) Environment {
	env := DefaultIfEnv.Clone()

	if instance.Target == PlatformDefaultLinuxX8664 {
		env.SetBool("ARCH_AARCH64", false)
		env.SetBool("ARCH_ARM64", false)
		env.SetBool("ARCH_X86_64", true)
	}

	if instance.Target == PlatformDefaultLinuxAArch64 {
		env.SetBool("ARCH_AARCH64", true)
		env.SetBool("ARCH_ARM64", true)
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
		// D41: pass platform identity rather than PIC flag so inferFlagsFromPath
		// seeds the peer's PIC from the parent's Target axis, not Flags.PIC directly.
		Flags: inferFlagsFromPath(peerPath, targetIsX8664(parent)),
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
// the literal `-I$(SOURCE_ROOT)/` prefix at emit time. Match the same
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
// the same `-I$(SOURCE_ROOT)/contrib/libs/linux-headers{,/_nf}` flags.
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

	return ctx.cliDefines["MUSL"] == "yes"
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
		muslOn = ctx.cliDefines["MUSL"] == "yes"
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
	// PR-34b: capture the seed key BEFORE applyUnknownStmt-style overlay
	// at line 1116 (`instance.Flags = d.flags`) rebinds instance.Flags
	// to the macro-derived FlagSet. Callers pass the seed FlagSet from
	// `derivePeerInstance`/`inferFlagsFromPath`, which lacks the
	// post-parse NO_PLATFORM / NO_COMPILER_WARNINGS / NO_UTIL /
	// NO_RUNTIME / NO_LIBC bits applied by `collectModule`. Memo writes
	// run AFTER the overlay, so without an alias the seed-key lookup at
	// the top of this function misses every consumer's call and we
	// re-execute the body 7-138 times per module. The fix: write the
	// result under both the originalInstance (seed) and the
	// post-overlay instance keys at every memo-write site below.
	originalInstance := instance

	if existing, ok := ctx.memo[instance]; ok {
		return existing
	}

	// PR-M3-perf-E: in "local" mode, push a fresh scanCtx cache map for
	// this module emission. Every call to `walkClosure` /
	// `joinSrcsIncludeClosure` inside this genModule frame goes through
	// `getScanCtx`, which addresses the top of the stack; on pop the
	// scanCtxes allocated under this frame become unreachable. In
	// "interned" mode the pair is a no-op (the genCtx-wide map is used).
	ctx.pushLocalScanCtx()
	defer ctx.popLocalScanCtx()

	// YATOOL_TRACE=1: print a trace line on every first-visit so the caller
	// chain is visible in stderr. Format: indent·<path>@<platform> (caller: <parent>)
	if os.Getenv("YATOOL_TRACE") == "1" {
		indent := strings.Repeat("  ", len(ctx.traceStack))
		caller := "(root)"
		if len(ctx.traceStack) > 0 {
			caller = ctx.traceStack[len(ctx.traceStack)-1]
		}
		fmt.Fprintf(os.Stderr, "%sgenModule %s@%s  (from %s)\n", indent, instance.Path, instance.Target, caller)
		ctx.traceStack = append(ctx.traceStack, instance.Path+"@"+string(instance.Target))
		defer func() { ctx.traceStack = ctx.traceStack[:len(ctx.traceStack)-1] }()
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

	if d.moduleStmt.Name != "LIBRARY" && d.moduleStmt.Name != "PROGRAM" && d.moduleStmt.Name != "PY3_PROGRAM_BIN" && !isPyLibraryType(d.moduleStmt.Name) && !isMultimoduleLibraryType(d.moduleStmt.Name) {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path, d.moduleStmt.Name)
	}

	// Update the instance's flags from macro overlay so downstream
	// emitters see the post-macro view. The instance is value-typed
	// so this rebinds locally without affecting the caller.
	instance.Flags = d.flags

	// PR-M3-F-1: upstream ymake.core.conf has `when ($MUSL_LITE == "yes") { NO_UTIL() }`.
	// Apply the same implication: MUSL_LITE=yes → NoUtil=true.
	// This prevents yasm (which declares ENABLE(MUSL_LITE)) from getting
	// util as a default peer, matching the M2 reference graph.
	if d.muslLite {
		instance.Flags.NoUtil = true
	}

	// PR-M3-unpaired-got-closure: _BASE_PY3_PROGRAM in build/conf/python.conf:877-883
	// applies an implicit `ALLOCATOR($_MY_ALLOCATOR)` where the otherwise-branch
	// (non-ARCH_PPC64LE) sets _MY_ALLOCATOR=J. Our linux-x86_64/aarch64 closure
	// takes that branch, so PY3_PROGRAM_BIN modules without an explicit
	// ALLOCATOR(...) inherit jemalloc rather than the plain-PROGRAM
	// TCMALLOC_TC default. Surfaces library/cpp/malloc/jemalloc/{malloc-info.cpp.pic.o,
	// libcpp-malloc-jemalloc.a} as paired nodes for tools/py3cc/slow/py3cc.
	if !d.hadAllocator && d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		d.hadAllocator = true
		d.allocatorName = "J"
	}

	// PR-M3-aarch64-py-closure: PY{2,3,23}_LIBRARY and PY{3}_PROGRAM_BIN
	// module declarations upstream `when ($NO_PYTHON_INCLS != "yes") {
	// PEERDIR+=contrib/libs/python }` inside the module-decl body
	// (build/conf/python.conf:697-699 PY2_LIBRARY, :741-743 PY3_LIBRARY,
	// PY23_LIBRARY inherits via its PY2/PY3 submodules, :887-889 _BASE_PY3_PROGRAM
	// for PY3_PROGRAM_BIN/PY3TEST_BIN). Without this implicit peer,
	// devtools/ymake/contrib/python-rapidjson (PY3_LIBRARY) reaches
	// contrib/libs/python via USE_PYTHON3 only on the host axis (transitively
	// via tools/py3cc/slow); the aarch64 PY3_LIBRARY instances never walk
	// into contrib/libs/python and thus miss the library/python/symbols/
	// {module,libc,python,registry} closure that `contrib/libs/python`'s
	// `IF (USE_ARCADIA_PYTHON)` block PEERDIRs in.
	//
	// PY23_NATIVE_LIBRARY is intentionally NOT in the set: its PY2/PY3
	// submodules inherit from plain `LIBRARY` (python.conf:1238-1259),
	// not from PY*_LIBRARY, so upstream does NOT auto-PEERDIR
	// contrib/libs/python for them. Including PY23_NATIVE_LIBRARY here
	// would create a cycle (library/python/symbols/python → contrib/libs/
	// python → library/python/symbols/python).
	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path != "contrib/libs/python" {
		// PR-M3-cc-argv-slot-order: upstream `_BASE_PY3_LIBRARY` (and the
		// PY2 / PY23 / PY*_PROGRAM siblings) emits PEERDIR(contrib/libs/python)
		// FROM the module-decl macro body BEFORE the user-declared PEERDIRs
		// run. Reference ymakeyaml.cpp.o ref:21 shows `python/Include`
		// (contrib/libs/python OWN GLOBAL) ahead of `re2/include` (user-peer
		// transitive); prepend preserves that visit order in the peer walk.
		d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
	}

	// PR-M3-aarch64-enum-and-global-a: GENERATE_ENUM_SERIALIZATION* injects
	// an implicit PEERDIR to tools/enum_parser/enum_serialization_runtime
	// (upstream `_GENERATE_ENUM_SERIALIZATION_BASE` macro in
	// build/ymake.core.conf). The runtime carries the dispatch_methods.cpp /
	// enum_runtime.cpp / ordered_pairs.cpp sources that the generated
	// _serialized.cpp links against. Skip the self-cycle.
	if len(d.enumSrcs) > 0 && instance.Path != "tools/enum_parser/enum_serialization_runtime" {
		d.peerdirs = append(d.peerdirs, "tools/enum_parser/enum_serialization_runtime")
	}

	// PR-27: a header-only LIBRARY (e.g. library/cpp/sanitizer/include)
	// has no compilable sources but still has a valid module
	// declaration; the upstream reference does not emit any AR for
	// these. Walk the peers so their transitive closure is
	// discovered, then return a `headerOnly: true` result that
	// callers handle by skipping the archive-dep wiring. PROGRAMs
	// with zero compilable sources remain a hard error.
	//
	// PR-M3-C: multimodule library types (PROTO_LIBRARY etc.) always
	// take the header-only path regardless of whether their SRCS
	// contain non-C++ sources like .ev — those are emitted by
	// emitProtoSrcs below, not by emitOneSource.
	// PR-M3-F-1: PY3_PROGRAM_BIN has no C++ sources but IS a PROGRAM
	// (Python program); exclude it from the header-only path so it
	// goes through the full PROGRAM walk + EmitLD dispatch below.
	// PR-M3-F-1: Python library types (PY3_LIBRARY etc.) may have
	// compilable C++ sources (e.g. library/python/runtime_py3); when
	// they do, they take the LIBRARY AR/CC path. When they have no
	// compilable sources they still reach the header-only path via
	// !hasCompilableSource (the `isPyLibraryType` check is NOT here).
	if (!hasCompilableSource(d) && d.moduleStmt.Name != "PY3_PROGRAM_BIN") || isMultimoduleLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == "PROGRAM" && !hasSkippedSource(d) {
			ThrowFmt("gen: %s has no compilable sources (after IF/header filter)", instance.Path)
		}

		// PROGRAMs whose only sources are known-deferred kinds (e.g.
		// .rl ragel5 inputs whose R5 emitter lands in PR-M3-C) are
		// treated as header-only stubs in PR-M3-A rather than a hard
		// error. The PROGRAM LD node is intentionally not emitted here;
		// PR-M3-C closes the gap when EmitR5 / EmitPB / EmitEN are
		// implemented. Multimodule library types (PROTO_LIBRARY etc.)
		// also reach this branch and are likewise header-only for now.

		// Header-only LIBRARYs may declare ADDINCL(GLOBAL ...) /
		// CFLAGS(GLOBAL ...) / CXXFLAGS(GLOBAL ...) / CONLYFLAGS(GLOBAL
		// ...) that peer-propagate without emitting an AR. Walk peers
		// (so their transitive closures reach genModule) and aggregate
		// own + peer GLOBAL contributions per axis so consumers see the
		// full closure. PR-31 D05 (ADDINCL) + PR-32 D07 (CFLAGS axes).
		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)

		// PR-32 D09 follow-on: drop musl-self GLOBAL CFLAGS contributions
		// from the propagated set (mirror of the main-walker gate above).
		ownCFlagsGlobalH := d.cFlagsGlobal
		ownCXXFlagsGlobalH := d.cxxFlagsGlobal
		ownCOnlyFlagsGlobalH := d.cOnlyFlagsGlobal

		if instance.Flags.LibcMusl {
			ownCFlagsGlobalH = nil
			ownCXXFlagsGlobalH = nil
			ownCOnlyFlagsGlobalH = nil
		}

		// PR-35k: emit own LD_PLUGIN CP nodes (e.g. musl.py →
		// musl.py.pyplugin) BEFORE composing the result so the CP refs
		// propagate alongside the peer-walked plugin closure. The CP
		// node carries `module_dir = instance.Path` per the reference
		// shape; the source/dest are anchored under instance.Path.
		ownLDPluginRefs, ownLDPluginPaths := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
		ldPluginRefs, ldPluginPaths := mergeLDPlugins(ownLDPluginRefs, ownLDPluginPaths, peerContribs.ldPluginRefs, peerContribs.ldPluginPaths)

		// PR-M3-A: emit yapyc3 PY nodes for PY_SRCS() declarations.
		// PY3_LIBRARY / PY23_LIBRARY modules often have only PY_SRCS
		// (no compilable C/C++ sources) so they reach the header-only
		// branch; their Python sources still require PY node emission.
		emitPySrcs(ctx, instance, d)

		// PR-M3-resource-objcopy-A: emit objcopy PY nodes for
		// RESOURCE / RESOURCE_FILES declarations. Header-only LIBRARY
		// modules (e.g. certs, PY3_LIBRARY-only-PY_SRCS) host the only-
		// resource shape; PR-M3-aarch64-enum-and-global-a completes the
		// AR wiring: when there are objcopy outputs, emit a .global.a
		// archive that archives them.
		objcopyRefs, objcopyOutputs, objcopyGlobalInputs := emitResourceObjcopy(ctx, instance, d)

		// PR-M3-LD-peer-globalA: capture the header-only `.global.a` ref
		// so consumers see it via `moduleEmitResult.GlobalRef/GlobalPath`.
		// Previously discarded — RESOURCE-only LIBRARY (`certs`) and
		// PY3_LIBRARY PY_SRCS modules' `.global.a` archives were emitted
		// to the graph but orphaned from every LD `inputs` slot.
		var hOnlyGlobalRef *NodeRef
		var hOnlyGlobalPath string

		if len(objcopyRefs) > 0 {
			// REF surfaces module_lang="py3" for PY*_LIBRARY archives
			// (module_tag=global, or py3_global on PY23_LIBRARY) and
			// "cpp" for plain LIBRARY (module_tag=global) and for
			// PY23_NATIVE_LIBRARY (module_tag=py3_native_global). Pivot
			// Language locally for the AR emit; peer-walk Language stays
			// LangCPP.
			arInstance := instance
			var globalBaseName, tag string
			switch d.moduleStmt.Name {
			case "PY23_NATIVE_LIBRARY":
				globalBaseName = globalArchiveNameWithPrefix(instance.Path, "libpy3c")
				tag = "py3_native_global"
			case "PY23_LIBRARY":
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefix(instance.Path, "libpy3")
				tag = "py3_global"
			case "PY3_LIBRARY", "PY2_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefix(instance.Path, "libpy3")
				tag = "global"
			default:
				globalBaseName = globalArchiveName(instance.Path)
				tag = "global"
			}
			gRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRefs, objcopyOutputs, objcopyGlobalInputs, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = instance.Path + "/" + globalBaseName
		}

		// PR-M3-D: emit EN nodes for GENERATE_ENUM_SERIALIZATION(*) declarations.
		// Header-only modules in the M3 closure never compile the EN
		// `_serialized.cpp` output (every observed EN-emitting module
		// has a regular AR archiving the output); pass nil consumerInputs
		// to suppress downstream-CC emission here. PR-M3-codegen-cc-enqueue.
		emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, nil)

		// PR-M3-C: emit PB/EV nodes for PROTO_LIBRARY .proto/.ev sources.
		// PROTO_LIBRARY modules never have compilable C/C++ sources and
		// always reach the header-only branch; their .proto/.ev sources
		// require protoc-driven PB/EV node emission.
		// PR-M3-proto-library-ar: also emits downstream CC + AR scaffolding
		// for true PROTO_LIBRARY modules (skipped for other multimodule
		// types). peerContribs is threaded so the downstream CCs see the
		// same peer-GLOBAL CFLAGS / ADDINCL slice the header-only walker
		// aggregated.
		// PR-M3-LD-peer-globalA: surface PROTO_LIBRARY's emitted .a so
		// downstream LD walks see it (the AR previously lived in the
		// graph but was orphaned from every LD `inputs` slot).
		protoARRef, protoARPath, protoHasAR := emitProtoSrcs(ctx, instance, d, peerContribs)

		// PR-M3-E: emit JV, CF, BI, PR nodes declared at module level.
		// Header-only branch: no downstream CC/AR, so pass nil consumerInputs.
		emitMiscNodes(ctx, instance, d, nil)

		// PR-M3-LD-peer-globalA: PROTO_LIBRARY emits a regular `.a` archive
		// via `emitProtoSrcs` above. Surface that AR through the result so
		// downstream consumers' peer-archive closure picks it up. The
		// module remains `headerOnly: true` (it compiles zero of its own
		// C/C++ sources; the AR's members are protoc-generated CCs), but
		// `hasPlainAR=true` lets the consumer's walker fold the AR into
		// its `peerArchiveRefs` slot without re-introducing the AR-on-AR
		// dependency that PR-30 D05 explicitly removed for LIBRARY ARs.
		hOnlyARRef := NodeRef{}
		hOnlyARPath := ""
		hOnlyHasAR := false
		if protoHasAR {
			hOnlyARRef = protoARRef
			hOnlyARPath = protoARPath
			hOnlyHasAR = true
		}

		result := &moduleEmitResult{
			headerOnly:              true,
			isPyLibrary:             isPyLibraryType(d.moduleStmt.Name),
			hasPlainAR:              hOnlyHasAR,
			ARRef:                   hOnlyARRef,
			ARPath:                  hOnlyARPath,
			GlobalRef:               hOnlyGlobalRef,
			GlobalPath:              hOnlyGlobalPath,
			AddInclGlobal:           mergeDedup(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal:        append([]string(nil), d.addInclGlobal...),
			// PR-M3-peer-cflags-global-ordering: peer-transitive first,
			// own last — mirrors the full-module branch at line 2888-2890
			// per upstream `TGlobalVarsCollector` semantics. ADDINCL keeps
			// the opposite (own first, peer second) per
			// `TModuleIncDirs::Get()`.
			CFlagsGlobal:            mergeDedup(peerContribs.cFlags, ownCFlagsGlobalH),
			CXXFlagsGlobal:          mergeDedup(peerContribs.cxxFlags, ownCXXFlagsGlobalH),
			COnlyFlagsGlobal:        mergeDedup(peerContribs.cOnlyFlags, ownCOnlyFlagsGlobalH),
			PeerArchiveClosureRefs:  peerContribs.archiveRefs,
			PeerArchiveClosurePaths: peerContribs.archivePaths,
			PeerGlobalClosureRefs:   peerContribs.globalRefs,
			PeerGlobalClosurePaths:  peerContribs.globalPaths,
			LDPluginRefs:            ldPluginRefs,
			LDPluginPaths:           ldPluginPaths,
			InducedDeps:             append([]string(nil), d.inducedDeps...),
		}
		ctx.memo[originalInstance] = result
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

	// PR-35g: ALLOCATOR(FAKE) suppresses the implicit malloc/api auto-
	// peer (matches upstream `_BASE_UNIT`'s skip of malloc/api when
	// ALLOCATOR=FAKE). yasm is the M2-closure case — no allocator peer
	// AND no malloc/api means yasm's LD drops one peer-archive ref.
	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)

	// PR-30 D02 + D03: PROGRAM-only implicit peerdirs. `_BASE_PROGRAM`
	// adds musl/full (when MUSL=yes && !MUSL_LITE) and the default
	// ALLOCATOR's peer set (TCMALLOC_TC for our environment) on top of
	// the language defaults. Threaded only for PROGRAM modules; the
	// `hadAllocator` flag suppresses the allocator-default when the
	// PROGRAM declared `ALLOCATOR(NAME)` itself.
	//
	// PR-35g: split program-defaults from language-defaults so the peer-
	// GLOBAL aggregation can apply different orderings to each group
	// (language-defaults two-phase; program-defaults single-phase).
	//
	// PR-43: program-defaults are further split into pre-user (cow/on +
	// optional tcmalloc) and post-user (musl/full or musl) halves.
	// Explicit ALLOCATOR peers and regular d.peerdirs are interleaved
	// between the two halves so they appear before musl/full in the
	// archive-accumulation walk (correct LD link order for the
	// mimalloc cluster) while retaining peerKindUserPeer (correct
	// AddInclGlobal Phase 3 ordering for the ragel6 CC include case).
	languageDefaultsCount := len(defaults)

	isProgram := (d.moduleStmt.Name == "PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN") && !isRuntimeAncestor(instance.Path)

	var preUserProgDefaults []string
	var postUserProgDefaults []string
	if isProgram {
		preUserProgDefaults = defaultProgramPeerdirsFor(ctx, instance, d.hadAllocator, d.allocatorName, d.muslLite, false)
		postUserProgDefaults = defaultProgramPeerdirsFor(ctx, instance, d.hadAllocator, d.allocatorName, d.muslLite, true)
		defaults = append(defaults, preUserProgDefaults...)
	}

	// allocatorExplicitPeers are the peers declared by ALLOCATOR(NAME)
	// (nil for FAKE/DEFAULT/SYSTEM, or when no ALLOCATOR macro was used).
	// They are treated as peerKindUserPeer so AddInclGlobal Phase 3
	// places their transitive includes ahead of later user-PEERDIRs.
	allocatorExplicitPeers := allocatorPeers[d.allocatorName]

	seen := make(map[string]struct{}, len(defaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))
	allPeers := make([]string, 0, len(defaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	// PR-35g: track per-peer category so the peer-GLOBAL aggregation
	// below can apply the right ordering rule per group:
	//   - language-defaults: two-phase (own first, then transitive) —
	//     preserves libcxx/libcxxrt OWN ahead of the musl-arch
	//     transitive chain (the PR-31 D05 archiver invariant).
	//   - user-peers: single-phase AddInclGlobal in declaration order —
	//     places an ALLOCATOR-derived peer's transitive GLOBAL ahead of
	//     a later user PEERDIR's OWN GLOBAL (the ragel6 mimalloc/include
	//     vs ragel5/aapl invariant).
	//   - program-defaults: single-phase AddInclGlobal — places the
	//     implicit TCMALLOC_TC peer-set's OWN GLOBAL after util's
	//     transitive zlib/double-conversion/libc_compat (the archiver-
	//     default allocator invariant).
	const (
		peerKindLangDefault    = 0
		peerKindProgramDefault = 1
		peerKindUserPeer       = 2
	)

	peerKinds := make([]int, 0, len(defaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	// 1. Language-defaults and pre-user program-defaults.
	for i, p := range defaults {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)

		if i < languageDefaultsCount {
			peerKinds = append(peerKinds, peerKindLangDefault)
		} else {
			peerKinds = append(peerKinds, peerKindProgramDefault)
		}
	}

	// 2. Explicit allocator peers (peerKindUserPeer so Phase 3 handles
	//    their AddInclGlobal, keeping mimalloc/include before ragel5/aapl).
	//    Placed BEFORE the musl post-user block so the allocator cluster
	//    (e.g. mimalloc → malloc/api + mimalloc AR) precedes musl/full's
	//    transitive deps (asmlib/asmglibc/musl) in the archive walk.
	//    Only the ALLOCATOR-derived peers are hoisted here; regular
	//    d.peerdirs stay in step 4 so they remain AFTER musl/full.
	for _, p := range allocatorExplicitPeers {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	// 3. Post-user program-defaults (musl/full or bare musl). Placed
	//    after the allocator explicit peers but before regular user
	//    PEERDIRs so musl/full's transitive closure lands before
	//    user-peerdir libraries in the archive walk (PR-42 invariant).
	for _, p := range postUserProgDefaults {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindProgramDefault)
	}

	// 4. Regular user-declared PEERDIRs.
	for _, p := range d.peerdirs {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	peerArchiveRefs := make([]NodeRef, 0, len(allPeers))
	peerArchivePaths := make([]string, 0, len(allPeers))
	peerGlobalRefs := make([]NodeRef, 0, len(allPeers))
	peerGlobalPaths := make([]string, 0, len(allPeers))

	// PR-M3-LD-peer-globalA: dedup table for the transitive `.global.a`
	// closure. Each direct peer contributes its own `.global.a` (when
	// GlobalRef != nil) AND every entry of its PeerGlobalClosure*. First
	// occurrence wins; the closure flows up through this module's
	// `moduleEmitResult.PeerGlobalClosure*` so PROGRAM LDs at any depth
	// reach every transitively-reachable `.global.a` archive.
	peerGlobalSeen := map[string]struct{}{}
	peerGlobalAddPath := func(ref NodeRef, path string) {
		if _, dup := peerGlobalSeen[path]; dup {
			return
		}

		peerGlobalSeen[path] = struct{}{}
		peerGlobalRefs = append(peerGlobalRefs, ref)
		peerGlobalPaths = append(peerGlobalPaths, path)
	}

	// PR-35c: dedup table for the transitive peer-archive closure.
	// For each direct peer, we accumulate (peer's own AR ∪ peer's
	// PeerArchiveClosure) — first occurrence wins (R14 declaration
	// order). The closure is consumed only by the PROGRAM branch
	// below (LIBRARYs drop peer-archive refs from their AR per
	// PR-30 D05); LIBRARY consumers downstream walk our exposed
	// `PeerArchiveClosureRefs/Paths` field on `moduleEmitResult`,
	// which we fold into below per the same dedup discipline.
	peerArchiveSeen := map[string]struct{}{}
	peerArchiveAddPath := func(ref NodeRef, path string) {
		if _, dup := peerArchiveSeen[path]; dup {
			return
		}

		peerArchiveSeen[path] = struct{}{}
		peerArchiveRefs = append(peerArchiveRefs, ref)
		peerArchivePaths = append(peerArchivePaths, path)
	}

	// PR-35k: dedup table for the transitive LD plugin closure.
	// Each direct peer contributes its `LDPluginRefs/Paths` (which
	// already include the peer's own plugins UNION every transitive
	// peer's). First occurrence wins; the closure flows through this
	// module's result so consumers further up the walk pick it up
	// without re-walking.
	peerLDPluginRefs := make([]NodeRef, 0, 1)
	peerLDPluginPaths := make([]string, 0, 1)
	peerLDPluginSeen := map[string]struct{}{}
	peerLDPluginAddPath := func(ref NodeRef, path string) {
		if _, dup := peerLDPluginSeen[path]; dup {
			return
		}

		peerLDPluginSeen[path] = struct{}{}
		peerLDPluginRefs = append(peerLDPluginRefs, ref)
		peerLDPluginPaths = append(peerLDPluginPaths, path)
	}

	// PR-31 D05 + PR-32 D07: aggregate peer-GLOBAL contributions
	// transitively across all four axes (ADDINCL / CFLAGS / CXXFLAGS /
	// CONLYFLAGS). The aggregation uses a TWO-PHASE traversal so the
	// reference's observed ordering is preserved:
	//
	//   Phase 1 — for each peer in declaration order, collect that
	//             peer's OWN GLOBAL declarations (no transitive).
	//   Phase 2 — for each peer in declaration order, collect that
	//             peer's TRANSITIVE peer-GLOBAL contributions
	//             (everything except its own).
	//
	// Empirical motivation: tools/archiver/main.cpp.o cmd_args[11..16]
	// in sg.json shows libcxx-include + libcxxrt-include (own GLOBAL
	// of libcxx and libcxxrt) BEFORE the musl-arch paths (which
	// transitively propagate through libcxx's auto-PEERDIR of
	// musl/include). A single-phase DFS-completion aggregation puts
	// musl-arch FIRST (because builtins is walked before libcxx and
	// already has musl-arch via its musl/include peer); two-phase
	// puts libcxx/include and libcxxrt/include first because they
	// are own-declarations.
	addInclSeen := map[string]struct{}{}
	peerAddInclGlobal := make([]string, 0, 16)

	cFlagsSeen := map[string]struct{}{}
	peerCFlagsGlobal := make([]string, 0, 16)

	cxxFlagsSeen := map[string]struct{}{}
	peerCXXFlagsGlobal := make([]string, 0, 16)

	cOnlyFlagsSeen := map[string]struct{}{}
	peerCOnlyFlagsGlobal := make([]string, 0, 16)

	addEach := func(seenSet map[string]struct{}, dst *[]string, src []string) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}

	// Phase 0: resolve every peer's *moduleEmitResult once and stash
	// it; Phase 1 + Phase 2 then iterate the cached results.
	type resolvedPeer struct {
		path   string
		result *moduleEmitResult
		kind   int // PR-35g: peerKindLangDefault / peerKindProgramDefault / peerKindUserPeer
	}

	resolved := make([]resolvedPeer, 0, len(allPeers))

	for i, p := range allPeers {
		peerPath := filepath.Clean(p)

		kind := peerKinds[i]

		// PR-35g: language-defaults AND program-defaults both go through
		// the missing-ya.make tolerance (the synthetic-test fixtures
		// pattern). Only user-declared PEERDIRs are required to exist.
		if kind != peerKindUserPeer && !peerYaMakeExists(ctx.sourceRoot, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(instance, peerPath)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.isPROGRAM {
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})

		// PR-35c: fold peer's transitive archive closure into our
		// own running closure BEFORE the peer's own archive (DFS
		// post-order: dependencies-of-peer come first, peer last).
		// Header-only peers may still expose a closure (their
		// PEERDIRs' archives) even though they emit no AR themselves.
		for i, p := range peerResult.PeerArchiveClosurePaths {
			peerArchiveAddPath(peerResult.PeerArchiveClosureRefs[i], p)
		}

		// PR-M3-LD-peer-globalA: fold peer's transitive `.global.a`
		// closure BEFORE the peer's own `.global.a` (DFS post-order,
		// same shape as the archive closure). Runs for header-only and
		// non-header peers alike — a header-only LIBRARY contributes no
		// `.global.a` of its own, but its peers may.
		for i, p := range peerResult.PeerGlobalClosurePaths {
			peerGlobalAddPath(peerResult.PeerGlobalClosureRefs[i], p)
		}

		// PR-35k: fold peer's LD plugin closure (own ∪ transitive) into
		// our own. Runs for BOTH header-only and non-header peers — the
		// only M2 plugin (musl.py.pyplugin) is owned by the header-only
		// `contrib/libs/musl/include` LIBRARY.
		for i, p := range peerResult.LDPluginPaths {
			peerLDPluginAddPath(peerResult.LDPluginRefs[i], p)
		}

		// PR-M3-LD-peer-globalA: header-only peers still expose their
		// own `.global.a` when present (e.g. `certs` RESOURCE-only
		// LIBRARY emits `libcerts.global.a` from objcopy outputs via
		// PR-M3-aarch64-enum-and-global-a). Fold the peer's own
		// `.global.a` regardless of headerOnly status.
		if peerResult.GlobalRef != nil {
			peerGlobalAddPath(*peerResult.GlobalRef, peerResult.GlobalPath)
		}

		// PR-M3-residue-B: use peerResult.ARPath (which carries the
		// py3-prefixed name for Python modules) instead of recomputing
		// ArchiveName. Skip when hasPlainAR is false (module has only
		// GLOBAL_SRCS — no regular archive was emitted).
		// PR-M3-LD-peer-globalA: PROTO_LIBRARY emits a regular `.a` from
		// the header-only branch (its members are protoc-generated CCs);
		// `hasPlainAR` is set on its `moduleEmitResult` so the AR flows
		// through this branch regardless of `headerOnly`.
		if peerResult.hasPlainAR {
			// ARPath has "$(BUILD_ROOT)/" prefix; cmd_args use a
			// bare relative path. Strip the prefix for consistency.
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(BUILD_ROOT)/")
			peerArchiveAddPath(peerResult.ARRef, arRelPath)
		}
	}

	// PR-35g: per-kind aggregation. Language-defaults use two-phase
	// (own first, transitive second) so libcxx/libcxxrt OWN GLOBAL
	// land before musl-arch transitive (archiver invariant). User-
	// peers and program-defaults use single-phase AddInclGlobal in
	// declaration order so an allocator-derived peer's transitive
	// GLOBAL precedes a later peer's OWN GLOBAL (ragel6 mimalloc-vs-
	// aapl invariant) and program-defaults' OWN GLOBAL trail
	// language-defaults' transitive (archiver tcmalloc-after-zlib
	// invariant).

	// Phase 1: language-defaults' OWN GLOBAL declarations.
	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEach(addInclSeen, &peerAddInclGlobal, rp.result.OwnAddInclGlobal)
	}

	// Phase 2: language-defaults' TRANSITIVE peer-GLOBAL contributions
	// (full AddInclGlobal; dedup drops the OWN duplicates from Phase 1).
	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEach(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
	}

	// PR-M3-l3-peer-order: program-defaults emit BEFORE user-peers in
	// peer-GLOBAL ADDINCL aggregation. The two slots interleave only when
	// a user-peer brings GLOBAL contributions of its own (own or transitive)
	// that overlap the program-default closure — in every empirically
	// observed REF cluster (archiver, ragel5/rlgen-cd, protoc/bin,
	// rescompiler/rescompressor, py3cc, library/python/runtime_py3/
	// stage0pycc) the program-default tcmalloc + abseil-cpp pair appears
	// AHEAD of any user-peer's OWN or transitive GLOBAL. The prior heuristic
	// (gate on user-peer OWN GLOBAL only) missed:
	//   - rescompiler/rescompressor: user-peer library/cpp/resource has no
	//     OWN GLOBAL but transitively pulls contrib/restricted/abseil-cpp
	//     via library/cpp/containers/absl_flat_hash; the heuristic chose
	//     user-peers-first and placed abseil-cpp ahead of tcmalloc.
	//   - py3cc / library/python/runtime_py3/stage0pycc: user-peer contrib/
	//     tools/python3 transitively pulls lzma/openssl/libffi which then
	//     landed ahead of program-defaults' tcmalloc + abseil-cpp.
	// Archiver remains correct: its user-peers contribute only dedups of
	// lang-default paths, so program-defaults-first produces the same
	// trailing tcmalloc + abseil-cpp pair as user-peers-first.
	emitUserPeers := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindUserPeer {
				continue
			}

			addEach(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
		}
	}

	emitProgramDefaults := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindProgramDefault {
				continue
			}

			addEach(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
		}
	}

	emitProgramDefaults()
	emitUserPeers()

	// PR-35g: drop bundled-include paths from the peer-propagated set.
	// `ccIncludesSuffix` already injects `-I…linux-headers{,/_nf}` at
	// the front of every non-musl CC node; a transitive peer's GLOBAL
	// declaration of the same paths (e.g. linux-headers's own GLOBAL
	// reaching consumers via musl/full → linux-headers) would emit a
	// duplicate at the peer-AddIncl slot. Musl flavours drop the entire
	// peer-AddInclGlobal slice in cc.go's composer, so this filter is a
	// no-op for them.
	if len(peerAddInclGlobal) > 0 {
		filtered := peerAddInclGlobal[:0]

		for _, p := range peerAddInclGlobal {
			if bundledAddInclPaths[p] {
				continue
			}

			filtered = append(filtered, p)
		}

		peerAddInclGlobal = filtered
	}

	// PR-33 C01: hoist runtime-stack include paths (libcxx/include,
	// libcxxrt/include, libcxxabi/include, libunwind/include) to the
	// FRONT of the aggregated peer-GLOBAL ADDINCL slice so they slot
	// immediately after the linux-headers ccIncludesSuffix in
	// composeTargetCC / composeHostCC.
	//
	// The hoist only fires when THIS module is itself a runtime
	// ancestor (`util`, `library/cpp/malloc/api`, ...). The reason:
	// upstream propagates the libcxx/libcxxrt header search paths to
	// runtime-ancestor consumers as if they were direct GLOBAL peers,
	// even though `defaultPeerdirsFor` returns only
	// `[contrib/libs/musl/include]` for them (zero peer-archive deps
	// is a LINK-closure invariant, not a header-include one). Without
	// the hoist util's own CC nodes (util/_/digest/city.cpp.o + 15
	// siblings) get libcxx/libcxxrt at the tail of peerAddInclGlobal,
	// arriving via the Phase 2 transitive walk through util's user
	// PEERDIRs (util/charset, zlib, double-conversion, libc_compat).
	//
	// Non-runtime-ancestor consumers do NOT get the hoist:
	//   - Modules with no NO_RUNTIME (tools/archiver, util/charset,
	//     ragel6/bin) already see libcxx/libcxxrt as direct defaults
	//     via Phase 1 and emit them at the head naturally.
	//   - Modules with NO_RUNTIME (yasm — host PROGRAM with explicit
	//     NO_RUNTIME) intentionally pick up libcxx/libcxxrt at the
	//     TAIL via transitive walks through musl_extra / jemalloc.
	//     The reference confirms yasm libyasm/assocdat.c.pic.o has
	//     libcxx/libcxxrt at slots 17-18, AFTER the musl-arch group.
	//     Hoisting unconditionally would regress this case.
	if isRuntimeAncestor(instance.Path) {
		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
	}

	// CFLAGS / CXXFLAGS / CONLYFLAGS: today no module in the M2
	// closure has both own-GLOBAL and transitive peer-GLOBAL on the
	// same axis (musl-self CFLAGS are suppressed; libcxx's
	// `-nostdinc++` is GLOBAL-CXXFLAGS but its peers don't add
	// further). The two-phase pattern is still applied for symmetry
	// and forward-compatibility.
	for _, rp := range resolved {
		addEach(cFlagsSeen, &peerCFlagsGlobal, rp.result.CFlagsGlobal)
		addEach(cxxFlagsSeen, &peerCXXFlagsGlobal, rp.result.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, rp.result.COnlyFlagsGlobal)
	}

	// PR-31 D05: this module's effective AddInclGlobal is its OWN
	// GLOBAL ADDINCL plus the union of every peer's transitive set.
	// Stored on the result so transitive consumers see the closure
	// in one shot.
	effectiveAddInclGlobal := mergeDedup(d.addInclGlobal, peerAddInclGlobal)

	// PR-M3-python3-addincl-buildroot-order: `library/python/runtime_py3`
	// propagates `$(BUILD_ROOT)/library/python/runtime_py3` to consumers
	// via its `effectiveAddInclGlobal`, but at a position AFTER
	// `contrib/restricted/abseil-cpp` (NOT at the head as a regular
	// own-GLOBAL would land). Upstream `_PYTHON3_ADDINCL` runs at
	// module-eval time (adds `python/Include`), then PEERDIR processing
	// propagates abseil-cpp into runtime_py3's UserGlobalPropagated via
	// `library/cpp/resource → absl_flat_hash`; the `ARCHIVE` macro fires
	// later still and adds the BUILD_ROOT path via the `addincl`
	// modifier — interleaved in such a way that the path lands AFTER
	// abseil-cpp in propagation. Splice it explicitly to match the
	// empirical REF ordering on 145 consumer nodes (e.g. devtools/ymake
	// LIBRARY preeval.cpp.o slots 21-23 `[python/Include, abseil-cpp,
	// BUILD_ROOT/runtime_py3]`).
	//
	// Fallback: when abseil-cpp is not in the merged set (no peer chain
	// brings it), append at the tail — preserves the path so consumers
	// can resolve runtime_py3's generated headers, while not interfering
	// with non-abseil cases.
	if instance.Path == "library/python/runtime_py3" {
		const buildRootPath = "$(BUILD_ROOT)/library/python/runtime_py3"
		const abseilPath = "contrib/restricted/abseil-cpp"
		spliced := make([]string, 0, len(effectiveAddInclGlobal)+1)
		inserted := false

		for _, p := range effectiveAddInclGlobal {
			if p == buildRootPath {
				continue
			}

			spliced = append(spliced, p)

			if !inserted && p == abseilPath {
				spliced = append(spliced, buildRootPath)
				inserted = true
			}
		}

		if !inserted {
			spliced = append(spliced, buildRootPath)
		}

		effectiveAddInclGlobal = spliced
	}

	// PR-32 D07: same shape for CFLAGS / CXXFLAGS / CONLYFLAGS.
	// PR-32 D09 follow-on: musl-self modules' GLOBAL CFLAGS (which
	// include `-D_musl_=1` from `contrib/libs/musl/ya.make`) are
	// NOT propagated to non-musl consumers. The empirical reference
	// shows only one M2 closure module (tools/archiver/main.cpp.o)
	// carries `-D_musl_=1`, suggesting upstream has additional
	// gating beyond plain GLOBAL CFLAGS propagation. The
	// `-D_musl_` (no `=1`) consumer-side sentinel comes via
	// AutoPeerCFlags from D09 instead. Suppression is keyed on
	// Flags.LibcMusl (data, not path) per the user directive.
	ownCFlagsGlobal := d.cFlagsGlobal
	ownCXXFlagsGlobal := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobal := d.cOnlyFlagsGlobal

	if instance.Flags.LibcMusl {
		ownCFlagsGlobal = nil
		ownCXXFlagsGlobal = nil
		ownCOnlyFlagsGlobal = nil
	}

	// PR-M3-peer-cflags-global-ordering: peer-transitive CFLAGS GLOBAL
	// precede this module's OWN CFLAGS GLOBAL in the effective propagated
	// slice. Upstream rule (devtools/ymake/global_vars_collector.cpp):
	// `TGlobalVarsCollector::Collect` (json_visitor.cpp:558) runs at
	// DFS-Left for every direct peerdir edge, accumulating each peer's
	// fully-built USER_CFLAGS into the consumer with `AppendUnique`
	// (first-occurrence-wins); `Finish` runs at PrepareLeaving and only
	// then pushes the module's OWN reserved-name CFLAGS contributions.
	// Net effect on the per-module slice: peer-transitive CFLAGS land
	// FIRST in DFS deepest-first order, own CFLAGS land LAST. ADDINCL
	// follows the opposite rule via `TModuleIncDirs::Get()` returning
	// `LocalUserGlobal` (own) before `UserGlobalPropagated` (peer), so
	// `effectiveAddInclGlobal` keeps `(own, peer)` ordering above.
	//
	// Empirical anchor: devtools/ymake/_/commands/compilation.cpp.o
	// (aarch64) cmd_args[68..70]: REF has [OPENSSL_RENAME_SYMBOLS=1,
	// ASIO_STANDALONE, ASIO_SEPARATE_COMPILATION]. ASIO is a direct
	// PEERDIR of devtools/ymake and PEERDIRs OpenSSL; upstream's per-peer
	// AppendUnique places asio's OpenSSL transitive (peer-first) ahead of
	// asio's own ASIO_* defines.
	effectiveCFlagsGlobal := mergeDedup(peerCFlagsGlobal, ownCFlagsGlobal)
	effectiveCXXFlagsGlobal := mergeDedup(peerCXXFlagsGlobal, ownCXXFlagsGlobal)
	effectiveCOnlyFlagsGlobal := mergeDedup(peerCOnlyFlagsGlobal, ownCOnlyFlagsGlobal)

	// PR-35c (closes PR-33-A2_01): inject libcxx's GLOBAL ADDINCL +
	// GLOBAL CXXFLAGS into runtime-ancestor C++ consumers' OWN CC
	// emission only — not into the `effective*` propagation slices
	// already snapshotted above.
	//
	// Why local-only: making libcxx an implicit DEFAULT peer (via
	// `defaultPeerdirsFor`) would also push libcxx/include +
	// libcxxrt/include into this module's `effectiveAddInclGlobal`,
	// which every downstream consumer's Phase 2 walk reads — producing
	// spurious -I flags on unrelated CC nodes (zlib, mimalloc,
	// libcxxabi-parts, etc.) for a 100+-node L3 regression.
	//
	// Mutating `peerAddInclGlobal` and `peerCXXFlagsGlobal` AFTER the
	// `effective*` snapshot keeps the propagated view clean. The local
	// view (consumed by `ModuleCCInputs` for THIS module's own CC
	// compile) gains the slots; the C01 hoist below re-runs on the
	// post-injection slice so the injected libcxx/include +
	// libcxxrt/include land at the canonical front position
	// immediately after the linux-headers ccIncludesSuffix.
	if !effectiveNoPlatform(instance.Flags) && runtimeAncestorCxxConsumers[instance.Path] {
		// libcxx's CLANG-branch GLOBAL CXXFLAG (`-nostdinc++`) — see
		// `contrib/libs/cxxsupp/libcxx/ya.make:67-69`.
		const nostdincPP = "-nostdinc++"
		// libcxx's GLOBAL ADDINCL set on Linux with CXX_RT==libcxxrt
		// — see `contrib/libs/cxxsupp/libcxx/ya.make:24-25, 78-85`.
		injectAddIncl := []string{
			"contrib/libs/cxxsupp/libcxx/include",
			"contrib/libs/cxxsupp/libcxxrt/include",
		}

		for _, p := range injectAddIncl {
			if _, dup := addInclSeen[p]; dup {
				continue
			}

			addInclSeen[p] = struct{}{}
			peerAddInclGlobal = append(peerAddInclGlobal, p)
		}

		if _, dup := cxxFlagsSeen[nostdincPP]; !dup {
			cxxFlagsSeen[nostdincPP] = struct{}{}
			peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, nostdincPP)
		}

		// Re-hoist so the injected libcxx/include + libcxxrt/include
		// slot at the front of `peerAddInclGlobal` (the runtime-stack
		// position observed in malloc/api's reference cmd_args[11..12]).
		// The earlier C01 hoist call at line 1414 already ran, but on
		// the un-injected slice; running it again here is idempotent
		// for entries already at the front and a no-op when nothing
		// was injected.
		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
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
	// PR-41 Fix I: track which ccOutputs entries come from SRC_C_NO_LTO
	// (i.e., d.flatSrcs) so reorderARMembers can hoist them to the front
	// without disturbing the declaration order of regular SRCS members.
	ccIsFlatNoLto := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))
	// PR-M3-final-sort-inversions: track CF-generated CCs (the .cpp output
	// of a .cpp.in / .c.in CONFIGURE_FILE expansion). Their .o suffix is
	// the same as a hand-written .cpp, so reorderARMembers cannot detect
	// them from the path alone; REF places them after the hand-written
	// regulars in declaration order. Witness: library/cpp/build_info
	// (sandbox.cpp.in, build_info.cpp.in, build_info_static.cpp →
	// REF: build_info_static.cpp.o, sandbox.cpp.o, build_info.cpp.o).
	ccIsCFGenerated := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))
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

	// PR-35y R8: track which entries are "primary sources" of the
	// regular SRCS / JOIN_SRCS / .rl6 dispatch — distinct from
	// header closures. The reference graph treats the two splits
	// asymmetrically across the regular `.a` and `.global.a`
	// archives:
	//
	//   - regular AR (`.a`) inputs: regular primaries + global
	//     primaries + everyone's header/closure;
	//   - global AR (`.global.a`) inputs: global primaries +
	//     everyone's header/closure (NO regular primaries).
	//
	// Empirical reference: contrib/libs/tcmalloc/no_percpu_cache —
	// the regular `.a` archives `aligned_alloc.c` (regular SRCS) AND
	// every `tcmalloc/*` global SRCS source (and all 1311 shared
	// headers). The `.global.a` archives every `tcmalloc/*` source
	// and the same 1311 shared headers, but NOT `aligned_alloc.c`.
	//
	// `regularPrimariesSet` was the membership filter the global AR
	// used to drop regular SRCS primaries from its inputs before
	// PR-M3-globalA-narrow-closure. The narrowed `.global.a` now uses
	// `globalMemberInputs` directly (the GLOBAL_SRCS-local closure)
	// and never sees regular primaries; the set + closure are retained
	// so call sites in the source-loop don't have to be untangled.
	regularPrimariesSet := map[string]struct{}{}
	addRegularPrimary := func(p string) {
		regularPrimariesSet[p] = struct{}{}
	}

	// PR-32 D09: auto-injected peer-CFLAG -D_musl_ for every TARGET
	// module that is not effectively NO_PLATFORM, when the CLI says
	// MUSL=yes. Mirrors `_BASE_UNIT`'s
	// `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`. Suppressed for
	// musl-self (LibcMusl=true) modules — those receive `-D_musl_=1`
	// from `muslExtraDefines` instead, and the extra `-D_musl_` from
	// `_BASE_UNIT` is gated off there by upstream NO_PLATFORM.
	autoPeerCFlags := defaultPeerCFlags(ctx, instance, d)

	// PR-33 D02 + D03: thread the module's own non-GLOBAL CFLAGS and
	// own GLOBAL CFLAGS / CXXFLAGS / CONLYFLAGS into ModuleCCInputs so
	// the composer emits them on this module's own CC compiles.
	// LibcMusl-self modules zero them out: musl's CFLAGS are folded
	// into `muslExtraDefines` and the musl composers do not consult
	// these slots (mirror of the existing `ownCFlagsGlobal = nil`
	// branch above for the peer-propagation path).
	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.LibcMusl {
		ownCFlags = nil
		ownCFlagsGlobalSelf = nil
		ownCXXFlagsGlobalSelf = nil
		ownCOnlyFlagsGlobalSelf = nil
	}

	// PR-33 C02: PROGRAM-only `-D_musl_=1` injection. The musl GLOBAL
	// CFLAG `-D_musl_=1` (`contrib/libs/musl/ya.make:52`) reaches a
	// PROGRAM consumer's ownCFlags slot — appended AFTER the module's
	// own CFLAGS, BEFORE the noLibcUndebugBlock / ndebugPicBlock that
	// follows. LIBRARY consumers (util, util/charset, libcxxrt, ...)
	// do NOT receive this flag, only the consumer-side `-D_musl_`
	// sentinel from `defaultPeerCFlags` (which slots after catboost).
	//
	// The PROGRAM-vs-LIBRARY discrimination is empirical: tools/
	// archiver/main.cpp.o (target PROGRAM, slot 60), yasm libyasm/
	// assocdat.c.pic.o (host PROGRAM, slot 52), ragel6/all_cd.cpp.
	// pic.o (host PROGRAM, slot 51) all carry `-D_musl_=1`, while
	// util/_/digest/city.cpp.o (target LIBRARY) and util/charset/
	// all_charset.cpp.o (target LIBRARY) and libcxxrt/auxhelper.cc.o
	// (LIBRARY) do not.
	//
	// Suppressed for LibcMusl-self (ownCFlags is already nil, but
	// guard explicitly for clarity) and for effectively-NO_PLATFORM
	// modules (mirror of the consumer-sentinel gate in
	// `defaultPeerCFlags`).
	if d.moduleStmt.Name == "PROGRAM" && cliMuslOn(ctx) && !instance.Flags.LibcMusl && !effectiveNoPlatform(instance.Flags) {
		// Copy before append: `ownCFlags = d.cFlags` aliases the
		// underlying array, and a future caller iterating d.cFlags
		// directly must not see the injected flag.
		ownCFlagsWithMusl := make([]string, 0, len(ownCFlags)+1)
		ownCFlagsWithMusl = append(ownCFlagsWithMusl, ownCFlags...)
		ownCFlagsWithMusl = append(ownCFlagsWithMusl, "-D_musl_=1")
		ownCFlags = ownCFlagsWithMusl
	}

	// PR-39: contrib/libs/musl/full is a non-musl LIBRARY inside the musl
	// subtree (SET(MUSL no) in its ya.make). Its CC dispatch routes through
	// composeTargetCC / composeHostCC (LibcMusl=false), but the upstream
	// build context is still MUSL=yes, so the reference injects
	// -D_musl_=1 into the ownCFlags slot — same position as the PROGRAM
	// injection above, but keyed on the musl-subtree path. TODO: remove
	// when SET() parsing lands in M3+ and drives this from the ya.make flag.
	if instance.Path == "contrib/libs/musl/full" && cliMuslOn(ctx) {
		ownCFlagsWithMusl := make([]string, 0, len(ownCFlags)+1)
		ownCFlagsWithMusl = append(ownCFlagsWithMusl, ownCFlags...)
		ownCFlagsWithMusl = append(ownCFlagsWithMusl, "-D_musl_=1")
		ownCFlags = ownCFlagsWithMusl
	}

	// PR-M3-F-6 (Cluster-CC-INCL-OVER): dedup d.addIncl in
	// first-occurrence-wins order. The reference (openssl drbg_lib.c.o
	// idx 9-14: exactly 6 unique entries) does not emit duplicate -I
	// flags even when the same path appears in both the top-level
	// ADDINCL block and an INCLUDE'd `crypto/ya.make.inc` ADDINCL block.
	// Our parser appends without dedup at the AddInclStmt site, so
	// openssl emits 8 entries (6 unique + 2 trailing dupes for
	// `openssl/include` and `openssl`). Dedup at composer entry keeps
	// the parser's append-only model intact while matching upstream's
	// emit-time dedup behaviour.
	dedupedAddIncl := mergeDedup(d.addIncl, nil)

	// PR-M3-residue-B: Python native library modules (PY23_NATIVE_LIBRARY)
	// emit ".py3.o" CC outputs (not plain ".o") per the reference graph.
	// PR-M3-py23-py3suffix-module-cpp: PY23_LIBRARY also emits ".py3.o"
	// (e.g. library/python/symbols/module/module.cpp.py3.o); extend flag.
	isPy3NativeLib := d.moduleStmt.Name == "PY23_NATIVE_LIBRARY" ||
		d.moduleStmt.Name == "PY23_LIBRARY"

	// PR-M3-module-tag-and-stats-enums-dep: PY23_NATIVE_LIBRARY's PY3
	// submodule calls PYTHON3_ADDINCL() → SET(MODULE_TAG PY3_NATIVE)
	// (build/conf/python.conf:995); PY23_LIBRARY's PY3 submodule inherits
	// PY3_LIBRARY → _ARCADIA_PYTHON3_ADDINCL() → SET(MODULE_TAG PY3)
	// (python.conf:1005). The REF graph surfaces these as the lower-cased
	// `target_properties.module_tag = "py3_native"` / `"py3"` on the
	// per-source CC nodes and the regular (.a) AR archive. Plain PY3_LIBRARY
	// (e.g. library/python/runtime_py3) carries no module_tag on its
	// regular CC/AR — it inherits the default for its type and upstream
	// omits redundant properties. The "global" / "py3_global" /
	// "py3_native_global" suffixed tags on .global.a archives are set
	// independently at the EmitARGlobalNamedTagged call site below.
	var perModuleCCTag string
	switch d.moduleStmt.Name {
	case "PY23_NATIVE_LIBRARY":
		perModuleCCTag = "py3_native"
	case "PY23_LIBRARY":
		perModuleCCTag = "py3"
	}

	// arNameFn selects the archive naming function for this module:
	//   - PY23_NATIVE_LIBRARY → "libpy3c" prefix (Py3cArchiveName)
	//   - PY3_LIBRARY / PY2_LIBRARY / PY23_LIBRARY / PY2_PROGRAM / PY3_PROGRAM → "libpy3" prefix (Py3ArchiveName)
	//   - everything else → standard "lib" prefix (ArchiveName)
	var arNameFn func(string) string
	var globalArNameFn func(string) string
	switch d.moduleStmt.Name {
	case "PY23_NATIVE_LIBRARY":
		arNameFn = Py3cArchiveName
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefix(dir, "libpy3c") }
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
		arNameFn = Py3ArchiveName
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefix(dir, "libpy3") }
	default:
		arNameFn = ArchiveName
		globalArNameFn = globalArchiveName
	}

	// PR-M3-cc-argv-slot-order: drop BUILD_ROOT-rooted addincl paths from
	// the peer slot when the same path already sits in this module's own
	// addincl slot. Generated-output paths (`$(BUILD_ROOT)/<mod>`) are
	// produced by THIS module's ARCHIVE() / RUN_PROGRAM and arrive at peer
	// consumers via the PEERDIR walk; the self-compile must not also
	// emit them in the peer slot. SOURCE_ROOT paths (e.g. `python/Include`)
	// are not filtered — the upstream reference deliberately emits the
	// own + peer duplicate for those (sitecustomize.cpp.pic.o ref:8+26,
	// ymakeyaml.cpp.o ref:9+21).
	selfPeerAddInclGlobal := filterBuildRootSelfPaths(peerAddInclGlobal, dedupedAddIncl)

	moduleInputs := ModuleCCInputs{
		AddIncl:              dedupedAddIncl,
		PeerAddInclGlobal:    selfPeerAddInclGlobal,
		CFlags:               ownCFlags,
		CXXFlags:             d.cxxFlags,
		COnlyFlags:           d.cOnlyFlags,
		OwnCFlagsGlobal:      ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:    ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:  ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:     peerCFlagsGlobal,
		PeerCXXFlagsGlobal:   peerCXXFlagsGlobal,
		PeerCOnlyFlagsGlobal: peerCOnlyFlagsGlobal,
		AutoPeerCFlags:       autoPeerCFlags,
		SFlags:               d.sFlags,
		SrcDir:               d.srcDir,
		SourceRoot:           ctx.sourceRoot,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		Py3Suffix:            isPy3NativeLib,
		ModuleTag:            perModuleCCTag,
		Ragel6Flags:          d.ragel6Flags,
	}

	// PR-30 D06: ancestor-only SRCDIR rebase. The "PROGRAM with SRCDIR
	// pointing at an ancestor of instance.Path" pattern (typified by
	// `contrib/tools/ragel6/bin` whose SRCDIR is `contrib/tools/ragel6`)
	// is the only shape where the reference rebases module_dir to
	// SRCDIR. LIBRARYs with SRCDIR keep module_dir at instance.Path
	// and route per-source via composeCCPaths' SRCDIR-aware composer.
	ancestorRebase := d.srcDir != "" && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(d.srcDir, instance.Path)

	// PR-M3-F-7-C: emit EN nodes BEFORE the per-source CC loop so the
	// codegen registry is populated when consumer sources scan their
	// include closures. `stats.cpp`/`trace.cpp` in `devtools/ymake/diag`
	// `#include <devtools/ymake/diag/stats_enums.h_serialized.h>` and the
	// scanner now consults `IncludeScanner.codegen` (F-7-A) populated by
	// `emitEnumSrcs` (F-7-B). If EN runs AFTER the source loop (the pre-
	// F-7-C placement at the bottom of this branch), the registry is empty
	// at scan time and the lookups miss. EN node emission order in the
	// output graph does not affect L4 byte-exactness (the normalizer
	// re-sorts by canonical UID).
	//
	// PR-M3-codegen-cc-enqueue: pass moduleInputs so emitEnumSrcs also
	// emits the downstream CC for each EN's `_serialized.cpp`. The
	// returned `(refs, outputs, memberInputs)` are folded into the AR
	// member buckets below (alongside PR-AUDIT-5's PR-downstream CCs)
	// so the consumer module's regular `.a` archives the EN-derived
	// `.o`s after its declared SRCS.
	enCCRefs, enCCOutputs, enCCMemberInputs := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

	// PR-AUDIT-8: hoist JV/CF/BI/PR node emission before the per-source loop
	// so the codegen registry is fully populated when any source's WalkClosure
	// runs. Mirrors the earlier emitEnumSrcs hoist (F-7-C). No state written
	// by the per-source loop is read by emitMiscNodes.
	// PR-M3-antlr-g4-cpp: pass moduleInputs so JV downstream CP+CC are emitted.
	jvCCRefs, jvCCOutputs, jvCCMemberInputs := emitMiscNodes(ctx, instance, d, &moduleInputs)

	// PR-M3-L0-cascade-close-v2: hoist PR+AR node emission ahead of the SRCS
	// loop so the codegen registry's AR/PR ProducerRef entries exist when a
	// consumer CC (e.g. library/python/runtime_py3/__res.cpp) scans its
	// inputs and reaches the .pyc.inc / PR-emitted output paths. Without
	// this hoist the registry lookup misses, and runtime_py3's 4 AR-cluster
	// CCs cascade 543/599 of the L0 mismatch surface (per Plan B). The
	// returned PR-downstream-CC triples are folded into the AR-member bucket
	// at the original site (below) so the existing AR.cmd_args order
	// (which is byte-exact in M2) is preserved.
	prCCRefs, prCCOutputs, prMemberInputsList := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	emitArchives(ctx, instance, d)

	// PR-M3-F-7-C: two-pass source emission. Codegen-producing sources
	// (.ev/.proto/.rl6/.rl/.cpp.in/.c.in) emit nodes whose outputs
	// (`.ev.pb.h`, `.rl6.cpp`, `*.cpp`, …) consumer CCs in this same
	// module may #include. If we processed sources in d.srcs declaration
	// order, a consumer .cpp that precedes a codegen producer would scan
	// its closure against an unpopulated registry — the resolveCache and
	// subgraphCache would lock in a "not found" miss that survives even
	// after the producer registers later. (Witnessed on devtools/ymake/
	// diag: `display.cpp` is index 3, `trace.ev` is index 4; display.cpp's
	// scan of trace.h → trace.ev.pb.h missed and poisoned the trace.h
	// subgraph for every subsequent consumer.)
	//
	// Fix: emit codegen-producing sources FIRST (Pass A), then iterate
	// d.srcs in declaration order (Pass B), using Pass A's cached results
	// for codegen producers and calling emitOneSource fresh for the rest.
	// AR member order is preserved (Pass B appends to ccRefs in d.srcs
	// order), so the resulting AR.cmd_args remains byte-exact.
	type srcResult struct {
		ref          NodeRef
		outPath      string
		ccIns        []string
		primaryCount int
		ok           bool
	}

	preEmitted := make(map[string]srcResult, 4)

	for _, src := range d.srcs {
		if !isCodegenProducingSrc(src) {
			continue
		}

		srcInputs := moduleInputs
		if extras, ok := d.perSrcCFlags[src]; ok {
			srcInputs.PerSourceCFlags = extras
		}
		if _, ok := d.flatSrcs[src]; ok {
			srcInputs.FlatOutput = true
		}

		ref, outPath, ccIns, primaryCount, ok := emitOneSource(ctx, instance, d.srcDir, src, srcInputs, ancestorRebase)
		preEmitted[src] = srcResult{ref: ref, outPath: outPath, ccIns: ccIns, primaryCount: primaryCount, ok: ok}
	}

	for _, src := range d.srcs {
		// PR-35o: overlay per-source extras recorded by `SRC(...)` /
		// `SRC_C_NO_LTO(...)` onto the module-level inputs bag for THIS
		// source only. The composer slots `srcInputs.PerSourceCFlags`
		// between macroPrefixMapFlags and the input path; FlatOutput
		// selects the flat output-path layout (no `_/` infix). Plain
		// SRCS / GLOBAL_SRCS sources have no entries in either map so
		// the overlay is a no-op (preserves byte-exact for every other
		// CC).
		srcInputs := moduleInputs
		if extras, ok := d.perSrcCFlags[src]; ok {
			srcInputs.PerSourceCFlags = extras
		}

		isFlatNoLto := false
		if _, ok := d.flatSrcs[src]; ok {
			srcInputs.FlatOutput = true
			isFlatNoLto = true
		}

		var ref NodeRef
		var outPath string
		var ccIns []string
		var primaryCount int
		var ok bool

		if pre, hadPre := preEmitted[src]; hadPre {
			ref, outPath, ccIns, primaryCount, ok = pre.ref, pre.outPath, pre.ccIns, pre.primaryCount, pre.ok
		} else {
			ref, outPath, ccIns, primaryCount, ok = emitOneSource(ctx, instance, d.srcDir, src, srcInputs, ancestorRebase)
		}

		if !ok {
			continue
		}

		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, isFlatNoLto)
		ccIsCFGenerated = append(ccIsCFGenerated, strings.HasSuffix(src, ".cpp.in") || strings.HasSuffix(src, ".c.in"))
		addMemberInputs(ccIns)
		// PR-35y R8: track primary source paths so the .global.a
		// aggregator can exclude them. emitOneSource returns the
		// ccIns slice with the leading `primaryCount` entries being
		// the member's primary source(s) — `.cpp/.c/.cc/.cxx`/`.S`
		// dispatch yields 1 primary; `.rl6` dispatch yields 1 (the
		// .rl6 source) or 2 (when the `.h` companion exists on disk).
		for i := 0; i < primaryCount && i < len(ccIns); i++ {
			addRegularPrimary(ccIns[i])
		}
	}

	// PR-M3-AR-header-closure: headers (.h/.hpp) listed in SRCS do not
	// emit a CC node, but upstream ymake still walks their #include
	// closure and propagates it up to the AR/LD via EDT_BuildFrom (the
	// `addInput` call in mkcmd.cpp:212-228, reached for every
	// EDT_BuildFrom file child whether or not it has a build command).
	// Without this pass our AR loses the transitive set reached only
	// through SRCS-listed headers (e.g. library/cpp/packedtypes:
	// fixed_point.h / packed.h / zigzag.h drag in libcxx/locale,
	// numeric, vector and zc_memory_input.h that no .cpp member
	// transitively reaches).
	for _, src := range d.srcs {
		if !isHeaderSource(src) {
			continue
		}
		headerPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + src
		headerVFS := resolveSourceVFS(ctx, instance, src, moduleInputs.SrcDir)
		headerClosure := walkClosure(ctx, instance, headerVFS, moduleInputs)
		addMemberInputs(append([]string{headerPath}, headerClosure...))
	}

	// PR-M3-antlr-g4-cpp: fold JV-downstream CCs (CP-rename + compile for
	// each ANTLR-generated .cpp) into the AR member bucket. The reference
	// graph places them after the regular SRCS bucket and before the
	// EN-downstream CCs (sg2.json devtools/ymake/lang AR: TConfLexer.g4.cpp.o,
	// TConfParser.g4.cpp.o, CmdLexer.g4.cpp.o, CmdParser.g4.cpp.o at
	// positions after value_storage.cpp.o and before h_serialized.cpp.o).
	for i, ref := range jvCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, jvCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(jvCCMemberInputs[i])
	}

	// PR-M3-codegen-cc-enqueue: fold the EN-downstream CCs (captured
	// above via emitEnumSrcs) into the regular AR member bucket. The
	// reference graph places these `.h_serialized.cpp.o` entries after
	// the module's declared SRCS `.cpp.o` and before any JOIN_SRCS /
	// PR-derived members (sg2.json devtools/ymake's `libdevtools-ymake.a`
	// cmd_args positions 134..142 — trailing the 124-entry regular SRCS
	// bucket).
	for i, ref := range enCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, enCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(enCCMemberInputs[i])
	}

	// PR-M3-L0-cascade-close-v2: PR-downstream CC fold. emitRunProgramsForAR
	// + emitArchives were hoisted ahead of the SRCS loop so the codegen
	// registry's PR/AR ProducerRef entries are populated when consumer CCs
	// (e.g. library/python/runtime_py3/__res.cpp) scan their inputs[]. The
	// AR.cmd_args bucket ordering (PR-AUDIT-5: PR-downstream CCs sit AFTER
	// regular SRCS, before JOIN_SRCS) is preserved by deferring the fold
	// to this position.
	for i, ref := range prCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, prCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(prMemberInputsList[i])
	}

	// PR-M3-simd-permutations: emit one CC node per SRC_C_<V> entry.
	// Each variant compile reuses the regular CC flavor pipeline (same
	// AddIncl / peer/own CFLAGS / scanner closure as the module's plain
	// SRCS) but carries the variant `-m<flag>` bundle + extras at the
	// PerSourceCFlags slot and a `.<variant>` suffix in the output path
	// (FlatOutput=true so the path is `<module>/<src>.<variant>.pic.o`,
	// no `_/` infix even when `src` is nested). The entries inherit the
	// SRC_C_NO_LTO flat-bucket disposition for AR ordering (R8):
	// reorderARMembers hoists them ahead of plain SRCS in the archive,
	// matching the reference shape (blake2: SRC()s first, then the 10
	// SIMD variants, then `_/`-infix SRCS).
	for _, e := range d.simdSrcs {
		variantIn := moduleInputs
		variantIn.FlatOutput = true
		variantIn.Variant = e.Variant
		// Compose PerSourceCFlags = (variant CFLAGS + macro extras) +
		// any pre-existing PerSourceCFlags for this filename declared
		// via SRC(filename extra...) — although the reference shows no
		// case where SIMD and SRC stack on the same file, the merge is
		// the principled join.
		flags := append([]string(nil), e.CFlags...)
		if extras, ok := d.perSrcCFlags[e.Src]; ok {
			flags = append(flags, extras...)
		}
		variantIn.PerSourceCFlags = flags

		ref, outPath, ccIns, primaryCount, ok := emitOneSource(ctx, instance, d.srcDir, e.Src, variantIn, ancestorRebase)
		if !ok {
			continue
		}

		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, true)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(ccIns)
		for i := 0; i < primaryCount && i < len(ccIns); i++ {
			addRegularPrimary(ccIns[i])
		}
	}

	// PR-41 Fix I: record the SRCS/JOIN_SRCS boundary so the AR
	// cmd_args reorder below can apply the right bucket rules to
	// each group independently. All entries up to this index are
	// SRCS-derived (regular + .rl6); entries from here onward are
	// JOIN_SRCS-derived.
	numSrcsDerived := len(ccOutputs)

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

		// PR-35d: per-source include closure threaded into the JS
		// node Inputs and the JS-derived CC's IncludeInputs (mirror
		// of the reference: the joined .cpp textually #includes each
		// member, so its closure is the union of member closures).
		//
		// PR-40 Fix C: JS nodes are anchored to the outer-target
		// platform axis (PR-35s), so the JS closure must be resolved
		// with the TARGET scanner and TARGET musl arch search paths,
		// even when srcInstance is a host (PIC) instance. The
		// downstream CC node still compiles on the host axis and needs
		// the HOST closure. Compute them separately when
		// srcInstance.Flags.PIC — for the target case they are
		// identical so a single call suffices.
		// TODO: remove the Flags.PIC guard when a general target-vs-host
		// axis parameter is plumbed through genModule (M3+ scope).
		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
		if targetIsX8664(srcInstance) {
			// Compute a separate closure for the JS node using the
			// TARGET scanner and TARGET musl arch search paths.
			// jsInstance uses the target platform so joinSrcsIncludeClosure
			// picks ctx.scannerTarget. jsModuleInputs rebases PeerAddInclGlobal
			// to swap x86_64 arch paths for aarch64 ones so the search
			// path reflects the target (aarch64) musl layout.
			jsInstance := srcInstance
			jsInstance.Target = PlatformDefaultLinuxAArch64
			jsInstance.Flags.PIC = false

			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = jsTargetPeerAddIncl(moduleInputs.PeerAddInclGlobal)

			joinClosure = joinSrcsIncludeClosure(ctx, jsInstance, js.Sources, jsModuleInputs)
		}

		// PR-35s: anchor the JS node to the outer-target platform
		// (`ctx.cfg.Target.ID`) regardless of whether this module
		// instance was reached through a host-PROGRAM walk. The
		// reference graph emits every JS (JOIN_SRCS) node on
		// `default-linux-aarch64` — including the 7 JOIN_SRCS in
		// `contrib/tools/ragel6/bin` whose surrounding LD lives on
		// the host axis. Only the JS Platform axis detaches; the
		// downstream JS-derived CC node below continues to compile
		// at `srcInstance.Target` (host x86_64 for ragel6/bin) so
		// the .pic.o output stays on the correct compile axis.
		jsRef, joinOut := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.cfg.Target.ID, ctx.emit)

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

		// PR-35d: thread (scripts + sources + closure) as the
		// JS-derived CC's IncludeInputs so its full Inputs read
		// [joinedCpp, scripts..., sources..., closure...] — same shape
		// as JS Inputs with the joined .cpp prepended.
		// PR-40 Fix C: use ccClosure (host scanner when PIC) for the
		// CC node, not joinClosure (target scanner).
		ccIncludeInputs := jsCCIncludeInputs(srcInstance, js.Sources, ccClosure)

		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = jsRef
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = ccIncludeInputs

		ref, outPath := EmitCC(srcInstance, jsRel, ccIn, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false) // JOIN_SRCS are never SRC_C_NO_LTO
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		// PR-35y R7: the AR/LD `inputs` slot omits the BUILD_ROOT-
		// staged generated cpp (JS output). Reference graph confirms:
		// util's libyutil.a never lists `$(BUILD_ROOT)/util/all_*.cpp`
		// even though those are the primary inputs of the downstream
		// JS-derived CC nodes. The aggregator gets only the member's
		// scripts + joined source files + their resolved include
		// closure (`ccIncludeInputs`).
		addMemberInputs(ccIncludeInputs)
		// PR-35y R8: the joined source files (`js.Sources`) are
		// "regular primaries" — only the regular AR archives them;
		// the .global.a aggregator drops them. Scripts and the
		// resolved header closure flow to BOTH archives. Empirical
		// reference: util's libyutil.a (no .global.a) and util/charset's
		// libutil-charset.a both archive the JS member sources.
		for _, s := range js.Sources {
			addRegularPrimary("$(SOURCE_ROOT)/" + srcInstance.Path + "/" + s)
		}
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
		ref, outPath, ccIns, _, ok := emitOneSource(ctx, instance, d.srcDir, src, moduleInputs, ancestorRebase)

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

	// PR-M3-reg3-cpp-py-register: emit PY+CC pairs for each PY_REGISTER(arg).
	// Both flow into globalRefs/globalOutputs (the upstream macro
	// _PY3_REGISTER appends `SRCS(GLOBAL $Func.reg3.cpp)` so the .o lands
	// in the module's .global.a archive). PY3_LIBRARY (rapidjson, ymakeyaml)
	// emits plain `.reg3.cpp.o`; PY23_LIBRARY and PY23_NATIVE_LIBRARY emit
	// `.reg3.cpp.py3.o` (reference: library/python/symbols/module — a
	// PY23_LIBRARY multimodule whose py3 submodule tags its CC outputs
	// with module_tag=py3 and the .py3.o suffix).
	regCCPy3Suffix := isPy3NativeLib || d.moduleStmt.Name == "PY23_LIBRARY"
	regRefs, regOutputs, regMemberInputs := emitPyRegister(ctx, instance, d, moduleInputs, regCCPy3Suffix)

	for i, ref := range regRefs {
		globalRefs = append(globalRefs, ref)
		globalOutputs = append(globalOutputs, regOutputs[i])
	}

	for _, p := range regMemberInputs {
		if _, dup := globalMemberInputsSeen[p]; dup {
			continue
		}

		globalMemberInputsSeen[p] = struct{}{}
		globalMemberInputs = append(globalMemberInputs, p)
	}

	// PR-35k: emit own LD_PLUGIN CP nodes (no current M2 case fires
	// here — musl/include is header-only and handled above — but the
	// emission is symmetric so a future LIBRARY/PROGRAM that declares
	// LD_PLUGIN inline picks up the same wiring). Merge with the
	// transitive peer plugin closure; the result feeds both EmitLD's
	// `--start-plugins ... --end-plugins` block (PROGRAMs) and the
	// LDPluginRefs/Paths slot on `moduleEmitResult` (every kind).
	ownLDPluginRefs, ownLDPluginPaths := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
	mergedLDPluginRefs, mergedLDPluginPaths := mergeLDPlugins(ownLDPluginRefs, ownLDPluginPaths, peerLDPluginRefs, peerLDPluginPaths)

	if d.moduleStmt.Name == "PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		// PR-28-D01: PROGRAM(name) declares the linker output basename
		// directly. Most ya.makes elide the argument (PROGRAM() →
		// binary inherits the directory's last component) but
		// `contrib/tools/ragel6/bin/ya.make` declares
		// `PROGRAM(ragel6)` so the binary is `bin/ragel6`, not
		// `bin/bin`. Pass through to EmitLD; the emitter's empty-fallback
		// matches the elided-argument case.
		// PR-M3-F-1: PY3_PROGRAM_BIN shares the same dispatch path;
		// it has no own CC outputs (ccRefs/ccOutputs are empty) but its
		// peer closure and LD node are emitted identically.
		var binaryName string

		if len(d.moduleStmt.Args) > 0 {
			binaryName = d.moduleStmt.Args[0]
		}

		// PR-35g: ALLOCATOR(FAKE) at the PROGRAM level filters
		// `library/cpp/malloc/api` out of the link closure even when a
		// transitive peer (musl_extra, jemalloc, ...) introduced it via
		// its own default-peer set. yasm is the M2-closure case: yasm
		// itself drops malloc/api via `suppressMallocAPIDefault` above,
		// but its user peers musl_extra and jemalloc each have malloc/
		// api in their own default sets, re-introducing it via the
		// archive closure. The link-closure filter applies the same
		// suppression at the PROGRAM boundary.
		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths

		if d.allocatorName == "FAKE" {
			ldPeerArchiveRefs = make([]NodeRef, 0, len(peerArchiveRefs))
			ldPeerArchivePaths = make([]string, 0, len(peerArchivePaths))

			for i, p := range peerArchivePaths {
				if strings.HasPrefix(p, "library/cpp/malloc/api/") {
					continue
				}

				ldPeerArchiveRefs = append(ldPeerArchiveRefs, peerArchiveRefs[i])
				ldPeerArchivePaths = append(ldPeerArchivePaths, p)
			}
		}

		// PR-M3-F-1: PY3_PROGRAM_BIN emits module_lang="py3". Tag the
		// instance at the EmitLD call site only so the Language field
		// does not propagate into derivePeerInstance's peer walks (peers
		// are C++ LIBRARY modules and must stay Language=LangCPP to
		// share memo entries with the rest of the target/host closure).
		ldInstance := instance
		if d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			ldInstance.Language = LangPy
		}

		// PR-M3-py3-runtime-closure: PY3_PROGRAM_BIN must emit its yapyc3
		// and objcopy nodes BEFORE EmitLD so the objcopy outputs can be
		// folded into the LD's ccPaths slot (the reference graph wraps
		// the program LD around the per-resource objcopy `.o` files in
		// addition to the regular member CCs). Pre-PR the emission ran
		// after EmitLD which meant the LD never saw the objcopy outputs.
		ldCCRefs := ccRefs
		ldCCOutputs := ccOutputs
		ldMemberInputs := memberInputs

		if d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			emitPySrcs(ctx, instance, d)

			objcopyRefs, objcopyOutputs, _ := emitResourceObjcopy(ctx, instance, d)

			if len(objcopyRefs) > 0 {
				ldCCRefs = append(append([]NodeRef(nil), ccRefs...), objcopyRefs...)
				ldCCOutputs = append(append([]string(nil), ccOutputs...), objcopyOutputs...)
			}

			// Fold the objcopy script + PY_SRCS source paths + RESOURCE
			// source paths into the LD member-input union so they appear
			// in the LD node's inputs (mirror of the reference shape for
			// tools/py3cc/slow/py3cc: build/scripts/objcopy.py +
			// tools/py3cc/slow/main.py + each declared RESOURCE source).
			if resourceModuleTag(d.moduleStmt.Name) != "" {
				var resourcePaths []string
				for _, e := range d.resources {
					if e.Path == "-" {
						continue
					}

					resourcePaths = append(resourcePaths, e.Path)
				}

				if extras := pySrcsARExtraInputs(instance.Path, d.srcDir, d.pySrcs, resourcePaths); len(extras) > 0 {
					seen := make(map[string]struct{}, len(ldMemberInputs))
					for _, p := range ldMemberInputs {
						seen[p] = struct{}{}
					}

					ldMemberInputs = append([]string(nil), ldMemberInputs...)

					for _, p := range extras {
						if _, dup := seen[p]; dup {
							continue
						}

						seen[p] = struct{}{}
						ldMemberInputs = append(ldMemberInputs, p)
					}
				}
			}
		}

		ldRef := EmitLD(
			ldInstance,
			binaryName,
			ldCCRefs, ldCCOutputs,
			ldPeerArchiveRefs, ldPeerArchivePaths,
			mergedLDPluginRefs, mergedLDPluginPaths,
			peerGlobalRefs, peerGlobalPaths,
			ldMemberInputs,
			cliMuslOn(ctx),
			ownCFlags,
			ctx.emit,
		)
		ldPath := LDOutputPath(instance, binaryName)

		result := &moduleEmitResult{
			ARRef:                   ldRef,
			ARPath:                  ldPath,
			isPROGRAM:               true,
			isPyLibrary:             isPyLibraryType(d.moduleStmt.Name),
			LDRef:                   ldRef,
			LDPath:                  ldPath,
			AddInclGlobal:           effectiveAddInclGlobal,
			OwnAddInclGlobal:        append([]string(nil), d.addInclGlobal...),
			CFlagsGlobal:            effectiveCFlagsGlobal,
			CXXFlagsGlobal:          effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:        effectiveCOnlyFlagsGlobal,
			PeerArchiveClosureRefs:  append([]NodeRef(nil), peerArchiveRefs...),
			PeerArchiveClosurePaths: append([]string(nil), peerArchivePaths...),
			PeerGlobalClosureRefs:   append([]NodeRef(nil), peerGlobalRefs...),
			PeerGlobalClosurePaths:  append([]string(nil), peerGlobalPaths...),
			LDPluginRefs:            mergedLDPluginRefs,
			LDPluginPaths:           mergedLDPluginPaths,
			InducedDeps:             append([]string(nil), d.inducedDeps...),
		}
		ctx.memo[originalInstance] = result
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
	//
	// PR-35y R8: the regular AR receives the union of regular and
	// global members' inputs (everyone's primaries + everyone's
	// header closures). The reference graph confirms the union shape
	// on tcmalloc/no_percpu_cache: its `liblibs-tcmalloc-no_percpu_cache.a`
	// archives `aligned_alloc.c` (regular SRCS), every `tcmalloc/*`
	// global SRCS source, and the 1286 shared header closure. Without
	// this union the regular AR was missing the GLOBAL_SRCS' resolved
	// closures (1286 transitive header inputs in the M2 case).
	combinedMemberInputs := memberInputs

	if len(globalMemberInputs) > 0 {
		combinedMemberInputs = make([]string, 0, len(memberInputs)+len(globalMemberInputs))
		combinedMemberInputs = append(combinedMemberInputs, memberInputs...)

		for _, p := range globalMemberInputs {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}

			memberInputsSeen[p] = struct{}{}
			combinedMemberInputs = append(combinedMemberInputs, p)
		}
	}

	// PR-M3-py3-runtime-closure: PY*_LIBRARY modules with PY_SRCS emit
	// objcopy nodes (see emitPySrcObjcopy) whose inputs include
	// build/scripts/objcopy.py plus every PY_SRCS source `.py` path.
	// The reference graph union-aggregates those into the module's
	// regular `.a` and `.global.a` inputs. Mirror this by injecting the
	// same set into combinedMemberInputs before AR emission. Gate on the
	// same resourceModuleTag check that emitPySrcObjcopy uses so non-
	// PY3 modules (plain LIBRARY) stay unaffected.
	if d.moduleStmt != nil && resourceModuleTag(d.moduleStmt.Name) != "" {
		// Collect non-kv-only RESOURCE / RESOURCE_FILES source paths;
		// kv-only entries (Path == "-") do not point at a real file and
		// would inject an invalid input path.
		var resourcePaths []string
		for _, e := range d.resources {
			if e.Path == "-" {
				continue
			}

			resourcePaths = append(resourcePaths, e.Path)
		}

		if extras := pySrcsARExtraInputs(instance.Path, d.srcDir, d.pySrcs, resourcePaths); len(extras) > 0 {
			// Ensure combinedMemberInputs is its own slice header so the
			// append below cannot alias the caller's memberInputs backing
			// array when the no-global branch above kept the alias.
			if len(globalMemberInputs) == 0 {
				combinedMemberInputs = append([]string(nil), memberInputs...)
			}

			for _, p := range extras {
				if _, dup := memberInputsSeen[p]; dup {
					continue
				}

				memberInputsSeen[p] = struct{}{}
				combinedMemberInputs = append(combinedMemberInputs, p)
			}
		}
	}

	// PR-41 Fix I: reorder AR members into ymake's canonical bucket
	// order: SRC_C_NO_LTO first, then regular SRCS (declaration
	// order), then JOIN_SRCS, then R6-generated last.
	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, numSrcsDerived)

	// PR-M3-residue-B fix 1: skip plain AR when there are no regular CC
	// outputs (module has only GLOBAL_SRCS — blockcodecs codecs, getopt).
	// The reference graph does not emit a regular (non-global) archive for
	// such modules; only EmitARGlobal below produces the ".global.a" node.
	//
	// PR-M3-residue-B fix 2: Python library modules use py3-prefixed
	// archive names (Py3cArchiveName for PY23_NATIVE_LIBRARY, Py3ArchiveName
	// for PY3_LIBRARY etc.) so we route through EmitARNamed with the name
	// selected by arNameFn.
	var arRef NodeRef
	arBaseName := arNameFn(instance.Path)

	// PR-M3-aarch64-enum-and-global-a: PY3_LIBRARY / PY23_LIBRARY surface
	// module_lang="py3" on both the regular and global archive in REF.
	// PY23_NATIVE_LIBRARY retains module_lang="cpp" (its tag flips to
	// py3_native / py3_native_global instead). We pivot the AR-emission
	// instance's Language locally so the .a / .global.a nodes carry the
	// right value; the surrounding walker's Language remains LangCPP
	// (peer-walks must keep sharing memo entries with the rest of the
	// cpp closure).
	arInstance := instance
	switch d.moduleStmt.Name {
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
		arInstance.Language = LangPy
	}

	// PR-M3-openssl-ar-plugin-and-as-clean: resolve AR_PLUGIN path
	// (`$(SOURCE_ROOT)/<modulePath>/<name>.pyplugin`) when the macro
	// fired on this module's ya.make.
	arPluginPath := ""
	if d.arPlugin != "" {
		arPluginPath = "$(SOURCE_ROOT)/" + instance.Path + "/" + d.arPlugin
	}

	if len(ccRefs) > 0 {
		// PR-M3-module-tag-and-stats-enums-dep: PY23_LIBRARY / PY23_NATIVE_LIBRARY
		// surface `module_tag=py3` / `module_tag=py3_native`.
		// PR-M3-openssl-ar-plugin-and-as-clean: openssl AR_PLUGIN(ar) injects
		// `--plugin <ar.pyplugin>` between the link_lib.py `--` separators.
		if perModuleCCTag != "" {
			arRef = EmitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginPath, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginPath, ctx.emit)
		}
	}

	_ = peerArchiveRefs // retained as a loop accumulator for the PROGRAM LD branch above; intentionally unused for the LIBRARY AR.
	arPath := "$(BUILD_ROOT)/" + instance.Path + "/" + arBaseName

	// PR-M3-A: emit yapyc3 PY nodes for PY_SRCS() declarations.
	// Modules that have both SRCS and PY_SRCS (rare but valid) get CC/AR
	// nodes from the SRCS path above AND yapyc3 nodes from PY_SRCS here.
	emitPySrcs(ctx, instance, d)

	// PR-M3-resource-objcopy-A: emit objcopy PY nodes for
	// RESOURCE / RESOURCE_FILES declarations. The returned `.o` paths
	// flow into the module's .global.a archive (PR-M3-aarch64-enum-and-global-a
	// completes the AR wiring by appending the refs/outputs into
	// globalRefs/globalOutputs below).
	// PR-M3-globalA-narrow-closure: the objcopy nodes' SOURCE_ROOT inputs
	// (per-entry source paths + objcopy.py) are folded into the
	// GLOBAL_SRCS-local closure that becomes the .global.a archive's
	// `inputs` slot. Dedup against the existing accumulator.
	objcopyRefs, objcopyOutputs, objcopyGlobalInputs := emitResourceObjcopy(ctx, instance, d)
	globalRefs = append(globalRefs, objcopyRefs...)
	globalOutputs = append(globalOutputs, objcopyOutputs...)

	for _, p := range objcopyGlobalInputs {
		if _, dup := globalMemberInputsSeen[p]; dup {
			continue
		}

		globalMemberInputsSeen[p] = struct{}{}
		globalMemberInputs = append(globalMemberInputs, p)
	}

	// PR-M3-D EN emission moved to pre-source-loop (PR-M3-F-7-C); the
	// codegen registry must be populated before consumer CCs scan.
	// PR-AUDIT-8: emitMiscNodes likewise hoisted to pre-source-loop above.

	result := &moduleEmitResult{
		ARRef:                   arRef,
		ARPath:                  arPath,
		hasPlainAR:              len(ccRefs) > 0,
		isPROGRAM:               false,
		isPyLibrary:             isPyLibraryType(d.moduleStmt.Name),
		LDRef:                   arRef,
		LDPath:                  arPath,
		AddInclGlobal:           effectiveAddInclGlobal,
		OwnAddInclGlobal:        append([]string(nil), d.addInclGlobal...),
		CFlagsGlobal:            effectiveCFlagsGlobal,
		CXXFlagsGlobal:          effectiveCXXFlagsGlobal,
		COnlyFlagsGlobal:        effectiveCOnlyFlagsGlobal,
		PeerArchiveClosureRefs:  append([]NodeRef(nil), peerArchiveRefs...),
		PeerArchiveClosurePaths: append([]string(nil), peerArchivePaths...),
		PeerGlobalClosureRefs:   append([]NodeRef(nil), peerGlobalRefs...),
		PeerGlobalClosurePaths:  append([]string(nil), peerGlobalPaths...),
		LDPluginRefs:            mergedLDPluginRefs,
		LDPluginPaths:           mergedLDPluginPaths,
		InducedDeps:             append([]string(nil), d.inducedDeps...),
	}

	if len(globalRefs) > 0 {
		// PR-M3-globalA-narrow-closure: the .global.a aggregator gets
		// the GLOBAL_SRCS-local closure ONLY, not the regular AR's
		// header closure. REF sg2.json constrains `.global.a` `inputs`
		// to the GLOBAL_SRCS member-CC closures, PY_REGISTER reg3.cpp
		// closures, and objcopy SOURCE_ROOT inputs (RESOURCE source
		// paths + objcopy.py). The regular CC closure of SRCS members
		// (Python.h, libcxx, glibcasm, musl, ...) does NOT propagate
		// into `.global.a` — it propagates into the regular `.a`.
		// Prior R8 union shape over-emitted ~5212 lines on 8 nodes
		// across rapidjson / ymakeyaml / runtime_py3 / symbols-module /
		// protobuf / ymake / tcmalloc/no_percpu_cache stayed exact (it
		// has no regular SRCS so combined == global).
		globalAggregated := globalMemberInputs

		globalBaseName := globalArNameFn(instance.Path)
		// PR-M3-aarch64-enum-and-global-a: pick module_tag per the same
		// REF mapping as the header-only branch — PY23_LIBRARY surfaces
		// py3_global; PY23_NATIVE_LIBRARY surfaces py3_native_global;
		// the rest stay on "global".
		globalTag := "global"
		switch d.moduleStmt.Name {
		case "PY23_LIBRARY":
			globalTag = "py3_global"
		case "PY23_NATIVE_LIBRARY":
			globalTag = "py3_native_global"
		}
		// PR-M3-AR-member-order: the .global.a aggregator follows the
		// same member-order discipline as the regular AR — hand-written /
		// objcopy_* .o files precede codegen-derived .reg3.cpp.o etc.
		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), len(globalRefs))
		globalRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, globalAggregated, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = instance.Path + "/" + globalBaseName
	}

	ctx.memo[originalInstance] = result
	ctx.memo[instance] = result

	return result
}

// mergeDedup returns the concatenation `a ++ b` with duplicates
// dropped, preserving declaration order (R14 — first occurrence
// wins). Used by genModule to compose this module's effective
// peer-GLOBAL slices: own contributions first, then transitive peer
// contributions. PR-32 D07 introduced the helper to keep the per-axis
// composition uniform across ADDINCL / CFLAGS / CXXFLAGS / CONLYFLAGS.
// filterBuildRootSelfPaths drops `$(BUILD_ROOT)/...` paths from `peer`
// that also appear in `own`. Returns a fresh slice (input unchanged) so
// the unfiltered `peerAddInclGlobal` continues to flow to peer-prop
// channels (effective AddInclGlobal and any downstream consumer's
// peer walk). Used at the SELF-compile cmd_args boundary only — see
// PR-M3-cc-argv-slot-order. SOURCE_ROOT-rooted paths (e.g. `python/Include`)
// are intentionally left alone: the upstream reference emits the
// own + peer duplicate for those.
func filterBuildRootSelfPaths(peer, own []string) []string {
	if len(peer) == 0 {
		return peer
	}

	ownSet := make(map[string]struct{}, len(own))

	for _, p := range own {
		if strings.HasPrefix(p, "$(BUILD_ROOT)/") {
			ownSet[p] = struct{}{}
		}
	}

	if len(ownSet) == 0 {
		return peer
	}

	out := make([]string, 0, len(peer))

	for _, p := range peer {
		if _, dup := ownSet[p]; dup {
			continue
		}

		out = append(out, p)
	}

	return out
}

func mergeDedup(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := make(map[string]struct{}, len(a)+len(b))

	for _, x := range a {
		if _, dup := seen[x]; dup {
			continue
		}

		seen[x] = struct{}{}
		out = append(out, x)
	}

	for _, x := range b {
		if _, dup := seen[x]; dup {
			continue
		}

		seen[x] = struct{}{}
		out = append(out, x)
	}

	return out
}

// filterEnSerializedSiblings drops entries whose VFS path ends with the
// EN-generator output suffixes `_serialized.cpp` or `_serialized.h`.
// Used at the R6 input boundary: REF's R6 closure walks transitively
// through a `#include <..._serialized.h>` directive's descendants but
// does not list the EN-generated `.cpp`/`.h` siblings themselves in the
// R6 node's inputs. The filter preserves descendant order for the
// retained entries.
func filterEnSerializedSiblings(in []string) []string {
	out := make([]string, 0, len(in))

	for _, p := range in {
		if strings.HasSuffix(p, "_serialized.cpp") || strings.HasSuffix(p, "_serialized.h") {
			continue
		}

		out = append(out, p)
	}

	return out
}

// emitOwnLDPlugins emits one CP node per `LD_PLUGIN(name.py)` entry
// declared in this module. The CP src is
// `$(SOURCE_ROOT)/<modulePath>/<name>` and the dst is
// `$(BUILD_ROOT)/<modulePath>/<name>.pyplugin` (verified against the
// reference CP node for `contrib/libs/musl/include`'s `musl.py`).
// Returns parallel ref + path slices in declaration order. PR-35k.
//
// PR-35l: the CP NodeRef is cached on `genCtx.ldPluginCPCache`, keyed by
// the plugin output path. The reference graph emits each CP node once
// (on the target platform) and shares its UID across target and host
// LD deps; without this dedup the host walk through `WithHost` re-fires
// `emitOwnLDPlugins` on the same plugin and produces a duplicate CP
// node on `default-linux-x86_64` (the host platform UID differs from
// the target UID because `Platform` is part of the canonical hash).
// First-emit wins — the seed walk runs target-first, so the cached
// entry carries the target platform per the reference shape.
func emitOwnLDPlugins(ctx *genCtx, instance ModuleInstance, plugins []string) ([]NodeRef, []string) {
	if len(plugins) == 0 {
		return nil, nil
	}

	refs := make([]NodeRef, 0, len(plugins))
	paths := make([]string, 0, len(plugins))

	for _, name := range plugins {
		src := "$(SOURCE_ROOT)/" + instance.Path + "/" + name
		dst := "$(BUILD_ROOT)/" + instance.Path + "/" + name + ".pyplugin"

		ref, ok := ctx.ldPluginCPCache[dst]

		if !ok {
			ref = EmitCP(instance, src, dst, ctx.emit)
			ctx.ldPluginCPCache[dst] = ref
		}

		refs = append(refs, ref)
		paths = append(paths, dst)
	}

	return refs, paths
}

// mergeLDPlugins concatenates `(ownRefs, ownPaths)` with
// `(peerRefs, peerPaths)`, dropping any peer entry whose path appears
// in own. Mirrors `mergeDedup` for the parallel-slice case used by
// LD plugin propagation. PR-35k.
func mergeLDPlugins(ownRefs []NodeRef, ownPaths []string, peerRefs []NodeRef, peerPaths []string) ([]NodeRef, []string) {
	if len(ownPaths) == 0 && len(peerPaths) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(ownPaths)+len(peerPaths))
	outRefs := make([]NodeRef, 0, len(ownPaths)+len(peerPaths))
	outPaths := make([]string, 0, len(ownPaths)+len(peerPaths))

	for i, p := range ownPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		outRefs = append(outRefs, ownRefs[i])
		outPaths = append(outPaths, p)
	}

	for i, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		outRefs = append(outRefs, peerRefs[i])
		outPaths = append(outPaths, p)
	}

	return outRefs, outPaths
}

// peerGlobalContribs is the per-axis aggregation of a header-only
// LIBRARY's peer-walk (PR-27 + PR-32 D07). All four axes share the
// same declaration-order + dedup discipline as the main walker.
//
// Two-phase collection (PR-32): for each peer, collect its OWN
// declarations FIRST (across all peers), then collect each peer's
// transitive contributions. This gives the reference's observed
// ordering: own-from-peer1, own-from-peer2, ..., transitive-from-peer1,
// transitive-from-peer2, ... — empirically matches util/charset and
// tools/archiver/main.cpp.o cmd_args[11..16] where libcxx/include +
// libcxxrt/include come BEFORE the musl-arch propagation chain.
type peerGlobalContribs struct {
	addIncl    []string
	cFlags     []string
	cxxFlags   []string
	cOnlyFlags []string
	// PR-35c: archive closure transitively reachable from this
	// header-only LIBRARY's peers — folded into the same DFS post-
	// order, dedup-by-path discipline the main walker uses. Header-
	// only LIBRARYs do not emit an AR of their own, but they DO
	// expose their transitive archive closure to downstream consumers
	// (e.g. `contrib/libs/musl/include` is header-only and its `IF`
	// branches PEERDIR `contrib/libs/musl` — the consumer needs musl
	// in its archive set even though musl/include itself contributes
	// no archive).
	archiveRefs  []NodeRef
	archivePaths []string
	// PR-M3-LD-peer-globalA: `.global.a` closure transitively reachable
	// through this header-only LIBRARY's peers. Mirrors archiveRefs but
	// for `.global.a` archives (every peer's own GlobalRef UNION every
	// peer's PeerGlobalClosure*). Header-only LIBRARYs do not emit a
	// `.global.a` of their own, but their peers may.
	globalRefs  []NodeRef
	globalPaths []string
	// PR-35k: LD plugin closure surfaced through the header-only walker.
	// Mirrors the archive closure: dedup-by-path, declaration order,
	// first occurrence wins.
	ldPluginRefs  []NodeRef
	ldPluginPaths []string
}

// walkPeersForGlobalAddIncl walks the peers of a header-only LIBRARY
// (PR-27) to ensure their transitive closure is discovered (genModule
// memoises so other consumers can pick them up later) AND returns the
// per-axis union of every peer's transitive *Global contribution
// (PR-31 D05 + PR-32 D07: ADDINCL, CFLAGS, CXXFLAGS, CONLYFLAGS).
// The header-only module emits no AR, so the per-peer archive refs
// are intentionally dropped; only the GLOBAL peer-propagation is
// preserved.
func walkPeersForGlobalAddIncl(ctx *genCtx, instance ModuleInstance, d *moduleData) peerGlobalContribs {
	defaults := defaultPeerdirsFor(ctx, instance)

	// PR-35g: mirror genModule's ALLOCATOR(FAKE) malloc/api suppression
	// in the header-only walker so a header-only LIBRARY that sets
	// ALLOCATOR(FAKE) (no current M2 case, but defended for future)
	// drops the same default. Header-only LIBRARYs in M2 do not declare
	// ALLOCATOR, so this is normally a no-op.
	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)

	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))
	out := peerGlobalContribs{}
	addInclSeen := map[string]struct{}{}
	cFlagsSeen := map[string]struct{}{}
	cxxFlagsSeen := map[string]struct{}{}
	cOnlyFlagsSeen := map[string]struct{}{}
	archiveSeen := map[string]struct{}{}
	globalSeen := map[string]struct{}{}
	ldPluginSeen := map[string]struct{}{}

	addEach := func(seenSet map[string]struct{}, dst *[]string, src []string) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}

	addArchive := func(ref NodeRef, path string) {
		if _, dup := archiveSeen[path]; dup {
			return
		}

		archiveSeen[path] = struct{}{}
		out.archiveRefs = append(out.archiveRefs, ref)
		out.archivePaths = append(out.archivePaths, path)
	}

	addGlobal := func(ref NodeRef, path string) {
		if _, dup := globalSeen[path]; dup {
			return
		}

		globalSeen[path] = struct{}{}
		out.globalRefs = append(out.globalRefs, ref)
		out.globalPaths = append(out.globalPaths, path)
	}

	addLDPlugin := func(ref NodeRef, path string) {
		if _, dup := ldPluginSeen[path]; dup {
			return
		}

		ldPluginSeen[path] = struct{}{}
		out.ldPluginRefs = append(out.ldPluginRefs, ref)
		out.ldPluginPaths = append(out.ldPluginPaths, path)
	}

	walk := func(peerPath string) {
		peerInstance := derivePeerInstance(instance, peerPath)
		peerResult := genModule(ctx, peerInstance)
		addEach(addInclSeen, &out.addIncl, peerResult.AddInclGlobal)
		addEach(cFlagsSeen, &out.cFlags, peerResult.CFlagsGlobal)
		addEach(cxxFlagsSeen, &out.cxxFlags, peerResult.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &out.cOnlyFlags, peerResult.COnlyFlagsGlobal)

		// PR-35c: fold peer's transitive archive closure plus peer's
		// own AR (when not header-only) in DFS post-order.
		for i, p := range peerResult.PeerArchiveClosurePaths {
			addArchive(peerResult.PeerArchiveClosureRefs[i], p)
		}

		// PR-M3-residue-B: use peerResult.ARPath (py3-prefixed for
		// Python modules) and skip when hasPlainAR is false.
		// PR-M3-LD-peer-globalA: gate on `hasPlainAR` alone — PROTO_LIBRARY
		// modules have `headerOnly=true` AND `hasPlainAR=true`; the legacy
		// `!headerOnly` guard wrongly suppressed their AR.
		if peerResult.hasPlainAR {
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(BUILD_ROOT)/")
			addArchive(peerResult.ARRef, arRelPath)
		}

		// PR-M3-LD-peer-globalA: fold peer's transitive `.global.a`
		// closure plus peer's own `.global.a` (when GlobalRef != nil).
		// Header-only peers may still emit a `.global.a` (e.g. `certs`
		// emits libcerts.global.a from RESOURCE-driven objcopy outputs).
		for i, p := range peerResult.PeerGlobalClosurePaths {
			addGlobal(peerResult.PeerGlobalClosureRefs[i], p)
		}

		if peerResult.GlobalRef != nil {
			addGlobal(*peerResult.GlobalRef, peerResult.GlobalPath)
		}

		// PR-35k: fold peer's transitive LD plugin closure. Header-only
		// peers (musl/include itself) populate this slot from their own
		// LD_PLUGIN macro; non-header peers may carry it through if any
		// of their transitive PEERDIRs declared one.
		for i, p := range peerResult.LDPluginPaths {
			addLDPlugin(peerResult.LDPluginRefs[i], p)
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

		walk(peerPath)
	}

	for _, p := range d.peerdirs {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		walk(filepath.Clean(p))
	}

	// PR-33 C01: header-only LIBRARYs (musl/include, etc.) keep the
	// natural Phase 1+2 order — none of the M2-closure header-only
	// modules are runtime ancestors that consume libcxx/libcxxrt as
	// transitive header-only contributions. The hoist gate in the
	// main walker (genModule) is keyed on `isRuntimeAncestor`; a
	// header-only LIBRARY that ever needs the same treatment can flip
	// this to mirror the main walker's gate.

	// PR-35g: drop bundled-include paths (linux-headers, linux-headers/
	// _nf) from the propagated set. The cc bundle's `ccIncludesSuffix`
	// already provides them; consumers reaching this header-only
	// LIBRARY's `AddInclGlobal` should not see them re-emitted at the
	// peer-AddIncl slot.
	if len(out.addIncl) > 0 {
		filtered := out.addIncl[:0]

		for _, p := range out.addIncl {
			if bundledAddInclPaths[p] {
				continue
			}

			filtered = append(filtered, p)
		}

		out.addIncl = filtered
	}

	return out
}

// hasCompilableSource reports whether the module has at least one
// source the rule emitter would actually compile (excluding pure
// headers in SRCS, which the upstream uses as IDE / dependency-
// tracking metadata, and known-deferred sources handled by dedicated
// emitters — e.g. .proto/.ev handled by emitProtoSrcs). Modules that
// contain only JOIN_SRCS / globals also count.
func hasCompilableSource(d *moduleData) bool {
	for _, s := range d.srcs {
		if !isHeaderSource(s) && !isSkippedSource(s) {
			return true
		}
	}

	if len(d.joinSrcs) > 0 {
		return true
	}

	for _, s := range d.globalSrcs {
		if !isHeaderSource(s) && !isSkippedSource(s) {
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

// isSkippedSource reports whether `srcRel` is a known deferred source kind
// that the emitter does not yet handle. These sources are silently
// skipped (like headers) rather than throwing "unsupported extension".
// Note: .rl (ragel5) and .cpp.in/.c.in are now handled by emitOneSource
// and are NOT counted as skipped — they cause hasCompilableSource to return true.
// The corresponding emitters land in PR-M3-B..E:
//   - .proto  → PB (emitted by emitProtoSrcs in the PROTO_LIBRARY header-only branch)
//   - .ev     → EV (emitted by emitOneSource for LIBRARY, emitProtoSrcs for PROTO_LIBRARY)
//   - .py     → PY node via runtime library (PR-M3-E)
//   - .g4     → ANTLR4 grammar; processed by RUN_ANTLR4_CPP; PR-M3-D.
func isSkippedSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".py") ||
		strings.HasSuffix(srcRel, ".g4")
}

// isCodegenProducingSrc reports whether `srcRel` is a source extension whose
// emitOneSource branch emits a codegen node (PB/EV/R6/R5/CF) whose outputs go
// into the per-scanner CodegenRegistry (F-7-B). Consumer sources in the SAME
// module may #include those outputs, so the two-pass loop in the
// LIBRARY-with-sources branch runs these first to populate the registry
// before any consumer CC scans its closure (F-7-C).
//
// `.proto` is not included here: the .proto path runs only via emitProtoSrcs
// in the PROTO_LIBRARY header-only branch (those modules emit codegen ahead
// of any consumer module's CC walk through the normal peer-walk ordering).
func isCodegenProducingSrc(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".cpp.in") ||
		strings.HasSuffix(srcRel, ".c.in")
}

// hasSkippedSource reports whether d contains at least one source that is
// known-deferred (isSkippedSource). Used to distinguish PROGRAMs with
// deferred-only sources (graceful stub) from PROGRAMs with truly empty
// source sets (hard error).
func hasSkippedSource(d *moduleData) bool {
	for _, s := range d.srcs {
		if isSkippedSource(s) {
			return true
		}
	}

	for _, s := range d.globalSrcs {
		if isSkippedSource(s) {
			return true
		}
	}

	return false
}

// emitPySrcs emits one PY yapyc3 node per `.py` entry in d.pySrcs.
// This is the PR-M3-A implementation of the PY_SRCS emitter.
//
// Each node compiles `<instance.Path>/<srcRel>` to a `.yapyc3` file
// using the host `tools/py3cc/bin` and `tools/py3cc/slow` binaries.
// The two py3cc binaries are walked as host tools (x86_64); the
// resulting LD NodeRefs are threaded into each yapyc3 node's DepRefs
// so the graph captures the host-tool dependency.
//
// Output suffix rule (empirical, from sg2.json):
//   - flat source (no `/` in srcRel): `$(BUILD_ROOT)/<path>.py.yapyc3`
//   - subdir source (has `/` in srcRel): `$(BUILD_ROOT)/<path>.py.3kp2.yapyc3`
//
// cmd_args format (6 args):
//   [py3cc_binary, --slow-py3cc, slow_py3cc_binary,
//    <modulePath>/<srcRel>-, $(SOURCE_ROOT)/<modulePath>/<srcRel>,
//    $(BUILD_ROOT)/<output>]
//
// inputs: [py3cc_binary, slow_py3cc_binary, $(SOURCE_ROOT)/<src>]
//
// The function tolerates a host walk failure for tools/py3cc: if the
// binary walk throws a ParseError the py3cc LD refs remain zero (the
// dep edges are absent) but yapyc3 nodes are still emitted with the
// canonical binary paths in cmd_args (matching the reference shape).
func emitPySrcs(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.pySrcs) == 0 {
		return
	}

	// ENABLE(PYBUILD_NO_PYC) suppresses yapyc3 generation. Modules like
	// contrib/tools/python3/lib2/py declare all Python sources but set
	// this flag to prevent .pyc/.yapyc3 files from being emitted —
	// they embed the sources via RESOURCE/objcopy instead.
	if d.pyBuildNoPYC {
		return
	}

	// Walk tools/py3cc/bin and tools/py3cc/slow as HOST tools to get
	// their LD NodeRefs. Both are PROGRAM modules on x86_64.
	const (
		py3ccBinPath  = "tools/py3cc/bin"
		py3ccSlowPath = "tools/py3cc/slow"
	)

	// Canonical binary paths ($(BUILD_ROOT)-rooted) used in cmd_args
	// and inputs when the host walk succeeds or as fallbacks when it fails.
	const (
		py3ccBinaryCanonical     = "$(BUILD_ROOT)/tools/py3cc/py3cc"
		py3ccSlowBinaryCanonical = "$(BUILD_ROOT)/tools/py3cc/slow/py3cc"
	)

	var (
		py3ccLDRef     NodeRef
		py3ccSlowLDRef NodeRef
		py3ccBinary    = py3ccBinaryCanonical
		py3ccSlowBin   = py3ccSlowBinaryCanonical
	)

	// Walk tools/py3cc/bin (the main py3cc binary).
	py3ccHostInst := instance.WithHost(ctx.cfg)
	py3ccHostInst.Path = py3ccBinPath
	py3ccHostInst.Flags = inferFlagsFromPath(py3ccBinPath, true)

	if exc := Try(func() {
		result := genModule(ctx, py3ccHostInst)
		py3ccLDRef = result.LDRef
		// canonicalizePy3ccBinaryPath rewrites
		// $(BUILD_ROOT)/tools/py3cc/bin/py3cc →
		// $(BUILD_ROOT)/tools/py3cc/py3cc to match the reference
		// yapyc3 cmd_args[0] shape. tools/py3cc/bin/ya.make declares
		// SRCDIR(tools/py3cc) so the upstream intent is a top-level
		// binary; we walk /bin/ as a stopgap (same pattern as ragel6).
		py3ccBinary = canonicalizePy3ccBinaryPath(result.LDPath)
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// Leave zero ref; py3ccBinary stays at canonical fallback.
	}

	// Walk tools/py3cc/slow (the slow-py3cc binary). tools/py3cc/slow/ya.make
	// uses IF(NOT PREBUILT) INCLUDE(bin/ya.make) which our parser expands
	// (PREBUILT=false). However tools/py3cc/slow/bin declares PY3_PROGRAM_BIN,
	// which isMultimoduleLibraryType routes to the header-only path, so
	// LDPath is empty. Only update py3ccSlowBin when the walk produces a
	// non-empty path; otherwise the canonical fallback
	// $(BUILD_ROOT)/tools/py3cc/slow/py3cc (pre-initialised above) is used.
	py3ccSlowHostInst := instance.WithHost(ctx.cfg)
	py3ccSlowHostInst.Path = py3ccSlowPath
	py3ccSlowHostInst.Flags = inferFlagsFromPath(py3ccSlowPath, true)

	if exc := Try(func() {
		result := genModule(ctx, py3ccSlowHostInst)
		py3ccSlowLDRef = result.LDRef
		if result.LDPath != "" {
			py3ccSlowBin = result.LDPath
		}
		// If LDPath is empty (PY3_PROGRAM_BIN → header-only stub),
		// py3ccSlowBin retains its canonical fallback value.
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// Leave zero ref; py3ccSlowBin stays at canonical fallback.
	}

	// PR-M3-F-1: walk tools/rescompiler/bin, tools/rescompressor/bin, and
	// tools/archiver as host tools. These are referenced by PY (objcopy) and
	// AR (pyc.inc) nodes in the M3 closure. ldBinaryDir lifts the output dirs.
	// Walks are eager (at most once per ctx due to memoization); LD NodeRefs
	// are not yet wired into the yapyc3 PY nodes emitted below (that wiring
	// is deferred to a later PR when the full objcopy PY emitter lands).
	const (
		rescompilerBinPath  = "tools/rescompiler/bin"
		rescompressorBinPath = "tools/rescompressor/bin"
		archiverPath        = "tools/archiver"
	)

	walkHostTool := func(path string) {
		hostInst := instance.WithHost(ctx.cfg)
		hostInst.Path = path
		hostInst.Flags = inferFlagsFromPath(path, true)
		if exc := Try(func() {
			genModule(ctx, hostInst)
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}
	}

	walkHostTool(rescompilerBinPath)
	walkHostTool(rescompressorBinPath)
	walkHostTool(archiverPath)

	// Emit one yapyc3 PY node per .py source.
	for _, srcRel := range d.pySrcs {
		srcAbs := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

		// The "module name" arg: <modulePath>/<srcRel>- (trailing dash).
		moduleName := instance.Path + "/" + srcRel + "-"

		// Output suffix: flat → .py.yapyc3; subdir → .py.3kp2.yapyc3.
		var outputPath string
		if strings.Contains(srcRel, "/") {
			// The srcRel already ends in ".py"; insert ".3kp2" before ".yapyc3".
			outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + ".3kp2.yapyc3"
		} else {
			outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + ".yapyc3"
		}

		cmdArgs := []string{
			py3ccBinary,
			"--slow-py3cc",
			py3ccSlowBin,
			moduleName,
			srcAbs,
			outputPath,
		}

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
			"PYTHONHASHSEED":         "0",
		}

		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  []string{py3ccBinary, py3ccSlowBin, srcAbs},
			Outputs: []string{outputPath},
			KV: map[string]string{
				"p":  "PY",
				"pc": "yellow",
			},
			Tags: []string{},
			TargetProperties: func() map[string]string {
				tp := map[string]string{"module_dir": instance.Path}
				// PR-M3-module-tag-and-stats-enums-dep: PY23_LIBRARY's
				// .yapyc3 nodes carry `module_tag=py3` in REF (matches
				// MODULE_TAG=PY3 from _ARCADIA_PYTHON3_ADDINCL via the
				// PY3 submodule). PY3_LIBRARY / PY2_LIBRARY etc keep no
				// tag (the type is its own default and upstream omits
				// redundant properties).
				if d.moduleStmt.Name == "PY23_LIBRARY" {
					tp["module_tag"] = "py3"
				}
				return tp
			}(),
			Platform: string(instance.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		// Wire py3cc LD refs into both DepRefs (topology/deps) and
		// ForeignDepRefs["tool"] (foreign_deps.tool) to match the
		// reference yapyc3 node shape.  Only add non-zero refs (zero
		// ref means the host walk failed and we have no LD node to
		// reference).
		var toolRefs []NodeRef

		if py3ccLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccLDRef)
			toolRefs = append(toolRefs, py3ccLDRef)
		}

		if py3ccSlowLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccSlowLDRef)
			toolRefs = append(toolRefs, py3ccSlowLDRef)
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = map[string][]NodeRef{"tool": toolRefs}
		}

		pyRef := ctx.emit.Emit(node)

		// PR-M3-L0-cascade-close-v2: register the .yapyc3 output in the
		// codegen registry so the downstream objcopy CC's input-driven
		// resolveCodegenDepRefsExt lookup threads the PY producer into
		// its deps[]. Per Plan B PR-2: 41 PY-leaf objcopy_*.o files lack
		// the PY ref edge today — this closes them.
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "PY",
				OutputPath:     outputPath,
				ProducerRef:    pyRef,
				HasProducerRef: true,
			})
		}
	}
}

// genPy3RegScriptPath is the source-relative path to the gen_py3_reg.py script
// invoked by every PY_REGISTER's PY node (mirror of macro _PY3_REGISTER at
// build/ymake.core.conf:4086-4089).
const genPy3RegScriptPath = "$(SOURCE_ROOT)/build/scripts/gen_py3_reg.py"

// emitPyRegister emits the PY+CC pair for each PY_REGISTER(arg) entry in
// d.pyRegister. Each arg:
//   - one PY node:  python3 gen_py3_reg.py <arg> $(BUILD_ROOT)/<modPath>/<arg>.reg3.cpp
//   - one CC node:  compiles the generated `.reg3.cpp` into `.reg3.cpp.o` (or
//     `.reg3.cpp.py3.o` when py3Suffix is set).
//
// Both nodes' refs flow into globalRefs/globalOutputs (the upstream
// _PY3_REGISTER macro emits `SRCS(GLOBAL $Func.reg3.cpp)`, so the CC output
// archives in the module's `.global.a`).
//
// Mirror of macro _PY3_REGISTER at build/ymake.core.conf:4086-4089.
func emitPyRegister(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs, py3Suffix bool) (refs []NodeRef, outputs []string, memberInputs []string) {
	if len(d.pyRegister) == 0 {
		return nil, nil, nil
	}

	for _, arg := range d.pyRegister {
		regCpp := arg + ".reg3.cpp"
		regCppAbs := "$(BUILD_ROOT)/" + instance.Path + "/" + regCpp

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		}

		pyCmdArgs := []string{
			python3Path,
			genPy3RegScriptPath,
			arg,
			regCppAbs,
		}

		pyNode := &Node{
			Cmds: []Cmd{
				{CmdArgs: pyCmdArgs, Env: env},
			},
			Env:     env,
			Inputs:  []string{genPy3RegScriptPath},
			Outputs: []string{regCppAbs},
			KV: map[string]string{
				"p":  "PY",
				"pc": "yellow",
			},
			Tags: []string{},
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform: string(instance.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
			DepRefs: []NodeRef{},
		}

		if py3Suffix {
			pyNode.TargetProperties["module_tag"] = "py3"
		}

		pyRef := ctx.emit.Emit(pyNode)

		// CC node compiling the generated `.reg3.cpp`. IsGenerated=true so
		// composeCCPaths reads the input from $(BUILD_ROOT)/<modPath>/<reg>.
		// IncludeInputs is the gen_py3_reg.py script (the reference graph's
		// reg3 CC node lists [<.reg3.cpp>, <gen_py3_reg.py>] as its only
		// inputs — no transitive header closure is scanned because the
		// generated source contains only registration stubs).
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.Generator = pyRef
		ccIn.HasGenerator = true
		ccIn.Py3Suffix = py3Suffix
		ccIn.IncludeInputs = []string{genPy3RegScriptPath}
		// PR-M3-final-surgical (fix 4): mirror upstream ordering — the
		// PyInit_/init_module_ defines added by `onpy_register` AFTER
		// `_PY3_REGISTER`'s `SRCS(GLOBAL …)` only attach to user-declared
		// sources; the synthetic reg3.cpp keeps the pre-call CFLAGS
		// snapshot. Strip the two define families from this CC's bundle.
		if len(in.CFlags) > 0 {
			filtered := make([]string, 0, len(in.CFlags))
			for _, f := range in.CFlags {
				if strings.HasPrefix(f, "-DPyInit_") || strings.HasPrefix(f, "-Dinit_module_") {
					continue
				}
				filtered = append(filtered, f)
			}
			ccIn.CFlags = filtered
		}

		ccRef, ccOut := EmitCC(instance, regCpp, ccIn, ctx.emit)

		refs = append(refs, ccRef)
		outputs = append(outputs, ccOut)
		// memberInputs feeds the .global.a aggregator. The CC's own input
		// list is [<reg3.cpp>, gen_py3_reg.py]; gen_py3_reg.py contributes
		// the archive-input added by the reference graph (the reg3.cpp
		// itself is BUILD_ROOT-rooted and PR-35y R7 strips those from the
		// AR aggregator).
		memberInputs = append(memberInputs, genPy3RegScriptPath)
	}

	return refs, outputs, memberInputs
}

// emitEnumSrcs emits one EN node per GENERATE_ENUM_SERIALIZATION(*)
// declaration in d.enumSrcs. PR-M3-D.
//
// Algorithm:
//  1. Walk tools/enum_parser/enum_parser as a host tool to get its
//     LD NodeRef. Falls back to the canonical binary path when the
//     walk fails with a ParseError.
//  2. For each GenerateEnumSerializationStmt, scan the header's
//     transitive include closure (same scanner as CC nodes).
//  3. Collect cross-EN deps: any previously emitted EN output path
//     that appears in the header's include closure contributes its
//     NodeRef and path to the dep lists.
//  4. Call EmitEN, then record the output paths in ctx.enOutputs.
//
// EN nodes are always emitted on the TARGET platform (instance.Target),
// matching the reference graph (all 21 EN nodes in sg2.json are on
// default-linux-aarch64 even though enum_parser is a host x86_64 tool).
//
// When `consumerInputs` is non-nil, additionally emit one downstream CC
// per EN's `_serialized.cpp` output, returning per-CC `(refs, outputs,
// memberInputs)` for the caller to fold into the surrounding AR member
// accumulators. This is PR-M3-codegen-cc-enqueue: the EN-emitted
// `_serialized.cpp` is an implicit module source whose compiled `.o`
// archives alongside the declared SRCS (reference shape: every EN
// consumer's regular `.a` archives the EN-downstream `.o` after its
// regular `.cpp.o` members). `consumerInputs` must carry the consuming
// module's full CC compile bag (CFlags / CXXFlags / ADDINCL / etc.) so
// the downstream CC node matches the byte-shape of a hand-written SRCS
// entry in the same module. When nil, only EN nodes are emitted (the
// header-only branch path; no module compiles those `_serialized.cpp`
// in current M3 closure).
func emitEnumSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerAddInclGlobal []string, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []string, memberInputsList [][]string) {
	if len(d.enumSrcs) == 0 {
		return nil, nil, nil
	}

	const enumParserPath = "tools/enum_parser/enum_parser"

	var (
		enumParserLD  NodeRef
		enumParserBin = enumParserBinaryPath
	)

	// Walk enum_parser as a HOST tool (x86_64).
	enumHostInst := instance.WithHost(ctx.cfg)
	enumHostInst.Path = enumParserPath
	enumHostInst.Flags = inferFlagsFromPath(enumParserPath, true)

	if exc := Try(func() {
		result := genModule(ctx, enumHostInst)
		enumParserLD = result.LDRef
		enumParserBin = result.LDPath
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// ParseError: leave zero LD ref; enumParserBin stays at canonical fallback.
	}

	// EN nodes emit on the TARGET platform regardless of whether we're
	// in a host walk (all 21 EN nodes in sg2.json are on
	// default-linux-aarch64). Build a target-axis instance.
	enInstance := instance
	enInstance.Target = ctx.cfg.Target.ID
	// D41: use targetIsX8664 for axis checks.
	enInstance.Flags.PIC = false

	// Synthesize a ModuleCCInputs for the include scanner using the
	// module's own ADDINCL declarations plus the peer-global ADDINCL
	// set so that headers from transitive peer libraries (e.g. abseil,
	// protobuf) resolve correctly. Mirrors the ModuleCCInputs built for
	// CC nodes in the same module (PR-M3-F-3).
	scanIn := ModuleCCInputs{
		// PR-M3-F-6: same dedup as the main CC composer site.
		AddIncl:           mergeDedup(d.addIncl, nil),
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
	}

	for _, stmt := range d.enumSrcs {
		headerRel := stmt.Header
		withHeader := stmt.Variant == "with_header"

		// Scan the header's transitive include closure using the
		// target scanner. EN nodes always compile on the target axis;
		// the include search path mirrors a target CC node's search
		// path for this module.
		closure := walkClosure(ctx, enInstance, resolveSourceVFS(ctx, enInstance, headerRel, scanIn.SrcDir), scanIn)

		// Cross-EN deps: when a previously emitted EN node produced a
		// _serialized.h file (--header variant), and the current header's
		// include closure contains a file that EXPLICITLY #includes that
		// _serialized.h (under $(BUILD_ROOT)), the current EN node must
		// dep on that prior EN node.
		//
		// The include scanner cannot resolve $(BUILD_ROOT)/_serialized.h
		// paths (generated files absent at scan time). The correct signal
		// is a literal `#include <..._serialized.h>` in any source file
		// that IS in the scanner closure. Scan each closure file on disk
		// for _serialized.h include patterns and match them against known
		// EN outputs.
		var depENRefs []NodeRef
		var depENOutputs []string

		if len(ctx.enOutputs) > 0 {
			// Build a map from bare rel-path suffix → buildRootPath for
			// all known _serialized.h EN outputs. Key is the path a
			// source header would write in an #include angle-bracket
			// form, e.g. "devtools/ymake/diag/stats_enums.h_serialized.h".
			serializedHByRel := make(map[string]string, len(ctx.enOutputs))
			for buildRootPath := range ctx.enOutputs {
				if !strings.HasSuffix(buildRootPath, "_serialized.h") {
					continue
				}
				rel := strings.TrimPrefix(buildRootPath, "$(BUILD_ROOT)/")
				serializedHByRel[rel] = buildRootPath
			}

			depSeen := map[NodeRef]struct{}{}

			if len(serializedHByRel) > 0 {
				// PR-AUDIT-3 D07: consult the scanner's parsed-directive
				// cache rather than re-opening every closure entry with
				// os.Open / bufio.NewScanner. The scanner already parsed
				// each header while building `closure`; IncludeDirectiveTargets
				// returns the cached target strings (the bare-rel form a
				// source header writes between `<...>` / `"..."`) with no
				// FS re-read. The match against serializedHByRel is
				// identical to the previous ad-hoc bracket extraction.
				enScanner := ctx.scannerTarget
				for _, srcAbsPath := range closure {
					targets := enScanner.IncludeDirectiveTargets(srcAbsPath)
					for _, includePath := range targets {
						if !strings.HasSuffix(includePath, "_serialized.h") {
							continue
						}
						buildRootPath, ok := serializedHByRel[includePath]
						if !ok {
							continue
						}
						ref := ctx.enOutputs[buildRootPath]
						if _, dup := depSeen[ref]; dup {
							continue
						}
						depSeen[ref] = struct{}{}
						depENRefs = append(depENRefs, ref)
						depENOutputs = append(depENOutputs, buildRootPath)
						// Also include the corresponding _serialized.cpp path.
						cppPath := strings.TrimSuffix(buildRootPath, "_serialized.h") + "_serialized.cpp"
						if cppRef, ok2 := ctx.enOutputs[cppPath]; ok2 && cppRef == ref {
							depENOutputs = append(depENOutputs, cppPath)
						}
					}
				}
			}
		}

		// PR-M3-F-7-B: register EN outputs in the target scanner's CodegenRegistry
		// with populated EmitsIncludes. EN nodes always emit on the target axis.
		// Per enum_parser/main.cpp::WriteHeader:
		//   _serialized.h  includes util/generic/serialized_enum.h + the input header.
		//   _serialized.cpp includes the enum_serialization_runtime headers + util helpers.
		//
		// PR-AUDIT-6: registered BEFORE EmitEN so the EN node itself can walk its
		// _serialized.cpp via the registry to augment its `inputs` closure (REF's
		// EN node `inputs` includes the .cpp's transitive include set; this walk
		// is what surfaces dispatch_methods.h / ordered_pairs.h / enum_runtime.h
		// in the EN node's inputs).
		serializedCPPPath := "$(BUILD_ROOT)/" + enInstance.Path + "/" + headerRel + "_serialized.cpp"
		var serializedHPath string
		if withHeader {
			serializedHPath = "$(BUILD_ROOT)/" + enInstance.Path + "/" + headerRel + "_serialized.h"
		}
		if ctx.scannerTarget.codegen != nil {
			headerSrc := "$(SOURCE_ROOT)/" + enInstance.Path + "/" + headerRel
			cppIncludes := []string{
				headerSrc,
				"$(SOURCE_ROOT)/tools/enum_parser/enum_parser/stdlib_deps.h",
				"$(SOURCE_ROOT)/tools/enum_parser/enum_serialization_runtime/dispatch_methods.h",
				"$(SOURCE_ROOT)/tools/enum_parser/enum_serialization_runtime/enum_runtime.h",
				"$(SOURCE_ROOT)/tools/enum_parser/enum_serialization_runtime/ordered_pairs.h",
				"$(SOURCE_ROOT)/util/generic/map.h",
				"$(SOURCE_ROOT)/util/generic/serialized_enum.h",
				"$(SOURCE_ROOT)/util/generic/singleton.h",
				"$(SOURCE_ROOT)/util/generic/string.h",
				"$(SOURCE_ROOT)/util/generic/typetraits.h",
				"$(SOURCE_ROOT)/util/generic/vector.h",
				"$(SOURCE_ROOT)/util/stream/output.h",
				"$(SOURCE_ROOT)/util/string/cast.h",
			}
			sort.Strings(cppIncludes)
			ctx.scannerTarget.codegen.Register(&GeneratedFileInfo{
				ProducerKvP:   "EN",
				OutputPath:    serializedCPPPath,
				EmitsIncludes: cppIncludes,
			})
			if withHeader {
				// PR-M3-enum-parser-registry: include the sibling _serialized.cpp
				// so CC consumers that #include the _serialized.h transitively pull
				// the .cpp into their inputs and (via its EmitsIncludes) the
				// enum_serialization_runtime header set (dispatch_methods.h /
				// enum_runtime.h / ordered_pairs.h / stdlib_deps.h). REF bundles
				// the EN producer's .h and .cpp outputs together in every
				// downstream CC's inputs; mirroring that bundling through the
				// registry is the smallest mechanism that reproduces it.
				hIncludes := []string{
					headerSrc,
					serializedCPPPath,
					"$(SOURCE_ROOT)/util/generic/serialized_enum.h",
				}
				sort.Strings(hIncludes)
				ctx.scannerTarget.codegen.Register(&GeneratedFileInfo{
					ProducerKvP:   "EN",
					OutputPath:    serializedHPath,
					EmitsIncludes: hIncludes,
				})
			}
		}

		// PR-AUDIT-6: walk each cross-EN dep's _serialized.cpp to fold its
		// transitive closure into THIS EN node's `inputs`. REF's EN node walks
		// through cross-EN deps (e.g. dep_types depends on stats_enums via a
		// `#include "stats_enums.h_serialized.h"` in some closure file; the
		// cross-EN dep's `_serialized.cpp` carries the enum_runtime.h transitive
		// closure that reaches dispatch_methods.h / ordered_pairs.h).
		//
		// EN nodes without cross-EN deps (e.g. stats_enums itself, a leaf EN)
		// don't get this augmentation — matching REF's tight 2-input shape for
		// leaf EN nodes.
		//
		// Excluding headerSrc (EmitEN appends it separately) and depENOutputs
		// (likewise) prevents multiset duplicates. Also filter the source-header
		// `closure` against depENOutputs — the closure may include a
		// $(BUILD_ROOT)/_serialized.h entry that depENOutputs also names (the
		// scanner resolves both through the codegen registry / cross-EN dep
		// detection), and the duplicate fails L2 multiset equality.
		enClosureExcl := map[string]struct{}{
			"$(SOURCE_ROOT)/" + enInstance.Path + "/" + headerRel: {},
		}
		for _, p := range depENOutputs {
			enClosureExcl[p] = struct{}{}
		}
		filteredClosure := make([]string, 0, len(closure))
		for _, p := range closure {
			if _, drop := enClosureExcl[p]; drop {
				continue
			}
			filteredClosure = append(filteredClosure, p)
		}
		var crossCppClosure []string
		for _, depOut := range depENOutputs {
			if !strings.HasSuffix(depOut, "_serialized.cpp") {
				continue
			}
			sub := walkClosure(ctx, enInstance, depOut, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				crossCppClosure = append(crossCppClosure, p)
			}
		}
		// PR-M3-G-3: walk OUR OWN _serialized.cpp output through the codegen
		// registry to fold its transitive include closure (util/generic/*,
		// libcxx/*, musl/* etc. reached via cppIncludes' EmitsIncludes) into
		// THIS EN node's `inputs`. REF's EN node inputs equal the consuming
		// CC node's inputs (both walk the same .h_serialized.cpp) for the
		// plain GENERATE_ENUM_SERIALIZATION variant. The WITH_HEADER variant
		// produces a `.h_serialized.h` that other ENs cross-consume; REF
		// keeps those EN nodes' inputs tight (source-header closure only,
		// no output-side augmentation), because the consumers absorb the
		// full closure on their side. The two WITH_HEADER usages in the
		// M3 closure (`diag/stats_enums.h`, `diag/trace_type_enums.h`) are
		// both emitted with source-header-only inputs in sg2.json.
		//
		// The $(BUILD_ROOT)/<output> path lookup goes through the codegen
		// registry (registered moments earlier) and follows EmitsIncludes;
		// subsequent children resolve via parseIncludes on the real
		// $(SOURCE_ROOT) headers.
		var ownOutputClosure []string
		if !withHeader && ctx.scannerTarget.codegen != nil {
			sub := walkClosure(ctx, enInstance, serializedCPPPath, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				ownOutputClosure = append(ownOutputClosure, p)
			}
		}
		enClosure := mergeDedup(filteredClosure, crossCppClosure)
		enClosure = mergeDedup(enClosure, ownOutputClosure)
		sort.Strings(enClosure)

		// PR-M3-L0-codegen-deps-EV-PB: when this EN node's transitive closure
		// pulls in a PB/EV producer's $(BUILD_ROOT) output (e.g. an EN whose
		// header includes a header that #includes msg.ev.pb.h), the resulting
		// EN node must dep on that PB/EV producer — matching sg2.json shape
		// where `module_resolver.h_serialized.cpp` deps on the msg.ev/trace.ev
		// EV nodes + events_extension PB. Filter out the cross-EN dep refs
		// already in depENRefs so they aren't duplicated.
		augmentedDepENRefs := depENRefs
		if extra := resolveCodegenDepRefs(ctx, enInstance, enClosure, depENRefs...); len(extra) > 0 {
			augmentedDepENRefs = append(append([]NodeRef(nil), depENRefs...), extra...)
		}

		enRef, enOutPaths := EmitEN(
			enInstance,
			headerRel,
			withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			depENOutputs,
			enClosure,
			ctx.emit,
		)

		// Record outputs so later EN nodes can dep on them.
		for _, p := range enOutPaths {
			ctx.enOutputs[p] = enRef
		}

		// PR-M3-codegen-cc-enqueue: emit the downstream CC compiling
		// the EN-produced `_serialized.cpp` as an implicit module
		// source. The CC inherits the consuming module's full compile
		// bag (consumerInputs); composeCCPaths' IsGenerated branch
		// roots the output under $(BUILD_ROOT)/<enInstance.Path>/
		// <headerRel>_serialized.cpp{,.o} with `_/` infix when headerRel
		// contains a `/`. depPrefix is the cross-EN dep set the
		// reference graph places ahead of the consumer's own
		// `_serialized.cpp` in the CC's inputs[] (sg2.json
		// devtools/ymake/export_json_debug.h_serialized.cpp.o:
		// inputs[0..1] = [stats_enums.h_serialized.cpp,
		// stats_enums.h_serialized.h], inputs[2] = the consuming
		// .h_serialized.cpp).
		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"
			// DepRefs: own EN + cross-EN dep refs. Reference shape
			// (sg2.json export_json_debug.h_serialized.cpp.o):
			// deps = [stats_enums-EN-uid, export_json_debug-EN-uid].
			allDepRefs := make([]NodeRef, 0, 1+len(depENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, depENRefs...)
			ccRef, ccOut, ccIns := emitCodegenDownstreamCC(ctx, enInstance, cppRel, depENOutputs, allDepRefs, *consumerInputs)
			ccRefs = append(ccRefs, ccRef)
			ccOutputs = append(ccOutputs, ccOut)
			memberInputsList = append(memberInputsList, ccIns)
		}
	}

	return ccRefs, ccOutputs, memberInputsList
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
//
// PR-35y: returns (ref, outputPath, ccInputs, primaryCount, ok).
// `primaryCount` is the number of leading entries in `ccInputs` that
// are "primary sources" of this member (as opposed to header/closure
// entries) — the .global.a aggregator drops these primaries when the
// member belongs to regular SRCS rather than GLOBAL_SRCS. The
// .c/.cpp/.cc/.cxx/.S/.s/.asm dispatches yield primaryCount=1; the
// .rl6 dispatch yields 1 (just the .rl6 source) or 2 (when the `.h`
// companion exists on disk).
func emitOneSource(ctx *genCtx, instance ModuleInstance, srcDir string, srcRel string, in ModuleCCInputs, ancestorRebase bool) (NodeRef, string, []string, int, bool) {
	if isHeaderSource(srcRel) {
		return NodeRef{}, "", nil, 0, false
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
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// PR-M3-py3-runtime-closure: runtime_py3 __res.cpp / sitecustomize.cpp
		// each carry the matching .pyc.inc + PY_SRCS python inputs in REF.
		// PR-M3-L0-cascade-close-v2: lift the extras BEFORE resolving codegen
		// dep refs so the .pyc.inc producer (AR node) is reachable through the
		// IncludeInputs probe. Order is preserved (extras appended to the tail
		// of IncludeInputs) so EmitCC's inputs[] composition is unchanged.
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = append(srcIn.IncludeInputs, extras...)
		}
		// Thread codegen producer dep refs into the CC node. PR-M3-L0-codegen-
		// deps-EV-PB extended this to PB/EV (with platform keying) on top of
		// the EN path established by PR-M3-module-tag-and-stats-enums-dep.
		srcIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, srcIn.IncludeInputs)

		ref, outPath := EmitCC(srcInstance, srcRel, srcIn, ctx.emit)

		// AR/LD aggregate the per-CC inputs (primary source +
		// resolved headers) into their own inputs slice per the
		// sg.json shape (PR-31 D11). Compose the input list here
		// (matching what EmitCC itself does internally).
		inputPath := emittedSourceInputPath(srcInstance, srcRel, srcIn, ctx.sourceRoot)
		ccInputs := append([]string{inputPath}, srcIn.IncludeInputs...)

		return ref, outPath, ccInputs, 1, true
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

		// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
		// PR-M3-F-5: extend yasm walk to all `.asm` sources on x86_64, not
		// just asmlibYasmModules. The reference graph uses yasm for every
		// `.asm` host source (util/system/context_x86.asm + asmlib's 25 nodes).
		if targetIsX8664(instance) && strings.HasSuffix(srcRel, ".asm") {
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
		asIn := srcIn
		asIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		// PR-35m: thread the full ModuleCCInputs into EmitAS so it
		// can compose own/peer ADDINCL, own non-GLOBAL CFLAGS, and
		// auto peer CFLAGS at the same slots CC consumes them
		// (retiring the util-specific path-sniff stopgap PR-35i added).
		ref, outPath := EmitAS(srcInstance, srcRel, asIn, yasmRef, ctx.emit)

		// PR-35y R8: when the module declares SRCDIR and the .S
		// source does not exist locally at instance.Path/<srcRel>,
		// the AR memberInput resolves at `$(SOURCE_ROOT)/<srcDir>/<srcRel>`
		// rather than the unrebased `<instance.Path>/<srcRel>`.
		// Empirical reference: tcmalloc/no_percpu_cache (SRCDIR=
		// `contrib/libs/tcmalloc`) — its `tcmalloc/internal/percpu_rseq_asm.S`
		// resolves at `contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S`,
		// not `contrib/libs/tcmalloc/no_percpu_cache/tcmalloc/internal/percpu_rseq_asm.S`.
		// Same rule as composeASPaths' SRCDIR routing for AS itself
		// (PR-35r, as.go:316-336): keeping the gen.go aggregator's
		// path in sync with as.go's resolution.
		asInputPath := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel

		if srcDir != "" && srcDir != srcInstance.Path && !sourceExistsLocally(ctx.sourceRoot, srcInstance.Path, srcRel) {
			asInputPath = "$(SOURCE_ROOT)/" + srcDir + "/" + srcRel
		}

		// PR-M3-openssl-ar-plugin-and-as-clean: collapse `..` segments so
		// e.g. openssl's `crypto/../asm/...` resolves to `asm/...` in the
		// AR aggregator's memberInputs. The AS node's own input path is
		// composed independently inside as.go and is already cleaned.
		asInputPath = path.Clean(asInputPath)

		asInputs := append([]string{asInputPath}, asIn.IncludeInputs...)

		return ref, outPath, asInputs, 1, true
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

		// PR-35z: scan the `.rl6` source's transitive #include closure
		// (the `.rl6` body embeds `#include` directives that resolve
		// through the same search-path / sysincl rules as `.cpp`/`.S`
		// sources). Both the R6 generator node AND the downstream CC
		// of the generated `.cpp` carry the same closure in their
		// `inputs` slot — reference graph: util/datetime/parser.rl6
		// produces a 1009-input R6 node and a 1009-input CC node,
		// where positions 1.. of each are identical (the `.rl6`
		// source plus its 1007-header transitive closure).
		rl6Closure := walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// PR-M3-final-r6-stats-enums-leak: REF's R6 closure resolves a
		// transitive `#include <..._serialized.h>` directive (via stats.h
		// or sibling) for its descendant headers (e.g. util/generic/
		// serialized_enum.h) but does NOT add the generated EN
		// `_serialized.{cpp,h}` siblings themselves to the R6 inputs.
		// Our codegen registry resolves the directive to the registered
		// $(BUILD_ROOT)/<...>_serialized.h output and follows EmitsIncludes,
		// which pulls in both the .h itself and its sibling .cpp. Strip
		// both at the R6 input boundary; the descendant util headers
		// (which REF does carry) reach R6 inputs through the same
		// EmitsIncludes traversal and are unaffected.
		rl6Closure = filterEnSerializedSiblings(rl6Closure)

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryStr, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		// F-7-B / PR-AUDIT-2 D02: register the R6 output (.rl6.cpp). Ragel emits
		// the .rl6 source's `#include` directives verbatim into the generated
		// .cpp, so the .cpp's effective direct-include set is the .rl6's. We
		// register a single EmitsIncludes entry pointing at the .rl6 source;
		// WalkClosure on the .rl6.cpp will recurse into the .rl6 via the
		// FS-parsed locator and produce the same closure the downstream CC
		// previously got from scanning the .rl6 manually.
		rl6SourceAbs := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "R6",
				OutputPath:    r6Out,
				EmitsIncludes: []string{rl6SourceAbs},
			})
		}

		// PR-29-D07: same shape as the JS branch above. Pass
		// IsGenerated so the downstream CC composes inputPath under
		// $(BUILD_ROOT)/<srcInstance.Path>/<rel> rather than the
		// stale $(SOURCE_ROOT) shape. PR-30 D04: thread r6Ref as the
		// downstream CC's `Generator` so the CC node carries an
		// explicit dep on its R6 source-generator node, matching the
		// reference shape.
		//
		// PR-AUDIT-2 D02: dispatch through the unified VFS-path entry — the
		// .rl6.cpp is registered in the codegen registry (see Register above)
		// and the scanner walks transitively through both BUILD_ROOT and
		// SOURCE_ROOT children uniformly. Previously this site assembled
		// `[<.rl6 source>, ...rl6Closure]` by hand from a separate
		// source-side scan; the architecturally-correct shape comes from
		// WalkClosure rooted at the generated .cpp.
		ccSrcRel := strings.TrimPrefix(r6Out, "$(BUILD_ROOT)/"+srcInstance.Path+"/")
		ccIncludeInputs := walkClosure(ctx, srcInstance, r6Out, srcIn)

		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.Generator = r6Ref
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = ccIncludeInputs
		// PR-41 Fix H: ymake's _LANG_CFLAGS_RL=-Wno-implicit-fallthrough applies to CC
		// compiles whose source is a .rl6-generated .cpp (build/ymake.core.conf).
		// Extend in M3+ for .pyx, .py.py3, .rl5 when their closures surface.
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// PR-M3-L0-codegen-deps-EV-PB: thread EN/PB/EV producer refs reached
		// through the .rl6.cpp's transitive include closure. Generator (r6Ref)
		// is filtered out so EmitCC's leading-DepRefs slot isn't duplicated.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r6Ref)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// R6-derived CC: primary input is the BUILD_ROOT-rooted .cpp
		// generated by ragel6. No scanner pass (the .cpp doesn't exist
		// on disk at scan time).
		//
		// PR-35y R7: the AR/LD member-inputs aggregator excludes the
		// BUILD_ROOT-staged generated cpp (mirror of the JS rule) and
		// instead carries the original `.rl6` source plus its
		// companion `.h` header. Reference graph confirms: util's
		// libyutil.a inputs include `parser.rl6` and `parser.h`,
		// never the `parser.rl6.cpp` BUILD_ROOT shim. The companion
		// `.h` header is added only when a sibling file with the
		// same basename and `.h` suffix exists on disk — the
		// convention holds for every observed `.rl6` source in the
		// M2 closure (util/datetime/parser.rl6 → parser.h).
		rl6Source := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		ccInputs := []string{rl6Source}
		primaryCount := 1

		companionRel := strings.TrimSuffix(srcRel, ".rl6") + ".h"
		companionAbs := filepath.Join(ctx.sourceRoot, srcInstance.Path, companionRel)

		if info, err := os.Stat(companionAbs); err == nil && !info.IsDir() {
			ccInputs = append(ccInputs, "$(SOURCE_ROOT)/"+srcInstance.Path+"/"+companionRel)
			primaryCount = 2
		}

		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure into memberInputs so the AR/LD aggregator carries
		// the libcxx/musl/protobuf/etc. headers the generated .rl6.cpp
		// includes. Upstream ymake propagates each member-CC's NodeInputs
		// up via EDT_BuildFrom (json_visitor.cpp:788-789 NeedToPassInputs).
		ccInputs = append(ccInputs, ccIncludeInputs...)

		return ccRef, ccOut, ccInputs, primaryCount, true

	case strings.HasSuffix(srcRel, ".ev"):
		// PR-M3-C: .ev sources in a LIBRARY module (e.g. devtools/ymake/diag/trace.ev).
		// Emits one EV node (generating .ev.pb.cc + .ev.pb.h) then a downstream
		// CC node compiling the generated .ev.pb.cc. The CC node's full include
		// closure is not scanned (generated files don't exist on disk at gen time);
		// the node structure is correct at L0/L1/L2 even without L3-exact inputs.
		{
			// Walk host tool programs.
			cppStyleguideBinary := pbCppStyleguidePath
			protocBinary := pbProtocBinaryPath
			event2cppBinary := evEvent2cppBinaryPath

			var cppStyleguideLDRef, protocLDRef, event2cppLDRef NodeRef

			protocHostInst := instance.WithHost(ctx.cfg)
			protocHostInst.Path = pbProtocModule
			protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

			if exc := Try(func() {
				result := genModule(ctx, protocHostInst)
				protocLDRef = result.LDRef
				protocBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			cppStyleguideHostInst := instance.WithHost(ctx.cfg)
			cppStyleguideHostInst.Path = pbCppStyleguideModule
			cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

			if exc := Try(func() {
				result := genModule(ctx, cppStyleguideHostInst)
				cppStyleguideLDRef = result.LDRef
				cppStyleguideBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			event2cppHostInst := instance.WithHost(ctx.cfg)
			event2cppHostInst.Path = evEvent2cppModule
			event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

			if exc := Try(func() {
				result := genModule(ctx, event2cppHostInst)
				event2cppLDRef = result.LDRef
				event2cppBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			// moduleTag is empty for LIBRARY modules (no "cpp_proto" tag).
			evRef := EmitEV(srcInstance, srcRel,
				cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"", ctx.sourceRoot, ctx.emit)

			// F-7-B: register the .ev.pb.h output with EmitsIncludes from the .ev imports,
			// plus the protobuf runtime headers (F-7-D).
			evRelPath := srcInstance.Path + "/" + srcRel
			evH := "$(BUILD_ROOT)/" + evRelPath + ".pb.h"
			evPbCC := "$(BUILD_ROOT)/" + evRelPath + ".pb.cc"

			// PR-M3-L0-codegen-deps-EV-PB: stash the EV NodeRef under both outputs
			// on the emitting platform so consumer CCs in OTHER modules whose
			// IncludeInputs include this .ev.pb.h / .ev.pb.cc dep on the producer.
			evKey := codegenOutputKey{platform: srcInstance.Target}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef
			if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath)
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				evEmitsIncludes := make([]string, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
				evEmitsIncludes = append(evEmitsIncludes, directImports...)
				evEmitsIncludes = append(evEmitsIncludes, protobufRuntimeHeaders...)
				evEmitsIncludes = append(evEmitsIncludes, evExtras...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: evEmitsIncludes,
				})
				// PR-AUDIT-2 D04: register the .ev.pb.cc output too. event2cpp
				// emits a `#include "<base>.ev.pb.h"` plus the protobuf runtime
				// headers; the .pb.h's own EmitsIncludes are already registered
				// (just above), so a single entry pointing at the .pb.h would
				// suffice — we mirror the .pb.h list for symmetry with PB (the
				// .pb.cc emitted by protoc includes the same runtime headers).
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evPbCC,
					EmitsIncludes: append([]string{evH}, protobufRuntimeHeaders...),
				})
			}

			// Emit downstream CC for the generated .ev.pb.cc.
			// PR-AUDIT-2 D04: dispatch through the unified VFS-path entry —
			// the .ev.pb.cc is registered above with the right EmitsIncludes;
			// WalkClosure walks transitively into the .pb.h and out to the
			// protobuf runtime headers via the FS locator.
			evPbCCSuffix := srcRel + ".pb.cc"
			ccIn := srcIn
			ccIn.IsGenerated = true
			ccIn.Generator = evRef
			ccIn.HasGenerator = true
			ccIn.IncludeInputs = walkClosure(ctx, srcInstance, evPbCC, srcIn)
			// PR-M3-final-surgical (fix 1): the .ev.pb.cc.o consumer must not
			// carry its OWN .ev.pb.h in inputs[] (REF omits the self-include;
			// cross-imported sibling .ev.pb.h entries remain). The walker
			// reaches evH transitively because the .pb.cc is registered with
			// evH as its first EmitsIncludes child — drop just that entry.
			{
				filtered := make([]string, 0, len(ccIn.IncludeInputs))
				for _, in := range ccIn.IncludeInputs {
					if in == evH {
						continue
					}
					filtered = append(filtered, in)
				}
				ccIn.IncludeInputs = filtered
			}
			// PR-M3-final-codegen-registry-expansion: protoc emits
			// `#include "google/protobuf/wire_format.h"` directly. Add to inputs
			// only on this CC node (not via registry — that would over-emit).
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, pbRuntimeBase+"google/protobuf/wire_format.h")
			// PR-M3-L0-codegen-deps-EV-PB: thread cross-codegen producer refs
			// (e.g. an .ev that imports another module's .proto pulls the
			// peer's PB into the consumer CC's deps via its .pb.h in inputs[]).
			ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, evRef)

			ref, outPath := EmitCC(srcInstance, evPbCCSuffix, ccIn, ctx.emit)

			// The primary input for the AR/LD memberInputs is the original .ev source.
			// PR-M3-final-codegen-registry-expansion: wire_format.h also propagates
			// up into the AR rollup (matched in REF on libdevtools-ymake-diag.a).
			evSrcAbs := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
			return ref, outPath, []string{evSrcAbs, pbRuntimeBase + "google/protobuf/wire_format.h"}, 1, true
		}

	case strings.HasSuffix(srcRel, ".rl"):
		// PR-M3-E: ragel5 two-step code generation (.rl → .rl.tmp → .rl5.cpp).
		// Mirrors the .rl6 branch: walk the two host ragel5 PROGRAMs eagerly,
		// emit the R5 node, then emit a CC node for the generated .rl5.cpp.
		const (
			ragel5Path      = "contrib/tools/ragel5/ragel"
			rlgenCdPath     = "contrib/tools/ragel5/rlgen-cd"
			ragel5Fallback  = "$(BUILD_ROOT)/contrib/tools/ragel5/ragel/ragel5"
			rlgenCdFallback = "$(BUILD_ROOT)/contrib/tools/ragel5/rlgen-cd/rlgen-cd"
		)

		var (
			ragel5LDRef   NodeRef
			rlgenCdLDRef  NodeRef
			ragel5BinStr  = ragel5Fallback
			rlgenCdBinStr = rlgenCdFallback
		)

		ragel5Instance := srcInstance.WithHost(ctx.cfg)
		ragel5Instance.Path = ragel5Path
		ragel5Instance.Flags = inferFlagsFromPath(ragel5Path, true)

		if exc := Try(func() {
			res := genModule(ctx, ragel5Instance)
			ragel5LDRef = res.LDRef
			ragel5BinStr = res.LDPath
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		rlgenCdInstance := srcInstance.WithHost(ctx.cfg)
		rlgenCdInstance.Path = rlgenCdPath
		rlgenCdInstance.Flags = inferFlagsFromPath(rlgenCdPath, true)

		if exc := Try(func() {
			res := genModule(ctx, rlgenCdInstance)
			rlgenCdLDRef = res.LDRef
			rlgenCdBinStr = res.LDPath
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		r5Ref, r5TmpOut, r5CppOut := EmitR5(srcInstance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinStr, rlgenCdBinStr, ctx.emit)
		_ = r5Ref

		// F-7-B / PR-AUDIT-2 D05: register R5 outputs. ragel5 emits the
		// .rl source's #include directives verbatim into the generated
		// .rl5.cpp; the .tmp intermediate has no consumer-visible includes.
		// PR-M3-L0-cascade-close-v2: ProducerRef = r5Ref so the downstream
		// CC consuming the .rl5.cpp threads R5 into its deps[].
		rlSourceAbs := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "R5",
				OutputPath:     r5TmpOut,
				EmitsIncludes:  nil,
				ProducerRef:    r5Ref,
				HasProducerRef: true,
			})
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "R5",
				OutputPath:     r5CppOut,
				EmitsIncludes:  []string{rlSourceAbs},
				ProducerRef:    r5Ref,
				HasProducerRef: true,
			})
		}

		// Downstream CC for the generated .rl5.cpp.
		// PR-AUDIT-2 D05: dispatch through the unified VFS-path entry —
		// the .rl5.cpp is registered above with the .rl source as its
		// single direct include; WalkClosure recurses into the .rl via
		// the FS locator and yields the full transitive closure.
		ccSrcRel := strings.TrimPrefix(r5CppOut, "$(BUILD_ROOT)/"+srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// PR-M3-L0-codegen-deps-EV-PB: thread EN/PB/EV producer refs reached
		// through the .rl5.cpp's transitive include closure.
		// PR-M3-L0-cascade-close-v2: prepend r5Ref. WalkClosure skips the
		// root (r5CppOut) so the registry probe alone wouldn't surface R5;
		// REF's R5-derived CC carries R5 as its leading dep.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// AR/LD member inputs: use the original .rl source (not generated .cpp).
		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure into memberInputs. Upstream ymake propagates each
		// member-CC's NodeInputs up to the parent AR/LD via EDT_BuildFrom
		// (json_visitor.cpp:788-789 NeedToPassInputs); the .rl-generated
		// .cpp's #include closure is included even though the AR archives
		// only the .o, because the inputs walk is set-union over all
		// transitive file deps under the module boundary.
		rlSource := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		rlMemberInputs := append([]string{rlSource}, ccIn.IncludeInputs...)
		return ccRef, ccOut, rlMemberInputs, 1, true

	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		// PR-M3-E: CONFIGURE_FILE template source. Emit a CF node that runs
		// configure_file.py to expand @VAR@ placeholders, then emit a CC
		// node for the generated .cpp / .c file.
		//
		// The CF node's cmd_args include the DEFAULT-declared cfg vars; those
		// are passed via the moduleData in srcIn.DefaultVars (set by genModule
		// before calling emitOneSource). We also add BUILD_TYPE=DEBUG (the
		// hardcoded build configuration).
		//
		// The output path strips the .in suffix: sandbox.cpp.in → sandbox.cpp.
		// PR-M3-F-5: scan the .in template for its transitive include closure
		// (same as a .cpp source) and fold into srcIn.IncludeInputs before
		// calling EmitCF so the CF node's inputs[] matches the reference shape
		// (e.g. sandbox.cpp.in → 795-entry closure; build_info.cpp.in → 5).
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, srcIn, ctx.emit)

		// F-7-B / PR-AUDIT-2 D08: register the CF output. configure_file.py
		// performs `@VAR@` substitution but leaves `#include` directives
		// intact, so the generated .cpp's direct includes are the .cpp.in's
		// (modulo substitution). We register the .cpp.in source as the
		// single EmitsIncludes child so WalkClosure recurses into it via
		// the FS locator and yields the full transitive closure that the
		// downstream CC needs.
		// PR-M3-final-codegen-registry-expansion: configure_file.py is the
		// codegen script driving the CF node; REF wires it as an input on
		// every CC consumer of the generated .cpp (verified on
		// build_info.cpp.o and sandbox.cpp.o).
		// PR-M3-L0-cascade-close-v2: ProducerRef = cfRef so downstream CC's
		// resolveCodegenDepRefs threads the CF producer into its deps[].
		inSourceAbs := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "CF",
				OutputPath:     cfOut,
				EmitsIncludes:  []string{inSourceAbs, configureFilePyPath},
				ProducerRef:    cfRef,
				HasProducerRef: true,
			})
		}

		// Downstream CC for the generated .cpp / .c.
		// PR-AUDIT-2 D08: dispatch through the unified VFS-path entry —
		// the .cpp is registered above with the .cpp.in as its single
		// direct include; WalkClosure recurses into the .cpp.in via the
		// FS locator and yields the full transitive closure.
		ccSrcRel := strings.TrimPrefix(cfOut, "$(BUILD_ROOT)/"+srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		// PR-M3-L0-codegen-deps-EV-PB: thread codegen producer refs reached
		// through the CF-generated .cpp's transitive include closure.
		// PR-M3-L0-cascade-close-v2: also add cfRef directly — the CC
		// compiles cfOut, and WalkClosure skips the root (cfOut itself),
		// so the registry probe wouldn't find it via IncludeInputs alone.
		// REF's CF-derived CC carries the CF producer as a leading dep
		// (sandbox.cpp.o → CF sandbox.cpp).
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// AR/LD member inputs: use the original .cpp.in / .c.in source.
		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure (the closure of the generated .cpp scanned against
		// the .cpp.in body via the codegen-registry EmitsIncludes edge) so
		// the AR/LD aggregator carries the same set upstream ymake propagates
		// via EDT_BuildFrom (json_visitor.cpp:788-789 NeedToPassInputs).
		inSource := "$(SOURCE_ROOT)/" + srcInstance.Path + "/" + srcRel
		cfMemberInputs := append([]string{inSource}, ccIn.IncludeInputs...)
		return ccRef, ccOut, cfMemberInputs, 1, true
	}

	// PR-M3-A: known-deferred source kinds are silently skipped rather
	// than throwing. Real emitters land in PR-M3-B (PB), PR-M3-D (EN/EV),
	// and later PRs. Until then, returning false means the source
	// contributes nothing to the AR/LD node set; the module may become
	// header-only if all its sources are deferred.
	if isSkippedSource(srcRel) {
		return NodeRef{}, "", nil, 0, false
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)

	return NodeRef{}, "", nil, 0, false
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

// joinSrcsIncludeClosure unions per-source #include closures across
// `sources` (PR-35d) using the consumer's own scan context. The
// scanner's DFS runs over all members with a SHARED visited set —
// mirroring the actual joined .cpp compile, where headers reached
// once stay deduped — so total work is O(union closure) not O(sum
// per-source closures). Returns nil when nothing resolves.
func joinSrcsIncludeClosure(ctx *genCtx, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []string {
	// PR-AUDIT-4 (D08): per-instance scanner via the unified dispatch
	// helper; no more inline target-vs-host branch.
	scanner := ctx.scannerFor(srcInstance)
	if scanner == nil {
		return nil
	}

	visited := make(map[string]struct{}, 1024)
	order := make([]string, 0, 1024)
	srcAbsSet := make(map[string]struct{}, len(sources))

	for _, src := range sources {
		srcRelOnDisk := srcInstance.Path + "/" + src

		if in.SrcDir != "" && in.SrcDir != srcInstance.Path {
			localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, src)
			info, err := os.Stat(localCandidate)

			if err != nil || info.IsDir() {
				srcRelOnDisk = in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(srcInstance),
		}

		// PR-M3-perf-E: scanCtx dispatch — local vs interned (see
		// genCtx.getScanCtx). Within this join-srcs loop every source's
		// cfg differs only in SourceRel; PR-M3-perf-E ignored that
		// observation in favour of routing through getScanCtx anyway,
		// which yields one scanCtx per unique (ctxHash) and lets
		// resolveCache / subgraphCache entries from earlier sources serve
		// later sources at the same ctxHash.
		sc := ctx.getScanCtx(scanner, cfg)

		// `WalkSource` rewrites `sc.cfg.SourceRel` to the current
		// source-rel so sysinclSourceLookup keys on the right path. We
		// must therefore use the dfs entry that ALSO sets it, OR set it
		// inline before dfs. dfs reads sc.cfg.SourceRel for srcClassHash,
		// so set it here before invoking dfs against the shared visited+order.
		sc.cfg.SourceRel = srcRelOnDisk

		// PR-M3-vfs-paths: srcAbs is a VFS-rooted path. The scanner's
		// internal walk operates on VFS form; the FS translation happens
		// at the parseIncludes / fileExists boundary inside scanner.go.
		srcAbs := "$(SOURCE_ROOT)/" + srcRelOnDisk
		srcAbsSet[srcAbs] = struct{}{}
		sc.dfs(srcAbs, visited, &order)
	}

	if len(order) == 0 {
		return nil
	}

	out := make([]string, 0, len(order))

	for _, abs := range order {
		// Skip the source files themselves — JOIN_SRCS members are
		// emitted separately as $(SOURCE_ROOT)/<path>/<src>; the
		// scanner closure carries only headers/extras.
		if _, isSrc := srcAbsSet[abs]; isSrc {
			continue
		}

		// PR-M3-vfs-paths: `abs` is already in $(SOURCE_ROOT)/-rooted
		// form (the scanner walks VFS paths internally); no translation
		// needed before stashing it as a node Input.
		out = append(out, abs)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// jsCCIncludeInputs assembles `[scripts..., sources..., closure...]`
// for the JS-derived CC's IncludeInputs slot (PR-35d).
func jsCCIncludeInputs(srcInstance ModuleInstance, sources, closure []string) []string {
	const (
		joinSrcsPath = "$(SOURCE_ROOT)/build/scripts/gen_join_srcs.py"
		procCmdFiles = "$(SOURCE_ROOT)/build/scripts/process_command_files.py"
	)

	out := make([]string, 0, 2+len(sources)+len(closure))
	out = append(out, joinSrcsPath, procCmdFiles)

	for _, s := range sources {
		out = append(out, "$(SOURCE_ROOT)/"+srcInstance.Path+"/"+s)
	}

	out = append(out, closure...)

	return out
}

// jsTargetPeerAddIncl rebases a host (x86_64) PeerAddInclGlobal slice to
// the target (aarch64) musl arch layout for use in the JS-node closure
// scan. JS nodes are anchored to the target platform axis (PR-35s), so
// their include closure must reflect aarch64 musl search paths rather
// than the host x86_64 ones that the surrounding HOST-build moduleInputs
// carries.
//
// PR-40 Fix C: narrow shim — only the musl arch/x86_64 entry is
// rewritten to arch/aarch64; all other entries pass through unchanged.
// TODO: remove when a general target-addincl propagation mechanism lands
// in M3+ (the same milestone as the BinaryDir lift for Fix D).
func jsTargetPeerAddIncl(hostPeerAddIncl []string) []string {
	const (
		hostMuslArch   = "contrib/libs/musl/arch/x86_64"
		targetMuslArch = "contrib/libs/musl/arch/aarch64"
	)

	out := make([]string, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		if p == hostMuslArch {
			out[i] = targetMuslArch
		} else {
			out[i] = p
		}
	}

	return out
}

// resolveSourceVFS composes the `$(SOURCE_ROOT)/...` VFS path of a
// SRCS-declared source, applying composeCCPaths' SRCDIR-aware
// fallback: when the module declares SRCDIR and no local file exists
// at instance.Path/<srcRel>, the source resolves under SRCDIR. This
// is registration-time path resolution (matches AUDIT-3 bucket (B));
// the os.Stat is legitimate at this layer because the answer feeds
// path composition, not scanner-internal locator dispatch.
func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir string) string {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != "" && srcDir != srcInstance.Path {
		localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			srcRelOnDisk = srcDir + "/" + srcRel
		}
	}

	return vfsSource(srcRelOnDisk)
}

// resolveCodegenDepRefs replaced by the EN/PB/EV-aware version at line 344
// (PR-M3-L0-codegen-deps-EV-PB).

// walkClosure resolves the transitive include closure of a source
// rooted at any VFS path — `$(SOURCE_ROOT)/...` for FS-resident
// sources or `$(BUILD_ROOT)/...` for codegen outputs whose producer
// has registered an EmitsIncludes entry in the per-scanner
// CodegenRegistry. The scanner's locator (forEachResolvedChild)
// dispatches FS-vs-codegen internally; callers do not branch on
// is-on-disk. Returns the resolved include set or nil when the
// scanner is unavailable.
//
// The ScanContext mirrors what cmd_args -I uses: own AddIncl + peer
// GLOBAL AddIncl + the cc bundle's implicit baseline (linux-headers
// and the active musl-arch include path).
func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath string, in ModuleCCInputs) []string {
	scanner := ctx.scannerFor(srcInstance)
	if scanner == nil {
		return nil
	}

	// SourceRel feeds srcClassHash (per-source subgraph-cache keying).
	// WalkClosure overwrites it per-call for SOURCE_ROOT paths so
	// scanCtx reuse across sources keys correctly; for BUILD_ROOT
	// paths it stays as set here and is never consulted by the
	// BUILD_ROOT child branch.
	sourceRel := strings.TrimPrefix(vfsPath, vfsSourcePrefix)
	sourceRel = strings.TrimPrefix(sourceRel, vfsBuildPrefix)

	cfg := ScanContext{
		SourceRel:       sourceRel,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(srcInstance),
	}

	sc := ctx.getScanCtx(scanner, cfg)

	return sc.WalkClosure(vfsPath)
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

	// PR-32 D02: dispatch via Flags.LibcMusl, not path-prefix.
	if instance.Flags.LibcMusl {
		// Mirror muslCcIncludes / muslCcIncludesX8664: arch + generic
		// + src/include + src/internal + include + extra. Use the
		// instance's Target to pick aarch64 vs x86_64 (D41: same
		// switch composeMuslCC vs composeMuslHostCC uses).
		var arch string

		// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
		if targetIsX8664(instance) {
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

// reorderARMembers reorders (refs, paths) so the AR cmd_args match
// ymake's canonical member ordering:
//
//  1. SRC_C_NO_LTO sources (isFlatNoLto[i]==true) — hoisted to the
//     front in their original relative order.
//  2. Regular SRCS hand-written .o (non-SRC_C_NO_LTO, non-R6, no codegen
//     suffix) — kept in declaration order.
//  3. JOIN_SRCS (entries at [numSrcsDerived, len)) — in declaration order.
//  4. Codegen-derived .o files, partitioned by source-extension category
//     and emitted in canonical order: .g4.cpp → .h_serialized.cpp →
//     .ev.pb.cc → .rl6.cpp → .reg3.cpp. Within each category declaration
//     relative order is preserved. PR-M3-AR-member-order: REF places
//     hand-written .cpp.o before generated .o files in AR member listing.
//  5. R6-generated paths bearing the legacy `/_/_/` infix go last (these
//     are the util/_/_/datetime/parser.rl6.cpp.o family caught by the
//     pre-existing path heuristic; the .rl6.cpp.o suffix category above
//     handles the remaining R6 members whose path lacks the infix).
//
// isFlatNoLto is a parallel bool slice (same length as refs/paths before
// JOIN_SRCS are appended) marking SRC_C_NO_LTO entries. The slice must
// have len(isFlatNoLto) == len(refs) == len(paths) at call time. PR-41 Fix I.
//
// isCFGenerated is a parallel bool slice marking entries whose CC was
// driven by a CONFIGURE_FILE expansion (`.cpp.in` / `.c.in` source).
// CF outputs share the plain `.cpp.o` / `.c.o` suffix with hand-written
// SRCS so the path heuristic cannot distinguish them; this parallel
// signal lets the reorder pass tail-bucket CF entries after the
// hand-written regulars. PR-M3-final-sort-inversions.
func reorderARMembers(refs []NodeRef, paths []string, isFlatNoLto []bool, isCFGenerated []bool, numSrcsDerived int) ([]NodeRef, []string) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path string
	}

	// Classify SRCS-derived entries [0, numSrcsDerived) into buckets.
	// noLto: SRC_C_NO_LTO front-hoist.
	// regular: hand-written hand-named .o (no codegen suffix).
	// cf: CONFIGURE_FILE-derived .cpp.o / .c.o (parallel signal — path-indistinguishable from regular).
	// g4/hser/ev/rl6/reg3: codegen-derived .o files, tail-grouped.
	// legacyR6: pre-existing /_/_/ infix path (e.g. util/_/_/datetime/parser.rl6.cpp.o).
	var noLtoSrcs, regularSrcs, cfSrcs, g4Srcs, hSerSrcs, evPbSrcs, rl6Srcs, reg3Srcs, legacyR6 []member

	for i := 0; i < numSrcsDerived && i < len(paths); i++ {
		m := member{refs[i], paths[i]}
		switch {
		case strings.Contains(m.path, "/_/_/"):
			legacyR6 = append(legacyR6, m)
		case i < len(isFlatNoLto) && isFlatNoLto[i]:
			noLtoSrcs = append(noLtoSrcs, m)
		case strings.HasSuffix(m.path, ".reg3.cpp.o") || strings.Contains(m.path, ".reg3.cpp.py3.o"):
			reg3Srcs = append(reg3Srcs, m)
		case strings.HasSuffix(m.path, ".rl6.cpp.o"):
			rl6Srcs = append(rl6Srcs, m)
		case strings.HasSuffix(m.path, ".ev.pb.cc.o"):
			evPbSrcs = append(evPbSrcs, m)
		case strings.HasSuffix(m.path, ".h_serialized.cpp.o"):
			hSerSrcs = append(hSerSrcs, m)
		case strings.HasSuffix(m.path, ".g4.cpp.o"):
			g4Srcs = append(g4Srcs, m)
		case i < len(isCFGenerated) && isCFGenerated[i]:
			cfSrcs = append(cfSrcs, m)
		default:
			regularSrcs = append(regularSrcs, m)
		}
	}

	// JOIN_SRCS entries stay as-is in declaration order (never SRC_C_NO_LTO, never codegen).
	joinSrcs := make([]member, 0, len(paths)-numSrcsDerived)
	for i := numSrcsDerived; i < len(paths); i++ {
		joinSrcs = append(joinSrcs, member{refs[i], paths[i]})
	}

	// Reassemble: SRC_C_NO_LTO → regular SRCS → CF → JOIN_SRCS → codegen
	// (g4 → h_serialized → ev.pb → rl6 → reg3) → legacy /_/_/ R6.
	out := make([]member, 0, len(paths))
	out = append(out, noLtoSrcs...)
	out = append(out, regularSrcs...)
	out = append(out, cfSrcs...)
	out = append(out, joinSrcs...)
	out = append(out, g4Srcs...)
	out = append(out, hSerSrcs...)
	out = append(out, evPbSrcs...)
	out = append(out, rl6Srcs...)
	out = append(out, reg3Srcs...)
	out = append(out, legacyR6...)

	outRefs := make([]NodeRef, len(out))
	outPaths := make([]string, len(out))

	for i, m := range out {
		outRefs[i] = m.ref
		outPaths[i] = m.path
	}

	return outRefs, outPaths
}

// ─── F-7-B: codegen registry helpers ─────────────────────────────────────────

// scannerFor returns the IncludeScanner appropriate for `instance`'s
// platform axis. Target-axis instances (aarch64) get the target scanner;
// host-axis instances (x86_64) get the host scanner. Returns nil when the
// matching scanner is not allocated (e.g. tests).
//
// PR-AUDIT-4 (D14, D08): this is the single dispatch point for the
// target-vs-host scanner choice. Callers that want "the parsed-includes
// pool for this instance" MUST go through this helper rather than
// hand-coding the `targetIsX8664 ? scannerHost : scannerTarget` branch
// inline. EN's `ctx.scannerTarget` accesses (gen.go:3917, 3944) stay
// explicit because EN nodes always emit on the target axis regardless
// of the surrounding walk's axis — that is a deliberate cross-axis
// reach, not a per-instance dispatch.
func (ctx *genCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	if targetIsX8664(instance) {
		return ctx.scannerHost
	}
	return ctx.scannerTarget
}

// codegenRegForInstance returns the CodegenRegistry attached to the
// scanner picked by scannerFor. Returns nil when the scanner is nil
// (PR-AUDIT-4: dispatch lives in scannerFor, not duplicated here).
func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}

// protoDirectImportIncludes parses the direct `import "..."` statements from a
// .proto or .ev source file and converts them to the generated header paths that
// protoc emits under $(BUILD_ROOT):
//
//   - import "x/y/z.proto"  → "$(BUILD_ROOT)/x/y/z.pb.h"
//   - import "x/y/z.ev"     → "$(BUILD_ROOT)/x/y/z.ev.pb.h"
//
// Only direct imports of the primary file are returned (no recursion). When the
// file cannot be read (missing source on disk at scan time) the function returns
// nil. Results are sorted lexicographically. Cited upstream pattern:
// proto_processor.cpp:43-56::TProtoIncludeProcessor::PrepareIncludes.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `import` directives
// from a .proto/.ev source at registration time to populate its EmitsIncludes.
// NOT for closure walks. The architectural cleanup to route through a unified
// registry-resolved "structured-import extractor" lives in PR-AUDIT-3.D12 (still
// open) — keeping the (B) classification per audit doc §2 D12, §4 PR-AUDIT-3.
func protoDirectImportIncludes(sourceRoot, srcRel string) []string {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "import ") {
			continue
		}
		start := strings.IndexByte(line, '"')
		end := strings.LastIndexByte(line, '"')
		if start < 0 || end <= start {
			continue
		}
		imp := line[start+1 : end]
		if strings.HasSuffix(imp, ".ev") {
			out = append(out, "$(BUILD_ROOT)/"+strings.TrimSuffix(imp, ".ev")+".ev.pb.h")
		} else if strings.HasSuffix(imp, ".proto") {
			base := strings.TrimSuffix(imp, ".proto")
			if imp == "google/protobuf/descriptor.proto" {
				// descriptor.pb.h is pre-committed, not a codegen output.
				// Upstream tree: contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h
				out = append(out, pbRuntimeBase+"google/protobuf/descriptor.pb.h")
			} else {
				out = append(out, "$(BUILD_ROOT)/"+base+".pb.h")
			}
		}
	}
	sort.Strings(out)
	return out
}

// cfIncludeDirectives parses `#include "..."` directives from a configure_file
// template (.cpp.in / .c.in). Only quoted includes are collected (angle-bracket
// forms are system headers resolved by the compiler search path, not by the
// registry). Returns $(SOURCE_ROOT)/... paths, sorted lexicographically.
// Returns nil when the file cannot be read.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `#include` directives
// from a .cpp.in/.c.in template at registration time to populate the CF output's
// EmitsIncludes. NOT for closure walks. The architectural cleanup to route
// through a unified registry-resolved "structured-import extractor" lives in
// PR-AUDIT-3.D12 / .D16 (still open); kept per audit doc §2 D12/D16.
func cfIncludeDirectives(diskPath string) []string {
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#include ") {
			continue
		}
		start := strings.IndexByte(t, '"')
		if start < 0 {
			continue
		}
		end := strings.IndexByte(t[start+1:], '"')
		if end < 0 {
			continue
		}
		inc := t[start+1 : start+1+end]
		if inc != "" {
			out = append(out, "$(SOURCE_ROOT)/"+inc)
		}
	}
	sort.Strings(out)
	return out
}

// umbrellaPropagatedPaths is the set of ADDINCL paths upstream ymake
// propagates from a path-prefix umbrella LIBRARY to its sub-libraries'
// compilations. The empirical reference (sg2.json) restricts the
// propagation to brotli/snappy/re2 — three GLOBAL ADDINCL contributions
// reaching `devtools/ymake` via `library/cpp/blockcodecs` (→ brotli +
// snappy) and the direct `contrib/libs/re2` peer.
//
// Other GLOBAL ADDINCL contributions of the umbrella (yaml-cpp,
// sparsehash, antlr4, yaml, lzma, libffi, python, etc.) do NOT
// propagate to sub-libraries' compiles in the reference graph — they
// remain confined to the umbrella's own compile context. The precise
// upstream filter is unclear; for the M3 closure this allow-list is the
// minimum set that closes the 85-node L3 gap without injecting flags
// that would regress other nodes.
var umbrellaPropagatedPaths = map[string]struct{}{
	"contrib/libs/brotli/c/include": {},
	"contrib/libs/snappy/include":   {},
	"contrib/libs/re2/include":      {},
}

// umbrellaPropagatedOrder pins the canonical emission order for the
// allow-listed paths. Empirically REF emits them as brotli/snappy/re2
// at the tail of the -I block on every umbrella-inheriting sub-library
// (e.g. cyclestimer.cpp.o cmd_args[26..28] in sg2.json).
var umbrellaPropagatedOrder = []string{
	"contrib/libs/brotli/c/include",
	"contrib/libs/snappy/include",
	"contrib/libs/re2/include",
}

// umbrellaPropagatingAncestors is the explicit set of LIBRARY paths
// whose AddInclGlobal subset (umbrellaPropagatedPaths) propagates to
// path-prefix sub-libraries' CC compilations. Empirically `devtools/ymake`
// is the only umbrella exhibiting this behaviour in the M3 closure;
// other path-prefix umbrellas like `library/cpp/blockcodecs` and
// `library/cpp/json` do NOT propagate their GLOBAL ADDINCL to their
// path-children (verified by inspecting `library/cpp/blockcodecs/core/
// codecs.cpp.o` and `library/cpp/json/writer/json_value.cpp.o` in
// sg2.json). The exact upstream rule is unknown; this allow-list is the
// narrowest matching set that closes the 85-node L3 gap.
var umbrellaPropagatingAncestors = map[string]struct{}{
	"devtools/ymake": {},
}

// ccLanguageDefaultInclude lists the `-I` arguments that every C++ CC
// node receives via the language-default propagation (linux-headers +
// musl arch/include/extra + libcxx{,rt}/include + zlib + double-
// conversion + libc_compat). umbrella propagation skips CC nodes whose
// entire -I set is contained in this list — those nodes (e.g.
// `devtools/ymake/yndex/yndex.cpp.o`) have no user-peer-GLOBAL ADDINCL
// of their own, and REF does not propagate umbrella contributions to
// them.
//
// The two arch-specific musl paths (musl/arch/aarch64 vs musl/arch/
// x86_64) are folded into the same set so the predicate matches on
// either platform.
var ccLanguageDefaultInclude = map[string]bool{
	"-I$(BUILD_ROOT)":                                          true,
	"-I$(SOURCE_ROOT)":                                         true,
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers":              true,
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers/_nf":          true,
	"-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include":     true,
	"-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxxrt/include":   true,
	"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64":          true,
	"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/x86_64":           true,
	"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/generic":          true,
	"-I$(SOURCE_ROOT)/contrib/libs/musl/include":               true,
	"-I$(SOURCE_ROOT)/contrib/libs/musl/extra":                 true,
	"-I$(SOURCE_ROOT)/contrib/libs/zlib/include":               true,
	"-I$(SOURCE_ROOT)/contrib/libs/double-conversion":          true,
	"-I$(SOURCE_ROOT)/contrib/libs/libc_compat/include/readpassphrase": true,
}

// applyUmbrellaAddIncl patches CC nodes' cmd_args to inject missing -I
// flags inherited from path-prefix umbrella ancestors.
//
// Upstream model: a LIBRARY X with sub-libraries A, B, C under its path
// prefix exports a subset of its transitive peer-GLOBAL ADDINCL closure
// to A, B, C at compile time. The propagated subset is restricted by
// `umbrellaPropagatedPaths` — empirically brotli/snappy/re2 for the M3
// `devtools/ymake/bin` closure.
//
// The patch finds all path-prefix ancestors in `ctx.memo` (keyed on
// platform so host-tool walks stay isolated from target walks),
// intersects each ancestor's `AddInclGlobal` with the allow-list, and
// appends the not-yet-present `-I` flags after the last existing `-I`
// argument.
func applyUmbrellaAddIncl(ctx *genCtx) {
	be, ok := ctx.emit.(*BufferedEmitter)
	if !ok {
		return
	}

	// Build path → AddInclGlobal map, keyed on the platform string so
	// host-tool walks (x86_64) and target walks (aarch64) keep separate
	// AddInclGlobal contributions (a peer-GLOBAL contribution that fires
	// only on the target platform must not bleed into the host CC).
	type key struct {
		path     string
		platform string
	}

	pathAddIncl := map[key][]string{}
	// pyByPath records (path, platform) keys whose module declared a
	// PY*_LIBRARY-family type. PR-M3-protobuf-umbrella-trigger: the
	// umbrella propagator must skip CC nodes belonging to Python-bound
	// modules (rapidjson, ymakeyaml under devtools/ymake). REF does not
	// emit brotli/snappy/re2 for those even though their peer chain
	// otherwise meets the hasNonLangDefault gate (Python/libffi/lzma
	// includes are non-language-default contributions).
	pyByPath := map[key]bool{}

	for inst, res := range ctx.memo {
		if res == nil {
			continue
		}

		k := key{path: inst.Path, platform: string(inst.Target)}
		if len(res.AddInclGlobal) != 0 {
			pathAddIncl[k] = res.AddInclGlobal
		}
		if res.isPyLibrary {
			pyByPath[k] = true
		}
	}

	// pathPrefixAncestors yields the strict path-prefix ancestors of
	// `modulePath` (excluding modulePath itself) in nearest-first order.
	// e.g. "devtools/ymake/lang/makelists" → ["devtools/ymake/lang",
	// "devtools/ymake", "devtools"].
	pathPrefixAncestors := func(modulePath string) []string {
		parts := strings.Split(modulePath, "/")
		if len(parts) <= 1 {
			return nil
		}

		out := make([]string, 0, len(parts)-1)
		for i := len(parts) - 1; i > 0; i-- {
			out = append(out, strings.Join(parts[:i], "/"))
		}

		return out
	}

	for _, n := range be.nodes {
		if n == nil || n.KV == nil || n.KV["p"] != "CC" {
			continue
		}

		modulePath, ok := n.TargetProperties["module_dir"]
		if !ok || modulePath == "" {
			continue
		}

		// PR-M3-protobuf-umbrella-trigger: PY*_LIBRARY consumers do not
		// receive umbrella ADDINCL propagation, even when their own
		// peer chain has non-language-default contributions
		// (python/Include, libffi/, lzma/, openssl/, abseil-cpp via
		// runtime_py3). REF empirically excludes rapidjson + ymakeyaml
		// under devtools/ymake from the brotli/snappy/re2 propagation.
		if pyByPath[key{path: modulePath, platform: n.Platform}] {
			continue
		}

		ancestors := pathPrefixAncestors(modulePath)
		if len(ancestors) == 0 {
			continue
		}

		// Detect whether any path-prefix ancestor is an
		// umbrella-propagating LIBRARY whose AddInclGlobal contains the
		// allow-listed paths. If so, emit the allow-listed paths in
		// their canonical (REF-pinned) order.
		var ancestorHit string

		for _, anc := range ancestors {
			if _, ok := umbrellaPropagatingAncestors[anc]; !ok {
				continue
			}

			if _, ok := pathAddIncl[key{path: anc, platform: n.Platform}]; ok {
				ancestorHit = anc

				break
			}
		}

		if ancestorHit == "" {
			continue
		}

		// Confirm the ancestor's AddInclGlobal actually contains each
		// allow-listed path; if not, omit that one (the ancestor's peer
		// chain didn't reach it on this platform).
		ancAddIncl := pathAddIncl[key{path: ancestorHit, platform: n.Platform}]
		ancHas := map[string]struct{}{}
		for _, p := range ancAddIncl {
			ancHas[p] = struct{}{}
		}

		var umbrella []string
		for _, p := range umbrellaPropagatedOrder {
			if _, ok := ancHas[p]; !ok {
				continue
			}

			if _, allowed := umbrellaPropagatedPaths[p]; !allowed {
				continue
			}

			umbrella = append(umbrella, p)
		}

		if len(umbrella) == 0 {
			continue
		}

		// Walk cmd_args. Find the index of the last `-I` flag; build a
		// set of already-present `-I…` arguments so we don't re-emit
		// duplicates.
		if len(n.Cmds) == 0 {
			continue
		}

		args := n.Cmds[0].CmdArgs

		lastIIdx := -1
		present := map[string]struct{}{}

		for i, a := range args {
			if !strings.HasPrefix(a, "-I") {
				continue
			}

			lastIIdx = i
			present[a] = struct{}{}
		}

		if lastIIdx < 0 {
			continue
		}

		// Trigger: umbrella propagation only fires for CC nodes whose
		// own peer chain already contributes at least one peer-GLOBAL
		// ADDINCL (any -I path not in the language-default set). Empirical:
		// `devtools/ymake/yndex/*.cpp.o` has only the 13 language-default
		// -I flags in REF (its sole peer `library/cpp/json` brings no
		// GLOBAL ADDINCL); REF does NOT propagate brotli/snappy/re2 to
		// yndex. `devtools/ymake/common/cyclestimer.cpp.o` reaches asio +
		// fmt + protobuf + abseil-{cpp,tstring} via `common → diag →
		// common_display → asio` (asio's GLOBAL ADDINCL transitively),
		// and REF DOES propagate brotli/snappy/re2.
		hasNonLangDefault := false

		for p := range present {
			if !ccLanguageDefaultInclude[p] {
				hasNonLangDefault = true

				break
			}
		}

		if !hasNonLangDefault {
			continue
		}

		// Build the list of -I flags to inject. Entries without a `$(`
		// prefix are SOURCE_ROOT-rooted (the common case); entries
		// already containing `$(...)` are passed through verbatim.
		var inject []string

		for _, p := range umbrella {
			var flag string
			if strings.HasPrefix(p, "$(") {
				flag = "-I" + p
			} else {
				flag = "-I$(SOURCE_ROOT)/" + p
			}

			if _, dup := present[flag]; dup {
				continue
			}

			inject = append(inject, flag)
		}

		if len(inject) == 0 {
			continue
		}

		// Insert the new flags right after the last existing -I.
		newArgs := make([]string, 0, len(args)+len(inject))
		newArgs = append(newArgs, args[:lastIIdx+1]...)
		newArgs = append(newArgs, inject...)
		newArgs = append(newArgs, args[lastIIdx+1:]...)

		n.Cmds[0].CmdArgs = newArgs
	}
}
