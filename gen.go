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

type moduleEmitResult struct {
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

	InducedDeps parsedIncludeSet

	Peerdirs []string

	ModuleStmtName TOK

	testSuiteInfo *testSuiteInfo

	// ResourceGlobalClosure is the transitive union of external-resource globals
	// (<NAME>_RESOURCE_GLOBAL) reachable through this module's PEERDIR closure,
	// deduped by global-var name in first-seen order. A RESOURCES_LIBRARY seeds it
	// with its own DECLARE_EXTERNAL_RESOURCE declarations; every module folds in its
	// peers'. Consumers (test-run nodes) render it into --global-resource lists.
	ResourceGlobalClosure []resourceDecl
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

type genCtx struct {
	sourceRoot string
	fs         FS
	parsers    *includeParserManager
	emit       Emitter

	// inclArgValues backs inclArgMemo (the "-I<path>" cache); owned here so future
	// VFS-keyed value columns can share its idx array. inclArgs points at it.
	inclArgValues   DenseMap[VFS, STR]
	inclArgs        inclArgMemo
	memo            map[ModuleInstance]*moduleEmitResult
	walking         map[ModuleInstance]bool
	cyclesTolerated int

	traceStack []string

	scannerTarget *IncludeScanner
	scannerHost   *IncludeScanner

	flatcEmissions map[codegenOutputKey]flatcEmission

	pyRegisterOutputs map[VFS]NodeRef

	checkConfigOutputs map[VFS]NodeRef

	ldPluginCPCache map[VFS]NodeRef

	// moduleByRef maps a module's LD NodeRef back to its emit result, populated in
	// toolResult (so every codegen tool resolved via ctx.tool is reachable by ref).
	// The include scanner uses it to pull a generated file's producing tools'
	// INDUCED_DEPS into the file's closure, via GeneratedFileInfo.GeneratorRefs —
	// so a tool's induced runtime headers come from its declared INDUCED_DEPS, not
	// a per-emitter hardcoded list. Scanners hold a pointer to it.
	moduleByRef DenseMap[NodeRef, *moduleEmitResult]

	// tools caches a codegen tool's emit result by its module-path ARG, so repeated
	// ctx.tool(argX) lookups skip rebuilding the ModuleInstance + memo probe.
	tools DenseMap[ARG, *moduleEmitResult]

	// scripts maps each build/scripts script VFS to [self, …transitive import
	// closure]; emit sites add a build script via append(inputs, scripts[v]...).
	scripts scriptDeps

	// fetchRefs maps an external-resource name (CLANG, LLD_ROOT, …) to its FETCH
	// node, emitted once when the declaring RESOURCES_LIBRARY is gen'd
	// (emitResourceFetch). Consumers that reference $(NAME) take the dep from here.
	// Shared with the resource-aware emitter so attachResourceDeps resolves it.
	fetchRefs map[string]NodeRef

	host   *Platform
	target *Platform

	testMode bool

	// tarjan is the run-wide Tarjan/closure working state; both scanners hold a
	// pointer to it (their tjc field) so its vfsBound-sized arrays grow once, not
	// once per scanner. reset() runs before every use, so the shared state is safe
	// under single-threaded gen.
	tarjan tarjanCtx
}

type codegenOutputKey struct {
	platform *Platform
	path     VFS
}

type scanCtxPerfStats struct {
	subgraphEntries int
	childrenEntries int
	closureWindows  int
}

func resolveCodegenDepRefs(ctx *genCtx, consumer ModuleInstance, includeInputs []VFS, exclude ...NodeRef) []NodeRef {
	return resolveCodegenDepRefsExt(ctx, consumer, includeInputs, nil, exclude...)
}

func resolveCodegenDepRefsExt(ctx *genCtx, consumer ModuleInstance, includeInputs, inputs []VFS, exclude ...NodeRef) []NodeRef {
	if len(includeInputs) == 0 && len(inputs) == 0 {
		return nil
	}

	// Reuse the per-run dedup map (genCtx) instead of allocating one per call —
	// its growth was ~25MB of churn across ~22k calls. Cleared on entry; keeps its
	// grown buckets between calls.
	// Dedup the producer refs through the run-wide VFS deduper (NodeRef is a
	// ~uint32 id, cast to VFS at the IdSet boundary — a different typedef over the
	// same dense space). probe touches no other deduper user (EmitCF takes no
	// ctx), so reset-then-stream here is safe.
	deduper.reset()

	for _, r := range exclude {
		deduper.add(VFS(r))
	}

	var out []NodeRef

	// All codegen producer refs (PB/EV/EN, and CP/CF) live on the codegen
	// registry entry's ProducerRef, so one reg.Lookup resolves every kind —
	// no per-kind side maps. Hoisted: invariant across the probes.
	reg := codegenRegForInstance(ctx, consumer)

	probe := func(v VFS) {
		var ref NodeRef
		var ok bool

		if reg != nil {
			if info := reg.Lookup(v); info != nil {
				if !info.HasProducerRef && info.DeferredCF != nil {
					def := info.DeferredCF
					cfRef, _ := EmitCF(def.instance, def.srcVFS, def.outVFS, def.cfgVars, def.includeInputs, consumer.Path.Rel(), 0, def.tc, ctx.emit)
					reg.SetProducerRef(v, cfRef)
				}

				if info.HasProducerRef {
					ref, ok = info.ProducerRef, true
				}
			}
		}

		if !ok {
			return
		}

		if !deduper.add(VFS(ref)) {
			return
		}

		out = append(out, ref)
	}

	// The IsBuild gate stays in the loops: it inlines there, and the dominant
	// cost was a closure call per element of a whole include closure just to
	// bounce off this bit for the (vast) $(S) majority.
	for _, p := range includeInputs {
		if p.IsBuild() {
			probe(p)
		}
	}

	for _, p := range inputs {
		if p.IsBuild() {
			probe(p)
		}
	}

	return out
}

func (ctx *genCtx) perfScanCtxStats(scanner *IncludeScanner) scanCtxPerfStats {
	return scanCtxPerfStats{
		// subgraph and children are columns of one DenseMap3 keyed per node, so
		// both report its distinct-key count.
		subgraphEntries: scanner.scanCache.Len(),
		childrenEntries: scanner.scanCache.Len(),
		closureWindows:  len(scanner.subgraphClosures),
	}
}

func reportPerfStats(ctx *genCtx, parsers *includeParserManager, targetScanner, hostScanner *IncludeScanner) {
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
	fmt.Fprintf(os.Stderr, "perf: windowImports calls=%d rootNotFirst=%d\n",
		windowImportsCalls, windowImportsRootNotFirst)

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
	// Shared across the genCtx (producer: emitResourceFetch) and the resource-aware
	// emitter (consumer: attachResourceDeps) so a $(<NAME>) reference resolves to the
	// fetch node emitted when its declaring RESOURCES_LIBRARY was gen'd.
	fetchRefs := map[string]NodeRef{}
	resourceEmit := newResourceAwareEmitter(hostP, plainEmit, scriptTbl, fetchRefs)

	// Mix $(S) input content hashes into node uids in every mode so a source edit
	// invalidates the cache (the dump path is re-uid'd from canonical content
	// downstream, but the raw uids must still be content-correct).
	switch e := plainEmit.(type) {
	case *BufferedEmitter:
		e.fs = fs
	case *StreamingEmitter:
		e.uidScratch.fs = fs
	}

	parsers := newIncludeParserManagerFS(fs, newSharedParseCache())

	targetReg := NewCodegenRegistry()
	hostReg := NewCodegenRegistry()

	ctx := &genCtx{
		sourceRoot:         fs.SourceRoot(),
		fs:                 fs,
		parsers:            parsers,
		emit:               resourceEmit,
		memo:               make(map[ModuleInstance]*moduleEmitResult),
		walking:            make(map[ModuleInstance]bool),
		host:               hostP,
		target:             targetP,
		fetchRefs:          fetchRefs,
		flatcEmissions:     make(map[codegenOutputKey]flatcEmission),
		pyRegisterOutputs:  make(map[VFS]NodeRef),
		checkConfigOutputs: make(map[VFS]NodeRef),
		ldPluginCPCache:    make(map[VFS]NodeRef),
		scripts:            scriptTbl,
		testMode:           testMode,
	}

	ctx.inclArgs = inclArgMemo{m: &ctx.inclArgValues}

	// Both scanners share ctx.tarjan (the run-wide Tarjan scratch) so its
	// vfsBound-sized arrays grow once, not once per scanner.
	targetScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(targetP.ISA), targetP.Flags[envMUSL] == strYes, targetP.Flags[envOPENSOURCE] == strYes, onWarn), onWarn, &ctx.tarjan)
	targetScanner.codegen = targetReg
	targetScanner.moduleByRef = &ctx.moduleByRef
	hostScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(hostP.ISA), hostP.Flags[envMUSL] == strYes, hostP.Flags[envOPENSOURCE] == strYes, onWarn), onWarn, &ctx.tarjan)
	hostScanner.codegen = hostReg
	hostScanner.moduleByRef = &ctx.moduleByRef
	ctx.scannerTarget = targetScanner
	ctx.scannerHost = hostScanner

	seed := ModuleInstance{
		Path:     Source(filepath.Clean(targetDir)),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: targetP,
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	if ctx.testMode && root.testSuiteInfo != nil {
		for _, ref := range emitTestRunNodes(resourceEmit, resourceEmit, targetP, *root.testSuiteInfo, root.LDRef, root.ResourceGlobalClosure) {
			ctx.emit.Result(ref)
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

func GenDumpGraphWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := NewBufferedEmitter()
	// -G emits the same graph that gets executed: the resource FETCH nodes are real
	// dependencies (dump normalize folds them back out for the byte-exact compare).
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, testMode)

	return finalizeDumpGraph(emitter)
}

func genWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, testMode)

	return Finalize(emitter)
}

func programBinaryName(instance ModuleInstance, moduleStmt *ModuleStmt) string {
	if moduleStmt == nil {
		return ""
	}

	if moduleStmt.Name == tokUnittestFor {
		return strings.ReplaceAll(path.Clean(instance.Path.Rel()), "/", "-")
	}

	// PY3_PROGRAM_BIN(progname) links as its argument when one is given (the
	// opensource reference: tools/py3cc/slow/bin's PY3_PROGRAM_BIN(py3cc),
	// INCLUDEd into tools/py3cc/slow, links as .../py3cc); without an argument
	// it falls through to the module-dir basename default.

	if len(moduleStmt.Args) > 0 {
		return moduleStmt.Args[0]
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

	return path.Clean(moduleStmt.Args[0])
}

func genModule(ctx *genCtx, instance ModuleInstance) *moduleEmitResult {
	if existing, ok := ctx.memo[instance]; ok {
		return existing
	}

	if os.Getenv("YATOOL_TRACE") == "1" {
		indent := strings.Repeat("  ", len(ctx.traceStack))
		caller := "(root)"

		if len(ctx.traceStack) > 0 {
			caller = ctx.traceStack[len(ctx.traceStack)-1]
		}

		fmt.Fprintf(os.Stderr, "%sgenModule %s@%s  (from %s)\n", indent, instance.Path.Rel(), instance.Platform.Target, caller)
		ctx.traceStack = append(ctx.traceStack, instance.Path.Rel()+"@"+string(instance.Platform.Target))

		defer func() { ctx.traceStack = ctx.traceStack[:len(ctx.traceStack)-1] }()
	}

	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path.Rel())
		return &moduleEmitResult{}
	}

	ctx.walking[instance] = true
	defer delete(ctx.walking, instance)

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path.Rel(), "ya.make")
	mf := Throw2(ParseFile(ctx.fs, yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(ctx.parsers, &deduper, instance.Path.Rel(), instance.Kind, mf.Stmts, env)

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
		ctx.memo[instance] = result

		return result
	}

	for _, stmt := range d.allPySrcs {
		applyAllPySrcs(ctx.fs, instance.Path.Rel(), stmt, d)
	}

	if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && instance.Language != LangPy {
		cppProtoEnv := env.Clone()
		cppProtoEnv.SetStringID(envMODULE_TAG, strCPPProto)

		cppProtoEnv.SetBool(envGEN_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.Rel(), instance.Kind, mf.Stmts, cppProtoEnv)
	} else if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy {
		py3ProtoEnv := env.Clone()
		py3ProtoEnv.SetBool(envPY3_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.Rel(), instance.Kind, mf.Stmts, py3ProtoEnv)
	}

	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.Rel(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.Rel())
	}

	if d.moduleStmt.Name == tokResourcesLibrary {
		// A RESOURCES_LIBRARY's own LDFLAGS may reference ${<NAME>_RESOURCE_GLOBAL}
		// (build/platform/lld: --ld-path=${LLD_ROOT_RESOURCE_GLOBAL}/bin/ld.lld) ahead
		// of the DECLARE that defines it. Bind the declared globals into the env and
		// re-collect once so those references expand (ymake defers; we re-collect).
		if bindResourceGlobalVars(ctx, instance, d, env) {
			d = collectModule(ctx.parsers, &deduper, instance.Path.Rel(), instance.Kind, mf.Stmts, env)
		}

		return genResourcesLibrary(ctx, instance, d)
	}

	if instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary {
		hasProtoSrc := false

		for _, src := range d.srcs {
			if strings.HasSuffix(src, ".proto") {
				hasProtoSrc = true
				break
			}
		}

		if hasProtoSrc && !strings.HasPrefix(instance.Path.Rel(), "contrib/libs/protobuf/builtin_proto") &&
			!strings.HasPrefix(instance.Path.Rel(), "contrib/python/protobuf") {
			d.peerdirs = append(d.peerdirs, "contrib/python/protobuf")
		}

		if hasProtoSrc && d.grpc {
			d.peerdirs = append(d.peerdirs, "contrib/python/grpcio")
		}
	}

	if d.moduleStmt.Name != tokLibrary && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path.Rel(), d.moduleStmt.Name)
	}

	if !d.hadAllocator && (d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) {
		d.hadAllocator = true
		d.allocatorName = "J"
	}

	py3ProtoVariant := d.moduleStmt.Name == tokProtoLibrary && d.usePython3

	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path.Rel() != "contrib/libs/python" {
		d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
	} else if py3ProtoVariant && !d.noPythonIncl && instance.Path.Rel() != "contrib/libs/python" {
		if moduleExcludesTag(d, "CPP_PROTO") {
			d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
		} else {
			d.peerdirs = append(d.peerdirs, "contrib/libs/python")
		}
	}

	if d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin {
		var earlyPeers []string

		if d.pythonSQLite3 {
			earlyPeers = append(earlyPeers, "contrib/tools/python3/Modules/_sqlite")
		}

		earlyPeers = append(earlyPeers, "library/python/runtime_py3/main")

		if !d.noImportTracing && instance.Path.Rel() != "library/python/import_tracing/constructor" {
			earlyPeers = append(earlyPeers, "library/python/import_tracing/constructor")
		}

		var latePeers []string

		if !d.noCheckImportsDisabled {
			latePeers = append(latePeers, "library/python/testing/import_test")
		}

		if d.moduleStmt.Name == tokPy3ProgramBin {
			insertAt := 0

			if len(d.peerdirs) > 0 && d.peerdirs[0] == "contrib/libs/python" {
				insertAt = 1
			}

			filteredEarly := earlyPeers[:0]

			for _, peer := range earlyPeers {
				if instance.Path.Rel() != peer {
					filteredEarly = append(filteredEarly, peer)
				}
			}

			spliced := make([]string, 0, len(d.peerdirs)+len(filteredEarly))
			spliced = append(spliced, d.peerdirs[:insertAt]...)
			spliced = append(spliced, filteredEarly...)
			spliced = append(spliced, d.peerdirs[insertAt:]...)
			d.peerdirs = spliced
		} else {
			for _, peer := range earlyPeers {
				if instance.Path.Rel() != peer {
					d.peerdirs = append(d.peerdirs, peer)
				}
			}
		}

		for _, peer := range latePeers {
			if instance.Path.Rel() != peer {
				d.peerdirs = append(d.peerdirs, peer)
			}
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) && pyLibraryAutoPythonPeer(d.moduleStmt.Name) && d.moduleStmt.Name != tokPy3Program && d.moduleStmt.Name != tokPy3ProgramBin && !d.noImportTracing && instance.Path.Rel() != "library/python/import_tracing/constructor" {
		d.peerdirs = append(d.peerdirs, "library/python/import_tracing/constructor")
	}

	// (enum_serialization_runtime PEERDIR is added at GenerateEnumSerializationStmt
	// processing time — see modules.go — to match upstream's macro position.)

	// Upstream's _CPP_FLATC_CMD (fbs.conf) carries .PEERDIR=contrib/libs/flatbuffers,
	// adding it as an induced dep to every module with .fbs SRCS (e.g. apache/arrow).
	// Append after explicit PEERDIRs so the peer archive closure puts flatbuffers
	// after the module's last declared peer, matching upstream's link order.
	if instance.Path.Rel() != "contrib/libs/flatbuffers" {
		for _, src := range d.srcs {
			if strings.HasSuffix(src, ".fbs") {
				d.peerdirs = append(d.peerdirs, "contrib/libs/flatbuffers")
				break
			}
		}
	}

	if isSpecializedLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == tokDynamicLibrary {
			result := emitDynamicLibrary(ctx, instance, d)
			ctx.memo[instance] = result

			return result
		}

		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)
		d.tc = resolveModuleToolchain(peerContribs.resourceGlobals, instance.Platform.ClangVer)

		ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins, d.tc)
		ldPlugins := mergeLDPlugins(ownLDPlugins, &ldPluginsResult{
			Refs:  peerContribs.ldPluginRefs,
			Paths: peerContribs.ldPluginPaths,
		})

		if ldPlugins == nil {
			ldPlugins = &ldPluginsResult{}
		}

		headerOnlyInputs := ModuleCCInputs{
			InclArgs:          ctx.inclArgs,
			Flags:             d.flags,
			AddIncl:           d.addIncl,
			PeerAddInclGlobal: peerContribs.addIncl,
			SrcDirs:           d.srcDirs,
			SourceRoot:        ctx.sourceRoot,
			FS:                ctx.fs,
			DefaultVars:       d.defaultVars,
			DefaultVarOrder:   d.defaultVarOrder,
			TC:                d.tc,
		}
		_ = emitRunProgramsForAR(ctx, instance, d, headerOnlyInputs)
		_ = emitRunPythonForAR(ctx, instance, d, headerOnlyInputs)

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
				archiveName = d.moduleStmt.Args[0]
			}

			switch d.moduleStmt.Name {
			case tokPy23NativeLibrary:
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.Rel(), "libpy3c", archiveName)
				tag = tagPy3NativeGlobal
			case tokPy23Library:
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.Rel(), "libpy3", archiveName)
				tag = tagPy3Global
			case tokPy3Library, tokPy2Library, tokPy2Program, tokPy3Program:
				arInstance.Language = LangPy
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.Rel(), "libpy3", archiveName)
				tag = tagGlobal
			default:
				globalBaseName = globalArchiveNameWithPrefixOrName(instance.Path.Rel(), "lib", archiveName)
				tag = tagGlobal
			}

			gRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRes.Refs, objcopyRes.Outputs, d.tc, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = vfsPtr(Build(instance.Path.Rel() + "/" + globalBaseName))
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
			ns := Source(filepath.ToSlash(filepath.Clean(*d.protoNamespace)))

			if d.protoNamespaceGlobal {
				ownProtoAddInclH = []VFS{ns}
			} else {
				ownProtoTailH = []VFS{ns}
			}
		}

		effectiveProtoAddInclH := dedupVFS(ownProtoAddInclH, peerContribs.protoAddIncl)
		effectiveProtoTailH := dedupVFS(ownProtoTailH, peerContribs.protoNamespaceTail)

		result := &moduleEmitResult{
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
			InducedDeps:                     d.inducedDeps,
			Peerdirs:                        d.peerdirs,
			ModuleStmtName:                  d.moduleStmt.Name,
		}
		ctx.memo[instance] = result

		return result
	}

	languageDefaults := defaultPeerdirsForModule(ctx, instance, d)

	languageDefaults = suppressMallocAPIDefault(languageDefaults, d.allocatorName)

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path.Rel())
	unitTestPeer := unittestForPeerPath(d.moduleStmt)

	var preUserProgDefaults []string
	var postUserProgDefaults []string

	if isProgram {
		preUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, false)
		postUserProgDefaults = defaultProgramPeerdirsForModule(ctx, instance, d, true)
	}

	allocatorExplicitPeers := allocatorPeers[d.allocatorName]

	unitTestPeerCount := 0

	if unitTestPeer != "" {
		unitTestPeerCount = 1
	}

	seen := make(map[string]struct{}, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))
	allPeers := make([]string, 0, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	const (
		peerKindLangDefault    = 0
		peerKindProgramDefault = 1
		peerKindUserPeer       = 2
		peerKindUnitTestPeer   = 3
	)

	peerKinds := make([]int, 0, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))

	for _, p := range languageDefaults {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindLangDefault)
	}

	if unitTestPeer != "" {
		if _, dup := seen[unitTestPeer]; !dup {
			seen[unitTestPeer] = struct{}{}
			allPeers = append(allPeers, unitTestPeer)
			peerKinds = append(peerKinds, peerKindUnitTestPeer)
		}
	}

	for _, p := range preUserProgDefaults {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindProgramDefault)
	}

	for _, p := range allocatorExplicitPeers {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	for _, p := range postUserProgDefaults {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		allPeers = append(allPeers, p)
		peerKinds = append(peerKinds, peerKindProgramDefault)
	}

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
	peerLinkCmdPaths := make([]VFS, 0, len(allPeers))
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
		result *moduleEmitResult
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
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path.Rel(), peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})
	}

	// Resource globals (<NAME>_RESOURCE_GLOBAL) propagate transitively: fold every
	// peer's closure, deduped by global-var STR through the run-wide deduper (a leaf
	// pass — no genModule reentry — so reset-then-stream is safe).
	var resourceGlobalsClosure []resourceDecl
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

	if d.moduleStmt != nil {
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

	if d.moduleStmt != nil && d.moduleStmt.Name == tokPy3Program {
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
		ns := Source(filepath.ToSlash(filepath.Clean(*d.protoNamespace)))

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

	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path.Rel()] {
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
		archiveName = d.moduleStmt.Args[0]
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

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path.Rel(), peerAddInclGlobal, dedupedAddIncl)

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
		SourceRoot:             ctx.sourceRoot,
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
		BisonGenExt: d.bisonGenExt,
		TC:          d.tc,
	}

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
		src  string
		emit *sourceEmit
	}

	// Collected in SRCS order so pass 2 appends them without a side map: a source's
	// codegen-ness is exactly isCodegenProducingSrc(src), so pass 2 needs no
	// membership set, only this ordered list of the pre-emitted nodes.
	codegenEmits := make([]codegenEmit, 0, 4)

	for _, src := range d.srcs {
		if !isCodegenProducingSrc(src) {
			continue
		}

		srcInputs := moduleInputs

		if extras := d.perSrcCFlagsFor(src); extras != nil {
			srcInputs.PerSourceCFlags = *extras
		}

		if d.flatSrc(src) {
			srcInputs.FlatOutput = true
		}

		codegenEmits = append(codegenEmits, codegenEmit{src, emitOneSource(ctx, instance, d, src, srcInputs)})
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
	emitSrcInputs := func(src string) ModuleCCInputs {
		si := moduleInputs

		if extras := d.perSrcCFlagsFor(src); extras != nil {
			si.PerSourceCFlags = *extras
		}

		if d.flatSrc(src) {
			si.FlatOutput = true
		}

		return adjustCythonCompanionSourceInputs(d, src, si)
	}
	appendCC := func(src string, emit *sourceEmit) {
		if emit == nil {
			return
		}

		isFlatNoLto := d.flatSrc(src)
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, isFlatNoLto)
		ccIsCFGenerated = append(ccIsCFGenerated, strings.HasSuffix(src, ".cpp.in") || strings.HasSuffix(src, ".c.in"))
		ccIsProtoGenerated = append(ccIsProtoGenerated, strings.HasSuffix(src, ".proto"))
	}

	for _, src := range d.srcs {
		if isCodegenProducingSrc(src) {
			continue
		}

		appendCC(src, emitOneSource(ctx, instance, d, src, emitSrcInputs(src)))
	}

	for _, ce := range codegenEmits {
		appendCC(ce.src, ce.emit)
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

		emit := emitOneSource(ctx, instance, d, e.Src, variantIn)

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

		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		if srcInstance.Platform.ISA == ISAX8664 {
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = rebasePerArchPeerAddIncl(moduleInputs.PeerAddInclGlobal, srcInstance.Platform.ISA, ctx.target.ISA)

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, srcInstance, js.Sources, jsModuleInputs)
		}

		jsRef, joinOutVFS := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.target, d.tc, ctx.scripts, ctx.emit)

		jsRel := strings.TrimPrefix(joinOutVFS.Rel(), srcInstance.Path.Rel()+"/")

		ccIncludeInputs := jsCCIncludeInputs(srcInstance, joinOutVFS, js.Sources, ccClosure, ctx.scripts)

		ccIn := moduleInputs
		ccIn.ExtraDepRefs = []NodeRef{jsRef}
		ccIn.IncludeInputs = ccIncludeInputs

		ref, outPath, _ := EmitCC(srcInstance, jsRel, joinOutVFS, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
	}

	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]VFS, 0, len(d.globalSrcs))

	for _, src := range d.globalSrcs {
		emit := emitOneSource(ctx, instance, d, src, moduleInputs)

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
	mergedLDPlugins := mergeLDPlugins(ownLDPlugins, &ldPluginsResult{
		Refs:  peerLDPluginRefs,
		Paths: peerLDPluginPaths,
	})

	if mergedLDPlugins == nil {
		mergedLDPlugins = &ldPluginsResult{}
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		binaryName := programBinaryName(instance, d.moduleStmt)

		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths
		ldPeerLinkCmdPaths := peerLinkCmdPaths

		if d.allocatorName == "FAKE" {
			ldPeerArchiveRefs = make([]NodeRef, 0, len(peerArchiveRefs))
			ldPeerArchivePaths = make([]VFS, 0, len(peerArchivePaths))

			for i, p := range peerArchivePaths {
				if strings.HasPrefix(p.Rel(), "library/cpp/malloc/api/") {
					continue
				}

				ldPeerArchiveRefs = append(ldPeerArchiveRefs, peerArchiveRefs[i])
				ldPeerArchivePaths = append(ldPeerArchivePaths, p)
			}
		}

		if d.moduleStmt.Name == tokPy3Program && d.allocatorName == "J" {
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

		ldRef := EmitLD(
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
		ldPath := LDOutputPath(instance, binaryName)
		var suiteInfo *testSuiteInfo

		if ctx.testMode && d.moduleStmt.Name == tokUnittestFor {
			suiteInfo = buildTestSuiteInfo(instance, d, ldPath)
		}

		result := &moduleEmitResult{
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
			InducedDeps:                     d.inducedDeps,
			Peerdirs:                        d.peerdirs,
			ModuleStmtName:                  d.moduleStmt.Name,
			ResourceGlobalClosure:           resourceGlobalsClosure,
			testSuiteInfo:                   suiteInfo,
		}
		ctx.memo[instance] = result

		return result
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, ccIsProtoGenerated, numSrcsDerived)

	var arRef NodeRef
	arBaseName := arNameFn(instance.Path.Rel())

	arInstance := instance

	switch d.moduleStmt.Name {
	case tokPy3Library, tokPy2Library, tokPy23Library, tokPy2Program, tokPy3Program:
		arInstance.Language = LangPy
	}

	var arPluginVFS *VFS

	if d.arPlugin != nil {
		v := Source(instance.Path.Rel() + "/" + *d.arPlugin)
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
			arRef = EmitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		}
	}

	_ = peerArchiveRefs
	var arPath *VFS

	if len(ccRefs) > 0 {
		arPath = vfsPtr(Build(instance.Path.Rel() + "/" + arBaseName))
	}

	result := &moduleEmitResult{
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
		InducedDeps:                     d.inducedDeps,
		Peerdirs:                        d.peerdirs,
		ModuleStmtName:                  d.moduleStmt.Name,
		ResourceGlobalClosure:           resourceGlobalsClosure,
	}

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path.Rel())

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
		globalRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, d.tc, ctx.host, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(Build(instance.Path.Rel() + "/" + globalBaseName))
	}

	ctx.memo[instance] = result

	return result
}

func filterBuildRootSelfPaths(instancePath string, peer, own []VFS) []VFS {
	if len(peer) == 0 {
		return peer
	}

	ownPrefix := Build(instancePath)
	deduper.reset()
	matched := false

	for _, p := range own {
		if p.IsBuild() && (p == ownPrefix || strings.HasPrefix(p.Rel(), ownPrefix.Rel()+"/")) {
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
		if strings.HasSuffix(p.Rel(), "_serialized.cpp") || strings.HasSuffix(p.Rel(), "_serialized.h") {
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

	deduper.reset()
	out := &ldPluginsResult{
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

type peerGlobalContribs struct {
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

	// resourceGlobals is the transitive resource-global closure aggregated across
	// peers (deduped by global-var name), the source for resolveModuleToolchain in
	// the specialized/header-only path (the general path folds it inline instead).
	resourceGlobals []resourceDecl
}

func walkPeersForGlobalAddIncl(ctx *genCtx, instance ModuleInstance, d *moduleData) peerGlobalContribs {
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)
	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))

	// Resolve every peer through genModule first (memoized; the recursion may
	// re-enter the deduper), then aggregate per output kind below in sequential
	// leaf passes. The visited guard stays a local string-keyed map because it
	// must stay live across the genModule calls.
	resolved := make([]*moduleEmitResult, 0, len(defaults)+len(d.peerdirs))

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
	if instance.Language == LangPy && d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && d.optimizePyProtos && !moduleExcludesTag(d, "CPP_PROTO") {
		seen[instance.Path.Rel()] = struct{}{}
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
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		walk(filepath.Clean(p))
	}

	out := peerGlobalContribs{}

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

		if strings.Contains(path.Rel(), "/_/_/") {
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
		rel := m.path.Rel()

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

func (ctx *genCtx) tool(modulePath ARG) (NodeRef, VFS) {
	res := ctx.toolResult(modulePath)
	return res.LDRef, *res.LDPath
}

func (ctx *genCtx) toolResult(modulePath ARG) *moduleEmitResult {
	if res, ok := ctx.tools.Get(modulePath); ok {
		return res
	}

	res := genModule(ctx, NewToolInstance(ctx.host, modulePath.String()))

	// Cache (and map the tool's LD node back to its result) only once it really
	// built: a tolerated PEERDIR cycle yields an empty stub with LDRef 0 that
	// genModule does NOT memoize, so caching it here would poison later lookups
	// (the tool would keep its empty InducedDeps forever instead of rebuilding).
	if res.LDRef != NodeRef(0) {
		ctx.tools.Put(modulePath, res)
		ctx.moduleByRef.Put(res.LDRef, res)
	}

	return res
}

func (ctx *genCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	return ctx.scannerForPlatform(instance.Platform)
}

func (ctx *genCtx) scannerForPlatform(p *Platform) *IncludeScanner {
	if p == ctx.host {
		return ctx.scannerHost
	}

	return ctx.scannerTarget
}
