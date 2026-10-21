package main

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

var asmlibYasmModules = map[string]bool{
	"contrib/libs/asmlib": true,
}

var acknowledgedMacros = map[string]struct{}{
	"ALICE_TYPED_CALLBACK":            {},
	"SOURCE_GROUP":                    {},
	"TS_PROTO_OPT":                    {},
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
	"NO_PROFILE_RUNTIME":              {},
	"NO_PYTHON_COVERAGE":              {},
	"NO_SANITIZE":                     {},
	"NO_SANITIZE_COVERAGE":            {},
	"NO_JOIN_SRC":                     {},
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
	"DISABLE_DATA_VALIDATION":         {},

	"FROM_SANDBOX":      {},
	"JAVA_PROTO_PLUGIN": {},

	"SPLIT_CODEGEN":             {},
	"DECIMAL_MD5_LOWER_32_BITS": {},
	"STYLE_DETEKT":              {},
	"DEFAULT_JDK_VERSION":       {},
}

var acknowledgedTokSet = func() BitSet {
	var b BitSet

	for name := range acknowledgedMacros {
		b.add(uint32(tokByName[name]))
	}

	return b
}()

const (
	variantDemandShift     = 5
	variantDemandMask      = uint16(0x7) << variantDemandShift
	variantPIC             = uint16(1) << 14
	peerKindLangDefault    = 0
	peerKindProgramDefault = 1
	peerKindUserPeer       = 2
	peerKindUnitTestPeer   = 3
	KindForeignDescent     = KindJavaTestable + 1
)

type ModuleEmitResult struct {
	ModuleVariants                  uint16
	GlobalSources                   []VFS
	PchFlagsGlobal                  []ANY
	JavaClosure                     []VFS
	DocsClosure                     []VFS
	ARRef                           NodeRef
	ARPath                          *VFS
	isPROGRAM                       bool
	LDRef                           NodeRef
	LDPath                          *VFS
	ArchiveFile                     VFS
	GlobalRef                       *NodeRef
	GlobalPath                      *VFS
	WholeArchiveRefs                []NodeRef
	WholeArchivePaths               []VFS
	WholeArchiveCmdPaths            []VFS
	AddInclGlobal                   []VFS
	OwnAddInclGlobal                []VFS
	ProtoInclude                    []VFS
	RosInclude                      []VFS
	FbsNamespaces                   []FbsNamespaceVar
	MlaunchDeps                     []string
	SwigInclude                     []VFS
	AddInclOneLevel                 []VFS
	AddInclUserGlobal               []VFS
	CFlagsGlobal                    []ANY
	CXXFlagsGlobal                  []ANY
	COnlyFlagsGlobal                []ANY
	LinkExcludeLibraries            []ANY
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
	PeerDynamicClosureRefs          []NodeRef
	PeerDynamicClosurePaths         []VFS
	SbomComponentRef                *NodeRef
	SbomComponentPath               *VFS
	PeerSbomClosureRefs             []NodeRef
	PeerSbomClosurePaths            []VFS
	InducedDeps                     ParsedIncludeSet
	ModuleStmtName                  TOK
	persisted                       bool
	DescClosure                     []DescProtoPeer
	ResourceGlobalClosure           []ResourceDecl
	java                            *JavaModule
	TestSuites                      []*TestSuite
	testsEmitted                    bool
}

func protoResultWholeArchiveCmdPaths(res *ProtoSrcsResult) []VFS {
	return slices.Clone(res.WholeArchiveCmdPaths)
}

type GenCtx struct {
	fs               FS
	parsers          *IncludeParserManager
	emit             *StreamingEmitter
	onWarn           func(Warn)
	keepGoing        bool
	na               *NodeArenas
	inclArgValues    DenseMap[VFS, STR]
	resHashScratch   []string
	resHashBuf       []byte
	resDashBuf       []byte
	resB64Scratch    []byte
	inclArgs         InclArgMemo
	memo             *moduleMemo
	refSlices        *SliceCache[NodeRef]
	vfsSlices        *SliceCache[VFS]
	argSlices        *SliceCache[ANY]
	declSlices       *SliceCache[ResourceDecl]
	results          *BumpAllocator[ModuleEmitResult]
	dirSlices        *SliceCache[IncludeDirective]
	descSlices       *SliceCache[DescProtoPeer]
	tcMemo           map[ToolchainKey]ModuleToolchain
	moduleStmts      map[string][]Stmt
	dllForStack      []string
	ifEnvScratch     EnvStore
	moduleDirsEnv    EnvStore
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
	tests            *TestOpts
	sbomEnabled      bool
	autoincludeIdx   *AutoincludeIndex
	parsedFiles      map[string]*MakeFile
	prodOuts         IdValueMap
	goEnvMemo        map[[2]STR]EnvVars
	frameSerial      uint32
	scannerTargetPIC *IncludeScanner
	roots            map[ModuleInstance]bool
	rootDemand       ModuleDemand
}

type moduleMemoEntry struct {
	next    *moduleMemoEntry
	result  *ModuleEmitResult
	variant uint16
}

type moduleMemo struct {
	heads   PageVec[*moduleMemoEntry]
	entries *BumpAllocator[moduleMemoEntry]
}

func newModuleMemo() *moduleMemo {
	return &moduleMemo{entries: newBumpAllocator[moduleMemoEntry]()}
}

func runGenIntoWithResources(fs FS, caches *GenCaches, targets []string, hostP, targetP *Platform, emitter *StreamingEmitter, onWarn func(Warn), tests *TestOpts, keepGoing bool, rootDemand ModuleDemand) {
	ctx := newGenCtx(fs, caches, hostP, targetP, emitter, onWarn, tests, keepGoing)
	ordered := ctx.linkingTargetsFirst(targets)
	discovered := make([]DiscoveredTargets, len(ordered))
	all := make([]DiscoveredModule, 0, len(ordered))

	for i, t := range ordered {
		discovered[i] = discoverRecursedTargets(ctx, filepath.Clean(t))
		all = append(all, discovered[i].Root)
	}

	for _, targets := range discovered {
		all = append(all, targets.Recursed...)
	}

	ctx.roots = make(map[ModuleInstance]bool, len(all))
	ctx.rootDemand = rootDemand

	for _, target := range all {
		ctx.roots[ctx.rootInstanceForDeclaration(target.Path, demandNone, target.Declaration)] = true
	}

	for _, target := range ctx.linkingModulesFirst(all) {
		if target.Declaration == nil || !isJavaModuleType(target.Declaration.Name) {
			genModule(ctx, ctx.rootInstanceForDeclaration(target.Path, rootDemand, target.Declaration))
		}
	}

	for _, targets := range discovered {
		ctx.genTarget(targets, rootDemand)
	}

	for _, target := range all {
		ctx.emitConfiguredSubmoduleTests(target.Path)
	}

	if ctx.buckets.h1Mismatches > 0 {
		onWarn(Warn{
			Kind: WarnBucketHash,
			Message: fmt.Sprintf("%d h1 mismatches, %d buckets in overflow (pair-collision headroom shrinking)",
				ctx.buckets.h1Mismatches, ctx.buckets.overflowed),
		})
	}
}

func newGenCtx(fs FS, caches *GenCaches, hostP, targetP *Platform, emitter *StreamingEmitter, onWarn func(Warn), tests *TestOpts, keepGoing bool) *GenCtx {
	plainEmit := emitter
	scriptTbl := buildScriptTable(fs)

	if ownershipOn {
		for _, deps := range scriptTbl {
			registerOwnedSlice(deps)
		}
	}

	fetchRefs := emitter.fetchRefs
	parsers := caches.parsers
	targetReg := newCodegenRegistry(plainEmit.nodeArenas())
	hostReg := newCodegenRegistry(plainEmit.nodeArenas())

	ctx := &GenCtx{
		fs:        fs,
		parsers:   parsers,
		emit:      plainEmit,
		onWarn:    onWarn,
		keepGoing: keepGoing,
		na:        plainEmit.nodeArenas(),
		memo:      newModuleMemo(),

		refSlices:   newSliceCache[NodeRef](1 << 12),
		vfsSlices:   newSliceCache[VFS](1 << 12),
		argSlices:   newSliceCache[ANY](1 << 8),
		declSlices:  newSliceCache[ResourceDecl](1 << 3),
		results:     newBumpAllocator[ModuleEmitResult](),
		dirSlices:   newSliceCache[IncludeDirective](1 << 6),
		descSlices:  newSliceCache[DescProtoPeer](1 << 3),
		tcMemo:      map[ToolchainKey]ModuleToolchain{},
		moduleStmts: map[string][]Stmt{},

		walking:   make(map[ModuleInstance]bool),
		host:      hostP,
		target:    targetP,
		fetchRefs: fetchRefs,
		scripts:   scriptTbl,
		testMode:  tests != nil,
		tests:     tests,

		sbomEnabled: fs.isFile(srcRootRel, sbomConfRel),

		autoincludeIdx: loadAutoincludeIndex(fs),
		parsedFiles:    caches.parsedFiles,
	}

	ctx.inclArgs = InclArgMemo{m: &ctx.inclArgValues}
	ctx.buckets = newBucketCache()

	targetScanner := newIncludeScannerWith(parsers, loadSysInclSet(parsers, targetP, onWarn), onWarn, ctx.buckets)

	targetScanner.codegen = targetReg
	targetScanner.moduleByRef = &ctx.moduleByRef

	hostScanner := newIncludeScannerWith(parsers, loadSysInclSet(parsers, hostP, onWarn), onWarn, ctx.buckets)

	hostScanner.codegen = hostReg
	hostScanner.moduleByRef = &ctx.moduleByRef

	ctx.scannerTarget = targetScanner
	ctx.scannerHost = hostScanner
	ctx.vcsRef = emitVCSNode(ctx.emit, ctx.host)

	return ctx
}

func (ctx *GenCtx) linkingTargetsFirst(targets []string) []string {
	if len(targets) < 2 {
		return targets
	}

	out := slices.Clone(targets)

	slices.SortStableFunc(out, func(a, b string) int {
		la, lb := isLinkingModuleType(ctx.moduleName(a)), isLinkingModuleType(ctx.moduleName(b))

		switch {
		case la == lb:
			return 0
		case la:
			return -1
		default:
			return 1
		}
	})

	return out
}

func (ctx *GenCtx) moduleName(dir string) TOK {
	return stmtsModuleName(moduleStmts(ctx, filepath.Clean(dir)))
}

func (ctx *GenCtx) rootInstance(dir string, demand ModuleDemand) ModuleInstance {
	decl, certain := inferModuleDeclaration(moduleStmts(ctx, filepath.Clean(dir)))

	if !certain {
		decl, _ = ctx.recurseScan(dir)
	}

	return ctx.rootInstanceForDeclaration(dir, demand, decl)
}

func (ctx *GenCtx) rootInstanceForDeclaration(dir string, demand ModuleDemand, decl *ModuleStmt) ModuleInstance {
	kind := KindLib
	language := LangCPP

	var name TOK

	if decl != nil {
		name = decl.Name

		if decl.Schema {
			language = LangProtoDescriptions
		}
	}

	if name == tokPy3Program || name == tokYqlUdf || name == tokDocs || name == 0 {
		kind = KindBin
	}

	platform := ctx.target

	if name == tokGoDll {
		platform = ctx.target.pic()
	}

	return ModuleInstance{
		Path:     source(filepath.Clean(dir)),
		Kind:     kind,
		Language: language,
		Platform: platform,
		Demand:   demand,
	}
}

func (ctx *GenCtx) genTarget(targets DiscoveredTargets, rootDemand ModuleDemand) {
	target := targets.Root
	targetDir := target.Path

	if target.Declaration != nil && isJavaModuleType(target.Declaration.Name) {
		for _, dir := range ctx.recurseClosure(filepath.Clean(targetDir)) {
			ctx.genStartTarget(dir)
		}

		return
	}

	root := genModule(ctx, ctx.rootInstanceForDeclaration(targetDir, rootDemand, target.Declaration))

	if root.LDPath != nil {
		ctx.scannerForPlatform(ctx.target).codegen.use(*root.LDPath)
	}

	ctx.emit.result(root.LDRef)

	if root.GlobalRef != nil {
		ctx.emit.result(*root.GlobalRef)
	}

	ctx.emitModuleTests(append(ctx.genMultimoduleTargets(target, root.ModuleVariants, rootDemand), root)...)

	for _, target := range targets.Recursed {
		dir := target.Path

		if target.Declaration != nil && isJavaModuleType(target.Declaration.Name) {
			ctx.genJavaTarget(dir, target.Declaration.Name)

			continue
		}

		sub := genModule(ctx, ctx.rootInstanceForDeclaration(dir, rootDemand, target.Declaration))

		if sub.LDPath != nil {
			ctx.scannerForPlatform(ctx.target).codegen.use(*sub.LDPath)
		}

		if sub.LDRef != 0 {
			ctx.emit.result(sub.LDRef)
		}

		ctx.emitModuleTests(append(ctx.genMultimoduleTargets(target, sub.ModuleVariants, rootDemand), sub)...)
	}
}

func (ctx *GenCtx) recurseClosure(root string) []string {
	seen := map[string]bool{}

	var out []string

	var walk func(dir string)

	walk = func(dir string) {
		if seen[dir] || !ctx.fs.isFile(internStr(dir), "ya.make") {
			return
		}

		seen[dir] = true
		out = append(out, dir)

		_, children := ctx.recurseScan(dir)

		for _, child := range children {
			walk(child)
		}
	}

	walk(root)

	return out
}

func (ctx *GenCtx) recurseScan(dir string) (*ModuleStmt, []string) {
	instance := ctx.rootInstanceForDeclaration(dir, demandNone, nil)
	env := buildIfEnvInto(&ctx.ifEnvScratch, instance)
	d := ModuleData{}

	apply := func(stmt Stmt) {
		collectStmts(ctx, dir, instance.Kind, instance.Language, []Stmt{stmt}, env, &d, ctx.onWarn)
	}

	var module *ModuleStmt
	var children []string

	var scan func(stmts []Stmt)

	scan = func(stmts []Stmt) {
		for _, st := range stmts {
			switch v := st.(type) {
			case *ModuleStmt:
				module = v
				instance = ctx.rootInstanceForDeclaration(dir, demandNone, v)
				apply(st)
			case *SetStmt, *DefaultVarStmt:
				apply(st)
			case *IfStmt:
				if evalCond(v.Cond, env) {
					scan(v.Then)
				} else {
					scan(v.Else)
				}
			case *UnknownStmt:
				switch v.Name {
				case tokEnable, tokDisable, tokSetAppend, tokSetListPreSuf,
					tokUsePython2, tokPython2Addincl, tokUsePython3, tokPython3Addincl:
					apply(st)
				}

				if v.Name != tokRecurse && v.Name != tokRecurseRootRelative && (v.Name != tokRecurseForTests || !ctx.testMode) {
					continue
				}

				for _, a := range expandStmtTokens(v.Args, env) {
					child := a.string()

					if v.Name != tokRecurseRootRelative {
						child = dir + "/" + child
					}

					children = append(children, filepath.Clean(child))
				}
			}
		}
	}

	scan(moduleStmts(ctx, dir))

	return module, children
}

func (ctx *GenCtx) genStartTarget(dir string) {
	name := ctx.moduleName(dir)

	switch {
	case name == 0:
	case isJavaModuleType(name):
		ctx.genJavaTarget(dir, name)
	default:
		root := genModule(ctx, ctx.rootInstance(dir, demandSelf))

		if root.LDRef != 0 {
			ctx.emit.result(root.LDRef)
		}
	}
}

func isFinalTargetModuleType(name TOK) bool {
	return isProgramModuleType(name) && name != tokGoTest && name != tokBoosttest && name != tokBoosttestWithMain || name == tokExectest || name == tokDllTool || name == tokDocs || name == tokJavaProgram || name == tokJavaContrib || name == tokProtoDescriptions || name == tokProtoRegistry || name == tokYqlUdf || name == tokYqlPythonUdf || name == tokYqlPython3Udf || name == tokYqlPrebuiltTool
}

func picTransition(name TOK) bool {
	return name == tokDll || name == tokDllJava || name == tokPy2Module || name == tokYqlUdf || name == tokYqlPythonUdf || name == tokYqlPython3Udf || name == tokDynamicLibrary
}

func isDllModuleType(name TOK) bool {
	return name == tokDll || name == tokDllTool || name == tokPy2Module
}

func (e *EmitContext) moduleInputArg(arg *ANY) *ANY {
	if arg == nil || !e.ctx.fs.isFile(e.instance.Path.rel(), arg.string()) {
		return arg
	}

	return ptr(internV(e.instance.Path.relString(), "/", arg.string()).any())
}

func stmtsModuleName(stmts []Stmt) TOK {
	if decl := stmtsModuleDeclaration(stmts); decl != nil {
		return decl.Name
	}

	return 0
}

func stmtsModuleDeclaration(stmts []Stmt) *ModuleStmt {
	decl, _ := inferModuleDeclaration(stmts)

	return decl
}

func inferModuleDeclaration(stmts []Stmt) (*ModuleStmt, bool) {
	var declaration *ModuleStmt

	certain := true

	for _, st := range stmts {
		var candidate *ModuleStmt

		switch v := st.(type) {
		case *ModuleStmt:
			return v, true
		case *UnknownStmt:
			if v.Name == tokDllFor {
				candidate = &ModuleStmt{Name: tokDll, Line: v.Line}
			}
		case *IfStmt:
			thenDecl, thenCertain := inferModuleDeclaration(v.Then)
			elseDecl, elseCertain := inferModuleDeclaration(v.Else)

			if thenDecl != nil && elseDecl != nil && thenDecl.Name != elseDecl.Name {
				return nil, false
			}

			candidate = thenDecl

			if candidate == nil {
				candidate = elseDecl
			}

			certain = certain && thenCertain && elseCertain

			if candidate != nil {
				certain = certain && thenDecl != nil && elseDecl != nil && thenDecl.Schema == elseDecl.Schema
			}
		}

		if candidate != nil {
			if declaration != nil && declaration.Name != candidate.Name {
				return nil, false
			}

			declaration = candidate
		}
	}

	return declaration, certain
}

func isLinkingModuleType(name TOK) bool {
	switch name {
	case tokDllTool, tokDll, tokSoProgram, tokDynamicLibrary, tokPy2Module, tokYqlUdf, tokYqlPythonUdf, tokYqlPython3Udf:
		return true
	}

	return isProgramModuleType(name)
}

func (e *EmitContext) peerDemand() ModuleDemand {
	if e.instance.Demand == demandLinked || (e.instance.Demand == demandSelf && (isLinkingModuleType(e.d.moduleStmt.Name) || isGoModuleType(e.d.moduleStmt.Name))) {
		return demandLinked
	}

	return demandNone
}

func (ctx *GenCtx) emitConfiguredSubmoduleTests(dir string) {
	if !ctx.testMode {
		return
	}

	instance := ctx.rootInstance(dir, demandNone)

	switch ctx.moduleName(dir) {
	case tokPy23Library:
		instance.Language = LangPy2
	case tokPy3Program:
		instance.Kind = KindLib
	default:
		return
	}

	if r := ctx.memoGet(instance); r != nil {
		ctx.emitModuleTests(r)
	}
}

func (ctx *GenCtx) emitModuleTests(results ...*ModuleEmitResult) {
	if !ctx.testMode {
		return
	}

	var suites []*TestSuite

	for _, r := range results {
		if r.testsEmitted {
			continue
		}

		r.testsEmitted = true

		for _, s := range r.TestSuites {
			if merged := mergeCheckSuite(suites, s); !merged {
				suites = append(suites, s)
			}
		}
	}

	for _, s := range suites {
		for _, ref := range ctx.emitTestSuite(s) {
			ctx.emit.result(ref)
		}
	}
}

func mergeCheckSuite(suites []*TestSuite, s *TestSuite) bool {
	if len(s.checkFiles) == 0 {
		return false
	}

	for _, prev := range suites {
		if prev.testType == s.testType && prev.projectPath == s.projectPath && len(prev.checkFiles) > 0 {
			prev.checkFiles = append(prev.checkFiles, s.checkFiles...)
			prev.deps = append(prev.deps, s.deps...)

			slices.SortFunc(prev.checkFiles, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

			prev.checkFiles = slices.Compact(prev.checkFiles)

			return true
		}
	}

	return false
}

type DiscoveredModule struct {
	Path        string
	Declaration *ModuleStmt
}

type DiscoveredTargets struct {
	Root     DiscoveredModule
	Recursed []DiscoveredModule
}

func (ctx *GenCtx) linkingModulesFirst(targets []DiscoveredModule) []DiscoveredModule {
	ordered := slices.Clone(targets)

	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i].Declaration, ordered[j].Declaration

		return a != nil && isLinkingModuleType(a.Name) && (b == nil || !isLinkingModuleType(b.Name))
	})

	return ordered
}

func discoverRecursedTargets(ctx *GenCtx, targetDir string) DiscoveredTargets {
	seen := map[string]bool{targetDir: true}

	var targets DiscoveredTargets

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

		module, children := ctx.recurseScan(dir)

		if root {
			targets.Root = DiscoveredModule{Path: dir, Declaration: module}
		}

		for _, child := range children {
			walk(child, false)
		}

		if !root && module != nil && (ctx.testMode || module.Schema || isFinalTargetModuleType(module.Name)) {
			targets.Recursed = append(targets.Recursed, DiscoveredModule{Path: dir, Declaration: module})
		}
	}

	walk(targetDir, true)

	return targets
}

func genDumpGraphWithResources(fs FS, caches *GenCaches, targets []string, hostP, targetP *Platform, onWarn func(Warn), tests *TestOpts, keepGoing bool, rootDemand ModuleDemand, addModulesToResults bool) *Graph {
	emitter := newStreamingEmitter(nil)

	emitter.addModulesToResults = addModulesToResults

	runGenIntoWithResources(fs, caches, targets, hostP, targetP, emitter, onWarn, tests, keepGoing, rootDemand)

	return finalize(emitter)
}

func programBinaryName(instance ModuleInstance, moduleStmt *ModuleStmt) string {
	if moduleStmt != nil && moduleStmt.Name == tokUnittestFor {
		return fullPathTestName(instance.Path.relString())
	}

	if moduleStmt != nil && len(moduleStmt.Args) > 0 {
		return moduleStmt.Prefix + moduleStmt.Args[0].string()
	}

	if moduleStmt != nil && (isUnitTestModule(moduleStmt.Name) || isPythonTestModule(moduleStmt.Name)) {
		return moduleStmt.Prefix + fullPathTestName(instance.Path.relString())
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

	mf := throw2(parseFileClean(ctx.fs, rel))

	ctx.parsedFiles[rel] = mf

	return mf.Stmts
}

func moduleStmts(ctx *GenCtx, dir string) []Stmt {
	if cached, ok := ctx.moduleStmts[dir]; ok {
		return cached
	}

	stmts := expandGoTestFor(ctx, ctx.parseFileCached(internJoined(dir, "ya.make").string()))

	if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(dir); ok && ctx.fs.isFile(srcRootRel, inc.relString()) {
		incStmts := ctx.parseFileCached(inc.relString())

		stmts = astStmts.list(concat(stmts, incStmts)...)
	}

	ctx.moduleStmts[dir] = stmts

	return stmts
}

func expandGoTestFor(ctx *GenCtx, stmts []Stmt) []Stmt {
	at := slices.IndexFunc(stmts, func(st Stmt) bool {
		u, ok := st.(*UnknownStmt)

		return ok && u.Name == tokGoTestFor && len(u.Args) > 0
	})

	if at < 0 {
		return stmts
	}

	stmt := stmts[at].(*UnknownStmt)
	forDir := path.Clean(stmt.Args[0].string())
	out := make([]Stmt, 0, len(stmts)+8)

	out = append(out, stmts[:at]...)

	out = append(out,
		&SetStmt{Name: "GO_TEST_IMPORT_PATH", NameEnv: envGO_TEST_IMPORT_PATH, Value: forDir, Line: stmt.Line},
		&SetStmt{Name: "_GO_IMPORT_PATH", NameEnv: env_GO_IMPORT_PATH, Value: forDir, Line: stmt.Line},
		&SrcDirStmt{Dirs: []ANY{internStr(forDir).any()}, Line: stmt.Line},
	)

	out = appendGoTestForModule(out, ctx.parseFileCached(internJoined(forDir, "ya.make").string()))

	return append(out, stmts[at+1:]...)
}

func appendGoTestForModule(out, stmts []Stmt) []Stmt {
	for _, st := range stmts {
		switch v := st.(type) {
		case *ModuleStmt:
			module := *v

			module.Name = tokGoTest
			out = append(out, &module)
		case *UnknownStmt:
			if v.Name != tokRecurse && v.Name != tokRecurseForTests && v.Name != tokRecurseRootRelative {
				out = append(out, v)
			}
		case *IfStmt:
			branch := *v

			branch.Then = appendGoTestForModule(nil, v.Then)
			branch.Else = appendGoTestForModule(nil, v.Else)

			out = append(out, &branch)
		default:
			out = append(out, st)
		}
	}

	return out
}

func applyImplicitPeerdirs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	needProto := instance.Language == LangPy && d.moduleStmt.Name == tokProtoLibrary
	hasGztproto := false
	hasProto := false

	for _, meta := range d.srcs {
		src := meta.Source.string()

		if !hasGztproto && extIsGztproto(src) {
			d.peerdirs = append(d.peerdirs, strKernelGazetteerProto.any())
			hasGztproto = true
		}

		if needProto && !hasProto && extIsProto(src) {
			hasProto = true
		}

		if hasGztproto && (!needProto || hasProto) {
			break
		}
	}

	if needProto {
		modulePath := instance.Path.relString()

		if hasProto && !strings.HasPrefix(modulePath, "contrib/libs/protobuf/builtin_proto") &&
			!strings.HasPrefix(modulePath, "contrib/python/protobuf") {
			d.peerdirs = append(d.peerdirs, strContribPythonProtobuf.any())
		}

		if hasProto && d.grpc {
			d.peerdirs = append(d.peerdirs, strContribPythonGrpcio.any())
		}
	}

	if !d.hadAllocator && isPyProgramType(d.moduleStmt.Name) {
		d.hadAllocator = true
		d.allocatorName = strJ.any()
	}

	if !d.hadAllocator && isDllModuleType(d.moduleStmt.Name) {
		d.hadAllocator = true
		d.allocatorName = strFAKE.any()
	}

	if d.moduleStmt.Name == tokPy2Module && instance.Platform.ifEnv.bool(envIS_CROSS_TOOLS) {
		d.peerdirs = append(d.peerdirs, strContribToolsPythonLib.any())
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

	if isPyProgramType(d.moduleStmt.Name) {
		var earlyPeers []string

		if isPythonTestModule(d.moduleStmt.Name) {
			earlyPeers = append(earlyPeers, "library/python/pytest")
		}

		if d.usePython2 {
			earlyPeers = append(earlyPeers, "library/python/runtime/main", "contrib/deprecated/python/subprocess32")

			if d.pythonSQLite3 {
				earlyPeers = append(earlyPeers, "contrib/tools/python/src/Modules/_sqlite")
			}
		} else {
			if d.pythonSQLite3 {
				earlyPeers = append(earlyPeers, "contrib/tools/python3/Modules/_sqlite")
			}

			earlyPeers = append(earlyPeers, "library/python/runtime_py3/main")
		}

		if !d.noImportTracing && instance.Path.relString() != "library/python/import_tracing/constructor" {
			earlyPeers = append(earlyPeers, "library/python/import_tracing/constructor")
		}

		if (d.moduleStmt.Name == tokPy2Program || d.moduleStmt.Name == tokPy2Test) && !instance.Platform.ifEnv.bool(envFAIL_PY2) && !instance.Platform.ifEnv.bool(envOPENSOURCE) {
			earlyPeers = append(earlyPeers, "build/rules/py2_deprecation")
		}

		var latePeers []string

		if !d.noCheckImportsDisabled {
			latePeers = append(latePeers, "library/python/testing/import_test")
		}

		if d.moduleStmt.Name != tokPy3Program {
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

	if d.hasFbs && !isGoModuleType(d.moduleStmt.Name) && instance.Path.relString() != "contrib/libs/flatbuffers" {
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

type ResolvedPeer struct {
	path   string
	result *ModuleEmitResult
	kind   int
}

func newModuleScanContext(ctx *GenCtx, instance ModuleInstance, d *ModuleData, ccOwn, ccPeer, fullPeer, protoInclude []VFS) *ScanContext {
	scanCtx := newScanContext(ctx.na, ctx.parsers, ccOwn, ccPeer, fullPeer, includeScannerBasePaths())

	scanCtx.asmAddIncl = ctx.na.vfsList(d.asmAddIncl...)
	scanCtx.cythonAddIncl = ctx.na.vfsList(d.cythonAddIncl...)
	scanCtx.protoInclude = ctx.na.vfsList(protoInclude...)
	scanCtx.swigInclude = ctx.na.vfsList(d.cc.SwigInclude...)
	scanCtx.cythonSys = cythonSysIncludes(d)
	scanCtx.goAsmIncl = instance.Platform.Go.AsmIncludeDirs
	scanCtx.joinFrom = instance.Platform.ISA
	scanCtx.joinTo = ctx.target.ISA

	if root := protoCPPOutRoot(d); root != "" {
		scanCtx.protoOutRoot = source(root)
	}

	return scanCtx
}

func genModule(ctx *GenCtx, instance ModuleInstance) *ModuleEmitResult {
	if !ctx.keepGoing {
		r := genModuleImpl(ctx, instance)

		persistResult(ctx, r)

		return r
	}

	var r *ModuleEmitResult

	exc := try(func() {
		r = genModuleImpl(ctx, instance)

		persistResult(ctx, r)
	})

	if exc != nil {
		ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: %s", instance.Path.relString(), exc.Error())})

		if ownershipOn {
			ctx.na.resetWindows()
		}

		r = &ModuleEmitResult{}

		ctx.memoPut(instance, r)
	}

	return r
}

func genModuleImpl(ctx *GenCtx, instance ModuleInstance) (result *ModuleEmitResult) {
	if existing := ctx.memoGet(instance); existing != nil {
		return existing
	}

	if ctx.walking[instance] {
		ctx.cyclesTolerated++
		fmt.Fprintf(os.Stderr, "gen: PEERDIR cycle tolerated at %s\n", instance.Path.relString())

		return &ModuleEmitResult{}
	}

	ctx.walking[instance] = true

	defer delete(ctx.walking, instance)

	stmts := moduleStmts(ctx, instance.Path.relString())
	moduleName := stmtsModuleName(stmts)

	if moduleName == tokYqlUdf && instance.Kind == KindBin && !instance.Platform.PIC {
		library := instance

		library.Kind = KindLib

		for _, current := range []bool{false, true} {
			library.YqlCurrent = current
			genModule(ctx, library)
		}
	}

	if !instance.Platform.PIC && instance.Language == LangCPP && instance.Kind != KindForeignDescent && picTransition(moduleName) && (moduleName != tokYqlUdf || instance.Kind != KindLib) {
		if moduleName != tokDynamicLibrary {
			descent := instance

			descent.Kind = KindForeignDescent
			descent.Demand = demandNone

			genModule(ctx, descent)
		}

		pic := instance

		pic.Platform = instance.Platform.pic()

		r := genModule(ctx, pic)

		ctx.memoPut(instance, r)

		return r
	}

	frame := ctx.pushFrame()
	env := buildIfEnvInto(&frame.ifEnv, instance)

	defer ctx.popFrame()

	if instance.Language == LangJava {
		return genJavaModule(ctx, instance, ctx.moduleName(instance.Path.relString()), stmts, env)
	}

	d := collectModuleInto(ctx, instance, stmts, env, ctx.onWarn, &frame.d)
	e := newEmitContextIn(frame, ctx, instance, d, nil)

	if d.discarded {
		result := &ModuleEmitResult{}

		ctx.memoPut(instance, result)

		return result
	}

	if tag := multimoduleVariantTag(d.multimodule, instance.Language); tag != "" && moduleExcludesTag(d, tag) {
		result := &ModuleEmitResult{ModuleVariants: d.moduleVariants}

		ctx.memoPut(instance, result)

		return result
	}

	if instance.Language == LangPy && d.moduleStmt != nil && d.moduleStmt.Name != tokProtoLibrary && d.multimodule != tokRosLibrary {
		cpp := instance

		cpp.Language = LangCPP

		result := genModule(ctx, cpp)

		ctx.memoPut(instance, result)

		return result
	}

	defer func() {
		if result != nil {
			e.publishModuleIncludes(result)
		}

		if result == nil || !ctx.testMode || !d.test.validateData {
			return
		}

		project := instance.Path.relString()

		for _, suite := range []*TestSuite{ctx.dataValidationSuite(project, d.test.data, d.test.env, result.ResourceGlobalClosure), ctx.sandboxValidationSuite(project, d.fromSandboxes, d.test.env, result.ResourceGlobalClosure)} {
			if suite != nil {
				result.TestSuites = append(result.TestSuites, suite)
			}
		}
	}()

	for i, group := range d.pySrcGroups {
		if group.AllSrcs != nil {
			applyAllPySrcs(ctx.fs, instance.Path.relString(), *group.AllSrcs, d, i)
		}
	}

	if d.conflictMod != nil {
		throwFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path.relString(), d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path.relString())
	}

	e.prepareUserverProtos()

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangCPP && moduleExcludesTag(d, "CPP_PROTO") {
		result := &ModuleEmitResult{ModuleVariants: d.moduleVariants, ModuleStmtName: tokProtoLibrary}

		ctx.memoPut(instance, result)

		return result
	}

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy2 {
		throwFmt("gen: %s: the Python 2 variant of PROTO_LIBRARY (PY_PROTO) is not modelled", instance.Path.relString())
	}

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangDescProto {
		result := &ModuleEmitResult{ModuleStmtName: d.moduleStmt.Name}

		if !moduleExcludesTag(d, "DESC_PROTO") {
			result = e.emitDescProtoSubmodule()
		}

		ctx.memoPut(instance, result)

		return result
	}

	if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangDocsProto {
		result := e.emitDocsProtoSubmodule()

		ctx.memoPut(instance, result)

		return result
	}

	if d.nots != nil {
		if d.moduleStmt.Name == tokProtoLibrary {
			tag := "TS_PROTO"

			if d.moduleStmt.Schema {
				tag = "TS_PROTO_FROM_SCHEMA"
			}

			if instance.Language == LangTSPrepare {
				tag = "TS_PREPARE_DEPS"
			}

			if !slices.Contains(d.nots.IncludeTags, tag) || moduleExcludesTag(d, tag) {
				result := &ModuleEmitResult{ModuleStmtName: d.moduleStmt.Name}

				ctx.memoPut(instance, result)

				return result
			}
		}

		result := e.emitNotsModule()

		ctx.memoPut(instance, result)

		return result
	}

	if d.moduleStmt.Name == tokGenLibrary {
		return e.genGeneratedLibrary()
	}

	if d.moduleStmt.Name == tokExectest {
		return e.genExecTest()
	}

	if d.moduleStmt.Name == tokPackageFinal {
		return e.genPackageFinal()
	}

	if isUnionModule(d.moduleStmt.Name) {
		return e.genUnion()
	}

	if d.moduleStmt.Name == tokCiGroup {
		return e.genCiGroup()
	}

	if d.docs != nil {
		return e.genDocs()
	}

	if d.moduleStmt.Name == tokProtoDescriptions || d.moduleStmt.Name == tokProtoRegistry {
		result := e.emitProtoDescriptions()

		ctx.memoPut(instance, result)

		return result
	}

	if d.moduleStmt.Name == tokResourcesLibrary {
		if e.bindResourceGlobalVars(env) {
			d = collectModuleInto(ctx, instance, stmts, env, ctx.onWarn, &frame.d)
			e = newEmitContextIn(frame, ctx, instance, d, nil)
		}

		return e.genResourcesLibrary()
	}

	if d.moduleStmt.Name == tokPrebuiltProgram {
		env.setString(envMODULE_SUFFIX, prebuiltModuleSuffix(instance.Platform))

		if e.bindResourceGlobalVars(env) {
			d = collectModuleInto(ctx, instance, stmts, env, ctx.onWarn, &frame.d)
			e = newEmitContextIn(frame, ctx, instance, d, nil)
		}

		if d.yqlPrebuiltTool {
			return e.genYqlPrebuiltTool()
		}

		return e.genPrebuiltProgram()
	}

	if d.yqlPrebuiltResource != "" {
		return e.genYqlPrebuiltProgram()
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

		ctx.memoPut(instance, result)

		return result
	}

	languageDefaults := e.defaultPeerdirsForModule()
	isProgram := (isProgramModuleType(d.moduleStmt.Name) || isDllModuleType(d.moduleStmt.Name)) && !isRuntimeAncestor(instance.Path.relString())
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

	allPeers := frame.allPeers[:0]
	peerKinds := frame.peerKinds[:0]

	defer func() {
		frame.allPeers = allPeers[:0]
		frame.peerKinds = peerKinds[:0]
	}()

	var preResolved []ResolvedPeer
	var resolveCppSelf, resolveGoogleapis bool

	frontSet := make(map[ANY]struct{}, len(d.protoCmdPeers))

	for _, p := range d.protoCmdPeers {
		frontSet[p] = struct{}{}
	}

	dedupers.with(func(deduper *DeDuper) {
		peerSeen := func(p string) bool {
			return !deduper.add(internStr(p).any().strID())
		}

		if d.moduleStmt.Name == tokProtoLibrary && instance.Language == LangPy && d.optimizePyProtos && !moduleExcludesTag(d, "CPP_PROTO") {
			peerSeen(instance.Path.relString())

			resolveCppSelf = true
		}

		if d.moduleStmt.Name == tokProtoLibrary && d.useCommonGoogleAPIs && instance.Language == LangCPP {
			resolveGoogleapis = !peerSeen(googleapisPeer)
		}

		for _, p := range languageDefaults {
			peerSeen(p)

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
	})

	preResolved = make([]ResolvedPeer, 0, 2)

	for _, language := range d.selfPeers {
		self := instance

		self.Language = language
		preResolved = append(preResolved, ResolvedPeer{path: instance.Path.relString(), result: genModule(ctx, self), kind: peerKindLangDefault})
	}

	if resolveCppSelf {
		cppSelf := instance

		cppSelf.Language = LangCPP
		preResolved = append(preResolved, ResolvedPeer{path: instance.Path.relString(), result: genModule(ctx, cppSelf), kind: peerKindLangDefault})
	}

	if resolveGoogleapis {
		preResolved = append(preResolved, ResolvedPeer{path: googleapisPeer, result: genModule(ctx, e.derivePeerInstance(googleapisPeer)), kind: peerKindLangDefault})
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

		resolved = append(resolved, ResolvedPeer{path: peerPath, result: peerResult, kind: kind})
	}

	var ownResourceGlobals []ResourceDecl

	for _, stmt := range d.resourceDeclStmts {
		ownResourceGlobals = append(ownResourceGlobals, resolveResourceDecls(ctx.fs, ctx.host, instance.Path.relString(), stmt)...)
	}

	for _, decl := range ownResourceGlobals {
		emitResourceFetch(ctx, decl)
	}

	var rosInclude []VFS

	for _, peer := range resolved {
		rosInclude = append(rosInclude, peer.result.RosInclude...)
	}

	rosInclude = ctx.na.vfsList(concat(dedup(d.rosInclude), dedup(rosInclude))...)

	rosGlobalInclude := ctx.na.vfsList(dedup(rosInclude)...)

	var globalSources []VFS
	var pchFlagsGlobal []ANY

	resGlobalsSum := len(ownResourceGlobals)
	archiveCap := 0
	sbomCap := 0
	globalCap := 0
	waCap := 0
	waCmdCap := 0
	dynCap := 0
	protoCap := 0
	swigCap := 0
	cflagsCap := 0
	cxxflagsCap := 0
	conlyflagsCap := 0
	objAddLibsCap := 0
	ldflagsCap := 0
	rpathflagsCap := 0
	fbsCap := 0

	for _, rp := range resolved {
		pr := rp.result

		resGlobalsSum += len(pr.ResourceGlobalClosure)
		globalSources = append(globalSources, pr.GlobalSources...)
		pchFlagsGlobal = append(pchFlagsGlobal, pr.PchFlagsGlobal...)
		archiveCap += len(pr.PeerArchiveClosurePaths)
		globalCap += len(pr.PeerGlobalClosurePaths)
		waCap += len(pr.PeerWholeArchiveClosurePaths) + len(pr.WholeArchivePaths)
		waCmdCap += len(pr.PeerWholeArchiveCmdClosurePaths) + len(pr.WholeArchiveCmdPaths)
		dynCap += len(pr.PeerDynamicClosurePaths)
		protoCap += len(pr.ProtoInclude)
		swigCap += len(pr.SwigInclude)
		cflagsCap += len(pr.CFlagsGlobal)
		cxxflagsCap += len(pr.CXXFlagsGlobal)
		conlyflagsCap += len(pr.COnlyFlagsGlobal)
		objAddLibsCap += len(pr.ObjAddLibsGlobal)
		ldflagsCap += len(pr.LDFlagsGlobal)
		rpathflagsCap += len(pr.RPathFlagsGlobal)
		fbsCap += len(pr.FbsNamespaces)

		if pr.ARPath != nil {
			archiveCap++
		}

		if pr.GlobalRef != nil && pr.GlobalPath != nil {
			globalCap++
		}

		if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil {
			dynCap++
		}

		if ctx.sbomEnabled {
			sbomCap += len(pr.PeerSbomClosurePaths)

			if pr.SbomComponentRef != nil {
				sbomCap++
			}
		}
	}

	peerLinkCmdPaths := frame.peerLinkCmdPaths[:0]

	defer func() { frame.peerLinkCmdPaths = peerLinkCmdPaths[:0] }()

	var (
		resourceGlobalsClosure []ResourceDecl

		peerArchiveRefs, peerGlobalRefs, peerWholeArchiveRefs, peerDynamicRefs, peerSbomRefs []NodeRef

		peerArchivePaths, peerGlobalPaths, peerWholeArchivePaths, peerWholeArchiveCmdPaths, peerDynamicPaths, peerSbomPaths, effectiveAddInclGlobal, effectiveProtoInclude, peerProtoInclude, effectiveSwigInclude, moduleSwigInclude []VFS

		effectiveCFlagsGlobal, effectiveCXXFlagsGlobal, effectiveCOnlyFlagsGlobal, effectiveRPathFlagsGlobal []ANY

		ownSbomInsertIdx = -1
	)

	dedupers.with(func(deduper *DeDuper) {
		k := 0

		if resGlobalsSum != 0 {
			declBlock := ctx.declSlices.alloc(resGlobalsSum)

			for _, decl := range ownResourceGlobals {
				if deduper.addStable(decl.GlobalVar.strID()) {
					declBlock[k] = decl
					k++
				}
			}

			for _, rp := range resolved {
				for _, decl := range rp.result.ResourceGlobalClosure {
					if deduper.addStable(decl.GlobalVar.strID()) {
						declBlock[k] = decl
						k++
					}
				}
			}

			resourceGlobalsClosure = ctx.declSlices.intern(declBlock[:k])
		}

		d.tc = resolveModuleToolchain(ctx, resourceGlobalsClosure, instance.Platform.ClangVer)

		deduper.reset()

		if archiveCap != 0 {
			archiveBlockR := ctx.refSlices.alloc(archiveCap)
			archiveBlockP := ctx.vfsSlices.alloc(archiveCap)

			k = 0

			for _, rp := range resolved {
				pr := rp.result

				for i, p := range pr.PeerArchiveClosurePaths {
					if deduper.addStable(p.strID()) {
						archiveBlockR[k] = pr.PeerArchiveClosureRefs[i]
						archiveBlockP[k] = p
						k++
					}
				}

				if pr.ARPath != nil && deduper.addStable(pr.ARPath.strID()) {
					archiveBlockR[k] = pr.ARRef
					archiveBlockP[k] = *pr.ARPath
					k++
				}
			}

			peerArchiveRefs = ctx.refSlices.intern(archiveBlockR[:k])
			peerArchivePaths = ctx.vfsSlices.intern(archiveBlockP[:k])
		}

		deduper.reset()

		if globalCap != 0 {
			globalBlockR := ctx.refSlices.alloc(globalCap)
			globalBlockP := ctx.vfsSlices.alloc(globalCap)

			k = 0

			for _, rp := range resolved {
				pr := rp.result

				for i, p := range pr.PeerGlobalClosurePaths {
					if deduper.addStable(p.strID()) {
						globalBlockR[k] = pr.PeerGlobalClosureRefs[i]
						globalBlockP[k] = p
						k++
					}
				}

				if pr.GlobalRef != nil && pr.GlobalPath != nil && deduper.addStable(pr.GlobalPath.strID()) {
					globalBlockR[k] = *pr.GlobalRef
					globalBlockP[k] = *pr.GlobalPath
					k++
				}
			}

			peerGlobalRefs = ctx.refSlices.intern(globalBlockR[:k])
			peerGlobalPaths = ctx.vfsSlices.intern(globalBlockP[:k])
		}

		if ctx.sbomEnabled {
			linkTarget := isProgramModuleType(d.moduleStmt.Name) || d.moduleStmt.Name == tokDllTool
			sbomBlockR := ctx.refSlices.alloc(sbomCap)
			sbomBlockP := ctx.vfsSlices.alloc(sbomCap)
			peerSbomRefsRaw, peerSbomPathsRaw, sbomInsertIdx := aggregateSbomComponents(deduper, e, d.moduleStmt.Name, linkTarget, resolved, allocatorExplicitPeers, sbomBlockR[:0], sbomBlockP[:0])

			ownSbomInsertIdx = sbomInsertIdx
			peerSbomRefs = ctx.refSlices.intern(peerSbomRefsRaw)
			peerSbomPaths = ctx.vfsSlices.intern(peerSbomPathsRaw)
		}

		deduper.reset()

		if waCap != 0 {
			waBlockR := ctx.refSlices.alloc(waCap)
			waBlockP := ctx.vfsSlices.alloc(waCap)

			k = 0

			for _, rp := range resolved {
				pr := rp.result

				for i, p := range pr.PeerWholeArchiveClosurePaths {
					if deduper.addStable(p.strID()) {
						waBlockR[k] = pr.PeerWholeArchiveClosureRefs[i]
						waBlockP[k] = p
						k++
					}
				}

				for i, p := range pr.WholeArchivePaths {
					if deduper.addStable(p.strID()) {
						waBlockR[k] = pr.WholeArchiveRefs[i]
						waBlockP[k] = p
						k++
					}
				}
			}

			peerWholeArchiveRefs = ctx.refSlices.intern(waBlockR[:k])
			peerWholeArchivePaths = ctx.vfsSlices.intern(waBlockP[:k])
		}

		deduper.reset()

		if waCmdCap != 0 {
			waCmdBlock := ctx.vfsSlices.alloc(waCmdCap)

			k = 0

			for _, rp := range resolved {
				pr := rp.result

				for _, p := range pr.PeerWholeArchiveCmdClosurePaths {
					if deduper.addStable(p.strID()) {
						waCmdBlock[k] = p
						k++
					}
				}

				for _, p := range pr.WholeArchiveCmdPaths {
					if deduper.addStable(p.strID()) {
						waCmdBlock[k] = p
						k++
					}
				}
			}

			peerWholeArchiveCmdPaths = ctx.vfsSlices.intern(waCmdBlock[:k])
		}

		deduper.reset()

		if dynCap != 0 {
			dynBlockR := ctx.refSlices.alloc(dynCap)
			dynBlockP := ctx.vfsSlices.alloc(dynCap)

			k = 0

			for _, rp := range resolved {
				pr := rp.result

				for i, p := range pr.PeerDynamicClosurePaths {
					if deduper.addStable(p.strID()) {
						dynBlockR[k] = pr.PeerDynamicClosureRefs[i]
						dynBlockP[k] = p
						k++
					}
				}

				if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.addStable(pr.LDPath.strID()) {
					dynBlockR[k] = pr.LDRef
					dynBlockP[k] = *pr.LDPath
					k++
				}
			}

			peerDynamicRefs = ctx.refSlices.intern(dynBlockR[:k])
			peerDynamicPaths = ctx.vfsSlices.intern(dynBlockP[:k])
		}

		deduper.reset()

		if archiveCap+dynCap != 0 {
			for _, rp := range resolved {
				pr := rp.result

				for _, p := range pr.PeerArchiveClosurePaths {
					if deduper.addStable(p.strID()) {
						peerLinkCmdPaths = append(peerLinkCmdPaths, p)
					}
				}

				for _, p := range pr.PeerDynamicClosurePaths {
					if deduper.addStable(p.strID()) {
						peerLinkCmdPaths = append(peerLinkCmdPaths, p)
					}
				}

				if pr.ModuleStmtName == tokDynamicLibrary && pr.LDPath != nil && deduper.addStable(pr.LDPath.strID()) {
					peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.LDPath)
				}

				if pr.ARPath != nil && deduper.addStable(pr.ARPath.strID()) {
					peerLinkCmdPaths = append(peerLinkCmdPaths, *pr.ARPath)
				}
			}
		}

		deduper.reset()

		for _, rp := range resolved {
			switch rp.kind {
			case peerKindLangDefault:
				for _, p := range rp.result.OwnAddInclGlobal {
					if deduper.addStable(p.strID()) {
						peerAddInclGlobal = append(peerAddInclGlobal, p)
					}
				}

				for _, p := range rp.result.AddInclGlobal {
					if deduper.addStable(p.strID()) {
						peerAddInclGlobal = append(peerAddInclGlobal, p)
					}
				}
			case peerKindUnitTestPeer, peerKindProgramDefault:
				for _, p := range rp.result.AddInclGlobal {
					if deduper.addStable(p.strID()) {
						peerAddInclGlobal = append(peerAddInclGlobal, p)
					}
				}
			case peerKindUserPeer:
				for _, p := range rp.result.AddInclUserGlobal {
					if deduper.addStable(p.strID()) {
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
					if deduper.addStable(p.strID()) {
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

		if cflagsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.CFlagsGlobal {
					if deduper.addStable(a.strID()) {
						peerCFlagsGlobal = append(peerCFlagsGlobal, a)
					}
				}
			}
		}

		deduper.reset()

		if cxxflagsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.CXXFlagsGlobal {
					if deduper.addStable(a.strID()) {
						peerCXXFlagsGlobal = append(peerCXXFlagsGlobal, a)
					}
				}
			}
		}

		deduper.reset()

		if conlyflagsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.COnlyFlagsGlobal {
					if deduper.addStable(a.strID()) {
						peerCOnlyFlagsGlobal = append(peerCOnlyFlagsGlobal, a)
					}
				}
			}
		}

		deduper.reset()

		if objAddLibsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.ObjAddLibsGlobal {
					if deduper.addStable(a.strID()) {
						peerObjAddLibsGlobal = append(peerObjAddLibsGlobal, a)
					}
				}
			}
		}

		deduper.reset()

		if ldflagsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.LDFlagsGlobal {
					if deduper.addStable(a.strID()) {
						peerLDFlagsGlobal = append(peerLDFlagsGlobal, a)
					}
				}
			}
		}

		deduper.reset()

		if rpathflagsCap != 0 {
			for _, rp := range cflagsAggOrder {
				for _, a := range rp.result.RPathFlagsGlobal {
					if deduper.addStable(a.strID()) {
						peerRPathFlagsGlobal = append(peerRPathFlagsGlobal, a)
					}
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

		effectiveAddInclGlobal = dedupSharedWith(deduper, ctx.vfsSlices, d.addInclGlobal, peerAddInclForProp)

		ownProtoInclude := frame.ownProtoInclude[:0]

		defer func() { frame.ownProtoInclude = ownProtoInclude[:0] }()

		ownProtoInclude = append(ownProtoInclude, d.protoAddInclGlobal...)
		peerProtoInclude = frame.peerProtoInclude[:0]

		defer func() { frame.peerProtoInclude = peerProtoInclude[:0] }()

		deduper.reset()

		if protoCap != 0 {
			for _, rp := range resolved {
				for _, p := range rp.result.ProtoInclude {
					if deduper.addStable(p.strID()) {
						peerProtoInclude = append(peerProtoInclude, p)
					}
				}
			}
		}

		effectiveProtoInclude = dedupSharedWith(deduper, ctx.vfsSlices, ownProtoInclude, peerProtoInclude)

		if swigCap != 0 || len(d.swigAddIncl) != 0 {
			var peerSwigInclude []VFS

			deduper.reset()

			for _, rp := range resolved {
				for _, p := range rp.result.SwigInclude {
					if deduper.addStable(p.strID()) {
						peerSwigInclude = append(peerSwigInclude, p)
					}
				}
			}

			effectiveSwigInclude = dedupSharedWith(deduper, ctx.vfsSlices, d.swigAddInclGlobal, peerSwigInclude)
			moduleSwigInclude = dedupSharedWith(deduper, ctx.vfsSlices, d.swigAddIncl, peerSwigInclude)
		}

		effectiveCFlagsGlobal = dedupSharedWith(deduper, ctx.argSlices, peerCFlagsGlobal, d.cFlagsGlobal)
		effectiveCXXFlagsGlobal = concatShared(ctx.argSlices, peerCXXFlagsGlobal, d.cxxFlagsGlobal)
		effectiveCOnlyFlagsGlobal = concatShared(ctx.argSlices, peerCOnlyFlagsGlobal, d.cOnlyFlagsGlobal)
		effectiveRPathFlagsGlobal = concatShared(ctx.argSlices, peerRPathFlagsGlobal, d.rpathFlagsGlobal)
	})

	var linkExcludeLibraries []ANY

	dedupers.with(func(deduper *DeDuper) {
		for _, rp := range resolved {
			for _, lib := range rp.result.LinkExcludeLibraries {
				if deduper.addStable(lib.strID()) {
					linkExcludeLibraries = append(linkExcludeLibraries, lib)
				}
			}
		}

		for _, lib := range d.linkExcludeLibraries {
			if deduper.addStable(lib.strID()) {
				linkExcludeLibraries = append(linkExcludeLibraries, lib)
			}
		}
	})

	linkExcludeLibraries = ctx.argSlices.internCopy(linkExcludeLibraries)

	var effectiveFbsNamespaces []FbsNamespaceVar

	if fbsCap != 0 || len(d.fbsNamespaces) != 0 {
		effectiveFbsNamespaces = collectFbsNamespaceVars(ctx, d.fbsNamespaces, resolved)
	}

	effectiveMlaunchDeps := mlaunchDeps(d, resolved)
	effectiveLDFlagsGlobal := concatShared(ctx.argSlices, peerLDFlagsGlobal, d.ldFlags)

	newResult := func() *ModuleEmitResult {
		r := ctx.results.one()

		*r = ModuleEmitResult{
			TestSuites:                      e.styleSuites(resourceGlobalsClosure),
			ModuleVariants:                  d.moduleVariants,
			GlobalSources:                   e.globalSources,
			PchFlagsGlobal:                  d.cc.PchFlagsGlobal,
			MlaunchDeps:                     effectiveMlaunchDeps,
			FbsNamespaces:                   effectiveFbsNamespaces,
			isPyLibrary:                     isPyLibraryType(d.moduleStmt.Name),
			AddInclGlobal:                   effectiveAddInclGlobal,
			OwnAddInclGlobal:                d.addInclGlobal,
			ProtoInclude:                    effectiveProtoInclude,
			RosInclude:                      rosGlobalInclude,
			SwigInclude:                     effectiveSwigInclude,
			AddInclOneLevel:                 d.addInclOneLevel,
			AddInclUserGlobal:               d.addInclUserGlobal,
			CXXFlagsGlobal:                  effectiveCXXFlagsGlobal,
			COnlyFlagsGlobal:                effectiveCOnlyFlagsGlobal,
			LinkExcludeLibraries:            linkExcludeLibraries,
			ObjAddLibsGlobal:                concatShared(ctx.argSlices, peerObjAddLibsGlobal, d.objAddLibsGlobal),
			LDFlagsGlobal:                   effectiveLDFlagsGlobal,
			RPathFlagsGlobal:                effectiveRPathFlagsGlobal,
			PeerArchiveClosureRefs:          peerArchiveRefs,
			PeerArchiveClosurePaths:         peerArchivePaths,
			PeerGlobalClosureRefs:           peerGlobalRefs,
			PeerGlobalClosurePaths:          peerGlobalPaths,
			PeerWholeArchiveClosureRefs:     peerWholeArchiveRefs,
			PeerWholeArchiveClosurePaths:    peerWholeArchivePaths,
			PeerWholeArchiveCmdClosurePaths: peerWholeArchiveCmdPaths,
			PeerDynamicClosureRefs:          peerDynamicRefs,
			PeerDynamicClosurePaths:         peerDynamicPaths,
			PeerSbomClosureRefs:             peerSbomRefs,
			PeerSbomClosurePaths:            peerSbomPaths,
			InducedDeps:                     d.inducedDeps,
			ModuleStmtName:                  d.moduleStmt.Name,
			CFlagsGlobal:                    effectiveCFlagsGlobal,
		}

		return r
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
	perModuleCCTag := d.unit.CCTag

	var arNameFn func(string) string
	var globalArNameFn func(string) string

	archiveName := ""

	if len(d.moduleStmt.Args) > 0 {
		archiveName = d.moduleStmt.Args[0].string()
	}

	arNameFn = func(dir string) string { return e.arName(dir, d.unit.ARPrefix, archiveName) }
	globalArNameFn = func(dir string) string { return e.globalArName(dir, d.unit.ARPrefix, archiveName) }

	selfPeerAddInclGlobal := filterBuildRootSelfPaths(instance.Path.relString(), peerAddInclGlobal, dedupedAddIncl)

	if d.moduleStmt.Name == tokProtoLibrary {
		selfPeerAddInclGlobal = peerAddInclGlobal
	}

	dedupedAddIncl = ctx.na.vfsList(dedupedAddIncl...)
	selfPeerAddInclGlobal = ctx.na.vfsList(selfPeerAddInclGlobal...)

	fullPeerAddInclGlobal := ctx.na.vfsList(peerAddInclGlobal...)
	effectiveSrcDirs := d.srcDirs

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		effectiveSrcDirs = concat(d.srcDirs, []VFS{dirKey(*pd).source()})
	}

	for _, stmt := range d.pchFiles {
		out := copyFileOutputVFS(instance.Path.relString(), stmt.Output.string())

		pchFlagsGlobal = append(pchFlagsGlobal, internV("--include-pch=", out.string()).any())
	}

	pchFlagsGlobal = ctx.argSlices.internCopy(dedup(pchFlagsGlobal))

	d.cc = ModuleCompileEnv{
		PchFlagsGlobal:       pchFlagsGlobal,
		InclArgs:             ctx.inclArgs,
		Flags:                d.flags,
		AddIncl:              dedupedAddIncl,
		PeerAddInclGlobal:    selfPeerAddInclGlobal,
		ProtoInclude:         effectiveProtoInclude,
		ProtoIncludePeers:    peerProtoInclude,
		SwigInclude:          moduleSwigInclude,
		CFlags:               ownCFlags,
		CXXFlags:             d.cxxFlags,
		CXXFlagsVar:          d.cxxFlagsVar,
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
		ObjSuf:               d.unit.ObjSuf,
		ObjectSuffixStem: func() *string {
			if d.unit.ObjectSuffixStem != "" {
				return &d.unit.ObjectSuffixStem
			}

			if isYqlUdfStaticModule(d.moduleStmt.Name) {
				return ptr("udfs")
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

	d.cc.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &d.cc)

	var modulePeers []VFS

	for _, peer := range resolved {
		modulePeers = e.appendModulePeer(modulePeers, peer.result, len(resolved))
	}

	if modulePeers != nil {
		ctx.na.vfs.commit(len(modulePeers))
	}

	frame.peerCtx = PeerContext{
		Modules:           modulePeers,
		GlobalSources:     globalSources,
		SelfAddInclGlobal: selfPeerAddInclGlobal,
		PeerAddInclGlobal: fullPeerAddInclGlobal,
		ResourceGlobals:   resourceGlobalsClosure,
		ProtoInclude:      peerProtoInclude,
	}

	if d.hasFbs && isGoModuleType(d.moduleStmt.Name) {
		frame.peerCtx.FbsNamespaces = fbsNamespaceArgs(ctx.na, effectiveFbsNamespaces)
	}

	if isGoModuleType(d.moduleStmt.Name) {
		frame.peerCtx.LDFlagsGlobal = resolveResourceGlobalArgs(effectiveLDFlagsGlobal, resourceGlobalsClosure)
	}

	e = newEmitContextIn(frame, ctx, instance, d, &frame.peerCtx)

	scanAddIncl := e.cythonAdjustModuleCCBlocks()

	e.sprotoAdjustProtoEnv()

	if len(d.protoAddIncl) > 0 {
		d.cc.ProtoInclude = dedup(d.protoAddInclGlobal, d.protoAddIncl, effectiveProtoInclude)
	}

	d.scanCtx = newModuleScanContext(ctx, instance, d, scanAddIncl, d.cc.PeerAddInclGlobal, fullPeerAddInclGlobal, d.cc.ProtoInclude)
	d.scanCtx.rosInclude = rosInclude
	e.emitMlaunch(resolved, peerArchivePaths, effectiveMlaunchDeps)
	e.emit()
	e.emitLDPlugins()
	e.collectPeerModuleGlobals()

	if instance.Demand == demandNone {
		result := newResult()

		result.ResourceGlobalClosure = resourceGlobalsClosure

		ctx.memoPut(instance, result)

		return result
	}

	local, global := e.partitionCollected()

	for _, out := range local.outs {
		e.codegen.use(out)
	}

	for _, out := range global.outs {
		e.codegen.use(out)
	}

	globalRefs := global.refs
	globalOutputs := global.outs
	globalMetas := global.metas

	if sbomActive(ctx, instance) && env.bool(envCLANG) && len(local.refs) > 0 {
		if r, p := clangToolchainSbomComponent(ctx, instance.Platform); r != nil && !slices.Contains(peerSbomPaths, *p) {
			peerSbomRefs = append(peerSbomRefs, *r)
			peerSbomPaths = append(peerSbomPaths, *p)
		}
	}

	if isGoExeModuleType(d.moduleStmt.Name) {
		goRef, goPath := e.emitGoExe(resolved, local.refs, local.outs, peerObjAddLibsGlobal, peerArchiveRefs, peerArchivePaths, peerGlobalRefs, peerGlobalPaths, peerSbomRefs, peerSbomPaths, resourceGlobalsClosure)
		result := newResult()

		result.isPROGRAM = true
		result.LDRef = goRef
		result.LDPath = ptr(goPath)
		result.ARRef = goRef
		result.ARPath = ptr(goPath)
		result.TestSuites = append(result.TestSuites, e.goTestSuites(goRef, build(goPath.relString(), ".vet.txt"), resourceGlobalsClosure)...)

		ctx.memoPut(instance, result)

		return result
	}

	if isProgramModuleType(d.moduleStmt.Name) || isDllModuleType(d.moduleStmt.Name) {
		binaryName := programBinaryName(instance, d.moduleStmt)
		exportsScript := e.moduleInputArg(d.exportsScript)
		linkerScripts := make([]VFS, len(d.linkerScripts))

		for i, script := range d.linkerScripts {
			linkerScripts[i] = e.moduleSourceVFS(script)
		}

		var dyn *DynLibLink

		if isDllModuleType(d.moduleStmt.Name) {
			binaryName = dllOutputName(instance, d.moduleStmt)
			dyn = &DynLibLink{soname: binaryName, symbolic: d.moduleStmt.Name == tokPy2Module && instance.Platform.OS != OSDarwin && instance.Platform.OS != OSWindows}

			if version := dllModuleVersion(d.moduleStmt); version != "" {
				dyn.soname += version
				dyn.alias = build(instance.Path.relString(), "/", dyn.soname)
			}

			if instance.Platform.OS == OSLinux && instance.Platform.ISA == ISAX8664 {
				dyn.fixElfRef, dyn.fixElf = ctx.tool(argToolsFixElf)
			}

			if d.yqlUdfProbe {
				dyn.probeRef, dyn.probe = ctx.tool(argYqlUdfProbe)
			}
		}

		ldPeerArchiveRefs := peerArchiveRefs
		ldPeerArchivePaths := peerArchivePaths
		ldPeerLinkCmdPaths := peerLinkCmdPaths
		ldInstance := instance
		ldCCRefs := local.refs
		ldCCOutputs := local.outs

		ldCCRefs, ldCCOutputs = e.reorderARMembers(ldCCRefs, ldCCOutputs, local.metas)

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

		pyStrip := isPyProgramType(d.moduleStmt.Name) &&
			!instance.Platform.pythonNoStrip()

		wantsStrip := (pyStrip || d.strip) && !d.noStrip

		var programModuleTag STR

		if d.moduleStmt.Name == tokPy3Program {
			programModuleTag = tagPy3Bin
		}

		var ownSbomRef *NodeRef
		var ownSbomPath *VFS

		if sbomActive(ctx, instance) && sbomQualifies(d) {
			sbomName := binaryName

			if dyn != nil {
				sbomName = strings.TrimSuffix(binaryName, ".so")
			}

			ownSbomRef, ownSbomPath = e.emitSbomComponent(sbomName)
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

		ldRef := e.emitLD(
			ldInstance,
			binaryName,
			ldCCRefs, ldCCOutputs,
			ldPeerArchiveRefs, ldPeerArchivePaths,
			ldPeerLinkCmdPaths,
			peerGlobalRefs, peerGlobalPaths,
			peerWholeArchiveRefs, peerWholeArchivePaths,
			peerWholeArchiveCmdPaths,
			peerDynamicRefs, peerDynamicPaths,
			ldObjcopyRefs, ldObjcopyPaths,
			ldSbomRefs, ldSbomPaths,
			ownCFlags,
			peerCFlagsGlobal,
			d.moduleScopeCFlags,
			resolveResourceGlobalArgs(peerLDFlagsGlobal, resourceGlobalsClosure),
			resolveResourceGlobalArgs(d.ldFlags, resourceGlobalsClosure),
			ownRPathFlags,
			peerRPathFlagsGlobal,
			peerObjAddLibsGlobal,
			linkExcludeLibraries,
			exportsScript,
			linkerScripts,
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
			dyn,
		)

		ldPath := lDOutputPath(instance, binaryName)
		result := newResult()

		if dyn == nil {
			result.ARRef = ldRef
			result.ARPath = &ldPath
			result.isPROGRAM = true
		}

		result.LDRef = ldRef
		result.LDPath = &ldPath
		result.SbomComponentRef = ownSbomRef
		result.SbomComponentPath = ownSbomPath
		result.ResourceGlobalClosure = resourceGlobalsClosure
		result.TestSuites = append(result.TestSuites, e.testRunSuites(ldPath, ldRef, resourceGlobalsClosure)...)
		result.TestSuites = append(result.TestSuites, e.importTestSuites(ldPath, ldRef, resourceGlobalsClosure)...)

		ctx.memoPut(instance, result)

		return result
	}

	local.refs, local.outs = e.reorderARMembers(local.refs, local.outs, local.metas)

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

	var arPath *VFS

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if isGoModuleType(d.moduleStmt.Name) {
		if sbomActive(ctx, instance) && sbomQualifies(d) {
			rel := instance.Path.relString()

			ownSbomRef, ownSbomPath = e.emitSbomComponent(rel[strings.LastIndexByte(rel, '/')+1:])
		}

		goRef, goPath := e.emitGoPackage(resolved, local.refs, local.outs, peerArchiveRefs, peerArchivePaths, peerGlobalRefs, peerGlobalPaths, peerSbomRefs, peerSbomPaths, ownSbomRef, ownSbomPath, resourceGlobalsClosure)

		arRef = goRef
		arPath = ptr(goPath)
	} else if len(local.refs) > 0 || d.moduleStmt.Name == tokCudaDeviceLinkLibrary {
		if d.moduleStmt.Name == tokCudaDeviceLinkLibrary {
			arRef = e.emitCudaDeviceLink(arBaseName, local, arPluginVFS)
		} else if perModuleCCTag != 0 {
			arRef = e.emitARNamedTagged(arInstance, arBaseName, perModuleCCTag, local.refs, local.outs, arPluginVFS, d.tc, ctx.host, ctx.emit)
		} else {
			arRef = e.emitARNamed(arInstance, arBaseName, local.refs, local.outs, arPluginVFS, d.tc, ctx.host, ctx.emit)
		}

		p := ctx.emit.nodeArenas().vfs.one()

		*p = build(instance.Path.relString(), "/", arBaseName)
		arPath = p
	}

	if sbomActive(ctx, instance) && sbomQualifies(d) && d.unit.Tag != unitTagPy3BinLib && !isGoModuleType(d.moduleStmt.Name) {
		realPrjName := strings.TrimSuffix(e.arName(instance.Path.relString(), "", archiveName), ".a")

		ownSbomRef, ownSbomPath = e.emitSbomComponent(realPrjName)
	}

	result = newResult()
	result.ARRef = arRef
	result.ARPath = arPath
	result.ArchiveFile = build(instance.Path.relString(), "/", arBaseName)
	result.LDRef = arRef
	result.LDPath = arPath
	result.SbomComponentRef = ownSbomRef
	result.SbomComponentPath = ownSbomPath
	result.ResourceGlobalClosure = resourceGlobalsClosure

	if isGoModuleType(d.moduleStmt.Name) {
		result.TestSuites = append(result.TestSuites, e.goTestSuites(arRef, build(arPath.relString(), ".vet.txt"), resourceGlobalsClosure)...)
	}

	if len(globalRefs) > 0 {
		globalBaseName := globalArNameFn(instance.Path.relString())
		globalTag := d.unit.GlobalARTag

		globalRefs, globalOutputs = e.reorderARMembers(globalRefs, globalOutputs, globalMetas)

		globalRef := emitARGlobalNamedTagged(arInstance, globalBaseName, globalTag, globalRefs, globalOutputs, d.tc, ctx.host, ctx.emit)

		result.GlobalRef = &globalRef
		result.GlobalPath = ptr(build(instance.Path.relString(), "/", globalBaseName))
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

	ctx.memoPut(instance, result)

	return result
}

func filterBuildRootSelfPaths(instancePath string, peer, own []VFS) []VFS {
	if len(peer) == 0 {
		return peer
	}

	ownPrefix := build(instancePath)
	out := peer

	dedupers.with(func(deduper *DeDuper) {
		matched := false

		for _, p := range own {
			if p.isBuild() && (p == ownPrefix || strings.HasPrefix(p.relString(), ownPrefix.relString()+"/")) {
				deduper.add(p.strID())
				matched = true
			}
		}

		if !matched {
			return
		}

		out = make([]VFS, 0, len(peer))

		for _, p := range peer {
			if deduper.has(p.strID()) {
				continue
			}

			out = append(out, p)
		}
	})

	return out
}

type ARMember struct {
	ref  NodeRef
	path VFS
	key  uint64
}

func (e *EmitContext) emittedProducer(ref NodeRef) *Node {
	return e.ctx.emit.nodes.s[ref]
}

func (e *EmitContext) queuedSourceInput(info *GeneratedFileInfo) VFS {
	if info.SourcePath != 0 {
		return info.SourcePath
	}

	producer := e.emittedProducer(info.ProducerRef)

	if producer == nil || producer.KV == nil || producer.KV.P != pkPB && producer.KV.P != pkR6 {
		return 0
	}

	for _, chunk := range producer.Inputs {
		for _, input := range chunk {
			next := e.codegen.lookup(input)

			if next != nil && next.OwnerModule == e.instance.Path && next.ProducerRef != info.ProducerRef {
				return input
			}
		}
	}

	return 0
}

func (e *EmitContext) srcRound(m SrcMeta) uint64 {
	cur := m.Source.vfs()

	if !cur.isBuild() {
		return 0
	}

	round := uint64(1)

	for {
		info := e.codegen.lookup(cur)
		nextPath := VFS(0)

		if info != nil {
			nextPath = e.queuedSourceInput(info)
		}

		if nextPath == 0 {
			return round
		}

		next := e.codegen.lookup(nextPath)

		if next == nil || next.OwnerModule != e.instance.Path {
			return round
		}

		if producer := e.emittedProducer(next.ProducerRef); producer != nil && producer.KV == &baseCodegenKV {
			return round
		}

		round++
		cur = nextPath
	}
}

func (e *EmitContext) reorderARMembers(refs []NodeRef, paths []VFS, metas []SrcMeta) ([]NodeRef, []VFS) {
	if len(paths) == 0 {
		return refs, paths
	}

	members := e.arMembers[:0]
	ordered := true

	var previous uint64

	for i := range paths {
		key := metas[i].sortKey(e.srcRound(metas[i]))

		if i > 0 && key < previous {
			ordered = false
		}

		members = append(members, ARMember{refs[i], paths[i], key})
		previous = key
	}

	e.arMembers = members[:0]

	if ordered {
		return refs, paths
	}

	slices.SortStableFunc(members, func(a, b ARMember) int {
		return cmp.Compare(a.key, b.key)
	})

	for i, m := range members {
		refs[i] = m.ref
		paths[i] = m.path
	}

	return refs, paths
}

func (ctx *GenCtx) tool(modulePath ARG) (NodeRef, VFS) {
	res := ctx.toolResult(modulePath)

	if res.LDPath == nil {
		throwFmt("gen: tool %s has no linkable output", modulePath.string())
	}

	return res.LDRef, *res.LDPath
}

func (ctx *GenCtx) toolResult(modulePath ARG) *ModuleEmitResult {
	if res, ok := ctx.tools.get(modulePath); ok {
		return res
	}

	var res *ModuleEmitResult

	if dir := modulePath.string(); isJavaModuleType(ctx.moduleName(dir)) {
		res = ctx.javaToolResult(dir)
	} else {
		res = genModule(ctx, newToolInstance(ctx.host, dir))
	}

	if res.LDRef != NodeRef(0) {
		ctx.tools.put(modulePath, res)
		ctx.moduleByRef.put(res.LDRef, res)
	}

	return res
}

func (ctx *GenCtx) scannerFor(instance ModuleInstance) *IncludeScanner {
	return ctx.scannerForPlatform(instance.Platform)
}

func (ctx *GenCtx) instanceVariant(in ModuleInstance) uint16 {
	pbit := uint16(0)

	switch in.Platform {
	case ctx.host:
		pbit = 1
	case ctx.target:
	default:
		pbit = variantPIC
	}

	if in.YqlCurrent {
		pbit |= 1 << 13
	}

	return uint16(in.Path&1)<<15 | uint16(in.Kind)<<8 | uint16(in.Demand)<<variantDemandShift | uint16(in.Language)<<1 | pbit
}

func (ctx *GenCtx) memoGet(in ModuleInstance) *ModuleEmitResult {
	variant := ctx.instanceVariant(in)
	other := variant &^ variantDemandMask

	var stronger *ModuleEmitResult

	for entry := ctx.memo.heads.getSafe(uint32(in.Path.rel())); entry != nil; entry = entry.next {
		if entry.variant == variant {
			return entry.result
		}

		if entry.variant&^variantDemandMask == other && entry.variant&variantDemandMask > variant&variantDemandMask {
			stronger = entry.result
		}
	}

	return stronger
}

func (ctx *GenCtx) memoPut(in ModuleInstance, result *ModuleEmitResult) {
	path := uint32(in.Path.rel())
	variant := ctx.instanceVariant(in)
	head := ctx.memo.heads.getSafe(path)
	entry := ctx.memo.entries.one()

	*entry = moduleMemoEntry{next: head, result: result, variant: variant}
	ctx.memo.heads.set(path, entry)
}

func (ctx *GenCtx) scannerForPlatform(p *Platform) *IncludeScanner {
	switch p {
	case ctx.host:
		return ctx.scannerHost
	case ctx.target:
		return ctx.scannerTarget
	}

	return ctx.targetPICScanner()
}

func (ctx *GenCtx) targetPICScanner() *IncludeScanner {
	if ctx.scannerTargetPIC == nil {
		s := newIncludeScannerWith(ctx.parsers, nil, ctx.onWarn, ctx.buckets)

		s.sysincl = ctx.scannerTarget.sysincl
		s.codegen = newCodegenRegistry(ctx.na)
		s.moduleByRef = &ctx.moduleByRef

		ctx.scannerTargetPIC = s
	}

	return ctx.scannerTargetPIC
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
	pending              int
	serial               uint32
	d                    ModuleData
	emitCtx              EmitContext
	peerCtx              PeerContext
	ifEnv                EnvStore
	allPeers             []string
	peerKinds            []int
	resolved             []ResolvedPeer
	peerAddInclGlobal    []VFS
	ownProtoInclude      []VFS
	peerProtoInclude     []VFS
	peerObjAddLibsGlobal []ANY
	peerLDFlagsGlobal    []ANY
	peerRPathFlagsGlobal []ANY
	peerCFlagsGlobal     []ANY
	peerCXXFlagsGlobal   []ANY
	peerCOnlyFlagsGlobal []ANY
	peerLinkCmdPaths     []VFS
}

func (ctx *GenCtx) pushFrame() *ModuleFrame {
	if ctx.frameDepth == len(ctx.frames) {
		ctx.frames = append(ctx.frames, &ModuleFrame{})
	}

	f := ctx.frames[ctx.frameDepth]

	if f.pending != 0 {
		f = &ModuleFrame{}
		ctx.frames[ctx.frameDepth] = f
	}

	ctx.frameDepth++
	ctx.frameSerial++
	f.serial = ctx.frameSerial

	return f
}

func (ctx *GenCtx) popFrame() {
	ctx.frameDepth--
}

func (ctx *GenCtx) renderModuleDirs() Environment {
	path := ctx.frames[ctx.frameDepth-1].emitCtx.instance.Path
	env := Environment{s: &ctx.moduleDirsEnv}

	env.setVFS(envCURDIR, path)
	env.setVFS(envBINDIR, path.rel().build())

	return env
}
