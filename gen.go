package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

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

	// AddInclOneLevel propagates to direct PEERDIR consumers only (one hop, not
	// transitive). Direct consumers absorb these paths into their own effective
	// addincl; they are NOT re-propagated via AddInclGlobal.
	AddInclOneLevel []VFS

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
	fs         *FS
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

	host   *Platform
	target *Platform

	testMode bool
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
				if info := reg.Lookup(v); info != nil {
					if !info.HasProducerRef && info.DeferredCF != nil {

						def := info.DeferredCF
						cfRef, _ := EmitCF(def.instance, def.srcVFS, def.outVFS, def.cfgVars, def.includeInputs, consumer.Path, ctx.emit)
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

var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

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
	"RECURSE_ROOT_RELATIVE": {},
	"ALLOCATOR_IMPL":        {},
	"NEED_CHECK":            {},
	"IDE_FOLDER":            {},
	"EXTRALIBS":             {},
	"HEADERS":               {},
	"DISABLE":               {},
	"NO_BUILD_IF":           {},
	"NO_SANITIZE":           {},
	"NO_SANITIZE_COVERAGE":  {},
	"DEFAULT":               {},
	"PROVIDES":              {},
	"USE_CXX":               {},
	"DEFINE_VARIABLE":       {},
	"PYTHON3":               {},
	"BUILD_ONLY_IF":         {},
	"MESSAGE":               {},

	"NO_CLANG_COVERAGE":      {},
	"NO_CLANG_MCDC_COVERAGE": {},
	"NO_PROFILE_RUNTIME":     {},
	"WITHOUT_VERSION":        {},
	"NO_CLANG_TIDY":          {},

	"USE_PYTHON2":                    {},
	"PYTHON3_ADDINCL":                {},
	"PYTHON2_ADDINCL":                {},
	"NO_PYTHON_COVERAGE":             {},
	"NO_IMPORT_TRACING":              {},
	"NO_LINT":                        {},
	"STYLE_PYTHON":                   {},
	"WINDOWS_LONG_PATH_MANIFEST":     {},
	"INCLUDE_TAGS":                   {},
	"INDUCED_DEPS":                   {},
	"NO_PYTHON2":                     {},
	"CHECK_DEPENDENT_DIRS":           {},
	"SUBSCRIBER":                     {},
	"OWNER":                          {},
	"LICENSE_RESTRICTION_EXCEPTIONS": {},
	"LICENSE_RESTRICTION":            {},
	"RESTRICT_PATH":                  {},
	"NO_OPTIMIZE":                    {},
	"TASKLET":                        {},
	"TASKLETSUPPORT":                 {},

	"OPENSOURCE_PROJECT": {},
	"SPLIT_FACTOR":       {},
	"FORK_TESTS":         {},
	"FORK_SUBTESTS":      {},
	"SIZE":               {},
	"TAG":                {},
	"REQUIREMENTS":       {},
	"TIMEOUT":            {},
	"ENV":                {},
	"DATA":               {},
	"TEST_SRCS":          {},
	"LINT":               {},
	"NO_YMAKE_PYTHON":    {},
	"USE_LIGHT_PY2CC":    {},

	"SUPPRESSIONS":                    {},
	"OPENSOURCE_EXPORT_REPLACEMENT":   {},
	"EXCLUDE_TAGS":                    {},
	"ONLY_TAGS":                       {},
	"FILES":                           {},
	"NO_JOIN_SRC":                     {},
	"MASMFLAGS":                       {},
	"NO_MYPY":                         {},
	"NO_OPTIMIZE_PY_PROTOS":           {},
	"PROTO_NAMESPACE":                 {},
	"PY_NAMESPACE":                    {},
	"GRPC":                            {},
	"CPP_PROTO_PLUGIN0":               {},
	"CPP_PROTO_PLUGIN":                {},
	"CPP_PROTO_PLUGIN2":               {},
	"CPP_EV_PLUGIN":                   {},
	"JAVA_SRCS":                       {},
	"JAVA_CLASSPATH_IGNORE_CONFLICTZ": {},
}

func runGenInto(srcRoot, targetDir string, hostP, targetP *Platform, emitter Emitter, onWarn func(Warn)) NodeRef {
	return runGenIntoWithResources(srcRoot, targetDir, hostP, targetP, emitter, onWarn, nil, false, true)
}

func runGenIntoWithResources(srcRoot, targetDir string, hostP, targetP *Platform, emitter Emitter, onWarn func(Warn), resources *resourceFetchPlan, testMode bool, materializeResourceFetches bool) NodeRef {
	plainEmit := emitter
	resourceEmit := resourceGraphEmitter(hostP, plainEmit, resources, materializeResourceFetches)

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
		sourceRoot:         srcRoot,
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

	return root.LDRef
}

func Gen(sourceRoot string, targetDir string, hostP, targetP *Platform, onWarn func(Warn)) *Graph {
	return genWithResources(sourceRoot, targetDir, hostP, targetP, onWarn, nil, false, true)
}

func GenWithResources(sourceRoot string, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	return genWithResources(sourceRoot, targetDir, hostP, targetP, onWarn, resources, testMode, true)
}

func GenDumpGraphWithResources(sourceRoot string, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(sourceRoot, targetDir, hostP, targetP, emitter, onWarn, resources, testMode, false)

	return finalizeDumpGraph(emitter)
}

func genWithResources(sourceRoot string, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool, materializeResourceFetches bool) *Graph {
	emitter := NewBufferedEmitter()
	runGenIntoWithResources(sourceRoot, targetDir, hostP, targetP, emitter, onWarn, resources, testMode, materializeResourceFetches)

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

	if len(d.enumSrcs) > 0 && instance.Path != "tools/enum_parser/enum_serialization_runtime" {
		d.peerdirs = append(d.peerdirs, "tools/enum_parser/enum_serialization_runtime")
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

		protoResult := emitProtoSrcs(ctx, instance, d, peerContribs)

		if d.moduleStmt.Name != "PROTO_LIBRARY" {
			emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, nil)
		}

		emitMiscNodes(ctx, instance, d, nil)

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
			AddInclOneLevel:  cloneVFSs(d.addInclOneLevel),

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

	peerArchiveSeen := map[VFS]struct{}{}
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

	// Direct PEERDIR consumers absorb the peer's AddInclOneLevel into their own
	// addincl (one hop only — see moduleEmitResult.AddInclOneLevel comment).
	// Goes through d.addIncl (own bag), so it reaches this module's CC compile
	// flags but is NOT re-propagated via result.AddInclGlobal upstream.
	for _, rp := range resolved {
		if rp.kind != peerKindUserPeer {
			continue
		}

		d.addIncl = append(d.addIncl, rp.result.AddInclOneLevel...)
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

	effectiveAddInclGlobal := mergeDedupVFS(d.addInclGlobal, peerAddInclGlobal)

	if instance.Path == "library/python/runtime_py3" {
		buildRootPath := Intern("$(B)/library/python/runtime_py3")
		abseilPath := Intern("$(S)/contrib/restricted/abseil-cpp")
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
			Intern("$(S)/contrib/libs/cxxsupp/libcxx/include"),
			Intern("$(S)/contrib/libs/cxxsupp/libcxxrt/include"),
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

	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	dedupedAddIncl := d.addIncl

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
		InclArgs:             ctx.inclArgs,
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
		ModuleScopeCFlags:    d.moduleScopeCFlags,
		SFlags:               d.sFlags,
		SrcDir:               effectiveSrcDir,
		SourceRoot:           ctx.sourceRoot,
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
		BisonGenExt: d.bisonGenExt,
	}

	ancestorRebase := d.srcDir != nil && d.moduleStmt.Name == "PROGRAM" && isAncestorPath(*d.srcDir, instance.Path)

	emitCopyFiles(ctx, instance, d, &moduleInputs)

	enCCRes := emitEnumSrcs(ctx, instance, d, selfPeerAddInclGlobal, &moduleInputs)

	jvCCRefs, jvCCOutputs := emitMiscNodes(ctx, instance, d, &moduleInputs)

	prCCRes := emitRunProgramsForAR(ctx, instance, d, moduleInputs)
	pyCCRes := emitRunPythonForAR(ctx, instance, d, moduleInputs)
	emitArchives(ctx, instance, d)

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

	for _, src := range d.srcs {

		srcInputs := moduleInputs
		if extras, ok := d.perSrcCFlags[src]; ok {
			srcInputs.PerSourceCFlags = extras
		}

		isFlatNoLto := false
		if _, ok := d.flatSrcs[src]; ok {
			srcInputs.FlatOutput = true
			isFlatNoLto = true
		}
		srcInputs = adjustCythonCompanionSourceInputs(d, src, srcInputs)

		emit, hadPre := preEmitted[src]
		if !hadPre {
			emit = emitOneSource(ctx, instance, d, src, srcInputs, ancestorRebase)
		}

		if emit == nil {
			continue
		}

		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, isFlatNoLto)
		ccIsCFGenerated = append(ccIsCFGenerated, strings.HasSuffix(src, ".cpp.in") || strings.HasSuffix(src, ".c.in"))
	}

	for _, emit := range emitCheckConfigH(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)

	}

	for _, emit := range emitCythonCpp(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)

	}

	for _, emit := range emitSwigC(ctx, instance, d, moduleInputs) {
		ccRefs = append(ccRefs, emit.Ref)
		ccOutputs = append(ccOutputs, emit.OutPath)
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, true)

	}

	for i, ref := range jvCCRefs {
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, jvCCOutputs[i])
		ccIsFlatNoLto = append(ccIsFlatNoLto, false)
		ccIsCFGenerated = append(ccIsCFGenerated, false)
	}

	if enCCRes != nil {
		for i, ref := range enCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, enCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
		}
	}

	if prCCRes != nil {
		for i, ref := range prCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, prCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
		}
	}
	if pyCCRes != nil {
		for i, ref := range pyCCRes.CCRefs {
			ccRefs = append(ccRefs, ref)
			ccOutputs = append(ccOutputs, pyCCRes.CCOutputs[i])
			ccIsFlatNoLto = append(ccIsFlatNoLto, false)
			ccIsCFGenerated = append(ccIsCFGenerated, false)
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

		jsRef, joinOutVFS := EmitJS(srcInstance, js.OutputName, js.Sources, joinClosure, ctx.target, ctx.emit)

		jsRel := strings.TrimPrefix(joinOutVFS.Rel(), srcInstance.Path+"/")

		ccIncludeInputs := jsCCIncludeInputs(srcInstance, js.Sources, ccClosure)

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
				Intern("$(B)/build/cow/on/libbuild-cow-on.a"),
				[]VFS{
					Intern("$(B)/library/cpp/malloc/api/libcpp-malloc-api.a"),
					Intern("$(B)/contrib/libs/jemalloc/libcontrib-libs-jemalloc.a"),
					Intern("$(B)/library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a"),
				},
			)
			ldPeerLinkCmdPaths = movePathsAfter(
				ldPeerLinkCmdPaths,
				Intern("$(B)/build/cow/on/libbuild-cow-on.a"),
				[]VFS{
					Intern("$(B)/library/cpp/malloc/api/libcpp-malloc-api.a"),
					Intern("$(B)/contrib/libs/jemalloc/libcontrib-libs-jemalloc.a"),
					Intern("$(B)/library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a"),
				},
			)
			ldPeerArchiveRefs, ldPeerArchivePaths = moveArchivePathsBefore(
				ldPeerArchiveRefs,
				ldPeerArchivePaths,
				Intern("$(B)/library/cpp/json/common/libcpp-json-common.a"),
				[]VFS{
					Intern("$(B)/tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"),
				},
			)
			ldPeerLinkCmdPaths = movePathsBefore(
				ldPeerLinkCmdPaths,
				Intern("$(B)/library/cpp/json/common/libcpp-json-common.a"),
				[]VFS{
					Intern("$(B)/tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"),
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

		if resourceModuleTagForData(d) != nil {
			emitPySrcs(ctx, instance, d)

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

		wantsStrip := d.moduleStmt.Name == "PY3_PROGRAM_BIN"
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
			d.flags.NoCompilerWarnings,
			wantsStrip,
			d.splitDwarf,
			ctx.host,
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
			OwnAddInclGlobal:                cloneVFSs(d.addInclGlobal),
			AddInclOneLevel:                 cloneVFSs(d.addInclOneLevel),
			CFlagsGlobal:                    effectiveCFlagsGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
			RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
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
			testSuiteInfo:                   suiteInfo,
		}
		ctx.memo[instance] = result

		return result
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, ccIsFlatNoLto, ccIsCFGenerated, numSrcsDerived)

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

	objcopyRes := emitResourceObjcopy(ctx, instance, d)
	if objcopyRes != nil {
		globalRefs = append(globalRefs, objcopyRes.Refs...)
		globalOutputs = append(globalOutputs, objcopyRes.Outputs...)
		if resourceBeforeGlobalSrcs(d) && globalSrcMemberCount > 0 && len(objcopyRes.Refs) > 0 {
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
		OwnAddInclGlobal:                cloneVFSs(d.addInclGlobal),
		AddInclOneLevel:                 cloneVFSs(d.addInclOneLevel),
		CFlagsGlobal:                    effectiveCFlagsGlobal,
		CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
		COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
		ObjAddLibsGlobal:                mergeDedup(peerObjAddLibsGlobal, d.objAddLibsGlobal),
		LDFlagsGlobal:                   mergeDedup(peerLDFlagsGlobal, d.ldFlags),
		RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
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

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, make([]bool, len(globalRefs)), make([]bool, len(globalRefs)), len(globalRefs))
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
	addIncl    []VFS
	cFlags     []string
	cxxFlags   []string
	cOnlyFlags []string
	objAddLibs []string
	ldFlags    []string
	rpathFlags []string

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

func reorderARMembers(refs []NodeRef, paths []VFS, isFlatNoLto []bool, isCFGenerated []bool, numSrcsDerived int) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
	}

	var noLtoSrcs, regularSrcs, cfSrcs, g4Srcs, hSerSrcs, evPbSrcs, rl6Srcs, reg3Srcs, legacyR6 []member

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
