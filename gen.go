package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver. Walks PEERDIR DFS, post-order, declaration-order (R14 link
// order), keyed on `ModuleInstance`.
//
// Macros understood by the walker:
//
//   - `IF (cond) ... [ELSE ...] ENDIF` — evaluated via macros.go's
//     EvalCond against a per-instance env (target/host platform + ARCH /
//     MUSL flags). Unreached branches contribute nothing.
//   - `NO_LIBC` / `NO_UTIL` / `NO_RUNTIME` / `NO_PLATFORM` /
//     `NO_COMPILER_WARNINGS` — override `inferFlagsFromPath` heuristic.
//   - `ADDINCL([GLOBAL] ...)`, `CFLAGS([GLOBAL] ...)`,
//     `CXXFLAGS([GLOBAL] ...)`, `CONLYFLAGS(...)`, `LDFLAGS(...)`,
//     `SRCDIR(dir)` — per-module; threaded into EmitCC via ModuleCCInputs.
//   - `JOIN_SRCS(name srcs...)` — JS node + CC compiling the joined output.
//   - `GLOBAL_SRCS(srcs...)` — separate CC nodes feeding `<lib>.global.a`.
//   - `INCLUDE(path)` — inlined by the parser; walker never sees an IncludeStmt.
//
// Source dispatch by extension: `.c/.cpp/.cc/.cxx` → EmitCC; `.h/.hpp`
// silently skipped; `.S/.s` → EmitAS (host yasm LD threaded for asmlib
// PIC); `.rl6` → EmitR6 (recurses into `contrib/tools/ragel6` for host
// ragel6 LD) then EmitCC of the generated `.cpp`.

// moduleEmitResult is the per-instance "what did we emit?" record
// kept by `genCtx.memo`. ARRef/LDRef split:
//
//   - LIBRARY modules populate ARRef (the .a archive); LDRef/LDPath
//     alias to ARRef/ARPath so a PROGRAM peering this LIBRARY wires
//     it via the AR fields.
//   - PROGRAM modules populate LDRef; ARRef/ARPath alias defensively
//     but no LIBRARY peers a PROGRAM (consumer never reads ARRef).
//
// `isPROGRAM` flags shape for the caller (Gen).
//
// `headerOnly` distinguishes header-only LIBRARY modules with no
// compilable sources (e.g. `library/cpp/sanitizer/include`). They are
// walked for transitive PEERDIR discovery but emit no AR/LD/Global;
// callers must skip archive-dep wiring rather than read a zero NodeRef.
type moduleEmitResult struct {
	ARRef      NodeRef
	ARPath     string
	isPROGRAM  bool
	headerOnly bool
	// hasPlainAR is true when EmitAR(Named) was called — i.e. at least
	// one regular (non-global) CC output. False for modules whose only
	// compilable sources are GLOBAL_SRCS (blockcodecs codecs, getopt):
	// these emit only `.global.a` and the consumer's peerLibPaths must
	// not include the plain `.a` path.
	hasPlainAR bool
	LDRef      NodeRef
	LDPath     string
	GlobalRef  *NodeRef // non-nil when the module has GLOBAL_SRCS (EmitARGlobal was called)
	GlobalPath string   // BUILD_ROOT-relative path to the .global.a archive; empty when GlobalRef is nil
	// AddInclGlobal is this module's own GLOBAL ADDINCL UNION the
	// transitive peer-GLOBAL ADDINCL across every PEERDIR. Consumers use
	// the set for (a) cmd_args -I emission (peer slot after module's own
	// ADDINCL) and (b) include-scanner resolution. SOURCE_ROOT-relative.
	AddInclGlobal []string
	// OwnAddInclGlobal is this module's OWN GLOBAL ADDINCL only, no
	// transitive peers. The consumer walker composes peerAddInclGlobal in
	// two phases (own-first across all peers, transitive second) so that
	// libcxx/include + libcxxrt/include precede musl-arch (the latter
	// propagates transitively through libcxx's auto-PEERDIR of musl/include).
	OwnAddInclGlobal []string
	// CFlagsGlobal / CXXFlagsGlobal / COnlyFlagsGlobal: own GLOBAL UNION
	// transitive peer-GLOBAL on each axis. Consumers receive via
	// ModuleCCInputs.Peer*FlagsGlobal. Declaration-order preserved across
	// PEERDIR walk; duplicates dropped (mirror of AddInclGlobal).
	CFlagsGlobal     []string
	CXXFlagsGlobal   []string
	COnlyFlagsGlobal []string
	// PeerArchiveClosureRefs / PeerArchiveClosurePaths: transitive archive
	// closure exposed to consumers — every peer's own AR UNION every
	// peer's PeerArchiveClosure*, deduplicated in DFS post-order (first
	// occurrence wins). Flows through LIBRARY moduleEmitResult so any
	// consumer (LIBRARY or PROGRAM) can union peers' closures with peers'
	// own archives for the full link-time archive set. Header-only
	// LIBRARYs propagate closures but contribute no archive themselves.
	PeerArchiveClosureRefs  []NodeRef
	PeerArchiveClosurePaths []string
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
	PeerGlobalClosurePaths []string
	// LDPluginRefs / LDPluginPaths: transitive set of LD plugin CP nodes a
	// consumer PROGRAM wires into its `--start-plugins ... --end-plugins`
	// block. The only M2-closure case is `contrib/libs/musl/include`'s
	// `LD_PLUGIN(musl.py)`, becoming
	// `$(B)/contrib/libs/musl/include/musl.py.pyplugin`, reaching archiver
	// / ragel6 / yasm through their PEERDIR walk through musl/include.
	// Aggregation mirrors archive closure (peer's own ∪ PeerLDPluginPaths,
	// dedup by path, first-occurrence wins). Header-only LIBRARYs emit
	// their own CP node AND propagate it; non-PROGRAM consumers carry
	// through but never consume.
	LDPluginRefs  []NodeRef
	LDPluginPaths []string
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

// genCtx threads state through the recursive walk. `emit` accumulates
// emitted nodes; `memo` deduplicates per-instance emission; `walking` is
// the cycle-detection stack. Both maps are keyed on `ModuleInstance`.
//
// `cyclesTolerated` counts back-edges suppressed by the headerOnly stub
// path. Tests assert known cycles fire exactly once.
//
// Host-tool walks fire eagerly from inside `emitOneSource`: `.rl6`
// recurses into ragel6/bin, `.S`/`.s` in a yasm-using host module
// recurses into yasm. The resulting host LD ref + output path wire into
// the per-source emitter (R6 cmd_args[0], AS foreign_deps.tool).
// `genModule`'s memo prevents re-walking the same host instance.
type genCtx struct {
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
	// ldPluginCPCache deduplicates LD_PLUGIN CP NodeRefs across the
	// target/host walk pair. Without dedup, `contrib/libs/musl/include`'s
	// `musl.py` would yield two CP nodes (one per platform). REF emits
	// the CP node ONCE on the target platform and reuses its UID in both
	// target and host LDs. Keying by plugin output path
	// (`$(B)/<modulePath>/<name>.pyplugin`) suffices: path is
	// platform-independent. First-write wins — the target walk precedes
	// any host walk recursion, so the cached entry carries the target
	// platform per REF.
	ldPluginCPCache map[string]NodeRef

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
	// Debug counters (printed when YATOOL_SCANCTX_STATS=1). scanCtxAllocs
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

// resolveCodegenDepRefs scans `includeInputs` for $(B)-rooted paths that
// match a previously emitted EN/PB/EV/AR/CF/BI/JV/PR/R5/PY producer
// output, returning the producer NodeRefs deduped in scan order. Each
// consumer CC carries these as ExtraDepRefs so the resulting `deps` list
// mirrors REF (every CC whose inputs[] references a $(B)/<gen>.h or
// <gen>.cc carries an explicit codegen-producer dep).
//
// `consumer.Platform.Target` disambiguates per-platform PB/EV lookup. EN
// always emits on the target axis so ctx.enOutputs is consulted by path
// alone.
//
// `exclude` suppresses NodeRefs from the result (typically the downstream
// CC's `Generator` ref, which EmitCC already threads into DepRefs as the
// leading entry — without filtering, a CC compiling its own producer's
// .cc would carry the producer twice).
//
// In addition to the legacy enOutputs/pbOutputs/evOutputs maps, the
// function consults the per-scanner CodegenRegistry for any entry whose
// HasProducerRef is set — the general path covering AR/CF/BI/JV/PR/R5/PY.
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

	sharedPC := newSharedParseCache()

	targetReg := NewCodegenRegistry()
	hostReg := NewCodegenRegistry()

	targetScanner := newIncludeScannerWith(srcRoot, LoadSysInclSetFor(srcRoot, string(targetP.ISA), onWarn), sharedPC, onWarn)
	targetScanner.codegen = targetReg
	targetScanner.fallbackLocators = []pathLocator{codegenLocator{reg: targetReg}}
	hostScanner := newIncludeScannerWith(srcRoot, LoadSysInclSetFor(srcRoot, string(hostP.ISA), onWarn), sharedPC, onWarn)
	hostScanner.codegen = hostReg
	hostScanner.fallbackLocators = []pathLocator{codegenLocator{reg: hostReg}}

	ctx := &genCtx{
		sourceRoot:      srcRoot,
		emit:            emitter,
		memo:            make(map[ModuleInstance]*moduleEmitResult),
		walking:         make(map[ModuleInstance]bool),
		host:            hostP,
		target:          targetP,
		scannerTarget:   targetScanner,
		scannerHost:     hostScanner,
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

	return root.LDRef
}

// GenWithMode runs Gen against an explicit (host, target) Platform pair
// with the chosen scanCtxMode (`local` or `interned`). Callers (`yatool
// gen`, `yatool make -G`, test helpers) construct both Platforms from
// CLI flags + mining; the walker reads every flag, tool path, and tag
// off the Platform pointers. `onWarn` receives one line per diagnostic.
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
// `*moduleEmitResult`. Memoised: a second call returns the cached result.
//
// Algorithm:
//
//  1. Memo hit → return.
//  2. Cycle check: instance on the walking stack → throw (tolerated by
//     headerOnly stub for the runtime-stack DEFAULT_PEERDIR cases).
//  3. Parse `<sourceRoot>/<instance.Path>/ya.make`.
//  4. `collectModule` resolves IF branches, collects SRCS / PEERDIR /
//     JOIN_SRCS / GLOBAL_SRCS / NO_* / ADDINCL / CFLAGS / SRCDIR; macro
//     NO_* flags override the path-based seed FlagSet.
//  5. Validate exactly one module declaration; non-empty sources.
//  6. Recurse into each PEERDIR in declaration order (post-order).
//  7. Per-source dispatch by extension (EmitCC / EmitAS / EmitR6 / ...);
//     headers silently skipped.
//  8. JOIN_SRCS: EmitJS + EmitCC against the joined output.
//  9. LIBRARY: EmitAR over own CCs (+EmitARGlobal when GLOBAL_SRCS).
//     PROGRAM: EmitLD over own CCs and peer archives.
//
// 10. Memoise and return.
func genModule(ctx *genCtx, instance ModuleInstance) *moduleEmitResult {
	// Capture the seed key BEFORE the `instance.Flags = d.flags` overlay
	// below rebinds Flags to the macro-derived FlagSet. Callers pass the
	// seed FlagSet from `derivePeerInstance`/`inferFlagsFromPath`, which
	// lacks the post-parse NO_PLATFORM / NO_COMPILER_WARNINGS / NO_UTIL /
	// NO_RUNTIME / NO_LIBC bits applied by `collectModule`. Memo writes
	// run AFTER the overlay; without an alias under the seed key the
	// top-of-function lookup misses every consumer call and the body
	// re-executes 7-138 times per module. Memo writes below store under
	// both keys.
	originalInstance := instance

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
	// Returning a `headerOnly` stub suffices: the peer's own walk
	// completes elsewhere on the stack, and consumers skip the empty
	// archive-ref instead of pinning a zero NodeRef. REF emits no
	// peer-archive deps in AR anyway (every LIBRARY's AR has only its
	// own .o files), so the loss is below the L1 comparator surface.
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

	// Upstream ymake.core.conf has `when ($MUSL_LITE == "yes") { NO_UTIL() }`.
	// Apply the implication: MUSL_LITE=yes → NoUtil=true. Prevents yasm
	// (ENABLE(MUSL_LITE)) from receiving util as a default peer.
	if d.muslLite {
		instance.Flags.NoUtil = true
	}

	// _BASE_PY3_PROGRAM (build/conf/python.conf:877-883) applies an
	// implicit `ALLOCATOR($_MY_ALLOCATOR)`; the otherwise-branch
	// (non-ARCH_PPC64LE) sets _MY_ALLOCATOR=J. Linux-x86_64/aarch64 takes
	// this branch, so PY3_PROGRAM_BIN modules without explicit ALLOCATOR
	// inherit jemalloc rather than the plain-PROGRAM TCMALLOC_TC default.
	if !d.hadAllocator && d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
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

	// GENERATE_ENUM_SERIALIZATION* injects an implicit PEERDIR to
	// tools/enum_parser/enum_serialization_runtime (upstream
	// `_GENERATE_ENUM_SERIALIZATION_BASE` in build/ymake.core.conf). The
	// runtime carries dispatch_methods.cpp / enum_runtime.cpp /
	// ordered_pairs.cpp that the generated _serialized.cpp links against.
	if len(d.enumSrcs) > 0 && instance.Path != "tools/enum_parser/enum_serialization_runtime" {
		d.peerdirs = append(d.peerdirs, "tools/enum_parser/enum_serialization_runtime")
	}

	// Header-only LIBRARYs (e.g. library/cpp/sanitizer/include) have no
	// compilable sources but a valid module declaration; REF emits no AR
	// for them. Walk peers so their transitive closure is discovered,
	// then return `headerOnly: true`; callers skip archive-dep wiring.
	// PROGRAMs with zero compilable sources are a hard error (except
	// PROGRAMs whose only sources are deferred kinds — treated as
	// header-only stubs).
	//
	// Multimodule library types (PROTO_LIBRARY etc.) always take the
	// header-only path; their .proto/.ev sources are emitted by
	// emitProtoSrcs below.
	//
	// PY3_PROGRAM_BIN is excluded: it has no C++ sources but is a PROGRAM
	// and needs the full PROGRAM walk + EmitLD dispatch.
	// PY3_LIBRARY etc. with compilable C++ sources take the LIBRARY path;
	// without sources they reach this header-only path via
	// !hasCompilableSource.
	if (!hasCompilableSource(d) && d.moduleStmt.Name != "PY3_PROGRAM_BIN") || isMultimoduleLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == "PROGRAM" && !hasSkippedSource(d) {
			ThrowFmt("gen: %s has no compilable sources (after IF/header filter)", instance.Path)
		}

		// Header-only LIBRARYs may declare ADDINCL(GLOBAL ...) /
		// {C,CXX,CONLY}FLAGS(GLOBAL ...) that peer-propagate without
		// emitting an AR. Walk peers (so their transitive closures reach
		// genModule) and aggregate own + peer GLOBAL per axis.
		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)

		// Emit own LD_PLUGIN CP nodes (e.g. musl.py → musl.py.pyplugin)
		// BEFORE composing the result so the refs propagate alongside the
		// peer-walked plugin closure. The CP node carries
		// `module_dir = instance.Path` per REF; src/dst anchor under it.
		ownLDPluginRefs, ownLDPluginPaths := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
		ldPluginRefs, ldPluginPaths := mergeLDPlugins(ownLDPluginRefs, ownLDPluginPaths, peerContribs.ldPluginRefs, peerContribs.ldPluginPaths)

		// Emit yapyc3 PY nodes for PY_SRCS() declarations. PY3_LIBRARY /
		// PY23_LIBRARY often have only PY_SRCS (no C/C++ sources) and
		// reach this branch; their Python sources still need PY emission.
		emitPySrcs(ctx, instance, d)

		// Emit objcopy PY nodes for RESOURCE / RESOURCE_FILES. Header-only
		// LIBRARYs (e.g. certs, PY3_LIBRARY-only-PY_SRCS) host the
		// only-resource shape; when there are objcopy outputs, emit a
		// .global.a archiving them.
		objcopyRefs, objcopyOutputs, objcopyGlobalInputs := emitResourceObjcopy(ctx, instance, d)

		// Capture the header-only `.global.a` ref so consumers see it via
		// `moduleEmitResult.GlobalRef/GlobalPath`. Otherwise RESOURCE-only
		// LIBRARY (`certs`) and PY3_LIBRARY PY_SRCS modules' `.global.a`
		// archives reach the graph but are orphaned from every LD inputs.
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
			gRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRefs, objcopyOutputs, objcopyGlobalInputs, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = instance.Path + "/" + globalBaseName
		}

		// Emit EN nodes for GENERATE_ENUM_SERIALIZATION(*). Header-only
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
		// module stays `headerOnly: true` (its AR's members are
		// protoc-generated CCs, none of its own C/C++); `hasPlainAR=true`
		// lets consumers fold the AR into `peerArchiveRefs` without
		// reintroducing the AR-on-AR dependency LIBRARY ARs drop.
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
			headerOnly:       true,
			isPyLibrary:      isPyLibraryType(d.moduleStmt.Name),
			hasPlainAR:       hOnlyHasAR,
			ARRef:            hOnlyARRef,
			ARPath:           hOnlyARPath,
			GlobalRef:        hOnlyGlobalRef,
			GlobalPath:       hOnlyGlobalPath,
			AddInclGlobal:    mergeDedup(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal: append([]string(nil), d.addInclGlobal...),
			// peer-transitive first, own last per upstream
			// `TGlobalVarsCollector` semantics. ADDINCL keeps the opposite
			// (own first, peer second) per `TModuleIncDirs::Get()`.
			CFlagsGlobal:            mergeDedup(peerContribs.cFlags, d.cFlagsGlobal),
			CXXFlagsGlobal:          mergeDedup(peerContribs.cxxFlags, d.cxxFlagsGlobal),
			COnlyFlagsGlobal:        mergeDedup(peerContribs.cOnlyFlags, d.cOnlyFlagsGlobal),
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

	// PROGRAM-only implicit peerdirs. `_BASE_PROGRAM` adds musl/full
	// (when MUSL=yes && !MUSL_LITE) and the default ALLOCATOR's peer set
	// (TCMALLOC_TC for our environment) on top of language defaults.
	// `hadAllocator` suppresses the allocator-default when the PROGRAM
	// declared `ALLOCATOR(NAME)` itself.
	//
	// Split into language-defaults and program-defaults so peer-GLOBAL
	// aggregation can apply different orderings per group (language two-
	// phase, program single-phase). Program-defaults are further split
	// into pre-user (cow/on + optional tcmalloc) and post-user (musl/full
	// or musl). Explicit ALLOCATOR peers and d.peerdirs interleave
	// between the halves so they appear before musl/full in archive
	// accumulation (correct LD link order for the mimalloc cluster)
	// while retaining peerKindUserPeer (correct AddInclGlobal Phase 3
	// ordering for the ragel6 CC include case).
	languageDefaultsCount := len(defaults)

	isProgram := (d.moduleStmt.Name == "PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN") && !isRuntimeAncestor(instance.Path)

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
	// right ordering rule per group:
	//   - language-defaults: two-phase (own first, then transitive) —
	//     preserves libcxx/libcxxrt OWN ahead of the musl-arch
	//     transitive chain (archiver invariant).
	//   - user-peers: single-phase AddInclGlobal in declaration order —
	//     places an ALLOCATOR-derived peer's transitive GLOBAL ahead of
	//     a later user PEERDIR's OWN GLOBAL (ragel6 mimalloc/include vs
	//     ragel5/aapl invariant).
	//   - program-defaults: single-phase AddInclGlobal — implicit
	//     TCMALLOC_TC peer-set's OWN GLOBAL falls after util's
	//     transitive zlib/double-conversion/libc_compat (archiver
	//     default-allocator invariant).
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
	peerArchivePaths := make([]string, 0, len(allPeers))
	peerGlobalRefs := make([]NodeRef, 0, len(allPeers))
	peerGlobalPaths := make([]string, 0, len(allPeers))

	// Dedup table for the transitive `.global.a` closure. Each direct
	// peer contributes its own `.global.a` (when GlobalRef != nil) AND
	// every entry of its PeerGlobalClosure*. First occurrence wins; the
	// closure flows up through `moduleEmitResult.PeerGlobalClosure*` so
	// PROGRAM LDs at any depth reach every transitively-reachable
	// `.global.a`.
	peerGlobalSeen := map[string]struct{}{}
	peerGlobalAddPath := func(ref NodeRef, path string) {
		if _, dup := peerGlobalSeen[path]; dup {
			return
		}

		peerGlobalSeen[path] = struct{}{}
		peerGlobalRefs = append(peerGlobalRefs, ref)
		peerGlobalPaths = append(peerGlobalPaths, path)
	}

	// Dedup table for the transitive peer-archive closure. For each
	// direct peer, accumulate (peer's own AR ∪ peer's PeerArchiveClosure)
	// — first occurrence wins (R14). Consumed only by the PROGRAM branch
	// below (LIBRARYs drop peer-archive refs from their AR); LIBRARY
	// consumers downstream walk our exposed
	// `PeerArchiveClosureRefs/Paths` and fold using the same discipline.
	peerArchiveSeen := map[string]struct{}{}
	peerArchiveAddPath := func(ref NodeRef, path string) {
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

	// Aggregate peer-GLOBAL across all four axes (ADDINCL / CFLAGS /
	// CXXFLAGS / CONLYFLAGS) with TWO-PHASE traversal:
	//
	//   Phase 1 — each peer's OWN GLOBAL declarations (declaration order).
	//   Phase 2 — each peer's TRANSITIVE peer-GLOBAL contributions
	//             (everything except its own), in declaration order.
	//
	// Empirical: tools/archiver/main.cpp.o cmd_args[11..16] in sg.json
	// has libcxx-include + libcxxrt-include (OWN GLOBAL) BEFORE musl-arch
	// (transitive via libcxx's auto-PEERDIR of musl/include).
	// Single-phase DFS-completion would put musl-arch first (builtins is
	// walked before libcxx and already has musl-arch via its
	// musl/include peer); two-phase puts the OWN libcxx/libcxxrt first.
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
		if kind != peerKindUserPeer && !peerYaMakeExists(ctx.sourceRoot, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(instance, peerPath)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.isPROGRAM {
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})

		// Fold peer's LD plugin closure (own ∪ transitive) into ours.
		// Runs for header-only and non-header peers alike — the only M2
		// plugin (musl.py.pyplugin) is owned by the header-only
		// `contrib/libs/musl/include` LIBRARY.
		for i, p := range peerResult.LDPluginPaths {
			peerLDPluginAddPath(peerResult.LDPluginRefs[i], p)
		}
	}

	// PASS B: archive + .global.a aggregation. Iterates `resolved` in an
	// archive-specific order so PROGRAM-class consumers (ymake/bin,
	// py3cc/slow/bin) defer USE_PYTHON3's implicit peers (contrib/libs/
	// python, library/python/runtime_py3) to AFTER user-declared
	// PEERDIRs. The AddInclGlobal aggregation below continues in source
	// order — source-order is empirically correct for include path slots;
	// deferral there regressed 142 nodes.
	//
	// Upstream's `macro USE_PYTHON3() { PEERDIR(contrib/libs/python);
	// when (...) { PEERDIR+=runtime_py3 } }` (python.conf:1063-1071)
	// effectively appends both peers to the tail of the module's peer
	// list (the `when` block is deferred; a plain PEERDIR inside a macro
	// body behaves the same way upstream). For PY*_PROGRAM consumers
	// (py3cc/slow/bin) REF places runtime_py3 AFTER user-
	// PEERDIR(runtime_py3/main).
	//
	// Scoped to PROGRAM/PY*_PROGRAM only — LIBRARY-class consumers
	// depend on the existing peer order for PeerArchiveClosurePaths
	// propagation (LIBRARY-scope deferral regressed 100+ L3 nodes).
	archiveOrder := resolved
	if d.usePython3 && d.moduleStmt != nil {
		// Tail-defer USE_PYTHON3 implicit peers ONLY for PY*_PROGRAM*.
		// For plain PROGRAM modules with USE_PYTHON3 (devtools/ymake/bin),
		// upstream's macro prepends these peers BEFORE the user PEERDIR
		// block, so the python closure must land FIRST — not deferred.
		// Tail reorder remains required for PY3_PROGRAM_BIN / PY*_PROGRAM
		// where user-PEERDIR(runtime_py3) intentionally dedups against
		// the implicit macro injection.
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
		if peerResult.GlobalRef != nil {
			peerGlobalAddPath(*peerResult.GlobalRef, peerResult.GlobalPath)
		}

		// peerResult.ARPath carries the py3-prefixed name for Python
		// modules — use it instead of recomputing ArchiveName. Skip when
		// hasPlainAR is false (only-GLOBAL_SRCS module, no regular .a).
		// PROTO_LIBRARY emits a regular `.a` from the header-only branch
		// (members are protoc-generated CCs); `hasPlainAR` is set on its
		// result so the AR flows through here regardless of `headerOnly`.
		if peerResult.hasPlainAR {
			// ARPath has "$(B)/" prefix; cmd_args use a
			// bare relative path. Strip the prefix for consistency.
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(B)/")
			peerArchiveAddPath(peerResult.ARRef, arRelPath)
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
	// peer-GLOBAL ADDINCL slice so they slot immediately after the
	// linux-headers ccIncludesSuffix in composeTargetCC / composeHostCC.
	//
	// Fires only when THIS module is itself a runtime ancestor (`util`,
	// `library/cpp/malloc/api`, ...). Upstream propagates libcxx/libcxxrt
	// header search paths to runtime-ancestor consumers as if they were
	// direct GLOBAL peers, even though `defaultPeerdirsFor` returns only
	// `[contrib/libs/musl/include]` for them. Without the hoist, util's
	// own CC nodes get libcxx/libcxxrt at the tail of peerAddInclGlobal
	// via Phase 2 transitive walk through util's user PEERDIRs.
	//
	// Non-runtime-ancestor consumers do NOT get the hoist:
	//   - Modules with no NO_RUNTIME (tools/archiver, util/charset,
	//     ragel6/bin) see libcxx/libcxxrt as direct defaults via Phase 1.
	//   - Modules with NO_RUNTIME (yasm) intentionally pick up
	//     libcxx/libcxxrt at the TAIL via musl_extra / jemalloc
	//     transitive walks (REF: yasm libyasm/assocdat.c.pic.o has
	//     libcxx/libcxxrt at slots 17-18, AFTER musl-arch). Unconditional
	//     hoist would regress this case.
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
	}

	// Effective AddInclGlobal = own GLOBAL ADDINCL ∪ every peer's
	// transitive. Stored on the result so transitive consumers see the
	// closure in one shot.
	effectiveAddInclGlobal := mergeDedup(d.addInclGlobal, peerAddInclGlobal)

	// `library/python/runtime_py3` propagates `$(B)/library/python/
	// runtime_py3` to consumers via its `effectiveAddInclGlobal`, but at
	// a position AFTER `contrib/restricted/abseil-cpp` (NOT at the head
	// as a regular own-GLOBAL would). Upstream `_PYTHON3_ADDINCL` runs at
	// module-eval time (adds `python/Include`); then PEERDIR processing
	// propagates abseil-cpp into runtime_py3's UserGlobalPropagated via
	// `library/cpp/resource → absl_flat_hash`; the `ARCHIVE` macro fires
	// later and adds the BUILD_ROOT path via the `addincl` modifier,
	// landing AFTER abseil-cpp. Splice to match REF ordering on 145
	// consumer nodes (devtools/ymake LIBRARY preeval.cpp.o slots 21-23
	// `[python/Include, abseil-cpp, BUILD_ROOT/runtime_py3]`).
	//
	// Fallback: when abseil-cpp is absent from the merged set, append at
	// the tail — preserves the path so consumers can resolve
	// runtime_py3's generated headers.
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

	// Peer-transitive CFLAGS GLOBAL precede this module's OWN CFLAGS
	// GLOBAL in the effective propagated slice. Upstream rule
	// (devtools/ymake/global_vars_collector.cpp):
	// `TGlobalVarsCollector::Collect` (json_visitor.cpp:558) runs at
	// DFS-Left for every direct peerdir edge, accumulating each peer's
	// USER_CFLAGS with `AppendUnique` (first-occurrence-wins); `Finish`
	// runs at PrepareLeaving and only then pushes the module's OWN
	// CFLAGS. ADDINCL follows the opposite rule via
	// `TModuleIncDirs::Get()` returning `LocalUserGlobal` (own) before
	// `UserGlobalPropagated` (peer), so `effectiveAddInclGlobal` keeps
	// `(own, peer)` order above.
	//
	// Empirical anchor: devtools/ymake/_/commands/compilation.cpp.o
	// (aarch64) cmd_args[68..70]: REF has [OPENSSL_RENAME_SYMBOLS=1,
	// ASIO_STANDALONE, ASIO_SEPARATE_COMPILATION]. ASIO is a direct
	// PEERDIR of devtools/ymake and PEERDIRs OpenSSL; per-peer
	// AppendUnique places asio's OpenSSL transitive ahead of asio's
	// own ASIO_*.
	effectiveCFlagsGlobal := mergeDedup(peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := mergeDedup(peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := mergeDedup(peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)

	// Inject libcxx's GLOBAL ADDINCL + GLOBAL CXXFLAGS into runtime-
	// ancestor C++ consumers' OWN CC emission only — not into the
	// `effective*` propagation slices already snapshotted above.
	//
	// Local-only because making libcxx an implicit DEFAULT peer would
	// also push libcxx/include + libcxxrt/include into
	// `effectiveAddInclGlobal`, which every downstream consumer's Phase 2
	// walk reads — producing spurious -I flags on unrelated CC nodes
	// (zlib, mimalloc, libcxxabi-parts, etc.) for a 100+-node L3
	// regression.
	//
	// Mutating `peerAddInclGlobal` and `peerCXXFlagsGlobal` AFTER the
	// `effective*` snapshot keeps the propagated view clean. The local
	// view (consumed by `ModuleCCInputs` for THIS module's own CC
	// compile) gains the slots; the runtime-stack hoist below re-runs
	// on the post-injection slice so the injected libcxx/include +
	// libcxxrt/include land immediately after the linux-headers
	// ccIncludesSuffix.
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

	// Track "primary sources" of regular SRCS / JOIN_SRCS / .rl6 dispatch
	// — distinct from header closures. REF treats the splits asymmetrically:
	//
	//   - regular AR (`.a`) inputs: regular primaries + global primaries
	//     + everyone's header/closure;
	//   - global AR (`.global.a`) inputs: global primaries + everyone's
	//     header/closure (NO regular primaries).
	//
	// Empirical: contrib/libs/tcmalloc/no_percpu_cache regular `.a`
	// archives `aligned_alloc.c` AND every `tcmalloc/*` global source +
	// 1311 shared headers; `.global.a` archives every `tcmalloc/*`
	// source + the same headers, but NOT `aligned_alloc.c`.
	//
	// The narrowed `.global.a` uses `globalMemberInputs` directly (the
	// GLOBAL_SRCS-local closure) and never sees regular primaries; the
	// set is retained so call sites don't have to be untangled.
	regularPrimariesSet := map[VFS]struct{}{}
	addRegularPrimary := func(p VFS) {
		regularPrimariesSet[p] = struct{}{}
	}

	// Auto-injected peer-CFLAG -D_musl_ for every TARGET module that is
	// not effectively NO_PLATFORM, when the CLI says MUSL=yes. Mirrors
	// `_BASE_UNIT`'s `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`.
	// Suppressed for no-stdinc modules — those receive
	// `-D_musl_=1` from `muslExtraDefines` and upstream NO_PLATFORM
	// gates off the extra `-D_musl_`.
	autoPeerCFlags := defaultPeerCFlags(ctx, instance, d)

	// Thread the module's own non-GLOBAL CFLAGS and own GLOBAL
	// CFLAGS / CXXFLAGS / CONLYFLAGS into ModuleCCInputs so the composer
	// emits them on this module's own CC compiles. NoStdInc modules
	// zero them: musl's CFLAGS are folded into `muslExtraDefines` and
	// the musl composers ignore these slots.
	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.NoStdInc {
		ownCFlags = nil
		ownCFlagsGlobalSelf = nil
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
	dedupedAddIncl := mergeDedup(d.addIncl, nil)

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

	// Drop BUILD_ROOT-rooted addincl paths from the peer slot when the
	// same path is in this module's own addincl. Generated-output paths
	// (`$(B)/<mod>`) are produced by THIS module's ARCHIVE() / RUN_PROGRAM
	// and arrive at peer consumers via the PEERDIR walk; the self-compile
	// must not also emit them in the peer slot. SOURCE_ROOT paths (e.g.
	// `python/Include`) are NOT filtered — REF deliberately emits the
	// own + peer duplicate (sitecustomize.cpp.pic.o ref:8+26,
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

	// Ancestor-only SRCDIR rebase. The "PROGRAM with SRCDIR pointing at
	// an ancestor of instance.Path" pattern (typified by
	// `contrib/tools/ragel6/bin` whose SRCDIR is `contrib/tools/ragel6`)
	// is the only shape where REF rebases module_dir to SRCDIR. LIBRARYs
	// with SRCDIR keep module_dir at instance.Path and route per-source
	// via composeCCPaths' SRCDIR-aware composer.
	ancestorRebase := d.srcDir != "" && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(d.srcDir, instance.Path)

	// Emit EN nodes BEFORE the per-source CC loop so the codegen registry
	// is populated when consumer sources scan their include closures.
	// E.g. `stats.cpp`/`trace.cpp` in `devtools/ymake/diag`
	// `#include <devtools/ymake/diag/stats_enums.h_serialized.h>`; the
	// scanner consults `IncludeScanner.codegen` populated by
	// `emitEnumSrcs`. If EN ran AFTER the source loop, the registry would
	// be empty at scan time and resolveCache / subgraphCache would lock
	// in a "not found" miss. EN node emission order in the output graph
	// does not affect L4 byte-exactness (normalizer re-sorts by UID).
	//
	// Passing `moduleInputs` causes `emitEnumSrcs` to also emit the
	// downstream CC for each EN's `_serialized.cpp`. The returned
	// `(refs, outputs, memberInputs)` are folded into the AR member
	// buckets below (alongside PR-downstream CCs) so the regular `.a`
	// archives the EN-derived `.o`s after declared SRCS.
	enCCRefs, enCCOutputs, enCCMemberInputs := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

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
	prCCRefs, prCCOutputs, prMemberInputsList := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	emitArchives(ctx, instance, d)

	// Two-pass source emission. Codegen-producing sources
	// (.ev/.proto/.rl6/.rl/.cpp.in/.c.in) emit nodes whose outputs
	// (`.ev.pb.h`, `.rl6.cpp`, `*.cpp`, …) consumer CCs in the same
	// module may #include. Processing in d.srcs order would have a
	// consumer .cpp preceding a codegen producer scan its closure against
	// an unpopulated registry; resolveCache/subgraphCache would lock in
	// a "not found" miss surviving the producer's later registration
	// (witnessed on devtools/ymake/diag: display.cpp idx 3, trace.ev
	// idx 4; display.cpp's scan of trace.h → trace.ev.pb.h missed and
	// poisoned the trace.h subgraph for every subsequent consumer).
	//
	// Fix: emit codegen-producing sources FIRST (Pass A), then iterate
	// d.srcs in declaration order (Pass B) using Pass A's cached results
	// for codegen producers and emitOneSource fresh for the rest. AR
	// member order is preserved (Pass B appends to ccRefs in d.srcs
	// order), so AR.cmd_args stays byte-exact.
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
	for i, ref := range enCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, enCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(enCCMemberInputs[i])
	}

	// PR-downstream CC fold. emitRunProgramsForAR + emitArchives are
	// hoisted ahead of the SRCS loop so the codegen registry's PR/AR
	// ProducerRef entries are populated when consumer CCs (e.g.
	// library/python/runtime_py3/__res.cpp) scan their inputs[]. The
	// AR.cmd_args bucket ordering (PR-downstream CCs AFTER regular SRCS,
	// before JOIN_SRCS) is preserved by deferring the fold to this site.
	for i, ref := range prCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, prCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		addMemberInputs(prMemberInputsList[i])
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

	// Record the SRCS/JOIN_SRCS boundary so the AR cmd_args reorder below
	// applies the right bucket rules to each group independently. Entries
	// before this index are SRCS-derived (regular + .rl6); after, JOIN_SRCS.
	numSrcsDerived := len(ccOutputs)

	for _, js := range d.joinSrcs {
		// Rebase onto SRCDIR only when `ancestorRebase` is set (PROGRAM
		// with ancestor SRCDIR; the ragel6/bin pattern). Otherwise keep
		// srcInstance at instance.Path — JOIN_SRCS in
		// LIBRARY-with-sibling-SRCDIR modules (none in M2 closure today)
		// emit at the LIBRARY's own dir.
		srcInstance := instance

		if ancestorRebase {
			srcInstance.Path = d.srcDir
		}

		// Per-source include closure threaded into the JS node Inputs and
		// the JS-derived CC's IncludeInputs (mirror of REF: the joined
		// .cpp textually #includes each member, so its closure is the
		// union of member closures).
		//
		// JS nodes are anchored to the outer-target platform axis, so the
		// JS closure must resolve with the TARGET scanner and TARGET musl
		// arch search paths even when srcInstance is a host (PIC)
		// instance. The downstream CC node still compiles on the host
		// axis and needs the HOST closure. Compute them separately when
		// srcInstance.Flags.PIC — for the target case they are identical
		// so a single call suffices. TODO: remove the Flags.PIC guard
		// when a general target-vs-host axis parameter is plumbed through.
		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		if srcInstance.Platform.ISA == ISAX8664 {
			// When this module is reached through a host (x86_64) walk
			// the JS node nevertheless emits on the target axis (see the
			// EmitJS call below — Platform is anchored to the outer-target
			// ID). Recompute the include closure with the target scanner +
			// target-arch musl search paths rebased; the surrounding host
			// walk's instance is kept verbatim — only the override
			// `scanPlatform` argument flips.
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = jsTargetPeerAddIncl(moduleInputs.PeerAddInclGlobal, srcInstance.Platform.ISA, ctx.target.ISA)

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

	// Emit PY+CC pairs for each PY_REGISTER(arg). Both flow into
	// globalRefs/globalOutputs (upstream macro `_PY3_REGISTER` appends
	// `SRCS(GLOBAL $Func.reg3.cpp)` so the .o lands in `.global.a`).
	// PY3_LIBRARY (rapidjson, ymakeyaml) emits plain `.reg3.cpp.o`;
	// PY23_LIBRARY and PY23_NATIVE_LIBRARY emit `.reg3.cpp.py3.o` (REF:
	// library/python/symbols/module — PY23_LIBRARY multimodule whose py3
	// submodule tags CC outputs with module_tag=py3 and .py3.o suffix).
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

	// Emit own LD_PLUGIN CP nodes. No current M2 case fires here
	// (musl/include is header-only and handled above) but the emission
	// is symmetric so a future LIBRARY/PROGRAM declaring LD_PLUGIN inline
	// gets the same wiring. Merged with the transitive peer plugin
	// closure; feeds EmitLD's `--start-plugins ... --end-plugins` block
	// (PROGRAMs) and the LDPluginRefs/Paths slot on `moduleEmitResult`.
	ownLDPluginRefs, ownLDPluginPaths := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
	mergedLDPluginRefs, mergedLDPluginPaths := mergeLDPlugins(ownLDPluginRefs, ownLDPluginPaths, peerLDPluginRefs, peerLDPluginPaths)

	if d.moduleStmt.Name == "PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
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
		// default-peer set. yasm is the M2-closure case: yasm drops
		// malloc/api via `suppressMallocAPIDefault` above, but its peers
		// musl_extra and jemalloc each carry malloc/api in their own
		// defaults — re-introduced via the archive closure. Apply the
		// same suppression at the PROGRAM boundary.
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

		// PY3_PROGRAM_BIN emits module_lang="py3". Tag the instance at
		// the EmitLD call site only so Language does not propagate into
		// derivePeerInstance's peer walks (peers are C++ LIBRARY modules
		// and must stay Language=LangCPP to share memo entries with the
		// rest of the target/host closure).
		ldInstance := instance
		if d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			ldInstance.Language = LangPy
		}

		// PY3_PROGRAM_BIN must emit yapyc3 and objcopy nodes BEFORE
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

		// PY3_PROGRAM_BIN's upstream `_BASE_PY3_PROGRAM` macro
		// (build/conf/python.conf:884) calls STRIP(), setting
		// STRIP_FLAG=$LD_STRIP_FLAG=-Wl,--strip-all on Linux. PY3_PROGRAM
		// (cpp module_lang) does not exercise the strip path today.
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
			ctx.host,
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
	// intentionally NOT threaded — REF probe confirmed every reference
	// AR has zero AR-on-AR deps. Peer archives flow into the consumer's
	// downstream LD via the `peerArchiveRefs` slot in EmitLD below; the
	// LIBRARY AR drops them.
	//
	// The regular AR receives the union of regular and global members'
	// inputs (primaries + header closures). REF: tcmalloc/no_percpu_cache
	// `liblibs-tcmalloc-no_percpu_cache.a` archives `aligned_alloc.c`
	// (regular SRCS), every `tcmalloc/*` GLOBAL_SRCS source, and the
	// 1286 shared header closure. Without the union, the regular AR
	// would miss GLOBAL_SRCS' resolved closures.
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
	if d.arPlugin != "" {
		v := Source(instance.Path + "/" + d.arPlugin)
		arPluginVFS = &v
	}

	if len(ccRefs) > 0 {
		// PY23_LIBRARY / PY23_NATIVE_LIBRARY surface
		// `module_tag=py3` / `module_tag=py3_native`. openssl AR_PLUGIN(ar)
		// injects `--plugin <ar.pyplugin>` between the link_lib.py `--`
		// separators.
		if perModuleCCTag != "" {
			arRef = EmitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.host, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, combinedMemberInputs, arPluginVFS, ctx.host, ctx.emit)
		}
	}

	_ = peerArchiveRefs // retained as a loop accumulator for the PROGRAM LD branch above; intentionally unused for the LIBRARY AR.
	arPath := Build(instance.Path + "/" + arBaseName).String()

	// Emit yapyc3 PY nodes for PY_SRCS(). Modules with both SRCS and
	// PY_SRCS (rare but valid) get CC/AR nodes from the SRCS path above
	// AND yapyc3 nodes here.
	emitPySrcs(ctx, instance, d)

	// Emit objcopy PY nodes for RESOURCE / RESOURCE_FILES. Returned `.o`
	// paths flow into the module's `.global.a` (appended into
	// globalRefs/globalOutputs below). The objcopy nodes' SOURCE_ROOT
	// inputs (per-entry source paths + objcopy.py) are folded into the
	// GLOBAL_SRCS-local closure feeding `.global.a`'s `inputs` slot;
	// dedup against the existing accumulator.
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

	// EN and JV/CF/BI/PR emissions are hoisted to the pre-source-loop
	// site above so the codegen registry is populated before consumer
	// CCs scan their inputs.

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
		result.GlobalPath = instance.Path + "/" + globalBaseName
	}

	ctx.memo[originalInstance] = result
	ctx.memo[instance] = result

	return result
}

// mergeDedup returns `a ++ b` deduped, preserving declaration order
// (R14 — first occurrence wins). Used by genModule to compose this
// module's effective peer-GLOBAL slices (own first, then transitive
// peer) uniformly across ADDINCL / CFLAGS / CXXFLAGS / CONLYFLAGS.
//
// filterBuildRootSelfPaths drops `$(B)/...` paths from `peer` that also
// appear in `own`. Returns a fresh slice (input unchanged) so unfiltered
// `peerAddInclGlobal` continues to flow into peer-prop channels.
// Applied at the SELF-compile cmd_args boundary only. SOURCE_ROOT paths
// (e.g. `python/Include`) are left alone — REF emits the own + peer
// duplicate for those.
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

// emitOwnLDPlugins emits one CP node per `LD_PLUGIN(name.py)` entry.
// CP src = `$(S)/<modulePath>/<name>`, dst =
// `$(B)/<modulePath>/<name>.pyplugin` (verified against REF for
// `contrib/libs/musl/include`'s `musl.py`). Returns parallel ref + path
// slices in declaration order.
//
// The CP NodeRef is cached on `genCtx.ldPluginCPCache` keyed by output
// path: REF emits each CP node once (on the target platform) and shares
// its UID across target and host LD deps; without dedup the host walk
// re-fires `emitOwnLDPlugins` on the same plugin and produces a
// duplicate on `default-linux-x86_64` (Platform is part of the
// canonical hash). First-emit wins — the seed runs target-first, so
// the cached entry carries the target platform per REF.
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

// mergeLDPlugins concatenates `(ownRefs, ownPaths)` with `(peerRefs,
// peerPaths)`, dropping any peer entry whose path appears in own.
// Mirrors `mergeDedup` for the parallel-slice case (LD plugin propagation).
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
// LIBRARY's peer-walk. All four axes share the same declaration-order +
// dedup discipline as the main walker. Two-phase collection: for each
// peer, collect its OWN declarations FIRST (across all peers), then
// collect transitive contributions — giving the REF ordering
// (own-from-peer1, ..., transitive-from-peer1, ...) where
// libcxx/include + libcxxrt/include precede the musl-arch propagation
// chain (util/charset, tools/archiver/main.cpp.o cmd_args[11..16]).
type peerGlobalContribs struct {
	addIncl    []string
	cFlags     []string
	cxxFlags   []string
	cOnlyFlags []string
	// Archive closure transitively reachable from this header-only
	// LIBRARY's peers — DFS post-order, dedup-by-path (same discipline as
	// the main walker). Header-only LIBRARYs emit no AR themselves but
	// expose the transitive archive closure (e.g. `contrib/libs/musl/
	// include` is header-only and its `IF` branches PEERDIR
	// `contrib/libs/musl` — the consumer needs musl in its archive set
	// even though musl/include contributes no archive).
	archiveRefs  []NodeRef
	archivePaths []string
	// `.global.a` closure transitively reachable through this header-only
	// LIBRARY's peers (every peer's own GlobalRef ∪ PeerGlobalClosure*).
	// Header-only LIBRARYs emit no `.global.a` themselves but peers may.
	globalRefs  []NodeRef
	globalPaths []string
	// LD plugin closure surfaced through the header-only walker. Same
	// dedup-by-path / declaration-order / first-occurrence-wins.
	ldPluginRefs  []NodeRef
	ldPluginPaths []string
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

		// Fold peer's transitive archive closure plus peer's own AR
		// (when present) in DFS post-order.
		for i, p := range peerResult.PeerArchiveClosurePaths {
			addArchive(peerResult.PeerArchiveClosureRefs[i], p)
		}

		// Use peerResult.ARPath (py3-prefixed for Python modules); skip
		// when hasPlainAR is false. Gate on `hasPlainAR` alone —
		// PROTO_LIBRARY has `headerOnly=true` AND `hasPlainAR=true`, so
		// a `!headerOnly` guard would wrongly suppress its AR.
		if peerResult.hasPlainAR {
			arRelPath := strings.TrimPrefix(peerResult.ARPath, "$(B)/")
			addArchive(peerResult.ARRef, arRelPath)
		}

		// Fold peer's transitive `.global.a` closure plus peer's own
		// `.global.a` (when GlobalRef != nil). Header-only peers may
		// emit a `.global.a` (e.g. `certs` from RESOURCE-driven objcopy).
		for i, p := range peerResult.PeerGlobalClosurePaths {
			addGlobal(peerResult.PeerGlobalClosureRefs[i], p)
		}

		if peerResult.GlobalRef != nil {
			addGlobal(*peerResult.GlobalRef, peerResult.GlobalPath)
		}

		// Fold peer's transitive LD plugin closure. Header-only peers
		// (musl/include) populate from their own LD_PLUGIN macro;
		// non-header peers carry through if any transitive PEERDIR did.
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

	// Header-only LIBRARYs keep the natural Phase 1+2 order — none of
	// the M2-closure header-only modules are runtime ancestors needing
	// libcxx/libcxxrt as transitive header-only contributions. The hoist
	// gate in genModule keys on `isRuntimeAncestor`; a future header-only
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
	return strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".py") ||
		strings.HasSuffix(srcRel, ".g4")
}

// isCodegenProducingSrc reports whether `srcRel` is a source whose
// emitOneSource branch emits a codegen node (PB/EV/R6/R5/CF) whose
// outputs go into the per-scanner CodegenRegistry. Consumer sources in
// the SAME module may #include those outputs, so the two-pass loop runs
// these first to populate the registry before any consumer CC scans
// its closure.
//
// `.proto` is excluded: it runs only via emitProtoSrcs in the
// PROTO_LIBRARY header-only branch (those modules emit codegen ahead of
// any consumer module's CC walk via normal peer-walk ordering).
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
// `(ref, outputPath, ccInputs, true)` when a node was emitted; the
// ccInputs entries are the CC node's input list (primary source +
// IncludeInputs) so the caller's downstream AR/LD step folds them into
// its own `inputs` aggregate per the sg.json AR/LD shape. Headers
// return `(_, _, nil, false)`. Unknown extensions throw so a new source
// kind surfaces during integration.
//
// `srcDir` is the module's `SRCDIR(...)` setting (empty when none).
// When non-empty it relocates the per-source emitter's view: SRCS
// resolve to `$(S)/<srcDir>/<rel>` and the emitted node's `module_dir`
// becomes `<srcDir>`. The LD/AR/Global archives wrapping these sources
// stay at `instance.Path`. For ragel6/bin: `instance.Path =
// contrib/tools/ragel6/bin`, `srcDir = contrib/tools/ragel6` →
// per-source CC nodes show `module_dir = contrib/tools/ragel6` and
// inputs `$(S)/contrib/tools/ragel6/<src>`, while the LD lands at
// `bin/ragel6`.
//
// `in` carries the module's per-source-language compile knobs
// (CXXFLAGS / CONLYFLAGS / ADDINCL).
//
// `primaryCount` is the number of leading entries in `ccInputs` that
// are "primary sources" of this member (vs header/closure entries) —
// the .global.a aggregator drops these primaries when the member
// belongs to regular SRCS. .c/.cpp/.cc/.cxx/.S/.s/.asm yield
// primaryCount=1; .rl6 yields 1 (just the .rl6) or 2 (when the `.h`
// companion exists on disk).

// reorderARMembers reorders (refs, paths) so AR cmd_args match ymake's
// canonical member ordering:
//
//  1. SRC_C_NO_LTO sources — hoisted to the front in original relative order.
//  2. Regular SRCS hand-written .o (non-SRC_C_NO_LTO, non-R6, no codegen
//     suffix) — kept in declaration order.
//  3. JOIN_SRCS (entries at [numSrcsDerived, len)) — declaration order.
//  4. Codegen-derived .o files, partitioned by source-extension and
//     emitted in canonical order: .g4.cpp → .h_serialized.cpp →
//     .ev.pb.cc → .rl6.cpp → .reg3.cpp. Within each category
//     declaration order is preserved. REF places hand-written .cpp.o
//     before generated .o files.
//  5. R6-generated paths with the legacy `/_/_/` infix go last
//     (util/_/_/datetime/parser.rl6.cpp.o family).
//
// `isFlatNoLto` is a parallel bool slice marking SRC_C_NO_LTO entries
// (len == len(refs) == len(paths) at call time).
//
// `isCFGenerated` marks entries whose CC was driven by a CONFIGURE_FILE
// expansion (`.cpp.in` / `.c.in`). CF outputs share the plain `.cpp.o`
// / `.c.o` suffix with hand-written SRCS, so the path heuristic cannot
// distinguish them; this parallel signal tail-buckets CF entries after
// hand-written regulars.
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

// scannerFor returns the IncludeScanner for `instance`'s platform axis.
// Target-axis (aarch64) → target scanner; host-axis (x86_64) → host
// scanner. Returns nil when the matching scanner is not allocated (tests).
//
// This is the single dispatch point for the target-vs-host scanner
// choice. Callers wanting "the parsed-includes pool for this instance"
// MUST go through this helper. EN's `ctx.scannerTarget` direct accesses
// remain explicit because EN nodes always emit on the target axis
// regardless of the surrounding walk's axis — a deliberate cross-axis
// reach, not a per-instance dispatch.
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
