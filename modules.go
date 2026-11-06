package main

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

var (
	confPluginDepsCache sync.Map
	dontExpandVars      = [...]ENV{envARCADIA_BUILD_ROOT, envARCADIA_ROOT, envBINDIR, envCURDIR, envPythonBin, envTestCaseRoot, envTestOutRoot, envTestSourceRoot, envTestWorkRoot}
	pbRuntimeBaseString = pbRuntimeBaseVFS.string()
	strExpandsToSelf    [tokenExpandRoots + 1]BitSet
	tokenSubstBufs      [2][2][]byte
)

var dontExpandVarNames = func() (names [len(dontExpandVars)]string) {
	for i, id := range dontExpandVars {
		names[i] = id.string()
	}

	return names
}()

const (
	event2cppPluginName = "event2cpp"
	event2cppToolPath   = "tools/event2cpp"
	stmtPrioDefault     = 2
	stmtPrioSrcs        = 4
	prioAddIncl         = 0
	prioRuleAddIncl     = 2
	prioOutputAddIncl   = 3
	yaffPluginPath      = "library/cpp/yaff/tools/protoc_plugin"
	yabsConfRel         = "build/internal/conf/project_specific/yabs.conf"
	declSeqBlock        = 1 << 24
	declSeqModuleEnd    = 1 << 40
	declSeqText         = declSeqModuleEnd - 1
)

const (
	tokenExpandLazy TokenExpansion = iota
	tokenExpandArguments
	tokenExpandFinal
	tokenExpandAssignment
	tokenExpandPathArguments
	tokenExpandStatement
	tokenExpandRoots
)

type CppProtoPlugin struct {
	Name              string
	ToolPath          string
	OutputSuffixes    []string
	Deps              []string
	ExtraOutFlag      string
	OutputsBeforeBase bool
	ApplyToEvents     bool
}

func (p CppProtoPlugin) isYaff() bool {
	return p.ToolPath == yaffPluginPath
}

func addCPPProtoPlugin(d *ModuleData, plugin CppProtoPlugin) {
	d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)

	if !isGoModuleType(d.unit.Type) && !pythonProtoPeerContext(d) {
		d.peerdirs = append(d.peerdirs, internAnys(plugin.Deps)...)
	}
}

func recalcProtoOutputs(d *ModuleData) {
	d.grpcOutputsBeforeBase = d.grpc

	for i := range d.cppProtoPlugins {
		d.cppProtoPlugins[i].OutputsBeforeBase = true
	}
}

func pythonProtoPeerContext(d *ModuleData) bool {
	return isPythonProtoModule(d) || isPythonModuleType(d.unit.Type)
}

func protoCmdPeers(d *ModuleData) []ANY {
	if pythonProtoPeerContext(d) {
		return nil
	}

	front := make([]ANY, 0, len(d.cppProtoPlugins))

	if d.grpc {
		front = append(front, strContribLibsGrpc.any())
	}

	for _, plugin := range d.cppProtoPlugins {
		for _, dep := range plugin.Deps {
			front = append(front, internStr(dep).any())
		}
	}

	return front
}

type TestMeta struct {
	dockerImages    []string
	commands        string
	testFiles       []string
	depends         []string
	recipes         []string
	binaryArgs      []string
	fuzzDicts       []string
	forkFiles       bool
	explicitData    bool
	size            string
	timeout         int
	tags            []string
	requirements    []string
	env             []string
	data            []string
	cwd             string
	canonizeSubPath string
	forkMode        string
	partition       string
	splitFactor     int
	validateData    bool
}

type ModuleData struct {
	recurses    []string
	mlaunch     *MlaunchModule
	ros         *RosModule
	multimodule TOK
	selfPeers   []Language
	rosInclude  []VFS

	configurationDepends []string

	discarded bool

	confMacroDepth int
	sourceEnv      EnvStore

	styleChecks []StyleCheck

	usrvFiles []UsrvFile

	yqlPrebuiltTool         bool
	yqlUdfProbe             bool
	yqlUdfShared            bool
	yqlPythonUdf            bool
	yqlCurrent              bool
	dynamicLink             bool
	macroCalls              []UnknownStmt
	yqlPrebuiltResource     string
	yqlPrebuiltAliases      bool
	yqlSourceSrcs           []ANY
	yqlSourcePeers          []ANY
	macroPeerdirs           []ANY
	moduleVariants          uint16
	listProtos              []ListProtoStmt
	nots                    *NotsModule
	docs                    *DocsData
	moduleStmt              *ModuleStmt
	moduleEnded             bool
	moduleOpening           bool
	readModuleContentOnly   bool
	readModuleContentDone   bool
	autoincluded            bool
	modver                  string
	hasLicense              bool
	goTestSrcs              []ANY
	goXTestSrcs             []ANY
	goFmtSrcs               []ANY
	goSkipTests             []ANY
	gettextKeepPath         bool
	noLint                  string
	pythonLint              pythonLintState
	test                    TestMeta
	mockgen                 GoMockgenVars
	goCodegens              []GoCodegenStmt
	userverProtos           []UserverProtoBatch
	goEmbeds                []GoEmbed
	goPluginPeers           []ANY
	goProtoOpts             []GoProtoOpt
	cgoSrcs                 []ANY
	cgoLdflags              []ANY
	goLdflags               []ANY
	goLinkFlags             []ANY
	cgoCflags               []ANY
	toolchainName           string
	srcs                    []SrcMeta
	pySrcs                  []ANY
	pySrcGroups             []PySrcGroup
	pyGroupScopes           []PyScope
	pyGroupSrcs             []DeclaredSource
	pyPyiResources          []ResourceEntry
	resKeyBuf               []byte
	pyBuildNoPYC            bool
	pyBuildNoPY             bool
	pyTopLevel              bool
	pyYapycSuffix           string
	noExtendedPySearch      bool
	enumSrcs                []*GenerateEnumSerializationStmt
	peerdirs                []ANY
	explicitPeers           []ANY
	unitTestPeerExplicit    bool
	resourcePeerDeferred    bool
	protoCmdPeers           []ANY
	joinSrcs                []*JoinSrcsStmt
	addIncl                 []VFS
	addInclP                []PrioVFS
	addInclGlobal           []VFS
	addInclOneLevel         []VFS
	addInclUserGlobal       []VFS
	cfAddIncl               []VFS
	cfAddInclGlobal         []VFS
	cythonAddIncl           []VFS
	cythonAddInclGlobal     []VFS
	useNasm                 bool
	clangSAEnable           bool
	clangSAConfig           string
	clangSAPlugins          []ANY
	nasmFlags               []ANY
	yasmFlags               []ANY
	yasmPlatformFlags       []ANY
	asmFormat               STR
	asmPreincludes          []ANY
	asmAddIncl              []VFS
	protoAddInclGlobal      []VFS
	protoAddIncl            []VFS
	protoIncludeLocal       []VFS
	dontResolveIncludes     bool
	ifTaken                 []bool
	ifReplay                []bool
	swigAddIncl             []VFS
	swigAddInclGlobal       []VFS
	scAddIncl               []VFS
	scAddInclGlobal         []VFS
	scAddInclExport         []VFS
	llvmBc                  []*LlvmBcStmt
	llvmCompile             []LlvmCompileStmt
	luaCompile              []LuaCompileStmt
	cFlags                  []ANY
	cFlagsGlobal            []ANY
	compilerFlagUses        [3]CompilerFlagResourceUses
	cxxFlags                []ANY
	cxxFlagsVar             []ANY
	cxxFlagsGlobal          []ANY
	cOnlyFlags              []ANY
	cOnlyFlagsGlobal        []ANY
	sFlags                  []ANY
	protocFlags             []ANY
	fbsNamespaces           []ANY
	flatcAddIncl            []VFS
	flatcAddInclGlobal      []VFS
	clangWarnings           []ANY
	ldFlags                 []ANY
	localLDFlags            LocalLinkerFlags
	localLDFlagsReady       bool
	rpathFlagsGlobal        []ANY
	objAddLibsGlobal        []ANY
	srcDirs                 []VFS
	flags                   FlagSet
	muslLite                bool
	cudaSrcs                []CudaSrcsStmt
	nvccDeviceSrcs          []ANY
	cuda                    CudaSettings
	implibs                 []ImplibStmt
	linkExcludeLibraries    []ANY
	useArcadiaLibm          bool
	useDynamicCuda          bool
	splitDwarf              bool
	peerRequest             PeerRequest
	unionExplicitOutputs    []ANY
	resourcePeerOverride    STR
	resourcePeerExplicit    bool
	resourcePeerClosed      bool
	resourcePassPeers       bool
	usePython2              bool
	usePython3              bool
	pythonSourceCallback    bool
	pythonSourceEntry       bool
	pythonSourceProgram     bool
	useLightPy2cc           bool
	useCommonGoogleAPIs     bool
	moduleScopeCFlags       []ANY
	pythonSQLite3           bool
	pyNamespace             *ANY
	pyNamespacePython2      bool
	protoNamespace          *ANY
	ymapsSprotoSrcs         []DeclaredToken
	pyProtoMypy             bool
	noOptimize              bool
	optimizePyProtos        bool
	ignoreCppProtoSelfPeers bool
	needGoogleProtoPeerdirs bool
	cppProtoPlugins         []CppProtoPlugin
	pyProtoPlugins          []PyProtoPlugin
	grpcOutputIndex         int
	grpcOutputsBeforeBase   bool
	excludeTags             map[STR]bool
	onlyTags                bool
	exportsScript           *ANY
	linkerScripts           []ANY
	ldPlugins               []ANY
	arPlugin                *ANY
	hasFbs                  bool
	hasFbs64                bool
	defaultVars             map[STR]STR
	configureFiles          []*ConfigureFileStmt
	createBuildInfoFor      *ANY
	antlr4Grammars          []Antlr4GrammarInfo
	antlrRuns               []AntlrRunInfo
	runPrograms             []*RunProgramStmt
	runJavaPrograms         []NativeJavaProgram
	runJavaJdk              string
	decimalMD5              []*DecimalMD5Lower32BitsStmt
	buildMns                []*BuildMnStmt
	splitCodegens           []*SplitCodegenStmt
	baseCodegens            []*BaseCodegenStmt
	proto2Fbs               []Proto2FbsStmt
	runPython               []*RunPythonStmt
	fromSandboxes           []*FromSandboxStmt
	fromArchives            []FromArchiveEntry
	sdcMetas                []SdcMetaEntry
	sdcInstallOutputs       []VFS
	pireInline              []DeclaredToken
	pchFiles                []PchFileStmt
	checkConfigHeaders      []DeclaredToken
	cythonOptions           []ANY
	cythonCpp               []*CythonStmt
	swigC                   []SwigSrc
	bisonGenExt             STR
	grpc                    bool
	yaConfJSON              []ANY
	yaConfDeclSeq           int
	archives                []ArchiveEntry
	archiveAsm              []ArchiveAsmEntry
	lj21                    *Lj21Archive
	copyFiles               []CopyFileEntry
	declSeq                 int
	resources               []ResourceEntry
	bundles                 []BundleEntry
	pyMain                  *ANY
	pyMainDeclSeq           int
	noFixElf                bool
	noStrip                 bool
	strip                   bool
	unit                    ModuleUnit
	importMask              pythonImportMask
	noCheckImports          []ANY
	noCheckImportsDisabled  bool
	pyRegister              []PyRegistration
	ragel6Flags             []ANY
	ragel6Builds            []Ragel6BuildStmt
	conflictMod             *ModuleStmt
	resourceDeclStmts       []*DeclareResourceStmt
	primaryOutput           string
	inducedDeps             ParsedIncludeSet
	setVars                 SetVars
	confModule              *confModule
	consumerTags            STR
	pending                 []PendingStmt
	interpreting            bool
	varMacrosApplied        []STR
	guards                  []STR
	startPeers              int
	tc                      ModuleToolchain
	scanCtx                 *ScanContext
	cc                      ModuleCompileEnv
}

type DeclaredToken struct {
	Value   ANY
	DeclSeq int
}

type PyRegistration struct {
	Name        STR
	RenameInit  bool
	CFlagsStart int
	DeclSeq     int
}

type CompileSpec struct {
	PyRegisterLimit int
	CFlags          []ANY
	FlatOutput      bool
	OutputStem      VFS
	Variant         STR
	ObjSuf          STR
	ForceCxx        bool
	EnvAddIncl      []VFS
	EnvCFlags       []ANY
}

type SrcMeta struct {
	Resolved    ResolvedSource
	Source      ANY
	Prio        int
	Seq         int
	Stmt        int
	Global      bool
	RosZeroCopy bool
	Auto        bool
	Compile     CompileSpec
	CompileRef  NodeRef
	PyMeta      uint32
}

func (d *ModuleData) nextDeclSeq() int {
	d.declSeq++

	return d.declSeq
}

type BundleEntry struct {
	DeclSeq int
	Target  string
	Name    string
	Suffix  string
	Output  string
}

type ResourceEntry struct {
	Resolved     ResolvedSource
	DeclSeq      int
	Path         string
	Key          string
	SrcPath      string
	EndsBatch    bool
	DontParse    bool
	DontCompress bool
}

func (d *ModuleData) resStr2(a, b string) string {
	start := len(d.resKeyBuf)

	d.resKeyBuf = append(d.resKeyBuf, a...)
	d.resKeyBuf = append(d.resKeyBuf, b...)

	return bytesString(d.resKeyBuf[start:])
}

func (d *ModuleData) resStr4(a, b, c, e string) string {
	start := len(d.resKeyBuf)

	d.resKeyBuf = append(d.resKeyBuf, a...)
	d.resKeyBuf = append(d.resKeyBuf, b...)
	d.resKeyBuf = append(d.resKeyBuf, c...)
	d.resKeyBuf = append(d.resKeyBuf, e...)

	return bytesString(d.resKeyBuf[start:])
}

type PyFbsSource struct {
	Path   string
	Module string
}

type PySrcGroup struct {
	Lint           bool
	SourceRoot     bool
	DeclSeq        int
	Fbs            []PyFbsSource
	Srcs           []DeclaredSource
	Scopes         []PyScope
	ResourceOffset int
	Prio           int
	PyiStart       int
	PyiEnd         int
	Statement      *UnknownStmt
}

type PyScope struct {
	TopLevel   bool
	Namespace  *ANY
	ModuleName *ANY
}

func (s PyScope) moduleName(modulePath, src string) string {
	if s.ModuleName != nil {
		return s.ModuleName.string()
	}

	return pythonModuleName(modulePath, src, s.TopLevel, s.Namespace)
}

type ArchiveEntry struct {
	DeclSeq      int
	Name         string
	DontCompress bool
	Files        []string
	Keys         []string
}

type ArchiveAsmEntry struct {
	Name         string
	DontCompress bool
	Files        []string
	DeclSeq      int
}

type Lj21Archive struct {
	Luas []string
}

type CopyFileEntry struct {
	Resolved       ResolvedSource
	DeclSeq        int
	Src            string
	Dst            string
	Auto           bool
	WithContext    bool
	OutputIncludes []string
}

type Antlr4GrammarInfo struct {
	DeclSeq        int
	IsSplit        bool
	Lexer          string
	Parser         string
	Grammar        string
	Options        []string
	Visitor        bool
	Listener       bool
	OutputIncludes []string
}

type AntlrRunInfo struct {
	DeclSeq        int
	Python         bool
	Jar            ANY
	Macro          string
	Args           []ANY
	INFiles        []ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	CWD            *ANY
	OutputIncludes []ANY
}

func parseCopyFileEntry(args []string, withContext bool, line int) CopyFileEntry {
	entry := CopyFileEntry{WithContext: withContext}
	positional := 0
	section := ""

	for _, arg := range args {
		switch arg {
		case "AUTO":
			entry.Auto = true
			section = ""

			continue
		case "TEXT":
			section = ""

			continue
		case "OUTPUT_INCLUDES", "INDUCED_DEPS":
			section = arg

			continue
		}

		switch {
		case section != "":
			entry.OutputIncludes = append(entry.OutputIncludes, arg)
		case positional == 0:
			entry.Src = arg
		case positional == 1:
			entry.Dst = arg
		}

		if section == "" {
			positional++
		}
	}

	if positional < 2 {
		throwFmt("gen: COPY_FILE at line %d expects at least source and destination, got %d args", line, len(args))
	}

	return entry
}

func sourceInputVFS(fs FS, moduleDir VFS, path string) (VFS, bool) {
	modulePath := moduleDir.relString()

	if vfs, ok := moduleRootedVFS(modulePath, path); ok {
		return vfs, true
	}

	clean := path

	if clean == "" || !pathIsClean(clean) {
		clean = filepath.ToSlash(filepath.Clean(clean))
	}

	if clean == "." || clean == "" {
		return moduleDir, true
	}

	if clean == modulePath ||
		(len(clean) > len(modulePath) && clean[len(modulePath)] == '/' && clean[:len(modulePath)] == modulePath) {
		return source(clean), true
	}

	if fs != nil {
		if fs.isFile(moduleDir.rel(), clean) {
			if strings.Contains(clean, "..") {
				return source(filepath.ToSlash(filepath.Clean(modulePath + "/" + clean))), true
			}

			if modulePath == "." {
				return source(clean), true
			}

			return source(modulePath, "/", clean), true
		}

		if fs.isFile(srcRootRel, clean) {
			return source(clean), true
		}
	}

	return 0, false
}

func copyFileInputVFS(fs FS, moduleDir VFS, src string) VFS {
	if vfs, ok := sourceInputVFS(fs, moduleDir, src); ok {
		return vfs
	}

	return sourceJoinClean(moduleDir.relString(), src)
}

func moduleInputCandidateVFS(modulePath string, path string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, path); ok {
		return vfs
	}

	return buildJoinClean(modulePath, path)
}

func moduleRootedVFS(modulePath string, path string) (VFS, bool) {
	if vfsHasPrefix(path) {
		return internClean(path), true
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}"):
		return sourceClean(strings.TrimLeft(strings.TrimPrefix(path, "${ARCADIA_ROOT}"), "/")), true
	case strings.HasPrefix(path, "${CURDIR}"):
		return sourceJoinClean(modulePath, strings.TrimLeft(strings.TrimPrefix(path, "${CURDIR}"), "/")), true
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}"):
		return buildClean(strings.TrimLeft(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}"), "/")), true
	case strings.HasPrefix(path, "${BINDIR}"):
		return buildJoinClean(modulePath, strings.TrimLeft(strings.TrimPrefix(path, "${BINDIR}"), "/")), true
	default:
		return 0, false
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, dst); ok {
		return buildClean(vfs.relString())
	}

	return buildJoinClean(modulePath, dst)
}

func resourceOutputVFS(modulePath string, path string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, path); ok {
		return vfs
	}

	clean := filepath.ToSlash(filepath.Clean(path))

	if clean == modulePath || strings.HasPrefix(clean, modulePath+"/") {
		return build(clean)
	}

	return buildJoinClean(modulePath, clean)
}

func copyFileIncludeTarget(modulePath string, target string) string {
	if path, ok := moduleRootedVFS(modulePath, target); ok {
		return path.relString()
	}

	return target
}

type PrioVFS struct {
	prio int
	vfs  VFS
}

func (d *ModuleData) addLocalIncl(prio int, v VFS) {
	d.addInclP = append(d.addInclP, PrioVFS{prio: prio, vfs: v})
}

func (d *ModuleData) materializeAddIncl() {
	slices.SortStableFunc(d.addInclP, func(a, b PrioVFS) int {
		return cmp.Compare(a.prio, b.prio)
	})

	for _, p := range d.addInclP {
		d.addIncl = append(d.addIncl, p.vfs)

		if p.prio == prioOutputAddIncl {
			d.addInclGlobal = append(d.addInclGlobal, p.vfs)
			d.addInclUserGlobal = append(d.addInclUserGlobal, p.vfs)
		}
	}

	d.addInclP = d.addInclP[:0]
}

func expandRunPythonStmt(stmt *RunPythonStmt, env Environment) *RunPythonStmt {
	expanded := *stmt

	expanded.ScriptPath = expandStmtTokenAny(stmt.ScriptPath, env)
	expanded.Args = expandCommandTokens(stmt.Args, env)
	expanded.INFiles, expanded.INNoParse = expandInFiles(stmt.INFiles, stmt.INNoParse, env, false)
	expanded.OUTFiles = expandStmtTokens(stmt.OUTFiles, env)
	expanded.OUTNoAutoFiles = expandStmtTokens(stmt.OUTNoAutoFiles, env)
	expanded.EnvPairs = expandStmtTokens(stmt.EnvPairs, env)
	expanded.ToolPaths = expandStmtTokens(stmt.ToolPaths, env)
	expanded.OutputIncludes = expandStmtTokens(stmt.OutputIncludes, env)
	expanded.InducedDeps = expandStmtTokens(stmt.InducedDeps, env)

	if stmt.StdoutFile != nil {
		stdout := expandStmtTokenAny((*stmt.StdoutFile), env)

		expanded.StdoutFile = &stdout
	}

	if stmt.CWD != nil {
		cwd := expandStmtTokenAny(*stmt.CWD, env)

		expanded.CWD = &cwd
	}

	return &expanded
}

func collectModuleInto(ctx *GenCtx, instance ModuleInstance, stmts []Stmt, env Environment, onWarn func(Warn), d *ModuleData, replay []bool) *ModuleData {
	pm := ctx.parsers
	fs := ctx.fs
	modulePath := instance.Path.relString()
	kind := instance.Kind
	modulePathID := instance.Path.rel()

	env.setStrID(envMODDIR, modulePathID)
	env.setVFS(envCURDIR, modulePathID.source())
	env.setVFS(envBINDIR, modulePathID.build())
	env.setVFS(envCmakeCurrentBinaryDir, modulePathID.build())
	env.setDefaultString(envCythonOptions, cythonDefaultOptions(instance.Platform))
	env.setDefaultString(envProtobufIncludePath, pbRuntimeBaseString)
	env.setDefaultString(envPyProtoMypyEnabled, "yes")

	d.reset()

	d.ifReplay = replay
	d.pythonSQLite3 = true
	d.needGoogleProtoPeerdirs = true
	d.yqlCurrent = instance.YqlCurrent
	initPythonImportMask(env, d)

	collectStmts(ctx, modulePath, kind, instance.Language, stmts, env, d, onWarn, instance.Platform)

	if d.moduleStmt != nil && !d.discarded && !d.moduleEnded {
		throwFmt("%s: module declaration has no END()", modulePath)
	}

	d.finishResourcePeerPolicy()

	if pd := programSourceDir(d.moduleStmt); pd != nil {
		d.srcDirs = slices.Insert(d.srcDirs, 0, dirKey(*pd).source())
	}

	kindOfResolve, _ := env.s.lookup(envDontResolveIncludes)

	d.dontResolveIncludes = kindOfResolve != envAbsent && env.string(envDontResolveIncludes) != "no"
	finishYqlModule(instance, env, d)

	if d.unit.Type == tokCrate && !envRustHost(env) && env.string(envSANITIZER_TYPE) == "memory" {
		d.peerdirs = append(d.peerdirs, internAny("contrib/restricted/rust_msan"))
	}

	d.peerdirs = append(d.peerdirs, d.macroPeerdirs...)
	d.pyProtoMypy = env.string(envPyProtoMypyEnabled) == "yes" &&
		(isPythonProtoModule(d) || d.unit.Type == tokPy2Library || d.unit.Type == tokPy3Library || d.unit.Type == tokPy23Library)
	d.flags.NoCompilerWarnings = env.bool(envNoCompilerWarnings)
	d.noLint = env.string(envNoLintValue)
	d.useCommonGoogleAPIs = env.string(envCommonGoogleApis) != "None"
	d.dynamicLink = env.bool(envDynamicLink)

	if env.string(envYqlAbiPeerdirTag) == "YQL_ABI_CURRENT" {
		d.yqlCurrent = true
	}

	if value := env.string(envPyNamespaceValue); value != "" && d.pyNamespace == nil {
		d.pyNamespace = ptr(internStr(value).any())
	}

	nativeSources := d.srcs[:0]

	for _, meta := range d.srcs {
		sourceClass := srcExtClassOf(meta.Source)

		if sourceClass == srcExtCSource {
			if rel, rooted := strings.CutPrefix(meta.Source.string(), "$S/"); rooted {
				meta.Source = sourceClean(rel).any()
			}

			if meta.Compile.FlatOutput && meta.Compile.OutputStem == 0 {
				meta.Compile.OutputStem = copyFileOutputVFS(modulePath, meta.Source.string())
			}
		}

		if extIsPyx(meta.Source.relOrSelf().string()) {
			native := meta

			d.cythonCpp = append(d.cythonCpp, &CythonStmt{Src: meta.Source.string(), Seq: declSeqModuleEnd | meta.Seq, Native: &native, Python3: env.bool(envPYTHON3)})
		} else {
			nativeSources = append(nativeSources, meta)
		}
	}

	d.srcs = nativeSources
	finishPythonImportMask(env, d)

	bindRunProgramCalls(d.runPrograms, env, &d.sourceEnv)

	if len(d.runJavaPrograms) > 0 {
		d.runJavaJdk = env.string(envJdkRealVersion)
	}

	d.test.expandValues(env)

	if !d.pythonLint.bound {
		d.pythonLint.bind(env)
	}

	last := d.declSeq

	for _, priority := range []int{stmtPrioDefault, stmtPrioSrcs} {
		for i, group := range d.pySrcGroups {
			if group.Prio == priority && group.Statement != nil && group.Statement.Name == tokPySrcs {
				d.declSeq = declSeqModuleEnd | group.DeclSeq
				collectPySrcGroup(fs, modulePath, *group.Statement, d, env, onWarn, i)
				d.pySrcGroups[i].Statement = nil
			}
		}
	}

	d.declSeq = last

	finishPyRegisterCFlags(d)

	d.test.canonizeSubPath = env.string(envCanonizeSubPath)
	d.test.validateData = resourceValidationEnabled(env)

	if len(d.cythonCpp) > 0 {
		d.cythonOptions = internAnys(strings.Fields(env.string(envCythonOptions)))
	}

	d.collectPythonStyleChecks(env)

	if d.nots != nil {
		return d
	}

	if len(d.pySrcs) > 0 || len(d.swigC) > 0 {
		d.pyYapycSuffix = pySrcYapycSuffix(modulePath)
	}

	if d.docs != nil {
		d.docs.finish(fs, modulePath, env)
	}

	sourcePeers, sourceAddIncl := d.sourceCommandDirs(modulePath, env, instance.Platform)

	if len(sourceAddIncl) > 0 {
		collectStmts(ctx, modulePath, instance.Kind, instance.Language, []Stmt{splitAddInclPaths(internAnys(sourceAddIncl))}, env, d, onWarn, instance.Platform)
	}

	d.materializeAddIncl()

	srcDirs := append(d.srcDirs, 0)

	copy(srcDirs[1:], srcDirs[:len(srcDirs)-1])

	srcDirs[0] = instance.Path

	d.srcDirs = srcDirs
	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = d.cfAddIncl[:0]
	d.cfAddInclGlobal = d.cfAddInclGlobal[:0]

	filterInvalidAddIncl(fs, d, modulePath, onWarn)

	if d.unit.Tag == unitTagPy3BinLib {
		d.pyMain = nil
	}

	d.useDynamicCuda = env.bool(envUseDynamicCuda)
	d.useArcadiaLibm = env.bool(envUSE_ARCADIA_LIBM) && !env.bool(envOS_EMSCRIPTEN)
	d.clangSAEnable = env.bool(envClangSAEnable)
	d.clangSAConfig = env.string(envClangSAConfig)
	d.clangSAPlugins = internAnys(strings.Fields(env.string(envClangSAPlugins)))

	if d.clangSAEnable && d.clangSAConfig != "" {
		d.peerdirs = append(d.peerdirs, clangSAPeer.any())
	}

	if d.multimodule == tokMlaunchPipeline && d.unit.Type == tokProgram && env.string(envSdcPlatform) == "" {
		env.setBool(envSTRIP, true)
	}

	d.strip = env.string(envSTRIP) == "yes"
	d.noStrip = env.string(envNO_STRIP) == "yes"
	d.noFixElf = env.string(envNoFixElf) == "yes"
	d.gettextKeepPath = env.string(envGettextKeepPath) == "yes"
	d.finishNativeSwigConfig(env)
	d.useNasm = env.bool(envUseNasmAssembler)
	d.collectAssemblerConfig(env)

	if d.muslLite {
		env.setBool(envNoutil, true)
		instance.Platform.conf.set(env, envNoutil, &d.setVars)
	}

	d.flags.NoUtil = env.bool(envNoutil)
	d.flags.NoRuntime = env.bool(envNoruntime)
	d.flags.NoLibc = env.bool(envNolibc)
	d.flags.NoPlatform = env.bool(envNoplatform)
	d.flags.NoWShadow = env.bool(envNoWshadow)
	d.flags.NoExportDynSymbols = env.bool(envNoExportDynamicSymbols)
	d.noExtendedPySearch = env.string(envNoExtendedSourceSearch) == "yes"
	d.noOptimize = env.bool(envNoOptimize)
	d.primaryOutput = env.string(envPrimaryOutputValue)

	if env.hasBindingID(envModver) {
		if version := env.string(envModver); strings.IndexFunc(version, unicode.IsSpace) < 0 {
			d.modver = version
		} else {
			d.modver = strings.Join(strings.Fields(version), ".")
		}
	}

	d.bisonGenExt = internStr(env.string(envBisonGenExt))

	if plugin := env.string(envArPlugin); plugin != "" {
		d.arPlugin = ptr(internStr(plugin).any())
	}

	if env.bool(envPY3_PROTO) && instance.Language == LangPy {
		d.usePython3 = true
	}

	applyArchiveAddIncl(modulePath, d)
	applyCythonHeaderAddIncl(modulePath, d)

	d.moduleScopeCFlags = moduleCFlagsTail(env, instance.Platform)

	if len(d.addIncl) > 1 || len(d.addInclGlobal) > 1 {
		dedupers.with(func(deduper *DeDuper) {
			if len(d.addIncl) > 1 {
				d.addIncl = dedupInPlaceWith(deduper, d.addIncl)
			}

			if len(d.addInclGlobal) > 1 {
				d.addInclGlobal = dedupInPlaceWith(deduper, d.addInclGlobal)
			}
		})
	}

	for _, a := range d.addIncl {
		pm.indexAddincl(a)
	}

	for _, a := range d.addInclGlobal {
		pm.indexAddincl(a)
	}

	hasProto := false
	hasPyGztProto := false

	for i, meta := range d.srcs {
		src := meta.Source

		if isPythonProtoModule(d) && extIsGztproto(src.string()) {
			src = gztProtoOutput(resolveSourceOrGeneratedVFS(ctx, instance, src.string(), d.srcDirs)).any()
			d.srcs[i].Source = src
			hasPyGztProto = true
		}

		switch srcExtClassOf(src) {
		case srcExtProto:
			hasProto = true
		case srcExtFbs:
			d.hasFbs = true
		case srcExtFbs64:
			d.hasFbs64 = true
		}
	}

	hasProto = hasProto || hasGeneratedSourceClass(d, srcExtProto)

	d.peerdirs = append(d.peerdirs, sourcePeers...)
	d.optimizePyProtos = env.string(envOptimizePyProtosFlag) == "yes"
	d.ignoreCppProtoSelfPeers = env.string(envIgnoreCppProtoSelfPeers) == "yes"

	if hasPyGztProto && d.optimizePyProtos {
		d.peerdirs = append(d.peerdirs, strKernelGazetteerProto.any())
	}

	if d.moduleStmt != nil && (d.moduleStmt.Name == tokProtoLibrary || hasProto) {
		d.protoCmdPeers = protoCmdPeers(d)
	}

	if d.resourcePeerDeferred || len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	return d
}

func (d *ModuleData) sourceCommandDirs(modulePath string, env Environment, platform *Platform) (peers []ANY, addincls []string) {
	if isGoModuleType(d.unit.Type) {
		return nil, nil
	}

	var seen []string

	apply := func(source ANY) {
		ext := strings.TrimPrefix(filepath.Ext(source.string()), ".")

		if ext == "" || slices.Contains(seen, ext) {
			return
		}

		seen = append(seen, ext)

		dirs := platform.conf.commandDirs("_SRC:"+ext, env)

		for _, peer := range dirs.peerdirs {
			if peer != modulePath {
				peers = append(peers, internStr(peer).any())
			}
		}

		addincls = append(addincls, dirs.addincls...)
	}

	generated := func(outputs []ANY, stdout *ANY, noAuto bool) {
		for _, out := range outputs {
			apply(out)
		}

		if stdout != nil && !noAuto {
			apply(*stdout)
		}
	}

	for _, stmt := range d.runPrograms {
		generated(stmt.OUTFiles, stmt.StdoutFile, stmt.StdoutNoAuto)
	}

	for _, stmt := range d.runPython {
		generated(stmt.OUTFiles, stmt.StdoutFile, stmt.StdoutNoAuto)
	}

	for _, stmt := range d.fromArchives {
		generated(stmt.OUTFiles, nil, true)
	}

	for _, meta := range d.srcs {
		apply(meta.Source)
	}

	return peers, addincls
}

func applyResourceFiles(fs FS, modulePath string, args []string, line int, d *ModuleData) {
	if isGoModuleType(d.unit.Type) {
		applyGoResource(fs, modulePath, expandResourceFiles(d, nil, args), d)

		return
	}

	d.resourcePeerDeferred = true

	before := len(d.resources)

	d.resources = expandResourceFiles(d, d.resources, args)

	seq := d.nextDeclSeq()

	for i := before; i < len(d.resources); i++ {
		d.resources[i].DeclSeq = seq
	}

	if len(d.resources) > before {
		d.resources[len(d.resources)-1].EndsBatch = true
	}
}

func ensureResourcePeer(modulePath string, d *ModuleData) {
	const resourcePeer = "library/cpp/resource"

	if modulePath == resourcePeer {
		return
	}

	for _, p := range d.peerdirs {
		if p.string() == resourcePeer {
			return
		}
	}

	d.peerdirs = append(d.peerdirs, internStr(resourcePeer).any())
}

func filterInvalidAddIncl(fs FS, d *ModuleData, modulePath string, onWarn func(Warn)) {
	d.addIncl = filterOwnAddIncl(fs, d.addIncl, modulePath, onWarn)
	d.addInclGlobal = filterOwnAddIncl(fs, d.addInclGlobal, modulePath, onWarn)
	d.cythonAddIncl = filterOwnAddIncl(fs, d.cythonAddIncl, modulePath, onWarn)
	d.asmAddIncl = filterOwnAddIncl(fs, d.asmAddIncl, modulePath, onWarn)

	if len(d.cythonAddInclGlobal) == 0 && len(d.addInclUserGlobal) == 0 {
		return
	}

	dedupers.with(func(dd *DeDuper) {
		if len(d.cythonAddInclGlobal) > 0 {
			dd.reset()

			for _, p := range d.cythonAddIncl {
				dd.add(p.strID())
			}

			out := d.cythonAddInclGlobal[:0]

			for _, p := range d.cythonAddInclGlobal {
				if dd.has(p.strID()) {
					out = append(out, p)
				}
			}

			d.cythonAddInclGlobal = out
		}

		if len(d.addInclUserGlobal) > 0 {
			dd.reset()

			for _, p := range d.addInclGlobal {
				dd.add(p.strID())
			}

			for _, p := range d.addInclOneLevel {
				dd.add(p.strID())
			}

			out := d.addInclUserGlobal[:0]

			for _, p := range d.addInclUserGlobal {
				if dd.has(p.strID()) {
					out = append(out, p)
				}
			}

			d.addInclUserGlobal = out
		}
	})
}

func filterOwnAddIncl(fs FS, paths []VFS, modulePath string, onWarn func(Warn)) []VFS {
	out := paths
	copied := false

	for i, path := range paths {
		if shouldCheckSourceDir(path) && !fs.isDir(path.rel(), "") {
			onWarn(Warn{
				Kind:    WarnMissingAddincl,
				Message: fmt.Sprintf("%s: ADDINCL to non existent source directory %s", modulePath, path.relString()),
			})

			if !copied {
				out = append([]VFS(nil), paths[:i]...)
				copied = true
			}

			continue
		}

		if copied {
			out = append(out, path)
		}
	}

	return out
}

func shouldCheckSourceDir(path VFS) bool {
	if !path.isSource() {
		return false
	}

	if path.relString() == "" {
		return false
	}

	if strings.Contains(path.relString(), "$") {
		return false
	}

	return true
}

func (d *ModuleData) syncPythonVersion(env Environment) {
	switch {
	case env.string(envPYTHON2) == "yes":
		d.usePython2, d.usePython3 = true, false
	case env.string(envPYTHON3) == "yes":
		d.usePython2, d.usePython3 = false, true
	}
}

func usePython2(env Environment, d *ModuleData, moduleTag STR) {
	d.usePython2 = true

	env.setBool(envPYTHON2, true)
	env.setBool(envPYTHON3, false)
	env.setStringID(envMODULE_TAG, moduleTag)
}

func moduleCFlagsTail(env Environment, p *Platform) []ANY {
	module := env.string(envCflags)
	tail, ok := strings.CutPrefix(module, p.ifEnv.string(envCflags))

	if !ok {
		throwFmt("module CFLAGS %q does not extend the platform value", module)
	}

	if tail == "" {
		return nil
	}

	key := internStr(tail)

	if memo, ok := p.cflagsTails[key]; ok && memo.holds(env) {
		return memo.result
	}

	memo := &confExpansionList{result: expandCompilerConfFlags(env, tail)}
	seen := map[ENV]bool{}

	var visit func(text string)

	visit = func(text string) {
		confValueVars(text, func(name string) {
			k := internEnv(name)

			if seen[k] {
				return
			}

			seen[k] = true

			v := confEnvValue(env, k)

			memo.keys = append(memo.keys, k)
			memo.values = append(memo.values, v)

			if v.kind != envAbsent {
				visit(v.value.string())
			}
		})
	}

	visit(tail)

	if p.cflagsTails == nil {
		p.cflagsTails = map[STR]*confExpansionList{}
	}

	p.cflagsTails[key] = memo

	return memo.result
}

type confExpansionList struct {
	keys   []ENV
	values []ConfValue
	result []ANY
}

func (m *confExpansionList) holds(env Environment) bool {
	for i, k := range m.keys {
		if confEnvValue(env, k) != m.values[i] {
			return false
		}
	}

	return true
}

func applyArchiveAddIncl(modulePath string, d *ModuleData) {
	for _, a := range d.archives {
		include := build(generatedIncludeDir(modulePath, a.Name))

		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	}

	if d.lj21 != nil {
		include := build(modulePath)

		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	}
}

func applyCythonHeaderAddIncl(modulePath string, d *ModuleData) {
	for _, stmt := range d.cythonCpp {
		if !stmt.Header {
			continue
		}

		dir := build(pathDir(modulePath + "/" + relStem(stmt.Src)))

		d.addIncl = append(d.addIncl, dir)
		d.addInclGlobal = append(d.addInclGlobal, dir)
		d.addInclUserGlobal = append(d.addInclUserGlobal, dir)
	}
}

func pyModuleTypeUsesPython3(name TOK) bool {
	switch name {
	case tokPy3Library, tokPy3Program, tokPy3ProgramBin, tokPy3Test,
		tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}

func moduleConfTag(owner STR, kind ModuleKind, language Language, d *ModuleData) string {
	if sub := multimoduleSub(owner, kind, language, d.yqlCurrent); sub != 0 {
		return sub.string()
	}

	return d.unit.Tag.string()
}

func collectModuleAutoinclude(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	if d.autoincluded || d.moduleStmt == nil {
		return
	}

	d.autoincluded = true
	collectStmts(ctx, modulePath, kind, language, ctx.autoincludeStmts(modulePath), env, d, onWarn, platform)
}

func (d *ModuleData) processModuleEnd(modulePath string, line int, onWarn func(Warn)) bool {
	if d.moduleStmt != nil && !d.moduleEnded && !(d.readModuleContentOnly && d.readModuleContentDone) {
		if d.readModuleContentOnly {
			d.readModuleContentDone = true

			return false
		}

		return true
	}

	onWarn(Warn{Kind: WarnMisconfiguration, Message: fmt.Sprintf("%s:%d: unexpected command END outside of a module", modulePath, line)})

	return false
}

type PendingStmt struct {
	key   uint64
	seq   int
	name  STR
	macro *ConfMacro
	stmt  Stmt
}

func statementPriority(name STR) uint64 {
	switch name {
	case kwPEERDIR, strGhostPeerdir, kwSRCDIR, strLateGlob:
		return 1
	case kwSRCS, strGoProcessSrcs, strPySrcs:
		return 4
	case strAddPyLinterCheck:
		return 5
	case strAddCompilableByk, strJavaModule, strJavaTest, strSetupExectest, strProcessDocs, strProcessDocslib:
		return 3
	}

	return 2
}

func multiStatement(name STR) bool {
	switch name {
	case strAllocator, kwINDUCED_DEPS, strGeneratePyProtos, strRunJavaProgram, strSplitCodegen, strCheckDependentDirs,
		kwDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE, strDeclareExternalHostResourcesPack, strLateGlob, strPeerQuery, strPeerExport:
		return true
	}

	return false
}

func (d *ModuleData) deferStmt(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, s Stmt, env Environment, onWarn func(Warn), platform *Platform) bool {
	if v, ok := s.(*UnknownStmt); ok {
		if policyDrops(d, v.macroName()) {
			return false
		}
	} else if dropsMacro(d, modulePath, stmtMacroNameID(s), onWarn) {
		return true
	}

	statement := platform.conf.statementKey(platform.conf.moduleAlias(d.confModule, stmtMacroNameID(s)))

	if statement.parse {
		d.deferGuard(platform, statement.name)

		return false
	}

	key := statement.prio << 32

	if statement.multi {
		key |= uint64(len(d.pending) + 1)
	}

	seq := d.declSeq

	d.declSeq += declSeqBlock

	d.pending = append(d.pending, PendingStmt{key: key, seq: seq, name: statement.name, macro: platform.conf.alias(statement.name), stmt: derefStmt(d.bindBody(ctx, modulePath, kind, language, s, env, onWarn, platform), env)})

	return true
}

func (d *ModuleData) bindBody(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, s Stmt, env Environment, onWarn func(Warn), platform *Platform) Stmt {
	switch v := s.(type) {
	case *UnknownStmt:
		if policyDrops(d, v.macroName()) {
			return s
		}

		if handledMacroTokens[v.Name] {
			d.expandBodyAtParse(ctx, modulePath, kind, language, v.Raw, expandStmtTokens(v.Args, env), v.Line, env, onWarn, platform)
		} else if name := platform.conf.moduleAlias(d.confModule, v.Raw); platform.conf.alias(name) != nil {
			return d.flattenConfMacro(ctx, modulePath, kind, language, v, name, expandStmtTokens(v.Args, env), env, onWarn, platform)
		}
	case *ResourceStmt:
		d.expandBodyAtParse(ctx, modulePath, kind, language, kwRESOURCE, nil, v.Line, env, func(Warn) {}, platform)
	case *GenerateEnumSerializationStmt:
		if !enumSerializationSkipped(d, language, env, v) {
			d.expandBodyAtParse(ctx, modulePath, kind, language, v.Macro, []ANY{internAny(v.Header)}, v.Line, env, func(Warn) {}, platform)
		}
	case *RunAntlrStmt:
		d.expandBodyAtParse(ctx, modulePath, kind, language, internStr(v.Macro), nil, v.Line, env, onWarn, platform)
	case *RunAntlr4CppStmt:
		d.expandBodyAtParse(ctx, modulePath, kind, language, kwRUN_ANTLR4_CPP, []ANY{v.Grammar}, v.Line, env, onWarn, platform)
	case *RunAntlr4CppSplitStmt:
		d.expandBodyAtParse(ctx, modulePath, kind, language, kwRUN_ANTLR4_CPP_SPLIT, []ANY{v.Lexer, v.Parser}, v.Line, env, onWarn, platform)
	case *RunAntlr4PythonStmt:
		d.expandBodyAtParse(ctx, modulePath, kind, language, internStr("RUN_ANTLR4_PYTHON"+v.Version), v.Args, v.Line, env, onWarn, platform)
	}

	return s
}

func (d *ModuleData) flattenConfMacro(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, v *UnknownStmt, name STR, args []ANY, env Environment, onWarn func(Warn), platform *Platform) *UnknownStmt {
	macro := platform.conf.alias(name)
	c := *v

	c.Expanded = true

	platform.conf.macroCalls(macro, UnknownStmt{Name: internTokMaybeSTR(name), Raw: name, Args: args, Line: v.Line}, env, &d.confMacroDepth, onWarn, modulePath, func(callee STR, args []ANY) {
		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], callee, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, callee.string()}, a...)...)
		})

		if baseMacro(callee) || dirStatement(callee) {
			markImplicitPeers(stmts)
			collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)

			return
		}

		d.deferGuard(platform, callee)

		for _, s := range stmts {
			c.Body = append(c.Body, d.bindBody(ctx, modulePath, kind, language, s, env, onWarn, platform))
		}
	})

	return &c
}

func (d *ModuleData) moduleBodyDirs(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, env Environment, onWarn func(Warn), platform *Platform) {
	if d.confModule == nil {
		return
	}

	for _, call := range d.confModule.dirs {
		if call.name == tokPyProgramPlugin.str() {
			d.deferStmt(ctx, modulePath, kind, language, &UnknownStmt{Name: tokPyProgramPlugin, Raw: call.name}, env, onWarn, platform)

			continue
		}

		text := call.args

		if strHasDollar(text.any()) {
			text = internStr(env.subst(text.string(), nil, nil))
		}

		d.bodyDirCall(ctx, modulePath, kind, language, call.name, call.macro, platform.conf.state.macroArgs(text), env, onWarn, platform)
	}
}

func (d *ModuleData) bodyDirCall(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, name STR, macro *ConfMacro, args []ANY, env Environment, onWarn func(Warn), platform *Platform) {
	if macro != nil {
		platform.conf.macroCalls(macro, UnknownStmt{Name: internTokMaybeSTR(name), Raw: name, Args: args}, env, &d.confMacroDepth, onWarn, modulePath, func(callee STR, args []ANY) {
			if callee == strGhostPeerdir || d.confModule.limits.ignored.has(callee) {
				return
			}

			if dirStatement(callee) {
				d.bodyDirCall(ctx, modulePath, kind, language, callee, nil, args, env, onWarn, platform)
			} else if nested := platform.conf.alias(callee); nested != nil && nested.dirs {
				d.bodyDirCall(ctx, modulePath, kind, language, callee, nested, args, env, onWarn, platform)
			}
		})

		return
	}

	var scratch [1]Stmt

	stmts := appendMacroCallStmts(scratch[:0], name, args, args, 0, func(format string, a ...any) {
		throwFmt("%s: %s: "+format, append([]any{modulePath, name.string()}, a...)...)
	})

	markImplicitPeers(stmts)
	collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
}

func (d *ModuleData) callConfMacro(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, call UnknownStmt, env Environment, onWarn func(Warn), platform *Platform) {
	if macro := platform.conf.alias(platform.conf.moduleAlias(d.confModule, call.Raw)); macro != nil {
		platform.conf.macroCalls(macro, call, env, &d.confMacroDepth, onWarn, modulePath, func(callee STR, args []ANY) {
			if !baseMacro(callee) && !dirStatement(callee) {
				d.callConfMacro(ctx, modulePath, kind, language, UnknownStmt{Name: internTokMaybeSTR(callee), Raw: callee, Args: args, Line: call.Line}, env, onWarn, platform)

				return
			}

			var scratch [1]Stmt

			stmts := appendMacroCallStmts(scratch[:0], callee, args, args, call.Line, func(format string, a ...any) {
				throwFmt("%s: %s: "+format, append([]any{macro.file, callee.string()}, a...)...)
			})

			markImplicitPeers(stmts)
			collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
		})

		platform.conf.setGuard(env, macro, &d.setVars)
	}

	applyUnknownStmt(ctx.fs, modulePath, call, d, env, onWarn)
}

func (d *ModuleData) expandBodyAtParse(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, macro STR, args []ANY, line int, env Environment, onWarn func(Warn), platform *Platform) {
	name := platform.conf.moduleAlias(d.confModule, macro)

	expandConfMacro(ctx, modulePath, kind, language, UnknownStmt{Name: internTokMaybeSTR(name), Raw: name, Args: args, Line: line}, true, env, d, onWarn, platform)
}

func enumSerializationSkipped(d *ModuleData, language Language, env Environment, v *GenerateEnumSerializationStmt) bool {
	if d.multimodule == tokFbsLibrary && language == LangPy && (v.Variant == "plain" || v.Variant == "with_header") {
		return true
	}

	return env.bool(envGO_PROTO)
}

func (d *ModuleData) deferGuard(platform *Platform, name STR) {
	d.deferGuardOf(platform, name, platform.conf.alias(name))
}

func (d *ModuleData) deferGuardOf(platform *Platform, name STR, macro *ConfMacro) {
	if macro == nil || !macro.conditions || slices.Contains(d.guards, name) {
		return
	}

	d.guards = append(d.guards, name)

	statement := platform.conf.statementKey(name)

	d.pending = append(d.pending, PendingStmt{key: statement.prio << 32, seq: d.declSeq, name: statement.name, macro: macro, stmt: &MacroGuardStmt{Name: statement.name}})
	d.declSeq += declSeqBlock
}

func parseTimeStmt(s Stmt) bool {
	if allowedOutsideModule(s) {
		return true
	}

	if v, ok := s.(*UnknownStmt); ok {
		switch v.Name {
		case tokSetAppendWithGlobal, tokDeclareExternalHostResourcesBundleByJson, tokGlob, tokLateGlob, tokDeclareInDirs:
			return true
		}
	}

	return false
}

func (d *ModuleData) deferModuleGuards(platform *Platform, module *confModule, seen []STR) []STR {
	if module == nil {
		return seen
	}

	if parent := platform.conf.tables.module(module.parent, ""); parent != nil {
		seen = d.deferModuleGuards(platform, parent, seen)
	}

	for _, call := range module.body {
		seen = d.deferCallGuards(platform, call.name, seen)
	}

	return seen
}

func (d *ModuleData) deferCallGuards(platform *Platform, name STR, seen []STR) []STR {
	if baseMacro(name) || dirStatement(name) || slices.Contains(seen, name) {
		return seen
	}

	seen = append(seen, name)
	d.deferGuard(platform, name)

	if macro := platform.conf.alias(name); macro != nil {
		for _, call := range macro.calls {
			seen = d.deferCallGuards(platform, call.name, seen)
		}
	}

	return seen
}

func (d *ModuleData) statementCommandDirs(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, s Stmt, env Environment, onWarn func(Warn), platform *Platform) {
	if isGoModuleType(d.unit.Type) {
		return
	}

	name := stmtMacroNameID(s)

	if platform.conf.noCommandDirs.has(uint32(name)) {
		return
	}

	dirs := platform.conf.commandDirs(name.string(), env)

	if len(dirs.peerdirs) == 0 && len(dirs.addincls) == 0 && len(dirs.vars) == 0 {
		platform.conf.noCommandDirs.add(uint32(name))

		return
	}

	if len(dirs.peerdirs) != 0 {
		collectStmts(ctx, modulePath, kind, language, []Stmt{&PeerdirStmt{Paths: appendInternAnys(nil, dirs.peerdirs), Implicit: true}}, env, d, onWarn, platform)
	}

	if len(dirs.addincls) != 0 {
		collectStmts(ctx, modulePath, kind, language, []Stmt{splitAddInclPaths(appendInternAnys(nil, dirs.addincls))}, env, d, onWarn, platform)
	}
}

func interpretPending(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	var seen [32]STR

	d.deferModuleGuards(platform, d.confModule, seen[:0])

	slices.SortStableFunc(d.pending, func(a, b PendingStmt) int {
		if a.key != b.key {
			return cmp.Compare(a.key, b.key)
		}

		return strings.Compare(a.name.string(), b.name.string())
	})

	d.interpreting = true

	if dir := env.string(envDllForDir); dir != "" {
		d.srcDirs = append(d.srcDirs, dirKey(dir).source())
		d.addLocalIncl(prioAddIncl, source(dir))
	}

	early := len(d.peerdirs)

	applyVarsAsMacros(ctx, modulePath, kind, language, env, d, onWarn, platform)

	d.startPeers = len(d.peerdirs) - early

	slices.Reverse(d.peerdirs)
	slices.Reverse(d.peerdirs[:d.startPeers])
	slices.Reverse(d.peerdirs[d.startPeers:])

	last := d.declSeq

	for i := 0; i < len(d.pending); i++ {
		d.declSeq = d.pending[i].seq

		collectStmts(ctx, modulePath, kind, language, []Stmt{d.pending[i].stmt}, env, d, onWarn, platform)
		d.statementCommandDirs(ctx, modulePath, kind, language, d.pending[i].stmt, env, onWarn, platform)
		platform.conf.setGuard(env, d.pending[i].macro, &d.setVars)
		applyVarsAsMacros(ctx, modulePath, kind, language, env, d, onWarn, platform)
	}

	d.declSeq = declSeqModuleEnd | last
	d.pending = d.pending[:0]
	d.interpreting = false
}

func (ctx *GenCtx) varWords(value STR) []ANY {
	if words, ok := ctx.varWordCache[value]; ok {
		return words
	}

	words := slices.Clip(internAnys(strings.Fields(value.string())))

	if ctx.varWordCache == nil {
		ctx.varWordCache = map[STR][]ANY{}
	}

	ctx.varWordCache[value] = words

	return words
}

func applyVarsAsMacros(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	for _, key := range [...]ENV{envPeerdir, envSrcdir, envAddincl} {
		bound, raw := env.s.lookup(key)

		if bound == envAbsent || raw == 0 || slices.Contains(d.varMacrosApplied, raw) {
			continue
		}

		d.varMacrosApplied = append(d.varMacrosApplied, raw)

		words := ctx.varWords(env.expandVariableSTR(env.string(key)))

		var stmt Stmt

		switch key {
		case envPeerdir:
			if own := internStr(modulePath).any(); slices.Contains(words, own) {
				words = slices.DeleteFunc(slices.Clone(words), func(w ANY) bool { return w == own })
			}

			stmt = &PeerdirStmt{Paths: words, Implicit: true}
		case envSrcdir:
			stmt = &SrcDirStmt{Dirs: words}
		default:
			stmt = splitAddInclPaths(words)
		}

		collectStmts(ctx, modulePath, kind, language, []Stmt{stmt}, env, d, onWarn, platform)
	}
}

func allowedOutsideModule(s Stmt) bool {
	switch v := s.(type) {
	case *IncludeStmt, *MissingIncludeStmt, *IfStmt, *SetStmt, *DefaultVarStmt, *ModuleStmt, *EndStmt:
		return true
	case *UnknownStmt:
		switch v.Name {
		case tokEnable, tokDisable, tokSetAppend, tokMessage, tokIncludeOnce, tokNoBuildIf, tokBuildOnlyIf, tokSubscriber,
			tokVersion, tokOriginalSource, tokNeedReview, tokNeedCheck, tokNoNeedCheck, tokOnlyTags, tokExcludeTags,
			tokIncludeTags, tokSetResourceUriFromJson, tokSetResourceMapFromJson, tokDllFor, tokPyProtosFor, tokGoTestFor,
			tokExtradir, tokSourceGroup, tokRecurse, tokRecurseForTests, tokRecurseRootRelative, tokPartitionedRecurse,
			tokPartitionedRecurseForTests, tokPartitionedRecurseRootRelative:
			return true
		}
	}

	return false
}

func stmtMacroName(s Stmt) string {
	switch v := s.(type) {
	case *UnknownStmt:
		return v.Raw.string()
	case *DeclareResourceStmt:
		return v.Macro.string()
	case *RunAntlrStmt:
		return v.Macro
	case *CFlagsStmt:
		if v.Global {
			return "GLOBAL_CFLAGS"
		}

		return "CFLAGS"
	case *AllResourceFilesStmt:
		if v.FromDirs {
			return "ALL_RESOURCE_FILES_FROM_DIRS"
		}

		return "ALL_RESOURCE_FILES"
	case *GenerateEnumSerializationStmt:
		return v.Macro.string()
	case *MacroGuardStmt:
		return v.Name.string()
	case *RunAntlr4PythonStmt:
		return "RUN_ANTLR4_PYTHON" + v.Version
	case *PeerdirStmt:
		return "PEERDIR"
	case *SrcsStmt:
		return "SRCS"
	case *JoinSrcsStmt:
		return "JOIN_SRCS"
	case *AddInclStmt:
		return "ADDINCL"
	case *CXXFlagsStmt:
		return "CXXFLAGS"
	case *CONLYFlagsStmt:
		return "CONLYFLAGS"
	case *LDFlagsStmt:
		return "LDFLAGS"
	case *SrcDirStmt:
		return "SRCDIR"
	case *GlobalSrcsStmt:
		return "GLOBAL_SRCS"
	case *RunProgramStmt:
		return "RUN_PROGRAM"
	case *RunPythonStmt:
		return "RUN_PYTHON3"
	case *FromSandboxStmt:
		return "FROM_SANDBOX"
	case *SplitCodegenStmt:
		return "SPLIT_CODEGEN"
	case *BaseCodegenStmt:
		if v.Macro != 0 {
			return v.Macro.string()
		}

		return "BASE_CODEGEN"
	case *ConfigureFileStmt:
		return "CONFIGURE_FILE"
	case *CreateBuildInfoStmt:
		return "CREATE_BUILDINFO_FOR"
	case *RunAntlr4CppStmt:
		return "RUN_ANTLR4_CPP"
	case *RunAntlr4CppSplitStmt:
		return "RUN_ANTLR4_CPP_SPLIT"
	case *ResourceStmt:
		return "RESOURCE"
	case *ResourceFilesStmt:
		return "RESOURCE_FILES"
	}

	return fmt.Sprintf("%T", s)
}

func stmtMacroNameID(s Stmt) STR {
	switch v := s.(type) {
	case *UnknownStmt:
		return v.Raw
	case *DeclareResourceStmt:
		return v.Macro.str()
	case *RunAntlrStmt:
		return internStr(v.Macro)
	case *CFlagsStmt:
		if v.Global {
			return strGlobalCflags
		}

		return kwCFLAGS
	case *AllResourceFilesStmt:
		if v.FromDirs {
			return strAllResourceFilesFromDirs
		}

		return strAllResourceFiles
	case *GenerateEnumSerializationStmt:
		return v.Macro
	case *MacroGuardStmt:
		return v.Name
	case *RunAntlr4PythonStmt:
		return internStr("RUN_ANTLR4_PYTHON" + v.Version)
	case *PeerdirStmt:
		return kwPEERDIR
	case *SrcsStmt:
		return kwSRCS
	case *JoinSrcsStmt:
		return kwJOIN_SRCS
	case *AddInclStmt:
		return kwADDINCL
	case *CXXFlagsStmt:
		return kwCXXFLAGS
	case *CONLYFlagsStmt:
		return kwCONLYFLAGS
	case *LDFlagsStmt:
		return kwLDFLAGS
	case *SrcDirStmt:
		return kwSRCDIR
	case *GlobalSrcsStmt:
		return kwGLOBAL_SRCS
	case *RunProgramStmt:
		return kwRUN_PROGRAM
	case *RunPythonStmt:
		return kwRUN_PYTHON3
	case *FromSandboxStmt:
		return strFromSandbox
	case *SplitCodegenStmt:
		return strSplitCodegen
	case *BaseCodegenStmt:
		if v.Macro != 0 {
			return v.Macro
		}

		return strBaseCodegen
	case *ConfigureFileStmt:
		return kwCONFIGURE_FILE
	case *CreateBuildInfoStmt:
		return kwCREATE_BUILDINFO_FOR
	case *RunAntlr4CppStmt:
		return kwRUN_ANTLR4_CPP
	case *RunAntlr4CppSplitStmt:
		return kwRUN_ANTLR4_CPP_SPLIT
	case *ResourceStmt:
		return kwRESOURCE
	case *ResourceFilesStmt:
		return kwRESOURCE_FILES
	}

	return internStr(stmtMacroName(s))
}

func collectStmts(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, stmts []Stmt, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	fs := ctx.fs

	for _, s := range stmts {
		if d.discarded {
			return
		}

		if d.moduleStmt == nil && !d.moduleOpening && !allowedOutsideModule(s) {
			onWarn(Warn{Kind: WarnSyntax, Message: fmt.Sprintf("%s: unexpected command %s outside of a module", modulePath, stmtMacroName(s))})

			continue
		}

		if d.moduleStmt != nil && !d.moduleEnded && !d.interpreting && !parseTimeStmt(s) && d.deferStmt(ctx, modulePath, kind, language, s, env, onWarn, platform) {
			continue
		}

		switch v := s.(type) {
		case *MacroGuardStmt:
		case *IncludeStmt:
			ctx.withIncludedStmts(v, env, func(included []Stmt) {
				collectStmts(ctx, modulePath, kind, language, included, env, d, onWarn, platform)
			})
		case *MissingIncludeStmt:
			throwFmt("%s:%d: INCLUDE file %s not found", modulePath, v.Line, v.Path)
		case *ModuleStmt:
			v = d.moduleContentDeclaration(v)

			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			d.moduleOpening = true

			expanded := *v

			expanded.Args = expandSourceArguments(v.Args, env)
			v = astOne(astModules, expanded)

			applyCppTestModule(v, d, env)

			if isNotsModule(v.Name) || v.Name == tokProtoLibrary && (language == LangTS || language == LangTSPrepare) {
				d.nots = newNotsModule(v.Name, language, env)
				d.nots.AutoProto = v.Name == tokProtoLibrary && !fs.isFilePathClean(modulePath+"/package.json")
			}

			if v.Name == tokProtoLibrary && language == LangTS {
				env.setString(envNoLintValue, "none")
			}

			d.unit = resolveModuleUnit(v.Name, kind, language)

			owner := multimoduleOwner(v)

			d.confModule = platform.conf.module(owner, moduleConfTag(owner, kind, language, d))

			_, prjName := env.s.lookup(envREALPRJNAME)

			platform.conf.initModule(env, d.confModule, &d.setVars)
			d.syncPythonVersion(env)

			if _, conf := env.s.lookup(envREALPRJNAME); conf == prjName {
				env.setString(envREALPRJNAME, moduleRealPrjName(modulePath, v, d.unit))
			}

			if moduleType := confModuleType(v.Name, d); moduleType != "" {
				env.setString(envMODULE_TYPE, moduleType)
			}

			if (v.Name == tokUnittestFor || v.Name == tokJtestFor) && len(v.Args) > 0 {
				env.setString(envUnittestDir, v.Args[0].string())
			}

			d.moduleBodyDirs(ctx, modulePath, kind, language, env, onWarn, platform)

			if isDllModuleType(d.unit.Type) {
				env.setString(envMODULE_TAG, "DLL")
				bindDllModulePrefix(v, env)
			}

			env.setStringID(envModuleKind, v.Name.str())
			setupYqlModule(d, v.Name, env)
			setupUdfBaseModule(v, d, env)
			setupPythonDllModule(v, env)

			if v.Schema && d.unit.Tag == unitTagPy3Proto {
				d.unit.HashTag = strPy3ProtoFromSchema
			}

			if v.Schema && d.unit.Tag == strPyProto {
				d.unit.HashTag = strPyProtoFromSchema
			}

			d.moduleStmt = moduleStmtForKind(v, d.unit)

			setupPythonImportMask(env, d)
			setupSandboxPythonTask(v.Name, modulePath, kind, language, env, d)

			if isPyProgramType(d.unit.Type) {
				env.setBool(envSTRIP, true)

				if pythonNoStrip(env) {
					env.setBool(envNO_STRIP, true)
				}
			}

			if isUnionModule(v.Name) {
				env.setStringID(envDontResolveIncludes, strYes)
			}

			if isUnionModule(d.unit.Type) {
				env.setString(envMODULE_SUFFIX, ".pkg.fake")
			}

			if v.Name == tokPy23Test {
				subPath := "py3test"

				if language == LangPy2 {
					subPath = "py2test"
				} else {
					d.moduleVariants = 1 << LangPy2
				}

				env.setString(envCanonizeSubPath, subPath)
			}

			if isGoModuleType(d.unit.Type) {
				initGoModuleName(modulePath, env, d, onWarn)
			}

			if d.moduleStmt.Name == tokProtoDescriptions || v.Name == tokProtoRegistry {
				env.setStringID(envMODULE_TAG, d.moduleStmt.Name.str())
				env.setStringID(envMODULE_TYPE, d.moduleStmt.Name.str())
			}

			if v.Name == tokResourcesLibrary {
				env.setString(envMODULE_TAG, "RESOURCE_LIB")
			}

			setupRosModule(v, modulePath, language, env, d)
			setupMlaunchModule(v, modulePath, language, env, d)
			setupPackageModule(v, d)
			setupSdcUnionModule(v, language, env, d)
			setupSdcDiagsModule(v, language, env, d)
			setupRustModule(v.Name, language, env, d)

			if isRustModule(v.Name) {
				collectStmts(ctx, modulePath, kind, language, []Stmt{&UnknownStmt{Name: tokNoPlatform, Raw: tokNoPlatform.str()}}, env, d, onWarn, platform)
			}

			if v.Name == tokFbsLibrary {
				d.multimodule = v.Name
				d.moduleVariants = 1<<LangCPP | 1<<LangGo | 1<<LangJava | 1<<LangPy
				env.setStringID(envMODULE_TAG, d.unit.Tag)
				env.setBool(envCppFbs, language == LangCPP)
				env.setBool(envJavaFbs, language == LangJava || language == LangFbsJavaNative)

				if language == LangFbsJavaNative {
					collectStmts(ctx, modulePath, kind, language, []Stmt{&UnknownStmt{Name: tokNoRuntime, Raw: tokNoRuntime.str()}}, env, d, onWarn, platform)
				}

				if language == LangPy {
					env.setBool(envPy3Fbs, true)
					env.setString(envNoLintValue, "none_internal")
				}
			}

			if v.Name == tokProtoLibrary && !v.Schema {
				d.moduleVariants = defaultProtoVariants
			}

			if isPythonDllModule(v.Name) || d.unit.HashTag == unitTagPy2Native || d.unit.HashTag == unitTagPy3Native {
			} else if d.unit.Tag == unitTagPy2 || d.unit.Tag == unitTagPy2Native || d.unit.Tag == strPyProto {
				usePython2(env, d, d.unit.HashTag)
			} else if d.unit.Tag != 0 {
				env.setStringID(envMODULE_TAG, d.unit.HashTag)
			}

			if isDllModuleType(v.Name) || v.Name == tokUdfBase || d.yqlUdfShared {
				d.exportsScript = dllExportsArg(v)

				if d.exportsScript == nil {
					if exports, ok := env.lookup("EXPORTS_FILE"); ok && exports != "" {
						d.exportsScript = ptr(internStr(exports).any())
					}
				}
			}

			if v.Name == tokDocs || v.Name == tokDocsLibrary {
				d.docs = &DocsData{}
			}

			if v.Name == tokProtoLibrary && language == LangGo {
				env.setString(envMODULE_TAG, "GO_PROTO")
				env.setBool(envGO_PROTO, true)
			}

			if d.moduleStmt.Name == tokProtoLibrary {
				if language == LangPy || language == LangPy2 {
					env.setString(envNoLintValue, "none_internal")
				}

				if language == LangPy {
					env.setBool(envPY3_PROTO, true)
				} else if language == LangPy2 {
					env.setBool(envPyProto, true)
				} else if language == LangTS {
					env.setBool(envTsProto, true)
				} else if language == LangTSPrepare {
					env.setBool(envTsProtoPrepareDeps, true)
				} else if language == LangDescProto {
					env.setString(envMODULE_TAG, "DESC_PROTO")
					env.setBool(envDescProto, true)
				} else if language == LangDocsProto {
					env.setString(envDocsProto, "yes")
				} else {
					env.setStringID(envMODULE_TAG, strCPPProto)
					env.setStringID(envCPP_PROTO, strCPPProto)
					env.setBool(envGEN_PROTO, true)
				}
			}

			if v.Name == tokProtoLibrary {
				env.setString(envMODULE_TAG, protoModuleTag(language, v.Schema))
			}

			moduleLang := sbomComponentLang(d.moduleStmt.Name)

			if d.moduleStmt.Name == tokProtoLibrary && env.bool(envPY3_PROTO) {
				moduleLang = moduleLangTokenPy3
			}

			if d.moduleStmt.Name == tokProtoLibrary && language == LangDescProto {
				moduleLang = ""
			}

			if d.usePython2 {
				moduleLang = moduleLangTokenPy2
			}

			if d.yqlPythonUdf && !d.usePython2 {
				moduleLang = moduleLangTokenPy3
			}

			if moduleAssignsLang(d.moduleStmt.Name) {
				env.setString(envMODULE_LANG, moduleLang)
			}

		case *SrcsStmt:
			if d.moduleStmt != nil && isRustModule(d.moduleStmt.Name) {
				applyRustMacro(UnknownStmt{Name: tokCrateSources, Args: expandStmtTokens(v.Sources, env)}, modulePath, env, d)

				continue
			}

			if d.yqlPrebuiltAliases {
				d.yqlSourceSrcs = append(d.yqlSourceSrcs, expandStmtTokens(v.Sources, env)...)

				continue
			}

			if isPythonProtoModule(d) || d.multimodule == tokFbsLibrary && language == LangPy {
				stmt := UnknownStmt{Name: tokPySrcs, Args: expandRootDeferredTokens(v.Sources, env, tokenExpandPathArguments)}

				d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{DeclSeq: d.nextDeclSeq(), Statement: astOne(astUnknowns, stmt), ResourceOffset: len(d.resources), Prio: stmtPrioSrcs})

				continue
			}

			if d.moduleStmt != nil && (isUnionModule(d.moduleStmt.Name) || d.moduleStmt.Name == tokPackageFinal) {
				d.addFiles(expandStmtTokens(v.Sources, env))

				continue
			}

			if d.docs != nil {
				d.docs.addSources(fs, modulePath, anyStrs(expandStmtTokens(v.Sources, env)))

				continue
			}

			routeAllToGlobal := d.moduleStmt != nil && isYqlUdfStaticModule(d.moduleStmt.Name)
			goSrcs := isGoModuleType(d.unit.Type)
			globalNext := false
			srcToks := v.Sources

			if !d.interpreting {
				srcToks = expandTokens(srcToks, env, false, tokenExpandStatement)
			}

			srcToks = expandTokens(srcToks, env, false, tokenExpandRoots)

			stmt := d.nextDeclSeq()

			if d.unit.SwigLang == strSwigLangJniCpp {
				srcToks = jniCppSwigSrcs(srcToks)
			}

			for _, srcTok := range srcToks {
				compile := CompileSpec{}

				if goSrcs {
					ext := filepath.Ext(srcTok.string())

					if srcTok.string() != "CGO_EXPORT" && !goSrcsExtensionSupported(ext) {
						continue
					}

					if ext == ".S" || slices.Contains(goCgoExportExts, ext) {
						compile.OutputStem = copyFileOutputVFS(modulePath, srcTok.string()).rel().build()
					}
				}

				if srcTok == kwGLOBAL.any() {
					globalNext = true

					continue
				}

				if strHasDollar(srcTok) && strings.Contains(srcTok.string(), "${") {
					continue
				}

				global := routeAllToGlobal || globalNext

				d.srcs = append(d.srcs, SrcMeta{Source: srcTok, Prio: stmtPrioSrcs, Seq: d.nextDeclSeq(), Stmt: stmt, Global: global, Compile: compile})
				globalNext = false

				switch srcExtClassOf(srcTok) {
				case srcExtHIn:
					addGeneratedHeaderInclude(modulePath, strings.TrimSuffix(srcTok.string(), ".in"), d)
				case srcExtCppIn, srcExtCIn:
					addGeneratedHeaderIncludeCF(modulePath, strings.TrimSuffix(srcTok.string(), ".in"), d)
				case srcExtY:
					src := srcTok.string()

					addGeneratedOwnHeaderInclude(modulePath, strings.TrimSuffix(src, filepath.Ext(src))+".h", d)
				}
			}

		case *PeerdirStmt:
			if d.yqlPrebuiltAliases {
				d.yqlSourcePeers = append(d.yqlSourcePeers, expandStmtTokens(v.Paths, env)...)

				continue
			}

			addInclNext := false

			var fromID STR

			if !v.Implicit {
				fromID = internStr(modulePath)
			}

			for _, pTok := range expandStmtTokens(v.Paths, env) {
				if pTok == kwADDINCL.any() {
					addInclNext = true

					continue
				}

				if pTok == kwGLOBAL.any() {
					continue
				}

				if strHasDollar(pTok) && strings.Contains(pTok.string(), "${") {
					continue
				}

				pTok = peerdirModulePath(pTok).any()

				if !v.Implicit && !platform.allowPeer(fromID, pTok.str(), onWarn) {
					addInclNext = false

					continue
				}

				if addInclNext {
					d.addLocalIncl(prioAddIncl, parseModulePathVFS(pTok.string()))
					addInclNext = false
				}

				d.peerdirs = append(d.peerdirs, pTok)

				if !v.Implicit {
					d.explicitPeers = append(d.explicitPeers, pTok)
				}

				if !v.Implicit && pTok.str() == unittestForPeer(d.moduleStmt) {
					d.unitTestPeerExplicit = true
				}

				if !v.Implicit && d.nots != nil && d.nots.Kind == tokProtoLibrary {
					d.nots.ProtoPeers = append(d.nots.ProtoPeers, pTok)
				}
			}
		case *SetStmt:

			sourceValue := v.Value

			if v.Name == importMaskVariable && v.LexicalValue != "" {
				sourceValue = v.LexicalValue
			}

			valueID, textInputs := expandSourceScalarInputSTR(sourceValue, env)
			value := valueID.string()

			env.setFromSTR(v.NameEnv, valueID)
			env.setTextInputs(v.NameEnv, textInputs)
			d.captureResourcePeerPolicy(v.Name, env)

			if v.Name == importMaskVariable {
				setPythonImportMask(value, d)
			}

			if v.NameEnv == envSANITIZER_TYPE {
				setupPythonImportMask(env, d)
			}

			if v.NameEnv == envPYTHON2 || v.NameEnv == envPYTHON3 {
				d.syncPythonVersion(env)
			}

			d.setVars.set(v.NameEnv.str(), valueID)
			platform.conf.set(env, v.NameEnv, &d.setVars)

			if v.Name == "SWIG_LANG" {
				d.unit.SwigLang = valueID
			}

			if v.Name == "PROTOC_TRANSITIVE_HEADERS" {
				recalcProtoOutputs(d)
			}

			if v.Name == "PROTO_NAMESPACE" {
				d.protoNamespace = nil

				if value != "" {
					d.protoNamespace = ptr(valueID.any())
				}
			}

			if v.Name == "RAGEL6_FLAGS" {
				d.ragel6Flags = internAnys(strings.Fields(value))
			}
		case *EndStmt:
			if !d.processModuleEnd(modulePath, v.Line, onWarn) {
				continue
			}

			collectModuleAutoinclude(ctx, modulePath, kind, language, env, d, onWarn, platform)
			yqlEpilogue(ctx, modulePath, kind, language, env, d, onWarn, platform)
			interpretPending(ctx, modulePath, kind, language, env, d, onWarn, platform)
			d.pythonLint.bind(env)
			d.finishResourcePeerPolicy()

			if d.nots != nil {
				d.nots.finish(env)
			}

			d.unit.HashTag = internStr(env.string(envMODULE_TAG))
			d.pyNamespacePython2 = env.string(envPYTHON2) == "yes"
			d.moduleEnded = true

		case *JoinSrcsStmt:
			expanded := *v

			expanded.OutputName = expandStmtToken(v.OutputName, env)
			expanded.Sources = expandStmtTokens(v.Sources, env)
			expanded.Seq = d.nextDeclSeq()

			d.joinSrcs = append(d.joinSrcs, &expanded)
		case *AddInclStmt:

			d.addInclGlobal = append(d.addInclGlobal, addInclDirs(v.GlobalPaths, env)...)
			d.addInclOneLevel = append(d.addInclOneLevel, addInclDirs(v.OneLevelPaths, env)...)
			d.addInclUserGlobal = append(d.addInclUserGlobal, addInclDirs(v.UserGlobalPaths, env)...)

			for _, p := range expandConfigVFSPaths(v.AllPaths, env) {
				if p != srcRootDirVFS {
					d.addLocalIncl(prioAddIncl, p)
				}
			}

			d.cythonAddIncl = append(d.cythonAddIncl, expandConfigVFSPaths(v.CythonPaths, env)...)
			d.cythonAddInclGlobal = append(d.cythonAddInclGlobal, expandConfigVFSPaths(v.CythonGlobalPaths, env)...)
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
			d.protoAddIncl = append(d.protoAddIncl, addInclDirs(v.ProtoPaths, env)...)
			d.protoAddInclGlobal = append(d.protoAddInclGlobal, addInclDirs(v.ProtoGlobalPaths, env)...)
			d.protoIncludeLocal = append(d.protoIncludeLocal, addInclDirs(v.ProtoAllPaths, env)...)
			d.flatcAddIncl = append(d.flatcAddIncl, expandConfigVFSPaths(v.FlatcPaths, env)...)
			d.flatcAddInclGlobal = append(d.flatcAddInclGlobal, expandConfigVFSPaths(v.FlatcGlobalPaths, env)...)
			d.swigAddIncl = append(d.swigAddIncl, expandConfigVFSPaths(v.SwigPaths, env)...)
			d.swigAddInclGlobal = append(d.swigAddInclGlobal, expandConfigVFSPaths(v.SwigGlobalPaths, env)...)
			d.scAddIncl = append(d.scAddIncl, expandConfigVFSPaths(v.ScPaths, env)...)
			d.scAddInclGlobal = append(d.scAddInclGlobal, expandConfigVFSPaths(v.ScGlobalPaths, env)...)
			d.scAddInclExport = append(d.scAddInclExport, expandConfigVFSPaths(v.ScExportPaths, env)...)
		case *CFlagsStmt:

			d.cFlagsGlobal, d.cFlags = appendCompilerFlags(v.Flags, env, "USER_CFLAGS", v.Global, d.cFlagsGlobal, d.cFlags, &d.compilerFlagUses[compilerFlagsCommon])
		case *CXXFlagsStmt:

			d.cxxFlagsGlobal, d.cxxFlags = appendCompilerFlags(v.Flags, env, "USER_CXXFLAGS", false, d.cxxFlagsGlobal, d.cxxFlags, &d.compilerFlagUses[compilerFlagsCxx])
		case *CONLYFlagsStmt:

			d.cOnlyFlagsGlobal, d.cOnlyFlags = appendCompilerFlags(v.Flags, env, "USER_CONLYFLAGS", false, d.cOnlyFlagsGlobal, d.cOnlyFlags, &d.compilerFlagUses[compilerFlagsCOnly])
		case *LDFlagsStmt:

			value := joinAnys(expandStmtTokens(v.Flags, env), " ")

			d.ldFlags = append(d.ldFlags, internAny(expandSourceScalarVarRef(value, env)))
		case *SrcDirStmt:

			for _, dirTok := range expandStmtTokens(v.Dirs, env) {
				dir := dirTok.string()
				rooted, ok := moduleRootedVFS(modulePath, dir)

				if !ok {
					rooted = dirKey(dir).source()
				}

				if !slices.Contains(d.srcDirs, rooted) {
					d.srcDirs = append(d.srcDirs, rooted)
				}
			}
		case *GlobalSrcsStmt:
			for _, src := range expandStmtTokens(v.Sources, env) {
				d.srcs = append(d.srcs, SrcMeta{Source: src, Prio: stmtPrioSrcs, Seq: d.nextDeclSeq(), Global: true})
			}
		case *GenerateEnumSerializationStmt:
			if enumSerializationSkipped(d, language, env, v) {
				continue
			}

			expandedEN := astOne(astEnumSers, *v)

			expandedEN.DeclSeq = d.nextDeclSeq()
			d.enumSrcs = append(d.enumSrcs, expandedEN)
		case *DefaultVarStmt:
			value := expandSourceScalarVarRef(v.Value, env)
			bound := env.hasBindingID(v.NameEnv)

			if d.defaultVars == nil {
				d.defaultVars = map[STR]STR{}
			}

			if _, exists := d.defaultVars[internStr(v.VarName)]; !exists {
				d.defaultVars[internStr(v.VarName)] = internStr(value)
			}

			env.setDefaultString(v.NameEnv, value)

			if !bound {
				d.captureResourcePeerPolicy(v.VarName, env)

				d.setVars.set(internStr(v.VarName), internStr(value))
				platform.conf.set(env, v.NameEnv, &d.setVars)
			}

		case *ConfigureFileStmt:
			expanded := *v

			expanded.Src = expandStmtToken(v.Src, env)
			expanded.Dst = expandStmtToken(v.Dst, env)
			expanded.DeclSeq = d.nextDeclSeq()

			d.configureFiles = append(d.configureFiles, &expanded)

			if strings.HasSuffix(expanded.Src, ".h.in") || strings.HasSuffix(expanded.Dst, ".h") {
				addGeneratedHeaderInclude(modulePath, expanded.Dst, d)
			} else {
				addGeneratedHeaderIncludeCF(modulePath, expanded.Dst, d)
			}
		case *CreateBuildInfoStmt:
			d.createBuildInfoFor = ptr(internStr(v.OutputHeader).any())
		case *RunAntlr4CppStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, Antlr4GrammarInfo{
				DeclSeq:        d.nextDeclSeq(),
				IsSplit:        false,
				Grammar:        expandStmtToken(v.Grammar.string(), env),
				Options:        anyStrs(expandStmtTokens(v.Options, env)),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: anyStrs(expandStmtTokens(v.OutputIncludes, env)),
			})
		case *RunAntlr4CppSplitStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, Antlr4GrammarInfo{
				DeclSeq:        d.nextDeclSeq(),
				IsSplit:        true,
				Options:        anyStrs(expandStmtTokens(v.Options, env)),
				Lexer:          expandStmtToken(v.Lexer.string(), env),
				Parser:         expandStmtToken(v.Parser.string(), env),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: anyStrs(expandStmtTokens(v.OutputIncludes, env)),
			})
		case *RunAntlr4PythonStmt:
			collectAntlrPython(ctx.parsers, modulePath, v, env, d)
		case *RunAntlrStmt:
			expanded := AntlrRunInfo{
				DeclSeq:        d.nextDeclSeq(),
				Macro:          v.Macro,
				Args:           expandStmtTokens(v.Args, env),
				INFiles:        expandStmtTokens(v.INFiles, env),
				OUTFiles:       expandStmtTokens(v.OUTFiles, env),
				OUTNoAutoFiles: expandStmtTokens(v.OUTNoAutoFiles, env),
				OutputIncludes: expandStmtTokens(v.OutputIncludes, env),
			}

			if v.CWD != nil {
				cwd := expandStmtTokenAny((*v.CWD), env)

				expanded.CWD = &cwd
			}

			d.antlrRuns = append(d.antlrRuns, expanded)
		case *RunProgramStmt:
			appendRunProgram(v, env, d)
		case *SplitCodegenStmt:
			expanded := *v

			expanded.DeclSeq = d.nextDeclSeq()
			expanded.ToolPath = expandStmtTokenAny(v.ToolPath, env)
			expanded.Prefix = expandStmtTokenAny(v.Prefix, env)
			expanded.Opts = expandStmtTokens(v.Opts, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)

			d.splitCodegens = append(d.splitCodegens, &expanded)
		case *BaseCodegenStmt:
			expanded := *v

			expanded.ToolPath = expandStmtTokenAny(v.ToolPath, env)
			expanded.Prefix = expandStmtTokenAny(v.Prefix, env)
			expanded.Opts = expandStmtTokens(v.Opts, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.DeclSeq = d.nextDeclSeq()

			d.baseCodegens = append(d.baseCodegens, &expanded)
		case *RunPythonStmt:
			expanded := expandRunPythonStmt(v, env)

			expanded.DeclSeq = d.nextDeclSeq()
			d.runPython = append(d.runPython, expanded)
		case *FromSandboxStmt:
			expanded := *v

			expanded.ResourceId = expandStmtTokenAny(v.ResourceId, env)
			expanded.Prefix = expandStmtToken(v.Prefix, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.InducedDeps = expandStmtTokens(v.InducedDeps, env)
			expanded.Renames = expandStmtTokens(v.Renames, env)
			expanded.DeclSeq = d.nextDeclSeq()

			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *ResourceStmt:
			pairs, dontParse, dontCompress := expandResourcePairs(v.Args, v.Line, env)

			if isGoModuleType(d.unit.Type) {
				entries := make([]ResourceEntry, len(pairs))

				for i, pair := range pairs {
					entries[i] = ResourceEntry{Path: pair.Path, Key: pair.Key}
				}

				applyGoResource(fs, modulePath, entries, d)

				break
			}

			ensureResourcePeer(modulePath, d)

			seq := d.nextDeclSeq()

			for i, pair := range pairs {
				d.resources = append(d.resources, ResourceEntry{
					DeclSeq:      seq,
					Path:         pair.Path,
					Key:          pair.Key,
					EndsBatch:    i == len(pairs)-1,
					DontParse:    dontParse,
					DontCompress: dontCompress,
				})
			}
		case *ResourceFilesStmt:
			applyResourceFiles(fs, modulePath, expandResourceFileArgs(v.Args, env), v.Line, d)
		case *AllResourceFilesStmt:
			if isGoModuleType(d.unit.Type) {
				entries := expandAllResourceFiles(d, nil, fs, modulePath, env, v)

				applyGoResource(fs, modulePath, entries, d)

				break
			}

			d.resourcePeerDeferred = true

			before := len(d.resources)

			d.resources = expandAllResourceFiles(d, d.resources, fs, modulePath, env, v)

			seq := d.nextDeclSeq()

			for i := before; i < len(d.resources); i++ {
				d.resources[i].DeclSeq = seq
			}

			if len(d.resources) > before {
				d.resources[len(d.resources)-1].EndsBatch = true
			}

		case *DeclareResourceStmt:

			expanded := *v

			expanded.Args = expandStmtTokens(v.Args, env)
			d.resourceDeclStmts = append(d.resourceDeclStmts, &expanded)

			if d.moduleStmt != nil && (d.moduleStmt.Name == tokResourcesLibrary || d.moduleStmt.Name == tokPrebuiltProgram) {
				for _, decl := range resolveResourceDecls(fs, ctx.host, modulePath, &expanded) {
					env.setResource(decl)
				}
			}
		case *IfStmt:
			taken := v.Then
			cond := false

			if len(d.ifReplay) > 0 {
				cond, d.ifReplay = d.ifReplay[0], d.ifReplay[1:]
			} else {
				cond = evalCond(v.Cond, env)
			}

			d.ifTaken = append(d.ifTaken, cond)

			if !cond {
				taken = v.Else
			}

			collectStmts(ctx, modulePath, kind, language, taken, env, d, onWarn, platform)
		case *UnknownStmt:
			if d.skipModuleContentStmt(v.Name) {
				continue
			}

			if v.Internal {
				onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: internal command %s is not allowed to appear in ya.make (line %d); skipped", modulePath, v.Raw.string(), v.Line)})

				continue
			}

			if v.Raw == strRunJavaProgram {
				raw := v.Args

				if v.LexicalArgs != nil {
					raw = v.LexicalArgs
				}

				expanded := javaRunProgramRawArgs(anyStrs(raw), env, nil)

				if bad := malformedCommandReference(expanded); bad != "" {
					onWarn(Warn{Kind: WarnSyntax, Message: fmt.Sprintf("in %s: RUN_JAVA_PROGRAM(%s) was not processed properly: could not parse command at %q. Skip RUN_JAVA_PROGRAM", modulePath, joinAnys(raw, " "), bad)})

					continue
				}

				args := splitJavaCommandWords(expandStmtToken(joinAnys(raw, " "), env))

				d.runJavaPrograms = append(d.runJavaPrograms, NativeJavaProgram{Call: JavaRunProgram{args: args, raw: expanded}, DeclSeq: d.nextDeclSeq()})
				recordHandledMacro(v.Name, internAnys(args))

				continue
			}

			if v.Name == tokAliceGenerateFunctionSpecs {
				collectStmts(ctx, modulePath, kind, language, aliceFunctionSpecStmts(expandStmtTokens(v.Args, env), v.Line), env, d, onWarn, platform)

				continue
			}

			if assignment := jdkVersionMacroAssignment(v, env); assignment != nil {
				recordHandledMacro(v.Name, expandStmtTokens(v.Args, env))
				collectStmts(ctx, modulePath, kind, language, []Stmt{assignment}, env, d, onWarn, platform)
				env.setBool(envHasPerModuleJdkVersion, true)

				continue
			}

			if v.Name != tokPack && v.Name != tokSetCudaNodeRAMRequirement {
			} else if assignment := declarativeMacroAssignment(v, d.multimodule == tokPackage, env); assignment != nil {
				recordHandledMacro(v.Name, expandStmtTokens(v.Args, env))
				collectStmts(ctx, modulePath, kind, language, []Stmt{assignment}, env, d, onWarn, platform)

				continue
			}

			expanded := *v
			textAppend := captureSourceTextAppend(v, env)

			if v.Name == tokDllFor {
				withDllForStmts(ctx, modulePath, v, func(stmts []Stmt) {
					collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
				})
			} else if v.Name == tokGoTestFor {
				d.withGoTestForStmts(ctx, modulePath, v, func(stmts []Stmt) {
					collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
				})
			} else {
				if v.Name == tokNoCheckImports {
					expanded.Args = prepareRunProgramArguments(runProgramSourceArguments(v.LexicalArgs, env), runProgramSourceEnv(env, &d.sourceEnv))
				} else if v.Name == tokSetAppend && len(v.Args) > 0 && v.Args[0].str() == importMaskVariableSTR {
					expanded.Args = runProgramSourceArguments(v.LexicalArgs, env)
				} else if v.Name == tokRun {
					expanded.Args = expandSourceArguments(v.Args, env)
				} else if v.Name == tokSetListPreSuf {
					expanded.Args = expandListPreSufArgs(v.Args, env)
				} else if v.Name == tokUseRecipe {
					expanded.Args = []ANY{internStr(collectTestRecipe(v.Args, env)).any()}
				} else if v.Name != tokEnv && v.Name != tokAddCustomExplicitLinterCheck && !(v.Name == tokSetAppend && len(v.Args) > 0 && v.Args[0].str() == strTestEnvValue) {
					if v.Name == tokPySrcs && d.usePython2 {
						expanded.Args = expandRootDeferredTokens(v.Args, env, tokenExpandLazy)
					} else if v.Name == tokCopyFile || v.Name == tokCopyFileWithContext {
						expanded.Args = expandRootDeferredTokens(v.Args, env, tokenExpandPathArguments)
					} else if v.Name == tokSetAppend && textAppend != nil {
						expanded.Args = expandRootDeferredTokens(v.Args, env, tokenExpandLazy)
					} else if v.Name == tokSetAppend {
						expanded.Args, _ = derefList(v.Args, env)
					} else {
						expanded.Args = expandStmtTokens(v.Args, env)
					}
				}

				d.recurses = appendConfigurationRecurses(d.recurses, modulePath, expanded, platform)

				if expanded.Name == tokLargeFiles {
					applyLargeFiles(fs, ctx.scannerForPlatform(platform).codegen, modulePath, expanded, d, onWarn)
					recordHandledMacro(expanded.Name, expanded.Args)
				} else {
					if !applyUnknownStmt(fs, modulePath, expanded, d, env, onWarn) {
						if v.Expanded {
							collectStmts(ctx, modulePath, kind, language, v.Body, env, d, onWarn, platform)
						} else {
							expandConfMacro(ctx, modulePath, kind, language, expanded, false, env, d, onWarn, platform)
						}
					} else if d.moduleStmt != nil && !d.moduleEnded && !d.interpreting && !policyDrops(d, expanded.macroName()) {
						d.expandBodyAtParse(ctx, modulePath, kind, language, expanded.Raw, expanded.Args, expanded.Line, env, onWarn, platform)
					}

					for len(d.macroCalls) > 0 {
						call := d.macroCalls[0]

						d.macroCalls = d.macroCalls[1:]
						d.callConfMacro(ctx, modulePath, kind, language, call, env, onWarn, platform)
					}

					if textAppend != nil {
						env.setTextInputs(textAppend.id, textAppend.ranges)
					}
				}

				if expanded.Name == tokEnable || expanded.Name == tokDisable || expanded.Name == tokSetAppend || expanded.Name == tokSetListPreSuf {
					for i, arg := range expanded.Args {
						if (expanded.Name == tokSetAppend || expanded.Name == tokSetListPreSuf) && i > 0 {
							break
						}

						if st := arg.str(); st != 0 {
							platform.conf.set(env, internEnvSTR(st), &d.setVars)
						}
					}
				}
			}
		}
	}
}

type ModuleUnit struct {
	Type             TOK
	Tag              STR
	HashTag          STR
	CCTag            STR
	ObjSuf           STR
	ModulePrefix     string
	ARPrefix         string
	GlobalARTag      STR
	SbomLang         STR
	SwigLang         STR
	ObjectSuffixStem string
	TestLD           bool
	NoStartTarget    bool
}

func jniCppSwigSrcs(srcs []ANY) []ANY {
	out := make([]ANY, 0, len(srcs))

	for _, ext := range []string{".cpp", ".swg"} {
		for _, src := range srcs {
			if strings.HasSuffix(src.string(), ext) {
				out = append(out, src)
			}
		}
	}

	return out
}

func resolveModuleUnit(name TOK, kind ModuleKind, language Language) ModuleUnit {
	u := resolveModuleUnitBare(name, kind, language)

	if u.HashTag == 0 {
		u.HashTag = u.Tag
	}

	return u
}

func resolveModuleUnitBare(name TOK, kind ModuleKind, language Language) ModuleUnit {
	if isRustModule(name) {
		return rustModuleUnit(name, language)
	}

	if language == LangPy2 {
		switch name {
		case tokPy23Library:
			return ModuleUnit{Type: tokPy2Library, Tag: unitTagPy2, CCTag: tagPy2, ObjSuf: tagPy2, ARPrefix: "libpy", GlobalARTag: tagPy2Global, SbomLang: unitSbomCpp}
		case tokPy23NativeLibrary:
			return ModuleUnit{Type: tokLibrary, Tag: unitTagPy2, HashTag: unitTagPy2Native, CCTag: tagPy2Native, ObjSuf: tagPy2, ARPrefix: "lib", GlobalARTag: tagPy2NativeGlobal, SbomLang: unitSbomCpp}
		}
	}

	if name == tokYqlUdfTest || name == tokYqlUdfMinitest || name == tokPy3TestBin {
		return ModuleUnit{Type: tokPy3Test, Tag: unitTagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	}

	if name == tokPy23Test {
		if language == LangPy2 {
			return ModuleUnit{Type: tokPytestBin, Tag: unitTagPy2, ObjSuf: tagPy2, ModulePrefix: "py2_", ARPrefix: "libpy", GlobalARTag: tagGlobal, SbomLang: unitSbomPy2}
		}

		return ModuleUnit{Type: tokPy3Test, Tag: unitTagPy3TestProgram, ObjSuf: tagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	}

	if name == tokPy3Program && kind == KindLib {
		name = tokPy3Library

		return ModuleUnit{Type: name, Tag: unitTagPy3BinLib, ARPrefix: "libpy3", GlobalARTag: tagPy3BinLibGlobal, SbomLang: unitSbomPy3}
	}

	switch name {
	case tokSandboxTask, tokSandboxPy3Task, tokSandboxPy23Task, tokSandboxPy233Task:
		return sandboxPythonTaskUnit(name, kind, language)
	case tokPackage:
		return packageModuleUnit(language)
	case tokRosLibrary:
		return rosModuleUnit(language)
	case tokMlaunchLibrary, tokMlaunchComponent, tokMlaunchPipeline, tokGolaunchProgram, tokGolaunchComposite:
		return mlaunchModuleUnit(name, language)
	case tokSdcDiagsLibrary:
		return sdcDiagsModuleUnit(language)
	case tokUdfBase:
		return ModuleUnit{Type: tokDll, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokDocs:
		if kind == KindLib || kind == KindDocsInternal {
			return ModuleUnit{Type: tokDocsLibrary, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}
	case tokDynamicLibrary:
		if kind == KindLib {
			return ModuleUnit{Type: tokLibrary, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		return ModuleUnit{Type: tokDll, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokDllJava:
		return ModuleUnit{Type: tokDll, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp, SwigLang: strSwigLangJniCpp}
	case tokGoLibrary, tokGoProgram, tokGoTest, tokGoDll:
		return ModuleUnit{Type: name, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy3Program:
		return ModuleUnit{Type: name, Tag: unitTagPy3Bin, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	case tokPy2Program, tokPy2Test:
		return ModuleUnit{Type: name, Tag: unitTagPy2, ARPrefix: "libpy", GlobalARTag: tagGlobal, SbomLang: unitSbomPy2}
	case tokPy2Module:
		return ModuleUnit{Type: name, Tag: unitTagPy2Native, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy3Module:
		return ModuleUnit{Type: name, Tag: unitTagPy3Native, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy3ProgramBin:
		return ModuleUnit{Type: name, Tag: unitTagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	case tokPy3Test:
		return ModuleUnit{Type: name, Tag: unitTagPy3TestProgram, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3, TestLD: true}
	case tokUnittest, tokUnittestFor, tokGtest:
		return ModuleUnit{Type: name, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp, TestLD: true}
	case tokPy3Library:
		return ModuleUnit{Type: name, Tag: unitTagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy2Library:
		return ModuleUnit{Type: name, Tag: unitTagPy2, ARPrefix: "libpy", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy23Library:
		return ModuleUnit{Type: name, Tag: unitTagPy3, CCTag: tagPy3, ObjSuf: tagPy3, ARPrefix: "libpy3", GlobalARTag: tagPy3Global, SbomLang: unitSbomCpp}
	case tokPy23NativeLibrary:
		return ModuleUnit{Type: name, Tag: unitTagPy3, HashTag: unitTagPy3Native, CCTag: tagPy3Native, ObjSuf: tagPy3, ARPrefix: "libpy3c", GlobalARTag: tagPy3NativeGlobal, SbomLang: unitSbomCpp}
	case tokYqlPrebuiltTool:
		return ModuleUnit{Type: tokPrebuiltProgram, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokYqlPythonUdf:
		return ModuleUnit{Type: tokDll, Tag: unitTagPy2, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomPy2}
	case tokYqlPython3Udf:
		return ModuleUnit{Type: tokDll, Tag: unitTagPy3, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	case tokYqlUdf, tokYqlUdfContrib:
		if kind != KindLib {
			return ModuleUnit{Type: tokDll, Tag: strYqlUdfShared, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		fallthrough
	case tokYqlUdfYdb:
		return ModuleUnit{Type: name, Tag: strYqlUdfStatic, CCTag: tagYqlUdfStatic, HashTag: strYqlUdfStatic, ARPrefix: "lib", GlobalARTag: tagYqlUdfStaticGlobal, SbomLang: unitSbomCpp}
	case tokFbsLibrary:
		if language == LangFbsJavaNative {
			return ModuleUnit{Type: tokJarLibrary, Tag: strJavaFbs, CCTag: strJavaFbs2, NoStartTarget: true}
		}

		if language == LangPy {
			return ModuleUnit{Type: tokPy3Library, Tag: strPy3Fbs, ObjSuf: tagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		if language == LangGo {
			return ModuleUnit{Type: tokGoLibrary, Tag: strGoFbs, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		return ModuleUnit{Type: name, Tag: strCppFbs, CCTag: tagCppFbs, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokProtoLibrary:
		if language == LangProtoDescriptions {
			return resolveModuleUnitBare(tokProtoDescriptions, kind, language)
		}

		if language == LangGo {
			return ModuleUnit{Type: tokGoLibrary, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		if language == LangPy {
			return ModuleUnit{Type: name, Tag: unitTagPy3Proto, ObjSuf: tagPy3, ARPrefix: "libpy3", GlobalARTag: tagPy3ProtoGlobal, SbomLang: unitSbomCpp}
		}

		if language == LangPy2 {
			return ModuleUnit{Type: name, Tag: strPyProto, ObjSuf: tagPy2, ARPrefix: "libpy", GlobalARTag: strPyProtoGlobal, SbomLang: unitSbomCpp, NoStartTarget: true}
		}

		return ModuleUnit{Type: name, Tag: strCPPProto, CCTag: tagCppProto, ARPrefix: "lib", GlobalARTag: tagCppProtoGlobal, SbomLang: unitSbomCpp}
	}

	return ModuleUnit{Type: name, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
}

func (u ModuleUnit) objectSuffix(p *Platform) string {
	if u.ObjSuf == 0 {
		return p.objectSuffix()
	}

	return "." + u.ObjSuf.string() + p.objectSuffix()
}

func expandSourceArguments(args []ANY, env Environment) []ANY {
	var out []ANY

	for _, arg := range args {
		if !strings.ContainsRune(arg.string(), '$') {
			out = append(out, arg)

			continue
		}

		for _, word := range strings.Split(expandSourceScalarVarRef(arg.string(), env), " ") {
			if word != "" {
				out = append(out, internAny(word))
			}
		}
	}

	return out
}

func moduleStmtForKind(stmt *ModuleStmt, unit ModuleUnit) *ModuleStmt {
	if stmt.Name == unit.Type && unit.ModulePrefix == "" {
		return stmt
	}

	out := *stmt

	out.Name = unit.Type
	out.Prefix = unit.ModulePrefix

	return &out
}

func generatedIncludeDir(modulePath, dst string) string {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.relString())))

	if dir != "." && dir != "" {
		return filepath.ToSlash(filepath.Clean(dir))
	}

	return modulePath
}

func addGeneratedHeaderInclude(modulePath, dst string, d *ModuleData) {
	include := build(generatedIncludeDir(modulePath, dst))

	d.addLocalIncl(prioAddIncl, include)
	d.addInclGlobal = append(d.addInclGlobal, include)
	d.addInclUserGlobal = append(d.addInclUserGlobal, include)
}

func addGeneratedHeaderIncludeCF(modulePath, dst string, d *ModuleData) {
	include := build(generatedIncludeDir(modulePath, dst))

	d.cfAddIncl = append(d.cfAddIncl, include)
	d.cfAddInclGlobal = append(d.cfAddInclGlobal, include)
}

func addGeneratedOwnHeaderInclude(modulePath, dst string, d *ModuleData) {
	include := build(generatedIncludeDir(modulePath, dst))

	d.addLocalIncl(prioOutputAddIncl, include)
}

func appendRunProgram(v *RunProgramStmt, env Environment, d *ModuleData) {
	captured := captureRunProgram(v, env)

	captured.DeclSeq = d.nextDeclSeq()
	d.runPrograms = append(d.runPrograms, captured)
}

func captureRunProgram(v *RunProgramStmt, env Environment) *RunProgramStmt {
	if v.CallArgs == nil {
		return bindRunProgramFiles(v, env)
	}

	args := runProgramSourceInputArguments(v.CallArgs, env)

	if len(args) == 0 {
		throwFmt("RUN_PROGRAM expects at least 1 argument (tool path)")
	}

	captured := *v

	captured.CallArgs = args

	return &captured
}

func bindRunProgramFiles(v *RunProgramStmt, env Environment) *RunProgramStmt {
	expanded := *v

	expanded.ToolPath = expandStmtTokenAny(v.ToolPath, env)
	expanded.INFiles, expanded.INNoParse = expandInFiles(v.INFiles, v.INNoParse, env, true)
	expanded.OUTFiles = expandTokens(v.OUTFiles, env, false, tokenExpandArguments)
	expanded.OUTNoAutoFiles = expandTokens(v.OUTNoAutoFiles, env, false, tokenExpandArguments)
	expanded.OutputIncludes = expandTokens(v.OutputIncludes, env, false, tokenExpandArguments)
	expanded.InducedDeps = expandTokens(v.InducedDeps, env, false, tokenExpandArguments)
	expanded.ToolPaths = expandTokens(v.ToolPaths, env, false, tokenExpandArguments)

	if v.StdoutFile != nil {
		stdout := expandStmtTokenAny((*v.StdoutFile), env)

		expanded.StdoutFile = &stdout
	}

	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)

	if v.CallArgs == nil {
		expanded.Args = expandTokens(v.Args, env, true, tokenExpandArguments)
		expanded.EnvPairs = expandTokens(v.EnvPairs, env, false, tokenExpandArguments)
	}

	if v.CWD != nil {
		cwd := expandStmtTokenAny(*v.CWD, env)

		expanded.CWD = &cwd
	}

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)

	return &expanded
}

func warnUnmodelledMacro(onWarn func(Warn), modulePath string, name TOK) {
	onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q not modelled — implement its upstream semantics (see yatool/build/conf, yatool/build/ymake.core.conf); skipped", modulePath, name.string())})
}

func applyUnknownStmt(fs FS, modulePath string, v UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn)) bool {
	if applyRustMacro(v, modulePath, env, d) {
		return true
	}

	if dropsMacro(d, modulePath, v.macroName(), onWarn) {
		return true
	}

	if applyTestMacro(v, d, env) {
		return true
	}

	if d.nots != nil && applyNotsStmt(v, d.nots, env) {
		return true
	}

	if d.docs != nil && d.docs.apply(v, modulePath, d) {
		recordHandledMacro(v.Name, v.Args)

		return true
	}

	if v.Name == tokInvalid {
		return false
	}

	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name, v.Args)
		}
	}()

	switch v.Name {
	case tokMessage:
		args := v.Args

		if len(args) > 0 && args[0].string() == "STATUS" {
			args = args[1:]
		}

		if len(args) > 0 && args[0].string() == "FATAL_ERROR" {
			throwFmt("%s: %s", modulePath, joinAnys(args[1:], " "))
		}

		onWarn(Warn{Kind: WarnPlugin, Message: modulePath + ": " + joinAnys(args, " ")})
	case tokPireInline, tokPireInlineCmd:
		if v.Name == tokPireInlineCmd && len(v.Args) != 1 {
			throwFmt("PIRE_INLINE_CMD expects one source file")
		}

		for _, arg := range v.Args {
			d.pireInline = append(d.pireInline, DeclaredToken{Value: arg, DeclSeq: d.nextDeclSeq()})
		}
	case tokGenerateYtRecord:
		applyYtRecord(modulePath, v, d, env)
	case tokRunJavascriptAfterBuildProcessInputs:
		applyRunJavascriptAfterBuildProcessInputs(v.Args, env)
	case tokGolaunchSrcs, tokGolaunchSrcdir, tokRuntimePeerdir, tokResourcePeerdir, tokGolaunchFunction, tokGolaunchOverride, tokNoGolaunchSrcs, tokGenerateGolaunchCallbacks, tokGolaunchCompositeComps:
		applyMlaunchStmt(modulePath, v, env, d)
	case tokGlob, tokLateGlob:
		applyGlobStmt(fs, modulePath, v.Args, env, onWarn)
	case tokAddCustomExplicitLinterCheck:
		d.collectCustomStyle(modulePath, v.Args, env)
	case tokSdcDiagsSplitGeneratorV3:
		applySdcDiagsSplitGeneratorV3(fs, modulePath, v, env, d)
	case tokSdcDiagsSplitGeneratorV4:
		applySdcDiagsSplitGenerator(fs, modulePath, v, env, d)
	case tokNodeBuilder:
		if d.multimodule != tokMlaunchComponent || !isGoModuleType(d.unit.Type) {
			handled = false

			warnUnmodelledMacro(onWarn, modulePath, v.Name)
		} else {
			checkNodeBuilderOptions(v.Args)
		}
	case tokRosDir, tokRosPartial, tokRosSrcs, tokRosMsgCmd, tokRosZeroCopyMsgCmd, tokRosSrvCmd:
		applyRosStmt(modulePath, v, env, d)
	case tokTaskletReg, tokTaskletRegExt:
		applyTaskletRegistration(v, d, env)
	case tokSdcInstall:
		applySdcInstall(modulePath, v.Args, d, env)
	case tokGenPchFile:
		if len(v.Args) != 2 {
			throwFmt("GEN_PCH_FILE expects an input header and output file")
		}

		d.pchFiles = append(d.pchFiles, PchFileStmt{Header: v.Args[0], Output: v.Args[1], DeclSeq: d.nextDeclSeq()})

	case tokSdcResourceInfo, tokSdcSbResourceInfo, tokSdcRosnodeInfo, tokSdcSolibInfo:
		d.sdcMetas = append(d.sdcMetas, parseSdcMeta(v.Name, anyStrs(v.Args)))
	case tokFromArchive:
		if len(v.Args) == 0 {
			throwFmt("FROM_ARCHIVE expects a source archive")
		}

		options := parseFromSandbox(v.Args, v.Line)

		d.fromArchives = append(d.fromArchives, FromArchiveEntry{DeclSeq: d.nextDeclSeq(), Source: options.ResourceId, ArchiveExtraction: options.ArchiveExtraction})
	case tokDebVersion:
		if len(v.Args) != 1 {
			throwFmt("DEB_VERSION expects one changelog file")
		}

		d.runPython = append(d.runPython, &RunPythonStmt{
			ScriptPath: buildScriptsMkverPy.any(), Args: v.Args, INFiles: v.Args,
			StdoutFile: ptr(strDebianversionH.any()), KV: &debVersionKV, DeclSeq: d.nextDeclSeq(),
		})
	case tokFiles:
		d.addFiles(v.Args)
	case tokDeclareInDirs:
		applyDeclareInDirs(fs, modulePath, v, env)
	case tokYaToolsConf:
		if len(v.Args) != 1 {
			throwFmt("%s: YA_TOOLS_CONF expects one directory, got %d arguments", modulePath, len(v.Args))
		}

		for _, args := range yaToolsConfResourceFiles(fs, expandStmtTokenAny(v.Args[0], env).string()) {
			applyResourceFiles(fs, modulePath, args, v.Line, d)
		}
	case tokSetResourceUriFromJson:

		if len(v.Args) != 2 {
			throwFmt("gen: %s: SET_RESOURCE_URI_FROM_JSON expects 2 args (var json), got %d", modulePath, len(v.Args))
		}

		bundle := readResourceBundleJSON(fs, moduleSourceRel(modulePath, v.Args[1].string()))

		if uri, ok := resolveResourceURIFromBundle(bundle, env); ok {
			env.setString(internEnv(v.Args[0].string()), uri)
		}
	case tokUseCxx, tokUseUtil:
		if !isGoModuleType(d.unit.Type) {
			throwFmt("%s is not allowed for %s modules", v.Name.string(), d.unit.Type.string())
		}

		if len(v.Args) != 0 {
			throwFmt("%s expects no arguments", v.Name.string())
		}

		handled = false

		return false
	case tokBpf, tokBpfStatic:
		args := expandStmtTokens(v.Args, env)

		if len(args) < 2 {
			throwFmt("%s requires input and output", v.Name.string())
		}

		d.llvmCompile = append(d.llvmCompile, LlvmCompileStmt{Kind: v.Name, Source: args[0].string(), Output: args[1].string(), Options: args[2:], DeclSeq: d.nextDeclSeq()})
		d.peerdirs = append(d.peerdirs, strBuildPlatformClang.any())

		if v.Name == tokBpfStatic {
			d.peerdirs = append(d.peerdirs, argBuildPlatformLld.any())
		}
	case tokLlvmCompileCxx:
		if len(v.Args) < 2 || env.string(envCLANG_BC_ROOT) == "" {
			throwFmt("LLVM_COMPILE_CXX requires input, output and USE_LLVM_BC16/18/20")
		}

		d.llvmCompile = append(d.llvmCompile, LlvmCompileStmt{
			DeclSeq: d.nextDeclSeq(), Source: v.Args[0].string(), Output: v.Args[1].string(),
			ClangRoot: env.string(envCLANG_BC_ROOT), Options: v.Args[2:],
		})

		d.peerdirs = append(d.peerdirs, strBuildPlatformClang.any())
	case tokCompileLua, tokCompileLua21, tokCompileLuaOpenresty:
		if len(v.Args) != 1 && (len(v.Args) != 3 || v.Args[1].string() != "NAME") {
			throwFmt("%s expects a source and optional NAME", v.Name.string())
		}

		runtime := "contrib/libs/luajit"

		if v.Name == tokCompileLua21 {
			runtime += "_21"
		} else if v.Name == tokCompileLuaOpenresty {
			runtime += "_openresty"
		}

		stmt := LuaCompileStmt{Source: v.Args[0].string(), Runtime: runtime}

		if len(v.Args) == 3 {
			stmt.Name = v.Args[2].string()
		}

		stmt.DeclSeq = d.nextDeclSeq()
		d.luaCompile = append(d.luaCompile, stmt)
	case tokSplitDwarf:
		d.splitDwarf = true
	case tokNoSplitDwarf:
		d.splitDwarf = false
	case tokStylePython, tokStyleRuff:
		d.collectExplicitPythonStyle(v, env)
	case tokLlvmBc:
		stmt := &LlvmBcStmt{ClangBCRoot: env.string(envCLANG_BC_ROOT), LLCTool: env.string(envLLVM_LLC_TOOL), DeclSeq: d.nextDeclSeq()}
		i := 0

		for i < len(v.Args) {
			switch v.Args[i].string() {
			case "NAME":
				if i+1 >= len(v.Args) {
					throwFmt("LLVM_BC NAME expects a value")
				}

				stmt.Name = v.Args[i+1].string()
				i += 2
			case "SUFFIX":
				if i+1 >= len(v.Args) {
					throwFmt("LLVM_BC SUFFIX expects a value")
				}

				stmt.Suffix = v.Args[i+1].string()
				i += 2
			case "SYMBOLS":
				i++

				for i < len(v.Args) && !isLlvmBcKeyword(v.Args[i].string()) {
					stmt.Symbols = append(stmt.Symbols, v.Args[i].string())
					markUserArg(v.Args[i])
					i++
				}
			case "GENERATE_MACHINE_CODE":
				stmt.GenerateMachineCode = true
				i++
			case "NO_COMPILE":
				stmt.NoCompile = true
				i++
			default:
				stmt.Sources = append(stmt.Sources, v.Args[i].string())
				i++
			}
		}

		d.llvmBc = append(d.llvmBc, stmt)

		if !stmt.GenerateMachineCode {
			ensureResourcePeer(modulePath, d)
		}

	case tokBundleOutput, tokBundle:
		for _, entry := range parseBundleEntries(v) {
			entry.DeclSeq = d.nextDeclSeq()
			d.bundles = append(d.bundles, entry)
		}

	case tokGoTestSrcs:
		if d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			d.goTestSrcs = append(d.goTestSrcs, v.Args...)
		}
	case tokGoXtestSrcs:
		if d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			d.goXTestSrcs = append(d.goXTestSrcs, v.Args...)
		}
	case tokGoSkipTests:
		d.goSkipTests = append(d.goSkipTests, v.Args...)
	case tokGoEmbedPattern, tokGoTestEmbedPattern, tokGoXTestEmbedPattern:
		if v.Name == tokGoEmbedPattern || d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			importPath := modulePath

			if imp := env.expand(env.string(env_GO_IMPORT_PATH)); imp != "" {
				importPath = imp
			}

			d.goEmbeds = append(d.goEmbeds, newGoEmbed(fs, v.Name == tokGoXTestEmbedPattern, importPath, v.Args[0].string()))
		}
	case tokGoEmbedBindir, tokGoTestEmbedBindir, tokGoXtestEmbedBindir:
		if v.Name == tokGoEmbedBindir || d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			importPath := env.expand(env.string(env_GO_IMPORT_PATH))

			if importPath == "" {
				importPath = modulePath
			}

			d.goEmbeds = append(d.goEmbeds, newGoEmbedBindir(v.Name == tokGoXtestEmbedBindir, importPath, v.Args))
		}
	case tokGoEmbedDir, tokGoEmbedTestDir, tokGoEmbedXtestDir:
		if v.Name == tokGoEmbedDir || d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			args := anyStrs(v.Args)
			all := len(args) > 0 && args[0] == "ALL"

			if all {
				args = args[1:]
			}

			if len(args) != 1 {
				throwFmt("%s expects a directory", v.Name)
			}

			importPath := modulePath

			if imp := env.expand(env.string(env_GO_IMPORT_PATH)); imp != "" {
				importPath = imp
			}

			d.goEmbeds = append(d.goEmbeds, newGoEmbedDir(fs, v.Name == tokGoEmbedXtestDir, importPath, args[0], all))
		}
	case tokAllGoTestSrcs:
		dir := modulePath

		if imp := env.expand(env.string(env_GO_IMPORT_PATH)); imp != "" {
			dir = imp
		}

		for _, name := range goDirFiles(fs, dir, true) {
			src := source(dir + "/" + name).any()

			d.goFmtSrcs = append(d.goFmtSrcs, src)

			if d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
				d.goTestSrcs = append(d.goTestSrcs, src)
			}
		}
	case tokAllGoSrcs:
		dir := modulePath

		if imp := env.expand(env.string(env_GO_IMPORT_PATH)); imp != "" {
			dir = imp
		}

		for _, name := range goDirSources(fs, dir) {
			d.srcs = append(d.srcs, SrcMeta{Source: source(dir + "/" + name).any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq()})
		}
	case tokStyleCpp:
		d.collectCPPStyle(v, env)
		env.setBool(envUseAllSrcs, true)
		applyHeaders(fs, modulePath, []ANY{str4.any()}, d)

		if noLint := env.string(envNoLintValue); noLint != "none" && noLint != "none_internal" {
			d.peerdirs = append(d.peerdirs, strBuildPlatformClangFormat.any())
		}
	case tokHeaders:
		applyHeaders(fs, modulePath, v.Args, d)
	case tokStyleCppUserSessions, tokStyleCppYt, tokStyleCppMaps:
		d.collectCPPStyle(v, env)
		env.setBool(envUseAllSrcs, true)
		applyHeaders(fs, modulePath, []ANY{str4.any()}, d)
	case tokSetCustomClangTidy:
		if len(v.Args) != 2 {
			throwFmt("SET_CUSTOM_CLANG_TIDY expects a resource module and variable name")
		}

		env.setBool(envCustomClangTidy, true)

		root := "${" + v.Args[1].string() + "_RESOURCE_GLOBAL}/bin/"

		env.setString(envClangTidyBin, root+"clang-tidy")
		env.setString(envYaml2jsonBin, root+"yaml2json")
	case tokGoProtoPlugin:

		if !isGoModuleType(d.unit.Type) {
			break
		}

		positional, _ := goMacroLists(v.Args, "DEPS")

		d.goProtoOpts = append(d.goProtoOpts, GoProtoOpt{plugin: positional[0].string(), ext: positional[1].string(), tool: positional[2].string()})
	case tokYtOrmProtoYson:
		applyYtOrmProtoYson(modulePath, v.Args, d, env)
	case tokUsrvGenProtoStructs, tokUsrvGenGrpcServiceV2, tokUsrvGenGrpcClientV2,
		tokUsrvGenGrpcServiceV2Structs, tokUsrvGenGrpcClientV2Structs:
		applyUserverProto(v, d)
	case tokGoGrpcGatewayV2OpenapiSrcs, tokGoGrpcGatewayV2Srcs, tokGoGrpcGatewaySrcs, tokGoGrpcGatewaySwagger:
		applyGoGrpcGatewayStmt(v, d, env)
	case tokGoMockgenFrom, tokGoMockgenContribFrom, tokGoMockgenTypes, tokGoMockgenExcludeIfaces, tokGoMockgenPackage,
		tokGoMockgenReflect, tokGoMockgenMocks, tokGoMockgenSource, tokGoOapiCodegen, tokGoOapiCodegenV2, tokGoOapiCodegenTaxi, tokGoOapiCodegenTaxi1134:
		applyGoCodegenStmt(modulePath, v, d, env)
	case tokCgoSrcs:

		d.cgoSrcs = append(d.cgoSrcs, expandStmtTokens(v.Args, env)...)
	case tokCgoLdflags:
		expanded := expandStmtTokens(v.Args, env)

		d.cgoLdflags = append(d.cgoLdflags, expanded...)
		d.goLdflags = append(d.goLdflags, expanded...)
	case tokGoLdflags:
		d.goLdflags = append(d.goLdflags, expandStmtTokens(v.Args, env)...)
	case tokGoLinkFlags:
		d.goLinkFlags = append(d.goLinkFlags, expandStmtTokens(v.Args, env)...)
	case tokCgoCflags:

		expanded := expandStmtTokens(v.Args, env)

		d.cgoCflags = append(d.cgoCflags, expanded...)
		d.cFlags = append(d.cFlags, expanded...)
	case tokLicense:

		d.hasLicense = true
	case tokToolchain:

		if len(v.Args) == 1 {
			d.toolchainName = v.Args[0].string()
		}
	case tokCheckConfigH:
		if len(v.Args) != 1 {
			throwFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, DeclaredToken{Value: v.Args[0], DeclSeq: d.nextDeclSeq()})
	case tokDecimalMd5Lower32Bits:

		if len(v.Args) == 0 {
			throwFmt("gen: %s: DECIMAL_MD5_LOWER_32_BITS expects at least 1 argument (File)", modulePath)
		}

		stmt := &DecimalMD5Lower32BitsStmt{File: v.Args[0].string()}
		rest := v.Args[1:]

		if len(rest) >= 1 && rest[0] == kwFUNCNAME.any() {
			if len(rest) < 2 {
				throwFmt("gen: %s: DECIMAL_MD5_LOWER_32_BITS FUNCNAME requires a value", modulePath)
			}

			stmt.FuncName = rest[1].string()
			rest = rest[2:]
		}

		stmt.Opts = append([]ANY(nil), rest...)
		stmt.DeclSeq = d.nextDeclSeq()

		d.decimalMD5 = append(d.decimalMD5, stmt)
	case tokBuildwithCythonCpp:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: anyStrs(v.Args[1:]),
			Seq:     d.nextDeclSeq(),
		})
	case tokBuildwithCythonC:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_C expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: anyStrs(v.Args[1:]),
			CMode:   true,
			Seq:     d.nextDeclSeq(),
		})
	case tokBuildwithRagel6:
		args := expandStmtTokens(v.Args, env)

		if len(args) == 0 {
			throwFmt("BUILDWITH_RAGEL6 expects a source")
		}

		d.ragel6Builds = append(d.ragel6Builds, Ragel6BuildStmt{Src: args[0].string(), Options: args[1:], DeclSeq: d.nextDeclSeq()})
	case tokGrpc:
		if !d.grpc {
			d.grpcOutputIndex = len(d.cppProtoPlugins)
		}

		d.grpc = true

		if isPythonProtoModule(d) || isPythonModuleType(d.unit.Type) {
			d.pyProtoPlugins = append(d.pyProtoPlugins, PyProtoPlugin{
				Name: "grpc_py", ToolPath: argContribToolsProtocPluginsGrpcPython.string(),
				OutputSuffixes: []string{"_pb2_grpc.py"},
				Deps:           []ANY{strContribPythonGrpcio.any()},
			})
		}

		if !isGoModuleType(d.unit.Type) && !pythonProtoPeerContext(d) {
			d.peerdirs = append(d.peerdirs, strContribLibsGrpc.any())
		}
	case tokProtoToNamespace:
		collectProtoToNamespace(fs, modulePath, v, d, env, onWarn)
	case tokRegisterYqlPythonUdf:
		registerYqlPythonUdf(fs, modulePath, v, d, env, onWarn)
	case tokClangWarnings:
		d.clangWarnings = append(d.clangWarnings, expandStmtTokens(v.Args, env)...)
	case tokFbsNamespace:
		if len(v.Args) == 0 {
			throwFmt("FBS_NAMESPACE expects a namespace and optional path")
		}

		namespacePath := modulePath

		if len(v.Args) >= 2 {
			namespacePath = v.Args[1].string()
		}

		d.fbsNamespaces = append(d.fbsNamespaces, internV(v.Args[0].string(), "=", namespacePath).any())
	case tokProto2Fbs:
		if len(v.Args) != 1 {
			throwFmt("PROTO2FBS expects one proto input")
		}

		d.proto2Fbs = append(d.proto2Fbs, Proto2FbsStmt{Source: expandStmtTokenAny(v.Args[0], env), Seq: d.nextDeclSeq()})
		d.hasFbs = true
	case tokCopyFile, tokCopyFileWithContext:
		entry := parseCopyFileEntry(anyStrs(v.Args), v.Name == tokCopyFileWithContext, v.Line)

		entry.DeclSeq = d.nextDeclSeq()
		d.copyFiles = append(d.copyFiles, entry)

		if entry.Auto {
			dstVFS := copyFileOutputVFS(modulePath, entry.Dst)

			if isSourceEligibleForCopyAuto(dstVFS.relString()) {
				d.srcs = append(d.srcs, SrcMeta{Source: dstVFS.any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Auto: true})
			}
		}
	case tokCopy:
		entries, result := parseCopyEntries(anyStrs(v.Args), v.Line)

		for _, entry := range entries {
			entry.DeclSeq = d.nextDeclSeq()
			d.copyFiles = append(d.copyFiles, entry)

			if entry.Auto {
				dstVFS := copyFileOutputVFS(modulePath, entry.Dst)

				if isSourceEligibleForCopyAuto(dstVFS.relString()) {
					d.srcs = append(d.srcs, SrcMeta{Source: dstVFS.any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Auto: true})
				}
			}
		}

		if result != "" {
			targets := make([]string, len(entries))

			for i, entry := range entries {
				targets[i] = entry.Dst
			}

			value := strings.Join(targets, " ")

			env.setFromString(internEnv(result), value)

			d.setVars.set(internStr(result), internStr(value))
		}
	case tokProtoNamespace:
		if len(v.Args) == 0 {
			throwFmt("gen: PROTO_NAMESPACE expects at least 1 argument")
		}

		d.protoNamespace = ptr(v.Args[len(v.Args)-1])
	case tokUseSdcMeta:

		d.setVars.set(strUSE_SDC_META, strYes)
	case tokGoPackageName:

		d.setVars.set(str_GO_PACKAGE_VALUE, v.Args[0].str())
		env.setStringID(envGoPackageOverride, v.Args[0].str())
	case tokExportYmapsProto:
		d.protoNamespace = ptr(mapsDocProtoNS.any())
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, mapsDocProto)
	case tokYmapsSproto:
		if env.bool(envGO_PROTO) {
			break
		}

		for _, argTok := range v.Args {
			if !extIsProto(argTok.string()) {
				continue
			}

			d.ymapsSprotoSrcs = append(d.ymapsSprotoSrcs, DeclaredToken{Value: argTok, DeclSeq: d.nextDeclSeq()})
		}

		if len(d.ymapsSprotoSrcs) > 0 {
			d.peerdirs = append(d.peerdirs, strMapsLibsSproto.any())
		}
	case tokListProto:
		stmt := ListProtoStmt{Output: strFilesProto.any()}

		for i := 0; i < len(v.Args); i++ {
			if v.Args[i].string() == "TO" && i+1 < len(v.Args) {
				i++
				stmt.Output = v.Args[i]
			} else {
				stmt.Files = append(stmt.Files, v.Args[i])
			}
		}

		stmt.DeclSeq = d.nextDeclSeq()
		d.listProtos = append(d.listProtos, stmt)
	case tokOnlyTags, tokIncludeTags, tokExcludeTags:
		if d.multimodule != 0 {
			applyMultimoduleVariantTags(v, d)
		}

		if d.moduleStmt != nil && (d.moduleStmt.Name == tokProtoLibrary || env.bool(envGO_PROTO)) {
			applyMultimoduleVariantTags(v, d)
		}

		if d.excludeTags == nil {
			d.excludeTags = make(map[STR]bool)
		}

		if v.Name == tokOnlyTags {
			clear(d.excludeTags)
			d.onlyTags = true
		}

		for _, argTok := range v.Args {
			d.excludeTags[argTok.str()] = v.Name == tokExcludeTags
		}
	case tokYaConfJson:
		if len(v.Args) != 1 {
			throwFmt("YA_CONF_JSON expects exactly 1 argument, got %d", len(v.Args))
		}

		if len(d.yaConfJSON) == 0 {
			d.yaConfDeclSeq = d.nextDeclSeq()
		}

		d.yaConfJSON = append(d.yaConfJSON, v.Args[0])
	case tokArchive:

		applyArchiveStmt(v, d)
	case tokArchiveByKeys:
		applyArchiveByKeysStmt(v, d)
	case tokArchiveAsm:
		applyArchiveAsmStmt(v, d)
	case tokLj21Archive:
		applyLj21ArchiveStmt(v, d)
	case tokEnable:

		for _, aTok := range v.Args {
			a := aTok.string()

			env.setBool(internEnv(a), true)
			d.captureResourcePeerPolicy(a, env)

			d.setVars.set(internStr(a), strYes)

			switch a {
			case "SANITIZER_TYPE":
				setupPythonImportMask(env, d)
			case "MUSL_LITE":
				d.muslLite = true
			case "PYBUILD_NO_PYC":
				d.pyBuildNoPYC = true
			case "PYBUILD_NO_PY":
				d.pyBuildNoPY = true
			case "PYTHON_SQLITE3":
				d.pythonSQLite3 = true
			case "USE_LIGHT_PY2CC":
				d.useLightPy2cc = true
			}
		}
	case tokDisable:

		for _, aTok := range v.Args {
			a := aTok.string()

			env.setBool(internEnv(a), false)
			d.captureResourcePeerPolicy(a, env)

			if a == "SANITIZER_TYPE" {
				setupPythonImportMask(env, d)
			}

			d.setVars.set(internStr(a), strNo)

			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
			}

			if a == "USE_LIGHT_PY2CC" {
				d.useLightPy2cc = false
			}

			if a == "NEED_GOOGLE_PROTO_PEERDIRS" {
				d.needGoogleProtoPeerdirs = false
			}
		}
	case tokExplicitOutputs:
		d.unionExplicitOutputs = append(d.unionExplicitOutputs, expandSourceArguments(v.Args, env)...)
	case tokSrc:

		if len(v.Args) == 0 {
			throwFmt("gen: SRC() requires at least 1 argument (filename); got 0 at line %d", v.Line)
		}

		filename := v.Args[0]

		var extras []ANY

		if len(v.Args) > 1 {
			extras = v.Args[1:]

			for _, flag := range extras {
				markUserArg(flag)
			}
		}

		d.srcs = append(d.srcs, SrcMeta{Source: filename, Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Compile: CompileSpec{
			CFlags:          extras,
			FlatOutput:      true,
			PyRegisterLimit: len(d.pyRegister) + 1,
		}})
	case tokSrcCNoLto:

		if len(v.Args) != 1 {
			throwFmt("gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got %d at line %d", len(v.Args), v.Line)
		}

		filename := v.Args[0]

		d.srcs = append(d.srcs, SrcMeta{Source: filename, Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Compile: CompileSpec{FlatOutput: true, PyRegisterLimit: len(d.pyRegister) + 1}})
	case tokSrcCAvx, tokSrcCAvx2, tokSrcCAvx512, tokSrcCAmx, tokSrcCSse2, tokSrcCSse3, tokSrcCSsse3,
		tokSrcCSse4, tokSrcCSse41, tokSrcCXop:

		variant, _ := simdVariantFor(v.Name)

		if len(v.Args) == 0 {
			throwFmt("gen: %s() requires at least 1 argument (filename); got 0 at line %d", v.Name, v.Line)
		}

		filename := v.Args[0]
		flags := concat(variant.CFlags, v.Args[1:])

		d.srcs = append(d.srcs, SrcMeta{Source: filename, Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Compile: CompileSpec{
			CFlags:          flags,
			FlatOutput:      true,
			Variant:         variant.Suffix,
			PyRegisterLimit: len(d.pyRegister) + 1,
		}})
	case tokBuildMn:

		if len(v.Args) != 2 {
			handled = false

			onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: BUILD_MN with options is not modelled, expected (MnInfo MnName), got %d args; skipped", modulePath, len(v.Args))})

			return true
		}

		d.buildMns = append(d.buildMns, &BuildMnStmt{Info: v.Args[0], Name: v.Args[1].string(), Seq: d.nextDeclSeq()})
	case tokLdPlugin:

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case tokExportsScript:
		if len(v.Args) != 1 {
			throwFmt("gen: EXPORTS_SCRIPT expects exactly 1 argument, got %d", len(v.Args))
		}

		d.exportsScript = ptr(v.Args[0])
	case tokLinkerScript:
		for _, arg := range expandStmtTokens(v.Args, env) {
			if !strings.HasSuffix(arg.string(), ".ld") {
				throwFmt("%s: LINKER_SCRIPT %s: only .ld scripts are modelled", modulePath, arg.string())
			}

			if isProgramModuleType(d.moduleStmt.Name) {
				d.linkerScripts = append(d.linkerScripts, arg)
			} else {
				d.srcs = append(d.srcs, SrcMeta{Source: arg, Prio: stmtPrioSrcs, Seq: d.nextDeclSeq(), Global: true})
			}
		}
	case tokExtralibs:
		libs := ""

		if len(d.objAddLibsGlobal) > 0 {
			libs = d.objAddLibsGlobal[0].string()
		}

		changed := false

		for _, argTok := range v.Args {
			lib := argTok.string()

			if !strings.HasPrefix(lib, "-") {
				lib = "-l" + lib
			}

			if !strings.Contains(libs, lib) {
				libs += " " + lib
				changed = true
			}
		}

		if changed {
			d.objAddLibsGlobal = append(d.objAddLibsGlobal[:0], internArg(libs).any())
		}
	case tokUseSAPlugins:
		applySAPlugins(v.Args, d, env)
	case tokNoBuildIf, tokBuildOnlyIf:
		strict, args := true, v.Args

		if len(args) > 0 {
			switch args[0].string() {
			case "WARNING":
				strict = false
				args = args[1:]
			case "STRICT", "FATAL_ERROR":
				args = args[1:]
			}
		}

		enabled := false

		for _, arg := range args {
			enabled = enabled || arg.str() != 0 && env.bool(internEnvSTR(arg.str()))
		}

		if strict && enabled == (v.Name == tokNoBuildIf) {
			d.discarded = true
		}
	case tokUsrvStyleJSON, tokUsrvStyleYAML:
		v.Args = nil

		if v.Name == tokUsrvStyleJSON {
			v.Name = tokStyleJSON
		} else {
			v.Name = tokStyleYAML
		}

		d.collectStyleCheck(fs, modulePath, v, env)
	case tokStyleJSON, tokStyleYAML, tokStyleYQL:
		d.collectStyleCheck(fs, modulePath, v, env)
	case tokStyleDetekt:
		noLint, _ := env.lookup("_NO_LINT_VALUE")
		tidy, _ := env.lookup("TIDY")

		if noLint != "none" && noLint != "none_internal" && tidy != "yes" {
			d.peerdirs = append(d.peerdirs, internStr(javaDetekt.resourcePeer).any())
		}
	case tokStyleYAMLConf:
		d.collectYAMLStyleWithConfig(modulePath, v, env)
	case tokCollectYAML:
		files := collectStyleFiles(fs, modulePath, v)

		for i, file := range files {
			files[i] = source(file).string()
		}

		env.setString(envYamlLintSrcs, strings.Join(files, " "))
	case tokUsrvBuild:
		d.collectUsrvFiles(anyStrs(v.Args))
	case tokUsrvAllSrcs:
		for _, f := range usrvAllSources(fs, modulePath, anyStrs(v.Args)) {
			d.srcs = append(d.srcs, SrcMeta{Source: internStr(f).any(), Prio: stmtPrioSrcs, Seq: d.nextDeclSeq()})
		}
	case tokTestSrcs:
		args := v.Args
		moduleRelative := len(args) > 0 && args[0].string() == "_MR"

		if moduleRelative {
			args = args[1:]
		}

		for _, a := range args {
			if a.string() == "NAMESPACE" || a.string() == "TOP_LEVEL" || a.string() == "__main__.py" {
				throwFmt("TEST_SRCS: %s is not allowed", a.string())
			}
		}

		if moduleRelative {
			relative := make([]ANY, len(args))

			for i, arg := range args {
				if file, rooted := moduleRootedVFS(modulePath, arg.string()); rooted {
					arg = internStr(trimModulePrefix(file.relString(), modulePath)).any()
				}

				relative[i] = arg
			}

			args = relative
		}

		d.test.testFiles = append(d.test.testFiles, anyStrs(args)...)

		before := len(d.pySrcGroups)

		applyInlinePySrcs(fs, modulePath, append([]ANY{kwNAMESPACE.any(), strTests.any()}, args...), d, env, onWarn)

		if moduleRelative {
			for i := before; i < len(d.pySrcGroups); i++ {
				d.pySrcGroups[i].SourceRoot = true
			}
		}
	case tokPySrcs:

		env.setDefaultString(envMODULE_TAG, "PY3")

		stmt := v

		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{DeclSeq: d.nextDeclSeq(), Statement: astOne(astUnknowns, stmt), ResourceOffset: len(d.resources), Prio: stmtPrioSrcs})
	case tokAllPytestSrcs:

		applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokTestSrcs, Args: allPytestSrcArgs(fs, modulePath, v)}, d, env, onWarn)
	case tokAllPySrcs:

		applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokPySrcs, Args: allPySrcArgs(fs, modulePath, v)}, d, env, onWarn)
	case tokPyMain:

		if len(v.Args) != 1 {
			throwFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}

		arg := strings.ReplaceAll(v.Args[0].string(), "/", ".")

		if !strings.Contains(arg, ":") {
			arg += ":main"
		}

		d.pyMain = ptr(internStr(arg).any())
		d.pyMainDeclSeq = d.nextDeclSeq()

		collectPythonSourceEntry(d, env, &d.macroPeerdirs)
	case tokPyProgramPlugin:
		pyProgram(d, env)
	case tokRegisterSandboxImport:

		for _, arg := range v.Args {
			registerSandboxImport(modulePath, arg.string(), d)
		}
	case tokPyConstructor:

		ensureResourcePeer(modulePath, d)

		if len(v.Args) != 1 {
			throwFmt("gen: PY_CONSTRUCTOR expects exactly 1 argument, got %d", len(v.Args))
		}

		arg := v.Args[0].string()

		if strings.Contains(arg, ":") {
			arg = strings.Replace(arg, ":", "=", 1)
		} else {
			arg += "=init"
		}

		d.resources = append(d.resources, ResourceEntry{Path: "-", Key: "py/constructors/" + arg, DontCompress: true, DeclSeq: d.nextDeclSeq()})
	case tokNoCheckImports:

		value := joinAnys(v.Args, " ")

		env.setString(internEnv(importMaskVariable), value)
		setPythonImportMask(value, d)
		d.setVars.set(internStr(importMaskVariable), internStr(value))
	case tokCppProtoPlugin0, tokCppProtoPlugin, tokCppProtoPlugin2, tokCppEvProtoPlugin:
		addCPPProtoPlugin(d, parseCPPProtoPlugin(v))
	case tokTasklet:
		if len(v.Args) != 0 {
			throwFmt("TASKLET expects no arguments, got %d", len(v.Args))
		}

		applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokCppProtoPlugin, Args: internAnys([]string{
			"tasklet_cpp", "tasklet/v1/gen/cpp", ".tasklet.h",
		})}, d, env, onWarn)

		applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokPyProtoPlugin2, Args: internAnys([]string{
			"tasklet_py", "_tasklet.py", "_sbtask.py", "tasklet/v1/gen/python", "DEPS",
			"tasklet/v1/domain/sandbox", "tasklet/v1/runtime", "sandbox/sdk2",
		})}, d, env, onWarn)
	case tokPyProtoPlugin, tokPyProtoPlugin2:
		if isPythonProtoModule(d) || isPythonModuleType(d.unit.Type) {
			d.pyProtoPlugins = append(d.pyProtoPlugins, parsePyProtoPlugin(v))
		}
	case tokWithKotlinGrpc:
	case tokAliceGenerateProtoIncludes, tokAliceGenerateFunctionProtoIncludes:
		applyAliceProtoIncludes(fs, modulePath, v, d)
	case tokCompileNlg:
		applyCompileNlg(modulePath, v, d)
	case tokGenerateTopology:
		_, lists := goMacroLists(v.Args, "ROS_MESSAGE_HEADERS")

		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:           "golaunch_generator",
			ToolPath:       "sdg/sdc/ros/configuration/golaunch/topology/generator",
			OutputSuffixes: []string{".topology.pb.h"},
			Deps:           []string{"sdg/sdc/ros/configuration/golaunch/topology/common_deps"},
			ExtraOutFlag:   joinAnys(lists["ROS_MESSAGE_HEADERS"], ","),
		})
	case tokAliceCapability:
		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:           "alice_capability_cpp",
			ToolPath:       "yandex_io/tools/capability_gen",
			OutputSuffixes: []string{".cap.h"},
			Deps:           confPluginDeps(fs, "build/internal/conf/project_specific/alice.conf", "ALICE_CAPABILITY"),
		})
	case tokApphost:
		itemDispatcher := ""
		itemDispatcherHeader := ""

		for i := 0; i < len(v.Args); i++ {
			switch v.Args[i].string() {
			case "ITEM_DISPATCHER":
				i++

				if i < len(v.Args) {
					itemDispatcher = v.Args[i].string()
				}
			case "ITEM_DISPATCHER_HEADER":
				i++

				if i < len(v.Args) {
					itemDispatcherHeader = v.Args[i].string()
				}
			default:
				throwFmt("gen: %s: APPHOST: unexpected argument %q", modulePath, v.Args[i].string())
			}
		}

		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:           "cpp_plugin",
			ToolPath:       "apphost/tools/stub_generator/cpp_plugin",
			OutputSuffixes: []string{".apphost.h"},
			Deps:           []string{"apphost/tools/stub_generator/cpp_includes"},
			ExtraOutFlag:   "item_dispatcher=" + itemDispatcher + ",item_dispatcher_header=" + itemDispatcherHeader,
		})
	case tokCppEvlog:

		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:     event2cppPluginName,
			ToolPath: event2cppToolPath,
			Deps:     []string{strLibraryCppEventlog.string()},
		})
	case tokYaff:
		conf := confProtoPlugin(fs, yabsConfRel, "YAFF")
		plugin := parseYAFF(v, conf.namespace)

		plugin.Deps = conf.deps

		if !isGoModuleType(d.unit.Type) {
			d.peerdirs = append(d.peerdirs, internAnys(plugin.Deps)...)
		}

		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	case tokYaffSchema:
		plugin := parseYAFFSchema(v)

		plugin.Deps = confPluginDeps(fs, yabsConfRel, "YAFF_SCHEMA")

		if !isGoModuleType(d.unit.Type) {
			d.peerdirs = append(d.peerdirs, internAnys(plugin.Deps)...)
		}

		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	case tokPyRegister:

		for _, nameTok := range v.Args {
			name := nameTok.string()

			appendPyRegister(d, name, true)
		}
	case tokSetListPreSuf:
		if len(v.Args) < 3 {
			throwFmt("gen: SET_LIST_PRE_SUF expects an output, prefix and suffix (line %d)", v.Line)
		}

		prefix, suffix := v.Args[1].string(), v.Args[2].string()

		var buf [256]byte

		joined := buf[:0]

		for i, value := range v.Args[3:] {
			if i > 0 {
				joined = append(joined, ' ')
			}

			joined = append(append(append(joined, prefix...), value.string()...), suffix...)
		}

		valueID := internBytes(joined)
		name := v.Args[0].str()

		env.setFromSTR(internEnvSTR(name), valueID)

		d.setVars.set(name, valueID)
	case tokSetAppend:
		if len(v.Args) >= 1 {
			nameID := v.Args[0].str()

			switch nameID {
			case strGoProtoOpts:
				for _, a := range v.Args[1:] {
					d.goProtoOpts = append(d.goProtoOpts, GoProtoOpt{arg: a})
				}
			case strSflags:
				d.sFlags = append(d.sFlags, v.Args[1:]...)
			case strUserCflags:

				d.cFlags = append(d.cFlags, v.Args[1:]...)
			case strUserCxxflags:
				d.cxxFlags = append(d.cxxFlags, v.Args[1:]...)
			case strProtocFlags:
				d.protocFlags = append(d.protocFlags, v.Args[1:]...)
			case strRpathGlobal:
				for _, argTok := range v.Args[1:] {
					arg := argTok.string()

					arg = strings.ReplaceAll(arg, `${"$"}`, "$")
					d.rpathFlagsGlobal = append(d.rpathFlagsGlobal, internArg(arg).any())
				}
			}

			nameEnv := internEnvSTR(nameID)
			args := v.Args[1:]

			if nameID == strTestEnvValue {
				args = []ANY{expandSourceScalarVarRefSTR(joinAnysSTR(args, " ").string(), env).any()}
			}

			if nameID == importMaskVariableSTR {
				env.setFromString(nameEnv, appendPythonImportMask(joinAnysSTR(args, " ").string(), env, d))
			} else {
				env.appendANYs(nameEnv, args)
			}

			if nameID == strPassPeers {
				d.captureResourcePeerPolicy("PASS_PEERS", env)
			}

			if nameID == strSanitizerType {
				setupPythonImportMask(env, d)
			}

			d.setVars.append(nameID, args)
		}
	case tokInducedDeps:

		if len(v.Args) >= 2 {
			toHeader := v.Args[0].string() != "cpp"
			toCpp := v.Args[0].string() != "h"

			for _, pTok := range v.Args[1:] {
				dir := IncludeDirective{kind: includeQuoted, target: includeTarget(pTok)}

				if toHeader {
					d.inducedDeps = appendParsedDirectives(d.inducedDeps, parsedIncludesHeader, dir)
				}

				if toCpp {
					d.inducedDeps = appendParsedDirectives(d.inducedDeps, parsedIncludesCpp, dir)
				}
			}
		}
	case tokGenerateImplib:
		implib := parseImplib(v.Args)

		implib.DeclSeq = d.nextDeclSeq()

		d.implibs = append(d.implibs, implib)
		d.linkExcludeLibraries = append(d.linkExcludeLibraries, v.Args[0])
	case tokLinkExcludeLibraries:
		d.linkExcludeLibraries = append(d.linkExcludeLibraries, v.Args...)
	case tokCudaSrcs:
		arches := env.string(envCUDA_ARCHITECTURES)

		if arches == "" {
			arches = "sm_50"
		}

		d.cudaSrcs = append(d.cudaSrcs, CudaSrcsStmt{DeclSeq: d.nextDeclSeq(), Sources: v.Args, Architectures: arches, EmbedPTX: !env.bool(envCUDA_NO_EMBED_PTX)})
	case tokNvccDeviceLink:
		d.nvccDeviceSrcs = append(d.nvccDeviceSrcs, v.Args...)
	default:

		handled = false

		if !acknowledgedTokSet.has(uint32(v.Name)) {
			return false
		}

		recordIgnoredMacro(v.Name)
	}

	return true
}

type LlvmBcStmt struct {
	DeclSeq             int
	Sources             []string
	Name                string
	Suffix              string
	Symbols             []string
	GenerateMachineCode bool
	ClangBCRoot         string
	LLCTool             string
	NoCompile           bool
}

func isLlvmBcKeyword(s string) bool {
	switch s {
	case "NAME", "SUFFIX", "SYMBOLS", "GENERATE_MACHINE_CODE", "NO_COMPILE":
		return true
	}

	return false
}

type pyxDeclaration struct {
	stmt   *CythonStmt
	module string
}

func cythonVariantBucket(s *CythonStmt) int {
	switch {
	case s.CMode && !s.Header:
		return 0
	case s.CMode && s.Header && !s.ApiHeader:
		return 1
	case s.CMode && s.Header && s.ApiHeader:
		return 2
	case !s.CMode && !s.Header:
		return 3
	case s.ApiHeader:
		return 5
	default:
		return 4
	}
}

func appendPyRegister(d *ModuleData, name string, renameInit bool) {
	d.pyRegister = append(d.pyRegister, PyRegistration{Name: internStr(name), RenameInit: renameInit, DeclSeq: d.nextDeclSeq()})
}

func finishPyRegisterCFlags(d *ModuleData) {
	if len(d.pyRegister) == 0 {
		return
	}

	for i := range d.pyRegister {
		registration := &d.pyRegister[i]

		registration.CFlagsStart = len(d.cFlags)

		name := registration.Name.string()
		dot := strings.LastIndexByte(name, '.')

		if !registration.RenameInit || dot < 0 {
			continue
		}

		shortname := name[dot+1:]
		mangled := pythonInitSuffix(name)
		initDefine := "-DPyInit_" + shortname + "=PyInit_" + mangled

		if d.usePython2 {
			initDefine = "-Dinit" + shortname + "=init" + mangled
		}

		d.cFlags = append(d.cFlags,
			internArg(initDefine).any(),
			internArg("-Dinit_module_"+shortname+"=init_module_"+mangled).any(),
		)
	}

	for i := range d.srcs {
		compile := &d.srcs[i].Compile
		limit := compile.PyRegisterLimit - 1

		if limit < 0 || limit >= len(d.pyRegister) {
			continue
		}

		end := d.pyRegister[limit].CFlagsStart

		if end == len(d.cFlags) {
			continue
		}

		compile.EnvCFlags = d.cFlags[:end:end]
	}
}

func parseCPPProtoPlugin(v UnknownStmt) CppProtoPlugin {
	requiredArgs := 0
	outputSuffixes := 0

	switch v.Name {
	case tokCppProtoPlugin0:
		requiredArgs = 2
	case tokCppProtoPlugin, tokCppEvProtoPlugin:
		requiredArgs = 3
		outputSuffixes = 1
	case tokCppProtoPlugin2:
		requiredArgs = 4
		outputSuffixes = 2
	}

	if len(v.Args) < requiredArgs {
		throwFmt("gen: %s expects at least %d arguments, got %d", v.Name, requiredArgs, len(v.Args))
	}

	plugin := CppProtoPlugin{
		Name:          v.Args[0].string(),
		ToolPath:      v.Args[1].string(),
		ApplyToEvents: v.Name == tokCppEvProtoPlugin,
	}

	tail := 2

	if outputSuffixes > 0 {
		plugin.OutputSuffixes = append(plugin.OutputSuffixes, anyStrs(v.Args[tail:tail+outputSuffixes])...)
		tail += outputSuffixes
	}

	for tail < len(v.Args) {
		switch v.Args[tail].string() {
		case "DEPS":
			tail++

			for tail < len(v.Args) && v.Args[tail].string() != "EXTRA_OUT_FLAG" {
				plugin.Deps = append(plugin.Deps, v.Args[tail].string())
				tail++
			}
		case "EXTRA_OUT_FLAG":
			tail++

			if tail >= len(v.Args) {
				throwFmt("gen: %s EXTRA_OUT_FLAG expects exactly 1 argument", v.Name)
			}

			if plugin.ExtraOutFlag != "" {
				throwFmt("gen: %s repeated EXTRA_OUT_FLAG", v.Name)
			}

			plugin.ExtraOutFlag = v.Args[tail].string()
			tail++
		default:
			throwFmt("gen: %s got unexpected tail token %q; supported suffixes are DEPS and EXTRA_OUT_FLAG", v.Name, v.Args[tail].string())
		}
	}

	return plugin
}

type YaffSections struct {
	positional   []string
	namespace    string
	files        []string
	experimental []string
}

func parseYAFFSections(v UnknownStmt) YaffSections {
	var s YaffSections

	section := STR(0)

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
		case kwNAMESPACE.any():
			i++

			if i >= len(v.Args) {
				throwFmt("gen: %s NAMESPACE expects a value", v.Name)
			}

			s.namespace = v.Args[i].string()
			markUserArg(v.Args[i])
			section = STR(0)
		case kwFILES.any():
			section = kwFILES
		case kwEXPERIMENTAL.any():
			section = kwEXPERIMENTAL
		default:
			markUserArg(a)

			switch section {
			case kwFILES:
				s.files = append(s.files, a.string())
			case kwEXPERIMENTAL:
				s.experimental = append(s.experimental, a.string())
			default:
				s.positional = append(s.positional, a.string())
			}
		}
	}

	return s
}

func yaffExtraOutFlag(lead string, s YaffSections) string {
	groups := []string{
		lead,
		strings.Join(prefixEach("file=", s.files), ","),
		strings.Join(prefixEach("experimental=", s.experimental), ","),
	}

	return strings.Join(groups, ",")
}

func parseYAFF(v UnknownStmt, defaultNamespace string) CppProtoPlugin {
	s := parseYAFFSections(v)

	if !slices.Contains(v.Args, kwNAMESPACE.any()) {
		s.namespace = defaultNamespace
	}

	if len(s.positional) != 0 {
		throwFmt("gen: YAFF got unexpected positional argument %q", s.positional[0])
	}

	return CppProtoPlugin{
		Name:           "yaff",
		ToolPath:       yaffPluginPath,
		OutputSuffixes: []string{".yaff.h", ".yaff.cpp"},
		ExtraOutFlag:   yaffExtraOutFlag("namespace="+s.namespace, s),
	}
}

func parseYAFFSchema(v UnknownStmt) CppProtoPlugin {
	s := parseYAFFSections(v)

	if len(s.positional) < 1 {
		throwFmt("gen: YAFF_SCHEMA expects SCHEMA_NAME, got %d positional args", len(s.positional))
	}

	schemaName := s.positional[0]
	namespace := s.namespace

	if len(s.positional) >= 2 {
		namespace = s.positional[1]
	}

	if len(s.positional) > 2 {
		throwFmt("gen: YAFF_SCHEMA got unexpected positional argument %q", s.positional[2])
	}

	lead := "tag=" + schemaName + ",namespace=" + namespace

	return CppProtoPlugin{
		Name:           "yaff_" + schemaName,
		ToolPath:       yaffPluginPath,
		OutputSuffixes: []string{"_" + schemaName + ".yaff.h", "_" + schemaName + ".yaff.cpp"},
		ExtraOutFlag:   yaffExtraOutFlag(lead, s),
	}
}

func moduleAssignsLang(name TOK) bool {
	switch name {
	case tokCiGroup, tokGenLibrary, tokExectest, tokResourcesLibrary, tokPackageFinal:
		return false
	}

	return !isUnionModule(name)
}

func pythonSourceModuleType(name TOK) string {
	if name == tokGoDll || isDllModuleType(name) {
		return "DLL"
	}

	if isProgramModuleType(name) || name == tokSoProgram {
		return "PROGRAM"
	}

	return ""
}

func confModuleType(name TOK, d *ModuleData) string {
	if name == tokGoLibrary || d.nots != nil {
		return "LIBRARY"
	}

	return pythonSourceModuleType(d.unit.Type)
}

func pyProgram(d *ModuleData, env Environment) {
	if env.string(envUSE_ARCADIA_PYTHON) != "yes" {
		return
	}

	sqlite := env.string(envPYTHON_SQLITE3) != "no"

	if env.string(envPYTHON2) == "yes" {
		d.peerdirs = append(d.peerdirs, strLibraryPythonRuntimeMain.any())

		if sqlite {
			d.peerdirs = append(d.peerdirs, strContribToolsPythonSrcModulesSqlite.any())
		}

		return
	}

	d.peerdirs = append(d.peerdirs, strLibraryPythonRuntimePy3Main.any())

	if sqlite {
		d.peerdirs = append(d.peerdirs, internV(env.string(envPYTHON3_TOOL_PEERDIR), "/Modules/_sqlite").any())
	}
}

func collectPythonSourceEntry(d *ModuleData, env Environment, peers *[]ANY) {
	d.pythonSourceCallback = true

	moduleType := env.string(envMODULE_TYPE)

	if isPyProgramType(d.moduleStmt.Name) || moduleType != "PROGRAM" && moduleType != "DLL" {
		return
	}

	d.pythonSourceEntry = true
	d.pythonSourceProgram = moduleType == "PROGRAM"

	if env.string(envUSE_ARCADIA_PYTHON) == "yes" {
		mainPeer := "library/python/runtime_py3/main"
		sqlitePeer := "contrib/tools/python3"

		if d.usePython2 {
			mainPeer = "library/python/runtime/main"
			sqlitePeer = "contrib/tools/python/src"
		} else if value, ok := env.lookup("PYTHON3_TOOL_PEERDIR"); ok {
			sqlitePeer = value
		} else if env.string(envUSE_PYTHON3_PREV) == "yes" {
			sqlitePeer = "contrib/tools/python3_prev"
		}

		*peers = append(*peers, internStr(mainPeer).any())

		if d.pythonSQLite3 {
			*peers = append(*peers, internV(sqlitePeer, "/Modules/_sqlite").any())
		}
	}

	if d.pythonSourceProgram && !d.noCheckImportsDisabled && !env.bool(envCppAnalysisMode) {
		*peers = append(*peers, strLibraryPythonTestingImportTest.any())
	}
}

func collectPySrcGroup(fs FS, modulePath string, v UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn), group int) {
	if isGoModuleType(d.unit.Type) && env.bool(envGO_PROTO) {
		return
	}

	collectPythonSourceEntry(d, env, &d.peerdirs)

	v.Args = expandRootDeferredTokens(v.Args, env, tokenExpandPathArguments)

	topLevel := false
	mainNext := false
	cythonizePy := false
	cythonPlainCpp := false
	cythonCMode := false
	cythonHeader := false
	cythonApiHeader := false
	swigCMode := false

	var namespace *ANY

	if value := env.string(envPyNamespaceValue); value != "" {
		namespace = ptr(internStr(value).any())
	}

	if namespace != nil && namespace.string() == "." {
		topLevel = true
		namespace = nil
	}

	srcStart, scopeStart := len(d.pyGroupSrcs), len(d.pyGroupScopes)
	lintPy := false
	cythonStmtStart := len(d.cythonCpp)
	pyiBefore := len(d.pyPyiResources)

	var cythonDirectives []string

	var pyxs []pyxDeclaration

	var swigs []SwigSrc

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
		case kwGLOBAL.any():
			continue
		case kwTOP_LEVEL.any():
			topLevel = true
			namespace = nil
			d.pyTopLevel = true

			continue
		case kwNAMESPACE.any():
			i++

			if i >= len(v.Args) {
				throwFmt("PY_SRCS NAMESPACE expects a value")
			}

			namespace = ptr(v.Args[i])
			topLevel = false
			d.pyNamespace = namespace

			continue
		case kwCYTHONIZE_PY.any():
			cythonizePy = true

			continue
		case kwCYTHON_CPP.any():
			cythonPlainCpp = true
			cythonCMode = false
			cythonHeader = false
			cythonApiHeader = false

			continue
		case kwCYTHON_C.any():
			cythonCMode = true
			cythonPlainCpp = false
			cythonHeader = false
			cythonApiHeader = false

			continue
		case strCythonCppH.any():

			cythonCMode = false
			cythonPlainCpp = false
			cythonHeader = true
			cythonApiHeader = false

			continue
		case strCythonCH.any():

			cythonCMode = true
			cythonPlainCpp = false
			cythonHeader = true
			cythonApiHeader = false

			continue
		case strCythonCApiH.any(), strCythonCppApiH.any():

			cythonCMode = a == strCythonCApiH.any()
			cythonPlainCpp = false
			cythonHeader = true
			cythonApiHeader = true

			continue
		case kwCYTHON_DIRECTIVE.any():
			i++

			if i >= len(v.Args) {
				throwFmt("PY_SRCS CYTHON_DIRECTIVE expects a value")
			}

			cythonDirectives = append(cythonDirectives, "-X", v.Args[i].string())

			continue
		case kwSWIG_C.any():
			swigCMode = true

			continue
		case kwSWIG_CPP.any():
			swigCMode = false

			continue
		case kwMAIN.any():
			mainNext = true

			continue
		}

		literal := a.string()

		if !d.usePython2 && strings.ContainsRune(literal, '$') {
			a = internStr(expandToken(literal, env, tokenExpandArguments)).any()
		}

		src, modNameOverride, explicitModule := strings.Cut(a.string(), "=")

		if extIsGztproto(src) {
			if d.moduleStmt.Name != tokProtoLibrary {
				continue
			}

			if !explicitModule {
				src = strings.TrimSuffix(src, ".gztproto") + ".proto"

				if env.string(envOptimizePyProtosFlag) == "yes" {
					d.peerdirs = append(d.peerdirs, strKernelGazetteerProto.any())
				}
			}
		}

		scope := PyScope{TopLevel: topLevel, Namespace: namespace}

		if explicitModule {
			scope.ModuleName = ptr(internStr(modNameOverride).any())
		}

		if mainNext {
			modName := scope.moduleName(modulePath, src)

			d.pyMain = ptr(internV(modName, ":main").any())
			d.pyMainDeclSeq = d.nextDeclSeq()

			mainNext = false
		} else if !explicitModule && d.pyMain == nil && d.moduleStmt != nil &&
			(d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin || d.pythonSourceEntry && !d.usePython2) &&
			(src == "__main__.py" || strings.HasSuffix(src, "/__main__.py")) {
			d.pyMain = ptr(internStr(pythonModuleName(modulePath, src, topLevel, namespace)).any())
			d.pyMainDeclSeq = d.nextDeclSeq()
		}

		if d.unit.Tag == unitTagPy3Bin {
			continue
		}

		lintPy = lintPy || extIsPy(src)

		if strings.HasSuffix(src, ".fbs") {
			path, modName, _ := strings.Cut(literal, "=")

			if modName == "" {
				modName = pythonModuleName(modulePath, strings.TrimSuffix(path, ".fbs"), topLevel, namespace)
			}

			d.pySrcGroups[group].Fbs = append(d.pySrcGroups[group].Fbs, PyFbsSource{Path: path, Module: modName})
			d.peerdirs = append(d.peerdirs, strContribPythonFlatbuffers.any())

			continue
		}

		if extIsPyx(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, src, topLevel, namespace)
			}

			stmt := &CythonStmt{
				InitialPath: true,
				Src:         src,
				CMode:       cythonCMode,
				Header:      cythonHeader,
				ApiHeader:   cythonApiHeader,
				Options: []string{
					"--module-name", modName,
					"--init-suffix", pythonInitSuffix(modName),
					"--source-root", "$(S)",
				},
			}

			if cythonPlainCpp && !d.usePython2 {
				stmt.Generated = ptr(src + ".cpp")
			}

			pyxs = append(pyxs, pyxDeclaration{stmt: stmt, module: modName})
			mainNext = false

			continue
		}

		if cythonizePy && extIsPy(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, src, topLevel, namespace)
			}

			pyxs = append(pyxs, pyxDeclaration{module: modName, stmt: &CythonStmt{
				InitialPath: true,
				Src:         src,
				CMode:       cythonCMode,
				Header:      cythonHeader,
				ApiHeader:   cythonApiHeader,

				Pxd: strings.ReplaceAll(modName, ".", "/") + ".pxd",
				Options: []string{
					"--module-name", modName,
					"--init-suffix", pythonInitSuffix(modName),
					"--source-root", "$(S)",
				},
			}})

			mainNext = false

			continue
		}

		if extIsSwg(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, strings.TrimSuffix(src, ".swg"), topLevel, namespace)
			}

			swigs = append(swigs, SwigSrc{Src: src, Module: modName, CMode: swigCMode})
			d.peerdirs = append(d.peerdirs, strContribToolsSwigLibPython.any())
			mainNext = false

			continue
		}

		if extIsPyi(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, strings.TrimSuffix(src, ".pyi"), topLevel, namespace)
			}

			dest := "py/" + strings.ReplaceAll(modName, ".", "/") + ".pyi"

			d.pyPyiResources = expandResourceFiles(d, d.pyPyiResources, []string{"DONT_COMPRESS", "DEST", dest, src})
			mainNext = false

			continue
		}

		if !extIsPy(src) && !extIsProto(src) && !extIsEv(src) {
			throwFmt("in PY_SRCS: unrecognized arg %q", src)
		}

		if !d.usePython2 && scope.isMain(src) {
			throwFmt("TOP_LEVEL __main__.py is not allowed in PY3_PROGRAM")
		}

		d.pySrcs = append(d.pySrcs, internStr(src).any())
		d.pyGroupSrcs = append(d.pyGroupSrcs, DeclaredSource{Token: internAny(src)})
		d.pyGroupScopes = append(d.pyGroupScopes, scope)
	}

	slices.SortStableFunc(pyxs, func(a, b pyxDeclaration) int { return cythonVariantBucket(a.stmt) - cythonVariantBucket(b.stmt) })

	for _, pyx := range pyxs {
		pyx.stmt.Seq = d.nextDeclSeq()
		pyx.stmt.Directives = append(pyx.stmt.Directives, cythonDirectives...)

		d.cythonCpp = append(d.cythonCpp, pyx.stmt)
		appendPyRegister(d, pyx.module, false)
	}

	for _, cMode := range [...]bool{true, false} {
		for _, swig := range swigs {
			if swig.CMode == cMode {
				swig.DeclSeq = d.nextDeclSeq()
				d.swigC = append(d.swigC, swig)
				appendPyRegister(d, swig.Module+"_swg", true)
			}
		}
	}

	d.pySrcGroups[group].Srcs = d.pyGroupSrcs[srcStart:len(d.pyGroupSrcs):len(d.pyGroupSrcs)]
	d.pySrcGroups[group].Scopes = d.pyGroupScopes[scopeStart:len(d.pyGroupScopes):len(d.pyGroupScopes)]
	d.pySrcGroups[group].PyiStart = pyiBefore
	d.pySrcGroups[group].PyiEnd = len(d.pyPyiResources)

	if (lintPy || cythonizePy && len(d.cythonCpp) > cythonStmtStart) && d.unit.Tag != unitTagPy3Bin {
		d.collectPythonLintFiles(modulePath, group)

		for _, stmt := range d.cythonCpp[cythonStmtStart:] {
			stmt.Lint = d.pySrcGroups[group].Lint
		}
	}
}

func (s PyScope) isMain(src string) bool {
	if s.ModuleName != nil {
		return s.ModuleName.string() == "__main__"
	}

	return s.TopLevel && strings.TrimSuffix(strings.TrimSuffix(src, ".py"), ".pyx") == "__main__"
}

func pythonModuleName(modulePath, src string, topLevel bool, namespace *ANY) string {
	prefix := modulePath

	if namespace != nil {
		prefix = strings.TrimSuffix(namespace.string(), ".")
	}

	if topLevel {
		prefix = ""
	}

	src = strings.TrimSuffix(strings.TrimSuffix(src, ".py"), ".pyx")

	n := len(prefix) + len(src)

	if !topLevel {
		n++
	}

	block := internTable.bytes.alloc(n)
	k := copy(block, prefix)

	if namespace == nil {
		for i := 0; i < k; i++ {
			if block[i] == '/' {
				block[i] = '.'
			}
		}
	}

	if !topLevel {
		block[k] = '.'
		k++
	}

	copy(block[k:], src)

	for i := k; i < n; i++ {
		if block[i] == '/' {
			block[i] = '.'
		}
	}

	return internBlock(block, n).string()
}

func pythonInitSuffix(name string) string {
	if !strings.Contains(name, ".") {
		return name
	}

	var buf [128]byte

	mangled := buf[:0]

	for rest := name; ; {
		var seg string
		var more bool

		seg, rest, more = strings.Cut(rest, ".")

		mangled = strconv.AppendInt(mangled, int64(len(seg)), 10)
		mangled = append(mangled, seg...)

		if !more {
			break
		}
	}

	return string(mangled)
}

func (d *ModuleData) addProtoInclude(paths []VFS, global bool) {
	d.protoIncludeLocal = append(d.protoIncludeLocal, paths...)

	if global {
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, paths...)
	} else {
		d.protoAddIncl = append(d.protoAddIncl, paths...)
	}
}

func protoIncludeDir(rel string) VFS {
	rel = filepath.ToSlash(filepath.Clean(rel))

	if rel == "." {
		rel = ""
	}

	return source(rel)
}

func protoAddInclPaths(args []ANY) ([]VFS, VFS, bool) {
	global, withGen := false, false

	var rel string

	for _, a := range args {
		switch a.string() {
		case "GLOBAL":
			global = true
		case "WITH_GEN":
			withGen = true
		default:
			rel = filepath.ToSlash(filepath.Clean(a.string()))
		}
	}

	src := protoIncludeDir(rel)
	bld := src.rel().build()

	var includes []VFS

	if withGen {
		includes = append(includes, bld)
	}

	if src.relString() != "" {
		includes = append(includes, src)
	}

	return includes, bld, global
}

func applyArchiveStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		inNameSlot bool
	)

	for _, aTok := range v.Args {
		a := aTok.string()

		switch {
		case inNameSlot:
			entry.Name = a
			inNameSlot = false
		case a == "NAME":
			inNameSlot = true
		case a == "DONTCOMPRESS":
			entry.DontCompress = true
		default:
			entry.Files = append(entry.Files, a)
		}
	}

	if inNameSlot {
		throwFmt("gen: ARCHIVE(NAME ...) missing value after NAME (line %d)", v.Line)
	}

	entry.DeclSeq = d.nextDeclSeq()
	d.archives = append(d.archives, entry)
}

func applyArchiveByKeysStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		inNameSlot bool
		inKeysSlot bool
	)

	entry.Keys = []string{}

	for _, aTok := range v.Args {
		a := aTok.string()

		switch {
		case inNameSlot:
			entry.Name = a
			inNameSlot = false
		case inKeysSlot:
			entry.Keys = []string{a}
			inKeysSlot = false
		case a == "NAME":
			inNameSlot = true
		case a == "KEYS":
			inKeysSlot = true
		case a == "DONTCOMPRESS":
			entry.DontCompress = true
		default:
			entry.Files = append(entry.Files, a)
		}
	}

	if inNameSlot {
		throwFmt("gen: ARCHIVE_BY_KEYS(NAME ...) missing value after NAME (line %d)", v.Line)
	}

	entry.DeclSeq = d.nextDeclSeq()
	d.archives = append(d.archives, entry)
}

func applyArchiveAsmStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry    ArchiveAsmEntry
		seenName bool
	)

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i].string()

		switch a {
		case "NAME":
			if seenName {
				throwFmt("gen: ARCHIVE_ASM at line %d: Keyword 'NAME' appears more than once and is not an array.", v.Line)
			}

			if i+1 >= len(v.Args) || v.Args[i+1].string() == "NAME" || v.Args[i+1].string() == "DONTCOMPRESS" {
				throwFmt("gen: ARCHIVE_ASM(NAME ...) missing value after NAME (line %d)", v.Line)
			}

			i++
			entry.Name = v.Args[i].string()
			seenName = true
		case "DONTCOMPRESS":
			entry.DontCompress = true
		default:
			entry.Files = append(entry.Files, a)
		}
	}

	entry.DeclSeq = d.nextDeclSeq()
	d.archiveAsm = append(d.archiveAsm, entry)
}

func applyLj21ArchiveStmt(v UnknownStmt, d *ModuleData) {
	var luas []string

	for _, aTok := range v.Args {
		a := aTok.string()

		if extIsLua(a) {
			luas = append(luas, a)
		}
	}

	if len(luas) > 0 {
		d.lj21 = &Lj21Archive{Luas: luas}
	}

	raws := make([]string, len(luas))

	for i, lua := range luas {
		raws[i] = strings.TrimSuffix(lua, ".lua") + ".raw"
	}

	d.archives = append(d.archives,
		ArchiveEntry{Name: "LuaScripts.inc", DontCompress: true, Files: raws, Keys: []string{strings.Join(luas, ":")}, DeclSeq: d.nextDeclSeq()},
		ArchiveEntry{DeclSeq: d.nextDeclSeq(), Name: "LuaSources.inc", DontCompress: true, Files: append([]string(nil), luas...), Keys: []string{strings.Join(luas, ":")}},
	)
}

func isProgramModuleType(name TOK) bool {
	switch name {
	case tokProgram, tokYqlPrebuiltProgram, tokPy2Program, tokPy3Program, tokPy3ProgramBin, tokSandboxTask, tokSandboxPy3Task, tokSandboxPy23Task, tokSandboxPy233Task, tokPy3Test, tokPy2Test, tokPytestBin, tokUnittestFor, tokUnittest, tokUnittestWithCustomEntryPoint, tokYtUnittest, tokGtest, tokGBenchmark, tokYBenchmark, tokFuzz, tokBoosttest, tokBoosttestWithMain, tokGoProgram, tokGoTest, tokGoDll:
		return true
	}

	return false
}

func isPyProgramType(name TOK) bool {
	switch name {
	case tokPy2Program, tokPy2Test, tokPytestBin, tokPy3Program, tokPy3ProgramBin, tokPy3Test:
		return true
	}

	return false
}

func isYqlUdfStaticModule(name TOK) bool {
	switch name {
	case tokYqlUdf, tokYqlUdfYdb, tokYqlUdfContrib:
		return true
	}

	return false
}

func yqlUdfImplicitPeers() []string {
	return []string{
		"yql/essentials/public/udf",
		"yql/essentials/public/udf/support",
	}
}

func isPyLibraryType(name TOK) bool {
	switch name {
	case tokPy23NativeLibrary, tokPy3Library, tokPy23Library, tokPy2Library,
		tokPy2Program, tokPy3Program, tokPy2Test, tokPytestBin, tokPy3Test:
		return true
	}

	return false
}

func isPythonModuleType(name TOK) bool {
	return isPyLibraryType(name) || name == tokPy3ProgramBin || isPythonTestModule(name)
}

func isSpecializedLibraryType(name TOK) bool {
	switch name {
	case tokProtoLibrary,
		tokDll, tokSoProgram, tokDynamicLibrary:
		return true
	}

	return false
}

func buildIfEnvInto(store *EnvStore, instance ModuleInstance) Environment {
	env := instance.Platform.ifEnv.cloneInto(store)

	if instance.Path != 0 {
		env.setVFS(envCURDIR, instance.Path)
		env.setVFS(envBINDIR, instance.Path.rel().build())
		env.setVFS(envCmakeCurrentBinaryDir, instance.Path.rel().build())
		env.setStringID(envMODDIR, instance.Path.rel())
	}

	return env
}

func expandConfigVFSPaths(paths []ANY, env Environment) []VFS {
	expanded := expandStmtTokens(paths, env)
	out := make([]VFS, 0, len(expanded))

	for _, path := range expanded {
		if path.string() == "$(B)" {
			out = append(out, bldRootDirVFS)

			continue
		}

		if v := path.vfs(); v != 0 {
			out = append(out, cleanVFS(v))

			continue
		}

		out = append(out, cleanVFS(path.str().source()))
	}

	return out
}

func addInclDirs(paths []ANY, env Environment) []VFS {
	dirs := expandConfigVFSPaths(paths, env)

	if slices.Contains(dirs, srcRootDirVFS) {
		dirs = slices.DeleteFunc(dirs, func(v VFS) bool { return v == srcRootDirVFS })
	}

	return dirs
}

func cleanVFS(v VFS) VFS {
	rel := v.sharedRel()

	if rel == "" || pathIsClean(rel) {
		return v
	}

	if v.isBuild() {
		return build(cleanRel(rel))
	}

	return source(cleanRel(rel))
}

func parseModulePathVFS(path string) VFS {
	if vfsHasPrefix(path) {
		return intern(path)
	}

	return source(path)
}

func peerdirModulePath(path ANY) STR {
	if peer := path.vfs(); peer != 0 && peer.isSource() {
		path = peer.rel().any()
	}

	if st := path.str(); st.cleanPath() {
		return st
	}

	return internStr(cleanRel(strings.Trim(path.string(), "/")))
}

func expandStmtToken(s string, env Environment) string {
	return expandToken(s, env, tokenExpandLazy)
}

type TokenExpansion uint8

func dontExpandVar(name string) bool {
	return slices.Contains(dontExpandVarNames[:], name)
}

func keepDeferredVar(name string) bool {
	switch name {
	case envBINDIR.string(), envCURDIR.string(), "S", "B", "MANAGED_PEERS", "MANAGED_PEERS_CLOSURE", "APPLIED_EXCLUDES", "_FMT_INDUCED_DEPS":
		return true
	}

	return strings.HasSuffix(name, resourceGlobalSuffix) || strings.HasPrefix(name, "_") && strings.HasSuffix(name, "__INCLUDE")
}

func expandToken(s string, env Environment, phase TokenExpansion) string {
	return expandTokenTracked(s, env, phase, nil)
}

func (phase TokenExpansion) policies(track *SourceTextTracker) (deref, render substPolicy) {
	deref = substPolicy{makefile: true, track: track}
	render = substPolicy{track: track}

	switch phase {
	case tokenExpandLazy:
		return deref, render
	case tokenExpandArguments:
		deref.reserved, render.reserved = keepDeferredVar, keepDeferredVar
	case tokenExpandAssignment:
		deref.reserved, render.reserved = dontExpandVar, dontExpandVar
	case tokenExpandPathArguments:
		deref.reserved, render.reserved = pathArgumentsReservedVar, pathArgumentsReservedVar
	case tokenExpandStatement:
		deref.literal = pathArgumentsReservedVar
	case tokenExpandRoots:
		deref.literal = notRootVar

		return deref, render
	}

	deref.emptyUnknown, render.emptyUnknown = true, true

	return deref, render
}

func expandTokenTracked(s string, env Environment, phase TokenExpansion, track *SourceTextTracker) string {
	return expandTokenWith(s, env, phase, track, nil)
}

func expandTokenView(s string, env Environment, phase TokenExpansion, constant *bool) string {
	if s == "$S" {
		return "$(S)"
	}

	if s == "$B" {
		return "$(B)"
	}

	deref, render := phase.policies(nil)
	derefed := substMacroDeeplyInto(s, nil, env, constant, &deref, &tokenSubstBufs[0])

	if phase == tokenExpandStatement || phase == tokenExpandRoots {
		return derefed
	}

	return substMacroDeeplyInto(derefed, nil, env, constant, &render, &tokenSubstBufs[1])
}

func expandTokenWith(s string, env Environment, phase TokenExpansion, track *SourceTextTracker, constant *bool) string {
	if s == "$S" {
		return "$(S)"
	}

	if s == "$B" {
		return "$(B)"
	}

	deref, render := phase.policies(track)
	derefed := substMacroDeeply(s, nil, env, constant, &deref)

	if phase == tokenExpandStatement || phase == tokenExpandRoots {
		return derefed
	}

	return substMacroDeeply(derefed, nil, env, constant, &render)
}

func keepDeferredOrDontExpandVar(name string) bool {
	return keepDeferredVar(name) || dontExpandVar(name)
}

func notRootVar(name string) bool {
	return !rootVar(name)
}

func pathArgumentsReservedVar(name string) bool {
	return dontExpandVar(name) || strings.HasSuffix(name, resourceGlobalSuffix)
}

func expandInFiles(files []ANY, noParse int, env Environment, evaluated bool) ([]ANY, int) {
	split := len(files) - noParse
	phase := tokenExpandLazy

	if evaluated {
		phase = tokenExpandArguments
	}

	parsed, lateText := expandSourceInputTokens(files[:split:split], env, phase)
	text := append(lateText, expandTokens(files[split:], env, false, phase)...)

	return append(parsed[:len(parsed):len(parsed)], text...), len(text)
}

func expandStmtTokens(items []ANY, env Environment) []ANY {
	return expandTokens(items, env, false, tokenExpandLazy)
}

func expandCommandTokens(items []ANY, env Environment) []ANY {
	return expandTokens(items, env, true, tokenExpandLazy)
}

func expandListPreSufArgs(items []ANY, env Environment) []ANY {
	if len(items) < 3 {
		return expandStmtTokens(items, env)
	}

	out := make([]ANY, 3, len(items))

	for i := range out {
		out[i] = internStr(expandToken(items[i].string(), env, tokenExpandArguments)).any()
	}

	return append(out, expandTokens(items[3:], env, false, tokenExpandArguments)...)
}

func expandTokens(items []ANY, env Environment, command bool, phase TokenExpansion) []ANY {
	var out []ANY

	for i, item := range items {
		if !strHasDollar(item.str().any()) || !command && strExpandsToSelf[phase].has(uint32(item.str())) {
			if out != nil {
				out = append(out, item)
			}

			continue
		}

		text := item.string()
		constant := true
		expanded := expandTokenView(text, env, phase, &constant)

		if !command && constant && expanded == text && !vfsHasPrefix(text) && strings.IndexFunc(text, unicode.IsSpace) < 0 {
			strExpandsToSelf[phase].add(uint32(item.str()))

			if out != nil {
				out = append(out, item)
			}

			continue
		}

		if out == nil {
			out = append(make([]ANY, 0, len(items)+4), items[:i]...)
		}

		if !command {
			for f := range strings.FieldsSeq(expanded) {
				out = append(out, internAny(f))
			}

			continue
		}

		for _, f := range splitShellWords(expanded) {
			out = append(out, internAny(f))
		}
	}

	if out == nil {
		return items
	}

	return out
}

func expandStmtTokenAny(item ANY, env Environment) ANY {
	if !strHasDollar(item.str().any()) {
		return item
	}

	return internAny(expandStmtToken(item.string(), env))
}

func applyDeclareInDirs(fs FS, modulePath string, v UnknownStmt, env Environment) {
	if len(v.Args) < 2 {
		throwFmt("gen: %s: DECLARE_IN_DIRS expects a var prefix and a pattern", modulePath)
	}

	prefix := v.Args[0].string()
	pattern := v.Args[1].string()
	matcher := newGlobMatcher("^(?:" + globSegmentRegex(pattern, true) + ")$")

	var dirs, excludes []string

	srcdir := ""
	recursive := false
	section := ""

	for _, a := range v.Args[2:] {
		t := a.string()

		switch t {
		case "DIRS", "EXCLUDES", "SRCDIR":
			section = t
		case "RECURSIVE":
			recursive = true
		default:
			switch section {
			case "DIRS":
				dirs = append(dirs, t)
			case "EXCLUDES":
				excludes = append(excludes, t)
			case "SRCDIR":
				srcdir = t
			default:
				throwFmt("gen: %s: DECLARE_IN_DIRS: unexpected argument %q", modulePath, t)
			}
		}
	}

	sourceRoot := srcdir == srcRootDirVFS.string() || strings.HasPrefix(srcdir, srcRootDirVFS.string()+"/")

	rootRel := func(dir string) string {
		switch {
		case sourceRoot:
			return path.Join(strings.TrimPrefix(strings.TrimPrefix(srcdir, srcRootDirVFS.string()), "/"), dir)
		case srcdir == "":
			return path.Clean(modulePath + "/" + dir)
		default:
			return path.Clean(modulePath + "/" + srcdir + "/" + dir)
		}
	}

	filePrefix := func(dirRel string) string {
		if sourceRoot {
			return srcRootDirVFS.string() + "/" + dirRel + "/"
		}

		return strings.TrimPrefix(dirRel+"/", modulePath+"/")
	}

	excluded := func(dirRel, name string) bool {
		for _, ex := range excludes {
			if ok, _ := path.Match(ex, name); ok {
				return true
			}

			if ok, _ := path.Match(ex, dirRel+"/"+name); ok {
				return true
			}
		}

		return false
	}

	var files []string

	for _, dir := range dirs {
		base := rootRel(dir)
		root := cleanRel(base)

		fs.walk(root, func(rel string, isDir bool) bool {
			if rel == root {
				return isDir
			}

			if isDir {
				return recursive
			}

			if root != base {
				rel = joinRel(base, strings.TrimPrefix(rel, root+"/"))
			}

			dirRel, name := splitDirName(rel)

			if matcher.match(name) && name != "ya.make" && name != "a.yaml" && !excluded(dirRel, name) {
				files = append(files, filePrefix(dirRel)+name)
			}

			return false
		})
	}

	value := strings.Join(files, " ")
	id := internEnv(prefix + "_FILES")

	env.setFromString(id, value)

	if value != "" {
		env.setTextInputs(id, []SourceTextRange{{0, len(value)}})
	}

	env.setFromString(internEnv(prefix+"_SRCDIR"), srcdir)
}

func isExpandVarName(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		b := s[i]

		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-' {
			continue
		}

		return false
	}

	return true
}

func expandScalarVarRef(s string, env Environment) string {
	return expandStmtToken(s, env)
}

func expandAssignedScalarVarRef(s string, env Environment) string {
	return expandToken(s, env, tokenExpandAssignment)
}

func expandSourceScalarVarRefSTR(s string, env Environment) STR {
	return internStr(substMacroDeeplyInto(s, nil, env, nil, scalarVarRefPolicy, &strSubstBufs))
}

func expandSourceScalarVarRef(s string, env Environment) string {
	return expandMakeExpression(s, env, pathArgumentsReservedVar)
}

func allPySrcArgs(fs FS, modulePath string, v UnknownStmt) []ANY {
	dirs := []string{"."}
	noTestFiles := false
	recursive := false
	topLevel := false

	var namespace ANY

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a.string() {
		case "TOP_LEVEL":
			topLevel = true
		case "NAMESPACE":
			i++

			if i >= len(v.Args) {
				throwFmt("ALL_PY_SRCS NAMESPACE expects a value")
			}

			namespace = v.Args[i]
		case "RECURSIVE":
			recursive = true
		case "NO_TEST_FILES":
			noTestFiles = true
		default:
			dirs = append(dirs, a.string())
		}
	}

	if len(dirs) > 1 {
		dirs = dirs[1:]
	}

	var files []string

	seen := make(map[string]bool)
	moduleRootRel := modulePath

	appendPyFile := func(rel string) {
		name := baseName(rel)

		if filepath.Ext(name) != ".py" {
			return
		}

		if noTestFiles && (strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")) {
			return
		}

		file := strings.TrimPrefix(rel, moduleRootRel+"/")

		if !seen[file] {
			seen[file] = true
			files = append(files, file)
		}
	}

	for _, dir := range dirs {
		walkRoot := cleanRel(filepath.ToSlash(filepath.Join(moduleRootRel, dir)))

		if recursive {
			fs.walk(walkRoot, func(rel string, isDir bool) bool {
				if rel == walkRoot {
					return true
				}

				if isDir {
					return !fs.isFileID(dirKey(rel), strYaMake)
				}

				if pathDir(rel) != walkRoot {
					appendPyFile(rel)
				}

				return false
			})
		}

		for _, packed := range fs.listdir(dirKey(walkRoot)).names {
			if packed&1 == 0 {
				appendPyFile(joinRel(walkRoot, STR(packed>>1).string()))
			}
		}
	}

	var args []ANY

	if namespace != 0 && namespace.string() != "" {
		args = append(args, kwNAMESPACE.any(), namespace)
	}

	if topLevel {
		args = append(args, kwTOP_LEVEL.any())
	}

	return append(args, internAnys(files)...)
}

func peerEntryLanguage(parent ModuleInstance, d *ModuleData) Language {
	parentModuleName := d.moduleStmt.Name

	if d.usePython2 {
		return LangPy2
	}

	if isPythonModuleType(parentModuleName) && parentModuleName != tokPy23NativeLibrary || d.yqlPythonUdf {
		return LangPy
	}

	if parentModuleName == tokProtoLibrary && parent.Language == LangPy {
		return LangPy
	}

	return LangCPP
}

func (e *EmitContext) derivePeerInstance(peerPath string) ModuleInstance {
	return e.derivePeerInstanceVFS(source(peerPath))
}

func (e *EmitContext) derivePeerInstanceVFS(peerVFS VFS) ModuleInstance {
	if e.d.peerRequest == 0 {
		e.d.peerRequest = makePeerRequest(e.d)
		e.d.consumerTags = consumerPeerTags(e.d, Environment{s: &e.frame.ifEnv})
	}

	return ModuleInstance{
		Path:     peerVFS,
		Kind:     KindLib,
		Language: peerEntryLanguage(e.instance, e.d),
		Platform: e.instance.Platform,
		Demand:   e.peerDemand(),
		Peer:     e.d.peerRequest,
		PeerTags: e.d.consumerTags,
		PeerFrom: e.instance.Path,
	}
}

func hasPy2Variant(name TOK) bool {
	switch name {
	case tokPy23Library, tokPy23NativeLibrary, tokSandboxTask, tokSandboxPy23Task, tokSandboxPy233Task, tokProtoLibrary:
		return true
	}

	return false
}

func moduleExcludesTag(d *ModuleData, tag string) bool {
	excluded, specified := d.excludeTags[internStr(tag)]

	return excluded || d.onlyTags && !specified
}

func applyCompileNlg(modulePath string, v UnknownStmt, d *ModuleData) {
	opts := map[string]string{}

	var files []string

	for i := 0; i < len(v.Args); i++ {
		switch a := v.Args[i].string(); a {
		case "NLG_CONFIG", "TRANSLATIONS_JSON", "PROTO_REGISTRY", "JAVA_PACKAGE_NAME":
			i++
			opts[a] = v.Args[i].string()
		default:
			files = append(files, a)
		}
	}

	str := func(s string) ANY { return internStr(s).any() }
	args := []ANY{str("compile-cpp")}

	if d.setVars.get(strCOMPILE_NLG_EMPTY_CPP) == strYes {
		args = append(args, str("--empty-cpp"))
	}

	args = append(args, str("--import-dir"), strS.any(), str("--out-dir"), strB.any(), str("--include-prefix"), str(modulePath))

	var ins []ANY

	for _, o := range [...][2]string{{"NLG_CONFIG", "--nlg-config-json"}, {"TRANSLATIONS_JSON", "--translations-json"}, {"PROTO_REGISTRY", "--proto-registry"}} {
		if f := opts[o[0]]; f != "" {
			args = append(args, str(o[1]), str(f))
			ins = append(ins, str(f))
		}
	}

	noAuto := []ANY{str("localizable_phrases.pb.txt"), str("tmacros.pb.txt"), str("nlg_renderer_factory.cpp"), str("nlg_renderer_factory.h"),
		str("register.cpp"), str("translations.cpp"), str("translations.h")}

	for _, suf := range []string{".cpp", ".h", ".pb.txt"} {
		for _, f := range files {
			noAuto = append(noAuto, str(f+suf))
		}
	}

	for _, f := range files {
		args = append(args, str(f))
		ins = append(ins, str(f))
	}

	d.runPrograms = append(d.runPrograms, &RunProgramStmt{
		ToolPath:       str("alice/nlg/tools/codegen"),
		Args:           args,
		INFiles:        ins,
		OUTFiles:       []ANY{str("dummy.h"), str("dummy.cpp")},
		OUTNoAutoFiles: noAuto,
		Line:           v.Line,
		DeclSeq:        d.nextDeclSeq(),
	})

	d.peerdirs = append(d.peerdirs, str("alice/nlg/library/nlg_project_runtime"))

	for _, f := range append([]string{"nlg_renderer_factory.cpp", "translations.cpp"}, files...) {
		if strings.HasSuffix(f, ".nlg") {
			f += ".cpp"
		}

		d.srcs = append(d.srcs, SrcMeta{Source: str(f), Prio: stmtPrioSrcs, Seq: d.nextDeclSeq()})
	}

	d.srcs = append(d.srcs, SrcMeta{Source: str("register.cpp"), Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Global: true})
}

type ConfPluginKey struct {
	fs    FS
	macro string
}

func confPluginDeps(fs FS, confRel, macro string) []string {
	return confProtoPlugin(fs, confRel, macro).deps
}

type ConfProtoPlugin struct {
	deps      []string
	namespace string
}

func confProtoPlugin(fs FS, confRel, macro string) ConfProtoPlugin {
	key := ConfPluginKey{fs: fs, macro: macro}

	if v, ok := confPluginDepsCache.Load(key); ok {
		return v.(ConfProtoPlugin)
	}

	var conf ConfProtoPlugin

	if fs.isFile(srcRootRel, confRel) {
		inMacro := false

		for _, line := range strings.Split(string(concatChunks(fs.read(confRel))), "\n") {
			line = strings.TrimSpace(line)

			switch {
			case strings.HasPrefix(line, "macro "+macro+"("):
				inMacro = true

				if _, value, ok := strings.Cut(line, `NAMESPACE="`); ok {
					conf.namespace, _, _ = strings.Cut(value, `"`)
				}
			case inMacro && line == "}":
				inMacro = false
			case inMacro && strings.HasPrefix(line, "CPP_PROTO_PLUGIN"):
				fields := strings.Fields(line[strings.IndexByte(line, '(')+1 : strings.LastIndexByte(line, ')')])

				if i := slices.Index(fields, "DEPS"); i >= 0 {
					for _, f := range fields[i+1:] {
						if f == "EXTRA_OUT_FLAG" {
							break
						}

						conf.deps = append(conf.deps, f)
					}
				}
			}
		}
	}

	confPluginDepsCache.Store(key, conf)

	return conf
}

func appendCompilerFlags(args []ANY, env Environment, name string, allGlobal bool, global, own []ANY, uses *CompilerFlagResourceUses) ([]ANY, []ANY) {
	if slices.IndexFunc(args, func(arg ANY) bool { return !literalCompilerFlag(arg) }) < 0 {
		return appendLiteralCompilerFlags(args, env, name, allGlobal, global, own)
	}

	var values []string

	for _, arg := range args {
		value := arg.string()

		if strings.ContainsRune(value, '$') {
			words := strings.Split(expandMakeExpression(value, env, func(key string) bool {
				return keepDeferredVar(key) || key == "ARCADIA_ROOT" || key == "ARCADIA_BUILD_ROOT"
			}), " ")

			values = append(values, slices.DeleteFunc(words, func(word string) bool { return word == "" })...)
		} else {
			values = append(values, value)
		}
	}

	for i, value := range values {
		if value == "&&" || value == ";" {
			values[i] = `${quo:"` + value + `"}`
		}
	}

	bound := make([]string, len(values))

	for i, value := range values {
		bound[i] = value

		if allGlobal {
			bound[i] = "GLOBAL " + bound[i]
		}
	}

	body, origins := compilerFlagForwardingWithoutRoots(name+" "+strings.Join(bound, " "), env, map[string][]string{"Flags": values})
	words := sourceWordSpans(body, func(r rune) bool { return r == ' ' })

	var globalValues, ownValues []string

	nextGlobal := false

	for _, span := range words[1:] {
		word := body[span.Start:span.End]

		if word == "GLOBAL" {
			nextGlobal = true

			continue
		}

		arg := internStr(compilerFlagRootValue(word, env)).any()

		if nextGlobal {
			global = append(global, arg)
			globalValues = append(globalValues, word)
			uses.Global = resourceKeyUnion(uses.Global, compilerFlagWordResources(origins, span))
		} else {
			own = append(own, arg)
			ownValues = append(ownValues, word)
			uses.Private = resourceKeyUnion(uses.Private, compilerFlagWordResources(origins, span))
		}

		nextGlobal = false
	}

	if len(ownValues) > 0 {
		appendMacroEnv(env, name, strings.Join(ownValues, " "))
	}

	if len(globalValues) > 0 {
		appendMacroEnv(env, name+"_GLOBAL", strings.Join(globalValues, " "))
	}

	return global, own
}

func compilerFlagRootValue(value string, env Environment) string {
	var roots [4]confMacroBinding

	for i, root := range [...]ENV{envARCADIA_ROOT, envARCADIA_BUILD_ROOT, envCURDIR, envBINDIR} {
		bound, _ := env.lookup(root.string())

		roots[i] = confMacroBinding{name: root.str(), values: []ANY{internAny(bound)}, bound: true}
	}

	return substMacroDeeply(value, roots[:], Environment{}, nil, &substPolicy{})
}

type SetVars struct {
	vals    map[STR]STR
	appends map[STR][]ANY
}

func (v *SetVars) len() int {
	return len(v.vals)
}

func (v *SetVars) has(k STR) bool {
	_, ok := v.vals[k]

	return ok
}

func (v *SetVars) get(k STR) STR {
	if v == nil {
		return 0
	}

	if chunks := v.appends[k]; chunks != nil {
		b := []byte(v.vals[k].string())

		for _, c := range chunks {
			b = appendANYText(append(b, ' '), c)
		}

		v.vals[k] = internBytes(b)

		delete(v.appends, k)
	}

	return v.vals[k]
}

func (v *SetVars) set(k, val STR) {
	if v.vals == nil {
		v.vals = map[STR]STR{}
	}

	if v.appends != nil {
		delete(v.appends, k)
	}

	v.vals[k] = val
}

func (v *SetVars) append(k STR, args []ANY) {
	if len(args) == 0 {
		args = emptyAppendArgs
	}

	if v.vals[k] == 0 {
		v.set(k, joinAnysSTR(args, " "))

		return
	}

	if v.appends == nil {
		v.appends = map[STR][]ANY{}
	}

	v.appends[k] = append(v.appends[k], args...)
}
