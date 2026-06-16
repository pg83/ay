package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

// acknowledgedMacros names every ya.make macro the gen accepts without a
// typed handler: each invocation lands in d.unhandledMacros[name] (its
// args, expanded against the per-module Environment) so a later pass can
// implement them properly, and the call is recorded in the audit visible
// via --dump-ignored-macros. Any macro NOT in this set causes
// applyUnknownStmt to throw — the right fix is to read upstream
// (yatool/build/conf, yatool/build/ymake.core.conf) and add a typed branch,
// not to extend this set lightly.
//
// Today's contents are macros we have empirically observed during sg2…sg5
// generation that contribute nothing to the emitted graph today:
//   - RECURSE / RECURSE_FOR_TESTS / RECURSE_ROOT_RELATIVE — re-target ya
//     make at sibling dirs; we drive the module set from the command-line
//     target plus the PEERDIR closure.
//   - Pure metadata: LICENSE / LICENSE_TEXTS / WITHOUT_LICENSE_TEXTS /
//     LICENSE_RESTRICTION / LICENSE_RESTRICTION_EXCEPTIONS / VERSION /
//     ORIGINAL_SOURCE / PROVIDES / SUPPRESSIONS / FILES / HEADERS /
//     NEED_CHECK / ENV / OWNER / SUBSCRIBER / MESSAGE / OPENSOURCE_PROJECT /
//     OPENSOURCE_EXPORT_REPLACEMENT / IDE_FOLDER / TAG / SIZE / TIMEOUT /
//     ALLOCATOR_IMPL.
//   - Build-toggles we don't gate on: NO_LTO / NO_CLANG_COVERAGE /
//     NO_CLANG_MCDC_COVERAGE / NO_CLANG_TIDY / NO_LINT / NO_PROFILE_RUNTIME /
//     NO_PYTHON_COVERAGE / NO_SANITIZE / NO_SANITIZE_COVERAGE / NO_JOIN_SRC /
//     STYLE_PYTHON / NO_OPTIMIZE / NO_OPTIMIZE_PY_PROTOS / NO_PYTHON2 /
//     NO_MYPY / NO_YMAKE_PYTHON / USE_LIGHT_PY2CC / WITHOUT_VERSION /
//     SPLIT_FACTOR / FORK_TESTS / FORK_SUBTESTS / REQUIREMENTS / DATA /
//     TEST_SRCS / LINT / TASKLET / TASKLETSUPPORT / DEFAULT / USE_CXX /
//     DEFINE_VARIABLE / PYTHON3 / MASMFLAGS / RESTRICT_PATH / JAVA_SRCS /
//     JAVA_CLASSPATH_IGNORE_CONFLICTZ / DISABLE.
//   - Tag/build-if filters we don't model: BUILD_ONLY_IF / NO_BUILD_IF /
//     INCLUDE_TAGS / ONLY_TAGS / CHECK_DEPENDENT_DIRS / EXCLUDE_TAGS.
//   - Windows-specific: WINDOWS_LONG_PATH_MANIFEST (ymake.core.conf:5590).
//   - Java/Kotlin-only: WITH_KOTLIN_GRPC (proto.conf:231, tag:java-specific) —
//     adds java protoc plugin args, java grpc/protobuf runtime peers, and a
//     sem-export var; contributes nothing to a C++/Python module's graph.
var acknowledgedMacros = map[string]struct{}{
	"RECURSE":                         {},
	"RECURSE_FOR_TESTS":               {},
	"RECURSE_ROOT_RELATIVE":           {},
	"LICENSE":                         {},
	"LICENSE_TEXTS":                   {},
	"WITHOUT_LICENSE_TEXTS":           {},
	"LICENSE_RESTRICTION":             {},
	"LICENSE_RESTRICTION_EXCEPTIONS":  {},
	"VERSION":                         {},
	"ORIGINAL_SOURCE":                 {},
	"PROVIDES":                        {},
	"SUPPRESSIONS":                    {},
	"FILES":                           {},
	"HEADERS":                         {},
	"NEED_CHECK":                      {},
	"NEED_REVIEW":                     {},
	"ENV":                             {},
	"OWNER":                           {},
	"SUBSCRIBER":                      {},
	"MESSAGE":                         {},
	"OPENSOURCE_PROJECT":              {},
	"OPENSOURCE_EXPORT_REPLACEMENT":   {},
	"IDE_FOLDER":                      {},
	"TAG":                             {},
	"SIZE":                            {},
	"TIMEOUT":                         {},
	"ALLOCATOR_IMPL":                  {},
	"NO_LTO":                          {},
	"NO_CLANG_COVERAGE":               {},
	"NO_CLANG_MCDC_COVERAGE":          {},
	"NO_CLANG_TIDY":                   {},
	"NO_LINT":                         {},
	"NO_PROFILE_RUNTIME":              {},
	"NO_PYTHON_COVERAGE":              {},
	"NO_SANITIZE":                     {},
	"NO_SANITIZE_COVERAGE":            {},
	"NO_JOIN_SRC":                     {},
	"STYLE_PYTHON":                    {},
	"NO_OPTIMIZE":                     {},
	"NO_OPTIMIZE_PY_PROTOS":           {},
	"NO_PYTHON2":                      {},
	"NO_MYPY":                         {},
	"NO_YMAKE_PYTHON":                 {},
	"NO_YMAKE_PYTHON3":                {},
	"TOOLCHAIN":                       {},
	"USE_LIGHT_PY2CC":                 {},
	"WITHOUT_VERSION":                 {},
	"SPLIT_FACTOR":                    {},
	"FORK_TESTS":                      {},
	"FORK_SUBTESTS":                   {},
	"REQUIREMENTS":                    {},
	"DATA":                            {},
	"TEST_SRCS":                       {},
	"LINT":                            {},
	"TASKLET":                         {},
	"TASKLETSUPPORT":                  {},
	"DEFAULT":                         {},
	"USE_CXX":                         {},
	"DEFINE_VARIABLE":                 {},
	"PYTHON3":                         {},
	"MASMFLAGS":                       {},
	"RESTRICT_PATH":                   {},
	"JAVA_SRCS":                       {},
	"JAVA_CLASSPATH_IGNORE_CONFLICTZ": {},
	"DISABLE":                         {},
	"BUILD_ONLY_IF":                   {},
	"NO_BUILD_IF":                     {},
	"INCLUDE_TAGS":                    {},
	"ONLY_TAGS":                       {},
	"EXCLUDE_TAGS":                    {},
	"CHECK_DEPENDENT_DIRS":            {},
	"WINDOWS_LONG_PATH_MANIFEST":      {},
	"WITH_KOTLIN_GRPC":                {},
}

// acknowledgedTokSet is acknowledgedMacros in TOK space, so the per-invocation
// gate is a bit probe instead of a name view + string-map read. Built once;
// every acknowledged name must exist in the closed TOK enum.
var acknowledgedTokSet = func() BitSet {
	var b BitSet

	for name := range acknowledgedMacros {
		t, ok := tokByName[name]

		if !ok {
			panic("acknowledgedMacros name missing from the TOK enum: " + name)
		}

		b.add(uint32(t))
	}

	return b
}()

type ModuleEmitResult struct {
	ARRef      NodeRef
	ARPath     *VFS
	isPROGRAM  bool
	LDRef      NodeRef
	LDPath     *VFS
	GlobalRef  *NodeRef
	GlobalPath *VFS

	WholeArchiveRefs  []NodeRef
	WholeArchivePaths []VFS

	WholeArchiveCmdPaths []VFS

	AddInclGlobal []VFS

	OwnAddInclGlobal []VFS

	// ProtoAddInclGlobal carries the $(S)-rooted PROTO_NAMESPACE this module
	// contributes upstream for downstream proto compiles. Upstream calls the
	// collected list _PROTO__INCLUDE and injects it via ${pre=-I=:_PROTO__INCLUDE}
	// in PROTOC cmdlines, sitting between -I=$(S)/contrib/libs/protobuf/src and
	// the trailing -I=$(B) / -I=$PROTOBUF_INCLUDE_PATH duplicate. A module
	// contributes only when its PROTO_NAMESPACE was GLOBAL or its kind is
	// PROTO_LIBRARY.
	ProtoAddInclGlobal []VFS

	// ProtoNamespaceTail carries the $(S)-rooted NON-GLOBAL PROTO_NAMESPACE
	// contributions (own + transitive peers'). Per the reference graphs these
	// trail the _PROTO__INCLUDE chain in protoc cmdlines and reach only
	// non-PROTO_LIBRARY consumers (moduleTag == 0) — a PROTO_LIBRARY's own
	// chain excludes them (yt_proto/yt/client vs yt/yt/library/quantile_digest
	// in sg5).
	ProtoNamespaceTail []VFS

	// AddInclOneLevel propagates to direct PEERDIR consumers only (one hop, not
	// transitive). Direct consumers absorb these paths into their own effective
	// addincl; they are NOT re-propagated via AddInclGlobal.
	AddInclOneLevel []VFS

	// AddInclUserGlobal is the peer's own GLOBAL and ONE_LEVEL ADDINCL paths in
	// declaration order — the equivalent of ymake's UserGlobal. Used by direct
	// consumers to preserve upstream -I ordering (UserGlobal before GlobalPropagated).
	AddInclUserGlobal []VFS

	CFlagsGlobal     []ARG
	CXXFlagsGlobal   []ARG
	COnlyFlagsGlobal []ARG
	ObjAddLibsGlobal []ARG

	LDFlagsGlobal []ARG

	RPathFlagsGlobal []ARG

	PeerArchiveClosureRefs  []NodeRef
	PeerArchiveClosurePaths []VFS

	isPyLibrary bool

	PeerGlobalClosureRefs  []NodeRef
	PeerGlobalClosurePaths []VFS

	PeerWholeArchiveClosureRefs     []NodeRef
	PeerWholeArchiveClosurePaths    []VFS
	PeerWholeArchiveCmdClosurePaths []VFS

	LDPluginRefs  []NodeRef
	LDPluginPaths []VFS

	PeerDynamicClosureRefs  []NodeRef
	PeerDynamicClosurePaths []VFS

	// SbomComponentRef/Path is this module's own _GEN_SBOM_COMPONENT DX node
	// (.component.sbom), set only for qualifying (contrib/vendor) modules.
	// PeerSbomClosure is the transitive union of qualifying peers' components
	// over the link closure; embedding programs collect it into the link node.
	SbomComponentRef     *NodeRef
	SbomComponentPath    *VFS
	PeerSbomClosureRefs  []NodeRef
	PeerSbomClosurePaths []VFS

	InducedDeps ParsedIncludeSet

	Peerdirs []STR

	ModuleStmtName TOK

	testSuiteInfo *TestSuiteInfo

	// ResourceGlobalClosure is the transitive union of external-resource globals
	// (<NAME>_RESOURCE_GLOBAL) reachable through this module's PEERDIR closure,
	// deduped by global-var name in first-seen order. A RESOURCES_LIBRARY seeds it
	// with its own DECLARE_EXTERNAL_RESOURCE declarations; every module folds in its
	// peers'. Consumers (test-run nodes) render it into --global-resource lists.
	ResourceGlobalClosure []ResourceDecl
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

func protoResultWholeArchiveCmdPaths(res *ProtoSrcsResult) []VFS {
	if res == nil {
		return nil
	}

	return cloneVFSs(res.WholeArchiveCmdPaths)
}

type GenCtx struct {
	fs      FS
	parsers *IncludeParserManager
	emit    Emitter
	// na is the emitter's node-construction arenas (see NodeArenas), shared
	// here so ctx-threaded builders reach them without the Emitter detour.
	na *NodeArenas

	// inclArgValues backs inclArgMemo (the "-I<path>" cache); owned here so future
	// VFS-keyed value columns can share its idx array. inclArgs points at it.
	inclArgValues   DenseMap[VFS, STR]
	inclArgs        InclArgMemo
	memo            *IntValueMap[*ModuleEmitResult]
	walking         map[ModuleInstance]bool
	cyclesTolerated int

	traceStack []string

	scannerTarget *IncludeScanner
	scannerHost   *IncludeScanner

	// moduleByRef maps a module's LD NodeRef back to its emit result, populated in
	// toolResult (so every codegen tool resolved via ctx.tool is reachable by ref).
	// The include scanner uses it to pull a generated file's producing tools'
	// INDUCED_DEPS into the file's closure, via GeneratedFileInfo.GeneratorRefs —
	// so a tool's induced runtime headers come from its declared INDUCED_DEPS, not
	// a per-emitter hardcoded list. Scanners hold a pointer to it.
	moduleByRef DenseMap[NodeRef, *ModuleEmitResult]

	// tools caches a codegen tool's emit result by its module-path ARG, so repeated
	// ctx.tool(argX) lookups skip rebuilding the ModuleInstance + memo probe.
	tools DenseMap[ARG, *ModuleEmitResult]

	// scripts maps each build/scripts script VFS to [self, …transitive import
	// closure]; emit sites add a build script via append(inputs, scripts[v]...).
	scripts ScriptDeps

	// fetchRefs maps an external-resource name (CLANG, LLD_ROOT, …) to its FETCH
	// node, emitted once when the declaring RESOURCES_LIBRARY is gen'd
	// (emitResourceFetch). Consumers that reference $(NAME) take the dep from here.
	// Shared with the resource-aware emitter so attachResourceDeps resolves it.
	fetchRefs *DenseMap[STR, NodeRef]

	host   *Platform
	target *Platform

	testMode bool

	// sbomEnabled is true when the build config defines the SBOM feature
	// (build/internal/conf/sbom.conf sets SBOM_GENERATION_ALLOWED=yes). Gates the
	// _GEN_SBOM_COMPONENT DX nodes; absent in the open-source contour (sg2–5).
	sbomEnabled bool

	// tarjan is the run-wide Tarjan/closure working state; both scanners hold a
	// pointer to it (their tjc field) so its vfsBound-sized arrays grow once, not
	// once per scanner. reset() runs before every use, so the shared state is safe
	// under single-threaded gen.
	tarjan TarjanCtx
}

type ScanCtxPerfStats struct {
	subgraphEntries int
	childrenEntries int
	closureWindows  int
}

func resolveCodegenDepRefs(ctx *GenCtx, consumer ModuleInstance, includeInputs []VFS, exclude ...NodeRef) []NodeRef {
	if len(includeInputs) == 0 {
		return nil
	}

	// Reuse the per-run dedup map (genCtx) instead of allocating one per call —
	// its growth was ~25MB of churn across ~22k calls. Cleared on entry; keeps its
	// grown buckets between calls.
	// Dedup the producer refs through the run-wide VFS deduper (NodeRef is a
	// ~uint32 id, cast to VFS at the IdSet boundary — a different typedef over the
	// same dense space). It touches no other deduper user (EmitCF takes no ctx), so
	// reset-then-stream here is safe.
	deduper.reset()

	for _, r := range exclude {
		deduper.add(VFS(r))
	}

	var out []NodeRef

	// All codegen producer refs (PB/EV/EN, and CP/CF) live on the codegen
	// registry entry's ProducerRef, so one reg.Lookup resolves every kind —
	// no per-kind side maps.
	reg := codegenRegForInstance(ctx, consumer)

	// The IsBuild gate guards the lookup inline: the dominant cost was touching
	// every element of a whole include closure just to bounce off this bit for
	// the (vast) $(S) majority.
	for _, p := range includeInputs {
		if !p.isBuild() {
			continue
		}

		info := reg.lookup(p)

		if info == nil {
			continue
		}

		if !deduper.add(VFS(info.ProducerRef)) {
			continue
		}

		out = append(out, info.ProducerRef)
	}

	return out
}

func (ctx *GenCtx) perfScanCtxStats(scanner *IncludeScanner) ScanCtxPerfStats {
	return ScanCtxPerfStats{
		// subgraph and children are columns of one DenseMap3 keyed per node, so
		// both report its distinct-key count.
		subgraphEntries: scanner.scanCache.len(),
		childrenEntries: scanner.scanCache.len(),
		closureWindows:  len(scanner.subgraphClosures),
	}
}

func reportPerfStats(ctx *GenCtx, parsers *IncludeParserManager, targetScanner, hostScanner *IncludeScanner) {
	if !perfStatsEnabled {
		return
	}

	parserStats := parsers.perfStats()
	fsStats := ctx.fs.perfStats()
	fmt.Fprintf(os.Stderr, "perf: parser parsedHits=%d parsedMisses=%d buildParsed=%d\n",
		parserStats.parsedHits, parserStats.parsedMisses, parserStats.buildParsed)
	fmt.Fprintf(os.Stderr, "perf: fs listdirHits=%d listdirMisses=%d existsHits=%d existsMisses=%d dirsCached=%d\n",
		fsStats.listdirHits, fsStats.listdirMisses, fsStats.existsHits, fsStats.existsMisses, fsStats.dirsCached)
	fmt.Fprintf(os.Stderr, "perf: intern strs=%d args=%d envs=%d overflow=%d\n",
		len(internTable.strs), len(argTable.strs), len(envTable.strs), len(internTable.overflow))

	reportScanner := func(label string, scanner *IncludeScanner) {
		scanStats := scanner.perfStats()
		ctxStats := ctx.perfScanCtxStats(scanner)
		fmt.Fprintf(os.Stderr, "perf: scanner %s closureEntries=%d closureWindows=%d childrenEntries=%d walkClosure=%d closureHits=%d closureMisses=%d cyclicSCCs=%d closureSubsumed=%d searchTierHits=%d searchTierMisses=%d resolveCalls=%d\n",
			label,
			ctxStats.subgraphEntries,
			ctxStats.closureWindows,
			ctxStats.childrenEntries,
			scanStats.walkClosureCalls,
			scanStats.subgraphHits,
			scanStats.subgraphMisses,
			scanStats.subgraphTainted,
			scanStats.subgraphSubsumed,
			scanStats.searchTierHits,
			scanStats.searchTierMisses,
			scanStats.resolveSearchPathCalls,
		)
	}

	reportScanner("target", targetScanner)
	reportScanner("host", hostScanner)
}

func runGenIntoWithResources(fs FS, targetDir string, hostP, targetP *Platform, emitter Emitter, onWarn func(Warn), testMode bool) NodeRef {
	plainEmit := emitter
	scriptTbl := buildScriptTable(fs)
	// fetchRefs (the resource pattern → FETCH node registry) is owned by the
	// emitter; the genCtx shares its pointer so emitResourceFetch populates the
	// same map Node.buildDeps later resolves Resources through, on the fly.
	var fetchRefs *DenseMap[STR, NodeRef]

	// Mix $(S) input content hashes into node uids in every mode so a source edit
	// invalidates the cache (the dump path is re-uid'd from canonical content
	// downstream, but the raw uids must still be content-correct).
	switch e := plainEmit.(type) {
	case *BufferedEmitter:
		e.fs = fs
		fetchRefs = e.fetchRefs
	case *StreamingEmitter:
		e.uidScratch.fs = fs
		e.uidScratch.fetchRefs = e.fetchRefs
		fetchRefs = e.fetchRefs
	}

	parsers := newIncludeParserManagerFS(fs, newSharedParseCache())

	targetReg := newCodegenRegistry()
	hostReg := newCodegenRegistry()

	ctx := &GenCtx{
		fs:        fs,
		parsers:   parsers,
		emit:      plainEmit,
		na:        plainEmit.nodeArenas(),
		memo:      newIntValueMap[*ModuleEmitResult](4096),
		walking:   make(map[ModuleInstance]bool),
		host:      hostP,
		target:    targetP,
		fetchRefs: fetchRefs,
		scripts:   scriptTbl,
		testMode:  testMode,
		// SBOM_GENERATION_ALLOWED is defined only by build/internal/conf/sbom.conf;
		// its presence is the feature gate (open-source roots lack it).
		sbomEnabled: fs.isFile(srcRootVFS, sbomConfRel),
	}

	ctx.inclArgs = InclArgMemo{m: &ctx.inclArgValues}

	// Both scanners share ctx.tarjan (the run-wide Tarjan scratch) so its
	// vfsBound-sized arrays grow once, not once per scanner.
	targetScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(targetP.ISA), targetP.Flags[envMUSL] == strYes, targetP.Flags[envOPENSOURCE] == strYes, onWarn), onWarn, &ctx.tarjan)
	targetScanner.codegen = targetReg
	targetScanner.moduleByRef = &ctx.moduleByRef
	hostScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(hostP.ISA), hostP.Flags[envMUSL] == strYes, hostP.Flags[envOPENSOURCE] == strYes, onWarn), onWarn, &ctx.tarjan)
	hostScanner.codegen = hostReg
	hostScanner.moduleByRef = &ctx.moduleByRef
	ctx.scannerTarget = targetScanner
	ctx.scannerHost = hostScanner

	seed := ModuleInstance{
		Path:     source(filepath.Clean(targetDir)),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: targetP,
	}

	root := genModule(ctx, seed)

	ctx.emit.result(root.LDRef)

	if ctx.testMode && root.testSuiteInfo != nil {
		for _, ref := range emitTestRunNodes(plainEmit, plainEmit, targetP, *root.testSuiteInfo, root.LDRef, root.ResourceGlobalClosure) {
			ctx.emit.result(ref)
		}
	}

	reportPerfStats(ctx, parsers, targetScanner, hostScanner)

	if be, ok := plainEmit.(*BufferedEmitter); ok {
		be.generatedFirstClaim = mergeGeneratedFirstClaims(targetScanner, hostScanner)
	}

	return root.LDRef
}

// mergeGeneratedFirstClaims merges the per-scanner first-consumer claim maps.
// On key conflict the target scanner wins — the host scanner only sees CC
// compiles for tool builds, which are an orthogonal claim space.
func mergeGeneratedFirstClaims(scanners ...*IncludeScanner) map[VFS]string {
	var n int

	for _, s := range scanners {
		if s != nil {
			n += len(s.generatedFirstClaim)
		}
	}

	if n == 0 {
		return nil
	}

	out := make(map[VFS]string, n)

	for _, s := range scanners {
		if s == nil {
			continue
		}

		for k, v := range s.generatedFirstClaim {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}

	return out
}

func genDumpGraphWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := newBufferedEmitter()
	// -G emits the same graph that gets executed: the resource FETCH nodes are real
	// dependencies (dump normalize folds them back out for the byte-exact compare).
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, testMode)

	return finalizeDumpGraph(emitter)
}

func genWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := newBufferedEmitter()
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, testMode)

	return finalize(emitter)
}

func programBinaryName(instance ModuleInstance, moduleStmt *ModuleStmt) string {
	if moduleStmt == nil {
		return ""
	}

	if moduleStmt.Name == tokUnittestFor {
		return strings.ReplaceAll(path.Clean(instance.Path.rel()), "/", "-")
	}

	// PY3_PROGRAM_BIN(progname) links as its argument (the opensource reference:
	// tools/py3cc/slow/bin's PY3_PROGRAM_BIN(py3cc), INCLUDEd into tools/py3cc/slow,
	// links as $(B)/tools/py3cc/slow/py3cc). In the internal contour the same dir is
	// instead a PREBUILT_PROGRAM (USE_PREBUILT_TOOLS + ya.make.prebuilt present) whose
	// output takes the module-dir basename (.../slow) via genPrebuiltProgram — a
	// distinct module type, so this from-source path must honour its arg. Without an
	// argument it falls through to the module-dir basename default below.
	if len(moduleStmt.Args) > 0 {
		return moduleStmt.Args[0].string()
	}

	return ""
}

func programSourceDir(moduleStmt *ModuleStmt) *string {
	peerPath := unittestForPeerPath(moduleStmt)

	if peerPath == "" {
		return nil
	}

	return &peerPath
}

func unittestForPeerPath(moduleStmt *ModuleStmt) string {
	if moduleStmt == nil || moduleStmt.Name != tokUnittestFor || len(moduleStmt.Args) == 0 {
		return ""
	}

	return path.Clean(moduleStmt.Args[0].string())
}

func genModule(ctx *GenCtx, instance ModuleInstance) *ModuleEmitResult {
	if existing := ctx.memo.get(ctx.instanceKey(instance)); existing != nil {
		return *existing
	}

	if os.Getenv("YATOOL_TRACE") == "1" {
		indent := strings.Repeat("  ", len(ctx.traceStack))
		caller := "(root)"

		if len(ctx.traceStack) > 0 {
			caller = ctx.traceStack[len(ctx.traceStack)-1]
		}

		fmt.Fprintf(os.Stderr, "%sgenModule %s@%s  (from %s)\n", indent, instance.Path.rel(), instance.Platform.Target, caller)
		ctx.traceStack = append(ctx.traceStack, instance.Path.rel()+"@"+string(instance.Platform.Target))

		defer func() { ctx.traceStack = ctx.traceStack[:len(ctx.traceStack)-1] }()
	}

	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path.rel())

		return &ModuleEmitResult{}
	}

	ctx.walking[instance] = true

	defer delete(ctx.walking, instance)

	mf := throw2(parseFile(ctx.fs, joinRel(instance.Path.rel(), "ya.make")))

	env := buildIfEnv(instance)
	d := collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, mf.Stmts, env)

	// The consumer requested a variant without pre-parsing this module
	// (peerEntryLanguage). Only a PROTO_LIBRARY has a python variant: any other
	// module re-enters as its C++ variant and the py key aliases that result.
	// This is the generic reenter-with-corrected-parameters point — a future
	// variant fix (e.g. a DLL consumer re-entering a static peer with PIC)
	// belongs here too, BEFORE anything is emitted: the streaming emitter
	// cannot retract nodes.
	if instance.Language == LangPy && d.moduleStmt != nil && d.moduleStmt.Name != tokProtoLibrary {
		cpp := instance
		cpp.Language = LangCPP
		result := genModule(ctx, cpp)
		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	for _, stmt := range d.allPySrcs {
		applyAllPySrcs(ctx.fs, instance.Path.rel(), stmt, d)
	}

	if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && instance.Language != LangPy {
		cppProtoEnv := env.clone()
		cppProtoEnv.setStringID(envMODULE_TAG, strCPPProto)

		cppProtoEnv.setBool(envGEN_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, mf.Stmts, cppProtoEnv)
	} else if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy {
		py3ProtoEnv := env.clone()
		py3ProtoEnv.setBool(envPY3_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, mf.Stmts, py3ProtoEnv)
	}

	if d.conflictMod != nil {
		throwFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.rel(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.rel())
	}

	if d.moduleStmt.Name == tokResourcesLibrary {
		// A RESOURCES_LIBRARY's own LDFLAGS may reference ${<NAME>_RESOURCE_GLOBAL}
		// (build/platform/lld: --ld-path=${LLD_ROOT_RESOURCE_GLOBAL}/bin/ld.lld) ahead
		// of the DECLARE that defines it. Bind the declared globals into the env and
		// re-collect once so those references expand (ymake defers; we re-collect).
		if bindResourceGlobalVars(ctx, instance, d, env) {
			d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, mf.Stmts, env)
		}

		return genResourcesLibrary(ctx, instance, d)
	}

	if d.moduleStmt.Name == tokPrebuiltProgram {
		// PRIMARY_OUTPUT references ${<NAME>_RESOURCE_GLOBAL} (bound by the module's
		// own DECLARE_EXTERNAL_RESOURCE) and ${MODULE_SUFFIX}. Bind both and re-collect
		// once so the stored primaryOutput is fully expanded — the same deferred-
		// expansion re-collect RESOURCES_LIBRARY does for its LDFLAGS globals.
		env.setString(envMODULE_SUFFIX, prebuiltModuleSuffix(instance.Platform))

		if bindResourceGlobalVars(ctx, instance, d, env) {
			d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, mf.Stmts, env)
		}

		return genPrebuiltProgram(ctx, instance, d)
	}

	if instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary {
		hasProtoSrc := false

		for _, src := range d.srcs {
			if strings.HasSuffix(src.string(), ".proto") {
				hasProtoSrc = true

				break
			}
		}

		if hasProtoSrc && !strings.HasPrefix(instance.Path.rel(), "contrib/libs/protobuf/builtin_proto") &&
			!strings.HasPrefix(instance.Path.rel(), "contrib/python/protobuf") {
			d.peerdirs = append(d.peerdirs, strContribPythonProtobuf)
		}

		if hasProtoSrc && d.grpc {
			d.peerdirs = append(d.peerdirs, strContribPythonGrpcio)
		}
	}

	if d.moduleStmt.Name != tokLibrary && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) {
		throwFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path.rel(), d.moduleStmt.Name)
	}

	if !d.hadAllocator && (d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) {
		d.hadAllocator = true
		d.allocatorName = strJ
	}

	py3ProtoVariant := d.moduleStmt.Name == tokProtoLibrary && d.usePython3

	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path.rel() != "contrib/libs/python" {
		d.peerdirs = append([]STR{strContribLibsPython}, d.peerdirs...)
	} else if py3ProtoVariant && !d.noPythonIncl && instance.Path.rel() != "contrib/libs/python" {
		if moduleExcludesTag(d, "CPP_PROTO") {
			d.peerdirs = append([]STR{strContribLibsPython}, d.peerdirs...)
		} else {
			d.peerdirs = append(d.peerdirs, strContribLibsPython)
		}
	}

	if d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin {
		var earlyPeers []string

		if d.pythonSQLite3 {
			earlyPeers = append(earlyPeers, "contrib/tools/python3/Modules/_sqlite")
		}

		earlyPeers = append(earlyPeers, "library/python/runtime_py3/main")

		if !d.noImportTracing && instance.Path.rel() != "library/python/import_tracing/constructor" {
			earlyPeers = append(earlyPeers, "library/python/import_tracing/constructor")
		}

		var latePeers []string

		if !d.noCheckImportsDisabled {
			latePeers = append(latePeers, "library/python/testing/import_test")
		}

		if d.moduleStmt.Name == tokPy3ProgramBin {
			insertAt := 0

			if len(d.peerdirs) > 0 && d.peerdirs[0].string() == "contrib/libs/python" {
				insertAt = 1
			}

			filteredEarly := earlyPeers[:0]

			for _, peer := range earlyPeers {
				if instance.Path.rel() != peer {
					filteredEarly = append(filteredEarly, peer)
				}
			}

			spliced := make([]STR, 0, len(d.peerdirs)+len(filteredEarly))
			spliced = append(spliced, d.peerdirs[:insertAt]...)
			spliced = append(spliced, STRS(filteredEarly...)...)
			spliced = append(spliced, d.peerdirs[insertAt:]...)
			d.peerdirs = spliced
		} else {
			for _, peer := range earlyPeers {
				if instance.Path.rel() != peer {
					d.peerdirs = append(d.peerdirs, internStr(peer))
				}
			}
		}

		for _, peer := range latePeers {
			if instance.Path.rel() != peer {
				d.peerdirs = append(d.peerdirs, internStr(peer))
			}
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) && pyLibraryAutoPythonPeer(d.moduleStmt.Name) && d.moduleStmt.Name != tokPy3Program && d.moduleStmt.Name != tokPy3ProgramBin && !d.noImportTracing && instance.Path.rel() != "library/python/import_tracing/constructor" {
		d.peerdirs = append(d.peerdirs, strLibraryPythonImportTracingConstructor)
	}

	// (enum_serialization_runtime PEERDIR is added at GenerateEnumSerializationStmt
	// processing time — see modules.go — to match upstream's macro position.)

	// Upstream's _CPP_FLATC_CMD (fbs.conf) carries .PEERDIR=contrib/libs/flatbuffers,
	// adding it as an induced dep to every module with .fbs SRCS (e.g. apache/arrow).
	// Append after explicit PEERDIRs so the peer archive closure puts flatbuffers
	// after the module's last declared peer, matching upstream's link order.
	if d.hasFbs && instance.Path.rel() != "contrib/libs/flatbuffers" {
		d.peerdirs = append(d.peerdirs, strContribLibsFlatbuffers)
	}

	// _SRC("y") induces .PEERDIR=build/induced/by_bison (bison_lex.conf) — an empty
	// licensed library that hangs the bison-grammar license (and its SBOM
	// component) onto every module with a .y source.
	if d.hasBisonY && instance.Path.rel() != strBuildInducedByBison.string() {
		d.peerdirs = append(d.peerdirs, strBuildInducedByBison)
	}

	// Upstream's C++ language default is the contrib/libs/cxxsupp parent (it
	// PEERDIRs libcxx); we shortcut straight to libcxx, so the licensed parent —
	// and its SBOM component — is never processed. Under SBOM, add it back: it has
	// no archive (its libcxx closure dedups against the existing default, leaving
	// link order intact), contributing only its component.
	if ctx.sbomEnabled && !d.flags.NoRuntime && !effectiveNoPlatform(d.flags) && !strings.HasPrefix(instance.Path.rel(), "contrib/libs/cxxsupp") {
		d.peerdirs = append(d.peerdirs, strContribLibsCxxsupp)
	}

	if isSpecializedLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == tokDynamicLibrary {
			result := emitDynamicLibrary(ctx, instance, d)
			ctx.memo.put(ctx.instanceKey(instance), result)

			return result
		}

		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)
		d.tc = resolveModuleToolchain(peerContribs.resourceGlobals, instance.Platform.ClangVer)

		ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins, d.tc)
		ldPlugins := mergeLDPlugins(ownLDPlugins, &LdPluginsResult{
			Refs:  peerContribs.ldPluginRefs,
			Paths: peerContribs.ldPluginPaths,
		})

		if ldPlugins == nil {
			ldPlugins = &LdPluginsResult{}
		}

		headerOnlyInputs := ModuleCCInputs{
			InclArgs:          ctx.inclArgs,
			Flags:             d.flags,
			AddIncl:           d.addIncl,
			PeerAddInclGlobal: peerContribs.addIncl,
			SrcDirs:           d.srcDirs,
			FS:                ctx.fs,
			DefaultVars:       d.defaultVars,
			DefaultVarOrder:   d.defaultVarOrder,
			TC:                d.tc,
		}
		headerOnlyInputs.ScanCfg = newScanContext(ctx.parsers, d.addIncl, peerContribs.addIncl, includeScannerBasePaths(), instance.Path.rel())
		headerOnlyInputs.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &headerOnlyInputs)
		emitRunProgramsForAR(ctx, instance, d, headerOnlyInputs)
		emitRunPythonForAR(ctx, instance, d, headerOnlyInputs)

		emitPySrcs(ctx, instance, d)

		objcopyRes := emitResourceObjcopy(ctx, instance, d)

		var hOnlyGlobalRef *NodeRef
		var hOnlyGlobalPath *VFS
		var hOnlyWholeArchiveRefs []NodeRef
		var hOnlyWholeArchivePaths []VFS

		if objcopyRes != nil && len(objcopyRes.Refs) > 0 {
			arInstance := instance
			var globalBaseName string
			var tag STR
			archiveName := ""

			if len(d.moduleStmt.Args) > 0 {
				archiveName = d.moduleStmt.Args[0].string()
			}

			switch d.moduleStmt.Name {
			case tokPy23NativeLibrary:
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.rel(), "libpy3c", archiveName)
				tag = tagPy3NativeGlobal
			case tokPy23Library:
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.rel(), "libpy3", archiveName)
				tag = tagPy3Global
			case tokPy3Library, tokPy2Library, tokPy2Program, tokPy3Program:
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.rel(), "libpy3", archiveName)
				tag = tagGlobal
			default:
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.rel(), "lib", archiveName)
				tag = tagGlobal
			}

			gRef := emitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRes.Refs, objcopyRes.Outputs, d.tc, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = vfsPtr(build(instance.Path.rel() + "/" + globalBaseName))
		}

		// emitMiscNodes registers JV / CF outputs in the CodegenRegistry so
		// emitProtoSrcs can wire SRCS(X.proto) that names a build-generated
		// proto (e.g. jsonpath's RUN_ANTLR -language protobuf output) to its
		// JV producer via the protoSrcOverride path in emit_proto.go.
		emitMiscNodes(ctx, instance, d, nil)

		protoResult := emitProtoSrcs(ctx, instance, d, peerContribs)

		if d.moduleStmt.Name != tokProtoLibrary {
			emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, nil)
		}

		hOnlyARRef := NodeRef(0)
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

		// Specialized-library path: same narrow rule — only an explicit
		// PROTO_NAMESPACE GLOBAL contributes to _PROTO__INCLUDE; a bare
		// PROTO_NAMESPACE rides the ProtoNamespaceTail instead.
		var ownProtoAddInclH []VFS
		var ownProtoTailH []VFS

		if d.protoNamespace != nil {
			ns := source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))

			if d.protoNamespaceGlobal {
				ownProtoAddInclH = []VFS{ns}
			} else {
				ownProtoTailH = []VFS{ns}
			}
		}

		effectiveProtoAddInclH := dedupVFS(ownProtoAddInclH, peerContribs.protoAddIncl)
		effectiveProtoTailH := dedupVFS(ownProtoTailH, peerContribs.protoNamespaceTail)

		var ownSbomRefH *NodeRef
		var ownSbomPathH *VFS

		if sbomActive(ctx, instance) && sbomQualifies(d) {
			realPrjName := strings.TrimSuffix(archiveNameWithPrefixOrName(instance.Path.rel(), "", ""), ".a")
			ownSbomRefH, ownSbomPathH = emitSbomComponent(ctx, instance, d, realPrjName)
		}

		result := &ModuleEmitResult{
			isPyLibrary:        isPyLibraryType(d.moduleStmt.Name),
			ARRef:              hOnlyARRef,
			ARPath:             hOnlyARPath,
			GlobalRef:          hOnlyGlobalRef,
			GlobalPath:         hOnlyGlobalPath,
			AddInclGlobal:      dedupVFS(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal:   d.addInclGlobal,
			ProtoAddInclGlobal: effectiveProtoAddInclH,
			ProtoNamespaceTail: effectiveProtoTailH,
			AddInclOneLevel:    d.addInclOneLevel,
			AddInclUserGlobal:  d.addInclUserGlobal,

			CFlagsGlobal:                    dedupARG(peerContribs.cFlags, d.cFlagsGlobal),
			CXXFlagsGlobal:                  dedupARG(peerContribs.cxxFlags, d.cxxFlagsGlobal),
			COnlyFlagsGlobal:                dedupARG(peerContribs.cOnlyFlags, d.cOnlyFlagsGlobal),
			ObjAddLibsGlobal:                dedupARG(peerContribs.objAddLibs, d.objAddLibsGlobal),
			LDFlagsGlobal:                   dedupARG(peerContribs.ldFlags, d.ldFlags),
			RPathFlagsGlobal:                dedupARG(peerContribs.rpathFlags, d.rpathFlagsGlobal),
			PeerArchiveClosureRefs:          peerArchiveRefsH,
			PeerArchiveClosurePaths:         peerArchivePathsH,
			PeerGlobalClosureRefs:           peerGlobalRefsH,
			PeerGlobalClosurePaths:          peerGlobalPathsH,
			WholeArchiveRefs:                hOnlyWholeArchiveRefs,
			WholeArchivePaths:               hOnlyWholeArchivePaths,
			WholeArchiveCmdPaths:            protoResultWholeArchiveCmdPaths(protoResult),
			PeerWholeArchiveClosureRefs:     peerContribs.wholeArchiveRefs,
			PeerWholeArchiveClosurePaths:    peerContribs.wholeArchivePaths,
			PeerWholeArchiveCmdClosurePaths: peerContribs.wholeArchiveCmdPaths,
			LDPluginRefs:                    ldPlugins.Refs,
			LDPluginPaths:                   ldPlugins.Paths,
			PeerDynamicClosureRefs:          peerContribs.dynamicRefs,
			PeerDynamicClosurePaths:         peerContribs.dynamicPaths,
			SbomComponentRef:                ownSbomRefH,
			SbomComponentPath:               ownSbomPathH,
			PeerSbomClosureRefs:             peerContribs.sbomRefs,
			PeerSbomClosurePaths:            peerContribs.sbomPaths,
			InducedDeps:                     d.inducedDeps,
			Peerdirs:                        d.peerdirs,
			ModuleStmtName:                  d.moduleStmt.Name,
		}
		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	languageDefaults := defaultPeerdirsForModule(ctx, instance, d)

	languageDefaults = suppressMallocAPIDefault(languageDefaults, d.allocatorName)

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path.rel())
	unitTestPeer := unittestForPeerPath(d.moduleStmt)

	var preUserProgDefaults []string
	var postUserProgDefaults []string

	if isProgram {
		preUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, false)
		postUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, true)
	}

	allocatorExplicitPeers := allocatorPeers[d.allocatorName.string()]

	unitTestPeerCount := 0

	if unitTestPeer != "" {
		unitTestPeerCount = 1
	}

	// Membership rides the global epoch deduper keyed by the peer string's
	// intern id (the peers get interned downstream anyway) — a bitset probe
	// instead of a string-map read. Leaf contract: the list assembly below is
	// pure appends, nothing reaches another deduper user.
	deduper.reset()
	peerSeen := func(p string) bool {
		return !deduper.add(VFS(internStr(p)) << 1)
	}
	allPeers := make([]string, 0, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	const (
		peerKindLangDefault    = 0
		peerKindProgramDefault = 1
		peerKindUserPeer       = 2
		peerKindUnitTestPeer   = 3
	)

	peerKinds := make([]int, 0, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	for _, p := range languageDefaults {
		if peerSeen(p) {
			continue
		}

		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindLangDefault)
	}

	if unitTestPeer != "" {
		if !peerSeen(unitTestPeer) {
			allPeers = append(allPeers, unitTestPeer)
			peerKinds = append(peerKinds, peerKindUnitTestPeer)
		}
	}

	for _, p := range preUserProgDefaults {
		if peerSeen(p) {
			continue
		}

		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindProgramDefault)
	}

	for _, p := range allocatorExplicitPeers {
		if peerSeen(p) {
			continue
		}

		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	for _, p := range postUserProgDefaults {
		if peerSeen(p) {
			continue
		}

		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindProgramDefault)
	}

	for _, p := range d.peerdirs {
		// peerSeen's id-space twin: p is already interned, so membership is a
		// direct deduper probe — no view, no re-intern.
		if !deduper.add(VFS(p) << 1) {
			continue
		}

		allPeers = append(allPeers, p.string())
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
	peerLinkCmdPaths := make([]VFS, 0, len(allPeers))
	peerSbomRefs := make([]NodeRef, 0, len(allPeers))
	peerSbomPaths := make([]VFS, 0, len(allPeers))
	// The peer-collection dedup sets are built below, after the recursive peer
	// genModule loop — each in its own inlined pass that resets the run-wide
	// deduper and streams exactly one set through deduper.add. Running after
	// the recursion lets the passes share one deduper (a nested genModule would
	// otherwise reset it mid-pass) instead of allocating a map per set.

	peerLDPluginRefs := make([]NodeRef, 0, 1)
	peerLDPluginPaths := make([]VFS, 0, 1)
	var objAddLibSeen BitSet
	peerObjAddLibsGlobal := make([]ARG, 0, 8)
	var ldFlagsSeen BitSet
	peerLDFlagsGlobal := make([]ARG, 0, 4)
	var rpathFlagsSeen BitSet
	peerRPathFlagsGlobal := make([]ARG, 0, 4)
	// peerAddInclGlobal aggregation routes through the run-global deduper. The
	// whole add sequence — lang/test/program/user peers plus the libc++ injection,
	// which is hoisted above the effectiveAddInclGlobal dedupVFS so it lands in the
	// same reset-free window — runs contiguously before that first dedupVFS reset.
	// Bundled-path filtering drops entries from the slice but never from the
	// deduper, so the membership stays broader than the slice, as required.
	peerAddInclGlobal := make([]VFS, 0, 16)
	// oneLevelOnlyPaths tracks paths added exclusively via ONE_LEVEL from direct user
	// peers. Such paths appear in peerAddInclGlobal (for correct CC command ordering)
	// but must be excluded from effectiveAddInclGlobal so they don't re-propagate
	// transitively — upstream ONE_LEVEL propagates only one hop.
	var oneLevelOnlyPaths map[VFS]struct{}
	var cFlagsSeen BitSet
	peerCFlagsGlobal := make([]ARG, 0, 16)
	var cxxFlagsSeen BitSet
	peerCXXFlagsGlobal := make([]ARG, 0, 16)
	var cOnlyFlagsSeen BitSet
	peerCOnlyFlagsGlobal := make([]ARG, 0, 16)
	type resolvedPeer struct {
		path   string
		result *ModuleEmitResult
		kind   int
	}

	resolved := make([]resolvedPeer, 0, len(allPeers))

	for i, p := range allPeers {
		peerPath := filepath.Clean(p)

		kind := peerKinds[i]

		if kind != peerKindUserPeer && !peerYaMakeExists(ctx.fs, peerPath) {
			continue
		}

		peerInstance := derivePeerInstance(ctx, instance, d, peerPath)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.isPROGRAM {
			throwFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path.rel(), peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})
	}

	// Resource globals (<NAME>_RESOURCE_GLOBAL) propagate transitively: fold every
	// peer's closure, deduped by global-var STR through the run-wide deduper (a leaf
	// pass — no genModule reentry — so reset-then-stream is safe).
	var resourceGlobalsClosure []ResourceDecl
	deduper.reset()

	for _, rp := range resolved {
		for _, decl := range rp.result.ResourceGlobalClosure {
			if deduper.add(VFS(decl.GlobalVar)) {
				resourceGlobalsClosure = append(resourceGlobalsClosure, decl)
			}
		}
	}

	// Tool paths (compiler/archiver/objcopy/strip/linker/python) come from the
	// build/platform/* peers via this closure, not from ambient platform flags.
	d.tc = resolveModuleToolchain(resourceGlobalsClosure, instance.Platform.ClangVer)

	deduper.reset()

	for _, rp := range resolved {
		for i, p := range rp.result.LDPluginPaths {
			if deduper.add(p) {
				peerLDPluginRefs = append(peerLDPluginRefs, rp.result.LDPluginRefs[i])
				peerLDPluginPaths = append(peerLDPluginPaths, p)
			}
		}
	}

	archiveOrder := resolved

	{
		switch d.moduleStmt.Name {
		case tokPy2Program:
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
		case tokPy3ProgramBin:

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
		case tokPy3Program:
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

	// peerArchive: closure paths, then the peer's own AR output (per peer).
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for i, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p) {
				peerArchiveRefs = append(peerArchiveRefs, pr.PeerArchiveClosureRefs[i])
				peerArchivePaths = append(peerArchivePaths, p)
			}
		}

		if pr.ARPath != nil && deduper.add(*pr.ARPath) {
			peerArchiveRefs = append(peerArchiveRefs, pr.ARRef)
			peerArchivePaths = append(peerArchivePaths, *pr.ARPath)
		}
	}

	// peerGlobal: closure paths, then the peer's own GLOBAL output.
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for i, p := range pr.PeerGlobalClosurePaths {
			if deduper.add(p) {
				peerGlobalRefs = append(peerGlobalRefs, pr.PeerGlobalClosureRefs[i])
				peerGlobalPaths = append(peerGlobalPaths, p)
			}
		}

		if pr.GlobalRef != nil && pr.GlobalPath != nil && deduper.add(*pr.GlobalPath) {
			peerGlobalRefs = append(peerGlobalRefs, *pr.GlobalRef)
			peerGlobalPaths = append(peerGlobalPaths, *pr.GlobalPath)
		}
	}

	// peerSbom: the .component.sbom global outputs of every qualifying module in
	// the link (archive) closure — mirrors peerArchive but carries the SBOM node
	// per peer. Embedding programs collect these into the link's inputs; only the
	// reached ones survive normalize's target closure.
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for i, p := range pr.PeerSbomClosurePaths {
			if deduper.add(p) {
				peerSbomRefs = append(peerSbomRefs, pr.PeerSbomClosureRefs[i])
				peerSbomPaths = append(peerSbomPaths, p)
			}
		}

		if pr.SbomComponentRef != nil && deduper.add(*pr.SbomComponentPath) {
			peerSbomRefs = append(peerSbomRefs, *pr.SbomComponentRef)
			peerSbomPaths = append(peerSbomPaths, *pr.SbomComponentPath)
		}
	}

	// peerWholeArchive: closure paths, then the peer's own whole-archive paths.
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for i, p := range pr.PeerWholeArchiveClosurePaths {
			if deduper.add(p) {
				peerWholeArchiveRefs = append(peerWholeArchiveRefs, pr.PeerWholeArchiveClosureRefs[i])
				peerWholeArchivePaths = append(peerWholeArchivePaths, p)
			}
		}

		for i, p := range pr.WholeArchivePaths {
			if deduper.add(p) {
				peerWholeArchiveRefs = append(peerWholeArchiveRefs, pr.WholeArchiveRefs[i])
				peerWholeArchivePaths = append(peerWholeArchivePaths, p)
			}
		}
	}

	// peerWholeArchiveCmd: command-line whole-archive paths (no refs).
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for _, p := range pr.PeerWholeArchiveCmdClosurePaths {
			if deduper.add(p) {
				peerWholeArchiveCmdPaths = append(peerWholeArchiveCmdPaths, p)
			}
		}

		for _, p := range pr.WholeArchiveCmdPaths {
			if deduper.add(p) {
				peerWholeArchiveCmdPaths = append(peerWholeArchiveCmdPaths, p)
			}
		}
	}

	// peerDynamic: closure paths, then the peer's own DYNAMIC_LIBRARY output.
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for i, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p) {
				peerDynamicRefs = append(peerDynamicRefs, pr.PeerDynamicClosureRefs[i])
				peerDynamicPaths = append(peerDynamicPaths, p)
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(*pr.LDPath) {
			peerDynamicRefs = append(peerDynamicRefs, pr.LDRef)
			peerDynamicPaths = append(peerDynamicPaths, *pr.LDPath)
		}
	}

	// peerLinkCmd is the dedup-union of the archive and dynamic paths, in the
	// interleaved order they were originally fed: per peer, archive-closure then
	// dynamic-closure then the peer's own dynamic-lib then its AR output. Its own
	// pass re-walks those sources (reading them a second time is cheap) so it need
	// not piggyback on the archive/dynamic passes.
	deduper.reset()

	for _, rp := range archiveOrder {
		pr := rp.result

		for _, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p) {
				peerLinkCmdPaths = append(peerLinkCmdPaths, p)
			}
		}

		for _, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p) {
				peerLinkCmdPaths = append(peerLinkCmdPaths, p)
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(*pr.LDPath) {
			peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.LDPath)
		}

		if pr.ARPath != nil && deduper.add(*pr.ARPath) {
			peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.ARPath)
		}
	}

	// Seed the run-global deduper for the peerAddInclGlobal aggregation; the
	// peer-collection dedup passes above left it in an arbitrary state.
	deduper.reset()

	// Aggregate every peer's propagated ADDINCL(GLOBAL) into peerAddInclGlobal,
	// deduping through the run-global deduper (seeded above). The passes run in a
	// fixed kind order — lang defaults, unit-test, program defaults, user peers —
	// and that order is load-bearing (it sets the -I order on the CC command).
	// Each pass sweeps all resolved peers of one kind, so the kinds stay grouped
	// even though `resolved` interleaves program-default and user peers.

	// Lang defaults: own GLOBAL across all, then transitive GLOBAL across all (two
	// sweeps so every own-GLOBAL path precedes every transitive-GLOBAL one).
	for _, rp := range resolved {
		if rp.kind == peerKindLangDefault {
			for _, p := range rp.result.OwnAddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

	for _, rp := range resolved {
		if rp.kind == peerKindLangDefault {
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

	// Unit-test peer: transitive GLOBAL.
	for _, rp := range resolved {
		if rp.kind == peerKindUnitTestPeer {
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

	// Program defaults: transitive GLOBAL.
	for _, rp := range resolved {
		if rp.kind == peerKindProgramDefault {
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

	// User peers: UserGlobal in declaration order (upstream PropagateTo propagates
	// UserGlobal before GlobalPropagated), ONE_LEVEL tracked for one-hop semantics,
	// then transitive GLOBAL (a GLOBAL re-export beats ONE_LEVEL-only).
	for _, rp := range resolved {
		if rp.kind != peerKindUserPeer {
			continue
		}

		for _, p := range rp.result.AddInclUserGlobal {
			if deduper.add(p) {
				peerAddInclGlobal = append(peerAddInclGlobal, p)
			}
		}

		for _, p := range rp.result.AddInclOneLevel {
			if oneLevelOnlyPaths == nil {
				oneLevelOnlyPaths = map[VFS]struct{}{}
			}

			oneLevelOnlyPaths[p] = struct{}{}
		}

		for _, p := range rp.result.AddInclGlobal {
			if deduper.add(p) {
				peerAddInclGlobal = append(peerAddInclGlobal, p)
			}

			if oneLevelOnlyPaths != nil {
				delete(oneLevelOnlyPaths, p)
			}
		}
	}

	cflagsAggOrder := resolved

	if d.moduleStmt.Name == tokPy3Program {
		cflagsAggOrder = archiveOrder
	}

	for _, rp := range cflagsAggOrder {
		addEachARG(&cFlagsSeen, &peerCFlagsGlobal, rp.result.CFlagsGlobal)
		addEachARG(&cxxFlagsSeen, &peerCXXFlagsGlobal, rp.result.CXXFlagsGlobal)
		addEachARG(&cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, rp.result.COnlyFlagsGlobal)
		addEachARG(&objAddLibSeen, &peerObjAddLibsGlobal, rp.result.ObjAddLibsGlobal)
		addEachARG(&ldFlagsSeen, &peerLDFlagsGlobal, rp.result.LDFlagsGlobal)
		addEachARG(&rpathFlagsSeen, &peerRPathFlagsGlobal, rp.result.RPathFlagsGlobal)
	}

	// ONE_LEVEL paths from direct user peers must not re-propagate to transitive
	// consumers — upstream's one-hop semantics. Filter them from the list used for
	// the module result (AddInclGlobal); they remain in peerAddInclGlobal only for
	// the CC command of this module itself (via selfPeerAddInclGlobal below).
	peerAddInclForProp := peerAddInclGlobal

	if len(oneLevelOnlyPaths) > 0 {
		peerAddInclForProp = make([]VFS, 0, len(peerAddInclGlobal))

		for _, p := range peerAddInclGlobal {
			if _, isOneLevel := oneLevelOnlyPaths[p]; !isOneLevel {
				peerAddInclForProp = append(peerAddInclForProp, p)
			}
		}
	}

	effectiveAddInclGlobal := dedupVFS(d.addInclGlobal, peerAddInclForProp)

	// ProtoAddInclGlobal: this module's $(S)/<PROTO_NAMESPACE> contribution,
	// unioned with everything peers reported (transitive — every peer's
	// ProtoAddInclGlobal already includes its own peers' contributions).
	// Mirrors upstream's _PROTO__INCLUDE chain and feeds the proto compile
	// -I= block. peerContribs is not in scope here; iterate `resolved`.
	// Only PROTO_NAMESPACE GLOBAL contributes to the chain; a bare
	// PROTO_NAMESPACE propagates too, but trails the chain and reaches only
	// non-PROTO_LIBRARY consumers — see ProtoNamespaceTail.
	var ownProtoAddIncl []VFS
	var ownProtoTail []VFS

	if d.protoNamespace != nil {
		ns := source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))

		if d.protoNamespaceGlobal {
			ownProtoAddIncl = []VFS{ns}
		} else {
			ownProtoTail = []VFS{ns}
		}
	}

	// `ADDINCL GLOBAL FOR proto X` (yatool/build/conf/proto.conf:117-120
	// PROTO_ADDINCL macro; contrib/libs/protobuf ya.make) propagates an
	// additional -I=$X into the protoc command of every transitive
	// consumer. Append after the PROTO_NAMESPACE entry — declaration order
	// matches upstream's PROTO_ADDINCL macro placement.
	ownProtoAddIncl = append(ownProtoAddIncl, d.protoAddInclGlobal...)
	peerProtoAddInclGlobal := make([]VFS, 0, 4)

	deduper.reset()

	for _, rp := range resolved {
		for _, p := range rp.result.ProtoAddInclGlobal {
			if deduper.add(p) {
				peerProtoAddInclGlobal = append(peerProtoAddInclGlobal, p)
			}
		}
	}

	effectiveProtoAddInclGlobal := dedupVFS(ownProtoAddIncl, peerProtoAddInclGlobal)
	peerProtoTail := make([]VFS, 0, 1)

	deduper.reset()

	for _, rp := range resolved {
		for _, p := range rp.result.ProtoNamespaceTail {
			if deduper.add(p) {
				peerProtoTail = append(peerProtoTail, p)
			}
		}
	}

	effectiveProtoNamespaceTail := dedupVFS(ownProtoTail, peerProtoTail)

	if instance.Path == libraryPythonRuntimePy3 {
		buildRootPath := bldLibraryPythonRuntimePy3
		abseilPath := contribRestrictedAbseilCpp
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

	effectiveCFlagsGlobal := dedupARG(peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := dedupARG(peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := dedupARG(peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)
	effectiveRPathFlagsGlobal := dedupARG(peerRPathFlagsGlobal, d.rpathFlagsGlobal)

	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path.rel()] {
		// The libc++ addincl dirs are injected above (before effectiveAddInclGlobal);
		// only the matching -nostdinc++ flag and the runtime-stack hoist remain here.
		if !cxxFlagsSeen.has(uint32(baseUnitCxxNostdinc)) {
			cxxFlagsSeen.add(uint32(baseUnitCxxNostdinc))
			peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, baseUnitCxxNostdinc)
		}
	}

	ccRefs := make([]NodeRef, 0, len(d.srcs)+len(d.joinSrcs))
	ccOutputs := make([]VFS, 0, len(d.srcs)+len(d.joinSrcs))

	ccIsFlatNoLto := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))

	ccIsCFGenerated := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))

	ccIsProtoGenerated := make([]bool, 0, len(d.srcs)+len(d.joinSrcs))

	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	// Per upstream TModuleIncDirs (devtools/ymake/addincls.h:135): module's
	// own resolve uses BOTH local ADDINCL and ADDINCL(GLOBAL ...). The GLOBAL
	// tag means "also exposes to peers"; it does NOT mean "skipped from own
	// compile". We previously kept own and global in separate buckets, so a
	// module that declared its destination ADDINCL only as GLOBAL (the common
	// pattern for header.ya.make.inc COPY_FILE targets) couldn't resolve its
	// own COPY destinations and fell through to a peer's COPY of the same
	// header — emitting the wrong $(B) path in CC inputs.
	dedupedAddIncl := dedupVFS(d.addIncl, d.addInclGlobal)

	isPy3NativeLib := d.moduleStmt.Name == tokPy23NativeLibrary ||
		d.moduleStmt.Name == tokPy23Library

	var perModuleCCTag STR

	switch d.moduleStmt.Name {
	case tokPy23NativeLibrary:
		perModuleCCTag = tagPy3Native
	case tokPy23Library:
		perModuleCCTag = tagPy3
	case tokYqlUdfYdb, tokYqlUdfContrib:
		perModuleCCTag = tagYqlUdfStatic
	}

	var arNameFn func(string) string
	var globalArNameFn func(string) string
	archiveName := ""

	if len(d.moduleStmt.Args) > 0 {
		archiveName = d.moduleStmt.Args[0].string()
	}

	switch d.moduleStmt.Name {
	case tokPy23NativeLibrary:
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "libpy3c", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "libpy3c", archiveName) }
	case tokPy3Library, tokPy2Library, tokPy23Library, tokPy2Program, tokPy3Program:
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "libpy3", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "libpy3", archiveName) }
	default:
		arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, "lib", archiveName) }
		globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, "lib", archiveName) }
	}

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path.rel(), peerAddInclGlobal, dedupedAddIncl)

	// The cumulative SRCDIR search path (always non-empty: module dir at index 0,
	// then explicit SRCDIRs). A UNITTEST_FOR program also searches the tested
	// module's dir, appended last (highest precedence in the reversed search).
	effectiveSrcDirs := d.srcDirs

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		effectiveSrcDirs = append(append([]VFS{}, d.srcDirs...), dirKey(*pd))
	}

	moduleInputs := ModuleCCInputs{
		InclArgs:               ctx.inclArgs,
		Flags:                  d.flags,
		AddIncl:                dedupedAddIncl,
		PeerAddInclGlobal:      selfPeerAddInclGlobal,
		PeerProtoAddInclGlobal: effectiveProtoAddInclGlobal,
		ProtoNamespaceTail:     effectiveProtoNamespaceTail,
		CFlags:                 ownCFlags,
		CXXFlags:               d.cxxFlags,
		COnlyFlags:             d.cOnlyFlags,
		OwnCFlagsGlobal:        ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:      ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:    ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:       peerCFlagsGlobal,
		PeerCXXFlagsGlobal:     peerCXXFlagsGlobal,
		PeerCOnlyFlagsGlobal:   peerCOnlyFlagsGlobal,
		ModuleScopeCFlags:      d.moduleScopeCFlags,
		SFlags:                 d.sFlags,
		SrcDirs:                effectiveSrcDirs,
		FS:                     ctx.fs,
		DefaultVars:            d.defaultVars,
		DefaultVarOrder:        d.defaultVarOrder,
		SetVars:                d.setVars,
		Py3Suffix:              isPy3NativeLib,
		ObjectSuffixStem: func() *string {
			if isYqlUdfStaticModule(d.moduleStmt.Name) {
				return stringPtr("udfs")
			}

			return nil
		}(),
		ModuleTag:   perModuleCCTag,
		Ragel6Flags: d.ragel6Flags,
		BisonGenExt: d.bisonGenExt.string(),
		TC:          d.tc,
	}
	moduleInputs.ScanCfg = newScanContext(ctx.parsers, dedupedAddIncl, selfPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
	moduleInputs.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &moduleInputs)

	// Pass 1 (codegen-producing srcs: .proto, .ev, .fbs, .rl, .cpp.in, .c.in, .y)
	// runs BEFORE emitCopyFiles / emitEnumSrcs / emitMiscNodes. Those later
	// emitters walkClosure across the module's headers, populating the SHARED
	// childrenCache (scanner.childrenCache, keyed by file ID — not per-config).
	// If a header includes a codegen output (e.g. `<X.pb.h>` from gclogic.h),
	// the resolve must see the registered codegen entry; otherwise the empty
	// children list is cached and every later scanCtx — including the eventual
	// CC compile — reuses the stale "no pb.h" closure, AND the first scan also
	// raises a spurious unresolved-include warning. Pass 1 registers all
	// PB / EV / FL / RL / CF outputs in the codegen registry first, so the
	// subsequent header walks resolve them correctly.
	type codegenEmit struct {
		srcID STR
		emit  *SourceEmit
	}

	// Collected in SRCS order so pass 2 appends them without a side map: a source's
	// codegen-ness is exactly isCodegenProducingSrc(src), so pass 2 needs no
	// membership set, only this ordered list of the pre-emitted nodes.
	codegenEmits := make([]codegenEmit, 0, 4)

	// A .fbs's generated .h references its imported .fbs's .h, so every .fbs
	// producer must be registered before any .fbs CC closure is walked — exactly
	// what proto does via its two-phase emitCPPProtoSrcs (register all pb.h, then
	// compile). Emit the module's .fbs producers here; emitLibraryFlatcSource then
	// takes each producer's ref from the codegen registry and walks against a
	// complete registry.
	for _, src := range d.srcs {
		if srcExtClassOf(src) == srcExtFbs {
			emitFlatcProducer(ctx, instance, d, src.string())
		}
	}

	for _, src := range d.srcs {
		if !isCodegenProducingSrcID(src) {
			continue
		}

		srcRel := src.string()
		srcInputs := moduleInputs

		if extras := d.perSrcCFlagsFor(src); extras != nil {
			srcInputs.PerSourceCFlags = *extras
		}

		if d.flatSrc(src) {
			srcInputs.FlatOutput = true
		}

		codegenEmits = append(codegenEmits, codegenEmit{src, emitOneSource(ctx, instance, d, srcRel, srcInputs)})
	}

	emitCopyFiles(ctx, instance, d, &moduleInputs)

	enCCRes := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

	jvCCRefs, jvCCOutputs := emitMiscNodes(ctx, instance, d, &moduleInputs)

	prCCRes := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	pyCCRes := emitRunPythonForAR(ctx, instance, d, moduleInputs)
	emitArchives(ctx, instance, d)

	// Pass 2 splits d.srcs in two: non-codegen first (regular .cpp/.c/.h),
	// codegen-produced ccRefs second (their preEmitted CC nodes from Pass 1).
	// Upstream archives non-codegen objs first then codegen objs regardless
	// of their relative position in SRCS — fast_sax (SRCS: parser.rl6,
	// unescape.cpp) hands AR [unescape.cpp.o, parser.rl6.cpp.o]; tdigest
	// (SRCS: tdigest.cpp, tdigest.proto) hands AR [tdigest.cpp.o,
	// tdigest.pb.cc.o]. Iterating d.srcs in-order with `preEmitted[src]`
	// preserves SRCS order, so rl6-before-cpp modules diverge. Two passes
	// fix it without re-emitting and without changing any node content.
	emitSrcInputs := func(srcID STR, src string) ModuleCCInputs {
		si := moduleInputs

		if extras := d.perSrcCFlagsFor(srcID); extras != nil {
			si.PerSourceCFlags = *extras
		}

		if d.flatSrc(srcID) {
			si.FlatOutput = true
		}

		return adjustCythonCompanionSourceInputs(ctx.na, instance.Platform, d, src, si)
	}
	appendCC := func(srcID STR, emit *SourceEmit) {
		if emit == nil {
			return
		}

		cls := srcExtClassOf(srcID)
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, d.flatSrc(srcID))
		ccIsCFGenerated = append(ccIsCFGenerated, cls == srcExtCppIn || cls == srcExtCIn)
		ccIsProtoGenerated = append(ccIsProtoGenerated, cls == srcExtProto)
	}

	for _, src := range d.srcs {
		if isCodegenProducingSrcID(src) {
			continue
		}

		srcRel := src.string()
		appendCC(src, emitOneSource(ctx, instance, d, srcRel, emitSrcInputs(src, srcRel)))
	}

	for _, ce := range codegenEmits {
		appendCC(ce.srcID, ce.emit)
	}

	// Extra FLAT objects for SRC(file …) whose file is also in SRCS (the SRCS
	// occurrence already emitted its non-flat object above).
	for _, fe := range d.srcExtraFlat {
		si := moduleInputs
		si.FlatOutput = true

		if len(fe.Flags) > 0 {
			si.PerSourceCFlags = fe.Flags
		}

		if emit := emitOneSource(ctx, instance, d, fe.Src.string(), si); emit != nil {
			ccRefs = append(ccRefs, emit.Ref)
			ccOutputs = append(ccOutputs, emit.OutPath)
			ccIsFlatNoLto = append(ccIsFlatNoLto, true)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			ccIsProtoGenerated = append(ccIsProtoGenerated, false)
		}
	}

	for _, emit := range emitCheckConfigH(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		ccIsProtoGenerated = append(ccIsProtoGenerated, false)
	}

	for _, emit := range emitCythonCpp(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		ccIsProtoGenerated = append(ccIsProtoGenerated, false)
	}

	for _, emit := range emitSwigC(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)
		ccIsProtoGenerated = append(ccIsProtoGenerated, false)
	}

	for i, ref := range jvCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, jvCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		ccIsProtoGenerated = append(ccIsProtoGenerated, false)
	}

	if enCCRes != nil {
		for i, ref := range enCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, enCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			ccIsProtoGenerated = append(ccIsProtoGenerated, false)
		}
	}

	if prCCRes != nil {
		for i, ref := range prCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, prCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			ccIsProtoGenerated = append(ccIsProtoGenerated, false)
		}
	}

	if pyCCRes != nil {
		for i, ref := range pyCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, pyCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
			ccIsProtoGenerated = append(ccIsProtoGenerated, false)
		}
	}

	for _, e := range d.simdSrcs {
		variantIn := moduleInputs
		variantIn.FlatOutput = true
		variantIn.Variant = stringPtr(e.Variant)

		flags := internArgs(e.CFlags)

		if extras := d.perSrcCFlagsFor(e.Src); extras != nil {
			flags = append(flags, *extras...)
		}

		variantIn.PerSourceCFlags = flags

		emit := emitOneSource(ctx, instance, d, e.Src.string(), variantIn)

		if emit == nil {
			continue
		}

		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, true)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
		ccIsProtoGenerated = append(ccIsProtoGenerated, false)
	}

	numSrcsDerived := len(ccOutputs)

	for _, js := range d.joinSrcs {
		srcInstance := instance

		jsSources := strStrings(js.Sources)
		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, jsSources, moduleInputs)

		ccClosure := joinClosure

		if srcInstance.Platform.ISA == ISAX8664 {
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = rebasePerArchPeerAddIncl(moduleInputs.PeerAddInclGlobal, srcInstance.Platform.ISA, ctx.target.ISA)
			jsModuleInputs.ScanCfg = newScanContext(ctx.parsers, jsModuleInputs.AddIncl, jsModuleInputs.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, srcInstance, jsSources, jsModuleInputs)
		}

		jsRef, joinOutVFS := emitJS(srcInstance, js.OutputName, jsSources, joinClosure, ctx.target, d.tc, ctx.scripts, ctx.emit)

		jsRel := strings.TrimPrefix(joinOutVFS.rel(), srcInstance.Path.rel()+"/")

		ccIncludeInputs := jsCCIncludeInputs(srcInstance, joinOutVFS, jsSources, ccClosure, ctx.scripts)

		ccIn := moduleInputs
		ccIn.ExtraDepRefs = []NodeRef{jsRef}
		ccIn.IncludeInputs = ccIncludeInputs

		ref, outPath, _ := emitCC(srcInstance, jsRel, joinOutVFS, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
	}

	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]VFS, 0, len(d.globalSrcs))

	for _, src := range d.globalSrcs {
		emit := emitOneSource(ctx, instance, d, src.string(), moduleInputs)

		if emit == nil {
			continue
		}

		globalRefs = append(globalRefs, emit.Ref)
		globalOutputs = append(globalOutputs, emit.OutPath)
	}

	globalSrcMemberCount := len(globalRefs)

	regCCPy3Suffix := isPy3NativeLib || d.moduleStmt.Name == tokPy23Library
	regRes := emitPyRegister(ctx, instance, d, moduleInputs, regCCPy3Suffix)

	if regRes != nil {
		for i, ref := range regRes.Refs {
			globalRefs = append(globalRefs, ref)
			globalOutputs = append(globalOutputs, regRes.Outputs[i])
		}
	}

	ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins, d.tc)
	mergedLDPlugins := mergeLDPlugins(ownLDPlugins, &LdPluginsResult{
		Refs:  peerLDPluginRefs,
		Paths: peerLDPluginPaths,
	})

	if mergedLDPlugins == nil {
		mergedLDPlugins = &LdPluginsResult{}
	}

	// A module that compiled any C-family TU (ccRefs) invoked _SRC(cpp|cxx|cc|C|
	// c|m), each carrying .PEERDIR=$_SRC_CPP_TOOLCHAIN_INFO_PEER = clang_toolchain_info
	// under SBOM+CLANG (sbom.conf:9). Mirror that induced peer by folding its
	// toolchain SBOM component into the closure (the only thing it contributes —
	// it has no archive). Threads into both the program link and the library result.
	if ctx.sbomEnabled && env.bool(envCLANG) && len(ccRefs) > 0 {
		if r, p := clangToolchainSbomComponent(ctx, instance.Platform); r != nil {
			peerSbomRefs = append(peerSbomRefs, *r)
			peerSbomPaths = append(peerSbomPaths, *p)
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		binaryName := programBinaryName(instance, d.moduleStmt)

		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths
		ldPeerLinkCmdPaths := peerLinkCmdPaths

		if d.allocatorName == strFAKE {
			ldPeerArchiveRefs = make([]NodeRef, 0, len(peerArchiveRefs))
			ldPeerArchivePaths = make([]VFS, 0, len(peerArchivePaths))

			for i, p := range peerArchivePaths {
				if strings.HasPrefix(p.rel(), "library/cpp/malloc/api/") {
					continue
				}

				ldPeerArchiveRefs = append(ldPeerArchiveRefs, peerArchiveRefs[i])
				ldPeerArchivePaths = append(ldPeerArchivePaths, p)
			}
		}

		if d.moduleStmt.Name == tokPy3Program && d.allocatorName == strJ {
			ldPeerArchiveRefs, ldPeerArchivePaths = moveArchivePathsAfter(
				ldPeerArchiveRefs,
				ldPeerArchivePaths,
				bldBuildCowOnLibbuildCowOnA,
				[]VFS{
					bldLibraryCppMallocApiLibcppMallocApiA,
					bldContribLibsJemallocLibcontribLibsJemallocA,
					bldLibraryCppMallocJemallocLibcppMallocJemallocA,
				},
			)
			ldPeerLinkCmdPaths = movePathsAfter(
				ldPeerLinkCmdPaths,
				bldBuildCowOnLibbuildCowOnA,
				[]VFS{
					bldLibraryCppMallocApiLibcppMallocApiA,
					bldContribLibsJemallocLibcontribLibsJemallocA,
					bldLibraryCppMallocJemallocLibcppMallocJemallocA,
				},
			)
			ldPeerArchiveRefs, ldPeerArchivePaths = moveArchivePathsBefore(
				ldPeerArchiveRefs,
				ldPeerArchivePaths,
				bldLibraryCppJsonCommonLibcppJsonCommonA,
				[]VFS{
					bldToolsEnumParserEnumSerializationRuntimeLibtoolsEnumParserEnumSerializationRuntimeA,
				},
			)
			ldPeerLinkCmdPaths = movePathsBefore(
				ldPeerLinkCmdPaths,
				bldLibraryCppJsonCommonLibcppJsonCommonA,
				[]VFS{
					bldToolsEnumParserEnumSerializationRuntimeLibtoolsEnumParserEnumSerializationRuntimeA,
				},
			)
		}

		ldInstance := instance

		if d.moduleStmt.Name == tokPy2Program || d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin {
			ldInstance.Language = LangPy
		}

		ldCCRefs := ccRefs
		ldCCOutputs := ccOutputs
		ldCCRefs, ldCCOutputs = reorderLDMembers(ldCCRefs, ldCCOutputs)

		var ldObjcopyRefs []NodeRef
		var ldObjcopyPaths []VFS

		if resourceLibTagForData(d) != nil {
			// PY3_PROGRAM's paired PY3_LIBRARY genModule (kind=KindLib,
			// reached via the prepended self-PEERDIR at gen.go:610) already
			// ran emitPySrcs and registered the .yapyc3 codegen outputs in
			// the codegen registry. Re-emitting from the PROGRAM path would
			// panic on Register duplicates — call only emitResourceObjcopy,
			// which Emitter-dedups by output path so the LIBRARY's
			// already-emitted objcopy_<hash>.o is reused and its ref/path
			// reach this LD's objcopyPaths slot.
			objcopyRes := emitResourceObjcopy(ctx, instance, d)

			if objcopyRes != nil && len(objcopyRes.Refs) > 0 {
				ldObjcopyRefs = objcopyRes.Refs
				ldObjcopyPaths = objcopyRes.Outputs
			}
		}

		var ownRPathFlags []ARG

		if len(peerDynamicPaths) > 0 {
			ownRPathFlags = append([]ARG(nil), peerRPathFlagsGlobal...)
		}

		// Both PY3_PROGRAM (via its PY3_BIN submodule) and PY3_PROGRAM_BIN
		// inherit _BASE_PY3_PROGRAM, which calls STRIP() at conf/python.conf:884
		// (ENABLE(STRIP) → STRIP_FLAG=-Wl,--strip-all on linux per
		// build/conf/linkers/ld.conf:22). ENABLE(NO_STRIP) or BUILD_TYPE=DEBUG
		// reverts this (ymake.core.conf:2669).
		wantsStrip := (d.moduleStmt.Name == tokPy3ProgramBin || d.moduleStmt.Name == tokPy3Program) && !d.noStrip
		// Upstream's PY3_BIN submodule (the PROGRAM side of the PY3_PROGRAM
		// multimodule) has MODULE_TAG=PY3_BIN auto-set from the submodule
		// name (lang/confreader.cpp:847-848). REF exposes it lowercased in
		// the LD node's target_properties. The non-multimodule PY3_PROGRAM_BIN
		// has no implicit MODULE_TAG, so it stays unset there.
		var programModuleTag STR

		if d.moduleStmt.Name == tokPy3Program {
			programModuleTag = tagPy3Bin
		}

		var ownSbomRef *NodeRef
		var ownSbomPath *VFS

		if sbomActive(ctx, instance) && sbomQualifies(d) {
			ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, binaryName)
		}

		// _GENERATE_EXTRA_OBJS collects ${ext=.component.sbom:SRCS_GLOBAL} into the
		// link only under EMBED_SBOM && BUILD_TYPE∈RELEASE (sbom.conf:26): a debug
		// program (ya-bin) links licensed libs without pulling their components.
		var ldSbomRefs []NodeRef
		var ldSbomPaths []VFS

		if instance.Platform.BuildRelease {
			ldSbomRefs = peerSbomRefs
			ldSbomPaths = peerSbomPaths

			if ownSbomRef != nil {
				ldSbomRefs = append(append([]NodeRef(nil), peerSbomRefs...), *ownSbomRef)
				ldSbomPaths = append(append([]VFS(nil), peerSbomPaths...), *ownSbomPath)
			}
		}

		ldRef := emitLD(
			ldInstance,
			binaryName,
			ldCCRefs, ldCCOutputs,
			ldPeerArchiveRefs, ldPeerArchivePaths,
			ldPeerLinkCmdPaths,
			mergedLDPlugins.Refs, mergedLDPlugins.Paths,
			peerGlobalRefs, peerGlobalPaths,
			peerWholeArchiveRefs, peerWholeArchivePaths,
			peerWholeArchiveCmdPaths,
			peerDynamicRefs, peerDynamicPaths,
			ldObjcopyRefs, ldObjcopyPaths,
			ldSbomRefs, ldSbomPaths,
			ownCFlags,
			peerCFlagsGlobal,
			d.moduleScopeCFlags,
			peerLDFlagsGlobal,
			d.ldFlags,
			ownRPathFlags,
			peerRPathFlagsGlobal,
			peerObjAddLibsGlobal,
			d.exportsScript,
			d.flags.NoCompilerWarnings,
			wantsStrip,
			d.splitDwarf,
			programModuleTag,
			d.tc,
			ctx.host,
			ctx.scripts,
			ctx.emit,
		)
		ldPath := lDOutputPath(instance, binaryName)
		var suiteInfo *TestSuiteInfo

		if ctx.testMode && d.moduleStmt.Name == tokUnittestFor {
			suiteInfo = buildTestSuiteInfo(instance, d, ldPath)
		}

		result := &ModuleEmitResult{
			ARRef:                           ldRef,
			ARPath:                          &ldPath,
			isPROGRAM:                       true,
			isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
			LDRef:                           ldRef,
			LDPath:                          &ldPath,
			AddInclGlobal:                   effectiveAddInclGlobal,
			OwnAddInclGlobal:                d.addInclGlobal,
			ProtoAddInclGlobal:              effectiveProtoAddInclGlobal,
			ProtoNamespaceTail:              effectiveProtoNamespaceTail,
			AddInclOneLevel:                 d.addInclOneLevel,
			AddInclUserGlobal:               d.addInclUserGlobal,
			CFlagsGlobal:                    effectiveCFlagsGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			ObjAddLibsGlobal:                dedupARG(peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   dedupARG(peerLDFlagsGlobal, d.ldFlags),
			RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
			PeerArchiveClosureRefs:          peerArchiveRefs,
			PeerArchiveClosurePaths:         peerArchivePaths,
			PeerGlobalClosureRefs:           peerGlobalRefs,
			PeerGlobalClosurePaths:          peerGlobalPaths,
			PeerWholeArchiveClosureRefs:     peerWholeArchiveRefs,
			PeerWholeArchiveClosurePaths:    peerWholeArchivePaths,
			PeerWholeArchiveCmdClosurePaths: peerWholeArchiveCmdPaths,
			LDPluginRefs:                    mergedLDPlugins.Refs,
			LDPluginPaths:                   mergedLDPlugins.Paths,
			PeerDynamicClosureRefs:          peerDynamicRefs,
			PeerDynamicClosurePaths:         peerDynamicPaths,
			SbomComponentRef:                ownSbomRef,
			SbomComponentPath:               ownSbomPath,
			PeerSbomClosureRefs:             peerSbomRefs,
			PeerSbomClosurePaths:            peerSbomPaths,
			InducedDeps:                     d.inducedDeps,
			Peerdirs:                        d.peerdirs,
			ModuleStmtName:                  d.moduleStmt.Name,
			ResourceGlobalClosure:           resourceGlobalsClosure,
			testSuiteInfo:                   suiteInfo,
		}
		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, ccIsProtoGenerated, numSrcsDerived)

	var arRef NodeRef
	arBaseName := arNameFn(instance.Path.rel())

	arInstance := instance

	switch d.moduleStmt.Name {
	case tokPy3Library, tokPy2Library, tokPy23Library, tokPy2Program, tokPy3Program:
		arInstance.Language = LangPy
	}

	var arPluginVFS *VFS

	if d.arPlugin != nil {
		v := source(instance.Path.rel() + "/" + d.arPlugin.string())
		arPluginVFS = &v
	}

	emitPySrcs(ctx, instance, d)

	genPyAuxRes := emitGeneratedPyAuxChunks(ctx, instance, d, moduleInputs)

	if genPyAuxRes != nil {
		globalRefs = append(globalRefs, genPyAuxRes.Refs...)
		globalOutputs = append(globalOutputs, genPyAuxRes.Outputs...)
	}

	emitLLVMBC(ctx, instance, d, moduleInputs, resourceGlobalsClosure)

	objcopyRes := emitResourceObjcopy(ctx, instance, d)

	if objcopyRes != nil {
		globalRefs = append(globalRefs, objcopyRes.Refs...)
		globalOutputs = append(globalOutputs, objcopyRes.Outputs...)

		// Upstream always places RESOURCE objcopy objects before SRCS(GLOBAL)
		// objects in the global archive regardless of their declaration order.
		// This applies only when there are explicit RESOURCE entries (d.resources);
		// pySrc objcopy from PY_SRCS follows declaration order instead.
		if globalSrcMemberCount > 0 && len(objcopyRes.Refs) > 0 && len(d.resources) > 0 {
			globalRefs = moveTailNodeRefsToFront(globalRefs, len(objcopyRes.Refs))
			globalOutputs = moveTailVFSToFront(globalOutputs, len(objcopyRes.Outputs))
		}
	}

	if len(ccRefs) > 0 {
		if perModuleCCTag != 0 {
			arRef = emitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = emitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		}
	}

	var arPath *VFS

	if len(ccRefs) > 0 {
		arPath = vfsPtr(build(instance.Path.rel() + "/" + arBaseName))
	}

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		realPrjName := strings.TrimSuffix(archiveNameWithPrefixOrName(instance.Path.rel(), "", archiveName), ".a")
		ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, realPrjName)
	}

	result := &ModuleEmitResult{
		ARRef:                           arRef,
		ARPath:                          arPath,
		isPROGRAM:                       false,
		isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
		LDRef:                           arRef,
		LDPath:                          arPath,
		AddInclGlobal:                   effectiveAddInclGlobal,
		OwnAddInclGlobal:                d.addInclGlobal,
		ProtoAddInclGlobal:              effectiveProtoAddInclGlobal,
		ProtoNamespaceTail:              effectiveProtoNamespaceTail,
		AddInclOneLevel:                 d.addInclOneLevel,
		AddInclUserGlobal:               d.addInclUserGlobal,
		CFlagsGlobal:                    effectiveCFlagsGlobal,
		CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
		COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
		ObjAddLibsGlobal:                dedupARG(peerObjAddLibsGlobal, d.objAddLibsGlobal),
		LDFlagsGlobal:                   dedupARG(peerLDFlagsGlobal, d.ldFlags),
		RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
		PeerArchiveClosureRefs:          peerArchiveRefs,
		PeerArchiveClosurePaths:         peerArchivePaths,
		PeerGlobalClosureRefs:           peerGlobalRefs,
		PeerGlobalClosurePaths:          peerGlobalPaths,
		PeerWholeArchiveClosureRefs:     peerWholeArchiveRefs,
		PeerWholeArchiveClosurePaths:    peerWholeArchivePaths,
		PeerWholeArchiveCmdClosurePaths: peerWholeArchiveCmdPaths,
		LDPluginRefs:                    mergedLDPlugins.Refs,
		LDPluginPaths:                   mergedLDPlugins.Paths,
		PeerDynamicClosureRefs:          peerDynamicRefs,
		PeerDynamicClosurePaths:         peerDynamicPaths,
		SbomComponentRef:                ownSbomRef,
		SbomComponentPath:               ownSbomPath,
		PeerSbomClosureRefs:             peerSbomRefs,
		PeerSbomClosurePaths:            peerSbomPaths,
		InducedDeps:                     d.inducedDeps,
		Peerdirs:                        d.peerdirs,
		ModuleStmtName:                  d.moduleStmt.Name,
		ResourceGlobalClosure:           resourceGlobalsClosure,
	}

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path.rel())

		globalTag := tagGlobal

		switch d.moduleStmt.Name {
		case tokPy23Library:
			globalTag = tagPy3Global
		case tokPy23NativeLibrary:
			globalTag = tagPy3NativeGlobal
		case tokYqlUdfYdb, tokYqlUdfContrib:
			globalTag = tagYqlUdfStaticGlobal
		}

		// The PY3_BIN_LIB submodule (KindLib half of PY3_PROGRAM multimodule)
		// composes its global.a tag from <MODULE_TAG>_global; the lang dump
		// expects "py3_bin_lib_global".
		if d.programPairedLib {
			globalTag = tagPy3BinLibGlobal
		}

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), len(globalRefs))
		globalRef := emitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, d.tc, ctx.host, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(build(instance.Path.rel() + "/" + globalBaseName))
	}

	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

func filterBuildRootSelfPaths(instancePath string, peer, own []VFS) []VFS {
	if len(peer) == 0 {
		return peer
	}

	ownPrefix := build(instancePath)
	deduper.reset()
	matched := false

	for _, p := range own {
		if p.isBuild() && (p == ownPrefix || strings.HasPrefix(p.rel(), ownPrefix.rel()+"/")) {
			deduper.add(p)
			matched = true
		}
	}

	if !matched {
		return peer
	}

	out := make([]VFS, 0, len(peer))

	for _, p := range peer {
		if deduper.has(p) {
			continue
		}

		out = append(out, p)
	}

	return out
}

func filterEnSerializedSiblings(in []VFS) []VFS {
	out := make([]VFS, 0, len(in))

	for _, p := range in {
		if strings.HasSuffix(p.rel(), "_serialized.cpp") || strings.HasSuffix(p.rel(), "_serialized.h") {
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

	deduper.reset()

	for _, path := range moved {
		deduper.add(path)
	}

	outRefs := make([]NodeRef, 0, len(refs))
	outPaths := make([]VFS, 0, len(paths))
	movedRefs := make(map[VFS]NodeRef, len(moved))
	movedPaths := make(map[VFS]VFS, len(moved))

	for i, path := range paths {
		if deduper.has(path) {
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

func movePathsAfter(paths []VFS, anchor VFS, moved []VFS) []VFS {
	if len(moved) == 0 {
		return paths
	}

	deduper.reset()

	for _, path := range moved {
		deduper.add(path)
	}

	outPaths := make([]VFS, 0, len(paths))
	movedPaths := make(map[VFS]VFS, len(moved))

	for _, path := range paths {
		if deduper.has(path) {
			movedPaths[path] = path

			continue
		}

		outPaths = append(outPaths, path)

		if path == anchor {
			for _, movedPath := range moved {
				if p, ok := movedPaths[movedPath]; ok {
					outPaths = append(outPaths, p)
				}
			}
		}
	}

	if len(outPaths) != len(paths) {
		return paths
	}

	return outPaths
}

func moveArchivePathsBefore(refs []NodeRef, paths []VFS, anchor VFS, moved []VFS) ([]NodeRef, []VFS) {
	if len(moved) == 0 || len(refs) != len(paths) {
		return refs, paths
	}

	deduper.reset()

	for _, path := range moved {
		deduper.add(path)
	}

	movedRefs := make(map[VFS]NodeRef, len(moved))
	movedPaths := make(map[VFS]VFS, len(moved))

	for i, path := range paths {
		if deduper.has(path) {
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
		if deduper.has(path) {
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

func movePathsBefore(paths []VFS, anchor VFS, moved []VFS) []VFS {
	if len(moved) == 0 {
		return paths
	}

	deduper.reset()

	for _, path := range moved {
		deduper.add(path)
	}

	movedPaths := make(map[VFS]VFS, len(moved))

	for _, path := range paths {
		if deduper.has(path) {
			movedPaths[path] = path
		}
	}

	if len(movedPaths) != len(moved) {
		return paths
	}

	outPaths := make([]VFS, 0, len(paths))

	for _, path := range paths {
		if deduper.has(path) {
			continue
		}

		if path == anchor {
			for _, movedPath := range moved {
				if p, ok := movedPaths[movedPath]; ok {
					outPaths = append(outPaths, p)
				}
			}
		}

		outPaths = append(outPaths, path)
	}

	if len(outPaths) != len(paths) {
		return paths
	}

	return outPaths
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

func mergeLDPlugins(own, peer *LdPluginsResult) *LdPluginsResult {
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

	deduper.reset()
	out := &LdPluginsResult{
		Refs:  make([]NodeRef, 0, len(ownPaths)+len(peerPaths)),
		Paths: make([]VFS, 0, len(ownPaths)+len(peerPaths)),
	}

	for i, p := range ownPaths {
		if !deduper.add(p) {
			continue
		}

		out.Refs = append(out.Refs, ownRefs[i])
		out.Paths = append(out.Paths, p)
	}

	for i, p := range peerPaths {
		if !deduper.add(p) {
			continue
		}

		out.Refs = append(out.Refs, peerRefs[i])
		out.Paths = append(out.Paths, p)
	}

	return out
}

type PeerGlobalContribs struct {
	addIncl            []VFS
	protoAddIncl       []VFS
	protoNamespaceTail []VFS
	cFlags             []ARG
	cxxFlags           []ARG
	cOnlyFlags         []ARG
	objAddLibs         []ARG
	ldFlags            []ARG
	rpathFlags         []ARG

	archiveRefs  []NodeRef
	archivePaths []VFS

	globalRefs  []NodeRef
	globalPaths []VFS

	wholeArchiveRefs     []NodeRef
	wholeArchivePaths    []VFS
	wholeArchiveCmdPaths []VFS

	ldPluginRefs  []NodeRef
	ldPluginPaths []VFS
	dynamicRefs   []NodeRef
	dynamicPaths  []VFS

	sbomRefs  []NodeRef
	sbomPaths []VFS

	// resourceGlobals is the transitive resource-global closure aggregated across
	// peers (deduped by global-var name), the source for resolveModuleToolchain in
	// the specialized/header-only path (the general path folds it inline instead).
	resourceGlobals []ResourceDecl
}

func walkPeersForGlobalAddIncl(ctx *GenCtx, instance ModuleInstance, d *ModuleData) PeerGlobalContribs {
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)
	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))

	// Resolve every peer through genModule first (memoized; the recursion may
	// re-enter the deduper), then aggregate per output kind below in sequential
	// leaf passes. The visited guard stays a local string-keyed map because it
	// must stay live across the genModule calls.
	resolved := make([]*ModuleEmitResult, 0, len(defaults)+len(d.peerdirs))

	walkInstance := func(peerInstance ModuleInstance) {
		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	walk := func(peerPath string) {
		walkInstance(derivePeerInstance(ctx, instance, d, peerPath))
	}

	// PY3_PROTO multimodule (proto.conf `module _PY3_PROTO`): the python proto
	// submodule PEERDIRs its CPP_PROTO sibling, ahead of the python-runtime
	// PEERDIRs (contrib/python/protobuf, contrib/python/grpcio added in
	// genModule). The sibling is the same unit at the same path with CPP flags —
	// peer that exact instance first so its archive (and closure) lands ahead of
	// protobuf-py3 in the link order. Upstream drops this self-peer under
	// NO_OPTIMIZE_PY_PROTOS (`when ($OPTIMIZE_PY_PROTOS_FLAG == "no") {
	// _IGNORE_PEERDIRSELF=CPP_PROTO }`), leaving the CPP archive whole-archive-only
	// (emitPyProtoSrcs still marks it whole-archive in both cases).
	if instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary && d.optimizePyProtos && !moduleExcludesTag(d, "CPP_PROTO") {
		seen[instance.Path.rel()] = struct{}{}
		cppSelf := instance
		cppSelf.Language = LangCPP
		walkInstance(cppSelf)
	}

	if d.useCommonGoogleAPIs && instance.Language == LangCPP {
		const googleapisPeer = "contrib/libs/googleapis-common-protos"

		if _, dup := seen[googleapisPeer]; !dup {
			seen[googleapisPeer] = struct{}{}
			walk(googleapisPeer)
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
		if _, dup := seen[p.string()]; dup {
			continue
		}

		seen[p.string()] = struct{}{}
		walk(filepath.Clean(p.string()))
	}

	out := PeerGlobalContribs{}

	// Resource globals, deduped by global-var STR cast into the run-wide
	// VFS-keyed deduper (single-namespace leaf pass, as in genModule).
	deduper.reset()

	for _, pr := range resolved {
		for _, decl := range pr.ResourceGlobalClosure {
			if deduper.add(VFS(decl.GlobalVar)) {
				out.resourceGlobals = append(out.resourceGlobals, decl)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.AddInclGlobal {
			if deduper.add(p) {
				out.addIncl = append(out.addIncl, p)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.ProtoAddInclGlobal {
			if deduper.add(p) {
				out.protoAddIncl = append(out.protoAddIncl, p)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.ProtoNamespaceTail {
			if deduper.add(p) {
				out.protoNamespaceTail = append(out.protoNamespaceTail, p)
			}
		}
	}

	// Flag unions are several independent ARG sets, so they share one pass over
	// local BitSets instead of the single-set deduper.
	var cFlagsSeen BitSet
	var cxxFlagsSeen BitSet
	var cOnlyFlagsSeen BitSet
	var objAddLibSeen BitSet
	var ldFlagsSeen BitSet
	var rpathFlagsSeen BitSet

	for _, pr := range resolved {
		addEachARG(&cFlagsSeen, &out.cFlags, pr.CFlagsGlobal)
		addEachARG(&cxxFlagsSeen, &out.cxxFlags, pr.CXXFlagsGlobal)
		addEachARG(&cOnlyFlagsSeen, &out.cOnlyFlags, pr.COnlyFlagsGlobal)
		addEachARG(&objAddLibSeen, &out.objAddLibs, pr.ObjAddLibsGlobal)
		addEachARG(&ldFlagsSeen, &out.ldFlags, pr.LDFlagsGlobal)
		addEachARG(&rpathFlagsSeen, &out.rpathFlags, pr.RPathFlagsGlobal)
	}

	// archive: closure paths, then the peer's own AR output (per peer).
	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p) {
				out.archiveRefs = append(out.archiveRefs, pr.PeerArchiveClosureRefs[i])
				out.archivePaths = append(out.archivePaths, p)
			}
		}

		if pr.ARPath != nil && deduper.add(*pr.ARPath) {
			out.archiveRefs = append(out.archiveRefs, pr.ARRef)
			out.archivePaths = append(out.archivePaths, *pr.ARPath)
		}
	}

	// sbom: the SBOM component of every peer in the link closure (mirrors archive).
	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.PeerSbomClosurePaths {
			if deduper.add(p) {
				out.sbomRefs = append(out.sbomRefs, pr.PeerSbomClosureRefs[i])
				out.sbomPaths = append(out.sbomPaths, p)
			}
		}

		if pr.SbomComponentRef != nil && deduper.add(*pr.SbomComponentPath) {
			out.sbomRefs = append(out.sbomRefs, *pr.SbomComponentRef)
			out.sbomPaths = append(out.sbomPaths, *pr.SbomComponentPath)
		}
	}

	// global: closure paths, then the peer's own GLOBAL output.
	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.PeerGlobalClosurePaths {
			if deduper.add(p) {
				out.globalRefs = append(out.globalRefs, pr.PeerGlobalClosureRefs[i])
				out.globalPaths = append(out.globalPaths, p)
			}
		}

		if pr.GlobalRef != nil && pr.GlobalPath != nil && deduper.add(*pr.GlobalPath) {
			out.globalRefs = append(out.globalRefs, *pr.GlobalRef)
			out.globalPaths = append(out.globalPaths, *pr.GlobalPath)
		}
	}

	// wholeArchive: closure paths, then the peer's own whole-archive paths.
	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.PeerWholeArchiveClosurePaths {
			if deduper.add(p) {
				out.wholeArchiveRefs = append(out.wholeArchiveRefs, pr.PeerWholeArchiveClosureRefs[i])
				out.wholeArchivePaths = append(out.wholeArchivePaths, p)
			}
		}

		for i, p := range pr.WholeArchivePaths {
			if deduper.add(p) {
				out.wholeArchiveRefs = append(out.wholeArchiveRefs, pr.WholeArchiveRefs[i])
				out.wholeArchivePaths = append(out.wholeArchivePaths, p)
			}
		}
	}

	// wholeArchiveCmd: command-line whole-archive paths (no refs).
	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.PeerWholeArchiveCmdClosurePaths {
			if deduper.add(p) {
				out.wholeArchiveCmdPaths = append(out.wholeArchiveCmdPaths, p)
			}
		}

		for _, p := range pr.WholeArchiveCmdPaths {
			if deduper.add(p) {
				out.wholeArchiveCmdPaths = append(out.wholeArchiveCmdPaths, p)
			}
		}
	}

	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.LDPluginPaths {
			if deduper.add(p) {
				out.ldPluginRefs = append(out.ldPluginRefs, pr.LDPluginRefs[i])
				out.ldPluginPaths = append(out.ldPluginPaths, p)
			}
		}
	}

	// dynamic: closure paths, then the peer's own DYNAMIC_LIBRARY output.
	deduper.reset()

	for _, pr := range resolved {
		for i, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p) {
				out.dynamicRefs = append(out.dynamicRefs, pr.PeerDynamicClosureRefs[i])
				out.dynamicPaths = append(out.dynamicPaths, p)
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(*pr.LDPath) {
			out.dynamicRefs = append(out.dynamicRefs, pr.LDRef)
			out.dynamicPaths = append(out.dynamicPaths, *pr.LDPath)
		}
	}

	return out
}

func isHeaderSource(srcRel string) bool {
	switch {
	case strings.HasSuffix(srcRel, ".h"),
		strings.HasSuffix(srcRel, ".hh"),
		strings.HasSuffix(srcRel, ".hpp"),
		strings.HasSuffix(srcRel, ".cuh"),
		strings.HasSuffix(srcRel, ".H"),
		strings.HasSuffix(srcRel, ".hxx"),
		strings.HasSuffix(srcRel, ".xh"),
		strings.HasSuffix(srcRel, ".ipp"),
		strings.HasSuffix(srcRel, ".ixx"),
		strings.HasSuffix(srcRel, ".inl"):
		return true
	}

	return false
}

func isCodegenProducingSrc(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".fbs") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".y") ||
		strings.HasSuffix(srcRel, ".cpp.in") ||
		strings.HasSuffix(srcRel, ".c.in")
}

func reorderLDMembers(refs []NodeRef, paths []VFS) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
	}

	regular := make([]member, 0, len(paths))
	legacy := make([]member, 0, len(paths))

	for i, path := range paths {
		m := member{path: path}

		if i < len(refs) {
			m.ref = refs[i]
		}

		if strings.Contains(path.rel(), "/_/_/") {
			legacy = append(legacy, m)

			continue
		}

		regular = append(regular, m)
	}

	out := append(regular, legacy...)
	outRefs := make([]NodeRef, len(out))
	outPaths := make([]VFS, len(out))

	for i, m := range out {
		outRefs[i] = m.ref
		outPaths[i] = m.path
	}

	return outRefs, outPaths
}

func reorderARMembers(refs []NodeRef, paths []VFS, isFlatNoLto []bool, isCFGenerated []bool, isProtoGenerated []bool, numSrcsDerived int) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
	}

	var noLtoSrcs, regularSrcs, cfSrcs, g4Srcs, hSerSrcs, evPbSrcs, pbCCSrcs, rl6Srcs, reg3Srcs, legacyR6 []member

	for i := 0; i < numSrcsDerived && i < len(paths); i++ {
		m := member{refs[i], paths[i]}
		rel := m.path.rel()

		switch {
		case strings.Contains(rel, "/_/_/"):
			legacyR6 = append(legacyR6, m)
		case i < len(isFlatNoLto) && isFlatNoLto[i]:
			noLtoSrcs = append(noLtoSrcs, m)
		case strings.Contains(rel, ".reg3.cpp") && strings.HasSuffix(rel, ".o"):
			reg3Srcs = append(reg3Srcs, m)
		case strings.HasSuffix(rel, ".rl6.cpp.o"):
			rl6Srcs = append(rl6Srcs, m)
		case strings.HasSuffix(rel, ".ev.pb.cc.o"):
			evPbSrcs = append(evPbSrcs, m)
		// pb.cc.o generated from a .proto SRCS entry (not a direct .pb.cc source
		// file) goes after h_serialized — this matches upstream's ordering where
		// the proto codegen output follows the enum serialization outputs.
		case i < len(isProtoGenerated) && isProtoGenerated[i] && strings.HasSuffix(rel, ".pb.cc.o"):
			pbCCSrcs = append(pbCCSrcs, m)
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

	joinSrcs := make([]member, 0, len(paths)-numSrcsDerived)

	for i := numSrcsDerived; i < len(paths); i++ {
		joinSrcs = append(joinSrcs, member{refs[i], paths[i]})
	}

	out := make([]member, 0, len(paths))
	out = append(out, noLtoSrcs...)
	out = append(out, regularSrcs...)
	out = append(out, cfSrcs...)
	out = append(out, joinSrcs...)
	out = append(out, g4Srcs...)
	out = append(out, hSerSrcs...)
	out = append(out, evPbSrcs...)
	out = append(out, pbCCSrcs...)
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

func (ctx *GenCtx) tool(modulePath ARG) (NodeRef, VFS) {
	res := ctx.toolResult(modulePath)

	return res.LDRef, *res.LDPath
}

func (ctx *GenCtx) toolResult(modulePath ARG) *ModuleEmitResult {
	if res, ok := ctx.tools.get(modulePath); ok {
		return res
	}

	res := genModule(ctx, newToolInstance(ctx.host, modulePath.string()))

	// Cache (and map the tool's LD node back to its result) only once it really
	// built: a tolerated PEERDIR cycle yields an empty stub with LDRef 0 that
	// genModule does NOT memoize, so caching it here would poison later lookups
	// (the tool would keep its empty InducedDeps forever instead of rebuilding).
	if res.LDRef != NodeRef(0) {
		ctx.tools.put(modulePath, res)
		ctx.moduleByRef.put(res.LDRef, res)
	}

	return res
}

func (ctx *GenCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	return ctx.scannerForPlatform(instance.Platform)
}

// instanceKey packs a ModuleInstance into the genModule memo key: the path
// STR id is unique per path, Kind/Language are tiny enums, and a run carries
// exactly two platforms — anything else fails fast.
func (ctx *GenCtx) instanceKey(in ModuleInstance) uint64 {
	pbit := uint64(0)

	if in.Platform == ctx.host {
		pbit = 1
	} else if in.Platform != ctx.target {
		throwFmt("instanceKey: unknown platform for %s", in.Path.string())
	}

	return uint64(in.Path)<<16 | uint64(in.Kind)<<8 | uint64(in.Language)<<1 | pbit
}

func (ctx *GenCtx) scannerForPlatform(p *Platform) *IncludeScanner {
	if p == ctx.host {
		return ctx.scannerHost
	}

	return ctx.scannerTarget
}
