package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// Path constants hoisted by `ay refac consts`.
	bldBuildCowOnLibbuildCowOnA                                                           = Build("build/cow/on/libbuild-cow-on.a")
	bldContribLibsJemallocLibcontribLibsJemallocA                                         = Build("contrib/libs/jemalloc/libcontrib-libs-jemalloc.a")
	bldLibraryCppJsonCommonLibcppJsonCommonA                                              = Build("library/cpp/json/common/libcpp-json-common.a")
	bldLibraryCppMallocApiLibcppMallocApiA                                                = Build("library/cpp/malloc/api/libcpp-malloc-api.a")
	bldLibraryCppMallocJemallocLibcppMallocJemallocA                                      = Build("library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a")
	bldLibraryPythonRuntimePy3                                                            = Build("library/python/runtime_py3")
	bldToolsEnumParserEnumSerializationRuntimeLibtoolsEnumParserEnumSerializationRuntimeA = Build("tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a")
	contribLibsCxxsuppLibcxxrtInclude                                                     = Source("contrib/libs/cxxsupp/libcxxrt/include")
	contribRestrictedAbseilCpp                                                            = Source("contrib/restricted/abseil-cpp")
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

	// AddInclOneLevel propagates to direct PEERDIR consumers only (one hop, not
	// transitive). Direct consumers absorb these paths into their own effective
	// addincl; they are NOT re-propagated via AddInclGlobal.
	AddInclOneLevel []VFS

	// AddInclUserGlobal is the peer's own GLOBAL and ONE_LEVEL ADDINCL paths in
	// declaration order — the equivalent of ymake's UserGlobal. Used by direct
	// consumers to preserve upstream -I ordering (UserGlobal before GlobalPropagated).
	AddInclUserGlobal []VFS

	CFlagsGlobal     []string
	CXXFlagsGlobal   []string
	COnlyFlagsGlobal []string
	ObjAddLibsGlobal []string

	LDFlagsGlobal []string

	RPathFlagsGlobal []string

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

	InducedDeps []string

	Peerdirs []string

	ModuleStmtName string

	testSuiteInfo *testSuiteInfo
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

	inclArgs        inclArgMemo
	memo            map[ModuleInstance]*moduleEmitResult
	moduleTypeCache map[moduleTypeCacheKey]moduleTypeInfo
	walking         map[ModuleInstance]bool
	cyclesTolerated int

	traceStack []string

	scannerTarget *IncludeScanner
	scannerHost   *IncludeScanner

	enOutputs map[VFS]NodeRef

	pbOutputs map[codegenOutputKey]NodeRef
	evOutputs map[codegenOutputKey]NodeRef

	flatcEmissions map[codegenOutputKey]flatcEmission

	pyRegisterOutputs map[VFS]NodeRef

	checkConfigOutputs map[VFS]NodeRef

	ldPluginCPCache map[VFS]NodeRef

	// scripts maps each build/scripts script VFS to [self, …transitive import
	// closure]; emit sites add a build script via append(inputs, scripts[v]...).
	scripts scriptDeps

	host   *Platform
	target *Platform

	testMode bool

	// codegenSeen is the reused dedup map for resolveCodegenDepRefsExt across its
	// ~22k per-run calls (map growth was ~25MB churn). One field is safe: the gen
	// goroutine is single-threaded and resolveCodegenDepRefsExt does not re-enter
	// itself (EmitCF emits no CC and calls no resolveCodegen*). Cleared per call.
	codegenSeen map[NodeRef]struct{}

	// peerArchiveSeenPool reuses the peer-archive dedup map in genModule's
	// result-construction (was ~12.5MB churn). A pool, not a field: genModule
	// recurses, and the map's live window does overlap a nested genModule, so a
	// nested call must borrow a distinct map (a shared field corrupts the graph).
	peerArchiveSeenPool sync.Pool
}

type codegenOutputKey struct {
	platform *Platform
	path     VFS
}

type scanCtxPerfStats struct {
	subgraphEntries int
	childrenEntries int
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
	seen := ctx.codegenSeen

	if seen == nil {
		seen = make(map[NodeRef]struct{}, 16)
		ctx.codegenSeen = seen
	}

	clear(seen)

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
				if info := reg.Lookup(v); info != nil {
					if !info.HasProducerRef && info.DeferredCF != nil {
						def := info.DeferredCF
						cfRef, _ := EmitCF(def.instance, def.srcVFS, def.outVFS, def.cfgVars, def.includeInputs, consumer.Path, "", ctx.emit)
						reg.SetProducerRef(v, cfRef)
					}

					if info.HasProducerRef {
						ref, ok = info.ProducerRef, true
					}
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

func (ctx *genCtx) perfScanCtxStats(scanner *IncludeScanner) scanCtxPerfStats {
	return scanCtxPerfStats{
		subgraphEntries: len(scanner.subgraphCache),
		childrenEntries: len(scanner.childrenCache),
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

	reportScanner := func(label string, scanner *IncludeScanner) {
		scanStats := scanner.perfStats()
		ctxStats := ctx.perfScanCtxStats(scanner)
		fmt.Fprintf(os.Stderr, "perf: scanner %s closureEntries=%d childrenEntries=%d walkClosure=%d dfs=%d plainDfs=%d closureHits=%d closureMisses=%d cyclicSCCs=%d searchTierHits=%d searchTierMisses=%d resolveCalls=%d sysinclSourceHits=%d sysinclSourceMisses=%d sysinclIncluderHits=%d sysinclIncluderMisses=%d\n",
			label,
			ctxStats.subgraphEntries,
			ctxStats.childrenEntries,
			scanStats.walkClosureCalls,
			scanStats.dfsCalls,
			scanStats.plainDfsCalls,
			scanStats.subgraphHits,
			scanStats.subgraphMisses,
			scanStats.subgraphTainted,
			scanStats.searchTierHits,
			scanStats.searchTierMisses,
			scanStats.resolveSearchPathCalls,
			scanStats.sysinclSourceHits,
			scanStats.sysinclSourceMisses,
			scanStats.sysinclIncluderHits,
			scanStats.sysinclIncluderMisses,
		)
	}

	reportScanner("target", targetScanner)
	reportScanner("host", hostScanner)
}

func runGenInto(fs FS, targetDir string, hostP, targetP *Platform, emitter Emitter, onWarn func(Warn)) NodeRef {
	return runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, nil, false, true)
}

func runGenIntoWithResources(fs FS, targetDir string, hostP, targetP *Platform, emitter Emitter, onWarn func(Warn), resources *resourceFetchPlan, testMode bool, materializeResourceFetches bool) NodeRef {
	plainEmit := emitter
	scriptTbl := buildScriptTable(fs)
	resourceEmit := resourceGraphEmitter(hostP, plainEmit, resources, materializeResourceFetches, scriptTbl)

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

	targetScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(targetP.ISA), onWarn), onWarn)
	targetScanner.codegen = targetReg
	targetScanner.fallbackLocators = []pathLocator{codegenLocator{reg: targetReg}}
	hostScanner := newIncludeScannerWith(parsers, LoadSysInclSetForFS(fs, string(hostP.ISA), onWarn), onWarn)
	hostScanner.codegen = hostReg
	hostScanner.fallbackLocators = []pathLocator{codegenLocator{reg: hostReg}}
	ctx := &genCtx{
		sourceRoot:         fs.SourceRoot(),
		fs:                 fs,
		parsers:            parsers,
		emit:               resourceEmit,
		inclArgs:           make(inclArgMemo, 4096),
		memo:               make(map[ModuleInstance]*moduleEmitResult),
		moduleTypeCache:    make(map[moduleTypeCacheKey]moduleTypeInfo),
		walking:            make(map[ModuleInstance]bool),
		host:               hostP,
		target:             targetP,
		scannerTarget:      targetScanner,
		scannerHost:        hostScanner,
		enOutputs:          make(map[VFS]NodeRef),
		pbOutputs:          make(map[codegenOutputKey]NodeRef),
		evOutputs:          make(map[codegenOutputKey]NodeRef),
		flatcEmissions:     make(map[codegenOutputKey]flatcEmission),
		pyRegisterOutputs:  make(map[VFS]NodeRef),
		checkConfigOutputs: make(map[VFS]NodeRef),
		ldPluginCPCache:    make(map[VFS]NodeRef),
		scripts:            scriptTbl,
		testMode:           testMode,
	}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: targetP,
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	if ctx.testMode && root.testSuiteInfo != nil {
		for _, ref := range emitTestRunNodes(resourceEmit, resourceEmit, targetP, *root.testSuiteInfo, root.LDRef) {
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

func Gen(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn)) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, nil, false, true)
}

func GenWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, resources, testMode, true)
}

func GenDumpGraphWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, resources, testMode, false)

	return finalizeDumpGraph(emitter)
}

func genWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool, materializeResourceFetches bool) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, resources, testMode, materializeResourceFetches)

	return Finalize(emitter)
}

func programBinaryName(instance ModuleInstance, moduleStmt *ModuleStmt) string {
	if moduleStmt == nil {
		return ""
	}

	if moduleStmt.Name == "UNITTEST_FOR" {
		return strings.ReplaceAll(path.Clean(instance.Path), "/", "-")
	}

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
	if moduleStmt == nil || moduleStmt.Name != "UNITTEST_FOR" || len(moduleStmt.Args) == 0 {
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

		fmt.Fprintf(os.Stderr, "%sgenModule %s@%s  (from %s)\n", indent, instance.Path, instance.Platform.Target, caller)
		ctx.traceStack = append(ctx.traceStack, instance.Path+"@"+string(instance.Platform.Target))

		defer func() { ctx.traceStack = ctx.traceStack[:len(ctx.traceStack)-1] }()
	}

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
	d := collectModule(ctx.parsers, instance.Path, instance.Kind, mf.Stmts, env)

	for _, stmt := range d.allPySrcs {
		applyAllPySrcs(ctx.fs, instance.Path, stmt, d)
	}

	if d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" && instance.Language != LangPy {
		cppProtoEnv := env.Clone()
		cppProtoEnv.SetString("MODULE_TAG", "CPP_PROTO")

		cppProtoEnv.SetBool("GEN_PROTO", true)
		d = collectModule(ctx.parsers, instance.Path, instance.Kind, mf.Stmts, cppProtoEnv)
	} else if d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" && instance.Language == LangPy {
		py3ProtoEnv := env.Clone()
		py3ProtoEnv.SetBool("PY3_PROTO", true)
		d = collectModule(ctx.parsers, instance.Path, instance.Kind, mf.Stmts, py3ProtoEnv)
	}

	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

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

	if d.moduleStmt.Name != "LIBRARY" && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path, d.moduleStmt.Name)
	}

	if !d.hadAllocator && (d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN") {
		d.hadAllocator = true
		d.allocatorName = "J"
	}

	py3ProtoVariant := d.moduleStmt.Name == "PROTO_LIBRARY" && d.usePython3

	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path != "contrib/libs/python" {
		d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
	} else if py3ProtoVariant && !d.noPythonIncl && instance.Path != "contrib/libs/python" {
		if moduleExcludesTag(d, "CPP_PROTO") {
			d.peerdirs = append([]string{"contrib/libs/python"}, d.peerdirs...)
		} else {
			d.peerdirs = append(d.peerdirs, "contrib/libs/python")
		}
	}

	if d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		var earlyPeers []string

		if d.pythonSQLite3 {
			earlyPeers = append(earlyPeers, "contrib/tools/python3/Modules/_sqlite")
		}

		earlyPeers = append(earlyPeers, "library/python/runtime_py3/main")

		if !d.noImportTracing && instance.Path != "library/python/import_tracing/constructor" {
			earlyPeers = append(earlyPeers, "library/python/import_tracing/constructor")
		}

		var latePeers []string

		if !d.noCheckImportsDisabled {
			latePeers = append(latePeers, "library/python/testing/import_test")
		}

		if d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
			insertAt := 0

			if len(d.peerdirs) > 0 && d.peerdirs[0] == "contrib/libs/python" {
				insertAt = 1
			}

			filteredEarly := earlyPeers[:0]

			for _, peer := range earlyPeers {
				if instance.Path != peer {
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
				if instance.Path != peer {
					d.peerdirs = append(d.peerdirs, peer)
				}
			}
		}

		for _, peer := range latePeers {
			if instance.Path != peer {
				d.peerdirs = append(d.peerdirs, peer)
			}
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) && pyLibraryAutoPythonPeer(d.moduleStmt.Name) && d.moduleStmt.Name != "PY3_PROGRAM" && d.moduleStmt.Name != "PY3_PROGRAM_BIN" && !d.noImportTracing && instance.Path != "library/python/import_tracing/constructor" {
		d.peerdirs = append(d.peerdirs, "library/python/import_tracing/constructor")
	}

	// (enum_serialization_runtime PEERDIR is added at GenerateEnumSerializationStmt
	// processing time — see modules.go — to match upstream's macro position.)

	// Upstream's _CPP_FLATC_CMD (fbs.conf) carries .PEERDIR=contrib/libs/flatbuffers,
	// adding it as an induced dep to every module with .fbs SRCS (e.g. apache/arrow).
	// Append after explicit PEERDIRs so the peer archive closure puts flatbuffers
	// after the module's last declared peer, matching upstream's link order.
	if instance.Path != "contrib/libs/flatbuffers" {
		for _, src := range d.srcs {
			if strings.HasSuffix(src, ".fbs") {
				d.peerdirs = append(d.peerdirs, "contrib/libs/flatbuffers")
				break
			}
		}
	}

	if isSpecializedLibraryType(d.moduleStmt.Name) {
		if d.moduleStmt.Name == "DYNAMIC_LIBRARY" {
			result := emitDynamicLibrary(ctx, instance, d)
			ctx.memo[instance] = result

			return result
		}

		peerContribs := walkPeersForGlobalAddIncl(ctx, instance, d)

		ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
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
			SrcDir:            d.srcDir,
			SourceRoot:        ctx.sourceRoot,
			FS:                ctx.fs,
			DefaultVars:       d.defaultVars,
			DefaultVarOrder:   d.defaultVarOrder,
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

			gRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRes.Refs, objcopyRes.Outputs, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = vfsPtr(Build(instance.Path + "/" + globalBaseName))
		}

		// emitMiscNodes registers JV / CF outputs in the CodegenRegistry so
		// emitProtoSrcs can wire SRCS(X.proto) that names a build-generated
		// proto (e.g. jsonpath's RUN_ANTLR -language protobuf output) to its
		// JV producer via the protoSrcOverride path in emit_proto.go.
		emitMiscNodes(ctx, instance, d, nil)

		protoResult := emitProtoSrcs(ctx, instance, d, peerContribs)

		if d.moduleStmt.Name != "PROTO_LIBRARY" {
			emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, nil)
		}

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

		// Specialized-library path: same narrow rule — only an explicit
		// PROTO_NAMESPACE GLOBAL contributes to _PROTO__INCLUDE.
		var ownProtoAddInclH []VFS

		if d.protoNamespace != nil && d.protoNamespaceGlobal {
			ownProtoAddInclH = []VFS{Source(filepath.ToSlash(filepath.Clean(*d.protoNamespace)))}
		}

		effectiveProtoAddInclH := mergeDedupVFS(ownProtoAddInclH, peerContribs.protoAddIncl)

		result := &moduleEmitResult{
			isPyLibrary:        isPyLibraryType(d.moduleStmt.Name),
			ARRef:              hOnlyARRef,
			ARPath:             hOnlyARPath,
			GlobalRef:          hOnlyGlobalRef,
			GlobalPath:         hOnlyGlobalPath,
			AddInclGlobal:      mergeDedupVFS(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal:   d.addInclGlobal,
			ProtoAddInclGlobal: effectiveProtoAddInclH,
			AddInclOneLevel:    d.addInclOneLevel,
			AddInclUserGlobal:  d.addInclUserGlobal,

			CFlagsGlobal:                    mergeDedup(peerContribs.cFlags, d.cFlagsGlobal),
			CXXFlagsGlobal:                  mergeDedup(peerContribs.cxxFlags, d.cxxFlagsGlobal),
			COnlyFlagsGlobal:                mergeDedup(peerContribs.cOnlyFlags, d.cOnlyFlagsGlobal),
			ObjAddLibsGlobal:                mergeDedup(peerContribs.objAddLibs, d.objAddLibsGlobal),
			LDFlagsGlobal:                   mergeDedup(peerContribs.ldFlags, d.ldFlags),
			RPathFlagsGlobal:                mergeDedup(peerContribs.rpathFlags, d.rpathFlagsGlobal),
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

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path)
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
	peerLinkCmdSeen := map[VFS]struct{}{}
	peerLinkCmdAddPath := func(path VFS) {
		if _, dup := peerLinkCmdSeen[path]; dup {
			return
		}

		peerLinkCmdSeen[path] = struct{}{}
		peerLinkCmdPaths = append(peerLinkCmdPaths, path)
	}
	peerDynamicAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerDynamicSeen[path]; dup {
			return
		}

		peerDynamicSeen[path] = struct{}{}
		peerDynamicRefs = append(peerDynamicRefs, ref)
		peerDynamicPaths = append(peerDynamicPaths, path)
		peerLinkCmdAddPath(path)
	}
	peerArchiveSeen, _ := ctx.peerArchiveSeenPool.Get().(map[VFS]struct{})

	if peerArchiveSeen == nil {
		peerArchiveSeen = make(map[VFS]struct{}, 64)
	}

	clear(peerArchiveSeen)

	defer ctx.peerArchiveSeenPool.Put(peerArchiveSeen)

	peerArchiveAddPath := func(ref NodeRef, path VFS) {
		if _, dup := peerArchiveSeen[path]; dup {
			return
		}

		peerArchiveSeen[path] = struct{}{}
		peerArchiveRefs = append(peerArchiveRefs, ref)
		peerArchivePaths = append(peerArchivePaths, path)
		peerLinkCmdAddPath(path)
	}

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
	rpathFlagsSeen := map[string]struct{}{}
	peerRPathFlagsGlobal := make([]string, 0, 4)
	addInclSeen := map[VFS]struct{}{}
	peerAddInclGlobal := make([]VFS, 0, 16)
	// oneLevelOnlyPaths tracks paths added exclusively via ONE_LEVEL from direct user
	// peers. Such paths appear in peerAddInclGlobal (for correct CC command ordering)
	// but must be excluded from effectiveAddInclGlobal so they don't re-propagate
	// transitively — upstream ONE_LEVEL propagates only one hop.
	var oneLevelOnlyPaths map[VFS]struct{}
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
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})

		for i, p := range peerResult.LDPluginPaths {
			peerLDPluginAddPath(peerResult.LDPluginRefs[i], p)
		}
	}

	archiveOrder := resolved

	if d.moduleStmt != nil {
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
		case "PY3_PROGRAM_BIN":

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
		case "PY3_PROGRAM":
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

		for i, p := range peerResult.PeerArchiveClosurePaths {
			peerArchiveAddPath(peerResult.PeerArchiveClosureRefs[i], p)
		}

		for i, p := range peerResult.PeerGlobalClosurePaths {
			peerGlobalAddPath(peerResult.PeerGlobalClosureRefs[i], p)
		}

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

		if peerResult.ARPath != nil {
			peerArchiveAddPath(peerResult.ARRef, *peerResult.ARPath)
		}
	}

	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.OwnAddInclGlobal)
	}

	for _, rp := range resolved {
		if rp.kind != peerKindLangDefault {
			continue
		}

		addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
	}

	emitUnitTestPeers := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindUnitTestPeer {
				continue
			}

			addEachVFS(addInclSeen, &peerAddInclGlobal, rp.result.AddInclGlobal)
		}
	}

	emitUserPeers := func() {
		for _, rp := range resolved {
			if rp.kind != peerKindUserPeer {
				continue
			}

			// Emit peer's own GLOBAL+ONE_LEVEL in declaration order (UserGlobal),
			// mirroring upstream PropagateTo which propagates UserGlobal before
			// GlobalPropagated (transitive). Declaration order is preserved by
			// AddInclUserGlobal (built from AddInclStmt.UserGlobalPaths).
			for _, p := range rp.result.AddInclUserGlobal {
				if _, dup := addInclSeen[p]; !dup {
					addInclSeen[p] = struct{}{}
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}

			// Track ONE_LEVEL-only paths so they are not re-propagated transitively
			// (upstream one-hop semantics: ONE_LEVEL is not in GlobalPropagated).
			for _, p := range rp.result.AddInclOneLevel {
				if oneLevelOnlyPaths == nil {
					oneLevelOnlyPaths = map[VFS]struct{}{}
				}

				oneLevelOnlyPaths[p] = struct{}{}
			}

			// Add transitive GLOBALs (GlobalPropagated). A path also exported as
			// GLOBAL by any peer wins over ONE_LEVEL-only and should propagate.
			for _, p := range rp.result.AddInclGlobal {
				if _, dup := addInclSeen[p]; !dup {
					addInclSeen[p] = struct{}{}
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}

				if oneLevelOnlyPaths != nil {
					delete(oneLevelOnlyPaths, p)
				}
			}
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

	emitUnitTestPeers()
	emitProgramDefaults()
	emitUserPeers()

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

	if isRuntimeAncestor(instance.Path) {
		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
	}

	cflagsAggOrder := resolved

	if d.moduleStmt != nil && d.moduleStmt.Name == "PY3_PROGRAM" {
		cflagsAggOrder = archiveOrder
	}

	for _, rp := range cflagsAggOrder {
		addEach(cFlagsSeen, &peerCFlagsGlobal, rp.result.CFlagsGlobal)
		addEach(cxxFlagsSeen, &peerCXXFlagsGlobal, rp.result.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &peerCOnlyFlagsGlobal, rp.result.COnlyFlagsGlobal)
		addEach(objAddLibSeen, &peerObjAddLibsGlobal, rp.result.ObjAddLibsGlobal)
		addEach(ldFlagsSeen, &peerLDFlagsGlobal, rp.result.LDFlagsGlobal)
		addEach(rpathFlagsSeen, &peerRPathFlagsGlobal, rp.result.RPathFlagsGlobal)
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

	effectiveAddInclGlobal := mergeDedupVFS(d.addInclGlobal, peerAddInclForProp)

	// ProtoAddInclGlobal: this module's $(S)/<PROTO_NAMESPACE> contribution
	// (only when GLOBAL was specified or the module is a PROTO_LIBRARY),
	// unioned with everything peers reported (transitive — every peer's
	// ProtoAddInclGlobal already includes its own peers' contributions).
	// Mirrors upstream's _PROTO__INCLUDE chain and feeds the proto compile
	// -I= block. peerContribs is not in scope here; iterate `resolved`.
	// Only PROTO_LIBRARY modules that explicitly tagged their PROTO_NAMESPACE
	// as GLOBAL propagate their namespace to consumers' proto compiles. A
	// bare PROTO_LIBRARY (no explicit namespace / no GLOBAL tag) does not
	// contribute, and a regular LIBRARY never does. Upstream's
	// _PROTO__INCLUDE chain is narrower than the C++ ADDINCL chain.
	var ownProtoAddIncl []VFS

	if d.protoNamespace != nil && d.protoNamespaceGlobal {
		ownProtoAddIncl = []VFS{Source(filepath.ToSlash(filepath.Clean(*d.protoNamespace)))}
	}

	// `ADDINCL GLOBAL FOR proto X` (yatool/build/conf/proto.conf:117-120
	// PROTO_ADDINCL macro; contrib/libs/protobuf ya.make) propagates an
	// additional -I=$X into the protoc command of every transitive
	// consumer. Append after the PROTO_NAMESPACE entry — declaration order
	// matches upstream's PROTO_ADDINCL macro placement.
	ownProtoAddIncl = append(ownProtoAddIncl, d.protoAddInclGlobal...)
	protoAddInclSeen := map[VFS]struct{}{}
	peerProtoAddInclGlobal := make([]VFS, 0, 4)

	for _, rp := range resolved {
		addEachVFS(protoAddInclSeen, &peerProtoAddInclGlobal, rp.result.ProtoAddInclGlobal)
	}

	effectiveProtoAddInclGlobal := mergeDedupVFS(ownProtoAddIncl, peerProtoAddInclGlobal)

	if instance.Path == "library/python/runtime_py3" {
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

	effectiveCFlagsGlobal := mergeDedup(peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := mergeDedup(peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := mergeDedup(peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)
	effectiveRPathFlagsGlobal := mergeDedup(peerRPathFlagsGlobal, d.rpathFlagsGlobal)

	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path] {
		const nostdincPP = "-nostdinc++"

		injectAddIncl := []VFS{
			contribLibsCxxsuppLibcxxInclude,
			contribLibsCxxsuppLibcxxrtInclude,
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

		peerAddInclGlobal = hoistRuntimeStackAddIncl(peerAddInclGlobal)
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
	dedupedAddIncl := mergeDedupVFS(d.addIncl, d.addInclGlobal)

	isPy3NativeLib := d.moduleStmt.Name == "PY23_NATIVE_LIBRARY" ||
		d.moduleStmt.Name == "PY23_LIBRARY"

	var perModuleCCTag *string

	switch d.moduleStmt.Name {
	case "PY23_NATIVE_LIBRARY":
		perModuleCCTag = stringPtr("py3_native")
	case "PY23_LIBRARY":
		perModuleCCTag = stringPtr("py3")
	case "YQL_UDF_YDB", "YQL_UDF_CONTRIB":
		perModuleCCTag = stringPtr("yql_udf_static")
	}

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

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path, peerAddInclGlobal, dedupedAddIncl)

	effectiveSrcDir := d.srcDir

	if effectiveSrcDir == nil {
		effectiveSrcDir = programSourceDir(d.moduleStmt)
	}

	moduleInputs := ModuleCCInputs{
		InclArgs:               ctx.inclArgs,
		Flags:                  d.flags,
		AddIncl:                dedupedAddIncl,
		PeerAddInclGlobal:      selfPeerAddInclGlobal,
		PeerProtoAddInclGlobal: effectiveProtoAddInclGlobal,
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
		SrcDir:                 effectiveSrcDir,
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
	}

	ancestorRebase := d.srcDir != nil && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(*d.srcDir, instance.Path)

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

		preEmitted[src] = emitOneSource(ctx, instance, d, src, srcInputs, ancestorRebase)
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

		if extras, ok := d.perSrcCFlags[src]; ok {
			si.PerSourceCFlags = extras
		}

		if _, ok := d.flatSrcs[src]; ok {
			si.FlatOutput = true
		}

		return adjustCythonCompanionSourceInputs(d, src, si)
	}
	appendCC := func(src string, emit *sourceEmit) {
		if emit == nil {
			return
		}

		_, isFlatNoLto := d.flatSrcs[src]
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, isFlatNoLto)
		ccIsCFGenerated = append(ccIsCFGenerated, strings.HasSuffix(src, ".cpp.in") || strings.HasSuffix(src, ".c.in"))
		ccIsProtoGenerated = append(ccIsProtoGenerated, strings.HasSuffix(src, ".proto"))
	}

	for _, src := range d.srcs {
		if _, isCodegen := preEmitted[src]; isCodegen {
			continue
		}

		appendCC(src, emitOneSource(ctx, instance, d, src, emitSrcInputs(src), ancestorRebase))
	}

	for _, src := range d.srcs {
		emit, isCodegen := preEmitted[src]

		if !isCodegen {
			continue
		}

		appendCC(src, emit)
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

		flags := append([]string(nil), e.CFlags...)

		if extras, ok := d.perSrcCFlags[e.Src]; ok {
			flags = append(flags, extras...)
		}

		variantIn.PerSourceCFlags = flags

		emit := emitOneSource(ctx, instance, d, e.Src, variantIn, ancestorRebase)

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

		if ancestorRebase {
			srcInstance.Path = *d.srcDir
		}

		joinClosure := joinSrcsIncludeClosure(ctx, srcInstance.Platform, srcInstance, js.Sources, moduleInputs)

		ccClosure := joinClosure

		if srcInstance.Platform.ISA == ISAX8664 {
			jsModuleInputs := moduleInputs
			jsModuleInputs.PeerAddInclGlobal = rebasePerArchPeerAddIncl(moduleInputs.PeerAddInclGlobal, srcInstance.Platform.ISA, ctx.target.ISA)

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, srcInstance, js.Sources, jsModuleInputs)
		}

		jsRef, joinOutVFS := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.target, ctx.scripts, ctx.emit)

		jsRel := strings.TrimPrefix(joinOutVFS.Rel(), srcInstance.Path+"/")

		ccIncludeInputs := jsCCIncludeInputs(srcInstance, js.Sources, ccClosure, ctx.scripts)

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
		emit := emitOneSource(ctx, instance, d, src, moduleInputs, ancestorRebase)

		if emit == nil {
			continue
		}

		globalRefs = append(globalRefs, emit.Ref)
		globalOutputs = append(globalOutputs, emit.OutPath)
	}

	globalSrcMemberCount := len(globalRefs)

	regCCPy3Suffix := isPy3NativeLib || d.moduleStmt.Name == "PY23_LIBRARY"
	regRes := emitPyRegister(ctx, instance, d, moduleInputs, regCCPy3Suffix)

	if regRes != nil {
		for i, ref := range regRes.Refs {
			globalRefs = append(globalRefs, ref)
			globalOutputs = append(globalOutputs, regRes.Outputs[i])
		}
	}

	ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins)
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

		if d.moduleStmt.Name == "PY3_PROGRAM" && d.allocatorName == "J" {
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

		if d.moduleStmt.Name == "PY2_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
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

		var ownRPathFlags []string

		if len(peerDynamicPaths) > 0 {
			ownRPathFlags = append([]string(nil), peerRPathFlagsGlobal...)
		}

		// Both PY3_PROGRAM (via its PY3_BIN submodule) and PY3_PROGRAM_BIN
		// inherit _BASE_PY3_PROGRAM, which calls STRIP() at conf/python.conf:884
		// (ENABLE(STRIP) → STRIP_FLAG=-Wl,--strip-all on linux per
		// build/conf/linkers/ld.conf:22). ENABLE(NO_STRIP) or BUILD_TYPE=DEBUG
		// reverts this (ymake.core.conf:2669).
		wantsStrip := (d.moduleStmt.Name == "PY3_PROGRAM_BIN" || d.moduleStmt.Name == "PY3_PROGRAM") && !d.noStrip
		// Upstream's PY3_BIN submodule (the PROGRAM side of the PY3_PROGRAM
		// multimodule) has MODULE_TAG=PY3_BIN auto-set from the submodule
		// name (lang/confreader.cpp:847-848). REF exposes it lowercased in
		// the LD node's target_properties. The non-multimodule PY3_PROGRAM_BIN
		// has no implicit MODULE_TAG, so it stays unset there.
		var programModuleTag string

		if d.moduleStmt.Name == "PY3_PROGRAM" {
			programModuleTag = "py3_bin"
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
			ctx.host,
			ctx.scripts,
			ctx.emit,
		)
		ldPath := LDOutputPath(instance, binaryName)
		var suiteInfo *testSuiteInfo

		if ctx.testMode && d.moduleStmt.Name == "UNITTEST_FOR" {
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
			AddInclOneLevel:                 d.addInclOneLevel,
			AddInclUserGlobal:               d.addInclUserGlobal,
			CFlagsGlobal:                    effectiveCFlagsGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
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
			testSuiteInfo:                   suiteInfo,
		}
		ctx.memo[instance] = result

		return result
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, ccIsProtoGenerated, numSrcsDerived)

	var arRef NodeRef
	arBaseName := arNameFn(instance.Path)

	arInstance := instance

	switch d.moduleStmt.Name {
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY", "PY2_PROGRAM", "PY3_PROGRAM":
		arInstance.Language = LangPy
	}

	var arPluginVFS *VFS

	if d.arPlugin != nil {
		v := Source(instance.Path + "/" + *d.arPlugin)
		arPluginVFS = &v
	}

	emitPySrcs(ctx, instance, d)

	genPyAuxRes := emitGeneratedPyAuxChunks(ctx, instance, d, moduleInputs)

	if genPyAuxRes != nil {
		globalRefs = append(globalRefs, genPyAuxRes.Refs...)
		globalOutputs = append(globalOutputs, genPyAuxRes.Outputs...)
	}

	emitLLVMBC(ctx, instance, d, moduleInputs)

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
		if perModuleCCTag != nil {
			arRef = EmitARNamedTagged(arInstance, arBaseName, *perModuleCCTag, ccRefs, ccOutputs, nil, arPluginVFS, ctx.host, ctx.emit)
		} else {
			arRef = EmitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, arPluginVFS, ctx.host, ctx.emit)
		}
	}

	_ = peerArchiveRefs
	var arPath *VFS

	if len(ccRefs) > 0 {
		arPath = vfsPtr(Build(instance.Path + "/" + arBaseName))
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
		AddInclOneLevel:                 d.addInclOneLevel,
		AddInclUserGlobal:               d.addInclUserGlobal,
		CFlagsGlobal:                    effectiveCFlagsGlobal,
		CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
		COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
		ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
		LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
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
	}

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path)

		globalTag := "global"

		switch d.moduleStmt.Name {
		case "PY23_LIBRARY":
			globalTag = "py3_global"
		case "PY23_NATIVE_LIBRARY":
			globalTag = "py3_native_global"
		case "YQL_UDF_YDB", "YQL_UDF_CONTRIB":
			globalTag = "yql_udf_static_global"
		}

		// The PY3_BIN_LIB submodule (KindLib half of PY3_PROGRAM multimodule)
		// composes its global.a tag from <MODULE_TAG>_global; the lang dump
		// expects "py3_bin_lib_global".
		if d.programPairedLib {
			globalTag = "py3_bin_lib_global"
		}

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), len(globalRefs))
		globalRef := EmitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, ctx.host, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(Build(instance.Path + "/" + globalBaseName))
	}

	ctx.memo[instance] = result

	return result
}

func filterBuildRootSelfPaths(instancePath string, peer, own []VFS) []VFS {
	if len(peer) == 0 {
		return peer
	}

	ownSet := make(map[VFS]struct{}, len(own))
	ownPrefix := Build(instancePath)

	for _, p := range own {
		if p.IsBuild() && (p == ownPrefix || strings.HasPrefix(p.Rel(), ownPrefix.Rel()+"/")) {
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

func movePathsAfter(paths []VFS, anchor VFS, moved []VFS) []VFS {
	if len(moved) == 0 {
		return paths
	}

	moveSet := make(map[VFS]struct{}, len(moved))

	for _, path := range moved {
		moveSet[path] = struct{}{}
	}

	outPaths := make([]VFS, 0, len(paths))
	movedPaths := make(map[VFS]VFS, len(moved))

	for _, path := range paths {
		if _, ok := moveSet[path]; ok {
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

func movePathsBefore(paths []VFS, anchor VFS, moved []VFS) []VFS {
	if len(moved) == 0 {
		return paths
	}

	moveSet := make(map[VFS]struct{}, len(moved))

	for _, path := range moved {
		moveSet[path] = struct{}{}
	}

	movedPaths := make(map[VFS]VFS, len(moved))

	for _, path := range paths {
		if _, ok := moveSet[path]; ok {
			movedPaths[path] = path
		}
	}

	if len(movedPaths) != len(moved) {
		return paths
	}

	outPaths := make([]VFS, 0, len(paths))

	for _, path := range paths {
		if _, ok := moveSet[path]; ok {
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

type peerGlobalContribs struct {
	addIncl      []VFS
	protoAddIncl []VFS
	cFlags       []string
	cxxFlags     []string
	cOnlyFlags   []string
	objAddLibs   []string
	ldFlags      []string
	rpathFlags   []string

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
}

func walkPeersForGlobalAddIncl(ctx *genCtx, instance ModuleInstance, d *moduleData) peerGlobalContribs {
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)
	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))
	out := peerGlobalContribs{}
	addInclSeen := map[VFS]struct{}{}
	protoAddInclSeen := map[VFS]struct{}{}
	cFlagsSeen := map[string]struct{}{}
	cxxFlagsSeen := map[string]struct{}{}
	cOnlyFlagsSeen := map[string]struct{}{}
	objAddLibSeen := map[string]struct{}{}
	ldFlagsSeen := map[string]struct{}{}
	rpathFlagsSeen := map[string]struct{}{}
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
		addEachVFS(protoAddInclSeen, &out.protoAddIncl, peerResult.ProtoAddInclGlobal)
		addEach(cFlagsSeen, &out.cFlags, peerResult.CFlagsGlobal)
		addEach(cxxFlagsSeen, &out.cxxFlags, peerResult.CXXFlagsGlobal)
		addEach(cOnlyFlagsSeen, &out.cOnlyFlags, peerResult.COnlyFlagsGlobal)
		addEach(objAddLibSeen, &out.objAddLibs, peerResult.ObjAddLibsGlobal)
		addEach(ldFlagsSeen, &out.ldFlags, peerResult.LDFlagsGlobal)
		addEach(rpathFlagsSeen, &out.rpathFlags, peerResult.RPathFlagsGlobal)

		for i, p := range peerResult.PeerArchiveClosurePaths {
			addArchive(peerResult.PeerArchiveClosureRefs[i], p)
		}

		if peerResult.ARPath != nil {
			addArchive(peerResult.ARRef, *peerResult.ARPath)
		}

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

func isSkippedSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".py") ||
		strings.HasSuffix(srcRel, ".g4")
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

func (ctx *genCtx) tool(modulePath string) (NodeRef, VFS) {
	res := ctx.toolResult(modulePath)
	return res.LDRef, *res.LDPath
}

func (ctx *genCtx) toolResult(modulePath string) *moduleEmitResult {
	return genModule(ctx, NewToolInstance(ctx.host, modulePath))
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
