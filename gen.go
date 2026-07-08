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
	"ALICE_TYPED_CALLBACK":            {},
	"SOURCE_GROUP":                    {},
	"TS_PROTO_OPT":                    {},
	"GO_PACKAGE_NAME":                 {},
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
	CFlagsGlobal                    []ANY
	CXXFlagsGlobal                  []ANY
	COnlyFlagsGlobal                []ANY
	ObjAddLibsGlobal                []ANY
	LDFlagsGlobal                   []ANY
	RPathFlagsGlobal                []ANY
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
	persisted                       bool
	DescClosure                     []DescProtoPeer
	ResourceGlobalClosure           []ResourceDecl
	GoSrcClosure                    []VFS
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
	fs               FS
	parsers          *IncludeParserManager
	emit             *StreamingEmitter
	onWarn           func(Warn)
	na               *NodeArenas
	inclArgValues    DenseMap[VFS, STR]
	prClosureScratch []VFS
	resHashScratch   []string
	resHashBuf       []byte
	resB64Scratch    []byte
	inclArgs         InclArgMemo
	memo             *IntValueMap[*ModuleEmitResult]
	refSlices        *SliceCache[NodeRef]
	vfsSlices        *SliceCache[VFS]
	argSlices        *SliceCache[ANY]
	declSlices       *SliceCache[ResourceDecl]
	dirSlices        *SliceCache[IncludeDirective]
	descSlices       *SliceCache[DescProtoPeer]
	tcMemo           map[string]ModuleToolchain
	walking          map[ModuleInstance]bool
	cyclesTolerated  int
	traceStack       []string
	scannerTarget    *IncludeScanner
	scannerHost      *IncludeScanner
	buckets          *BucketCache
	moduleByRef      DenseMap[NodeRef, *ModuleEmitResult]
	tools            DenseMap[ARG, *ModuleEmitResult]
	frames           []*ModuleFrame
	frameDepth       int
	py3ccHeadChunk   []ANY
	scripts          ScriptDeps
	fetchRefs        *DenseMap[STR, NodeRef]
	host             *Platform
	target           *Platform
	vcsRef           NodeRef
	testMode         bool
	sbomEnabled      bool
	autoincludeIdx   *AutoincludeIndex
	tarjan           TarjanCtx
	parsedFiles      map[string]*MakeFile
	prodOuts         IdValueMap
	py3NoStripDebug  bool
	goEnvMemo        map[[2]STR]EnvVars
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

func (e *EmitContext) resolveCodegenDepRefsChunks(chunks InputChunks, incl []NodeRef) []NodeRef {
	deduper.reset()

	na := e.ctx.na
	total := len(incl)

	for _, ch := range chunks {
		total += len(ch)
	}

	out := na.noderefs.alloc(total)
	k := 0

	for _, r := range incl {
		deduper.add(r.strID())
		out[k] = r
		k++
	}

	reg := e.codegen

	for _, ch := range chunks {
		for _, p := range ch {
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

	if ownershipOn {
		for _, deps := range scriptTbl {
			registerOwnedSlice(deps)
		}
	}

	fetchRefs := emitter.fetchRefs
	parsers := newIncludeParserManagerFS(fs, newSharedParseCache())
	targetReg := newCodegenRegistry()
	hostReg := newCodegenRegistry()

	ctx := &GenCtx{
		fs:      fs,
		parsers: parsers,
		emit:    plainEmit,
		onWarn:  onWarn,
		na:      plainEmit.nodeArenas(),
		memo:    newIntValueMap[*ModuleEmitResult](4096),

		refSlices:  newSliceCache[NodeRef](1 << 12),
		vfsSlices:  newSliceCache[VFS](1 << 12),
		argSlices:  newSliceCache[ANY](1 << 8),
		declSlices: newSliceCache[ResourceDecl](1 << 3),
		dirSlices:  newSliceCache[IncludeDirective](1 << 6),
		descSlices: newSliceCache[DescProtoPeer](1 << 3),
		tcMemo:     map[string]ModuleToolchain{},

		walking:   make(map[ModuleInstance]bool),
		host:      hostP,
		target:    targetP,
		fetchRefs: fetchRefs,
		scripts:   scriptTbl,
		testMode:  testMode,

		sbomEnabled: fs.isFile(srcRootRel, sbomConfRel),

		autoincludeIdx: loadAutoincludeIndex(fs),
		parsedFiles:    map[string]*MakeFile{},

		py3NoStripDebug: confPy3NoStripOnDebug(fs),
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

	if root.GlobalRef != nil {
		ctx.emit.result(*root.GlobalRef)
	}

	for _, dir := range discoverRecursedFinalTargets(ctx, filepath.Clean(targetDir)) {
		sub := genModule(ctx, ModuleInstance{
			Path:     source(dir),
			Kind:     KindBin,
			Language: LangCPP,
			Platform: targetP,
		})

		if sub.LDRef != 0 {
			ctx.emit.result(sub.LDRef)
		}

		if sub.GlobalRef != nil {
			ctx.emit.result(*sub.GlobalRef)
		}
	}

	if ctx.testMode && root.testSuiteInfo != nil {
		for _, ref := range emitTestRunNodes(plainEmit, plainEmit, targetP, *root.testSuiteInfo, root.LDRef, root.ResourceGlobalClosure) {
			ctx.emit.result(ref)
		}
	}

	return root.LDRef
}

func confPy3NoStripOnDebug(fs FS) bool {
	if !fs.isFile(srcRootRel, "build/conf/python.conf") {
		return false
	}

	return strings.Contains(string(fs.read("build/conf/python.conf")), "NO_STRIP=yes")
}

func isFinalTargetModuleType(name TOK) bool {
	return isProgramModuleType(name) || name == tokDllTool
}

func discoverRecursedFinalTargets(ctx *GenCtx, targetDir string) []string {
	seen := map[string]bool{targetDir: true}

	var finals []string

	var walk func(dir string, root bool)

	walk = func(dir string, root bool) {
		if !root {
			if seen[dir] {
				return
			}

			seen[dir] = true

			if !ctx.fs.isFile(internStr(dir), "ya.make") {
				return
			}
		}

		var moduleName TOK

		for _, st := range moduleStmts(ctx, dir) {
			switch v := st.(type) {
			case *ModuleStmt:
				moduleName = v.Name
			case *UnknownStmt:
				if v.Name != tokRecurse && v.Name != tokRecurseRootRelative {
					continue
				}

				for _, a := range v.Args {
					child := a.string()

					if v.Name == tokRecurse {
						child = dir + "/" + child
					}

					walk(filepath.Clean(child), false)
				}
			}
		}

		if !root && moduleName != 0 && isFinalTargetModuleType(moduleName) {
			finals = append(finals, dir)
		}
	}

	walk(targetDir, true)

	return finals
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
		return strings.ReplaceAll(path.Clean(instance.Path.relString()), "/", "-")
	}

	if moduleStmt != nil && len(moduleStmt.Args) > 0 {
		return moduleStmt.Args[0].string()
	}

	return baseName(instance.Path.relString())
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

	if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(dir); ok && ctx.fs.isFile(srcRootRel, inc.relString()) {
		incStmts := ctx.parseFileCached(inc.relString())

		return concat(stmts, incStmts)
	}

	return stmts
}

func applyImplicitPeerdirs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	for _, src := range d.srcs {
		if extIsGztproto(src.string()) {
			d.peerdirs = append(d.peerdirs, internStr("kernel/gazetteer/proto").any())

			break
		}
	}

	if instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary {
		hasProtoSrc := false

		for _, src := range d.srcs {
			if extIsProto(src.string()) {
				hasProtoSrc = true

				break
			}
		}

		if hasProtoSrc && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/protobuf/builtin_proto") &&
			!strings.HasPrefix(instance.Path.relString(), "contrib/python/protobuf") {
			d.peerdirs = append(d.peerdirs, strContribPythonProtobuf.any())
		}

		if hasProtoSrc && d.grpc {
			d.peerdirs = append(d.peerdirs, strContribPythonGrpcio.any())
		}
	}

	if !d.hadAllocator && (d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) {
		d.hadAllocator = true
		d.allocatorName = strJ.any()
	}

	py3ProtoVariant := d.moduleStmt.Name == tokProtoLibrary && d.usePython3

	if pyLibraryAutoPythonPeer(d.moduleStmt.Name) && !d.noPythonIncl && instance.Path.relString() != "contrib/libs/python" {
		d.peerdirs = append([]ANY{strContribLibsPython.any()}, d.peerdirs...)
	} else if py3ProtoVariant && !d.noPythonIncl && instance.Path.relString() != "contrib/libs/python" {
		if moduleExcludesTag(d, "CPP_PROTO") {
			d.peerdirs = append([]ANY{strContribLibsPython.any()}, d.peerdirs...)
		} else {
			d.peerdirs = append(d.peerdirs, strContribLibsPython.any())
		}
	}

	if d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin {
		var earlyPeers []string

		if d.pythonSQLite3 {
			earlyPeers = append(earlyPeers, "contrib/tools/python3/Modules/_sqlite")
		}

		earlyPeers = append(earlyPeers, "library/python/runtime_py3/main")

		if !d.noImportTracing && instance.Path.relString() != "library/python/import_tracing/constructor" {
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
				if instance.Path.relString() != peer {
					filteredEarly = append(filteredEarly, peer)
				}
			}

			spliced := make([]ANY, 0, len(d.peerdirs)+len(filteredEarly))

			spliced = append(spliced, d.peerdirs[:insertAt]...)
			spliced = append(spliced, internAnys(filteredEarly)...)
			spliced = append(spliced, d.peerdirs[insertAt:]...)

			d.peerdirs = spliced
		} else {
			for _, peer := range earlyPeers {
				if instance.Path.relString() != peer {
					d.peerdirs = append(d.peerdirs, internStr(peer).any())
				}
			}
		}

		for _, peer := range latePeers {
			if instance.Path.relString() != peer {
				d.peerdirs = append(d.peerdirs, internStr(peer).any())
			}
		}
	}

	if isProgramModuleType(d.moduleStmt.Name) && pyLibraryAutoPythonPeer(d.moduleStmt.Name) && d.moduleStmt.Name != tokPy3Program && d.moduleStmt.Name != tokPy3ProgramBin && !d.noImportTracing && instance.Path.relString() != "library/python/import_tracing/constructor" {
		d.peerdirs = append(d.peerdirs, strLibraryPythonImportTracingConstructor.any())
	}

	if d.hasFbs && instance.Path.relString() != "contrib/libs/flatbuffers" {
		d.peerdirs = append(d.peerdirs, strContribLibsFlatbuffers.any())
	}

	if d.hasFbs64 && instance.Path.relString() != "contrib/libs/flatbuffers64" {
		d.peerdirs = append(d.peerdirs, strContribLibsFlatbuffers64.any())
	}

	if d.hasBisonY && instance.Path.relString() != strBuildInducedByBison.string() {
		d.peerdirs = append(d.peerdirs, strBuildInducedByBison.any())
	}

	if ctx.sbomEnabled && !d.flags.NoRuntime && !effectiveNoPlatform(d.flags) && !isGoModuleType(d.moduleStmt.Name) && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/cxxsupp") {
		d.peerdirs = append(d.peerdirs, strContribLibsCxxsupp.any())
	}
}

type resolvedPeer struct {
	path   string
	result *ModuleEmitResult
	kind   int
}

func genModule(ctx *GenCtx, instance ModuleInstance) *ModuleEmitResult {
	r := genModuleImpl(ctx, instance)

	persistResult(ctx, r)

	return r
}

func genModuleImpl(ctx *GenCtx, instance ModuleInstance) *ModuleEmitResult {
	if existing := ctx.memo.get(ctx.instanceKey(instance)); existing != nil {
		return *existing
	}

	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path.relString())

		return &ModuleEmitResult{}
	}

	ctx.walking[instance] = true

	defer delete(ctx.walking, instance)

	stmts := moduleStmts(ctx, instance.Path.relString())
	env := buildIfEnv(instance)
	frame := ctx.pushFrame()

	defer ctx.popFrame()

	d := collectModuleInto(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn, &frame.d)
	e := newEmitContextIn(frame, ctx, instance, d, nil)

	if instance.Language == LangPy && d.moduleStmt != nil && d.moduleStmt.Name != tokProtoLibrary {
		cpp := instance

		cpp.Language = LangCPP

		result := genModule(ctx, cpp)

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	for _, stmt := range d.allPySrcs {
		applyAllPySrcs(ctx.fs, instance.Path.relString(), stmt, d)
	}

	if d.conflictMod != nil {
		throwFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.relString(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.relString())
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
			d = collectModuleInto(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn, &frame.d)
			e = newEmitContextIn(frame, ctx, instance, d, nil)
		}

		return e.genResourcesLibrary()
	}

	if d.moduleStmt.Name == tokPrebuiltProgram {
		env.setString(envMODULE_SUFFIX, prebuiltModuleSuffix(instance.Platform))

		if e.bindResourceGlobalVars(env) {
			d = collectModuleInto(ctx.parsers, &deduper, instance, stmts, env, ctx.onWarn, &frame.d)
			e = newEmitContextIn(frame, ctx, instance, d, nil)
		}

		return e.genPrebuiltProgram()
	}

	if d.moduleStmt.Name != tokLibrary && d.moduleStmt.Name != tokFbsLibrary && d.moduleStmt.Name != tokDllTool && !isProgramModuleType(d.moduleStmt.Name) && !isPyLibraryType(d.moduleStmt.Name) && !isYqlUdfStaticModule(d.moduleStmt.Name) && !isSpecializedLibraryType(d.moduleStmt.Name) && !isResourceContainerType(d.moduleStmt.Name) && d.moduleStmt.Name != tokGoLibrary && d.moduleStmt.Name != tokGoProgram {
		throwFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path.relString(), d.moduleStmt.Name)
	}

	applyImplicitPeerdirs(ctx, instance, d)

	if isGoModuleType(d.moduleStmt.Name) {
		applyGoImplicitPeerdirs(ctx, instance, d)
	}

	if pyModuleTypeUsesPython3(d.moduleStmt.Name) && d.moduleStmt.Name != tokProtoLibrary {
		hasPyProto := false

		for _, g := range d.pySrcGroups {
			for _, s := range g.Srcs {
				hasPyProto = hasPyProto || extIsProto(s.string())
			}
		}

		for _, s := range d.pySrcs {
			hasPyProto = hasPyProto || extIsProto(s.string())
		}

		if hasPyProto {
			d.peerdirs = append(d.peerdirs, strContribPythonProtobuf.any())
		}
	}

	if d.moduleStmt.Name == tokDynamicLibrary {
		result := e.emitDynamicLibrary()

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
	}

	languageDefaults := e.defaultPeerdirsForModule()

	languageDefaults = suppressMallocAPIDefault(languageDefaults, d.allocatorName)

	isProgram := isProgramModuleType(d.moduleStmt.Name) && !isRuntimeAncestor(instance.Path.relString())
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

	const googleapisPeer = "contrib/libs/googleapis-common-protos"

	instance.Path.rel()
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
		peerSeen(instance.Path.relString())

		cppSelf := instance

		cppSelf.Language = LangCPP
		preResolved = append(preResolved, resolvedPeer{path: instance.Path.relString(), result: genModule(ctx, cppSelf), kind: peerKindLangDefault})
	}

	if d.moduleStmt.Name == tokProtoLibrary && d.useCommonGoogleAPIs && instance.Language == LangCPP {
		if !peerSeen(googleapisPeer) {
			preResolved = append(preResolved, resolvedPeer{path: googleapisPeer, result: genModule(ctx, e.derivePeerInstance(googleapisPeer)), kind: peerKindLangDefault})
		}
	}

	allPeers := frame.allPeers[:0]
	peerKinds := frame.peerKinds[:0]

	defer func() {
		frame.allPeers = allPeers[:0]
		frame.peerKinds = peerKinds[:0]
	}()

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

	frontSet := make(map[ANY]struct{}, len(d.protoCmdPeers))

	for _, p := range d.protoCmdPeers {
		frontSet[p] = struct{}{}
	}

	appendUserPeer := func(p ANY) {
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

	peerObjAddLibsGlobal := frame.peerObjAddLibsGlobal[:0]
	peerLDFlagsGlobal := frame.peerLDFlagsGlobal[:0]
	peerRPathFlagsGlobal := frame.peerRPathFlagsGlobal[:0]

	defer func() {
		frame.peerObjAddLibsGlobal = peerObjAddLibsGlobal[:0]
		frame.peerLDFlagsGlobal = peerLDFlagsGlobal[:0]
		frame.peerRPathFlagsGlobal = peerRPathFlagsGlobal[:0]
	}()

	peerAddInclGlobal := frame.peerAddInclGlobal[:0]

	defer func() { frame.peerAddInclGlobal = peerAddInclGlobal[:0] }()

	var oneLevelOnlyPaths map[VFS]struct{}

	peerCFlagsGlobal := frame.peerCFlagsGlobal[:0]
	peerCXXFlagsGlobal := frame.peerCXXFlagsGlobal[:0]
	peerCOnlyFlagsGlobal := frame.peerCOnlyFlagsGlobal[:0]

	defer func() {
		frame.peerCFlagsGlobal = peerCFlagsGlobal[:0]
		frame.peerCXXFlagsGlobal = peerCXXFlagsGlobal[:0]
		frame.peerCOnlyFlagsGlobal = peerCOnlyFlagsGlobal[:0]
	}()

	allPeers, peerKinds = applyDeferredPeerOrder(d.moduleStmt.Name, allPeers, peerKinds, allocatorExplicitPeers)

	resolved := append(frame.resolved[:0], preResolved...)

	defer func() { frame.resolved = resolved[:0] }()

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
			throwFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path.relString(), peerPath)
		}

		resolved = append(resolved, resolvedPeer{path: peerPath, result: peerResult, kind: kind})
	}

	resGlobalsSum := 0
	archiveCap := 0
	sbomCap := 0
	globalCap := 0
	waCap := 0
	waCmdCap := 0
	dynCap := 0
	ldplugCap := 0
	linkCmdCap := 0

	for _, rp := range resolved {
		pr := rp.result

		resGlobalsSum += len(pr.ResourceGlobalClosure)
		archiveCap += len(pr.PeerArchiveClosurePaths) + 1
		sbomCap += len(pr.PeerSbomClosurePaths) + 1
		globalCap += len(pr.PeerGlobalClosurePaths) + 1
		waCap += len(pr.PeerWholeArchiveClosurePaths) + len(pr.WholeArchivePaths)
		waCmdCap += len(pr.PeerWholeArchiveCmdClosurePaths) + len(pr.WholeArchiveCmdPaths)
		dynCap += len(pr.PeerDynamicClosurePaths) + 1
		ldplugCap += len(pr.LDPluginPaths)
		linkCmdCap += len(pr.PeerArchiveClosurePaths) + len(pr.PeerDynamicClosurePaths) + 2
	}

	peerLinkCmdPaths := make([]VFS, 0, linkCmdCap)

	deduper.reset()

	declBlock := ctx.declSlices.alloc(resGlobalsSum)
	k := 0

	for _, rp := range resolved {
		for _, decl := range rp.result.ResourceGlobalClosure {
			if deduper.add(decl.GlobalVar.strID()) {
				declBlock[k] = decl
				k++
			}
		}
	}

	resourceGlobalsClosure := ctx.declSlices.intern(declBlock[:k])

	d.tc = resolveModuleToolchain(ctx, resourceGlobalsClosure, instance.Platform.ClangVer)

	deduper.reset()

	ldplugBlockR := ctx.refSlices.alloc(ldplugCap)
	ldplugBlockP := ctx.vfsSlices.alloc(ldplugCap)

	k = 0

	for _, rp := range resolved {
		for i, p := range rp.result.LDPluginPaths {
			if deduper.add(p.strID()) {
				ldplugBlockR[k] = rp.result.LDPluginRefs[i]
				ldplugBlockP[k] = p
				k++
			}
		}
	}

	peerLDPluginRefs := ctx.refSlices.intern(ldplugBlockR[:k])
	peerLDPluginPaths := ctx.vfsSlices.intern(ldplugBlockP[:k])

	deduper.reset()

	archiveBlockR := ctx.refSlices.alloc(archiveCap)
	archiveBlockP := ctx.vfsSlices.alloc(archiveCap)

	k = 0

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerArchiveClosurePaths {
			if deduper.add(p.strID()) {
				archiveBlockR[k] = pr.PeerArchiveClosureRefs[i]
				archiveBlockP[k] = p
				k++
			}
		}

		if pr.ARPath != nil && deduper.add(pr.ARPath.strID()) {
			archiveBlockR[k] = pr.ARRef
			archiveBlockP[k] = *pr.ARPath
			k++
		}
	}

	peerArchiveRefs := ctx.refSlices.intern(archiveBlockR[:k])
	peerArchivePaths := ctx.vfsSlices.intern(archiveBlockP[:k])

	deduper.reset()

	globalBlockR := ctx.refSlices.alloc(globalCap)
	globalBlockP := ctx.vfsSlices.alloc(globalCap)

	k = 0

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerGlobalClosurePaths {
			if deduper.add(p.strID()) {
				globalBlockR[k] = pr.PeerGlobalClosureRefs[i]
				globalBlockP[k] = p
				k++
			}
		}

		if pr.GlobalRef != nil && pr.GlobalPath != nil && deduper.add(pr.GlobalPath.strID()) {
			globalBlockR[k] = *pr.GlobalRef
			globalBlockP[k] = *pr.GlobalPath
			k++
		}
	}

	peerGlobalRefs := ctx.refSlices.intern(globalBlockR[:k])
	peerGlobalPaths := ctx.vfsSlices.intern(globalBlockP[:k])
	linkTarget := isProgramModuleType(d.moduleStmt.Name) || d.moduleStmt.Name == tokDllTool
	sbomBlockR := ctx.refSlices.alloc(sbomCap)
	sbomBlockP := ctx.vfsSlices.alloc(sbomCap)
	peerSbomRefsRaw, peerSbomPathsRaw, ownSbomInsertIdx := aggregateSbomComponents(d.moduleStmt.Name, linkTarget, resolved, allocatorExplicitPeers, sbomBlockR[:0], sbomBlockP[:0])
	peerSbomRefs := ctx.refSlices.intern(peerSbomRefsRaw)
	peerSbomPaths := ctx.vfsSlices.intern(peerSbomPathsRaw)

	deduper.reset()

	waBlockR := ctx.refSlices.alloc(waCap)
	waBlockP := ctx.vfsSlices.alloc(waCap)

	k = 0

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerWholeArchiveClosurePaths {
			if deduper.add(p.strID()) {
				waBlockR[k] = pr.PeerWholeArchiveClosureRefs[i]
				waBlockP[k] = p
				k++
			}
		}

		for i, p := range pr.WholeArchivePaths {
			if deduper.add(p.strID()) {
				waBlockR[k] = pr.WholeArchiveRefs[i]
				waBlockP[k] = p
				k++
			}
		}
	}

	peerWholeArchiveRefs := ctx.refSlices.intern(waBlockR[:k])
	peerWholeArchivePaths := ctx.vfsSlices.intern(waBlockP[:k])

	deduper.reset()

	waCmdBlock := ctx.vfsSlices.alloc(waCmdCap)

	k = 0

	for _, rp := range resolved {
		pr := rp.result

		for _, p := range pr.PeerWholeArchiveCmdClosurePaths {
			if deduper.add(p.strID()) {
				waCmdBlock[k] = p
				k++
			}
		}

		for _, p := range pr.WholeArchiveCmdPaths {
			if deduper.add(p.strID()) {
				waCmdBlock[k] = p
				k++
			}
		}
	}

	peerWholeArchiveCmdPaths := ctx.vfsSlices.intern(waCmdBlock[:k])

	deduper.reset()

	dynBlockR := ctx.refSlices.alloc(dynCap)
	dynBlockP := ctx.vfsSlices.alloc(dynCap)

	k = 0

	for _, rp := range resolved {
		pr := rp.result

		for i, p := range pr.PeerDynamicClosurePaths {
			if deduper.add(p.strID()) {
				dynBlockR[k] = pr.PeerDynamicClosureRefs[i]
				dynBlockP[k] = p
				k++
			}
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.add(pr.LDPath.strID()) {
			dynBlockR[k] = pr.LDRef
			dynBlockP[k] = *pr.LDPath
			k++
		}
	}

	peerDynamicRefs := ctx.refSlices.intern(dynBlockR[:k])
	peerDynamicPaths := ctx.vfsSlices.intern(dynBlockP[:k])

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

	effectiveAddInclGlobal := dedupShared(ctx.vfsSlices, d.addInclGlobal, peerAddInclForProp)
	ownProtoInclude := frame.ownProtoInclude[:0]

	defer func() { frame.ownProtoInclude = ownProtoInclude[:0] }()

	if d.protoNamespace != nil {
		ownProtoInclude = append(ownProtoInclude, sourceClean(d.protoNamespace.string()))
	}

	ownProtoInclude = append(ownProtoInclude, d.protoAddInclGlobal...)

	peerProtoInclude := frame.peerProtoInclude[:0]

	defer func() { frame.peerProtoInclude = peerProtoInclude[:0] }()

	deduper.reset()

	for _, rp := range resolved {
		for _, p := range rp.result.ProtoInclude {
			if deduper.add(p.strID()) {
				peerProtoInclude = append(peerProtoInclude, p)
			}
		}
	}

	effectiveProtoInclude := dedupShared(ctx.vfsSlices, ownProtoInclude, peerProtoInclude)
	effectiveCFlagsGlobal := dedupShared(ctx.argSlices, peerCFlagsGlobal, d.cFlagsGlobal)
	effectiveCXXFlagsGlobal := concatShared(ctx.argSlices, peerCXXFlagsGlobal, d.cxxFlagsGlobal)
	effectiveCOnlyFlagsGlobal := concatShared(ctx.argSlices, peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)
	effectiveRPathFlagsGlobal := concatShared(ctx.argSlices, peerRPathFlagsGlobal, d.rpathFlagsGlobal)
	ownLDPlugins := emitOwnLDPlugins(ctx, instance, d.ldPlugins, d.tc)
	mergedLDPluginRefs := peerLDPluginRefs
	mergedLDPluginPaths := peerLDPluginPaths

	if ownLDPlugins != nil && len(ownLDPlugins.Paths) > 0 {
		total := len(ownLDPlugins.Paths) + len(peerLDPluginPaths)
		mergeBlockR := ctx.refSlices.alloc(total)
		mergeBlockP := ctx.vfsSlices.alloc(total)

		deduper.reset()

		k = 0

		for i, p := range ownLDPlugins.Paths {
			if deduper.add(p.strID()) {
				mergeBlockR[k] = ownLDPlugins.Refs[i]
				mergeBlockP[k] = p
				k++
			}
		}

		for i, p := range peerLDPluginPaths {
			if deduper.add(p.strID()) {
				mergeBlockR[k] = peerLDPluginRefs[i]
				mergeBlockP[k] = p
				k++
			}
		}

		mergedLDPluginRefs = ctx.refSlices.intern(mergeBlockR[:k])
		mergedLDPluginPaths = ctx.vfsSlices.intern(mergeBlockP[:k])
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
			ObjAddLibsGlobal:                concatShared(ctx.argSlices, peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   concatShared(ctx.argSlices, peerLDFlagsGlobal, d.ldFlags),
			RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
			PeerArchiveClosureRefs:          peerArchiveRefs,
			PeerArchiveClosurePaths:         peerArchivePaths,
			PeerGlobalClosureRefs:           peerGlobalRefs,
			PeerGlobalClosurePaths:          peerGlobalPaths,
			PeerWholeArchiveClosureRefs:     peerWholeArchiveRefs,
			PeerWholeArchiveClosurePaths:    peerWholeArchivePaths,
			PeerWholeArchiveCmdClosurePaths: peerWholeArchiveCmdPaths,
			LDPluginRefs:                    mergedLDPluginRefs,
			LDPluginPaths:                   mergedLDPluginPaths,
			PeerDynamicClosureRefs:          peerDynamicRefs,
			PeerDynamicClosurePaths:         peerDynamicPaths,
			PeerSbomClosureRefs:             peerSbomRefs,
			PeerSbomClosurePaths:            peerSbomPaths,
			InducedDeps:                     d.inducedDeps,
			ModuleStmtName:                  d.moduleStmt.Name,
			CFlagsGlobal:                    effectiveCFlagsGlobal,
		}
	}

	if !effectiveNoPlatform(d.flags) && runtimeAncestorCxxConsumers[instance.Path.relString()] {
		hasNostdinc := false

		for _, a := range peerCXXFlagsGlobal {
			if a == baseUnitCxxNostdinc.any() {
				hasNostdinc = true

				break
			}
		}

		if !hasNostdinc {
			peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, baseUnitCxxNostdinc.any())
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

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path.relString(), peerAddInclGlobal, dedupedAddIncl)

	if d.moduleStmt.Name == tokProtoLibrary {
		selfPeerAddInclGlobal = peerAddInclGlobal
	}

	effectiveSrcDirs := d.srcDirs

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		effectiveSrcDirs = concat(d.srcDirs, []VFS{dirKey(*pd).source()})
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

		ForceConsistentDebug: isGoModuleType(d.moduleStmt.Name),
	}

	d.cc.ScanCfg = newScanContext(ctx.parsers, dedupedAddIncl, selfPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.relString())
	d.cc.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &d.cc)

	frame.peerCtx = PeerContext{
		SelfAddInclGlobal: selfPeerAddInclGlobal,
		PeerAddInclGlobal: peerAddInclGlobal,
		ResourceGlobals:   resourceGlobalsClosure,
		ProtoInclude:      peerProtoInclude,
	}
	e = newEmitContextIn(frame, ctx, instance, d, &frame.peerCtx)

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

	if d.moduleStmt.Name == tokGoProgram {
		goRef, goPath := e.emitGoExe(resolved, peerArchiveRefs, peerArchivePaths, peerSbomRefs, peerSbomPaths, resourceGlobalsClosure)
		result := newResult()

		result.isPROGRAM = true
		result.LDRef = goRef
		result.LDPath = vfsPtr(goPath)
		result.ARRef = goRef
		result.ARPath = vfsPtr(goPath)

		ctx.memo.put(ctx.instanceKey(instance), result)

		return result
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

		var ownRPathFlags []ANY

		if len(peerDynamicPaths) > 0 {
			ownRPathFlags = append([]ANY(nil), peerRPathFlagsGlobal...)
		}

		wantsStrip := (d.moduleStmt.Name == tokPy3ProgramBin || d.moduleStmt.Name == tokPy3Program) && !d.noStrip &&
			!(ctx.py3NoStripDebug && !instance.Platform.BuildRelease)

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
			mergedLDPluginRefs, mergedLDPluginPaths,
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
			d.flags.NoExportDynSymbols,
			d.flags.NoCompilerWarnings,
			d.noOptimize,
			wantsStrip,
			d.useArcadiaLibm,
			d.splitDwarf,
			programModuleTag,
			d.unit.SbomLang,
			len(d.bundles) > 0 && !pyModuleTypeUsesPython3(d.moduleStmt.Name),
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

	arBaseName := arNameFn(instance.Path.relString())
	arInstance := instance

	var arPluginVFS *VFS

	if d.arPlugin != nil {
		v := source(instance.Path.relString(), "/", d.arPlugin.string())

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

	var arPath *VFS

	var goSrcClosure []VFS

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if isGoModuleType(d.moduleStmt.Name) {
		if sbomActive(ctx, instance) && sbomQualifies(d) {
			rel := instance.Path.relString()

			ownSbomRef, ownSbomPath = e.emitSbomComponent(rel[strings.LastIndexByte(rel, '/')+1:])
		}

		goRef, goPath, srcClosure := e.emitGoPackage(resolved, local.refs, local.outs, peerArchiveRefs, peerArchivePaths, peerSbomRefs, peerSbomPaths, ownSbomRef, ownSbomPath, resourceGlobalsClosure)

		arRef = goRef
		arPath = vfsPtr(goPath)
		goSrcClosure = srcClosure
	} else if len(local.refs) > 0 {
		if perModuleCCTag != 0 {
			arRef = emitARNamedTagged(arInstance, arBaseName, perModuleCCTag, local.refs, local.outs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = emitARNamed(arInstance, arBaseName, local.refs, local.outs, nil, arPluginVFS, d.tc, ctx.host, ctx.emit)
		}

		arPath = vfsPtr(build(instance.Path.relString(), "/", arBaseName))
	}

	if sbomActive(ctx, instance) && sbomQualifies(d) && d.unit.Tag != unitTagPy3BinLib && !isGoModuleType(d.moduleStmt.Name) {
		realPrjName := strings.TrimSuffix(archiveNameWithPrefixOrName(instance.Path.relString(), "", archiveName), ".a")

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
	result.GoSrcClosure = goSrcClosure

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path.relString())
		globalTag := d.unit.GlobalARTag

		globalRefs, globalOutputs = reorderARMembers(globalRefs, globalOutputs, globalMetas)

		globalRef := emitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, d.tc, ctx.host, ctx.emit)

		result.GlobalRef = &globalRef
		result.GlobalPath = vfsPtr(build(instance.Path.relString(), "/", globalBaseName))
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
		if p.isBuild() && (p == ownPrefix || strings.HasPrefix(p.relString(), ownPrefix.relString()+"/")) {
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

func (ctx *GenCtx) py3ccHead(py3ccBinary, py3ccSlowBin VFS) []ANY {
	if ctx.py3ccHeadChunk == nil {
		ctx.py3ccHeadChunk = []ANY{py3ccBinary.any(), argSlowPy3cc.any(), py3ccSlowBin.any()}

		if ownershipOn {
			registerOwnedSlice(ctx.py3ccHeadChunk)
		}
	}

	return ctx.py3ccHeadChunk
}

type ModuleFrame struct {
	d                    ModuleData
	emitCtx              EmitContext
	peerCtx              PeerContext
	allPeers             []string
	peerKinds            []int
	resolved             []resolvedPeer
	peerAddInclGlobal    []VFS
	ownProtoInclude      []VFS
	peerProtoInclude     []VFS
	peerObjAddLibsGlobal []ANY
	peerLDFlagsGlobal    []ANY
	peerRPathFlagsGlobal []ANY
	peerCFlagsGlobal     []ANY
	peerCXXFlagsGlobal   []ANY
	peerCOnlyFlagsGlobal []ANY
}

func (ctx *GenCtx) pushFrame() *ModuleFrame {
	if ctx.frameDepth == len(ctx.frames) {
		ctx.frames = append(ctx.frames, &ModuleFrame{})
	}

	f := ctx.frames[ctx.frameDepth]

	ctx.frameDepth++

	return f
}

func (ctx *GenCtx) popFrame() {
	ctx.frameDepth--
}
