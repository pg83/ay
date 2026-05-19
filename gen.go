package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver. Walks PEERDIR DFS, post-order, declaration-order, keyed on
// `ModuleInstance`. IF/ELSE branches are evaluated against a per-instance
// env; unreached branches contribute nothing. Source dispatch by
// extension: `.c/.cpp/.cc/.cxx` → EmitCC; `.h/.hpp` silently skipped;
// `.S/.s` → EmitAS; `.rl6` → EmitR6 then EmitCC of the generated `.cpp`.

// moduleEmitResult is the per-instance "what did we emit?" record
// kept by `genCtx.memo`. LIBRARY populates ARRef; LDRef/LDPath alias
// to ARRef/ARPath so a PROGRAM peering this LIBRARY wires through the
// AR fields. PROGRAM populates LDRef; ARRef/ARPath alias defensively.
// `isPROGRAM` flags shape for the caller.
type moduleEmitResult struct {
	ARRef      NodeRef
	ARPath     *VFS
	isPROGRAM  bool
	LDRef      NodeRef
	LDPath     *VFS
	GlobalRef  *NodeRef // non-nil when the module has GLOBAL_SRCS (EmitARGlobal was called)
	GlobalPath *VFS     // BUILD_ROOT path to the .global.a archive; non-nil when GlobalRef is non-nil
	// WholeArchiveRefs / WholeArchivePaths are this module's own
	// `_WHOLE_ARCHIVE_LIBS_VALUE_GLOBAL` contribution. Upstream uses
	// this for PY{2,3}_PROTO to inject sibling CPP_PROTO archives via
	// `--whole-archive-libs <path>` in LD.
	WholeArchiveRefs  []NodeRef
	WholeArchivePaths []VFS
	// WholeArchiveCmdPaths are command-only whole-archive paths with no
	// corresponding graph node/dependency. Upstream builtin py-proto uses
	// `_WHOLE_ARCHIVE_LIBS_VALUE_GLOBAL` this way even when the CPP peer-self
	// is suppressed.
	WholeArchiveCmdPaths []VFS
	// AddInclGlobal is this module's own GLOBAL ADDINCL UNION the
	// transitive peer-GLOBAL ADDINCL across every PEERDIR. Consumers use
	// the set for (a) cmd_args -I emission (peer slot after module's own
	// ADDINCL) and (b) include-scanner resolution. SOURCE_ROOT-relative.
	AddInclGlobal []VFS
	// OwnAddInclGlobal is this module's OWN GLOBAL ADDINCL only, no
	// transitive peers. The consumer walker composes peerAddInclGlobal in
	// two phases (own-first across all peers, transitive second) so that
	// libcxx/include + libcxxrt/include precede musl-arch (the latter
	// propagates transitively through libcxx's auto-PEERDIR of musl/include).
	OwnAddInclGlobal []VFS
	// CFlagsGlobal / CXXFlagsGlobal / COnlyFlagsGlobal: own GLOBAL UNION
	// transitive peer-GLOBAL on each axis. Consumers receive via
	// ModuleCCInputs.Peer*FlagsGlobal. Declaration-order preserved across
	// PEERDIR walk; duplicates dropped (mirror of AddInclGlobal).
	CFlagsGlobal     []string
	CXXFlagsGlobal   []string
	COnlyFlagsGlobal []string
	ObjAddLibsGlobal []string
	// LDFlagsGlobal: peer-aggregated `LDFLAGS()` from every PEERDIR'd
	// ya.make plus the module's own. Mirrors upstream `LDFLAGS_GLOBAL`
	// (`_EXE_FLAGS` slot in ld.conf:168). musl/ya.make contributes
	// `-static -Wl,--no-dynamic-linker` via this channel.
	LDFlagsGlobal []string
	// PeerArchiveClosureRefs / PeerArchiveClosurePaths: transitive archive
	// closure exposed to consumers — every peer's own AR UNION every
	// peer's PeerArchiveClosure*, deduplicated in DFS post-order (first
	// occurrence wins). Flows through LIBRARY moduleEmitResult so any
	// consumer (LIBRARY or PROGRAM) can union peers' closures with peers'
	// own archives for the full link-time archive set. Header-only
	// LIBRARYs propagate closures but contribute no archive themselves.
	PeerArchiveClosureRefs  []NodeRef
	PeerArchiveClosurePaths []VFS
	// isPyLibrary marks the module type as a Python variant (PY3_LIBRARY,
	// PY23_LIBRARY, PY2_LIBRARY, PY23_NATIVE_LIBRARY, PY2_PROGRAM,
	// PY3_PROGRAM). The umbrella branch of newPostEmitPrepare reads it to
	// suppress umbrella ADDINCL propagation into Python sub-libraries
	// under a propagating ancestor.
	isPyLibrary bool
	// PeerGlobalClosureRefs / PeerGlobalClosurePaths: transitive set of
	// `.global.a` archives reachable through this module's PEERDIR closure
	// (DFS post-order, dedup by path). REF wires transitively-reachable
	// `.global.a` archives into the consumer PROGRAM's LD `inputs` slot.
	PeerGlobalClosureRefs  []NodeRef
	PeerGlobalClosurePaths []VFS
	// PeerWholeArchiveClosureRefs / Paths: transitive
	// `_WHOLE_ARCHIVE_LIBS_VALUE_GLOBAL` closure reachable through this
	// module's PEERDIR walk (dedup by path, first occurrence wins).
	PeerWholeArchiveClosureRefs     []NodeRef
	PeerWholeArchiveClosurePaths    []VFS
	PeerWholeArchiveCmdClosurePaths []VFS
	// LDPluginRefs / LDPluginPaths: transitive set of LD plugin CP nodes a
	// consumer PROGRAM wires into its `--start-plugins ... --end-plugins`
	// block (e.g. `contrib/libs/musl/include`'s `LD_PLUGIN(musl.py)`).
	// Aggregation mirrors archive closure (peer's own ∪ PeerLDPluginPaths,
	// dedup by path, first-occurrence wins). Header-only LIBRARYs emit
	// their own CP node AND propagate it; non-PROGRAM consumers carry
	// through but never consume.
	LDPluginRefs  []NodeRef
	LDPluginPaths []VFS
	// PeerDynamicClosureRefs / Paths: transitive shared-library outputs
	// reachable through this module's PEERDIR closure. PROGRAM LDs link
	// against them and copy them into their output dir.
	PeerDynamicClosureRefs  []NodeRef
	PeerDynamicClosurePaths []VFS
	// InducedDeps is the module-level INDUCED_DEPS(<ext-filter> headers...)
	// declaration list (repo-relative). emitRunProgram, when walking a
	// tool PROGRAM via genModule, reads this to seed the PR output's
	// EmitsIncludes so the include scanner reaches the tool-injected
	// header closure (e.g. struct2fieldcalc's
	// `library/cpp/fieldcalc/field_calc_int.h` chain into autoarray.h).
	InducedDeps []string
	// Peerdirs is the post-collect effective peer list (user-declared
	// PEERDIRs after auto-injection of contrib/libs/python for
	// PY*_LIBRARY families, etc.). The back-peer branch of
	// newPostEmitPrepare reads this: when module P PEERDIRs module M, M
	// is a back-peer-child of P and inherits P's AddInclGlobal closure
	// (filtered to non-language-default). SOURCE_ROOT-relative.
	Peerdirs []string
	// ModuleStmtName is the module declaration name (PY23_LIBRARY,
	// LIBRARY, PROGRAM, ...). The back-peer branch of newPostEmitPrepare
	// gates the back-peer propagation on the parent declaring a
	// PY*_LIBRARY family that auto-PEERDIRs contrib/libs/python.
	ModuleStmtName string
}

func stringPtr(s string) *string {
	return &s
}

func vfsPtr(v VFS) *VFS {
	return &v
}

func cloneVFSs(in []VFS) []VFS {
	return append([]VFS(nil), in...)
}

func protoResultWholeArchiveCmdPaths(res *protoSrcsResult) []VFS {
	if res == nil {
		return nil
	}

	return cloneVFSs(res.WholeArchiveCmdPaths)
}

const (
	py3ProgramVariantBin    = "py3_bin"
	py3ProgramVariantBinLib = "py3_bin_lib"
)

// genCtx threads state through the recursive walk. `emit` accumulates
// emitted nodes; `memo` deduplicates per-instance emission; `walking` is
// the cycle-detection stack. Host-tool walks fire eagerly from inside
// `emitOneSource`; `genModule`'s memo prevents re-walking the same host
// instance.
type genCtx struct {
	sourceRoot      string
	fs              *FS
	parsers         *includeParserManager
	emit            Emitter
	memo            map[ModuleInstance]*moduleEmitResult
	moduleTypeCache map[moduleTypeCacheKey]moduleTypeInfo
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
	// enOutputs maps an emitted EN node's $(B)-rooted output path to its
	// NodeRef. Cross-EN header-inclusion deps look up previously emitted
	// EN nodes whose outputs are included by the current header's
	// transitive closure. EN nodes only emit on the target axis, so a
	// flat path-keyed map collapses cleanly; PB/EV use codegenOutputKey
	// because both axes can emit their own producer NodeRefs.
	enOutputs map[VFS]NodeRef
	// pbOutputs/evOutputs map (platform, $(B)-rooted output path) →
	// emitted PB/EV NodeRef. Per-platform because PB/EV emit on BOTH
	// target and host axes (host emission carries tags=["tool"]); CC
	// consumers must dep on the producer sharing their own platform.
	// Consulted by resolveCodegenDepRefs() to thread the producer NodeRef
	// into a consumer CC's ExtraDepRefs when its IncludeInputs carries
	// the BUILD_ROOT path of a generated .pb.h / .pb.cc / .ev.pb.h / .ev.pb.cc.
	pbOutputs map[codegenOutputKey]NodeRef
	evOutputs map[codegenOutputKey]NodeRef
	// pyRegisterOutputs caches PY_REGISTER producer nodes by their
	// platform-independent generated source path. Upstream emits the
	// gen_py3_reg.py PY node on the target axis and reuses that producer
	// as the generator dep for both target and host reg3.cpp compiles.
	pyRegisterOutputs map[VFS]NodeRef
	// ldPluginCPCache deduplicates LD_PLUGIN CP NodeRefs across the
	// target/host walk pair. Without dedup, `contrib/libs/musl/include`'s
	// `musl.py` would yield two CP nodes (one per platform). REF emits
	// the CP node ONCE on the target platform and reuses its UID in both
	// target and host LDs. Keying by plugin output path
	// (`$(B)/<modulePath>/<name>.pyplugin`) suffices: path is
	// platform-independent. First-write wins — the target walk precedes
	// any host walk recursion, so the cached entry carries the target
	// platform per REF.
	ldPluginCPCache map[VFS]NodeRef
	// scanCtx (per-ctxHash resolve/subgraph cache) lifecycle policy:
	//
	//   - "local"    — one scanCtx per (genModule, scanner, ctxHash);
	//                  pushed at genModule entry, popped at exit. No
	//                  cross-module reuse.
	//   - "interned" — one scanCtx per (scanner, ctxHash) for the whole
	//                  Gen call; cross-module reuse when ctxHash matches.
	//
	// Plumbed from CLI `--scan-ctx-mode`. Default = "interned".
	scanCtxMode       string
	localScanCtxStack []map[scanCtxCacheKey]*scanCtx
	internedScanCtx   map[scanCtxCacheKey]*scanCtx
	// Debug counters (printed when YATOOL_PERF_STATS=1). scanCtxAllocs
	// counts every fresh allocation; scanCtxPeak is the max bucket size
	// observed at any store. Local-mode peak = deepest in-flight bucket;
	// interned-mode peak = total scanCtx count (bucket never shrinks).
	scanCtxAllocs int
	scanCtxPeak   int

	// Canonical (host, target) Platform pair constructed once in
	// GenWithMode from CLI args. Threaded through every emitter so
	// renderers read off the Platform pointer directly (see platform.go).
	// Tool sub-graph emission flips the recursive call to (host, host) so
	// rendered nodes carry `node.platform = host.Target`,
	// `node.host_platform = true`, `node.tags = host.Tags` (`["tool"]`)
	// without renderer branches.
	host   *Platform
	target *Platform
}

// scanCtxCacheKey identifies a scanCtx by (scanner pointer, ctxHash).
// Pointer identity separates target vs host scanners; ctxHash separates
// module-config equivalence classes within one scanner.
type scanCtxCacheKey struct {
	scanner *IncludeScanner
	ctxHash uint64
}

// codegenOutputKey identifies a codegen producer's output on a specific
// Platform instance. PB/EV emit on both target and host; when host and
// target share the same PlatformID (x86_64 native target), the Platform
// pointer still distinguishes the two configured axes.
type codegenOutputKey struct {
	platform *Platform
	path     VFS
}

type scanCtxPerfStats struct {
	activeScanCtx     int
	resolveEntries    int
	searchTierEntries int
	subgraphEntries   int
	taintedKnown      int
	inProgress        int
	walkClosureCache  int
}

// resolveCodegenDepRefs scans `includeInputs` for $(B)-rooted paths that
// match a previously emitted codegen producer's output, returning the
// producer NodeRefs deduped in scan order. Each consumer CC carries
// these as ExtraDepRefs. `exclude` suppresses NodeRefs from the result
// (typically the downstream CC's `Generator` ref already in DepRefs).
// Consults enOutputs/pbOutputs/evOutputs and the per-scanner
// CodegenRegistry.
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
		} else if r, found := ctx.pbOutputs[codegenOutputKey{platform: consumer.Platform, path: v}]; found {
			ref, ok = r, true
		} else if r, found := ctx.evOutputs[codegenOutputKey{platform: consumer.Platform, path: v}]; found {
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

// getScanCtx returns a `*scanCtx` for (scanner, cfg). Dispatch:
//
//   - "local":    the per-genModule cache (top of localScanCtxStack).
//     Pop drops every scanCtx allocated under that frame.
//   - "interned": the genCtx-wide internedScanCtx map. The scanCtx
//     persists across modules and accumulates cache entries reusable by
//     any later matching ctxHash.
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
func (ctx *genCtx) pushLocalScanCtx() {
	if ctx.scanCtxMode != "local" {
		return
	}

	ctx.localScanCtxStack = append(ctx.localScanCtxStack, make(map[scanCtxCacheKey]*scanCtx, 4))
}

// popLocalScanCtx pops the top entry from the stack. No-op in "interned"
// mode.
func (ctx *genCtx) popLocalScanCtx() {
	if ctx.scanCtxMode != "local" {
		return
	}

	if len(ctx.localScanCtxStack) == 0 {
		ThrowFmt("genCtx.popLocalScanCtx: stack underflow")
	}

	ctx.localScanCtxStack = ctx.localScanCtxStack[:len(ctx.localScanCtxStack)-1]
}

func (ctx *genCtx) perfScanCtxStats(scanner *IncludeScanner) scanCtxPerfStats {
	stats := scanCtxPerfStats{}
	seen := make(map[*scanCtx]struct{})

	addBucket := func(bucket map[scanCtxCacheKey]*scanCtx) {
		for key, sc := range bucket {
			if key.scanner != scanner {
				continue
			}

			if _, ok := seen[sc]; ok {
				continue
			}

			seen[sc] = struct{}{}
			stats.activeScanCtx++
			stats.resolveEntries += len(sc.resolveCache)
			stats.searchTierEntries += len(sc.searchTierCache)
			stats.subgraphEntries += len(sc.subgraphCache)
			stats.taintedKnown += len(sc.subgraphTaintedKnown)
			stats.inProgress += len(sc.subgraphInProgress)
		}
	}

	addBucket(ctx.internedScanCtx)
	for _, bucket := range ctx.localScanCtxStack {
		addBucket(bucket)
	}

	stats.walkClosureCache = len(scanner.walkClosureCache)

	return stats
}

func reportPerfStats(ctx *genCtx, parsers *includeParserManager, targetScanner, hostScanner *IncludeScanner) {
	if !perfStatsEnabled {
		return
	}

	parserStats := parsers.perfStats()
	fsStats := ctx.fs.perfStats()
	fmt.Fprintf(os.Stderr, "perf: gen mode=%s scanCtxAllocs=%d scanCtxPeak=%d internedScanCtx=%d localBuckets=%d\n",
		ctx.scanCtxMode, ctx.scanCtxAllocs, ctx.scanCtxPeak, len(ctx.internedScanCtx), len(ctx.localScanCtxStack))
	fmt.Fprintf(os.Stderr, "perf: parser parsedHits=%d parsedMisses=%d buildParsed=%d\n",
		parserStats.parsedHits, parserStats.parsedMisses, parserStats.buildParsed)
	fmt.Fprintf(os.Stderr, "perf: fs listdirHits=%d listdirMisses=%d existsHits=%d existsMisses=%d dirsCached=%d\n",
		fsStats.listdirHits, fsStats.listdirMisses, fsStats.existsHits, fsStats.existsMisses, fsStats.dirsCached)

	reportScanner := func(label string, scanner *IncludeScanner) {
		scanStats := scanner.perfStats()
		ctxStats := ctx.perfScanCtxStats(scanner)
		fmt.Fprintf(os.Stderr, "perf: scanner %s activeScanCtx=%d walkClosureCache=%d resolveEntries=%d searchTierEntries=%d subgraphEntries=%d taintedKnown=%d inProgress=%d walkClosure=%d dfs=%d plainDfs=%d subgraphHits=%d subgraphMisses=%d tainted=%d searchTierHits=%d searchTierMisses=%d resolveCalls=%d resolveHits=%d resolveMisses=%d sysinclSourceHits=%d sysinclSourceMisses=%d sysinclIncluderHits=%d sysinclIncluderMisses=%d\n",
			label,
			ctxStats.activeScanCtx,
			ctxStats.walkClosureCache,
			ctxStats.resolveEntries,
			ctxStats.searchTierEntries,
			ctxStats.subgraphEntries,
			ctxStats.taintedKnown,
			ctxStats.inProgress,
			scanStats.walkClosureCalls,
			scanStats.dfsCalls,
			scanStats.plainDfsCalls,
			scanStats.subgraphHits,
			scanStats.subgraphMisses,
			scanStats.subgraphTainted,
			scanStats.searchTierHits,
			scanStats.searchTierMisses,
			scanStats.resolveSearchPathCalls,
			scanStats.resolveCacheHits,
			scanStats.resolveCacheMisses,
			scanStats.sysinclSourceHits,
			scanStats.sysinclSourceMisses,
			scanStats.sysinclIncluderHits,
			scanStats.sysinclIncluderMisses,
		)
	}

	reportScanner("target", targetScanner)
	reportScanner("host", hostScanner)
}

// asmlibYasmModules lists module paths whose host `.S`/`.s` sources
// invoke yasm via `foreign_deps.tool`. REF wires yasm into 25 host-asmlib
// AS nodes; no other host AS source reaches yasm
// (`cxxsupp/builtins/chkstk_aarch64.S` and libcxx/libcxxabi shims use
// clang's built-in assembler with no foreign_deps).
var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

// whitelistedMetadataMacros lists UnknownStmt names the walker treats as
// no-ops (metadata only — no node emission). "Real" effects (NO_LIBC
// etc.) are handled directly in `collectModule` and override the
// inferred-from-path FlagSet. Pure-metadata names (LICENSE, VERSION,
// ALLOCATOR_IMPL, ...) stay as no-ops. New entries OK if confirmed
// metadata-only.
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
	"DISABLE":               {}, // ENABLE is handled explicitly (to track MUSL_LITE); DISABLE has no per-module side effect.
	"NO_BUILD_IF":           {},
	"NO_SANITIZE":           {},
	"NO_SANITIZE_COVERAGE":  {},
	"DEFAULT":               {},
	"PROVIDES":              {},
	"USE_CXX":               {},
	"DEFINE_VARIABLE":       {},
	"PYTHON3":               {},
	"BUILD_ONLY_IF":         {}, // contrib/libs/cxxsupp/libcxxrt
	"MESSAGE":               {}, // contrib/libs/cxxsupp/libcxx (FATAL_ERROR in dead branch)
	// SRC_C_SSE41 / SSE2 / SSSE3 / AVX / XOP / SSE3 / SSE4: handled in
	// applyUnknownStmt → d.simdSrcs (one CC node per variant). Not in
	// the whitelist.
	"NO_CLANG_COVERAGE":  {}, // contrib/tools/yasm
	"NO_PROFILE_RUNTIME": {}, // contrib/tools/yasm
	"WITHOUT_VERSION":    {}, // contrib/libs/musl/include neighbours; metadata-only.

	// USE_PYTHON3 / NO_PYTHON_INCLUDES / NO_CHECK_IMPORTS / PYBUILD_NO_PYC /
	// RESOURCE / RESOURCE_FILES / PY_REGISTER / RUN_PROGRAM /
	// RUN_ANTLR4_CPP[_SPLIT] / GENERATE_ENUM_SERIALIZATION[_WITH_HEADER|
	// _NOUTF] / ARCHIVE / CREATE_BUILDINFO_FOR are NOT in this whitelist
	// — they have typed handlers in applyUnknownStmt / yamake.go.
	"USE_PYTHON2":                    {}, // Python 2 dependency marker.
	"PYTHON3_ADDINCL":                {}, // Adds Python3 include paths (system python, handled by emitter).
	"PYTHON2_ADDINCL":                {}, // Adds Python2 include paths.
	"NO_PYTHON_COVERAGE":             {}, // Suppresses Python coverage instrumentation.
	"NO_IMPORT_TRACING":              {}, // Suppresses import tracing.
	"NO_LINT":                        {}, // Suppresses linting.
	"STYLE_PYTHON":                   {}, // Python style checker metadata.
	"WINDOWS_LONG_PATH_MANIFEST":     {}, // Windows-only manifest; no-op on Linux.
	"INCLUDE_TAGS":                   {}, // Proto include-tag filter; metadata.
	"INDUCED_DEPS":                   {}, // Adds header deps without PEERDIR; metadata.
	"NO_PYTHON2":                     {}, // Marks PY2 unavailability; metadata.
	"CHECK_DEPENDENT_DIRS":           {}, // Dependency restriction check; metadata.
	"SUBSCRIBER":                     {}, // Ownership metadata.
	"OWNER":                          {}, // Ownership metadata.
	"LICENSE_RESTRICTION_EXCEPTIONS": {}, // License metadata.
	"LICENSE_RESTRICTION":            {}, // License metadata.
	"RESTRICT_PATH":                  {}, // Path-restriction metadata.
	"NO_OPTIMIZE":                    {}, // Suppresses optimization; metadata.
	"TASKLET":                        {}, // Tasklet metadata; deferred.
	"TASKLETSUPPORT":                 {}, // Tasklet support metadata; deferred.
	// SET_APPEND is handled by applyUnknownStmt for the SFLAGS axis;
	// other SET_APPEND targets remain as no-ops (handled there).
	"OPENSOURCE_PROJECT": {}, // Metadata.
	"SPLIT_FACTOR":       {}, // Test metadata.
	"FORK_TESTS":         {}, // Test metadata.
	"FORK_SUBTESTS":      {}, // Test metadata.
	"SIZE":               {}, // Test size metadata.
	"TAG":                {}, // Test tag metadata.
	"REQUIREMENTS":       {}, // Test requirements metadata.
	"TIMEOUT":            {}, // Test timeout metadata.
	"ENV":                {}, // Test env metadata.
	"DATA":               {}, // Test data metadata.
	"TEST_SRCS":          {}, // Test source list.
	"LINT":               {}, // Lint metadata.
	"NO_YMAKE_PYTHON":    {}, // Suppresses ymake python binding; metadata.
	"USE_LIGHT_PY2CC":    {}, // Python build variant; metadata.

	"SUPPRESSIONS":                    {}, // Sanitizer suppression file; metadata.
	"OPENSOURCE_EXPORT_REPLACEMENT":   {}, // CMake/Conan export replacement; metadata.
	"EXCLUDE_TAGS":                    {}, // Build-system tag exclusion; metadata.
	"FILES":                           {}, // Proto library file listing; metadata.
	"NO_JOIN_SRC":                     {}, // Suppresses JOIN_SRCS optimisation; metadata.
	"MASMFLAGS":                       {}, // MASM compiler flags (Windows); no-op on Linux.
	"NO_MYPY":                         {}, // Suppresses mypy type checking; metadata.
	"NO_OPTIMIZE_PY_PROTOS":           {}, // Suppresses proto Python optimisation; metadata.
	"PROTO_NAMESPACE":                 {}, // Proto namespace declaration; metadata.
	"PY_NAMESPACE":                    {}, // Python namespace declaration; metadata.
	"GRPC":                            {}, // gRPC service declaration; deferred.
	"CPP_PROTO_PLUGIN":                {}, // protoc C++ plugin; deferred.
	"CPP_PROTO_PLUGIN2":               {}, // protoc C++ plugin variant; deferred.
	"CPP_EV_PLUGIN":                   {}, // event compiler plugin; deferred.
	"JAVA_SRCS":                       {}, // Java sources; deferred.
	"JAVA_CLASSPATH_IGNORE_CONFLICTZ": {}, // Java classpath; metadata.
}

// defaultScanCtxMode is the per-Gen scanCtx lifecycle policy used when
// no explicit mode is passed (tests, Gen wrapper). "interned" was
// selected for ~6% wall-time reduction over "local".
const defaultScanCtxMode = "interned"

// runGenInto runs the Gen walk against the supplied emitter without
// calling Finalize on it. Returns the root NodeRef. `onWarn` receives
// one line per diagnostic surfaced during loading (sysincl
// source_filter records the runtime cannot model, …); callers route
// it to stderr under `--verbose` and to a no-op otherwise.
func runGenInto(srcRoot, targetDir string, hostP, targetP *Platform, emitter Emitter, mode string, onWarn func(Warn)) NodeRef {
	return runGenIntoWithResources(srcRoot, targetDir, hostP, targetP, emitter, mode, onWarn, nil)
}

func runGenIntoWithResources(srcRoot, targetDir string, hostP, targetP *Platform, emitter Emitter, mode string, onWarn func(Warn), resources *resourceFetchPlan) NodeRef {
	if mode != "local" && mode != "interned" {
		ThrowFmt("gen: --scan-ctx-mode must be \"local\" or \"interned\", got %q", mode)
	}

	resources.emitAll(hostP, emitter)
	emitter = newResourceAwareEmitter(emitter, resources)

	fs := NewFS(srcRoot)
	parsers := newIncludeParserManagerFS(fs, newSharedParseCache())

	targetReg := NewCodegenRegistry()
	hostReg := NewCodegenRegistry()

	targetScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(targetP.ISA), onWarn), onWarn)
	targetScanner.codegen = targetReg
	targetScanner.fallbackLocators = []pathLocator{codegenLocator{reg: targetReg}}
	hostScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(hostP.ISA), onWarn), onWarn)
	hostScanner.codegen = hostReg
	hostScanner.fallbackLocators = []pathLocator{codegenLocator{reg: hostReg}}

	ctx := &genCtx{
		sourceRoot:        srcRoot,
		fs:                fs,
		parsers:           parsers,
		emit:              emitter,
		memo:              make(map[ModuleInstance]*moduleEmitResult),
		moduleTypeCache:   make(map[moduleTypeCacheKey]moduleTypeInfo),
		walking:           make(map[ModuleInstance]bool),
		host:              hostP,
		target:            targetP,
		scannerTarget:     targetScanner,
		scannerHost:       hostScanner,
		enOutputs:         make(map[VFS]NodeRef),
		pbOutputs:         make(map[codegenOutputKey]NodeRef),
		evOutputs:         make(map[codegenOutputKey]NodeRef),
		pyRegisterOutputs: make(map[VFS]NodeRef),
		ldPluginCPCache:   make(map[VFS]NodeRef),
		scanCtxMode:       mode,
		internedScanCtx:   make(map[scanCtxCacheKey]*scanCtx, 64),
	}

	ctx.localScanCtxStack = []map[scanCtxCacheKey]*scanCtx{make(map[scanCtxCacheKey]*scanCtx, 4)}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: targetP,
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)
	reportPerfStats(ctx, parsers, targetScanner, hostScanner)

	return root.LDRef
}

// GenWithMode runs Gen against an explicit (host, target) Platform pair
// with the chosen scanCtxMode (`local` or `interned`). Callers (`yatool
// make -G`, test helpers) construct both Platforms from CLI flags +
// mining; the walker reads every flag, tool path, and tag off the
// Platform pointers. `onWarn` receives one line per diagnostic.
func GenWithMode(sourceRoot string, targetDir string, hostP, targetP *Platform, mode string, onWarn func(Warn)) *Graph {
	return GenWithModeWithResources(sourceRoot, targetDir, hostP, targetP, mode, onWarn, nil)
}

func GenWithModeWithResources(sourceRoot string, targetDir string, hostP, targetP *Platform, mode string, onWarn func(Warn), resources *resourceFetchPlan) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(sourceRoot, targetDir, hostP, targetP, emitter, mode, onWarn, resources)

	return Finalize(emitter)
}

// moduleData is the per-module accumulator populated by `collectModule`,
// capturing everything the rule-emission stage needs after macro
// evaluation has flattened IF branches. The `flags` field starts from
// the path-based heuristic and is overlaid with macro-derived bools
// (NO_LIBC etc.).

// genModule emits the subgraph for `instance` and returns its
// `*moduleEmitResult`. Memoised: cycles on the walking stack tolerate
// and return a zero-result stub. After parse + collectModule, recurses
// into each PEERDIR in declaration order and dispatches per source by
// extension; LIBRARY ends with EmitAR over own CCs, PROGRAM with EmitLD
// over own CCs and peer archives.
func genModule(ctx *genCtx, instance ModuleInstance) *moduleEmitResult {
	if existing, ok := ctx.memo[instance]; ok {
		return existing
	}

	// In "local" mode, push a fresh scanCtx cache map for this module.
	// Every `walkClosure` / `joinSrcsIncludeClosure` call in this frame
	// goes through `getScanCtx`, which addresses the top of the stack;
	// on pop, scanCtxes allocated under this frame become unreachable.
	// In "interned" mode the pair is a no-op.
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

	// A back-edge during the walk is tolerated, not a hard error. The
	// implicit DEFAULT_PEERDIR set creates legitimate mutual references
	// between runtime-stack modules (libcxx ↔ libcxxrt, libunwind ↔
	// libcxxrt via sanitizer/include's ancestor chain, etc.) which
	// upstream handles via exclusion lists not yet modelled here.
	// Returning a zero-result stub suffices: the peer's own walk
	// completes elsewhere on the stack, and consumers skip the empty
	// archive-ref instead of pinning a zero NodeRef. REF emits no
	// peer-archive deps in AR anyway (every LIBRARY's AR has only its
	// own .o files), so the loss is below the L1 comparator surface.
	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path)

		return &moduleEmitResult{}
	}

	ctx.walking[instance] = true
	defer delete(ctx.walking, instance)

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path, "ya.make")
	mf := Throw2(ParseFile(ctx.fs, yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(ctx.fs, instance.Path, instance.Kind, mf.Stmts, env)
	for _, stmt := range d.allPySrcs {
		applyAllPySrcs(ctx.fs, instance.Path, stmt, d)
	}

	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

	// PY_PROTO / PY3_PROTO branches of PROTO_LIBRARY implicitly depend on
	// the Python protobuf runtime; GRPC() extends that set with grpcio.
	// builtin_proto is the runtime itself, so pybuild.py suppresses the
	// self-peer there.
	if instance.Language == LangPy && d.moduleStmt.Name == "PROTO_LIBRARY" {
		hasProtoSrc := false
		for _, src := range d.srcs {
			if strings.HasSuffix(src, ".proto") {
				hasProtoSrc = true
				break
			}
		}
		if hasProtoSrc && !strings.HasPrefix(instance.Path, "contrib/libs/protobuf/builtin_proto") &&
			!strings.HasPrefix(instance.Path, "contrib/python/protobuf") {
			d.peerdirs = append(d.peerdirs, "contrib/python/protobuf")
		}
		if hasProtoSrc && d.grpc {
			d.peerdirs = append(d.peerdirs, "contrib/python/grpcio")
		}
	}

	if d.moduleStmt.Name != "LIBRARY" && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isMultimoduleLibraryType(d.moduleStmt.Name) {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path, d.moduleStmt.Name)
	}

	// Upstream ymake.core.conf has `when ($MUSL_LITE == "yes") { NO_UTIL() }`.
	// Apply the implication: MUSL_LITE=yes → NoUtil=true. Prevents yasm
	// (ENABLE(MUSL_LITE)) from receiving util as a default peer.
	if d.muslLite {
		d.flags.NoUtil = true
	}

	// _BASE_PY3_PROGRAM (build/conf/python.conf:877-883) applies an
	// implicit `ALLOCATOR($_MY_ALLOCATOR)`; the otherwise-branch
	// (non-ARCH_PPC64LE) sets _MY_ALLOCATOR=J. Linux-x86_64/aarch64 takes
	// this branch, so PY3_PROGRAM modules without explicit ALLOCATOR
	// inherit jemalloc rather than the plain-PROGRAM TCMALLOC_TC default.
	if !d.hadAllocator && (d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN") {
		d.hadAllocator = true
		d.allocatorName = "J"
	}

	// PY{2,3,23}_LIBRARY and PY{3}_PROGRAM_BIN bodies upstream:
	// `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }`
	// (python.conf:697-699 PY2_LIBRARY, :741-743 PY3_LIBRARY, :887-889
	// _BASE_PY3_PROGRAM; PY23_LIBRARY inherits via PY2/PY3 submodules).
	// PY23_NATIVE_LIBRARY is intentionally excluded: its PY2/PY3
	// submodules inherit from plain `LIBRARY` (python.conf:1238-1259), so
	// upstream does NOT auto-PEERDIR contrib/libs/python and adding it
	// would create a cycle (library/python/symbols/python → contrib/libs/
	// python → library/python/symbols/python).
	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path != "contrib/libs/python" {
		// Upstream `_BASE_PY3_LIBRARY` (and siblings) emits the implicit
		// PEERDIR(contrib/libs/python) FROM the module-decl body BEFORE
		// user-declared PEERDIRs. Prepending preserves that visit order;
		// REF ymakeyaml.cpp.o ref:21 confirms `python/Include` (contrib/
		// libs/python OWN GLOBAL) ahead of `re2/include` (user-peer
		// transitive).
		d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
	}

	if d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		if d.py3ProgramMultimodule && instance.Kind == KindBin && instance.Path != "" {
			d.peerdirs = append([]string{instance.Path}, d.peerdirs...)
		}
		// `_BASE_PY3_PROGRAM` adds runtime_py3/main unconditionally,
		// `_sqlite` only when PYTHON_SQLITE3 != "no", and import_test
		// only when ADD_CHECK_PY_IMPORTS remains enabled.
		if d.pythonSQLite3 {
			d.peerdirs = append(d.peerdirs, "contrib/tools/python3/Modules/_sqlite")
		}
		implicitPeers := []string{"library/python/runtime_py3/main"}
		if !d.noImportTracing && instance.Path != "library/python/import_tracing/constructor" {
			implicitPeers = append(implicitPeers, "library/python/import_tracing/constructor")
		}
		if !d.noCheckImportsDisabled {
			implicitPeers = append(implicitPeers, "library/python/testing/import_test")
		}
		for _, peer := range implicitPeers {
			if instance.Path != peer {
				d.peerdirs = append(d.peerdirs, peer)
			}
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) && pyLibraryAutoPythonPeer(d.moduleStmt.Name) && d.moduleStmt.Name != "PY3_PROGRAM" && d.moduleStmt.Name != "PY3_PROGRAM_BIN" && !d.noImportTracing && instance.Path != "library/python/import_tracing/constructor" {
		d.peerdirs = append(d.peerdirs, "library/python/import_tracing/constructor")
	}

	// GENERATE_ENUM_SERIALIZATION* injects an implicit PEERDIR to
	// tools/enum_parser/enum_serialization_runtime (upstream
	// `_GENERATE_ENUM_SERIALIZATION_BASE` in build/ymake.core.conf). The
	// runtime carries dispatch_methods.cpp / enum_runtime.cpp /
	// ordered_pairs.cpp that the generated _serialized.cpp links against.
	if len(d.enumSrcs) > 0 && instance.Path != "tools/enum_parser/enum_serialization_runtime" {
		d.peerdirs = append(d.peerdirs, "tools/enum_parser/enum_serialization_runtime")
	}

	// Multimodule library types (PROTO_LIBRARY, DLL, ...) still need a
	// dedicated path: they do not flow through the regular SRCS/AR
	// pipeline, but emit their own specialised artefacts.
	if isMultimoduleLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == "DYNAMIC_LIBRARY" {
			result := emitDynamicLibrary(ctx, instance, d)
			ctx.memo[instance] = result

			return result
		}

		// Multimodule modules may declare ADDINCL(GLOBAL ...) /
		// {C,CXX,CONLY}FLAGS(GLOBAL ...) that peer-propagate without
		// emitting an AR. Walk peers (so their transitive closures reach
		// genModule) and aggregate own + peer GLOBAL per axis.
		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)

		// Emit own LD_PLUGIN CP nodes (e.g. musl.py → musl.py.pyplugin)
		// BEFORE composing the result so the refs propagate alongside the
		// peer-walked plugin closure. The CP node carries
		// `module_dir = instance.Path` per REF; src/dst anchor under it.
		ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
		ldPlugins := mergeLDPlugins(ownLDPlugins, &ldPluginsResult{
			Refs:  peerContribs.ldPluginRefs,
			Paths: peerContribs.ldPluginPaths,
		})
		if ldPlugins == nil {
			ldPlugins = &ldPluginsResult{}
		}

		// Multimodule modules may still declare RUN_PROGRAM whose OUT is
		// later consumed by RESOURCE/RESOURCE_FILES (libmagic/magic:
		// Magdir.mgc). Emit PR nodes early so d.prOutputProducer is
		// populated before emitResourceObjcopy resolves resource paths.
		// Any downstream CCs returned here are intentionally ignored:
		// this branch models modules with no compilable own sources.
		headerOnlyInputs := ModuleCCInputs{
			Flags:             d.flags,
			AddIncl:           mergeDedupVFS(d.addIncl, nil),
			PeerAddInclGlobal: peerContribs.addIncl,
			SrcDir:            d.srcDir,
			SourceRoot:        ctx.sourceRoot,
			FS:                ctx.fs,
			DefaultVars:       d.defaultVars,
			DefaultVarOrder:   d.defaultVarOrder,
		}
		_ = emitRunProgramsForAR(ctx, instance, d, headerOnlyInputs)

		// Emit yapyc3 PY nodes for PY_SRCS() declarations. PY3_LIBRARY /
		// PY23_LIBRARY often have only PY_SRCS (no C/C++ sources) and
		// reach this branch; their Python sources still need PY emission.
		emitPySrcs(ctx, instance, d)

		// Emit objcopy PY nodes for RESOURCE / RESOURCE_FILES. Multimodule
		// LIBRARYs (e.g. certs, PY3_LIBRARY-only-PY_SRCS) host the
		// only-resource shape; when there are objcopy outputs, emit a
		// .global.a archiving them.
		objcopyRes := emitResourceObjcopy(ctx, instance, d)

		// Capture the `.global.a` ref so consumers see it via
		// `moduleEmitResult.GlobalRef/GlobalPath`. Otherwise RESOURCE-only
		// LIBRARY (`certs`) and PY3_LIBRARY PY_SRCS modules' `.global.a`
		// archives reach the graph but are orphaned from every LD inputs.
		var hOnlyGlobalRef *NodeRef
		var hOnlyGlobalPath *VFS
		var hOnlyWholeArchiveRefs []NodeRef
		var hOnlyWholeArchivePaths []VFS

		if objcopyRes != nil && len(objcopyRes.Refs) > 0 {
			// REF surfaces module_lang="py3" for PY*_LIBRARY archives
			// (module_tag=global, or py3_global on PY23_LIBRARY) and
			// "cpp" for plain LIBRARY (module_tag=global) and for
			// PY23_NATIVE_LIBRARY (module_tag=py3_native_global). Pivot
			// Language locally for the AR emit; peer-walk Language stays
			// LangCPP.
			arInstance := instance
			var globalBaseName, tag string
			archiveName := ""
			if len(d.moduleStmt.Args) > 0 {
				archiveName = d.moduleStmt.Args[0]
			}
			switch d.moduleStmt.Name {
			case "PY23_NATIVE_LIBRARY":
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path, "libpy3c", archiveName)
				tag = "py3_native_global"
			case "PY23_LIBRARY":
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path, "libpy3", archiveName)
				tag = "py3_global"
			case "PY3_LIBRARY", "PY2_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path, "libpy3", archiveName)
				tag = "global"
			default:
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path, "lib", archiveName)
				tag = "global"
			}
			gRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRes.Refs, objcopyRes.Outputs, objcopyRes.GlobalMemberInputs, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = vfsPtr(Build(instance.Path + "/" + globalBaseName))
		}

		// Emit EN nodes for GENERATE_ENUM_SERIALIZATION(*). Multimodule
		// modules never compile the `_serialized.cpp` output (every
		// EN-emitting module has a regular AR archiving the output); pass
		// nil consumerInputs to suppress downstream-CC emission here.
		emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, nil)

		// Emit PB/EV nodes for PROTO_LIBRARY .proto/.ev sources.
		// PROTO_LIBRARY modules always reach the header-only branch.
		// Also emits downstream CC + AR scaffolding for true
		// PROTO_LIBRARY (skipped for other multimodule types).
		// peerContribs is threaded so downstream CCs see the same
		// peer-GLOBAL CFLAGS / ADDINCL the header-only walker aggregated.
		// Surfaces PROTO_LIBRARY's emitted `.a` for downstream LD walks
		// (otherwise the AR is orphaned from every LD inputs).
		protoResult := emitProtoSrcs(ctx, instance, d, peerContribs)

		// Emit JV, CF, BI, PR nodes declared at module level. Header-only
		// branch: no downstream CC/AR, so consumerInputs is nil.
		emitMiscNodes(ctx, instance, d, nil)

		// PROTO_LIBRARY emits a regular `.a` via `emitProtoSrcs` above;
		// surface it so downstream peer-archive closures pick it up. The
		// A non-empty ARPath lets consumers fold the AR into
		// `peerArchiveRefs` without reintroducing the AR-on-AR dependency
		// LIBRARY ARs drop.
		hOnlyARRef := NodeRef{}
		var hOnlyARPath *VFS
		if protoResult != nil {
			if protoResult.ARPath != nil {
				hOnlyARRef = protoResult.ARRef
				hOnlyARPath = protoResult.ARPath
			}
			if protoResult.GlobalRef != nil && protoResult.GlobalPath != nil {
				hOnlyGlobalRef = protoResult.GlobalRef
				hOnlyGlobalPath = protoResult.GlobalPath
			}
			hOnlyWholeArchiveRefs = append(hOnlyWholeArchiveRefs, protoResult.WholeArchiveRefs...)
			hOnlyWholeArchivePaths = append(hOnlyWholeArchivePaths, protoResult.WholeArchivePaths...)
		}

		peerArchivePathsH := peerContribs.archivePaths
		peerArchiveRefsH := peerContribs.archiveRefs
		peerGlobalPathsH := peerContribs.globalPaths
		peerGlobalRefsH := peerContribs.globalRefs

		result := &moduleEmitResult{
			isPyLibrary:      isPyLibraryType(d.moduleStmt.Name),
			ARRef:            hOnlyARRef,
			ARPath:           hOnlyARPath,
			GlobalRef:        hOnlyGlobalRef,
			GlobalPath:       hOnlyGlobalPath,
			AddInclGlobal:    mergeDedupVFS(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal: cloneVFSs(d.addInclGlobal),
			// peer-transitive first, own last per upstream
			// `TGlobalVarsCollector` semantics. ADDINCL keeps the opposite
			// (own first, peer second) per `TModuleIncDirs::Get()`.
			CFlagsGlobal:                    mergeDedup(peerContribs.cFlags, d.cFlagsGlobal),
			CXXFlagsGlobal:                  mergeDedup(peerContribs.cxxFlags, d.cxxFlagsGlobal),
			COnlyFlagsGlobal:                mergeDedup(peerContribs.cOnlyFlags, d.cOnlyFlagsGlobal),
			ObjAddLibsGlobal:                mergeDedup(peerContribs.objAddLibs, d.objAddLibsGlobal),
			LDFlagsGlobal:                   mergeDedup(peerContribs.ldFlags, d.ldFlags),
			PeerArchiveClosureRefs:          peerArchiveRefsH,
			PeerArchiveClosurePaths:         peerArchivePathsH,
			PeerGlobalClosureRefs:           peerGlobalRefsH,
			PeerGlobalClosurePaths:          peerGlobalPathsH,
			WholeArchiveRefs:                append([]NodeRef(nil), hOnlyWholeArchiveRefs...),
			WholeArchivePaths:               cloneVFSs(hOnlyWholeArchivePaths),
			WholeArchiveCmdPaths:            protoResultWholeArchiveCmdPaths(protoResult),
			PeerWholeArchiveClosureRefs:     append([]NodeRef(nil), peerContribs.wholeArchiveRefs...),
			PeerWholeArchiveClosurePaths:    cloneVFSs(peerContribs.wholeArchivePaths),
			PeerWholeArchiveCmdClosurePaths: cloneVFSs(peerContribs.wholeArchiveCmdPaths),
			LDPluginRefs:                    ldPlugins.Refs,
			LDPluginPaths:                   ldPlugins.Paths,
			PeerDynamicClosureRefs:          append([]NodeRef(nil), peerContribs.dynamicRefs...),
			PeerDynamicClosurePaths:         cloneVFSs(peerContribs.dynamicPaths),
			InducedDeps:                     append([]string(nil), d.inducedDeps...),
			Peerdirs:                        append([]string(nil), d.peerdirs...),
			ModuleStmtName:                  d.moduleStmt.Name,
		}
		ctx.memo[instance] = result

		return result
	}

	// Recurse into peers. Implicit DEFAULT_PEERDIRs are prepended to the
	// explicit `PEERDIR(...)` list so the closure includes the runtime /
	// libc / allocator scaffolding ymake adds via `_BUILTIN_PEERDIR`. R14
	// (declaration order) preserved for the explicit set — defaults
	// sort first, then explicit.
	//
	// Defaults are tolerant of a missing ya.make: synthetic test fixtures
	// populate only modules they care about, and a helper-supplied
	// default (musl / builtins / malloc/api) may not exist there. A
	// missing EXPLICIT peer is still a hard error (fixture bug).
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	// ALLOCATOR(FAKE) suppresses the implicit malloc/api auto-peer
	// (mirrors upstream `_BASE_UNIT`'s skip when ALLOCATOR=FAKE). yasm:
	// no allocator peer AND no malloc/api → yasm's LD drops one
	// peer-archive ref.
	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)

	// Program-defaults split into pre-user (cow/on + optional tcmalloc)
	// and post-user (musl/full or musl). Explicit ALLOCATOR peers and
	// d.peerdirs interleave between the halves so they appear before
	// musl/full in archive accumulation while retaining peerKindUserPeer
	// (correct AddInclGlobal Phase 3 ordering).
	languageDefaultsCount := len(defaults)

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path)

	var preUserProgDefaults []string
	var postUserProgDefaults []string
	if isProgram {
		preUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, false)
		postUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, true)
		defaults = append(defaults, preUserProgDefaults...)
	}

	// Peers declared by ALLOCATOR(NAME) (nil for FAKE/DEFAULT/SYSTEM, or
	// when no ALLOCATOR macro was used). Treated as peerKindUserPeer so
	// AddInclGlobal Phase 3 places their transitive includes ahead of
	// later user-PEERDIRs.
	allocatorExplicitPeers := allocatorPeers[d.allocatorName]

	seen := make(map[string]struct{}, len(defaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))
	allPeers := make([]string, 0, len(defaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	// Track per-peer category so peer-GLOBAL aggregation applies the
	// right ordering: language-defaults use two-phase (own first, then
	// transitive); user-peers and program-defaults use single-phase
	// AddInclGlobal in declaration order.
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
	//    their AddInclGlobal — mimalloc/include before ragel5/aapl).
	//    Placed BEFORE the musl post-user block so the allocator cluster
	//    (mimalloc → malloc/api + mimalloc AR) precedes musl/full's
	//    transitive deps (asmlib/asmglibc/musl) in the archive walk.
	//    Regular d.peerdirs stay in step 4 so they remain AFTER musl/full.
	for _, p := range allocatorExplicitPeers {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	// 3. Post-user program-defaults (musl/full or bare musl). Placed
	//    after allocator explicit peers but before regular user PEERDIRs
	//    so musl/full's transitive closure lands before user-peerdir
	//    libraries in the archive walk.
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
	peerArchivePaths := make([]VFS, 0, len(allPeers))
	peerGlobalRefs := make([]NodeRef, 0, len(allPeers))
	peerGlobalPaths := make([]VFS, 0, len(allPeers))
	peerWholeArchiveRefs := make([]NodeRef, 0, len(allPeers))
	peerWholeArchivePaths := make([]VFS, 0, len(allPeers))
	peerWholeArchiveCmdPaths := make([]VFS, 0, len(allPeers))
	peerDynamicRefs := make([]NodeRef, 0, len(allPeers))
	peerDynamicPaths := make([]VFS, 0, len(allPeers))

	// Dedup table for the transitive `.global.a` closure. Each direct
	// peer contributes its own `.global.a` (when GlobalRef != nil) AND
	// every entry of its PeerGlobalClosure*. First occurrence wins; the
	// closure flows up through `moduleEmitResult.PeerGlobalClosure*` so
	// PROGRAM LDs at any depth reach every transitively-reachable
	// `.global.a`.
	peerGlobalSeen := map[VFS]struct{}{}
	peerGlobalAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerGlobalSeen[path]; dup {
			return
		}

		peerGlobalSeen[path] = struct{}{}
		peerGlobalRefs = append(peerGlobalRefs, ref)
		peerGlobalPaths = append(peerGlobalPaths, path)
	}

	peerWholeArchiveSeen := map[VFS]struct{}{}
	peerWholeArchiveAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerWholeArchiveSeen[path]; dup {
			return
		}

		peerWholeArchiveSeen[path] = struct{}{}
		peerWholeArchiveRefs = append(peerWholeArchiveRefs, ref)
		peerWholeArchivePaths = append(peerWholeArchivePaths, path)
	}

	peerWholeArchiveCmdSeen := map[VFS]struct{}{}
	peerWholeArchiveAddCmdPath := func(path VFS) {
		if _, dup := peerWholeArchiveCmdSeen[path]; dup {
			return
		}

		peerWholeArchiveCmdSeen[path] = struct{}{}
		peerWholeArchiveCmdPaths = append(peerWholeArchiveCmdPaths, path)
	}

	peerDynamicSeen := map[VFS]struct{}{}
	peerDynamicAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerDynamicSeen[path]; dup {
			return
		}

		peerDynamicSeen[path] = struct{}{}
		peerDynamicRefs = append(peerDynamicRefs, ref)
		peerDynamicPaths = append(peerDynamicPaths, path)
	}

	// Dedup table for the transitive peer-archive closure. For each
	// direct peer, accumulate (peer's own AR ∪ peer's PeerArchiveClosure)
	// — first occurrence wins (R14). Consumed only by the PROGRAM branch
	// below (LIBRARYs drop peer-archive refs from their AR); LIBRARY
	// consumers downstream walk our exposed
	// `PeerArchiveClosureRefs/Paths` and fold using the same discipline.
	peerArchiveSeen := map[VFS]struct{}{}
	peerArchiveAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerArchiveSeen[path]; dup {
			return
		}

		peerArchiveSeen[path] = struct{}{}
		peerArchiveRefs = append(peerArchiveRefs, ref)
		peerArchivePaths = append(peerArchivePaths, path)
	}

	// Dedup table for the transitive LD plugin closure. Each direct peer
	// contributes its `LDPluginRefs/Paths` (already containing own ∪
	// transitive). First-occurrence wins; the closure flows through this
	// module's result so consumers further up pick it up without
	// re-walking.
	peerLDPluginRefs := make([]NodeRef, 0, 1)
	peerLDPluginPaths := make([]VFS, 0, 1)
	peerLDPluginSeen := map[VFS]struct{}{}
	peerLDPluginAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerLDPluginSeen[path]; dup {
			return
		}

		peerLDPluginSeen[path] = struct{}{}
		peerLDPluginRefs = append(peerLDPluginRefs, ref)
		peerLDPluginPaths = append(peerLDPluginPaths, path)
	}

	objAddLibSeen := map[string]struct{}{}
	peerObjAddLibsGlobal := make([]string, 0, 8)

	ldFlagsSeen := map[string]struct{}{}
	peerLDFlagsGlobal := make([]string, 0, 4)

	// Aggregate peer-GLOBAL across ADDINCL / CFLAGS / CXXFLAGS / CONLYFLAGS
	// with two-phase traversal: Phase 1 collects each peer's OWN GLOBAL
	// in declaration order; Phase 2 collects each peer's transitive
	// peer-GLOBAL. Single-phase DFS would put musl-arch ahead of
	// libcxx/libcxxrt OWN.
	addInclSeen := map[VFS]struct{}{}
	peerAddInclGlobal := make([]VFS, 0, 16)

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
	addEachVFS := func(seenSet map[VFS]struct{}, dst *[]VFS, src []VFS) {
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
		kind   int // peerKindLangDefault / peerKindProgramDefault / peerKindUserPeer
	}

	resolved := make([]resolvedPeer, 0, len(allPeers))

	// PASS A: resolve peers and aggregate LD plugin closure in
	// declaration order. Archive / .global.a aggregation is deferred to
	// PASS B so it can use a different iteration order without
	// disturbing the AddInclGlobal aggregation below.
	for i, p := range allPeers {
		peerPath := filepath.Clean(p)

		kind := peerKinds[i]

		// Language-defaults AND program-defaults tolerate a missing
		// ya.make (synthetic test fixtures). Only user-declared PEERDIRs
		// must exist.
		if kind != peerKindUserPeer && !peerYaMakeExists(ctx.fs, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(ctx, instance, d, peerPath)
		peerResult := genModule(ctx, peerInstance)
		if peerResult.isPROGRAM {
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})

		// Fold peer's LD plugin closure (own ∪ transitive) into ours.
		// Runs for header-only and non-header peers alike (musl.py.pyplugin
		// is owned by the header-only `contrib/libs/musl/include`).
		for i, p := range peerResult.LDPluginPaths {
			peerLDPluginAddPath(peerResult.LDPluginRefs[i], p)
		}
	}

	// PASS B: archive + .global.a aggregation in an archive-specific
	// order. PY*_PROGRAM consumers defer USE_PYTHON3's implicit peers
	// (contrib/libs/python, library/python/runtime_py3) to AFTER
	// user-declared PEERDIRs. Scoped to PY*_PROGRAM only — LIBRARY-scope
	// deferral regresses peer order for PeerArchiveClosurePaths
	// propagation.
	archiveOrder := resolved
	if d.moduleStmt != nil {
		// Tail-defer USE_PYTHON3 implicit peers ONLY for PY*_PROGRAM*.
		// For plain PROGRAM modules with USE_PYTHON3 (devtools/ymake/bin),
		// upstream's macro prepends these peers BEFORE the user PEERDIR
		// block, so the python closure must land FIRST — not deferred.
		// Tail reorder remains required for PY3_PROGRAM_BIN / PY*_PROGRAM
		// where user-PEERDIR(runtime_py3) intentionally dedups against
		// the implicit macro injection.
		switch d.moduleStmt.Name {
		case "PY2_PROGRAM":
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
		case "PY3_PROGRAM", "PY3_PROGRAM_BIN":
			if d.moduleStmt.Name == "PY3_PROGRAM_BIN" && !d.py3ProgramMultimodule {
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

				break
			}

			allocatorExplicitSet := make(map[string]struct{}, len(allocatorExplicitPeers))
			for _, p := range allocatorExplicitPeers {
				allocatorExplicitSet[filepath.Clean(p)] = struct{}{}
			}

			head := make([]resolvedPeer, 0, len(resolved))
			programTail := make([]resolvedPeer, 0, len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(postUserProgDefaults))
			pythonTail := make([]resolvedPeer, 0, 4)

			for _, rp := range resolved {
				if rp.path == "contrib/tools/python3/Modules/_sqlite" ||
					rp.path == "library/python/runtime_py3/main" ||
					rp.path == "library/python/import_tracing/constructor" ||
					rp.path == "library/python/testing/import_test" {
					pythonTail = append(pythonTail, rp)

					continue
				}

				if rp.kind == peerKindProgramDefault {
					programTail = append(programTail, rp)

					continue
				}

				if _, ok := allocatorExplicitSet[rp.path]; ok {
					programTail = append(programTail, rp)

					continue
				}

				head = append(head, rp)
			}

			archiveOrder = append(head, programTail...)
			archiveOrder = append(archiveOrder, pythonTail...)
		}
	}
	for _, rp := range archiveOrder {
		peerResult := rp.result

		// Fold peer's transitive archive closure BEFORE the peer's own
		// archive (DFS post-order: dependencies first, peer last).
		// Header-only peers may still expose a closure (their PEERDIRs'
		// archives) even though they emit no AR themselves.
		for i, p := range peerResult.PeerArchiveClosurePaths {
			peerArchiveAddPath(peerResult.PeerArchiveClosureRefs[i], p)
		}

		// Fold peer's transitive `.global.a` closure BEFORE the peer's
		// own (same shape as archive closure). Runs for header-only and
		// non-header peers — a header-only LIBRARY contributes no
		// `.global.a` itself but its peers may.
		for i, p := range peerResult.PeerGlobalClosurePaths {
			peerGlobalAddPath(peerResult.PeerGlobalClosureRefs[i], p)
		}

		// Header-only peers may expose their own `.global.a` (e.g. `certs`
		// RESOURCE-only LIBRARY emits `libcerts.global.a` from objcopy
		// outputs). Fold regardless of headerOnly.
		if peerResult.GlobalRef != nil && peerResult.GlobalPath != nil {
			peerGlobalAddPath(*peerResult.GlobalRef, *peerResult.GlobalPath)
		}

		for i, p := range peerResult.PeerWholeArchiveClosurePaths {
			peerWholeArchiveAddPath(peerResult.PeerWholeArchiveClosureRefs[i], p)
		}
		for i, p := range peerResult.WholeArchivePaths {
			peerWholeArchiveAddPath(peerResult.WholeArchiveRefs[i], p)
		}
		for _, p := range peerResult.PeerWholeArchiveCmdClosurePaths {
			peerWholeArchiveAddCmdPath(p)
		}
		for _, p := range peerResult.WholeArchiveCmdPaths {
			peerWholeArchiveAddCmdPath(p)
		}
		for i, p := range peerResult.PeerDynamicClosurePaths {
			peerDynamicAddPath(peerResult.PeerDynamicClosureRefs[i], p)
		}
		if peerResult.ModuleStmtName == "DYNAMIC_LIBRARY" && peerResult.LDPath != nil {
			peerDynamicAddPath(peerResult.LDRef, *peerResult.LDPath)
		}

		// peerResult.ARPath carries the py3-prefixed name for Python
		// modules — use it instead of recomputing ArchiveName. Empty path
		// means this module produced no plain `.a` (for example, only a
		// `.global.a`).
		if peerResult.ARPath != nil {
			peerArchiveAddPath(peerResult.ARRef, *peerResult.ARPath)
		}
	}

	// Per-kind aggregation. Language-defaults: two-phase (own first,
	// transitive second) so libcxx/libcxxrt OWN lands before musl-arch
	// transitive. User-peers and program-defaults: single-phase
	// AddInclGlobal in declaration order so an allocator-derived peer's
	// transitive GLOBAL precedes a later peer's OWN GLOBAL (ragel6
	// mimalloc-vs-aapl invariant) and program-defaults' OWN trail
	// language-defaults' transitive (archiver tcmalloc-after-zlib).

	// Phase 1: language-defaults' OWN GLOBAL declarations.
	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.OwnAddInclGlobal)
	}

	// Phase 2: language-defaults' TRANSITIVE peer-GLOBAL contributions
	// (full AddInclGlobal; dedup drops the OWN duplicates from Phase 1).
	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
	}

	// Program-defaults emit BEFORE user-peers in peer-GLOBAL ADDINCL
	// aggregation. In every REF cluster (archiver, ragel5/rlgen-cd,
	// protoc/bin, rescompiler/rescompressor, py3cc, library/python/
	// runtime_py3/stage0pycc) the program-default tcmalloc + abseil-cpp
	// pair appears AHEAD of any user-peer's OWN or transitive GLOBAL.
	// Archiver's user-peers contribute only dedups of lang-default paths
	// so program-defaults-first yields the same trailing tcmalloc +
	// abseil-cpp pair as user-peers-first.
	emitUserPeers := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindUserPeer {
				continue
			}

			addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
		}
	}

	emitProgramDefaults := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindProgramDefault {
				continue
			}

			addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
		}
	}

	emitProgramDefaults()
	emitUserPeers()

	// Drop bundled-include paths from the peer-propagated set.
	// `ccIncludesSuffix` already injects `-I…linux-headers{,/_nf}` at the
	// front of every non-musl CC node; a transitive peer's GLOBAL
	// declaration of the same paths would emit a duplicate at the
	// peer-AddIncl slot. Musl flavours drop the entire peer-AddInclGlobal
	// slice in cc.go's composer, so this filter is a no-op for them.
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

	// Hoist runtime-stack include paths (libcxx/include, libcxxrt/include,
	// libcxxabi/include, libunwind/include) to the FRONT of the aggregated
	// peer-GLOBAL ADDINCL slice. Fires only when THIS module is itself a
	// runtime ancestor; non-runtime-ancestor consumers (yasm with
	// NO_RUNTIME) intentionally pick up libcxx/libcxxrt at the TAIL via
	// musl_extra / jemalloc transitive walks, so the hoist is gated.
	if isRuntimeAncestor(instance.Path) {
		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
	}

	// CFLAGS / CXXFLAGS / CONLYFLAGS: collect every peer's transitive
	// GLOBAL flags. No-stdinc modules still propagate their GLOBAL flags
	// to explicit consumers; their own compile nodes suppress those flags
	// separately because the no-stdinc composer folds them into its fixed
	// compile shape.
	for _, rp := range resolved {
		addEach(cFlagsSeen, &peerCFlagsGlobal, rp.result.CFlagsGlobal)
		addEach(cxxFlagsSeen, &peerCXXFlagsGlobal, rp.result.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, rp.result.COnlyFlagsGlobal)
		addEach(objAddLibSeen, &peerObjAddLibsGlobal, rp.result.ObjAddLibsGlobal)
		addEach(ldFlagsSeen, &peerLDFlagsGlobal, rp.result.LDFlagsGlobal)
	}

	// Effective AddInclGlobal = own GLOBAL ADDINCL ∪ every peer's
	// transitive. Stored on the result so transitive consumers see the
	// closure in one shot.
	effectiveAddInclGlobal := mergeDedupVFS(d.addInclGlobal, peerAddInclGlobal)

	// `library/python/runtime_py3` propagates `$(B)/library/python/
	// runtime_py3` to consumers AFTER `contrib/restricted/abseil-cpp`,
	// not at the head as a regular own-GLOBAL would. Splice
	// matches upstream ordering: ARCHIVE's `addincl` modifier fires after
	// PEERDIR processing has already propagated abseil-cpp into
	// UserGlobalPropagated. Fallback: when abseil-cpp is absent, append
	// at the tail.
	if instance.Path == "library/python/runtime_py3" {
		buildRootPath := Build("library/python/runtime_py3")
		abseilPath := Source("contrib/restricted/abseil-cpp")
		spliced := make([]VFS, 0, len(effectiveAddInclGlobal)+1)
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

	// Peer-transitive CFLAGS GLOBAL precede this module's OWN CFLAGS
	// GLOBAL in the propagated slice (upstream `TGlobalVarsCollector`
	// pushes peers at DFS-Left and OWN only at PrepareLeaving). ADDINCL
	// follows the opposite rule (own before peer), so
	// `effectiveAddInclGlobal` keeps `(own, peer)` order above.
	effectiveCFlagsGlobal := mergeDedup(peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := mergeDedup(peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := mergeDedup(peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)

	// Inject libcxx's GLOBAL ADDINCL + GLOBAL CXXFLAGS into runtime-
	// ancestor C++ consumers' OWN CC emission only — not into the
	// `effective*` propagation slices already snapshotted above. Making
	// libcxx an implicit DEFAULT peer would propagate the includes to
	// every downstream consumer's Phase 2 walk, producing spurious -I
	// flags on unrelated CC nodes (zlib, mimalloc, libcxxabi-parts).
	// Mutating `peerAddInclGlobal`/`peerCXXFlagsGlobal` AFTER the snapshot
	// keeps the propagated view clean.
	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path] {
		// libcxx's CLANG-branch GLOBAL CXXFLAG (`-nostdinc++`) — see
		// `contrib/libs/cxxsupp/libcxx/ya.make:67-69`.
		const nostdincPP = "-nostdinc++"
		// libcxx's GLOBAL ADDINCL set on Linux with CXX_RT==libcxxrt
		// — see `contrib/libs/cxxsupp/libcxx/ya.make:24-25, 78-85`.
		injectAddIncl := []VFS{
			Source("contrib/libs/cxxsupp/libcxx/include"),
			Source("contrib/libs/cxxsupp/libcxxrt/include"),
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
		// position observed in malloc/api's REF cmd_args[11..12]). The
		// earlier hoist ran on the un-injected slice; running again is
		// idempotent for entries already at the front and a no-op when
		// nothing was injected.
		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
	}

	// Per-source dispatch. JoinSrcs entries become JS+CC pairs folded in
	// alongside regular SRCS. Header sources (`.h` / `.hpp`) skipped.
	// Own-source ordering: regular SRCS in declaration order, then each
	// JOIN_SRCS's compiled output appended; global srcs are processed
	// as a separate AR step so they don't pollute the regular `.a`.
	ccRefs := make([]NodeRef, 0, len(d.srcs)+len(d.joinSrcs))
	ccOutputs := make([]VFS, 0, len(d.srcs)+len(d.joinSrcs))
	// Track ccOutputs entries from SRC_C_NO_LTO (d.flatSrcs) so
	// reorderARMembers can hoist them to the front without disturbing
	// declaration order of regular SRCS members.
	ccIsFlatNoLto := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))
	// Track CF-generated CCs (the .cpp output of a .cpp.in / .c.in
	// CONFIGURE_FILE expansion). Their .o suffix matches a hand-written
	// .cpp, so reorderARMembers cannot detect them from the path alone;
	// REF places them after hand-written regulars in declaration order.
	// Witness: library/cpp/build_info (sandbox.cpp.in, build_info.cpp.in,
	// build_info_static.cpp → REF: build_info_static.cpp.o,
	// sandbox.cpp.o, build_info.cpp.o).
	ccIsCFGenerated := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))
	// Accumulate the union of every CC member's inputs (primary source +
	// IncludeInputs, deduped, in DFS-discovery order) so the downstream
	// AR/LD step folds these into its `inputs` per sg.json (AR includes
	// source files of its archived .o files plus their resolved header
	// closures).
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

	// Track primary sources of regular SRCS / JOIN_SRCS / .rl6 dispatch
	// — distinct from header closures. Regular AR archives regular
	// primaries + global primaries + everyone's header closures;
	// .global.a archives global primaries + everyone's header closures
	// (no regular primaries). The set is retained so call sites stay
	// untangled.
	regularPrimariesSet := map[VFS]struct{}{}
	addRegularPrimary := func(p VFS) {
		regularPrimariesSet[p] = struct{}{}
	}

	// Auto-injected peer-CFLAG -D_musl_ for every TARGET module that is
	// not effectively NO_PLATFORM, when the CLI says MUSL=yes. Mirrors
	// `_BASE_UNIT`'s `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`.
	// Suppressed for no-stdinc modules — those receive `-D_musl_=1`
	// directly via their dedicated composer slot.
	autoPeerCFlags := defaultPeerCFlags(ctx, instance, d)

	// Thread the module's own non-GLOBAL CFLAGS and own GLOBAL
	// CFLAGS / CXXFLAGS / CONLYFLAGS into ModuleCCInputs so the composer
	// emits them on this module's own CC compiles. NoStdInc modules
	// (musl-self) consume CFlags + OwnCFlagsGlobal via the dedicated
	// no-stdinc composer slot; cxx/conly remain zeroed because musl's
	// ya.make declares no CXXFLAGS/CONLYFLAGS.
	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if d.flags.NoStdInc {
		ownCXXFlagsGlobalSelf = nil
		ownCOnlyFlagsGlobalSelf = nil
	}

	// Dedup d.addIncl in first-occurrence-wins order. REF (openssl
	// drbg_lib.c.o idx 9-14: 6 unique entries) does not emit duplicate -I
	// flags when the same path appears in both top-level ADDINCL and an
	// INCLUDE'd `crypto/ya.make.inc` ADDINCL. Our parser appends without
	// dedup at the AddInclStmt site, so without this dedup openssl emits
	// 8 entries (6 unique + 2 trailing dupes). Composer-entry dedup keeps
	// the parser's append-only model while matching upstream's emit-time
	// behaviour.
	dedupedAddIncl := mergeDedupVFS(d.addIncl, nil)

	// PY23_NATIVE_LIBRARY and PY23_LIBRARY emit ".py3.o" CC outputs (not
	// plain ".o") per REF (e.g. library/python/symbols/module/
	// module.cpp.py3.o).
	isPy3NativeLib := d.moduleStmt.Name == "PY23_NATIVE_LIBRARY" ||
		d.moduleStmt.Name == "PY23_LIBRARY"

	// PY23_NATIVE_LIBRARY's PY3 submodule: PYTHON3_ADDINCL() →
	// SET(MODULE_TAG PY3_NATIVE) (build/conf/python.conf:995).
	// PY23_LIBRARY's PY3 submodule inherits PY3_LIBRARY →
	// _ARCADIA_PYTHON3_ADDINCL() → SET(MODULE_TAG PY3) (python.conf:1005).
	// REF surfaces these as lower-cased `target_properties.module_tag =
	// "py3_native"` / `"py3"` on per-source CC and the regular (.a) AR.
	// Plain PY3_LIBRARY (library/python/runtime_py3) carries no
	// module_tag — inherits its type default; upstream omits redundant
	// properties. The "global" / "py3_global" / "py3_native_global" tags
	// on .global.a archives are set at EmitARGlobalNamedTagged below.
	var perModuleCCTag *string
	switch d.moduleStmt.Name {
	case "PY23_NATIVE_LIBRARY":
		perModuleCCTag = stringPtr("py3_native")
	case "PY23_LIBRARY":
		perModuleCCTag = stringPtr("py3")
	}

	// arNameFn selects the archive naming function for this module:
	//   - PY23_NATIVE_LIBRARY → "libpy3c" prefix (Py3cArchiveName)
	//   - PY3_LIBRARY / PY2_LIBRARY / PY23_LIBRARY / PY2_PROGRAM / PY3_PROGRAM → "libpy3" prefix (Py3ArchiveName)
	//   - everything else → standard "lib" prefix (ArchiveName)
	var arNameFn func(string) string
	var globalArNameFn func(string) string
	archiveName := ""
	if len(d.moduleStmt.Args) > 0 {
		archiveName = d.moduleStmt.Args[0]
	}
	switch d.moduleStmt.Name {
	case "PY23_NATIVE_LIBRARY":
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "libpy3c", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "libpy3c", archiveName) }
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "libpy3", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "libpy3", archiveName) }
	default:
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "lib", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "lib", archiveName) }
	}

	// Drop BUILD_ROOT-rooted addincl paths from the peer slot when the
	// same path is in this module's own addincl. Generated-output paths
	// (`$(B)/<mod>`) are produced by THIS module's ARCHIVE() / RUN_PROGRAM
	// and arrive at peer consumers via the PEERDIR walk; the self-compile
	// must not also emit them in the peer slot. SOURCE_ROOT paths (e.g.
	// `python/Include`) are NOT filtered — REF deliberately emits the
	// own + peer duplicate (sitecustomize.cpp.pic.o ref:8+26,
	// ymakeyaml.cpp.o ref:9+21).
	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path, peerAddInclGlobal, dedupedAddIncl)

	moduleInputs := ModuleCCInputs{
		Flags:                d.flags,
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
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		Py3Suffix:            isPy3NativeLib,
		ModuleTag:            perModuleCCTag,
		Ragel6Flags:          d.ragel6Flags,
		BisonGenExt:          d.bisonGenExt,
	}

	// Ancestor-only SRCDIR rebase. The "PROGRAM with SRCDIR pointing at
	// an ancestor of instance.Path" pattern (typified by
	// `contrib/tools/ragel6/bin` whose SRCDIR is `contrib/tools/ragel6`)
	// is the only shape where REF rebases module_dir to SRCDIR. LIBRARYs
	// with SRCDIR keep module_dir at instance.Path and route per-source
	// via composeCCPaths' SRCDIR-aware composer.
	ancestorRebase := d.srcDir != nil && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(*d.srcDir, instance.Path)

	// Emit EN nodes BEFORE the per-source CC loop so the codegen registry
	// is populated when consumer sources scan their include closures.
	// If EN ran AFTER the source loop, the registry would be empty at
	// scan time and resolveCache / subgraphCache would lock in a
	// "not found" miss. Passing `moduleInputs` causes `emitEnumSrcs` to
	// also emit the downstream CC for each EN's `_serialized.cpp`.
	enCCRes := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

	// Hoist JV/CF/BI/PR node emission before the per-source loop so the
	// codegen registry is fully populated when any source's WalkClosure
	// runs. Mirrors the earlier emitEnumSrcs hoist; no state written by
	// the per-source loop is read here. Passing moduleInputs causes
	// emitMiscNodes to emit JV-downstream CP+CC pairs.
	jvCCRefs, jvCCOutputs, jvCCMemberInputs := emitMiscNodes(ctx, instance, d, &moduleInputs)

	// Hoist PR+AR node emission ahead of the SRCS loop so the codegen
	// registry's AR/PR ProducerRef entries exist when a consumer CC (e.g.
	// library/python/runtime_py3/__res.cpp) scans inputs and reaches the
	// .pyc.inc / PR-emitted output paths. Returned PR-downstream-CC
	// triples are folded into the AR-member bucket at the original site
	// below so the existing AR.cmd_args order stays byte-exact.
	prCCRes := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	emitArchives(ctx, instance, d)

	// Two-pass source emission. Codegen-producing sources
	// (.ev/.proto/.rl6/.rl/.cpp.in/.c.in) emit nodes whose outputs
	// consumer CCs in the same module may #include. Pass A emits all
	// codegen producers first to populate the registry; Pass B iterates
	// d.srcs in declaration order, using Pass A's cached results for
	// codegen producers and emitOneSource for the rest. AR member order
	// is preserved (Pass B appends in d.srcs order).
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
		// Overlay per-source extras recorded by `SRC(...)` /
		// `SRC_C_NO_LTO(...)` onto module-level inputs for THIS source
		// only. The composer slots `srcInputs.PerSourceCFlags` between
		// macroPrefixMapFlags and the input path; FlatOutput selects the
		// flat output layout (no `_/` infix). Plain SRCS / GLOBAL_SRCS
		// have no entries in either map so the overlay is a no-op.
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
		// Track primary source paths so the .global.a aggregator can
		// exclude them. The leading `PrimaryCount` entries of ccIns are
		// the member's primary source(s): .cpp/.c/.cc/.cxx/.S dispatch
		// yields 1; .rl6 yields 1 (the .rl6 source) or 2 (when the `.h`
		// companion exists on disk).
		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
		}
	}

	for _, emit := range emitCheckConfigH(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		addMemberInputs(emit.CcIns)

		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
		}
	}

	for _, emit := range emitCythonCpp(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		addMemberInputs(emit.CcIns)

		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
		}
	}

	for _, emit := range emitSwigC(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		addMemberInputs(emit.CcIns)

		for i := 0; i < emit.PrimaryCount && i < len(emit.CcIns); i++ {
			addRegularPrimary(emit.CcIns[i])
		}
	}

	// Headers (.h/.hpp) in SRCS do not emit a CC node, but upstream ymake
	// still walks their #include closure and propagates it up to the
	// AR/LD via EDT_BuildFrom (`addInput` in mkcmd.cpp:212-228, reached
	// for every EDT_BuildFrom file child whether or not it has a build
	// command). Without this pass the AR loses the transitive set reached
	// only through SRCS-listed headers (e.g. library/cpp/packedtypes:
	// fixed_point.h / packed.h / zigzag.h drag in libcxx/locale, numeric,
	// vector and zc_memory_input.h not reached by any .cpp member).
	for _, src := range d.srcs {
		if !isHeaderSource(src) {
			continue
		}
		headerVFS := resolveSourceVFS(ctx, instance, src, moduleInputs.SrcDir)
		headerClosure := walkClosure(ctx, instance, headerVFS, moduleInputs)
		all := append([]VFS{Source(instance.Path + "/" + src)}, headerClosure...)
		addMemberInputs(all)
	}

	// Fold JV-downstream CCs (CP-rename + compile per ANTLR-generated
	// .cpp) into the AR member bucket. REF places them after the regular
	// SRCS bucket and before the EN-downstream CCs (sg2.json
	// devtools/ymake/lang AR: TConfLexer.g4.cpp.o, TConfParser.g4.cpp.o,
	// CmdLexer.g4.cpp.o, CmdParser.g4.cpp.o after value_storage.cpp.o and
	// before h_serialized.cpp.o).
	for i, ref := range jvCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, jvCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(jvCCMemberInputs[i])
	}

	// Fold EN-downstream CCs (captured above via emitEnumSrcs) into the
	// regular AR member bucket. REF places these `.h_serialized.cpp.o`
	// entries after the module's declared SRCS `.cpp.o` and before any
	// JOIN_SRCS / PR-derived members (sg2.json devtools/ymake's
	// `libdevtools-ymake.a` cmd_args positions 134..142 — trailing the
	// 124-entry regular SRCS bucket).
	if enCCRes != nil {
		for i, ref := range enCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, enCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			addMemberInputs(enCCRes.MemberInputsList[i])
		}
	}

	// PR-downstream CC fold. emitRunProgramsForAR + emitArchives are
	// hoisted ahead of the SRCS loop so the codegen registry's PR/AR
	// ProducerRef entries are populated when consumer CCs (e.g.
	// library/python/runtime_py3/__res.cpp) scan their inputs[]. The
	// AR.cmd_args bucket ordering (PR-downstream CCs AFTER regular SRCS,
	// before JOIN_SRCS) is preserved by deferring the fold to this site.
	if prCCRes != nil {
		for i, ref := range prCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, prCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			addMemberInputs(prCCRes.MemberInputs[i])
		}
	}

	// Emit one CC node per SRC_C_<V> entry. Each variant compile reuses
	// the regular CC flavor pipeline (same AddIncl / peer/own CFLAGS /
	// scanner closure as plain SRCS) but carries the variant `-m<flag>`
	// bundle + extras at PerSourceCFlags and a `.<variant>` suffix in
	// the output path (FlatOutput=true so the path is `<module>/
	// <src>.<variant>.pic.o`, no `_/` infix even when nested). Inherits
	// the SRC_C_NO_LTO flat-bucket disposition for AR ordering:
	// reorderARMembers hoists them ahead of plain SRCS, matching REF
	// (blake2: SRC()s first, then 10 SIMD variants, then `_/`-infix SRCS).
	for _, e := range d.simdSrcs {
		variantIn := moduleInputs
		variantIn.FlatOutput = true
		variantIn.Variant = stringPtr(e.Variant)
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

	// Record the SRCS/JOIN_SRCS boundary so the AR cmd_args reorder below
	// applies the right bucket rules to each group independently. Entries
	// before this index are SRCS-derived (regular + .rl6); after, JOIN_SRCS.
	numSrcsDerived := len(ccOutputs)

	for _, js := range d.joinSrcs {
		// Rebase onto SRCDIR only when `ancestorRebase` is set (PROGRAM
		// with ancestor SRCDIR; the ragel6/bin pattern). Otherwise keep
		// srcInstance at instance.Path — JOIN_SRCS in
		// LIBRARY-with-sibling-SRCDIR modules emit at the LIBRARY's own dir.
		srcInstance := instance

		if ancestorRebase {
			srcInstance.Path = *d.srcDir
		}

		// JS nodes are anchored to the outer-target platform axis, so the
		// JS closure resolves with the TARGET scanner and arch search
		// paths even when srcInstance is a host (PIC) instance. The
		// downstream CC node compiles on the host axis and needs the
		// HOST closure; compute them separately for x86_64 (host PIC).
		// TODO: remove the ISA guard when a general target-vs-host axis
		// parameter is plumbed through.
		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		if srcInstance.Platform.ISA == ISAX8664 {
			// When this module is reached through a host (x86_64) walk
			// the JS node nevertheless emits on the target axis (see the
			// EmitJS call below — Platform is anchored to the outer-target
			// ID). Recompute the include closure with the target scanner +
			// arch-suffixed peer ADDINCL paths rebased to the target ISA;
			// the surrounding host walk's instance is kept verbatim — only
			// the override `scanPlatform` argument flips.
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = rebasePerArchPeerAddIncl(moduleInputs.PeerAddInclGlobal, srcInstance.Platform.ISA, ctx.target.ISA)

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, srcInstance, js.Sources, jsModuleInputs)
		}

		// Anchor the JS node to the outer-target platform regardless of
		// whether this module instance was reached through a host-PROGRAM
		// walk. REF emits every JS node on `default-linux-aarch64`
		// — including the 7 JOIN_SRCS in `contrib/tools/ragel6/bin` whose
		// surrounding LD lives on the host axis. Only the JS Platform
		// axis detaches; the downstream JS-derived CC below still
		// compiles at `srcInstance.Platform.Target` (host x86_64 for
		// ragel6/bin) so the .pic.o output stays on the correct axis.
		jsRef, joinOutVFS := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.target.Target, ctx.emit)

		// EmitJS returns a $(B)/<srcInstance.Path>/<name> absolute path;
		// convert to srcInstance-relative for the downstream EmitCC. The
		// JS output lives under $(B), so pass IsGenerated so EmitCC
		// composes inputPath under $(B) instead of $(S). The JS NodeRef
		// is threaded as the downstream CC's `Generator` so the CC
		// carries an explicit dep on its source-generating JS node,
		// matching REF (every JS-derived CC has DepRefs=[js UID]).
		jsRel := strings.TrimPrefix(joinOutVFS.Rel, srcInstance.Path+"/")

		// Thread (scripts + sources + closure) as the JS-derived CC's
		// IncludeInputs so its full Inputs read [joinedCpp, scripts...,
		// sources..., closure...] — same shape as JS Inputs with the
		// joined .cpp prepended. Use ccClosure (host scanner when PIC)
		// for the CC node, not joinClosure (target scanner).
		ccIncludeInputs := jsCCIncludeInputs(srcInstance, js.Sources, ccClosure)

		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = jsRef
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = ccIncludeInputs

		ref, outPath := EmitCC(srcInstance, jsRel, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false) // JOIN_SRCS are never SRC_C_NO_LTO
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		// The AR/LD `inputs` slot omits the BUILD_ROOT-staged generated
		// cpp (JS output). REF confirms: util's libyutil.a never lists
		// `$(B)/util/all_*.cpp` even though those are the primary inputs
		// of the downstream JS-derived CC nodes. The aggregator gets only
		// scripts + joined source files + their resolved include closure.
		addMemberInputs(ccIncludeInputs)
		// The joined source files (`js.Sources`) are "regular primaries"
		// — only the regular AR archives them; the .global.a aggregator
		// drops them. Scripts and the resolved header closure flow to
		// BOTH archives. REF: util's libyutil.a (no .global.a) and
		// util/charset's libutil-charset.a both archive the JS members.
		for _, s := range js.Sources {
			addRegularPrimary(Source(srcInstance.Path + "/" + s))
		}
	}

	// GLOBAL_SRCS get their own CC nodes and a separate AR pass
	// (see below). Filter headers here too.
	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]VFS, 0, len(d.globalSrcs))

	// GLOBAL_SRCS contribute their own member-inputs slice to the
	// .global.a archive (separate accumulator from regular AR).
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
	globalSrcMemberCount := len(globalRefs)

	// Emit PY+CC pairs for each PY_REGISTER(arg). Both flow into
	// globalRefs/globalOutputs (upstream macro `_PY3_REGISTER` appends
	// `SRCS(GLOBAL $Func.reg3.cpp)` so the .o lands in `.global.a`).
	// PY3_LIBRARY (rapidjson, ymakeyaml) emits plain `.reg3.cpp.o`;
	// PY23_LIBRARY and PY23_NATIVE_LIBRARY emit `.reg3.cpp.py3.o` (REF:
	// library/python/symbols/module — PY23_LIBRARY multimodule whose py3
	// submodule tags CC outputs with module_tag=py3 and .py3.o suffix).
	regCCPy3Suffix := isPy3NativeLib || d.moduleStmt.Name == "PY23_LIBRARY"
	regRes := emitPyRegister(ctx, instance, d, moduleInputs, regCCPy3Suffix)
	if regRes != nil {
		for i, ref := range regRes.Refs {
			globalRefs = append(globalRefs, ref)
			globalOutputs = append(globalOutputs, regRes.Outputs[i])
		}

		for _, p := range regRes.MemberInputs {
			if _, dup := globalMemberInputsSeen[p]; dup {
				continue
			}

			globalMemberInputsSeen[p] = struct{}{}
			globalMemberInputs = append(globalMemberInputs, p)
		}
	}

	// Emit own LD_PLUGIN CP nodes. Merged with the transitive peer plugin
	// closure; feeds EmitLD's `--start-plugins ... --end-plugins` block
	// (PROGRAMs) and the LDPluginRefs/Paths slot on `moduleEmitResult`.
	ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
	mergedLDPlugins := mergeLDPlugins(ownLDPlugins, &ldPluginsResult{
		Refs:  peerLDPluginRefs,
		Paths: peerLDPluginPaths,
	})
	if mergedLDPlugins == nil {
		mergedLDPlugins = &ldPluginsResult{}
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		// PROGRAM(name) declares the linker output basename directly.
		// Most ya.makes elide the argument (PROGRAM() → binary inherits
		// the directory's last component); `contrib/tools/ragel6/bin/
		// ya.make` declares `PROGRAM(ragel6)` so the binary is
		// `bin/ragel6`, not `bin/bin`. EmitLD's empty-fallback matches
		// the elided case. PY3_PROGRAM_BIN shares this dispatch with no
		// own CC outputs; its peer closure and LD node emit identically.
		var binaryName string

		if len(d.moduleStmt.Args) > 0 {
			binaryName = d.moduleStmt.Args[0]
		}

		// ALLOCATOR(FAKE) at PROGRAM level filters `library/cpp/malloc/
		// api` out of the link closure even when a transitive peer
		// (musl_extra, jemalloc, ...) reintroduces it via its own
		// default-peer set. Apply the same suppression that
		// `suppressMallocAPIDefault` applies to direct peers, at the
		// PROGRAM link-closure boundary.
		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths

		if d.allocatorName == "FAKE" {
			ldPeerArchiveRefs = make([]NodeRef, 0, len(peerArchiveRefs))
			ldPeerArchivePaths = make([]VFS, 0, len(peerArchivePaths))

			for i, p := range peerArchivePaths {
				if strings.HasPrefix(p.Rel, "library/cpp/malloc/api/") {
					continue
				}

				ldPeerArchiveRefs = append(ldPeerArchiveRefs, peerArchiveRefs[i])
				ldPeerArchivePaths = append(ldPeerArchivePaths, p)
			}
		}
		if d.py3ProgramMultimodule && d.moduleStmt.Name == "PY3_PROGRAM_BIN" && d.allocatorName == "J" {
			ldPeerArchiveRefs, ldPeerArchivePaths = moveArchivePathsAfter(
				ldPeerArchiveRefs,
				ldPeerArchivePaths,
				Build("build/cow/on/libbuild-cow-on.a"),
				[]VFS{
					Build("library/cpp/malloc/api/libcpp-malloc-api.a"),
					Build("contrib/libs/jemalloc/libcontrib-libs-jemalloc.a"),
					Build("library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a"),
				},
			)
			ldPeerArchiveRefs, ldPeerArchivePaths = moveArchivePathsBefore(
				ldPeerArchiveRefs,
				ldPeerArchivePaths,
				Build("library/cpp/json/common/libcpp-json-common.a"),
				[]VFS{
					Build("tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"),
				},
			)
		}

		// Python PROGRAM modules emit module_lang="py3". Tag the instance at
		// the EmitLD call site only so Language does not propagate into
		// derivePeerInstance's peer walks (peers are C++ LIBRARY modules
		// and must stay Language=LangCPP to share memo entries with the
		// rest of the target/host closure).
		ldInstance := instance
		if d.moduleStmt.Name == "PY2_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			ldInstance.Language = LangPy
		}

		// Python PROGRAM modules must emit yapyc3 and objcopy nodes BEFORE
		// EmitLD so the objcopy outputs can be folded into the LD's
		// SRCS_GLOBAL slot (REF wraps the program LD around per-resource
		// objcopy `.o` files). Objcopy paths flow through a dedicated
		// EmitLD slot — they go BEFORE $VCS_C_OBJ in cmd[2] and emit
		// BUILD_ROOT-relative (bare) per ld.conf:229-230 +
		// ${rootrel;ext=.o:SRCS_GLOBAL}.
		ldCCRefs := ccRefs
		ldCCOutputs := ccOutputs
		ldMemberInputs := memberInputs
		var ldObjcopyRefs []NodeRef
		var ldObjcopyPaths []VFS

		if resourceModuleTag(d.moduleStmt.Name) != nil {
			emitPySrcs(ctx, instance, d)

			objcopyRes := emitResourceObjcopy(ctx, instance, d)

			if objcopyRes != nil && len(objcopyRes.Refs) > 0 {
				ldObjcopyRefs = objcopyRes.Refs
				ldObjcopyPaths = objcopyRes.Outputs
			}
			if objcopyRes != nil && len(objcopyRes.GlobalMemberInputs) > 0 {
				seen := make(map[VFS]struct{}, len(ldMemberInputs))
				for _, p := range ldMemberInputs {
					seen[p] = struct{}{}
				}
				ldMemberInputs = append([]VFS(nil), ldMemberInputs...)
				for _, p := range objcopyRes.GlobalMemberInputs {
					if _, dup := seen[p]; dup {
						continue
					}
					seen[p] = struct{}{}
					ldMemberInputs = append(ldMemberInputs, p)
				}
			}

			// Fold the objcopy script + PY_SRCS source paths + RESOURCE
			// source paths into the LD member-input union so they appear
			// in the LD node's inputs (mirror of the reference shape for
			// tools/py3cc/slow/py3cc: build/scripts/objcopy.py +
			// tools/py3cc/slow/main.py + each declared RESOURCE source).
			if resourceModuleTag(d.moduleStmt.Name) != nil {
				var resourcePaths []string
				for _, e := range d.resources {
					if e.Path == "-" {
						continue
					}

					resourcePaths = append(resourcePaths, e.Path)
				}

				if extras := pySrcsARExtraInputs(instance.Path, d.srcDir, d.pySrcs, d.pyGeneratedSrcs, resourcePaths); len(extras) > 0 {
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

		// Explicit PY3_PROGRAM_BIN modules inherit _BASE_PY3_PROGRAM's
		// STRIP(). The PY3_PROGRAM multimodule bin-half does not surface
		// STRIP_FLAG in the observed reference shape for devtools/ya/bin.
		wantsStrip := d.moduleStmt.Name == "PY3_PROGRAM_BIN" && !d.py3ProgramMultimodule
		ldRef := EmitLD(
			ldInstance,
			binaryName,
			ldCCRefs, ldCCOutputs,
			ldPeerArchiveRefs, ldPeerArchivePaths,
			mergedLDPlugins.Refs, mergedLDPlugins.Paths,
			peerGlobalRefs, peerGlobalPaths,
			peerWholeArchiveRefs, peerWholeArchivePaths,
			peerWholeArchiveCmdPaths,
			peerDynamicRefs, peerDynamicPaths,
			ldObjcopyRefs, ldObjcopyPaths,
			ldMemberInputs,
			ownCFlags,
			peerCFlagsGlobal,
			autoPeerCFlags,
			peerLDFlagsGlobal,
			peerObjAddLibsGlobal,
			d.flags.NoCompilerWarnings,
			wantsStrip,
			d.splitDwarf,
			ctx.host,
			ctx.emit,
		)
		ldPath := LDOutputPath(instance, binaryName)

		result := &moduleEmitResult{
			ARRef:                           ldRef,
			ARPath:                          &ldPath,
			isPROGRAM:                       true,
			isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
			LDRef:                           ldRef,
			LDPath:                          &ldPath,
			AddInclGlobal:                   effectiveAddInclGlobal,
			OwnAddInclGlobal:                cloneVFSs(d.addInclGlobal),
			CFlagsGlobal:                    effectiveCFlagsGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
			PeerArchiveClosureRefs:          append([]NodeRef(nil), peerArchiveRefs...),
			PeerArchiveClosurePaths:         cloneVFSs(peerArchivePaths),
			PeerGlobalClosureRefs:           append([]NodeRef(nil), peerGlobalRefs...),
			PeerGlobalClosurePaths:          cloneVFSs(peerGlobalPaths),
			PeerWholeArchiveClosureRefs:     append([]NodeRef(nil), peerWholeArchiveRefs...),
			PeerWholeArchiveClosurePaths:    cloneVFSs(peerWholeArchivePaths),
			PeerWholeArchiveCmdClosurePaths: cloneVFSs(peerWholeArchiveCmdPaths),
			LDPluginRefs:                    mergedLDPlugins.Refs,
			LDPluginPaths:                   mergedLDPlugins.Paths,
			PeerDynamicClosureRefs:          append([]NodeRef(nil), peerDynamicRefs...),
			PeerDynamicClosurePaths:         cloneVFSs(peerDynamicPaths),
			InducedDeps:                     append([]string(nil), d.inducedDeps...),
			Peerdirs:                        append([]string(nil), d.peerdirs...),
			ModuleStmtName:                  d.moduleStmt.Name,
		}
		ctx.memo[instance] = result

		return result
	}

	// LIBRARY: regular AR over own CCs. Peer-archive DepRefs are
	// intentionally NOT threaded — every reference AR has zero AR-on-AR
	// deps; peer archives flow into the consumer's downstream LD via
	// `peerArchiveRefs` in EmitLD. The regular AR receives the union of
	// regular and global members' inputs (primaries + header closures).
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

	// PY*_LIBRARY modules with PY_SRCS emit objcopy nodes (see
	// emitPySrcObjcopy) whose inputs include build/scripts/objcopy.py
	// plus every PY_SRCS source `.py` path. REF union-aggregates those
	// into the module's regular `.a` and `.global.a` inputs. Mirror by
	// injecting the same set into combinedMemberInputs before AR emission.
	// Gate on resourceModuleTag (same as emitPySrcObjcopy) so non-PY3
	// modules stay unaffected.
	if d.moduleStmt != nil && resourceModuleTag(d.moduleStmt.Name) != nil {
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
		for _, e := range d.pyPyiResources {
			if e.Path == "-" {
				continue
			}

			resourcePaths = append(resourcePaths, e.Path)
		}

		if extras := pySrcsARExtraInputs(instance.Path, d.srcDir, d.pySrcs, d.pyGeneratedSrcs, resourcePaths); len(extras) > 0 {
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

	// Reorder AR members into ymake's canonical bucket order: SRC_C_NO_LTO
	// first, then regular SRCS (declaration order), then JOIN_SRCS, then
	// R6-generated last.
	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, numSrcsDerived)

	// Skip plain AR when there are no regular CC outputs (module has only
	// GLOBAL_SRCS — blockcodecs codecs, getopt). REF emits no regular
	// (non-global) archive for these; only EmitARGlobal below produces
	// the `.global.a`.
	//
	// Python library modules use py3-prefixed archive names
	// (Py3cArchiveName for PY23_NATIVE_LIBRARY, Py3ArchiveName for
	// PY3_LIBRARY etc.); we route through EmitARNamed with the name
	// selected by arNameFn.
	var arRef NodeRef
	arBaseName := arNameFn(instance.Path)

	// PY3_LIBRARY / PY23_LIBRARY surface module_lang="py3" on both the
	// regular and global archive in REF. PY23_NATIVE_LIBRARY retains
	// module_lang="cpp" (its tag flips to py3_native / py3_native_global
	// instead). Pivot the AR-emission instance's Language locally so the
	// .a / .global.a nodes carry the right value; surrounding walker's
	// Language stays LangCPP (peer-walks must share memo entries with
	// the rest of the cpp closure).
	arInstance := instance
	switch d.moduleStmt.Name {
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
		arInstance.Language = LangPy
	}

	// Resolve AR_PLUGIN path (`$(S)/<modulePath>/<name>.pyplugin`) when
	// the macro fired on this module's ya.make.
	var arPluginVFS *VFS
	if d.arPlugin != nil {
		v := Source(instance.Path + "/" + *d.arPlugin)
		arPluginVFS = &v
	}

	if len(ccRefs) > 0 {
		// PY23_LIBRARY / PY23_NATIVE_LIBRARY surface
		// `module_tag=py3` / `module_tag=py3_native`. openssl AR_PLUGIN(ar)
		// injects `--plugin <ar.pyplugin>` between the link_lib.py `--`
		// separators.
		if perModuleCCTag != nil {
			arRef = EmitARNamedTagged(arInstance, arBaseName, *perModuleCCTag, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.host, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.host, ctx.emit)
		}
	}

	_ = peerArchiveRefs // retained as a loop accumulator for the PROGRAM LD branch above; intentionally unused for the LIBRARY AR.
	var arPath *VFS
	if len(ccRefs) > 0 {
		arPath = vfsPtr(Build(instance.Path + "/" + arBaseName))
	}

	// Emit yapyc3 PY nodes for PY_SRCS(). Modules with both SRCS and
	// PY_SRCS (rare but valid) get CC/AR nodes from the SRCS path above
	// AND yapyc3 nodes here.
	emitPySrcs(ctx, instance, d)

	genPyAuxRes := emitGeneratedPyAuxChunks(ctx, instance, d, moduleInputs)
	if genPyAuxRes != nil {
		globalRefs = append(globalRefs, genPyAuxRes.Refs...)
		globalOutputs = append(globalOutputs, genPyAuxRes.Outputs...)
		for _, p := range genPyAuxRes.MemberInputs {
			if _, dup := globalMemberInputsSeen[p]; dup {
				continue
			}

			globalMemberInputsSeen[p] = struct{}{}
			globalMemberInputs = append(globalMemberInputs, p)
		}
	}

	// Emit objcopy PY nodes for RESOURCE / RESOURCE_FILES. Returned `.o`
	// paths flow into the module's `.global.a` (appended into
	// globalRefs/globalOutputs below). The objcopy nodes' SOURCE_ROOT
	// inputs (per-entry source paths + objcopy.py) are folded into the
	// GLOBAL_SRCS-local closure feeding `.global.a`'s `inputs` slot;
	// dedup against the existing accumulator.
	objcopyRes := emitResourceObjcopy(ctx, instance, d)
	if objcopyRes != nil {
		globalRefs = append(globalRefs, objcopyRes.Refs...)
		globalOutputs = append(globalOutputs, objcopyRes.Outputs...)
		if resourceBeforeGlobalSrcs(d) && globalSrcMemberCount > 0 && len(objcopyRes.Refs) > 0 {
			globalRefs = moveTailNodeRefsToFront(globalRefs, len(objcopyRes.Refs))
			globalOutputs = moveTailVFSToFront(globalOutputs, len(objcopyRes.Outputs))
		}

		for _, p := range objcopyRes.GlobalMemberInputs {
			if _, dup := globalMemberInputsSeen[p]; dup {
				continue
			}

			globalMemberInputsSeen[p] = struct{}{}
			globalMemberInputs = append(globalMemberInputs, p)
		}
	}

	// EN and JV/CF/BI/PR emissions are hoisted to the pre-source-loop
	// site above so the codegen registry is populated before consumer
	// CCs scan their inputs.

	result := &moduleEmitResult{
		ARRef:                           arRef,
		ARPath:                          arPath,
		isPROGRAM:                       false,
		isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
		LDRef:                           arRef,
		LDPath:                          arPath,
		AddInclGlobal:                   effectiveAddInclGlobal,
		OwnAddInclGlobal:                cloneVFSs(d.addInclGlobal),
		CFlagsGlobal:                    effectiveCFlagsGlobal,
		CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
		COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
		ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
		LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
		PeerArchiveClosureRefs:          append([]NodeRef(nil), peerArchiveRefs...),
		PeerArchiveClosurePaths:         cloneVFSs(peerArchivePaths),
		PeerGlobalClosureRefs:           append([]NodeRef(nil), peerGlobalRefs...),
		PeerGlobalClosurePaths:          cloneVFSs(peerGlobalPaths),
		PeerWholeArchiveClosureRefs:     append([]NodeRef(nil), peerWholeArchiveRefs...),
		PeerWholeArchiveClosurePaths:    cloneVFSs(peerWholeArchivePaths),
		PeerWholeArchiveCmdClosurePaths: cloneVFSs(peerWholeArchiveCmdPaths),
		LDPluginRefs:                    mergedLDPlugins.Refs,
		LDPluginPaths:                   mergedLDPlugins.Paths,
		PeerDynamicClosureRefs:          append([]NodeRef(nil), peerDynamicRefs...),
		PeerDynamicClosurePaths:         cloneVFSs(peerDynamicPaths),
		InducedDeps:                     append([]string(nil), d.inducedDeps...),
		Peerdirs:                        append([]string(nil), d.peerdirs...),
		ModuleStmtName:                  d.moduleStmt.Name,
	}
	if len(globalRefs) > 0 {
		// The `.global.a` aggregator gets the GLOBAL_SRCS-local closure
		// ONLY, not the regular AR's header closure. REF constrains
		// `.global.a` `inputs` to GLOBAL_SRCS member-CC closures,
		// PY_REGISTER reg3.cpp closures, and objcopy SOURCE_ROOT inputs
		// (RESOURCE source paths + objcopy.py). The regular CC closure
		// of SRCS members (Python.h, libcxx, glibcasm, musl, ...) does
		// NOT propagate into `.global.a` — it propagates into the
		// regular `.a`. tcmalloc/no_percpu_cache has no regular SRCS,
		// so combined == global there.
		globalAggregated := globalMemberInputs

		globalBaseName := globalArNameFn(instance.Path)
		// module_tag mapping: PY23_LIBRARY → py3_global;
		// PY23_NATIVE_LIBRARY → py3_native_global; rest → "global".
		globalTag := "global"
		switch d.moduleStmt.Name {
		case "PY23_LIBRARY":
			globalTag = "py3_global"
		case "PY23_NATIVE_LIBRARY":
			globalTag = "py3_native_global"
		}
		// The `.global.a` aggregator uses the same member-order discipline
		// as the regular AR — hand-written / objcopy_* .o files precede
		// codegen-derived .reg3.cpp.o etc.
		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), len(globalRefs))
		globalRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, globalAggregated, ctx.host, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(Build(instance.Path + "/" + globalBaseName))
	}

	ctx.memo[instance] = result

	return result
}

// mergeDedup returns `a ++ b` deduped, preserving declaration order
// (R14 — first occurrence wins). Used by genModule to compose this
// module's effective peer-GLOBAL slices (own first, then transitive
// peer) uniformly across ADDINCL / CFLAGS / CXXFLAGS / CONLYFLAGS.
//
// filterBuildRootSelfPaths drops peer `$(B)/<this-module>/...` paths that
// also appear in `own`. Broader BUILD_ROOT dedup is incorrect: modules like
// `yt/yt/client` legitimately keep an own `$(B)/yt` and a peer-propagated
// `$(B)/yt` from PROTO_NAMESPACE peers as two separate include slots.
func filterBuildRootSelfPaths(instancePath string, peer, own []VFS) []VFS {
	if len(peer) == 0 {
		return peer
	}

	ownSet := make(map[VFS]struct{}, len(own))
	ownPrefix := Build(instancePath)

	for _, p := range own {
		if p.IsBuild() && (p == ownPrefix || strings.HasPrefix(p.Rel, ownPrefix.Rel+"/")) {
			ownSet[p] = struct{}{}
		}
	}

	if len(ownSet) == 0 {
		return peer
	}

	out := make([]VFS, 0, len(peer))

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

// filterEnSerializedSiblings drops entries whose VFS path ends in the
// EN-generator suffixes `_serialized.cpp` or `_serialized.h`. Used at
// the R6 input boundary: REF's R6 closure walks transitively through
// `#include <..._serialized.h>` descendants but does not list the
// EN-generated siblings themselves in the R6 node's inputs.
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

func moveArchivePathsAfter(refs []NodeRef, paths []VFS, anchor VFS, moved []VFS) ([]NodeRef, []VFS) {
	if len(moved) == 0 || len(refs) != len(paths) {
		return refs, paths
	}

	moveSet := make(map[VFS]struct{}, len(moved))
	for _, path := range moved {
		moveSet[path] = struct{}{}
	}

	outRefs := make([]NodeRef, 0, len(refs))
	outPaths := make([]VFS, 0, len(paths))
	movedRefs := make(map[VFS]NodeRef, len(moved))
	movedPaths := make(map[VFS]VFS, len(moved))

	for i, path := range paths {
		if _, ok := moveSet[path]; ok {
			movedRefs[path] = refs[i]
			movedPaths[path] = path
			continue
		}

		outRefs = append(outRefs, refs[i])
		outPaths = append(outPaths, path)
		if path == anchor {
			for _, movedPath := range moved {
				if p, ok := movedPaths[movedPath]; ok {
					outRefs = append(outRefs, movedRefs[movedPath])
					outPaths = append(outPaths, p)
				}
			}
		}
	}

	if len(outPaths) != len(paths) {
		return refs, paths
	}

	return outRefs, outPaths
}

func moveArchivePathsBefore(refs []NodeRef, paths []VFS, anchor VFS, moved []VFS) ([]NodeRef, []VFS) {
	if len(moved) == 0 || len(refs) != len(paths) {
		return refs, paths
	}

	moveSet := make(map[VFS]struct{}, len(moved))
	for _, path := range moved {
		moveSet[path] = struct{}{}
	}

	movedRefs := make(map[VFS]NodeRef, len(moved))
	movedPaths := make(map[VFS]VFS, len(moved))
	for i, path := range paths {
		if _, ok := moveSet[path]; ok {
			movedRefs[path] = refs[i]
			movedPaths[path] = path
		}
	}

	if len(movedPaths) != len(moved) {
		return refs, paths
	}

	outRefs := make([]NodeRef, 0, len(refs))
	outPaths := make([]VFS, 0, len(paths))

	for i, path := range paths {
		if _, ok := moveSet[path]; ok {
			continue
		}

		if path == anchor {
			for _, movedPath := range moved {
				if p, ok := movedPaths[movedPath]; ok {
					outRefs = append(outRefs, movedRefs[movedPath])
					outPaths = append(outPaths, p)
				}
			}
		}

		outRefs = append(outRefs, refs[i])
		outPaths = append(outPaths, path)
	}

	if len(outPaths) != len(paths) {
		return refs, paths
	}

	return outRefs, outPaths
}

func resourceBeforeGlobalSrcs(d *moduleData) bool {
	return d.firstResourceEvent >= 0 &&
		d.firstGlobalSrcsEvent >= 0 &&
		d.firstResourceEvent < d.firstGlobalSrcsEvent
}

func moveTailNodeRefsToFront(in []NodeRef, tailLen int) []NodeRef {
	if tailLen <= 0 || tailLen >= len(in) {
		return in
	}

	pivot := len(in) - tailLen
	out := make([]NodeRef, 0, len(in))
	out = append(out, in[pivot:]...)
	out = append(out, in[:pivot]...)

	return out
}

func moveTailVFSToFront(in []VFS, tailLen int) []VFS {
	if tailLen <= 0 || tailLen >= len(in) {
		return in
	}

	pivot := len(in) - tailLen
	out := make([]VFS, 0, len(in))
	out = append(out, in[pivot:]...)
	out = append(out, in[:pivot]...)

	return out
}

// mergeLDPlugins concatenates `(ownRefs, ownPaths)` with `(peerRefs,
// peerPaths)`, dropping any peer entry whose path appears in own.
// Mirrors `mergeDedup` for the parallel-slice case (LD plugin propagation).
func mergeLDPlugins(own, peer *ldPluginsResult) *ldPluginsResult {
	var ownRefs []NodeRef
	var ownPaths []VFS
	if own != nil {
		ownRefs = own.Refs
		ownPaths = own.Paths
	}
	var peerRefs []NodeRef
	var peerPaths []VFS
	if peer != nil {
		peerRefs = peer.Refs
		peerPaths = peer.Paths
	}
	if len(ownPaths) == 0 && len(peerPaths) == 0 {
		return nil
	}

	seen := make(map[VFS]struct{}, len(ownPaths)+len(peerPaths))
	out := &ldPluginsResult{
		Refs:  make([]NodeRef, 0, len(ownPaths)+len(peerPaths)),
		Paths: make([]VFS, 0, len(ownPaths)+len(peerPaths)),
	}

	for i, p := range ownPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out.Refs = append(out.Refs, ownRefs[i])
		out.Paths = append(out.Paths, p)
	}

	for i, p := range peerPaths {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out.Refs = append(out.Refs, peerRefs[i])
		out.Paths = append(out.Paths, p)
	}

	return out
}

// peerGlobalContribs is the per-axis aggregation of a header-only
// LIBRARY's peer-walk. All four axes share the same declaration-order +
// dedup discipline as the main walker. Two-phase collection: for each
// peer, collect its OWN declarations FIRST (across all peers), then
// collect transitive contributions — giving the REF ordering
// (own-from-peer1, ..., transitive-from-peer1, ...) where
// libcxx/include + libcxxrt/include precede the musl-arch propagation
// chain (util/charset, tools/archiver/main.cpp.o cmd_args[11..16]).
type peerGlobalContribs struct {
	addIncl    []VFS
	cFlags     []string
	cxxFlags   []string
	cOnlyFlags []string
	objAddLibs []string
	ldFlags    []string
	// Archive closure transitively reachable from this header-only
	// LIBRARY's peers — DFS post-order, dedup-by-path (same discipline as
	// the main walker). Header-only LIBRARYs emit no AR themselves but
	// expose the transitive archive closure (e.g. `contrib/libs/musl/
	// include` is header-only and its `IF` branches PEERDIR
	// `contrib/libs/musl` — the consumer needs musl in its archive set
	// even though musl/include contributes no archive).
	archiveRefs  []NodeRef
	archivePaths []VFS
	// `.global.a` closure transitively reachable through this header-only
	// LIBRARY's peers (every peer's own GlobalRef ∪ PeerGlobalClosure*).
	// Header-only LIBRARYs emit no `.global.a` themselves but peers may.
	globalRefs  []NodeRef
	globalPaths []VFS
	// `_WHOLE_ARCHIVE_LIBS_VALUE_GLOBAL` closure transitively reachable
	// through this header-only LIBRARY's peers.
	wholeArchiveRefs     []NodeRef
	wholeArchivePaths    []VFS
	wholeArchiveCmdPaths []VFS
	// LD plugin closure surfaced through the header-only walker. Same
	// dedup-by-path / declaration-order / first-occurrence-wins.
	ldPluginRefs  []NodeRef
	ldPluginPaths []VFS
	dynamicRefs   []NodeRef
	dynamicPaths  []VFS
}

// walkPeersForGlobalAddIncl walks a header-only LIBRARY's peers,
// ensuring transitive closure discovery (genModule memoises) AND
// returning the per-axis union of every peer's transitive *Global
// contribution (ADDINCL, CFLAGS, CXXFLAGS, CONLYFLAGS). Header-only
// modules emit no AR; archive refs are dropped except for the GLOBAL
// peer-propagation path.
func walkPeersForGlobalAddIncl(ctx *genCtx, instance ModuleInstance, d *moduleData) peerGlobalContribs {
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	// Mirror genModule's ALLOCATOR(FAKE) malloc/api suppression. No
	// current header-only case declares ALLOCATOR, so normally a no-op.
	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)

	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))
	out := peerGlobalContribs{}
	addInclSeen := map[VFS]struct{}{}
	cFlagsSeen := map[string]struct{}{}
	cxxFlagsSeen := map[string]struct{}{}
	cOnlyFlagsSeen := map[string]struct{}{}
	objAddLibSeen := map[string]struct{}{}
	ldFlagsSeen := map[string]struct{}{}
	archiveSeen := map[VFS]struct{}{}
	globalSeen := map[VFS]struct{}{}
	wholeArchiveSeen := map[VFS]struct{}{}
	wholeArchiveCmdSeen := map[VFS]struct{}{}
	ldPluginSeen := map[VFS]struct{}{}
	dynamicSeen := map[VFS]struct{}{}

	addEach := func(seenSet map[string]struct{}, dst *[]string, src []string) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}
	addEachVFS := func(seenSet map[VFS]struct{}, dst *[]VFS, src []VFS) {
		for _, x := range src {
			if _, dup := seenSet[x]; dup {
				continue
			}

			seenSet[x] = struct{}{}
			*dst = append(*dst, x)
		}
	}

	addArchive := func(ref NodeRef, path VFS) {
		if _, dup := archiveSeen[path]; dup {
			return
		}

		archiveSeen[path] = struct{}{}
		out.archiveRefs = append(out.archiveRefs, ref)
		out.archivePaths = append(out.archivePaths, path)
	}

	addGlobal := func(ref NodeRef, path VFS) {
		if _, dup := globalSeen[path]; dup {
			return
		}

		globalSeen[path] = struct{}{}
		out.globalRefs = append(out.globalRefs, ref)
		out.globalPaths = append(out.globalPaths, path)
	}

	addWholeArchive := func(ref NodeRef, path VFS) {
		if _, dup := wholeArchiveSeen[path]; dup {
			return
		}

		wholeArchiveSeen[path] = struct{}{}
		out.wholeArchiveRefs = append(out.wholeArchiveRefs, ref)
		out.wholeArchivePaths = append(out.wholeArchivePaths, path)
	}

	addWholeArchiveCmd := func(path VFS) {
		if _, dup := wholeArchiveCmdSeen[path]; dup {
			return
		}

		wholeArchiveCmdSeen[path] = struct{}{}
		out.wholeArchiveCmdPaths = append(out.wholeArchiveCmdPaths, path)
	}

	addLDPlugin := func(ref NodeRef, path VFS) {
		if _, dup := ldPluginSeen[path]; dup {
			return
		}

		ldPluginSeen[path] = struct{}{}
		out.ldPluginRefs = append(out.ldPluginRefs, ref)
		out.ldPluginPaths = append(out.ldPluginPaths, path)
	}

	addDynamic := func(ref NodeRef, path VFS) {
		if _, dup := dynamicSeen[path]; dup {
			return
		}

		dynamicSeen[path] = struct{}{}
		out.dynamicRefs = append(out.dynamicRefs, ref)
		out.dynamicPaths = append(out.dynamicPaths, path)
	}

	walk := func(peerPath string) {
		peerInstance := derivePeerInstance(ctx, instance, d, peerPath)
		peerResult := genModule(ctx, peerInstance)
		addEachVFS(addInclSeen, &out.addIncl, peerResult.AddInclGlobal)
		addEach(cFlagsSeen, &out.cFlags, peerResult.CFlagsGlobal)
		addEach(cxxFlagsSeen, &out.cxxFlags, peerResult.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &out.cOnlyFlags, peerResult.COnlyFlagsGlobal)
		addEach(objAddLibSeen, &out.objAddLibs, peerResult.ObjAddLibsGlobal)
		addEach(ldFlagsSeen, &out.ldFlags, peerResult.LDFlagsGlobal)

		// Fold peer's transitive archive closure plus peer's own AR
		// (when present) in DFS post-order.
		for i, p := range peerResult.PeerArchiveClosurePaths {
			addArchive(peerResult.PeerArchiveClosureRefs[i], p)
		}

		// Use peerResult.ARPath (py3-prefixed for Python modules); skip
		// Empty ARPath means this peer produced no plain `.a`.
		if peerResult.ARPath != nil {
			addArchive(peerResult.ARRef, *peerResult.ARPath)
		}

		// Fold peer's transitive `.global.a` closure plus peer's own
		// `.global.a` (when GlobalRef != nil). Header-only peers may
		// emit a `.global.a` (e.g. `certs` from RESOURCE-driven objcopy).
		for i, p := range peerResult.PeerGlobalClosurePaths {
			addGlobal(peerResult.PeerGlobalClosureRefs[i], p)
		}

		if peerResult.GlobalRef != nil {
			if peerResult.GlobalPath != nil {
				addGlobal(*peerResult.GlobalRef, *peerResult.GlobalPath)
			}
		}

		for i, p := range peerResult.PeerWholeArchiveClosurePaths {
			addWholeArchive(peerResult.PeerWholeArchiveClosureRefs[i], p)
		}
		for i, p := range peerResult.WholeArchivePaths {
			addWholeArchive(peerResult.WholeArchiveRefs[i], p)
		}
		for _, p := range peerResult.PeerWholeArchiveCmdClosurePaths {
			addWholeArchiveCmd(p)
		}
		for _, p := range peerResult.WholeArchiveCmdPaths {
			addWholeArchiveCmd(p)
		}

		// Fold peer's transitive LD plugin closure. Header-only peers
		// (musl/include) populate from their own LD_PLUGIN macro;
		// non-header peers carry through if any transitive PEERDIR did.
		for i, p := range peerResult.LDPluginPaths {
			addLDPlugin(peerResult.LDPluginRefs[i], p)
		}
		for i, p := range peerResult.PeerDynamicClosurePaths {
			addDynamic(peerResult.PeerDynamicClosureRefs[i], p)
		}
		if peerResult.ModuleStmtName == "DYNAMIC_LIBRARY" && peerResult.LDPath != nil {
			addDynamic(peerResult.LDRef, *peerResult.LDPath)
		}
	}

	for _, p := range defaults {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		peerPath := filepath.Clean(p)

		if !peerYaMakeExists(ctx.fs, peerPath) {
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

	// Header-only LIBRARYs keep the natural Phase 1+2 order — the hoist
	// gate in genModule keys on `isRuntimeAncestor`. A future header-only
	// LIBRARY needing the same treatment can flip this to mirror it.

	// Drop bundled-include paths (linux-headers, linux-headers/_nf) from
	// the propagated set — `ccIncludesSuffix` already provides them.
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
// source the rule emitter would compile (excluding pure headers in
// SRCS, which upstream uses as IDE/dependency-tracking metadata, and
// known-deferred sources handled by dedicated emitters — e.g.
// .proto/.ev via emitProtoSrcs). Modules with only JOIN_SRCS / globals
// also count.
func hasCompilableSource(d *moduleData) bool {
	for _, s := range d.srcs {
		if !isHeaderSource(s) && !isSkippedSource(s) {
			return true
		}
	}

	if len(d.joinSrcs) > 0 {
		return true
	}

	if len(d.cythonCpp) > 0 {
		return true
	}

	if len(d.swigC) > 0 {
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

// isSkippedSource reports whether `srcRel` is a deferred source kind
// the emitter does not yet handle. Silently skipped rather than
// throwing "unsupported extension". .rl (ragel5) and .cpp.in/.c.in are
// handled by emitOneSource and are NOT skipped here:
//   - .proto → PB (emitProtoSrcs, PROTO_LIBRARY header-only)
//   - .ev    → EV (emitOneSource for LIBRARY, emitProtoSrcs for PROTO_LIBRARY)
//   - .py    → PY via runtime library
//   - .g4    → ANTLR4 grammar via RUN_ANTLR4_CPP
func isSkippedSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".py") ||
		strings.HasSuffix(srcRel, ".g4")
}

// isCodegenProducingSrc reports whether `srcRel` is a source whose
// emitOneSource branch emits a codegen node (PB/EV/R6/R5/CF) whose
// outputs go into the per-scanner CodegenRegistry. Consumer sources in
// the SAME module may #include those outputs, so the two-pass loop runs
// these first to populate the registry before any consumer CC scans
// its closure.
func isCodegenProducingSrc(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".y") ||
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

// emitOneSource dispatches a single source by extension and returns a
// `*sourceEmit` with the emitted ref, output path, CC inputs (primary
// source + IncludeInputs), and primaryCount. Headers return nil.
// Unknown extensions throw. When `srcDir` is set, per-source emitter
// view relocates SRCS to `$(S)/<srcDir>/<rel>` and the node's
// `module_dir` becomes `<srcDir>`; LD/AR archives stay at
// `instance.Path`. `primaryCount` distinguishes member primaries from
// header/closure entries so the .global.a aggregator can drop
// regular-SRCS primaries.

// reorderARMembers reorders (refs, paths) so AR cmd_args match ymake's
// canonical member ordering:
//
//  1. SRC_C_NO_LTO sources — hoisted to the front in original order.
//  2. Regular hand-written SRCS — declaration order.
//  3. CONFIGURE_FILE-derived (parallel signal — path-indistinguishable).
//  4. JOIN_SRCS — declaration order.
//  5. Codegen-derived .o files: .g4 → .h_serialized → .ev.pb →
//     .rl6 → .reg3 (declaration order within each).
//  6. Legacy R6 paths with the `/_/_/` infix go last.
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

// tool walks `modulePath` as a host-platform tool and returns its LD
// NodeRef + binary path. Memoised via ctx.memo. Panics if the module
// does not emit an LD.
func (ctx *genCtx) tool(modulePath string) (NodeRef, VFS) {
	res := ctx.toolResult(modulePath)
	return res.LDRef, *res.LDPath
}

// toolResult returns the full moduleEmitResult of walking `modulePath`
// as a host-platform tool. Callers needing more than (LDRef, LDPath)
// use this; everyone else uses the slimmer `tool()`.
func (ctx *genCtx) toolResult(modulePath string) *moduleEmitResult {
	return genModule(ctx, NewToolInstance(ctx.host, modulePath))
}

// scannerFor returns the IncludeScanner for `instance`'s platform axis.
// Single dispatch point for the target-vs-host scanner choice.
func (ctx *genCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	return ctx.scannerForPlatform(instance.Platform)
}

// scannerForPlatform returns the scanner pinned to `p`. Callers needing
// to resolve includes against a DIFFERENT platform than their instance
// (e.g. JOIN_SRCS forcing target-arch search paths during a host walk)
// call this overload directly.
func (ctx *genCtx) scannerForPlatform(p *Platform) *IncludeScanner {
	if p == ctx.host {
		return ctx.scannerHost
	}
	return ctx.scannerTarget
}
