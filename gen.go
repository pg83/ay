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

const (
	peerKindLangDefault    = 0
	peerKindProgramDefault = 1
	peerKindUserPeer       = 2
	peerKindUnitTestPeer   = 3
)

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
	ModuleStmtName                  TOK
	testSuiteInfo                   *TestSuiteInfo
	DescClosure                     []DescProtoPeer
	ResourceGlobalClosure           []ResourceDecl
}

func stringPtr(s string) *string {
	return &s
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
	buckets         *BucketCache
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
	parsedFiles     map[string]*MakeFile
	prodOuts        IdValueMap
}

func resolveCodegenDepRefsIncl(ctx *GenCtx, consumer ModuleInstance, na *NodeArenas, includeInputs []VFS, incl ...NodeRef) []NodeRef {
	deduper.reset()

	out := na.noderefs.alloc(len(incl) + len(includeInputs))
	k := 0

	for _, r := range incl {
		deduper.add(r.strID())
		out[k] = r
		k++
	}

	reg := ctx.codegenFor(consumer)

	for _, p := range includeInputs {
		if !p.isBuild() {
			continue
		}

		info := reg.lookup(p)

		if info == nil {
			continue
		}

		if !deduper.add(info.ProducerRef.strID()) {
			continue
		}

		out[k] = info.ProducerRef
		k++
	}

	na.noderefs.commit(k)

	if k == 0 {
		return nil
	}

	return out[:k]
}

func resolveCodegenDepRefsInclView(ctx *GenCtx, consumer ModuleInstance, na *NodeArenas, cv Closure, incl ...NodeRef) []NodeRef {
	deduper.reset()

	out := na.noderefs.alloc(len(incl) + cv.len())
	k := 0

	for _, r := range incl {
		deduper.add(r.strID())
		out[k] = r
		k++
	}

	reg := ctx.codegenFor(consumer)

	cv.each(func(p VFS) {
		if !p.isBuild() {
			return
		}

		info := reg.lookup(p)

		if info == nil {
			return
		}

		if !deduper.add(info.ProducerRef.strID()) {
			return
		}

		out[k] = info.ProducerRef
		k++
	})

	na.noderefs.commit(k)

	if k == 0 {
		return nil
	}

	return out[:k]
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
		parsedFiles:    map[string]*MakeFile{},
	}

	ctx.inclArgs = InclArgMemo{m: &ctx.inclArgValues}
	ctx.buckets = newBucketCache()

	targetScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(targetP.ISA), targetP.Flags[envMUSL] == strYes, targetP.Flags[envOPENSOURCE] == strYes, targetP.OS, onWarn), onWarn, &ctx.tarjan, ctx.buckets)

	targetScanner.codegen = targetReg
	targetScanner.moduleByRef = &ctx.moduleByRef

	hostScanner := newIncludeScannerWith(parsers, loadSysInclSetForFS(fs, string(hostP.ISA), hostP.Flags[envMUSL] == strYes, hostP.Flags[envOPENSOURCE] == strYes, hostP.OS, onWarn), onWarn, &ctx.tarjan, ctx.buckets)

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

	if ctx.buckets.h1Mismatches > 0 {
		onWarn(Warn{
			Kind: WarnBucketHash,
			Message: fmt.Sprintf("%d h1 mismatches, %d buckets in overflow (pair-collision headroom shrinking)",
				ctx.buckets.h1Mismatches, ctx.buckets.overflowed),
		})
	}

	ctx.emit.result(root.LDRef)

	if ctx.testMode && root.testSuiteInfo != nil {
		for _, ref := range emitTestRunNodes(plainEmit, plainEmit, targetP, *root.testSuiteInfo, root.LDRef, root.ResourceGlobalClosure) {
			ctx.emit.result(ref)
		}
	}

	return root.LDRef
}

func genDumpGraphWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), testMode bool) *Graph {
	emitter := newStreamingEmitter(nil)

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

	return baseName(instance.Path.rel())
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

func (ctx *GenCtx) parseFileCached(rel string) []Stmt {
	rel = cleanRel(rel)

	if mf, ok := ctx.parsedFiles[rel]; ok {
		return mf.Stmts
	}

	mf := throw2(parseFile(ctx.fs, rel))

	ctx.parsedFiles[rel] = mf

	return mf.Stmts
}

func moduleStmts(ctx *GenCtx, dir string) []Stmt {
	stmts := ctx.parseFileCached(joinRel(dir, "ya.make"))

	if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(dir); ok && ctx.fs.isFile(srcRootVFS, inc.rel()) {
		incStmts := ctx.parseFileCached(inc.rel())

		return concat(stmts, incStmts)
	}

	return stmts
}

func applyImplicitPeerdirs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	if instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary {
		hasProtoSrc := false

		for _, src := range d.srcs {
			if extIsProto(src.string()) {
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
			spliced = append(spliced, sTRS(filteredEarly...)...)
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
}

type resolvedPeer struct {
	path   string
	result *ModuleEmitResult
	kind   int
}

func genModule(ctx *GenCtx, instance ModuleInstance) *ModuleEmitResult {
	if existing := ctx.memo.get(ctx.instanceKey(instance)); existing != nil {
		return *existing
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
	d := collectModule(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn)
	e := newEmitContext(ctx, instance, d, nil)

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

	if d.conflictMod != nil {
		throwFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.rel(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.rel())
	}

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangDescProto {
		result := e.emitDescProtoSubmodule()

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	if d.moduleStmt.Name == tokProtoDescriptions {
		result := e.emitProtoDescriptions()

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	if d.moduleStmt.Name == tokResourcesLibrary {
		if e.bindResourceGlobalVars(env) {
			d = collectModule(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn)
			e = newEmitContext(ctx, instance, d, nil)
		}

		return e.genResourcesLibrary()
	}

	if d.moduleStmt.Name == tokPrebuiltProgram {
		env.setString(envMODULE_SUFFIX, prebuiltModuleSuffix(instance.Platform))

		if e.bindResourceGlobalVars(env) {
			d = collectModule(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn)
			e = newEmitContext(ctx, instance, d, nil)
		}

		return e.genPrebuiltProgram()
	}

	if d.moduleStmt.Name != tokLibrary && d.moduleStmt.Name != tokFbsLibrary && d.moduleStmt.Name != tokDllTool && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) {
		throwFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path.rel(), d.moduleStmt.Name)
	}

	applyImplicitPeerdirs(ctx, instance, d)

	if d.moduleStmt.Name == tokDynamicLibrary {
		result := e.emitDynamicLibrary()

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	languageDefaults := e.defaultPeerdirsForModule()

	languageDefaults = suppressMallocAPIDefault(languageDefaults, d.allocatorName)

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path.rel())
	unitTestPeer := unittestForPeerPath(d.moduleStmt)

	var preUserProgDefaults []string
	var postUserProgDefaults []string

	if isProgram {
		preUserProgDefaults = e.defaultProgramPeerdirsForModule(false)
		postUserProgDefaults = e.defaultProgramPeerdirsForModule(true)
	}

	var allocatorExplicitPeers []string

	if isProgram {
		allocatorExplicitPeers = allocatorPeers[d.allocatorName.string()]
	}

	unitTestPeerCount := 0

	if unitTestPeer != "" {
		unitTestPeerCount = 1
	}


	const googleapisPeer = "contrib/libs/googleapis-common-protos"

	internStr(instance.Path.rel())
	internStr(googleapisPeer)

	if unitTestPeer != "" {
		internStr(unitTestPeer)
	}

	for _, ss := range [][]string{languageDefaults, preUserProgDefaults, allocatorExplicitPeers, postUserProgDefaults} {
		for _, p := range ss {
			internStr(p)
		}
	}

	deduper.reset()

	peerSeen := func(p string) bool {
		return !deduper.add(internStr(p).strID())
	}

	preResolved := make([]resolvedPeer, 0, 2)

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy && d.optimizePyProtos && !moduleExcludesTag(d, "CPP_PROTO") {
		peerSeen(instance.Path.rel())

		cppSelf := instance

		cppSelf.Language = LangCPP
		preResolved = append(preResolved, resolvedPeer{path: instance.Path.rel(), result: genModule(ctx, cppSelf), kind: peerKindLangDefault})
	}

	if d.moduleStmt.Name == tokProtoLibrary && d.useCommonGoogleAPIs && instance.Language == LangCPP {
		if !peerSeen(googleapisPeer) {
			preResolved = append(preResolved, resolvedPeer{path: googleapisPeer, result: genModule(ctx, e.derivePeerInstance(googleapisPeer)), kind: peerKindLangDefault})
		}
	}

	allPeers := make([]string, 0, len(languageDefaults)+unitTestPeerCount+len(preUserProgDefaults)+len(allocatorExplicitPeers)+len(d.peerdirs)+len(postUserProgDefaults))
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
		if !deduper.add(p.strID()) {
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

	peerGlobalRefs := make([]NodeRef, 0, len(allPeers))
	peerGlobalPaths := make([]VFS, 0, len(allPeers))
	peerWholeArchiveRefs := make([]NodeRef, 0, len(allPeers))
	peerWholeArchivePaths := make([]VFS, 0, len(allPeers))
	peerWholeArchiveCmdPaths := make([]VFS, 0, len(allPeers))
	peerLinkCmdPaths := make([]VFS, 0, len(allPeers))

	var (
		peerDynamicRefs   []NodeRef
		peerDynamicPaths  []VFS
		peerLDPluginRefs  []NodeRef
		peerLDPluginPaths []VFS
	)

	var peerObjAddLibsGlobal []ARG
	var peerLDFlagsGlobal []ARG
	var peerRPathFlagsGlobal []ARG

	peerAddInclGlobal := make([]VFS, 0, 16)

	var oneLevelOnlyPaths map[VFS]struct{}
	var peerCFlagsGlobal []ARG
	var peerCXXFlagsGlobal []ARG
	var peerCOnlyFlagsGlobal []ARG

	allPeers, peerKinds = applyDeferredPeerOrder(d.moduleStmt.Name, allPeers, peerKinds, allocatorExplicitPeers)

	resolved := append(make([]resolvedPeer, 0, len(preResolved)+len(allPeers)), preResolved...)

	for i, p := range allPeers {
		peerPath := filepath.Clean(p)
		peerVFS := source(peerPath)
		kind := peerKinds[i]

		if kind != peerKindUserPeer && !peerYaMakeExists(ctx.fs, peerVFS) {
			continue
		}

		peerInstance := e.derivePeerInstanceVFS(peerVFS)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.isPROGRAM {
			throwFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path.rel(), peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})
	}

	resGlobalsSum := 0
	archiveCap := 0
	sbomCap := 0

	for _, rp := range resolved {
		resGlobalsSum += len(rp.result.ResourceGlobalClosure)
		archiveCap += len(rp.result.PeerArchiveClosurePaths) + 1
		sbomCap += len(rp.result.PeerSbomClosurePaths) + 1
	}

	resourceGlobalsClosure := make([]ResourceDecl, 0, resGlobalsSum)
	peerArchiveRefs := make([]NodeRef, 0, archiveCap)
	peerArchivePaths := make([]VFS, 0, archiveCap)
	peerSbomRefs := make([]NodeRef, 0, sbomCap)
	peerSbomPaths := make([]VFS, 0, sbomCap)

	deduper.reset()

	for _, rp := range resolved {
		for _, decl := range rp.result.ResourceGlobalClosure {
			if deduper.add(decl.GlobalVar.strID()) {
				resourceGlobalsClosure = append(resourceGlobalsClosure, decl)
			}
		}
	}

	d.tc = resolveModuleToolchain(resourceGlobalsClosure, instance.Platform.ClangVer)

	deduper.reset()

	for _, rp := range resolved {
		for i, p := range rp.result.LDPluginPaths {
			if deduper.add(p.strID()) {
				peerLDPluginRefs = append(peerLDPluginRefs, rp.result.LDPluginRefs[i])
				peerLDPluginPaths = append(peerLDPluginPaths, p)
			}
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p.strID()) {
				peerArchiveRefs = append(peerArchiveRefs, pr.PeerArchiveClosureRefs[i])
				peerArchivePaths = append(peerArchivePaths, p)
			}
		}

		if pr.ARPath != nil && deduper.add(pr.ARPath.strID()) {
			peerArchiveRefs = append(peerArchiveRefs, pr.ARRef)
			peerArchivePaths = append(peerArchivePaths, *pr.ARPath)
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerGlobalClosurePaths {
			if deduper.add(p.strID()) {
				peerGlobalRefs = append(peerGlobalRefs, pr.PeerGlobalClosureRefs[i])
				peerGlobalPaths = append(peerGlobalPaths, p)
			}
		}

		if pr.GlobalRef != nil && pr.GlobalPath != nil && deduper.add(pr.GlobalPath.strID()) {
			peerGlobalRefs = append(peerGlobalRefs, *pr.GlobalRef)
			peerGlobalPaths = append(peerGlobalPaths, *pr.GlobalPath)
		}
	}

	linkTarget := isProgramModuleType(d.moduleStmt.Name) || d.moduleStmt.Name == tokDllTool

	var ownSbomInsertIdx int

	peerSbomRefs, peerSbomPaths, ownSbomInsertIdx = aggregateSbomComponents(d.moduleStmt.Name, linkTarget, resolved, allocatorExplicitPeers, peerSbomRefs, peerSbomPaths)

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerWholeArchiveClosurePaths {
			if deduper.add(p.strID()) {
				peerWholeArchiveRefs = append(peerWholeArchiveRefs, pr.PeerWholeArchiveClosureRefs[i])
				peerWholeArchivePaths = append(peerWholeArchivePaths, p)
			}
		}

		for i, p := range pr.WholeArchivePaths {
			if deduper.add(p.strID()) {
				peerWholeArchiveRefs = append(peerWholeArchiveRefs, pr.WholeArchiveRefs[i])
				peerWholeArchivePaths = append(peerWholeArchivePaths, p)
			}
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for _, p := range pr.PeerWholeArchiveCmdClosurePaths {
			if deduper.add(p.strID()) {
				peerWholeArchiveCmdPaths = append(peerWholeArchiveCmdPaths, p)
			}
		}

		for _, p := range pr.WholeArchiveCmdPaths {
			if deduper.add(p.strID()) {
				peerWholeArchiveCmdPaths = append(peerWholeArchiveCmdPaths, p)
			}
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p.strID()) {
				peerDynamicRefs = append(peerDynamicRefs, pr.PeerDynamicClosureRefs[i])
				peerDynamicPaths = append(peerDynamicPaths, p)
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(pr.LDPath.strID()) {
			peerDynamicRefs = append(peerDynamicRefs, pr.LDRef)
			peerDynamicPaths = append(peerDynamicPaths, *pr.LDPath)
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		pr := rp.result

		for _, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p.strID()) {
				peerLinkCmdPaths = append(peerLinkCmdPaths, p)
			}
		}

		for _, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p.strID()) {
				peerLinkCmdPaths = append(peerLinkCmdPaths, p)
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(pr.LDPath.strID()) {
			peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.LDPath)
		}

		if pr.ARPath != nil && deduper.add(pr.ARPath.strID()) {
			peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.ARPath)
		}
	}

	deduper.reset()

	for _, rp := range resolved {
		switch rp.kind {
		case peerKindLangDefault:
			for _, p := range rp.result.OwnAddInclGlobal {
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}

			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		case peerKindUnitTestPeer, peerKindProgramDefault:
			for _, p := range rp.result.AddInclGlobal {
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}
			}
		case peerKindUserPeer:
			for _, p := range rp.result.AddInclUserGlobal {
				if deduper.add(p.strID()) {
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
				if deduper.add(p.strID()) {
					peerAddInclGlobal = append(peerAddInclGlobal, p)
				}

				if oneLevelOnlyPaths != nil {
					delete(oneLevelOnlyPaths, p)
				}
			}
		}
	}

	cflagsAggOrder := resolved

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.CFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCFlagsGlobal = append(peerCFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.CXXFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.COnlyFlagsGlobal {
			if deduper.add(a.strID()) {
				peerCOnlyFlagsGlobal = append(peerCOnlyFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.ObjAddLibsGlobal {
			if deduper.add(a.strID()) {
				peerObjAddLibsGlobal = append(peerObjAddLibsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.LDFlagsGlobal {
			if deduper.add(a.strID()) {
				peerLDFlagsGlobal = append(peerLDFlagsGlobal, a)
			}
		}
	}

	deduper.reset()

	for _, rp := range cflagsAggOrder {
		for _, a := range rp.result.RPathFlagsGlobal {
			if deduper.add(a.strID()) {
				peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
			}
		}
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

	effectiveAddInclGlobal := dedup(d.addInclGlobal, peerAddInclForProp)

	var ownProtoInclude []VFS

	if d.protoNamespace != nil {
		ownProtoInclude = []VFS{sourceClean(d.protoNamespace.string())}
	}

	ownProtoInclude = append(ownProtoInclude, d.protoAddInclGlobal...)

	peerProtoInclude := make([]VFS, 0, 4)

	deduper.reset()

	for _, rp := range resolved {
		for _, p := range rp.result.ProtoInclude {
			if deduper.add(p.strID()) {
				peerProtoInclude = append(peerProtoInclude, p)
			}
		}
	}

	effectiveProtoInclude := dedup(ownProtoInclude, peerProtoInclude)
	effectiveCFlagsGlobal := dedup(peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := concat(peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := concat(peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)
	effectiveRPathFlagsGlobal := concat(peerRPathFlagsGlobal, d.rpathFlagsGlobal)
	ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins, d.tc)

	mergedLDPlugins := mergeLDPlugins(ownLDPlugins, &LdPluginsResult{
		Refs:  peerLDPluginRefs,
		Paths: peerLDPluginPaths,
	})

	if mergedLDPlugins == nil {
		mergedLDPlugins = &LdPluginsResult{}
	}

	newResult := func() *ModuleEmitResult {
		return &ModuleEmitResult{
			isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
			AddInclGlobal:                   effectiveAddInclGlobal,
			OwnAddInclGlobal:                d.addInclGlobal,
			ProtoInclude:                    effectiveProtoInclude,
			AddInclOneLevel:                 d.addInclOneLevel,
			AddInclUserGlobal:               d.addInclUserGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			ObjAddLibsGlobal:                concat(peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   concat(peerLDFlagsGlobal, d.ldFlags),
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
			PeerSbomClosureRefs:             peerSbomRefs,
			PeerSbomClosurePaths:            peerSbomPaths,
			InducedDeps:                     d.inducedDeps,
			ModuleStmtName:                  d.moduleStmt.Name,
			CFlagsGlobal:                    effectiveCFlagsGlobal,
		}
	}

	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path.rel()] {
		hasNostdinc := false

		for _, a := range peerCXXFlagsGlobal {
			if a == baseUnitCxxNostdinc {
				hasNostdinc = true

				break
			}
		}

		if !hasNostdinc {
			peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, baseUnitCxxNostdinc)
		}
	}

	ownCFlags := d.cFlags
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal
	dedupedAddIncl := dedup(d.addIncl, d.addInclGlobal)

	isPy3NativeLib := d.moduleStmt.Name == tokPy23NativeLibrary ||
		d.moduleStmt.Name == tokPy23Library

	perModuleCCTag := d.unit.CCTag

	var arNameFn func(string) string
	var globalArNameFn func(string) string

	archiveName := ""

	if len(d.moduleStmt.Args) > 0 {
		archiveName = d.moduleStmt.Args[0].string()
	}

	arNameFn = func(dir string) string { return archiveNameWithPrefixOrName(dir, d.unit.ARPrefix, archiveName) }
	globalArNameFn = func(dir string) string { return globalArchiveNameWithPrefixOrName(dir, d.unit.ARPrefix, archiveName) }

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path.rel(), peerAddInclGlobal, dedupedAddIncl)

	if d.moduleStmt.Name == tokProtoLibrary {
		selfPeerAddInclGlobal = peerAddInclGlobal
	}
	effectiveSrcDirs := d.srcDirs

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		effectiveSrcDirs = concat(d.srcDirs, []VFS{dirKey(*pd)})
	}

	d.cc = ModuleCompileEnv{
		InclArgs:             ctx.inclArgs,
		Flags:                d.flags,
		CudaNvccFlags:        d.cudaNvccFlags,
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

	d.cc.ScanCfg = newScanContext(ctx.parsers, dedupedAddIncl, selfPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
	d.cc.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &d.cc)

	e = newEmitContext(ctx, instance, d, &PeerContext{
		SelfAddInclGlobal: selfPeerAddInclGlobal,
		PeerAddInclGlobal: peerAddInclGlobal,
		ResourceGlobals:   resourceGlobalsClosure,
		ProtoInclude:      peerProtoInclude,
	})

	e.cythonAdjustModuleCCBlocks()
	e.sprotoAdjustProtoEnv()
	e.emit()

	local, global := e.partitionCollected()


	globalRefs := global.refs
	globalOutputs := global.outs
	globalMetas := global.metas

	if ctx.sbomEnabled && env.bool(envCLANG) && len(local.refs) > 0 {
		if r, p := clangToolchainSbomComponent(ctx, instance.Platform); r != nil && !containsVFS(peerSbomPaths, *p) {
			peerSbomRefs = append(peerSbomRefs, *r)
			peerSbomPaths = append(peerSbomPaths, *p)
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		binaryName := programBinaryName(instance, d.moduleStmt)
		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths
		ldPeerLinkCmdPaths := peerLinkCmdPaths
		ldInstance := instance
		ldCCRefs := local.refs
		ldCCOutputs := local.outs

		ldCCRefs, ldCCOutputs = reorderARMembers(ldCCRefs, ldCCOutputs, local.metas)

		var ldObjcopyRefs []NodeRef
		var ldObjcopyPaths []VFS

		if e.objcopyRes != nil && len(e.objcopyRes.Refs) > 0 {
			ldObjcopyRefs = e.objcopyRes.Refs
			ldObjcopyPaths = e.objcopyRes.Outputs
		}

		ldObjcopyRefs = append(globalRefs, ldObjcopyRefs...)
		ldObjcopyPaths = append(globalOutputs, ldObjcopyPaths...)

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
			ownSbomRef, ownSbomPath = e.emitSbomComponent(binaryName)
		}

		var ldSbomRefs []NodeRef
		var ldSbomPaths []VFS

		if instance.Platform.BuildRelease {
			ldSbomRefs = peerSbomRefs
			ldSbomPaths = peerSbomPaths

			if ownSbomRef != nil {
				ldSbomRefs, ldSbomPaths = insertOwnSbomComponent(peerSbomRefs, peerSbomPaths, *ownSbomRef, *ownSbomPath, ownSbomInsertIdx)
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
			d.unit.SbomLang,
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

		result := newResult()

		result.ARRef = ldRef
		result.ARPath = &ldPath
		result.isPROGRAM = true
		result.LDRef = ldRef
		result.LDPath = &ldPath
		result.SbomComponentRef = ownSbomRef
		result.SbomComponentPath = ownSbomPath
		result.ResourceGlobalClosure = resourceGlobalsClosure
		result.testSuiteInfo = suiteInfo

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	local.refs, local.outs = reorderARMembers(local.refs, local.outs, local.metas)

	var arRef NodeRef

	arBaseName := arNameFn(instance.Path.rel())
	arInstance := instance

	var arPluginVFS *VFS

	if d.arPlugin != nil {
		v := source(instance.Path.rel(), "/", d.arPlugin.string())

		arPluginVFS = &v
	}

	if objcopyRes := e.objcopyRes; objcopyRes != nil {
		lead := len(objcopyRes.Refs) - objcopyRes.PySrcTrailCount
		objMetas := make([]SrcMeta, len(objcopyRes.Refs))

		for i := range objMetas {
			objMetas[i] = SrcMeta{Prio: stmtPrioDefault}
		}

		if len(e.resources) > 0 && lead > 0 {
			globalRefs = concat(objcopyRes.Refs[:lead], globalRefs, objcopyRes.Refs[lead:])
			globalOutputs = concat(objcopyRes.Outputs[:lead], globalOutputs, objcopyRes.Outputs[lead:])
			globalMetas = concat(objMetas[:lead], globalMetas, objMetas[lead:])
		} else {
			globalRefs = append(globalRefs, objcopyRes.Refs...)
			globalOutputs = append(globalOutputs, objcopyRes.Outputs...)
			globalMetas = append(globalMetas, objMetas...)
		}
	}

	if d.moduleStmt.Name == tokDllTool {
		result := e.emitDllShared(local.refs, local.outs, peerArchiveRefs, peerArchivePaths, peerSbomRefs, peerSbomPaths)

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	if len(local.refs) > 0 {
		if perModuleCCTag != 0 {
			arRef = emitARNamedTagged(arInstance, arBaseName, perModuleCCTag, local.refs, local.outs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = emitARNamed(arInstance, arBaseName, local.refs, local.outs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		}
	}

	var arPath *VFS

	if len(local.refs) > 0 {
		arPath = vfsPtr(build(instance.Path.rel(), "/", arBaseName))
	}

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) && d.unit.Tag != unitTagPy3BinLib {
		realPrjName := strings.TrimSuffix(archiveNameWithPrefixOrName(instance.Path.rel(), "", archiveName), ".a")

		ownSbomRef, ownSbomPath = e.emitSbomComponent(realPrjName)
	}

	result := newResult()

	result.ARRef = arRef
	result.ARPath = arPath
	result.LDRef = arRef
	result.LDPath = arPath
	result.SbomComponentRef = ownSbomRef
	result.SbomComponentPath = ownSbomPath
	result.ResourceGlobalClosure = resourceGlobalsClosure

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path.rel())
		globalTag := d.unit.GlobalARTag

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, globalMetas)

		globalRef := emitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, d.tc, ctx.host, ctx.emit)

		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(build(instance.Path.rel(), "/", globalBaseName))
	}

	if protoResult := e.protoRes; protoResult != nil {
		result.WholeArchiveRefs = protoResult.WholeArchiveRefs
		result.WholeArchivePaths = protoResult.WholeArchivePaths
		result.WholeArchiveCmdPaths = protoResultWholeArchiveCmdPaths(protoResult)

		if protoResult.GlobalRef != nil && protoResult.GlobalPath != nil {
			result.GlobalRef = protoResult.GlobalRef
			result.GlobalPath = protoResult.GlobalPath
		}
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
			deduper.add(p.strID())
			matched = true
		}
	}

	if !matched {
		return peer
	}

	out := make([]VFS, 0, len(peer))

	for _, p := range peer {
		if deduper.has(p.strID()) {
			continue
		}

		out = append(out, p)
	}

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
		if !deduper.add(p.strID()) {
			continue
		}

		out.Refs = append(out.Refs, ownRefs[i])
		out.Paths = append(out.Paths, p)
	}

	for i, p := range peerPaths {
		if !deduper.add(p.strID()) {
			continue
		}

		out.Refs = append(out.Refs, peerRefs[i])
		out.Paths = append(out.Paths, p)
	}

	return out
}

func reorderARMembers(refs []NodeRef, paths []VFS, metas []SrcMeta) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	type member struct {
		ref  NodeRef
		path VFS
		key  uint64
	}

	members := make([]member, len(paths))

	for i := range paths {
		members[i] = member{refs[i], paths[i], metas[i].sortKey()}
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
