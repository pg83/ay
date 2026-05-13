package main

import (
	"bufio"
	"fmt"
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
// ragel6 instance (`instance.WithHost(ctx.host)` with Path overridden
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
	// umbrella-trigger: the umbrella branch of newPostEmitPrepare reads
	// this flag to suppress umbrella ADDINCL propagation into Python-
	// bound sub-libraries that sit under a propagating ancestor's path
	// prefix.
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
	// `$(B)/contrib/libs/musl/include/musl.py.pyplugin` and
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
	// Peerdirs is the post-collect effective peer list for this module
	// (user-declared PEERDIRs after the auto-injection of contrib/libs/python
	// for PY*_LIBRARY families, etc.). PR-M3-symbols-module-abseil-addincl:
	// the back-peer branch of newPostEmitPrepare reads this to compute
	// the back-peer registry — when a module P PEERDIRs a module M, M is
	// a back-peer-child of P and inherits P's AddInclGlobal closure
	// (filtered to non-language-default).
	// SOURCE_ROOT-relative paths.
	Peerdirs []string
	// ModuleStmtName records the module declaration name (PY23_LIBRARY,
	// LIBRARY, PROGRAM, ...). PR-M3-symbols-module-abseil-addincl:
	// the back-peer branch of newPostEmitPrepare gates the back-peer
	// propagation on the parent declaring a PY*_LIBRARY family that
	// auto-PEERDIRs contrib/libs/python.
	ModuleStmtName string
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
	// $(B)-rooted output path; the value is the EN NodeRef.
	//
	// EN nodes only ever emit on the target axis (see gen.go:4531-4535), so
	// a flat path-keyed map collapses cleanly. PB/EV use codegenOutputKey
	// because both axes can emit their own producer NodeRefs.
	enOutputs map[VFS]NodeRef
	// pbOutputs/evOutputs map (platform, $(B)-rooted output path)
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
	// (`$(B)/<modulePath>/<name>.pyplugin`) is sufficient: the
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

	// PR-M3-platform-pair: the canonical (host, target) Platform pair
	// constructed once in GenWithMode from the CLI args. Threaded through
	// every emitter so renderers read `instance.Platform.Target` / `instance.Platform.Flags` /
	// `instance.Platform.Tags` / `instance.Platform.IsHost` instead of inferring "am I a host
	// build?" from `targetIsX8664(instance)`. See platform.go header.
	//
	// Tool sub-graph emission flips the second slot to host: the recursive
	// emit call passes `(host, host)` so the rendered nodes carry
	// `node.platform = host.Target`, `node.host_platform = true`,
	// `node.tags = host.Tags` (`["tool"]`) without any branch in the
	// renderer.
	host   *Platform
	target *Platform
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
	path     VFS
}

// resolveCodegenDepRefs scans `includeInputs` for $(B)-rooted paths
// that match a previously emitted EN/PB/EV/AR/CF/BI/JV/PR/R5/PY producer
// output, and returns the producer NodeRefs deduped in scan order. Each
// consumer CC node carries those NodeRefs as ExtraDepRefs so the resulting
// CC `deps` list mirrors the reference graph shape (sg2.json places explicit
// codegen-producer deps on every CC whose inputs[] references a $(B)/
// <gen>.h or <gen>.cc).
//
// `consumer.Platform.Target` disambiguates per-platform PB/EV lookup. EN nodes always
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
func resolveCodegenDepRefs(ctx *genCtx, consumer ModuleInstance, includeInputs []VFS, exclude ...NodeRef) []NodeRef {
	return resolveCodegenDepRefsExt(ctx, consumer, includeInputs, nil, exclude...)
}

// resolveCodegenDepRefsExt is the extended form that also scans `inputs` for
// $(B) producer paths. Used by consumers whose producer ref is
// input-driven (RESOURCE objcopy via .pyc.inc, .yapyc3 bytecode) rather than
// #include-driven. The two slices are scanned in order; dedup is global.
func resolveCodegenDepRefsExt(ctx *genCtx, consumer ModuleInstance, includeInputs, inputs []VFS, exclude ...NodeRef) []NodeRef {
	if len(includeInputs) == 0 && len(inputs) == 0 {
		return nil
	}

	seen := make(map[NodeRef]struct{}, 4+len(exclude))
	for _, r := range exclude {
		seen[r] = struct{}{}
	}

	var out []NodeRef

	probe := func(v VFS) {
		if !v.IsBuild() {
			return
		}

		var ref NodeRef
		var ok bool

		if r, found := ctx.enOutputs[v]; found {
			ref, ok = r, true
		} else if r, found := ctx.pbOutputs[codegenOutputKey{platform: consumer.Platform.Target, path: v}]; found {
			ref, ok = r, true
		} else if r, found := ctx.evOutputs[codegenOutputKey{platform: consumer.Platform.Target, path: v}]; found {
			ref, ok = r, true
		} else {
			reg := codegenRegForInstance(ctx, consumer)
			if reg != nil {
				if info, found := reg.Lookup(v); found && info.HasProducerRef {
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

// runGenInto runs the Gen walk against the supplied emitter without
// calling Finalize on it. Returns the root NodeRef and a per-node
// `prepare` mutator: the caller must thread the mutator through their
// finalize step (Finalize / FinalizeStreamWith) so the umbrella +
// back-peer cmd_args mutations happen inline, per-node, before each
// node's UID is computed. There is no longer a separate
// applyUmbrellaAddIncl / applyBackPeerAddIncl batch pass that has to
// walk every node twice before the first compile can fire.
func runGenInto(srcRoot, targetDir string, cliDefines map[string]string, emitter Emitter, mode string) (NodeRef, func(*Node)) {
	if mode != "local" && mode != "interned" {
		ThrowFmt("gen: --scan-ctx-mode must be \"local\" or \"interned\", got %q", mode)
	}

	if cliDefines == nil {
		cliDefines = map[string]string{"MUSL": "yes"}
	}

	sharedPC := newSharedParseCache()

	targetReg := NewCodegenRegistry()
	hostReg := NewCodegenRegistry()

	targetScanner := newIncludeScannerWith(srcRoot, LoadSysInclSetFor(srcRoot, "aarch64"), sharedPC)
	targetScanner.codegen = targetReg
	targetScanner.fallbackLocators = []pathLocator{codegenLocator{reg: targetReg}}
	hostScanner := newIncludeScannerWith(srcRoot, LoadSysInclSetFor(srcRoot, "x86_64"), sharedPC)
	hostScanner.codegen = hostReg
	hostScanner.fallbackLocators = []pathLocator{codegenLocator{reg: hostReg}}

	hostP, targetP := defaultLinuxPlatforms(cliDefines)

	ctx := &genCtx{
		cfg:             TargetCfg,
		sourceRoot:      srcRoot,
		emit:            emitter,
		memo:            make(map[ModuleInstance]*moduleEmitResult),
		walking:         make(map[ModuleInstance]bool),
		host:            hostP,
		target:          targetP,
		scannerTarget:   targetScanner,
		scannerHost:     hostScanner,
		cliDefines:      cliDefines,
		enOutputs:       make(map[VFS]NodeRef),
		pbOutputs:       make(map[codegenOutputKey]NodeRef),
		evOutputs:       make(map[codegenOutputKey]NodeRef),
		ldPluginCPCache: make(map[string]NodeRef),
		scanCtxMode:     mode,
		internedScanCtx: make(map[scanCtxCacheKey]*scanCtx, 64),
	}

	ctx.localScanCtxStack = []map[scanCtxCacheKey]*scanCtx{make(map[scanCtxCacheKey]*scanCtx, 4)}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Language: LangCPP,
		Platform: targetP,
		Flags:    inferFlagsFromPath(filepath.Clean(targetDir), false),
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	return root.LDRef, newPostEmitPrepare(ctx)
}

// GenWithMode is GenWith plus the scanCtxMode dispatch knob (PR-M3-perf-E).
// `mode` must be either "local" or "interned"; anything else throws.
// Wraps runGenInto: the per-node umbrella + back-peer prepare hook
// runGenInto returns is threaded through FinalizeWith so the mutations
// apply per-node inline, not as two separate full-graph post-passes.
func GenWithMode(cfg PlatformConfig, sourceRoot string, targetDir string, cliDefines map[string]string, mode string) *Graph {
	emitter := NewBufferedEmitter()
	_, prepare := runGenInto(sourceRoot, targetDir, cliDefines, emitter, mode)
	_ = cfg // PlatformConfig is reserved for the M5 host-cross-compile dispatch.

	return FinalizeWith(emitter, prepare)
}

// moduleData is the per-module accumulator populated by
// `collectModule`. It captures everything the rule-emission stage
// needs after macro evaluation has flattened IF branches and
// inlined macros. The `flags` field starts from the path-based
// heuristic and is overlaid with macro-derived bools (NO_LIBC etc.).


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
		fmt.Fprintf(os.Stderr, "%sgenModule %s@%s  (from %s)\n", indent, instance.Path, instance.Platform.Target, caller)
		ctx.traceStack = append(ctx.traceStack, instance.Path+"@"+string(instance.Platform.Target))
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
		protoResult := emitProtoSrcs(ctx, instance, d, peerContribs)

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
		if protoResult != nil {
			hOnlyARRef = protoResult.ARRef
			hOnlyARPath = protoResult.ARPath.String()
			hOnlyHasAR = true
		}

		peerArchivePathsH := peerContribs.archivePaths
		peerArchiveRefsH := peerContribs.archiveRefs
		peerGlobalPathsH := peerContribs.globalPaths
		peerGlobalRefsH := peerContribs.globalRefs

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
			PeerArchiveClosureRefs:  peerArchiveRefsH,
			PeerArchiveClosurePaths: peerArchivePathsH,
			PeerGlobalClosureRefs:   peerGlobalRefsH,
			PeerGlobalClosurePaths:  peerGlobalPathsH,
			LDPluginRefs:            ldPluginRefs,
			LDPluginPaths:           ldPluginPaths,
			InducedDeps:             append([]string(nil), d.inducedDeps...),
			Peerdirs:                append([]string(nil), d.peerdirs...),
			ModuleStmtName:          d.moduleStmt.Name,
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

	// PASS A: resolve peers and aggregate LD plugin closure in source
	// (declaration) order. Archive / .global.a aggregation is deferred to
	// PASS B so it can use a different iteration order without disturbing
	// the AddInclGlobal aggregation (which iterates `resolved` directly
	// in this order downstream).
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

		// PR-35k: fold peer's LD plugin closure (own ∪ transitive) into
		// our own. Runs for BOTH header-only and non-header peers — the
		// only M2 plugin (musl.py.pyplugin) is owned by the header-only
		// `contrib/libs/musl/include` LIBRARY.
		for i, p := range peerResult.LDPluginPaths {
			peerLDPluginAddPath(peerResult.LDPluginRefs[i], p)
		}
	}

	// PASS B: archive + .global.a aggregation. Iterates `resolved` in an
	// archive-specific order so PROGRAM-class consumers (ymake/bin,
	// py3cc/slow/bin) can defer USE_PYTHON3's implicit peers (contrib/
	// libs/python, library/python/runtime_py3) to AFTER user-declared
	// PEERDIRs. The AddInclGlobal aggregation below continues to iterate
	// `resolved` in source order so CC nodes see the existing -I... slot
	// ordering (source-order is empirically correct for the include path
	// slots; deferral there regressed 142 nodes).
	//
	// PR-M3-peer-archive-multimodule-partition: USE_PYTHON3() implicit
	// peers (contrib/libs/python + library/python/runtime_py3) are
	// reordered to the TAIL of the archive aggregation for PROGRAM-class
	// modules. Upstream's `macro USE_PYTHON3() { PEERDIR(contrib/libs/
	// python); when (...) { PEERDIR+=runtime_py3 } }` (python.conf:1063-
	// 1071) effectively appends both peers to the tail of the module's
	// peer list (the `when` block is explicitly deferred; the plain
	// `PEERDIR(...)` inside a macro body behaves the same way upstream).
	// For PY*_PROGRAM consumers (py3cc/slow/bin) REF places runtime_py3
	// AFTER user-PEERDIR runtime_py3/main, matching this deferral and
	// closing the cmd[2] slot 65-66 inversion.
	// Scoped to PROGRAM/PY*_PROGRAM only — LIBRARY-class consumers depend
	// on the existing peer order for their PeerArchiveClosurePaths
	// propagation (LIBRARY-scope deferral regressed 100+ L3 nodes).
	archiveOrder := resolved
	if d.usePython3 && d.moduleStmt != nil {
		// PR-M3-python-closure-order: tail-defer USE_PYTHON3 implicit peers
		// (contrib/libs/python + library/python/runtime_py3) only for
		// PY*_PROGRAM* modules. For plain PROGRAM modules that opted into
		// USE_PYTHON3 (e.g. devtools/ymake/bin), upstream's macro prepends
		// these peers BEFORE the user PEERDIR block, so the python closure
		// must land FIRST in the archive aggregation — not deferred. The
		// tail reorder is still required for PY3_PROGRAM_BIN / PY*_PROGRAM
		// where the user-PEERDIR(runtime_py3) intentionally dedups against
		// the implicit macro injection (py3cc/slow/bin cmd[2] slot 65-66).
		switch d.moduleStmt.Name {
		case "PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM":
			head := make([]resolvedPeer, 0, len(resolved))
			tail := make([]resolvedPeer, 0, 2)

			for _, rp := range resolved {
				if rp.path == "contrib/libs/python" || rp.path == "library/python/runtime_py3" {
					tail = append(tail, rp)

					continue
				}

				head = append(head, rp)
			}

			archiveOrder = append(head, tail...)
		}
	}

	for _, rp := range archiveOrder {
		peerResult := rp.result

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
			// ARPath has "$(B)/" prefix; cmd_args use a
			// bare relative path. Strip the prefix for consistency.
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(B)/")
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
	// propagates `$(B)/library/python/runtime_py3` to consumers
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
		const buildRootPath = "$(B)/library/python/runtime_py3"
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
	ccOutputs := make([]VFS, 0, len(d.srcs)+len(d.joinSrcs))
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
	memberInputs := make([]VFS, 0, 64)
	memberInputsSeen := map[VFS]struct{}{}

	addMemberInputs := func(paths []VFS) {
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
	regularPrimariesSet := map[VFS]struct{}{}
	addRegularPrimary := func(p VFS) {
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
	if isPROGRAMForMuslDef(d.moduleStmt.Name) && cliMuslOn(ctx) && !instance.Flags.LibcMusl && !effectiveNoPlatform(instance.Flags) {
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
	// addincl slot. Generated-output paths (`$(B)/<mod>`) are
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
	preEmitted := make(map[string]*sourceEmit, 4)

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

		preEmitted[src] = emitOneSource(ctx, instance, d.srcDir, src, srcInputs, ancestorRebase)
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

		emit, hadPre := preEmitted[src]
		if !hadPre {
			emit = emitOneSource(ctx, instance, d.srcDir, src, srcInputs, ancestorRebase)
		}

		if emit == nil {
			continue
		}

		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, isFlatNoLto)
		ccIsCFGenerated = append(ccIsCFGenerated, strings.HasSuffix(src, ".cpp.in") || strings.HasSuffix(src, ".c.in"))
		addMemberInputs(emit.CcIns)
		// PR-35y R8: track primary source paths so the .global.a
		// aggregator can exclude them. emitOneSource returns the
		// ccIns slice with the leading `PrimaryCount` entries being
		// the member's primary source(s) — `.cpp/.c/.cc/.cxx`/`.S`
		// dispatch yields 1 primary; `.rl6` dispatch yields 1 (the
		// .rl6 source) or 2 (when the `.h` companion exists on disk).
		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
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
		headerVFS := resolveSourceVFS(ctx, instance, src, moduleInputs.SrcDir)
		headerClosure := walkClosure(ctx, instance, headerVFS, moduleInputs)
		all := append([]VFS{Source(instance.Path + "/" + src)}, headerClosure...)
		addMemberInputs(all)
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

		emit := emitOneSource(ctx, instance, d.srcDir, e.Src, variantIn, ancestorRebase)
		if emit == nil {
			continue
		}

		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, true)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(emit.CcIns)
		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
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
		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		if targetIsX8664(srcInstance) {
			// When this module is reached through a host (x86_64) walk
			// the JS node nevertheless emits on the target axis (see the
			// EmitJS call below — Platform is anchored to the outer-target
			// ID). Recompute the include closure with the target scanner +
			// target-arch musl search paths rebased; the surrounding host
			// walk's instance is kept verbatim — only the override
			// `scanPlatform` argument flips.
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = jsTargetPeerAddIncl(moduleInputs.PeerAddInclGlobal)

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, srcInstance, js.Sources, jsModuleInputs)
		}

		// PR-35s: anchor the JS node to the outer-target platform
		// (`ctx.cfg.Target.ID`) regardless of whether this module
		// instance was reached through a host-PROGRAM walk. The
		// reference graph emits every JS (JOIN_SRCS) node on
		// `default-linux-aarch64` — including the 7 JOIN_SRCS in
		// `contrib/tools/ragel6/bin` whose surrounding LD lives on
		// the host axis. Only the JS Platform axis detaches; the
		// downstream JS-derived CC node below continues to compile
		// at `srcInstance.Platform.Target` (host x86_64 for ragel6/bin) so
		// the .pic.o output stays on the correct compile axis.
		jsRef, joinOut := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.cfg.Target.ID, ctx.emit)

		// EmitJS returns a $(B)/<srcInstance.Path>/<name>
		// absolute path; convert to srcInstance-relative for the
		// downstream EmitCC. PR-29-D07: the JS output lives under
		// $(B) — pass IsGenerated so EmitCC composes the
		// inputPath under $(B) instead of $(S).
		// PR-30 D04: thread the JS NodeRef as the downstream CC's
		// `Generator` so the CC node carries an explicit dep on its
		// source-generating JS node, matching the reference shape
		// (every JS-derived CC in the reference has DepRefs=[js UID]).
		// PR-29 deferred this because the wider closure had not yet
		// landed; PR-30's musl/full + ALLOCATOR_IMPL closure widening
		// absorbs the 2-multiset cost many times over.
		jsRel := strings.TrimPrefix(joinOut, "$(B)/"+srcInstance.Path+"/")

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
		// util's libyutil.a never lists `$(B)/util/all_*.cpp`
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
			addRegularPrimary(Source(srcInstance.Path + "/" + s))
		}
	}

	// GLOBAL_SRCS get their own CC nodes and a separate AR pass
	// (see below). Filter headers here too.
	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]VFS, 0, len(d.globalSrcs))

	// PR-31 D11: GLOBAL_SRCS contribute their own member-inputs slice
	// to the .global.a archive (separate accumulator from regular AR).
	globalMemberInputs := make([]VFS, 0, 16)
	globalMemberInputsSeen := map[VFS]struct{}{}

	for _, src := range d.globalSrcs {
		emit := emitOneSource(ctx, instance, d.srcDir, src, moduleInputs, ancestorRebase)

		if emit == nil {
			continue
		}

		globalRefs = append(globalRefs, emit.Ref)
		globalOutputs = append(globalOutputs, emit.OutPath)

		for _, p := range emit.CcIns {
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
		// folded into the LD's SRCS_GLOBAL slot (the reference graph wraps
		// the program LD around the per-resource objcopy `.o` files).
		// PR-M3-py3cc-objcopy-shape: objcopy paths now flow through a
		// dedicated EmitLD slot — they go BEFORE $VCS_C_OBJ in cmd[2] and
		// emit BUILD_ROOT-relative (bare) per ld.conf:229-230 +
		// ${rootrel;ext=.o:SRCS_GLOBAL}.
		ldCCRefs := ccRefs
		ldCCOutputs := ccOutputs
		ldMemberInputs := memberInputs
		var ldObjcopyRefs []NodeRef
		var ldObjcopyPaths []VFS

		if d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			emitPySrcs(ctx, instance, d)

			objcopyRefs, objcopyOutputs, _ := emitResourceObjcopy(ctx, instance, d)

			if len(objcopyRefs) > 0 {
				ldObjcopyRefs = objcopyRefs
				ldObjcopyPaths = objcopyOutputs
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
					seen := make(map[VFS]struct{}, len(ldMemberInputs))
					for _, p := range ldMemberInputs {
						seen[p] = struct{}{}
					}

					ldMemberInputs = append([]VFS(nil), ldMemberInputs...)

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

		// PR-M3-py3-program-bin-strip-all: PY3_PROGRAM_BIN's upstream
		// `_BASE_PY3_PROGRAM` macro (build/conf/python.conf:884) calls
		// STRIP(), which sets STRIP_FLAG=$LD_STRIP_FLAG=-Wl,--strip-all
		// on Linux. PY3_PROGRAM (cpp module_lang) does not exercise the
		// strip path in the M3 closure.
		wantsStrip := d.moduleStmt.Name == "PY3_PROGRAM_BIN"

		ldRef := EmitLD(
			ldInstance,
			binaryName,
			ldCCRefs, ldCCOutputs,
			ldPeerArchiveRefs, ldPeerArchivePaths,
			mergedLDPluginRefs, mergedLDPluginPaths,
			peerGlobalRefs, peerGlobalPaths,
			ldObjcopyRefs, ldObjcopyPaths,
			ldMemberInputs,
			cliMuslOn(ctx),
			ownCFlags,
			peerCFlagsGlobal,
			d.usePython3,
			wantsStrip,
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
			Peerdirs:                append([]string(nil), d.peerdirs...),
			ModuleStmtName:          d.moduleStmt.Name,
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
		combinedMemberInputs = make([]VFS, 0, len(memberInputs)+len(globalMemberInputs))
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
				combinedMemberInputs = append([]VFS(nil), memberInputs...)
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
	// (`$(S)/<modulePath>/<name>.pyplugin`) when the macro
	// fired on this module's ya.make.
	var arPluginVFS *VFS
	if d.arPlugin != "" {
		v := Source(instance.Path + "/" + d.arPlugin)
		arPluginVFS = &v
	}

	if len(ccRefs) > 0 {
		// PR-M3-module-tag-and-stats-enums-dep: PY23_LIBRARY / PY23_NATIVE_LIBRARY
		// surface `module_tag=py3` / `module_tag=py3_native`.
		// PR-M3-openssl-ar-plugin-and-as-clean: openssl AR_PLUGIN(ar) injects
		// `--plugin <ar.pyplugin>` between the link_lib.py `--` separators.
		if perModuleCCTag != "" {
			arRef = EmitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.emit)
		}
	}

	_ = peerArchiveRefs // retained as a loop accumulator for the PROGRAM LD branch above; intentionally unused for the LIBRARY AR.
	arPath := Build(instance.Path + "/" + arBaseName).String()

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
		Peerdirs:                append([]string(nil), d.peerdirs...),
		ModuleStmtName:          d.moduleStmt.Name,
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
// filterBuildRootSelfPaths drops `$(B)/...` paths from `peer`
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
		if strings.HasPrefix(p, "$(B)/") {
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

// mergeDedupVFS is mergeDedup for []VFS — keeps order of first
// occurrence and drops later duplicates.
func mergeDedupVFS(a, b []VFS) []VFS {
	out := make([]VFS, 0, len(a)+len(b))
	seen := make(map[VFS]struct{}, len(a)+len(b))

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
func filterEnSerializedSiblings(in []VFS) []VFS {
	out := make([]VFS, 0, len(in))

	for _, p := range in {
		if strings.HasSuffix(p.Rel, "_serialized.cpp") || strings.HasSuffix(p.Rel, "_serialized.h") {
			continue
		}

		out = append(out, p)
	}

	return out
}

// emitOwnLDPlugins emits one CP node per `LD_PLUGIN(name.py)` entry
// declared in this module. The CP src is
// `$(S)/<modulePath>/<name>` and the dst is
// `$(B)/<modulePath>/<name>.pyplugin` (verified against the
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
		src := Source(instance.Path + "/" + name)
		dst := Build(instance.Path + "/" + name + ".pyplugin")

		ref, ok := ctx.ldPluginCPCache[dst.String()]

		if !ok {
			ref = EmitCP(instance, src, dst, ctx.emit)
			ctx.ldPluginCPCache[dst.String()] = ref
		}

		refs = append(refs, ref)
		paths = append(paths, dst.String())
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
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(B)/")
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
// view of the module: SRCS resolve to `$(S)/<srcDir>/<rel>`
// and the emitted node's `module_dir` becomes `<srcDir>` instead of
// `instance.Path`. The LD/AR/Global archives that wrap these sources
// remain at `instance.Path` (the walker called from genModule keeps
// instance unchanged for those). For ragel6/bin: `instance.Path =
// contrib/tools/ragel6/bin`, `srcDir = contrib/tools/ragel6` →
// per-source CC nodes show `module_dir = contrib/tools/ragel6` and
// inputs `$(S)/contrib/tools/ragel6/<src>`, while the
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
func reorderARMembers(refs []NodeRef, paths []VFS, isFlatNoLto []bool, isCFGenerated []bool, numSrcsDerived int) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
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
		rel := m.path.Rel
		switch {
		case strings.Contains(rel, "/_/_/"):
			legacyR6 = append(legacyR6, m)
		case i < len(isFlatNoLto) && isFlatNoLto[i]:
			noLtoSrcs = append(noLtoSrcs, m)
		case strings.HasSuffix(rel, ".reg3.cpp.o") || strings.Contains(rel, ".reg3.cpp.py3.o"):
			reg3Srcs = append(reg3Srcs, m)
		case strings.HasSuffix(rel, ".rl6.cpp.o"):
			rl6Srcs = append(rl6Srcs, m)
		case strings.HasSuffix(rel, ".ev.pb.cc.o"):
			evPbSrcs = append(evPbSrcs, m)
		case strings.HasSuffix(rel, ".h_serialized.cpp.o"):
			hSerSrcs = append(hSerSrcs, m)
		case strings.HasSuffix(rel, ".g4.cpp.o"):
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
	outPaths := make([]VFS, len(out))

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
	return ctx.scannerForPlatform(instance.Platform)
}

// scannerForPlatform returns the scanner pinned to `p`. PR-AUDIT-4: the
// per-platform dispatch lives here; `scannerFor` is a thin wrapper around
// it that derives the platform from a ModuleInstance. Callers that need
// to resolve includes against a DIFFERENT platform than their instance
// (e.g. JOIN_SRCS forcing target-arch search paths during a host walk)
// call this overload directly with the override platform.
func (ctx *genCtx) scannerForPlatform(p *Platform) *IncludeScanner {
	if p.Target == PlatformDefaultLinuxX8664 {
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
// protoc emits under $(B):
//
//   - import "x/y/z.proto"  → "$(B)/x/y/z.pb.h"
//   - import "x/y/z.ev"     → "$(B)/x/y/z.ev.pb.h"
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
func protoDirectImportIncludes(sourceRoot, srcRel string) []VFS {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []VFS
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
			out = append(out, Build(strings.TrimSuffix(imp, ".ev")+".ev.pb.h"))
		} else if strings.HasSuffix(imp, ".proto") {
			base := strings.TrimSuffix(imp, ".proto")
			if imp == "google/protobuf/descriptor.proto" {
				// descriptor.pb.h is pre-committed, not a codegen output.
				// Upstream tree: contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h
				out = append(out, Source(pbRuntimeBase+"google/protobuf/descriptor.pb.h"))
			} else {
				out = append(out, Build(base+".pb.h"))
			}
		}
	}
	SortVFS(out)
	return out
}

// cfIncludeDirectives parses `#include "..."` directives from a configure_file
// template (.cpp.in / .c.in). Only quoted includes are collected (angle-bracket
// forms are system headers resolved by the compiler search path, not by the
// registry). Returns Source-rooted VFSes, sorted lexicographically.
// Returns nil when the file cannot be read.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `#include` directives
// from a .cpp.in/.c.in template at registration time to populate the CF output's
// EmitsIncludes. NOT for closure walks. The architectural cleanup to route
// through a unified registry-resolved "structured-import extractor" lives in
// PR-AUDIT-3.D12 / .D16 (still open); kept per audit doc §2 D12/D16.
func cfIncludeDirectives(diskPath string) []VFS {
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return nil
	}
	var out []VFS
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
			out = append(out, Source(inc))
		}
	}
	SortVFS(out)
	return out
}

