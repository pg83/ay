package main

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

var acknowledgedMacros = map[string]struct{}{
	"CUDA_NVCC_FLAGS":                 {},
	"SET_APPEND_WITH_GLOBAL":          {},
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
	"STYLE_CPP":                       {},
	"STYLE_CPP_YT":                    {},
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
	"DISABLE_DATA_VALIDATION":         {},

	"FROM_SANDBOX":      {},
	"LIST_PROTO":        {},
	"JAVA_PROTO_PLUGIN": {},
	"GO_PROTO_PLUGIN":   {},

	"SPLIT_CODEGEN":             {},
	"DECIMAL_MD5_LOWER_32_BITS": {},
	"STYLE_DETEKT":              {},
	"DEFAULT_JDK_VERSION":       {},
}

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
	ARRef                           NodeRef
	ARPath                          *VFS
	isPROGRAM                       bool
	LDRef                           NodeRef
	LDPath                          *VFS
	GlobalRef                       *NodeRef
	GlobalPath                      *VFS
	WholeArchiveRefs                []NodeRef
	WholeArchivePaths               []VFS
	WholeArchiveCmdPaths            []VFS
	AddInclGlobal                   []VFS
	OwnAddInclGlobal                []VFS
	ProtoInclude                    []VFS
	AddInclOneLevel                 []VFS
	AddInclUserGlobal               []VFS
	CFlagsGlobal                    []ARG
	CXXFlagsGlobal                  []ARG
	COnlyFlagsGlobal                []ARG
	ObjAddLibsGlobal                []ARG
	LDFlagsGlobal                   []ARG
	RPathFlagsGlobal                []ARG
	PeerArchiveClosureRefs          []NodeRef
	PeerArchiveClosurePaths         []VFS
	isPyLibrary                     bool
	PeerGlobalClosureRefs           []NodeRef
	PeerGlobalClosurePaths          []VFS
	PeerWholeArchiveClosureRefs     []NodeRef
	PeerWholeArchiveClosurePaths    []VFS
	PeerWholeArchiveCmdClosurePaths []VFS
	LDPluginRefs                    []NodeRef
	LDPluginPaths                   []VFS
	PeerDynamicClosureRefs          []NodeRef
	PeerDynamicClosurePaths         []VFS
	SbomComponentRef                *NodeRef
	SbomComponentPath               *VFS
	PeerSbomClosureRefs             []NodeRef
	PeerSbomClosurePaths            []VFS
	InducedDeps                     ParsedIncludeSet
	Peerdirs                        []STR
	ModuleStmtName                  TOK
	testSuiteInfo                   *TestSuiteInfo
	DescClosure                     []DescProtoPeer
	ResourceGlobalClosure           []ResourceDecl
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
	fs              FS
	parsers         *IncludeParserManager
	emit            *StreamingEmitter
	onWarn          func(Warn)
	na              *NodeArenas
	inclArgValues   DenseMap[VFS, STR]
	inclArgs        InclArgMemo
	memo            *IntValueMap[*ModuleEmitResult]
	walking         map[ModuleInstance]bool
	cyclesTolerated int
	traceStack      []string
	scannerTarget   *IncludeScanner
	scannerHost     *IncludeScanner
	moduleByRef     DenseMap[NodeRef, *ModuleEmitResult]
	tools           DenseMap[ARG, *ModuleEmitResult]
	scripts         ScriptDeps
	fetchRefs       *DenseMap[STR, NodeRef]
	host            *Platform
	target          *Platform
	vcsRef          NodeRef
	testMode        bool
	sbomEnabled     bool
	autoincludeIdx  *AutoincludeIndex
	tarjan          TarjanCtx
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

	deduper.reset()

	for _, r := range exclude {
		deduper.add(VFS(r))
	}

	var out []NodeRef

	reg := codegenRegForInstance(ctx, consumer)

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
	fmt.Fprintf(os.Stderr, "perf: parser parsedHits=%d parsedMisses=%d\n",
		parserStats.parsedHits, parserStats.parsedMisses)
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

func runGenIntoWithResources(fs FS, targetDir string, hostP, targetP *Platform, emitter *StreamingEmitter, onWarn func(Warn), testMode bool) NodeRef {
	plainEmit := emitter
	scriptTbl := buildScriptTable(fs)

	fetchRefs := emitter.fetchRefs

	parsers := newIncludeParserManagerFS(fs, newSharedParseCache())

	targetReg := newCodegenRegistry()
	hostReg := newCodegenRegistry()

	ctx := &GenCtx{
		fs:        fs,
		parsers:   parsers,
		emit:      plainEmit,
		onWarn:    onWarn,
		na:        plainEmit.nodeArenas(),
		memo:      newIntValueMap[*ModuleEmitResult](4096),
		walking:   make(map[ModuleInstance]bool),
		host:      hostP,
		target:    targetP,
		fetchRefs: fetchRefs,
		scripts:   scriptTbl,
		testMode:  testMode,

		sbomEnabled: fs.isFile(srcRootVFS, sbomConfRel),

		autoincludeIdx: loadAutoincludeIndex(fs),
	}

	ctx.inclArgs = InclArgMemo{m: &ctx.inclArgValues}

	targetScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(targetP.ISA), targetP.Flags[envMUSL] == strYes, targetP.Flags[envOPENSOURCE] == strYes, targetP.OS, onWarn), onWarn, &ctx.tarjan)
	targetScanner.codegen = targetReg
	targetScanner.moduleByRef = &ctx.moduleByRef
	hostScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(hostP.ISA), hostP.Flags[envMUSL] == strYes, hostP.Flags[envOPENSOURCE] == strYes, hostP.OS, onWarn), onWarn, &ctx.tarjan)
	hostScanner.codegen = hostReg
	hostScanner.moduleByRef = &ctx.moduleByRef
	ctx.scannerTarget = targetScanner
	ctx.scannerHost = hostScanner

	ctx.vcsRef = emitVCSNode(ctx.emit, ctx.host)

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

	return root.LDRef
}

func genDumpGraphWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := newStreamingEmitter(fs, nil)
	runGenIntoWithResources(fs, targetDir, hostP, targetP, emitter, onWarn, testMode)

	return finalize(emitter)
}

func genWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	return genDumpGraphWithResources(fs, targetDir, hostP, targetP, onWarn, testMode)
}

func programBinaryName(instance ModuleInstance, moduleStmt *ModuleStmt) string {
	if moduleStmt != nil && moduleStmt.Name == tokUnittestFor {
		return strings.ReplaceAll(path.Clean(instance.Path.rel()), "/", "-")
	}

	if moduleStmt != nil && len(moduleStmt.Args) > 0 {
		return moduleStmt.Args[0].string()
	}

	return lastPathComponent(instance.Path.rel())
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

func moduleCCTag(name TOK) STR {
	switch name {
	case tokPy23NativeLibrary:
		return tagPy3Native
	case tokPy23Library:
		return tagPy3
	case tokYqlUdfYdb, tokYqlUdfContrib:
		return tagYqlUdfStatic
	case tokFbsLibrary:
		return tagCppFbs
	}

	return 0
}

func moduleStmts(ctx *GenCtx, dir string) []Stmt {
	stmts := throw2(parseFile(ctx.fs, joinRel(dir, "ya.make"))).Stmts

	if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(dir); ok && ctx.fs.isFile(srcRootVFS, inc.rel()) {
		stmts = append(stmts, throw2(parseFile(ctx.fs, inc.rel())).Stmts...)
	}

	return stmts
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

	stmts := moduleStmts(ctx, instance.Path.rel())

	env := buildIfEnv(instance)
	d := collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, stmts, env, ctx.onWarn)

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
		cppProtoEnv := buildIfEnv(instance)
		cppProtoEnv.setStringID(envMODULE_TAG, strCPPProto)

		cppProtoEnv.setStringID(envCPP_PROTO, strCPPProto)
		cppProtoEnv.setBool(envGEN_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, stmts, cppProtoEnv, ctx.onWarn)
	} else if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy {
		py3ProtoEnv := buildIfEnv(instance)
		py3ProtoEnv.setBool(envPY3_PROTO, true)
		d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, stmts, py3ProtoEnv, ctx.onWarn)
	}

	if d.conflictMod != nil {
		throwFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.rel(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.rel())
	}

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangDescProto {
		result := emitDescProtoSubmodule(ctx, instance, d)
		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	if d.moduleStmt.Name == tokProtoDescriptions {
		result := emitProtoDescriptions(ctx, instance, d)
		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	if d.moduleStmt.Name == tokResourcesLibrary {
		if bindResourceGlobalVars(ctx, instance, d, env) {
			d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, stmts, env, ctx.onWarn)
		}

		return genResourcesLibrary(ctx, instance, d)
	}

	if d.moduleStmt.Name == tokPrebuiltProgram {
		env.setString(envMODULE_SUFFIX, prebuiltModuleSuffix(instance.Platform))

		if bindResourceGlobalVars(ctx, instance, d, env) {
			d = collectModule(ctx.parsers, &deduper, instance.Path.rel(), instance.Kind, stmts, env, ctx.onWarn)
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

	if d.moduleStmt.Name != tokLibrary && d.moduleStmt.Name != tokFbsLibrary && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) {
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

	if d.hasFbs && instance.Path.rel() != "contrib/libs/flatbuffers" {
		d.peerdirs = append(d.peerdirs, strContribLibsFlatbuffers)
	}

	if d.hasFbs64 && instance.Path.rel() != "contrib/libs/flatbuffers64" {
		d.peerdirs = append(d.peerdirs, strContribLibsFlatbuffers64)
	}

	if d.hasBisonY && instance.Path.rel() != strBuildInducedByBison.string() {
		d.peerdirs = append(d.peerdirs, strBuildInducedByBison)
	}

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

		emitFromSandboxes(ctx, instance, d)
		emitBundles(ctx, instance, d)

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

		objcopyRes := emitResourceObjcopy(ctx, instance, d, headerOnlyInputs)

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

			if cfModuleTag(d, instance) == tagCppProto {
				tag = tagCppProtoGlobal
			}

			gRef := emitARGlobalNamedTagged(arInstance, globalBaseName, tag, objcopyRes.Refs, objcopyRes.Outputs, d.tc, ctx.host, ctx.emit)
			hOnlyGlobalRef = &gRef
			hOnlyGlobalPath = vfsPtr(build(instance.Path.rel() + "/" + globalBaseName))
		}

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

		var ownProtoIncludeH []VFS

		if d.protoNamespace != nil {
			ownProtoIncludeH = []VFS{source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))}
		}

		ownProtoIncludeH = append(ownProtoIncludeH, d.protoAddInclGlobal...)

		effectiveProtoIncludeH := dedupVFS(ownProtoIncludeH, peerContribs.protoInclude)

		var ownSbomRefH *NodeRef
		var ownSbomPathH *VFS

		if sbomActive(ctx, instance) && sbomQualifies(d) {
			realPrjName := strings.TrimSuffix(archiveNameWithPrefixOrName(instance.Path.rel(), "", ""), ".a")
			ownSbomRefH, ownSbomPathH = emitSbomComponent(ctx, instance, d, realPrjName)
		}

		result := &ModuleEmitResult{
			isPyLibrary:       isPyLibraryType(d.moduleStmt.Name),
			ARRef:             hOnlyARRef,
			ARPath:            hOnlyARPath,
			GlobalRef:         hOnlyGlobalRef,
			GlobalPath:        hOnlyGlobalPath,
			AddInclGlobal:     dedupVFS(d.addInclGlobal, peerContribs.addIncl),
			OwnAddInclGlobal:  d.addInclGlobal,
			ProtoInclude:      effectiveProtoIncludeH,
			AddInclOneLevel:   d.addInclOneLevel,
			AddInclUserGlobal: d.addInclUserGlobal,

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

	var allocatorExplicitPeers []string

	if isProgram {
		allocatorExplicitPeers = allocatorPeers[d.allocatorName.string()]
	}

	unitTestPeerCount := 0

	if unitTestPeer != "" {
		unitTestPeerCount = 1
	}

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

	frontSet := make(map[STR]struct{}, len(d.protoCmdPeers))

	for _, p := range d.protoCmdPeers {
		frontSet[p] = struct{}{}
	}

	appendUserPeer := func(p STR) {
		if !deduper.add(VFS(p) << 1) {
			return
		}

		allPeers = append(allPeers, p.string())
		peerKinds = append(peerKinds, peerKindUserPeer)
	}

	for _, p := range d.peerdirs {
		if _, isFront := frontSet[p]; isFront {
			appendUserPeer(p)
		}
	}

	for _, p := range d.peerdirs {
		if _, isFront := frontSet[p]; !isFront {
			appendUserPeer(p)
		}
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

	peerLDPluginRefs := make([]NodeRef, 0, 1)
	peerLDPluginPaths := make([]VFS, 0, 1)
	var objAddLibSeen BitSet
	peerObjAddLibsGlobal := make([]ARG, 0, 8)
	var ldFlagsSeen BitSet
	peerLDFlagsGlobal := make([]ARG, 0, 4)
	var rpathFlagsSeen BitSet
	peerRPathFlagsGlobal := make([]ARG, 0, 4)

	peerAddInclGlobal := make([]VFS, 0, 16)

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

	var resourceGlobalsClosure []ResourceDecl
	deduper.reset()

	for _, rp := range resolved {
		for _, decl := range rp.result.ResourceGlobalClosure {
			if deduper.add(VFS(decl.GlobalVar)) {
				resourceGlobalsClosure = append(resourceGlobalsClosure, decl)
			}
		}
	}

	d.tc = resolveModuleToolchain(resourceGlobalsClosure, instance.Platform.ClangVer)

	fsMemberRefs, fsMemberPaths := emitFromSandboxes(ctx, instance, d)
	emitBundles(ctx, instance, d)

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

	deduper.reset()

	linkTarget := isProgramModuleType(d.moduleStmt.Name)

	sbomOrder := archiveOrder
	{
		cxxIdx, libcxxIdx := -1, -1

		for i, rp := range archiveOrder {
			switch rp.path {
			case "contrib/libs/cxxsupp":
				cxxIdx = i
			case "contrib/libs/cxxsupp/libcxx":
				libcxxIdx = i
			}
		}

		if cxxIdx > libcxxIdx && libcxxIdx >= 0 {
			reordered := make([]resolvedPeer, 0, len(archiveOrder))
			cxx := archiveOrder[cxxIdx]

			for i, rp := range archiveOrder {
				if i == cxxIdx {
					continue
				}

				reordered = append(reordered, rp)

				if i == libcxxIdx {
					reordered = append(reordered, cxx)
				}
			}

			sbomOrder = reordered
		}
	}

	if linkTarget && len(allocatorExplicitPeers) > 0 {
		allocSet := make(map[string]struct{}, len(allocatorExplicitPeers))

		for _, p := range allocatorExplicitPeers {
			allocSet[filepath.Clean(p)] = struct{}{}
		}

		lldIdx, allocIdx := -1, -1

		for i, rp := range sbomOrder {
			if rp.path == "build/platform/lld" {
				lldIdx = i
			}

			if _, ok := allocSet[rp.path]; ok && allocIdx < 0 {
				allocIdx = i
			}
		}

		if lldIdx >= 0 && allocIdx >= 0 && lldIdx < allocIdx {
			relocated := make([]resolvedPeer, 0, len(sbomOrder))
			lld := sbomOrder[lldIdx]

			for i, rp := range sbomOrder {
				if i == lldIdx {
					continue
				}

				if i == allocIdx {
					relocated = append(relocated, lld)
				}

				relocated = append(relocated, rp)
			}

			sbomOrder = relocated
		}
	}

	ownSbomInsertIdx := -1

	for _, rp := range sbomOrder {
		pr := rp.result

		for i, p := range pr.PeerSbomClosurePaths {
			if p == lldToolchainSbomVFS {
				continue
			}

			if deduper.add(p) {
				peerSbomRefs = append(peerSbomRefs, pr.PeerSbomClosureRefs[i])
				peerSbomPaths = append(peerSbomPaths, p)
			}
		}

		if rp.path == "build/platform/lld" && d.moduleStmt.Name == tokPy3Program {
			ownSbomInsertIdx = len(peerSbomPaths)
		}

		if pr.SbomComponentRef != nil && (*pr.SbomComponentPath != lldToolchainSbomVFS || linkTarget) && deduper.add(*pr.SbomComponentPath) {
			peerSbomRefs = append(peerSbomRefs, *pr.SbomComponentRef)
			peerSbomPaths = append(peerSbomPaths, *pr.SbomComponentPath)
		}
	}

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

	deduper.reset()

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

	for _, rp := range resolved {
		if rp.kind == peerKindUnitTestPeer {
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

	for _, rp := range resolved {
		if rp.kind == peerKindProgramDefault {
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		}
	}

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

	var ownProtoInclude []VFS

	if d.protoNamespace != nil {
		ownProtoInclude = []VFS{source(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())))}
	}

	ownProtoInclude = append(ownProtoInclude, d.protoAddInclGlobal...)
	peerProtoInclude := make([]VFS, 0, 4)

	deduper.reset()

	for _, rp := range resolved {
		for _, p := range rp.result.ProtoInclude {
			if deduper.add(p) {
				peerProtoInclude = append(peerProtoInclude, p)
			}
		}
	}

	effectiveProtoInclude := dedupVFS(ownProtoInclude, peerProtoInclude)

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
		if !cxxFlagsSeen.has(uint32(baseUnitCxxNostdinc)) {
			cxxFlagsSeen.add(uint32(baseUnitCxxNostdinc))
			peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, baseUnitCxxNostdinc)
		}
	}

	ccRefs := make([]NodeRef, 0, len(d.srcs)+len(d.joinSrcs))
	ccOutputs := make([]VFS, 0, len(d.srcs)+len(d.joinSrcs))

	arDeclMeta := map[VFS]SrcMeta{}

	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	dedupedAddIncl := dedupVFS(d.addIncl, d.addInclGlobal)

	isPy3NativeLib := d.moduleStmt.Name == tokPy23NativeLibrary ||
		d.moduleStmt.Name == tokPy23Library

	perModuleCCTag := moduleCCTag(d.moduleStmt.Name)

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

	effectiveSrcDirs := d.srcDirs

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		effectiveSrcDirs = append(append([]VFS{}, d.srcDirs...), dirKey(*pd))
	}

	moduleInputs := ModuleCCInputs{
		InclArgs:             ctx.inclArgs,
		Flags:                d.flags,
		AddIncl:              dedupedAddIncl,
		PeerAddInclGlobal:    selfPeerAddInclGlobal,
		ProtoInclude:         effectiveProtoInclude,
		ProtoIncludePeers:    peerProtoInclude,
		CFlags:               ownCFlags,
		CXXFlags:             d.cxxFlags,
		COnlyFlags:           d.cOnlyFlags,
		ClangWarnings:        d.clangWarnings,
		OwnCFlagsGlobal:      ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:    ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:  ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:     peerCFlagsGlobal,
		PeerCXXFlagsGlobal:   peerCXXFlagsGlobal,
		PeerCOnlyFlagsGlobal: peerCOnlyFlagsGlobal,
		ModuleScopeCFlags:    d.moduleScopeCFlags,
		SFlags:               d.sFlags,
		SrcDirs:              effectiveSrcDirs,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		Py3Suffix:            isPy3NativeLib,
		ObjectSuffixStem: func() *string {
			if isYqlUdfStaticModule(d.moduleStmt.Name) {
				return stringPtr("udfs")
			}

			return nil
		}(),
		ModuleTag:   perModuleCCTag,
		Ragel6Flags: d.ragel6Flags,
		BisonFlags:  d.bisonFlags,
		BisonGenExt: d.bisonGenExt.string(),
		NoOptimize:  d.noOptimize,
		TC:          d.tc,
	}
	moduleInputs.ScanCfg = newScanContext(ctx.parsers, dedupedAddIncl, selfPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

	moduleInputs.ScanCfg.OwnerModuleTag = cfModuleTag(d, instance)
	moduleInputs.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &moduleInputs)

	type codegenEmit struct {
		srcID STR
		emit  *SourceEmit
	}

	codegenEmits := make([]codegenEmit, 0, 4)

	for _, src := range d.srcs {
		switch srcExtClassOf(src) {
		case srcExtFbs:
			emitFlatcProducer(ctx, instance, d, resolveSourceVFS(ctx, instance, src.string(), d.srcDirs), &flatcVariantFL, nil)
		case srcExtFbs64:
			emitFlatcProducer(ctx, instance, d, resolveSourceVFS(ctx, instance, src.string(), d.srcDirs), &flatcVariantFL64, nil)
		}
	}

	for _, src := range d.srcs {
		if srcExtClassOf(src) == srcExtY {
			emitBisonProducer(ctx, instance, src.string(), moduleInputs, moduleInputs.BisonGenExt)
		}
	}

	for _, src := range d.srcs {
		if srcExtClassOf(src) == srcExtProto {
			emitProtoProducer(ctx, instance, d, src.string(), moduleInputs)
		}
	}

	cythonPlans := planCythonCpp(ctx, instance, d, moduleInputs)

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

	cpMemberRefs, cpMemberOuts, cpMemberSrcs := emitCopyFiles(ctx, instance, d, &moduleInputs)

	for i, ref := range cpMemberRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, cpMemberOuts[i])
	}

	jvCCRefs, jvCCOutputs := emitMiscNodes(ctx, instance, d, &moduleInputs)

	prCCRes := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	dmCCRes := emitDecimalMD5ForAR(ctx, instance, d, moduleInputs)
	scCCRes := emitSplitCodegensForAR(ctx, instance, d, moduleInputs)
	emitBaseCodegensForAR(ctx, instance, d, moduleInputs)
	pyCCRes := emitRunPythonForAR(ctx, instance, d, moduleInputs)

	aaCCRes := emitArchiveAsmForAR(ctx, instance, d, moduleInputs)

	enCCRes := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

	emitLuaJit21(ctx, instance, d)

	emitArchives(ctx, instance, d)

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
	appendCC := func(srcID STR, emit *SourceEmit, generated bool) {
		if emit == nil {
			return
		}

		m := d.srcMetaOf(srcID)
		m.Generated = generated

		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		arDeclMeta[emit.OutPath] = m

		for _, ex := range emit.Extra {
			ccRefs = append(ccRefs, ex.Ref)
			ccOutputs = append(ccOutputs, ex.OutPath)
			arDeclMeta[ex.OutPath] = m
		}
	}

	for _, src := range d.srcs {
		if isCodegenProducingSrcID(src) {
			continue
		}

		srcRel := src.string()
		appendCC(src, emitOneSource(ctx, instance, d, srcRel, emitSrcInputs(src, srcRel)), false)
	}

	for _, ce := range codegenEmits {
		appendCC(ce.srcID, ce.emit, true)
	}

	for _, fe := range d.srcExtraFlat {
		si := moduleInputs
		si.FlatOutput = true

		if len(fe.Flags) > 0 {
			si.PerSourceCFlags = fe.Flags
		}

		if emit := emitOneSource(ctx, instance, d, fe.Src.string(), si); emit != nil {
			ccRefs = append(ccRefs, emit.Ref)
			ccOutputs = append(ccOutputs, emit.OutPath)
			arDeclMeta[emit.OutPath] = SrcMeta{Prio: stmtPrioDefault, Seq: fe.Seq}
		}
	}

	genCCMeta := func(emit *SourceEmit, m SrcMeta) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		arDeclMeta[emit.OutPath] = m

		for _, ex := range emit.Extra {
			ccRefs = append(ccRefs, ex.Ref)
			ccOutputs = append(ccOutputs, ex.OutPath)
			arDeclMeta[ex.OutPath] = m
		}
	}
	genCC := func(emit *SourceEmit) {
		genCCMeta(emit, SrcMeta{Prio: stmtPrioDefault, Generated: true})
	}

	for _, emit := range emitCheckConfigH(ctx, instance, d, moduleInputs) {
		genCC(emit)
	}

	for _, emit := range emitCythonCppPlanned(ctx, instance, d, moduleInputs, cythonPlans) {
		genCC(emit)
	}

	for _, emit := range emitSwigC(ctx, instance, d, moduleInputs) {
		genCC(emit)
	}

	for i, ref := range jvCCRefs {
		genCC(&SourceEmit{Ref: ref, OutPath: jvCCOutputs[i]})
	}

	if enCCRes != nil {
		for i, ref := range enCCRes.CCRefs {
			genCCMeta(&SourceEmit{Ref: ref, OutPath: enCCRes.CCOutputs[i]},
				SrcMeta{Prio: stmtPrioDefault, Seq: enCCRes.Seqs[i], Generated: true, SecondLevel: enCCRes.SecondLevel[i]})
		}
	}

	if prCCRes != nil {
		for i, ref := range prCCRes.CCRefs {
			genCCMeta(&SourceEmit{Ref: ref, OutPath: prCCRes.CCOutputs[i]},
				SrcMeta{Prio: stmtPrioDefault, Seq: prCCRes.Seqs[i], Generated: true, SecondLevel: prCCRes.SecondLevel[i]})
		}
	}

	if scCCRes != nil {
		for i, ref := range scCCRes.CCRefs {
			genCC(&SourceEmit{Ref: ref, OutPath: scCCRes.CCOutputs[i]})
		}
	}

	if pyCCRes != nil {
		for i, ref := range pyCCRes.CCRefs {
			genCC(&SourceEmit{Ref: ref, OutPath: pyCCRes.CCOutputs[i]})
		}
	}

	if aaCCRes != nil {
		for i, ref := range aaCCRes.CCRefs {
			genCC(&SourceEmit{Ref: ref, OutPath: aaCCRes.CCOutputs[i]})
		}
	}

	if dmCCRes != nil {
		for i, ref := range dmCCRes.CCRefs {
			genCC(&SourceEmit{Ref: ref, OutPath: dmCCRes.CCOutputs[i]})
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
		arDeclMeta[emit.OutPath] = SrcMeta{Prio: stmtPrioDefault, Seq: e.Seq}
	}

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
		arDeclMeta[outPath] = SrcMeta{Prio: stmtPrioDefault, Seq: js.Seq, Generated: true}
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

			arDeclMeta[regRes.Outputs[i]] = SrcMeta{Prio: stmtPrioDefault, Generated: true}
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

	if ctx.sbomEnabled && env.bool(envCLANG) && len(ccRefs) > 0 {
		if r, p := clangToolchainSbomComponent(ctx, instance.Platform); r != nil && !containsVFS(peerSbomPaths, *p) {
			peerSbomRefs = append(peerSbomRefs, *r)
			peerSbomPaths = append(peerSbomPaths, *p)
		}
	}

	ccRefs = append(ccRefs, fsMemberRefs...)
	ccOutputs = append(ccOutputs, fsMemberPaths...)

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
		}

		ldInstance := instance

		if d.moduleStmt.Name == tokPy2Program || d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin {
			ldInstance.Language = LangPy
		}

		ldCCRefs := ccRefs
		ldCCOutputs := ccOutputs

		ldCCRefs, ldCCOutputs = reorderARMembers(ldCCRefs, ldCCOutputs, arDeclMeta)

		var ldObjcopyRefs []NodeRef
		var ldObjcopyPaths []VFS

		if resourceLibTagForData(d) != nil || len(d.resources) > 0 {
			objcopyRes := emitResourceObjcopy(ctx, instance, d, moduleInputs)

			if objcopyRes != nil && len(objcopyRes.Refs) > 0 {
				ldObjcopyRefs = objcopyRes.Refs
				ldObjcopyPaths = objcopyRes.Outputs
			}
		}

		var ownRPathFlags []ARG

		if len(peerDynamicPaths) > 0 {
			ownRPathFlags = append([]ARG(nil), peerRPathFlagsGlobal...)
		}

		wantsStrip := (d.moduleStmt.Name == tokPy3ProgramBin || d.moduleStmt.Name == tokPy3Program) && !d.noStrip

		var programModuleTag STR

		if d.moduleStmt.Name == tokPy3Program {
			programModuleTag = tagPy3Bin
		}

		var ownSbomRef *NodeRef
		var ownSbomPath *VFS

		if sbomActive(ctx, instance) && sbomQualifies(d) {
			ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, binaryName)
		}

		var ldSbomRefs []NodeRef
		var ldSbomPaths []VFS

		if instance.Platform.BuildRelease {
			ldSbomRefs = peerSbomRefs
			ldSbomPaths = peerSbomPaths

			if ownSbomRef != nil {
				if ownSbomInsertIdx >= 0 && ownSbomInsertIdx <= len(peerSbomPaths) {
					ldSbomRefs = make([]NodeRef, 0, len(peerSbomRefs)+1)
					ldSbomRefs = append(ldSbomRefs, peerSbomRefs[:ownSbomInsertIdx]...)
					ldSbomRefs = append(ldSbomRefs, *ownSbomRef)
					ldSbomRefs = append(ldSbomRefs, peerSbomRefs[ownSbomInsertIdx:]...)

					ldSbomPaths = make([]VFS, 0, len(peerSbomPaths)+1)
					ldSbomPaths = append(ldSbomPaths, peerSbomPaths[:ownSbomInsertIdx]...)
					ldSbomPaths = append(ldSbomPaths, *ownSbomPath)
					ldSbomPaths = append(ldSbomPaths, peerSbomPaths[ownSbomInsertIdx:]...)
				} else {
					ldSbomRefs = append(append([]NodeRef(nil), peerSbomRefs...), *ownSbomRef)
					ldSbomPaths = append(append([]VFS(nil), peerSbomPaths...), *ownSbomPath)
				}
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
			d.noOptimize,
			wantsStrip,
			d.useArcadiaLibm,
			d.splitDwarf,
			programModuleTag,
			len(d.bundles) > 0,
			d.tc,
			ctx.host,
			ctx.scripts,
			ctx.emit,
			ctx.vcsRef,
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
			ProtoInclude:                    effectiveProtoInclude,
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

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, arDeclMeta)

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

	objcopyRes := emitResourceObjcopy(ctx, instance, d, moduleInputs)

	if objcopyRes != nil {
		globalRefs = append(globalRefs, objcopyRes.Refs...)
		globalOutputs = append(globalOutputs, objcopyRes.Outputs...)

		leadCount := len(objcopyRes.Refs) - objcopyRes.PySrcTrailCount

		if globalSrcMemberCount > 0 && leadCount > 0 && len(d.resources) > 0 {
			objBase := len(globalRefs) - len(objcopyRes.Refs)
			globalRefs = moveSubrangeToFront(globalRefs, objBase, leadCount)
			globalOutputs = moveSubrangeToFront(globalOutputs, objBase, leadCount)
		}
	}

	if len(ccRefs) > 0 {
		if perModuleCCTag != 0 {
			arRef = emitARNamedTagged(arInstance, arBaseName, perModuleCCTag, ccRefs, ccOutputs, nil, arPluginVFS, cpMemberSrcs, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = emitARNamed(arInstance, arBaseName, ccRefs, ccOutputs, nil, arPluginVFS, cpMemberSrcs, d.tc, ctx.host, ctx.emit)
		}
	}

	var arPath *VFS

	if len(ccRefs) > 0 {
		arPath = vfsPtr(build(instance.Path.rel() + "/" + arBaseName))
	}

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) && !d.programPairedLib {
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
		ProtoInclude:                    effectiveProtoInclude,
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

		if d.programPairedLib {
			globalTag = tagPy3BinLibGlobal
		}

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, arDeclMeta)
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

func moveSubrangeToFront[T any](in []T, start, count int) []T {
	if count <= 0 || start < 0 || start+count > len(in) {
		return in
	}

	out := make([]T, 0, len(in))
	out = append(out, in[start:start+count]...)
	out = append(out, in[:start]...)
	out = append(out, in[start+count:]...)

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
	addIncl              []VFS
	protoInclude         []VFS
	cFlags               []ARG
	cxxFlags             []ARG
	cOnlyFlags           []ARG
	objAddLibs           []ARG
	ldFlags              []ARG
	rpathFlags           []ARG
	archiveRefs          []NodeRef
	archivePaths         []VFS
	globalRefs           []NodeRef
	globalPaths          []VFS
	wholeArchiveRefs     []NodeRef
	wholeArchivePaths    []VFS
	wholeArchiveCmdPaths []VFS
	ldPluginRefs         []NodeRef
	ldPluginPaths        []VFS
	dynamicRefs          []NodeRef
	dynamicPaths         []VFS
	sbomRefs             []NodeRef
	sbomPaths            []VFS
	resourceGlobals      []ResourceDecl
}

func walkPeersForGlobalAddIncl(ctx *GenCtx, instance ModuleInstance, d *ModuleData) PeerGlobalContribs {
	defaults := defaultPeerdirsForModule(ctx, instance, d)

	defaults = suppressMallocAPIDefault(defaults, d.allocatorName)
	seen := make(map[string]struct{}, len(defaults)+len(d.peerdirs))

	resolved := make([]*ModuleEmitResult, 0, len(defaults)+len(d.peerdirs))

	walkInstance := func(peerInstance ModuleInstance) {
		resolved = append(resolved, genModule(ctx, peerInstance))
	}

	walk := func(peerPath string) {
		walkInstance(derivePeerInstance(ctx, instance, d, peerPath))
	}

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

	firstPeerdirIdx := len(resolved)

	frontSet := map[string]struct{}{}

	for _, p := range d.protoCmdPeers {
		frontSet[filepath.Clean(p.string())] = struct{}{}
	}

	peerdirFront := make([]bool, firstPeerdirIdx, firstPeerdirIdx+len(d.peerdirs))

	for _, p := range d.peerdirs {
		if _, dup := seen[p.string()]; dup {
			continue
		}

		seen[p.string()] = struct{}{}
		peerPath := filepath.Clean(p.string())
		_, isFront := frontSet[peerPath]
		peerdirFront = append(peerdirFront, isFront)
		walk(peerPath)
	}

	orderedResolved := resolved

	if firstPeerdirIdx < len(resolved) {
		orderedResolved = make([]*ModuleEmitResult, 0, len(resolved))
		orderedResolved = append(orderedResolved, resolved[:firstPeerdirIdx]...)

		for i := firstPeerdirIdx; i < len(resolved); i++ {
			if peerdirFront[i] {
				orderedResolved = append(orderedResolved, resolved[i])
			}
		}

		for i := firstPeerdirIdx; i < len(resolved); i++ {
			if !peerdirFront[i] {
				orderedResolved = append(orderedResolved, resolved[i])
			}
		}
	}

	out := PeerGlobalContribs{}

	deduper.reset()

	for _, pr := range resolved {
		for _, decl := range pr.ResourceGlobalClosure {
			if deduper.add(VFS(decl.GlobalVar)) {
				out.resourceGlobals = append(out.resourceGlobals, decl)
			}
		}
	}

	deduper.reset()

	addInclFrom := func(pr *ModuleEmitResult) {
		for _, p := range pr.AddInclGlobal {
			if deduper.add(p) {
				out.addIncl = append(out.addIncl, p)
			}
		}
	}

	for _, pr := range orderedResolved {
		addInclFrom(pr)
	}

	deduper.reset()

	for _, pr := range resolved {
		for _, p := range pr.ProtoInclude {
			if deduper.add(p) {
				out.protoInclude = append(out.protoInclude, p)
			}
		}
	}

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

	deduper.reset()

	for _, pr := range orderedResolved {
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

	deduper.reset()

	for _, pr := range orderedResolved {
		for i, p := range pr.PeerSbomClosurePaths {
			if p == lldToolchainSbomVFS {
				continue
			}

			if deduper.add(p) {
				out.sbomRefs = append(out.sbomRefs, pr.PeerSbomClosureRefs[i])
				out.sbomPaths = append(out.sbomPaths, p)
			}
		}

		if pr.SbomComponentRef != nil && *pr.SbomComponentPath != lldToolchainSbomVFS && deduper.add(*pr.SbomComponentPath) {
			out.sbomRefs = append(out.sbomRefs, *pr.SbomComponentRef)
			out.sbomPaths = append(out.sbomPaths, *pr.SbomComponentPath)
		}
	}

	deduper.reset()

	for _, pr := range orderedResolved {
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

	deduper.reset()

	for _, pr := range orderedResolved {
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

	deduper.reset()

	for _, pr := range orderedResolved {
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

	for _, pr := range orderedResolved {
		for i, p := range pr.LDPluginPaths {
			if deduper.add(p) {
				out.ldPluginRefs = append(out.ldPluginRefs, pr.LDPluginRefs[i])
				out.ldPluginPaths = append(out.ldPluginPaths, p)
			}
		}
	}

	deduper.reset()

	for _, pr := range orderedResolved {
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
		strings.HasSuffix(srcRel, ".gztproto") ||
		strings.HasSuffix(srcRel, ".fbs64") ||
		strings.HasSuffix(srcRel, ".fbs") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".cfgproto") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".y") ||
		strings.HasSuffix(srcRel, ".ypp") ||
		strings.HasSuffix(srcRel, ".cpp.in") ||
		strings.HasSuffix(srcRel, ".c.in") ||
		strings.HasSuffix(srcRel, ".sc") ||
		strings.HasSuffix(srcRel, ".gperf") ||
		strings.HasSuffix(srcRel, ".lpp") ||
		strings.HasSuffix(srcRel, ".lex") ||
		strings.HasSuffix(srcRel, ".l")
}

func reorderARMembers(refs []NodeRef, paths []VFS, declMeta map[VFS]SrcMeta) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
		key  uint64
	}

	defaultKey := SrcMeta{Prio: stmtPrioDefault}.sortKey()
	members := make([]member, len(paths))

	for i := range paths {
		k := defaultKey

		if m, ok := declMeta[paths[i]]; ok {
			k = m.sortKey()
		}

		members[i] = member{refs[i], paths[i], k}
	}

	slices.SortStableFunc(members, func(a, b member) int {
		return cmp.Compare(a.key, b.key)
	})

	outRefs := make([]NodeRef, len(members))
	outPaths := make([]VFS, len(members))

	for i, m := range members {
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

	if res.LDRef != NodeRef(0) {
		ctx.tools.put(modulePath, res)
		ctx.moduleByRef.put(res.LDRef, res)
	}

	return res
}

func (ctx *GenCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	return ctx.scannerForPlatform(instance.Platform)
}

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
