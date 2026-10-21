package main

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	muslCFlagsChunk     = []ANY{argDMusl.any()}
	confPluginDepsCache sync.Map
)

var allocatorPeers = map[string][]string{
	"MIM":                       {"library/cpp/malloc/mimalloc"},
	"MIM_SDC":                   {"library/cpp/malloc/mimalloc_sdc"},
	"HU":                        {"library/cpp/malloc/hu"},
	"PROFILED_HU":               {"library/cpp/malloc/profiled_hu"},
	"THREAD_PROFILED_HU":        {"library/cpp/malloc/thread_profiled_hu"},
	"TCMALLOC_256K":             {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc"},
	"TCMALLOC_SMALL_BUT_SLOW":   {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/small_but_slow"},
	"TCMALLOC_NUMA_256K":        {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/numa_256k"},
	"TCMALLOC_NUMA_LARGE_PAGES": {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/numa_large_pages"},
	"TCMALLOC":                  {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/default"},
	"TCMALLOC_TC":               {"library/cpp/malloc/tcmalloc", "contrib/libs/tcmalloc/no_percpu_cache"},
	"GOOGLE":                    {"library/cpp/malloc/galloc"},
	"J":                         {"library/cpp/malloc/jemalloc"},
	"LF":                        {"library/cpp/lfalloc"},
	"LF_YT":                     {"library/cpp/lfalloc/yt"},
	"LF_DBG":                    {"library/cpp/lfalloc/dbg"},
	"B":                         {"library/cpp/balloc"},
	"BM":                        {"library/cpp/balloc_market"},
	"C":                         {"library/cpp/malloc/calloc"},
	"LOCKLESS":                  {"library/cpp/malloc/lockless"},
	"YT":                        {"library/cpp/ytalloc/impl"},
	"FAKE":                      nil,
	"SYSTEM":                    {"library/cpp/malloc/system"},
	"DEFAULT":                   nil,
}

const (
	event2cppPluginName = "event2cpp"
	event2cppToolPath   = "tools/event2cpp"
	stmtPrioDefault     = 2
	stmtPrioSrcs        = 4
	prioAddIncl         = 0
	prioAddInclSelf     = 1
	yaffPluginPath      = "library/cpp/yaff/tools/protoc_plugin"
	yabsConfRel         = "build/internal/conf/project_specific/yabs.conf"
)

const (
	bkDefault = iota
	bkCheckConfig
	bkCython
	bkSwig
	bkAntlr
	bkJV
	bkSplitCodegen
	bkRunPython
	bkArchiveAsm
	bkDecimalMD5
)

const (
	tokenExpandLazy TokenExpansion = iota
	tokenExpandArguments
	tokenExpandFinal
	tokenExpandAssignment
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

	if !isGoModuleType(d.unit.Type) {
		d.peerdirs = append(d.peerdirs, internAnys(plugin.Deps)...)
	}
}

func recalcProtoOutputs(d *ModuleData) {
	d.grpcOutputsBeforeBase = d.grpc

	for i := range d.cppProtoPlugins {
		d.cppProtoPlugins[i].OutputsBeforeBase = true
	}
}

func protoCmdPeers(d *ModuleData) []ANY {
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
	mlaunch     *MlaunchModule
	ros         *RosModule
	multimodule TOK
	selfPeers   []Language
	rosInclude  []VFS

	discarded bool

	styleChecks []StyleCheck

	usrvFiles []UsrvFile

	yqlPrebuiltTool          bool
	yqlUdfProbe              bool
	yqlUdfShared             bool
	yqlPythonUdf             bool
	yqlCurrent               bool
	yqlStableABI             bool
	yqlPrebuiltResource      string
	moduleVariants           uint16
	listProtos               []ListProtoStmt
	nots                     *NotsModule
	docs                     *DocsData
	moduleStmt               *ModuleStmt
	modver                   string
	hasLicense               bool
	goTestSrcs               []ANY
	goXTestSrcs              []ANY
	goSkipTests              []ANY
	gettextKeepPath          bool
	noLint                   string
	test                     TestMeta
	mockgen                  GoMockgenVars
	goCodegens               []GoCodegenStmt
	userverProtos            []UserverProtoBatch
	goEmbeds                 []GoEmbed
	goCommonGoogleApis       []ANY
	goPluginPeers            []ANY
	goProtoOpts              []GoProtoOpt
	cgoSrcs                  []ANY
	cgoLdflags               []ANY
	cgoCflags                []ANY
	hasBisonY                bool
	toolchainName            string
	srcs                     []SrcMeta
	pySrcs                   []ANY
	pySrcGroups              []PySrcGroup
	pyPyiResources           []ResourceEntry
	resKeyBuf                []byte
	pyBuildNoPYC             bool
	pyBuildNoPY              bool
	pyTopLevel               bool
	pyYapycSuffix            string
	noExtendedPySearch       bool
	enumSrcs                 []*GenerateEnumSerializationStmt
	peerdirs                 []ANY
	resourcePeerDeferred     bool
	protoCmdPeers            []ANY
	joinSrcs                 []*JoinSrcsStmt
	addIncl                  []VFS
	addInclP                 []PrioVFS
	addInclGlobal            []VFS
	addInclOneLevel          []VFS
	addInclUserGlobal        []VFS
	cfAddIncl                []VFS
	cfAddInclGlobal          []VFS
	cythonAddIncl            []VFS
	cythonAddInclGlobal      []VFS
	useNasm                  bool
	clangSAEnable            bool
	clangSAConfig            string
	clangSAPlugins           []ANY
	nasmFlags                []ANY
	asmAddIncl               []VFS
	protoAddInclGlobal       []VFS
	protoAddIncl             []VFS
	swigAddIncl              []VFS
	swigAddInclGlobal        []VFS
	llvmBc                   []*LlvmBcStmt
	llvmCompile              []LlvmCompileStmt
	luaCompile               []LuaCompileStmt
	cFlags                   []ANY
	cFlagsGlobal             []ANY
	cxxFlags                 []ANY
	cxxFlagsVar              []ANY
	cxxFlagsGlobal           []ANY
	cOnlyFlags               []ANY
	cOnlyFlagsGlobal         []ANY
	sFlags                   []ANY
	protocFlags              []ANY
	fbsNamespaces            []ANY
	flatcFlags               []ANY
	flatcAddIncl             []VFS
	flatcAddInclGlobal       []VFS
	clangWarnings            []ANY
	ldFlags                  []ANY
	rpathFlagsGlobal         []ANY
	objAddLibsGlobal         []ANY
	srcDirs                  []VFS
	flags                    FlagSet
	hadAllocator             bool
	allocatorName            ANY
	muslLite                 bool
	cudaSrcs                 []CudaSrcsStmt
	nvccDeviceSrcs           []ANY
	cuda                     CudaSettings
	implibs                  []ImplibStmt
	linkExcludeLibraries     []ANY
	muslEnabled              bool
	useAsmlib                bool
	useArcadiaLibm           bool
	useDynamicCuda           bool
	splitDwarf               bool
	noPythonIncl             bool
	noImportTracing          bool
	usePython2               bool
	usePython3               bool
	useLightPy2cc            bool
	useCommonGoogleAPIs      bool
	moduleScopeCFlags        []ANY
	pythonSQLite3            bool
	pyNamespace              *ANY
	pyModuleNamespace        *ANY
	protoNamespace           *ANY
	ymapsSprotoSrcs          []ANY
	noMypy                   bool
	noOptimize               bool
	optimizePyProtos         bool
	optimizePyProtosSet      bool
	needGoogleProtoPeerdirs  bool
	cppProtoPlugins          []CppProtoPlugin
	grpcOutputIndex          int
	grpcOutputsBeforeBase    bool
	excludeTags              map[STR]bool
	onlyTags                 bool
	dynamicLibraryFrom       []ANY
	exportsScript            *ANY
	linkerScripts            []ANY
	ldPlugins                []ANY
	arPlugin                 *ANY
	hasFbs                   bool
	hasFbs64                 bool
	defaultVars              map[STR]STR
	defaultVarOrder          []STR
	configureFiles           []*ConfigureFileStmt
	createBuildInfoFor       *ANY
	antlr4Grammars           []Antlr4GrammarInfo
	antlrRuns                []AntlrRunInfo
	runPrograms              []*RunProgramStmt
	decimalMD5               []*DecimalMD5Lower32BitsStmt
	buildMns                 []*BuildMnStmt
	splitCodegens            []*SplitCodegenStmt
	baseCodegens             []*BaseCodegenStmt
	proto2Fbs                []Proto2FbsStmt
	runPython                []*RunPythonStmt
	fromSandboxes            []*FromSandboxStmt
	fromArchives             []FromArchiveEntry
	sdcMetas                 []SdcMetaEntry
	sdcInstallOutputs        []VFS
	pireInline               []ANY
	pchFiles                 []PchFileStmt
	checkConfigHeaders       []ANY
	cythonOptions            []ANY
	cythonCpp                []*CythonStmt
	cythonNumpyBeforeInclude bool
	swigC                    []SwigSrc
	bisonGenExt              STR
	grpc                     bool
	yaConfJSON               []ANY
	archives                 []ArchiveEntry
	archiveAsm               []ArchiveAsmEntry
	lj21                     *Lj21Archive
	copyFiles                []CopyFileEntry
	copyFileAutoOutputs      map[STR]CopyFileEntry
	declSeq                  int
	resources                []ResourceEntry
	bundles                  []BundleEntry
	pyMain                   *ANY
	noStrip                  bool
	strip                    bool
	unit                     ModuleUnit
	noCheckImports           []ANY
	noCheckImportsDisabled   bool
	pyRegister               []PyRegistration
	ragel6Flags              []ANY
	bisonFlags               []ANY
	conflictMod              *ModuleStmt
	resourceDeclStmts        []*DeclareResourceStmt
	primaryOutput            string
	inducedDeps              ParsedIncludeSet
	setVars                  map[STR]STR
	tc                       ModuleToolchain
	scanCtx                  *ScanContext
	cc                       ModuleCompileEnv
}

type PyRegistration struct {
	Name        STR
	RenameInit  bool
	CFlagsStart int
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
	Source      ANY
	Prio        int
	Seq         int
	Stmt        int
	Global      bool
	RosZeroCopy bool
	Bucket      int
	Compile     CompileSpec
	CompileRef  NodeRef
	PyMeta      uint32
}

func (m SrcMeta) sortKey(round uint64) uint64 {
	return round<<48 | uint64(m.Prio)<<40 | uint64(uint32(m.Seq))<<8 | uint64(uint8(m.Bucket))
}

func (d *ModuleData) nextDeclSeq() int {
	d.declSeq++

	return d.declSeq
}

func (d *ModuleData) hasSrc(src string) bool {
	for _, meta := range d.srcs {
		if meta.Source.string() == src {
			return true
		}
	}

	return false
}

func muslCFlags(on bool) []ANY {
	if on {
		return muslCFlagsChunk
	}

	return nil
}

type BundleEntry struct {
	Target string
	Name   string
	Suffix string
	Output string
}

type ResourceEntry struct {
	DeclSeq      int
	Path         string
	Key          string
	SrcPath      string
	EndsBatch    bool
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

type PySrcGroup struct {
	Srcs           []ANY
	Scopes         []PyScope
	ResourceOffset int
	Prio           int
	PyiStart       int
	PyiEnd         int
	Statement      *UnknownStmt
}

type PyScope struct {
	TopLevel  bool
	Namespace *ANY
}

type ArchiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
	Keys         []string
}

type ArchiveAsmEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

type Lj21Archive struct {
	Luas []string
}

type CopyFileEntry struct {
	Src            string
	Dst            string
	Auto           bool
	WithContext    bool
	OutputIncludes []string
}

type Antlr4GrammarInfo struct {
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
	Macro          string
	Args           []ANY
	INFiles        []ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	CWD            *ANY
	OutputIncludes []ANY
}

func parseCopyFileEntry(args []string, withContext bool, line int) CopyFileEntry {
	i := 0
	auto := false

	for i < len(args) {
		switch args[i] {
		case "AUTO":
			auto = true
			i++
		case "TEXT":
			i++
		default:
			goto parsedFlags
		}
	}

parsedFlags:
	if len(args)-i < 2 {
		throwFmt("gen: COPY_FILE at line %d expects at least source and destination, got %d args", line, len(args))
	}

	entry := CopyFileEntry{
		Src:         args[i],
		Dst:         args[i+1],
		Auto:        auto,
		WithContext: withContext,
	}

	i += 2

	section := ""

	for i < len(args) {
		switch args[i] {
		case "OUTPUT_INCLUDES", "INDUCED_DEPS":
			section = args[i]
			i++

			continue
		}

		if section == "OUTPUT_INCLUDES" || section == "INDUCED_DEPS" {
			entry.OutputIncludes = append(entry.OutputIncludes, args[i])
		}

		i++
	}

	return entry
}

func parseCopyEntries(args []string, line int) []CopyFileEntry {
	i := 0
	auto := false
	withContext := false

	for i < len(args) {
		switch args[i] {
		case "AUTO":
			auto = true
			i++
		case "WITH_CONTEXT":
			withContext = true
			i++
		default:
			goto parsedFlags
		}
	}

parsedFlags:
	if i >= len(args) || args[i] != "FROM" {
		throwFmt("gen: COPY at line %d expects FROM <dir>", line)
	}

	i++

	if i >= len(args) {
		throwFmt("gen: COPY at line %d expects source directory after FROM", line)
	}

	fromDir := args[i]

	i++

	files := make([]string, 0, 8)
	outputIncludes := make([]string, 0, 8)
	section := "FILES"

	for i < len(args) {
		switch args[i] {
		case "OUTPUT_INCLUDES", "INDUCED_DEPS":
			section = args[i]
			i++

			continue
		}

		if section == "FILES" {
			files = append(files, args[i])
		} else {
			outputIncludes = append(outputIncludes, args[i])
		}

		i++
	}

	out := make([]CopyFileEntry, 0, len(files))

	for _, file := range files {
		src := filepath.ToSlash(filepath.Clean(fromDir + "/" + file))
		includes := append([]string(nil), outputIncludes...)

		if withContext {
			includes = append(includes, src)
		}

		out = append(out, CopyFileEntry{
			Src:            src,
			Dst:            file,
			Auto:           auto,
			OutputIncludes: includes,
		})
	}

	return out
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

func moduleRootedVFS(modulePath string, path string) (VFS, bool) {
	if vfsHasPrefix(path) {
		return intern(path), true
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}/"):
		return sourceClean(strings.TrimPrefix(path, "${ARCADIA_ROOT}/")), true
	case strings.HasPrefix(path, "${CURDIR}/"):
		return sourceJoinClean(modulePath, strings.TrimPrefix(path, "${CURDIR}/")), true
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}/"):
		return buildClean(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}/")), true
	case strings.HasPrefix(path, "${BINDIR}/"):
		return buildJoinClean(modulePath, strings.TrimPrefix(path, "${BINDIR}/")), true
	default:
		return 0, false
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, dst); ok {
		return vfs
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
	if vfsHasPrefix(target) {
		return intern(target).relString()
	}

	switch {
	case strings.HasPrefix(target, "${ARCADIA_ROOT}/"):
		return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(target, "${ARCADIA_ROOT}/")))
	case strings.HasPrefix(target, "${ARCADIA_BUILD_ROOT}/"):
		return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(target, "${ARCADIA_BUILD_ROOT}/")))
	case strings.HasPrefix(target, "${BINDIR}/"):
		return filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(target, "${BINDIR}/")))
	default:
		return target
	}
}

type PrioVFS struct {
	prio int
	vfs  VFS
}

func (d *ModuleData) addLocalIncl(prio int, v VFS) {
	d.addInclP = append(d.addInclP, PrioVFS{prio: prio, vfs: v})
}

func (d *ModuleData) materializeAddIncl() {
	sort.SliceStable(d.addInclP, func(i, j int) bool {
		return d.addInclP[i].prio < d.addInclP[j].prio
	})

	for _, p := range d.addInclP {
		d.addIncl = append(d.addIncl, p.vfs)
	}

	d.addInclP = nil
}

func collectModuleInto(ctx *GenCtx, instance ModuleInstance, stmts []Stmt, env Environment, onWarn func(Warn), d *ModuleData) *ModuleData {
	pm := ctx.parsers
	fs := ctx.fs
	modulePath := instance.Path.relString()
	kind := instance.Kind

	env.setString(envMODDIR, modulePath)
	env.setVFS(envCURDIR, source(modulePath))
	env.setVFS(envBINDIR, build(modulePath))
	env.setVFS(envCmakeCurrentBinaryDir, build(modulePath))
	env.setDefaultString(envCythonOptions, cythonDefaultOptions(instance.Platform.OS))

	d.reset()

	d.pythonSQLite3 = true
	d.useAsmlib = true
	d.bisonGenExt = strCpp
	d.needGoogleProtoPeerdirs = true
	d.yqlCurrent = instance.YqlCurrent
	d.yqlStableABI = instance.Platform.YqlStableABI

	collectStmts(ctx, modulePath, kind, instance.Language, stmts, env, d, onWarn, instance.Platform)
	finishYqlModule(modulePath, env, d)

	for _, priority := range []int{stmtPrioDefault, stmtPrioSrcs} {
		for i, group := range d.pySrcGroups {
			if group.Prio == priority && group.Statement != nil && group.Statement.Name == tokPySrcs {
				collectPySrcGroup(fs, modulePath, *group.Statement, d, env, onWarn, i)
				d.pySrcGroups[i].Statement = nil
			}
		}
	}

	d.cFlags = compilerCommandFlags(d.cFlags)
	d.cxxFlags = compilerCommandFlags(d.cxxFlags)
	d.cOnlyFlags = compilerCommandFlags(d.cOnlyFlags)

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

	if len(d.pySrcs) > 0 {
		d.pyYapycSuffix = pySrcYapycSuffix(modulePath)
	}

	if d.docs != nil {
		d.docs.finish(fs, modulePath, env)
	}

	d.materializeAddIncl()

	srcDirs := append(d.srcDirs, 0)

	copy(srcDirs[1:], srcDirs[:len(srcDirs)-1])

	srcDirs[0] = instance.Path

	d.srcDirs = srcDirs
	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = nil
	d.cfAddInclGlobal = nil

	dedupers.with(func(deduper *DeDuper) {
		filterInvalidAddIncl(fs, deduper, d, modulePath, onWarn)
	})

	if d.unit.Tag == unitTagPy3BinLib {
		d.pyMain = nil
	}

	d.useDynamicCuda = env.bool(envUseDynamicCuda)
	d.muslEnabled = env.bool(envMUSL)
	d.useArcadiaLibm = env.bool(envUSE_ARCADIA_LIBM) && !env.bool(envOS_EMSCRIPTEN)
	d.clangSAEnable = env.bool(envClangSAEnable)
	d.clangSAConfig = env.string(envClangSAConfig)
	d.clangSAPlugins = internAnys(strings.Fields(env.string(envClangSAPlugins)))

	if d.clangSAEnable && d.clangSAConfig != "" {
		d.peerdirs = append(d.peerdirs, clangSAPeer.any())
	}

	d.noStrip = env.bool(envNO_STRIP)
	d.gettextKeepPath = env.string(envGettextKeepPath) == "yes"
	d.useNasm = env.bool(envUseNasmAssembler)
	d.nasmFlags = internAnys(strings.Fields(env.string(envNasmFlags)))

	if d.muslLite {
		d.flags.NoUtil = true
	}

	if env.bool(envPY3_PROTO) && instance.Language == LangPy {
		d.usePython3 = true
	}

	applyPythonAddIncl(modulePath, d, env)
	applyBuildInfoAddIncl(modulePath, d)
	applyArchiveAddIncl(modulePath, d)
	applyCythonHeaderAddIncl(modulePath, d)

	musl := muslCFlags(d.muslEnabled && !effectiveNoPlatform(d.flags))
	sse := sseBaseCFlags(env.bool(envARCH_X86_64))

	if n := len(musl) + len(sse); n > 0 {
		scope := d.moduleScopeCFlags
		grown := append(scope, make([]ANY, n)...)

		copy(grown[n:], grown[:len(scope)])
		copy(grown, musl)
		copy(grown[len(musl):], sse)

		d.moduleScopeCFlags = grown
	}

	dedupers.with(func(deduper *DeDuper) {
		d.addIncl = dedupInPlaceWith(deduper, d.addIncl)
		d.addInclGlobal = dedupInPlaceWith(deduper, d.addInclGlobal)
	})

	for _, a := range d.addIncl {
		pm.indexAddincl(a)
	}

	for _, a := range d.addInclGlobal {
		pm.indexAddincl(a)
	}

	hasProto := false
	hasSc := false
	hasCfgProto := false
	hasPyGztProto := false
	hasFortran := false

	for i, meta := range d.srcs {
		src := meta.Source

		if d.unit.Tag == unitTagPy3Proto && extIsGztproto(src.string()) {
			src = internV(strings.TrimSuffix(src.string(), ".gztproto"), ".proto").any()
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
		case srcExtY:
			d.hasBisonY = true
		case srcExtSc:
			hasSc = true
		case srcExtCfgProto:
			hasCfgProto = true
		case srcExtFortran:
			hasFortran = true
		}
	}

	evInduced, protoInduced := false, false

	for _, meta := range d.srcs {
		src := meta.Source

		switch srcExtClassOf(src) {
		case srcExtEv:
			if !evInduced && !isGoModuleType(d.unit.Type) {
				evInduced = true
				d.peerdirs = append(d.peerdirs, strLibraryCppEventlog.any(), strContribLibsProtobuf.any())
			}
		case srcExtProto:
			if !protoInduced {
				protoInduced = true

				isProtoLibrary := d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary

				if !isGoModuleType(d.unit.Type) && (!isProtoLibrary || !env.bool(envPY3_PROTO)) {
					d.peerdirs = append(d.peerdirs, strContribLibsProtobuf.any())
				}
			}
		}
	}

	if hasFortran || hasFortranGeneratedSource(d) {
		d.peerdirs = append(d.peerdirs, strBuildPlatformFlang.any(), strContribLibsLlvmFlangRt.any())
	}

	if hasSc {
		d.peerdirs = append(d.peerdirs, strLibraryCppDomscheme.any())
	}

	if hasCfgProto {
		d.peerdirs = append(d.peerdirs, strLibraryCppProtoConfigCodegen.any(), strLibraryCppProtoConfigProtos.any(), strContribLibsProtobuf.any())
	}

	if hasProto {
		isProtoLibrary := d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary

		if isProtoLibrary && !d.optimizePyProtosSet {
			d.optimizePyProtos = true
		}
	}

	if hasPyGztProto && d.optimizePyProtos {
		d.peerdirs = append(d.peerdirs, strKernelGazetteerProto.any())
	}

	if d.moduleStmt != nil && (d.moduleStmt.Name == tokProtoLibrary || hasProto) {
		d.protoCmdPeers = protoCmdPeers(d)
	}

	if d.resourcePeerDeferred || len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	if len(d.antlr4Grammars) > 0 || len(d.antlrRuns) > 0 {
		d.peerdirs = append(d.peerdirs, strBuildPlatformJavaJdk.any(), strBuildPlatformJavaJdkJdk17.any())
	}

	return d
}

func applyResourceFiles(fs FS, modulePath string, args []string, line int, d *ModuleData) {
	if isGoModuleType(d.unit.Type) {
		applyGoResource(fs, modulePath, expandResourceFiles(d, nil, args), line, d)

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

func filterInvalidAddIncl(fs FS, dd *DeDuper, d *ModuleData, modulePath string, onWarn func(Warn)) {
	d.addIncl = filterOwnAddIncl(fs, d.addIncl, modulePath, onWarn)
	d.addInclGlobal = filterOwnAddIncl(fs, d.addInclGlobal, modulePath, onWarn)
	d.cythonAddIncl = filterOwnAddIncl(fs, d.cythonAddIncl, modulePath, onWarn)

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

	d.asmAddIncl = filterOwnAddIncl(fs, d.asmAddIncl, modulePath, onWarn)

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

func usePython2(env Environment, d *ModuleData, moduleTag STR) {
	d.usePython2 = true

	env.setBool(envPYTHON2, true)
	env.setBool(envPYTHON3, false)
	env.setStringID(envMODULE_TAG, moduleTag)
}

func applyPythonAddIncl(modulePath string, d *ModuleData, env Environment) {
	if d.moduleStmt == nil {
		return
	}

	if !d.usePython2 && !d.usePython3 && !pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		return
	}

	if modulePath == "contrib/libs/python" {
		return
	}

	if d.usePython2 {
		d.moduleScopeCFlags = append(d.moduleScopeCFlags, internArg("-DARCADIA_PYTHON_UNICODE_SIZE="+env.string(envArcadiaPythonUnicodeSize)).any())
	} else {
		d.usePython3 = true
		d.moduleScopeCFlags = append(d.moduleScopeCFlags, argDusePython3.any())
	}

	d.addInclGlobal = append(d.addInclGlobal, pythonIncludeDir)
	d.addInclUserGlobal = append(d.addInclUserGlobal, pythonIncludeDir)
	d.addIncl = append(d.addIncl, pythonIncludeDir)
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

func applyBuildInfoAddIncl(modulePath string, d *ModuleData) {
	if d.createBuildInfoFor == nil {
		return
	}

	biDir := build(modulePath)

	d.addIncl = append(d.addIncl, biDir)
	d.addInclGlobal = append(d.addInclGlobal, biDir)
	d.addInclUserGlobal = append(d.addInclUserGlobal, biDir)
}

func pyModuleTypeUsesPython3(name TOK) bool {
	switch name {
	case tokPy3Library, tokPy3Program, tokPy3ProgramBin, tokPy3Test,
		tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}

func collectStmts(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, stmts []Stmt, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	fs := ctx.fs

	for _, s := range stmts {
		if d.discarded {
			return
		}

		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			if v.Name == tokPy3Program && kind == KindBin {
				d.peerdirs = append([]ANY{internStr(modulePath).any()}, d.peerdirs...)
			}

			applyCppTestModule(v, d, env)

			if isYqlUdfStaticModule(v.Name) {
				d.peerdirs = append(d.peerdirs, internAnys(yqlUdfImplicitPeers())...)
			}

			if isNotsModule(v.Name) || v.Name == tokProtoLibrary && (language == LangTS || language == LangTSPrepare) {
				d.nots = newNotsModule(v.Name, env)
				d.nots.AutoProto = v.Name == tokProtoLibrary && !fs.isFilePathClean(modulePath+"/package.json")
			}

			d.unit = resolveModuleUnit(v.Name, kind, language)
			setupYqlModule(d, v.Name, env)

			if v.Schema && d.unit.Tag == unitTagPy3Proto {
				d.unit.HashTag = strPy3ProtoFromSchema
			}

			d.moduleStmt = moduleStmtForKind(v, d.unit)

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
				d.flags.NoRuntime = true
				d.flags.NoUtil = true
			}

			if d.moduleStmt.Name == tokProtoDescriptions || v.Name == tokProtoRegistry {
				env.setStringID(envMODULE_TAG, d.moduleStmt.Name.str())
				env.setStringID(envMODULE_TYPE, d.moduleStmt.Name.str())
			}

			setupRosModule(v, modulePath, language, env, d)
			setupMlaunchModule(v, modulePath, language, env, d)
			setupPackageModule(v, language, d)
			setupSdcDiagsModule(v, language, env, d)

			if v.Name == tokFbsLibrary {
				d.multimodule = v.Name
				d.moduleVariants = 1<<LangCPP | 1<<LangGo
				env.setStringID(envMODULE_TAG, d.unit.Tag)
			}

			if v.Name == tokProtoLibrary && !v.Schema {
				d.moduleVariants = defaultProtoVariants
			}

			if d.unit.Tag == unitTagPy2 || d.unit.Tag == unitTagPy2Native {
				usePython2(env, d, d.unit.Tag)
			} else if d.unit.Tag != 0 {
				env.setStringID(envMODULE_TAG, d.unit.Tag)
			}

			if isDllModuleType(v.Name) || d.yqlUdfShared {
				d.exportsScript = dllExportsArg(v)

				if d.exportsScript == nil {
					if exports, ok := env.lookup("EXPORTS_FILE"); ok && exports != "" {
						d.exportsScript = ptr(internStr(exports).any())
					}
				}
			}

			if v.Name == tokDocs || v.Name == tokDocsLibrary {
				d.docs = &DocsData{config: modulePath + "/.yfm"}
			}

			if v.Name == tokProtoLibrary && language == LangGo {
				env.setString(envMODULE_TAG, "GO_PROTO")
				env.setBool(envGO_PROTO, true)
			}

			if d.moduleStmt.Name == tokProtoLibrary {
				if language == LangPy {
					env.setBool(envPY3_PROTO, true)
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

			env.setString(envMODULE_LANG, moduleLang)

		case *SrcsStmt:
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
			srcToks := expandStmtTokens(v.Sources, env)
			stmt := d.nextDeclSeq()

			if d.unit.SwigLang == strSwigLangJniCpp {
				srcToks = jniCppSwigSrcs(srcToks)

				d.peerdirs = append(d.peerdirs, swigJNIPeerdirs(srcToks, env)...)
				d.peerdirs = append(d.peerdirs, strContribToolsSwigLibJava.any())
			}

			if d.unit.SwigLang.string() == "perl" {
				for _, src := range srcToks {
					if srcExtClassOf(src) == srcExtSwg {
						d.peerdirs = append(d.peerdirs, strContribToolsSwigLibPerl5.any(), strBuildPlatformPerl.any())

						break
					}
				}
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
				prio, seq := stmtPrioDefault, 0

				if !global {
					prio, seq = stmtPrioSrcs, d.nextDeclSeq()
				}

				d.srcs = append(d.srcs, SrcMeta{Source: srcTok, Prio: prio, Seq: seq, Stmt: stmt, Global: global, Compile: compile})
				globalNext = false

				switch srcExtClassOf(srcTok) {
				case srcExtHIn:
					addGeneratedHeaderInclude(modulePath, strings.TrimSuffix(srcTok.string(), ".in"), d)
				case srcExtY:
					src := srcTok.string()

					addGeneratedOwnHeaderInclude(modulePath, strings.TrimSuffix(src, filepath.Ext(src))+".h", d)
				case srcExtFlex:

					d.addLocalIncl(prioAddIncl, argContribToolsFlexOld.str().source())
				}
			}

		case *PeerdirStmt:

			addInclNext := false

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

				if addInclNext {
					d.addLocalIncl(prioAddIncl, parseModulePathVFS(pTok.string()))
					addInclNext = false
				}

				d.peerdirs = append(d.peerdirs, pTok)

				if d.nots != nil && d.nots.Kind == tokProtoLibrary {
					d.nots.ProtoPeers = append(d.nots.ProtoPeers, pTok)
				}
			}
		case *SetStmt:

			value := expandAssignedScalarVarRef(v.Value, env)
			perlMask := perlInputMask(v.NameEnv)
			perlChanged := perlMask != 0 && env.string(v.NameEnv) != value

			env.setFromString(v.NameEnv, value)

			if d.setVars == nil {
				d.setVars = map[STR]STR{}
			}

			d.setVars[internStr(v.Name)] = internStr(value)

			if perlChanged {
				recalcPerlEnv(env, platform, d.setVars, perlMask)
			}

			if v.Name == "SWIG_LANG" {
				d.unit.SwigLang = internStr(value)
			}

			if v.Name == "PROTOC_TRANSITIVE_HEADERS" {
				recalcProtoOutputs(d)
			}

			if v.Name == "PROTO_NAMESPACE" {
				d.protoNamespace = nil

				if value != "" {
					d.protoNamespace = ptr(internStr(value).any())
				}
			}

			if v.Name == "RAGEL6_FLAGS" {
				d.ragel6Flags = internAnys(strings.Fields(value))
			}
		case *EndStmt:

		case *JoinSrcsStmt:
			expanded := *v

			expanded.Sources = expandStmtTokens(v.Sources, env)
			expanded.Seq = d.nextDeclSeq()

			d.joinSrcs = append(d.joinSrcs, &expanded)
		case *AddInclStmt:

			d.addInclGlobal = append(d.addInclGlobal, expandConfigVFSPaths(v.GlobalPaths, env)...)
			d.addInclOneLevel = append(d.addInclOneLevel, expandConfigVFSPaths(v.OneLevelPaths, env)...)
			d.addInclUserGlobal = append(d.addInclUserGlobal, expandConfigVFSPaths(v.UserGlobalPaths, env)...)

			for _, p := range expandConfigVFSPaths(v.AllPaths, env) {
				d.addLocalIncl(prioAddIncl, p)
			}

			d.cythonAddIncl = append(d.cythonAddIncl, expandConfigVFSPaths(v.CythonPaths, env)...)
			d.cythonAddInclGlobal = append(d.cythonAddInclGlobal, expandConfigVFSPaths(v.CythonGlobalPaths, env)...)
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
			d.protoAddInclGlobal = append(d.protoAddInclGlobal, expandConfigVFSPaths(v.ProtoGlobalPaths, env)...)
			d.flatcAddIncl = append(d.flatcAddIncl, expandConfigVFSPaths(v.FlatcPaths, env)...)
			d.flatcAddInclGlobal = append(d.flatcAddInclGlobal, expandConfigVFSPaths(v.FlatcGlobalPaths, env)...)
			d.swigAddIncl = append(d.swigAddIncl, expandConfigVFSPaths(v.SwigPaths, env)...)
			d.swigAddInclGlobal = append(d.swigAddInclGlobal, expandConfigVFSPaths(v.SwigGlobalPaths, env)...)
		case *CFlagsStmt:

			d.cFlagsGlobal, d.cFlags = appendCompilerFlags(v.Flags, env, d.cFlagsGlobal, d.cFlags)
		case *CXXFlagsStmt:

			d.cxxFlagsGlobal, d.cxxFlags = appendCompilerFlags(v.Flags, env, d.cxxFlagsGlobal, d.cxxFlags)
		case *CONLYFlagsStmt:

			d.cOnlyFlagsGlobal, d.cOnlyFlags = appendCompilerFlags(v.Flags, env, d.cOnlyFlagsGlobal, d.cOnlyFlags)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, expandStmtTokens(v.Flags, env)...)
		case *SrcDirStmt:

			for _, dirTok := range expandStmtTokens(v.Dirs, env) {
				dir := dirTok.string()

				if rooted, ok := moduleRootedVFS(modulePath, dir); ok {
					d.srcDirs = append(d.srcDirs, rooted)
				} else {
					d.srcDirs = append(d.srcDirs, dirKey(dir).source())
				}
			}
		case *GlobalSrcsStmt:
			for _, src := range expandStmtTokens(v.Sources, env) {
				d.srcs = append(d.srcs, SrcMeta{Source: src, Prio: stmtPrioDefault, Global: true})
			}
		case *GenerateEnumSerializationStmt:
			if env.bool(envGO_PROTO) {
				continue
			}

			expandedEN := astOne(astEnumSers, *v)

			expandedEN.DeclSeq = d.nextDeclSeq()
			d.enumSrcs = append(d.enumSrcs, expandedEN)

			const enumSerPeer = "tools/enum_parser/enum_serialization_runtime"

			if v.Variant != "noexcept_header" && modulePath != enumSerPeer {
				d.peerdirs = append(d.peerdirs, internStr(enumSerPeer).any())
			}
		case *DefaultVarStmt:
			value := expandAssignedScalarVarRef(v.Value, env)
			bound := env.hasBindingID(v.NameEnv)

			if d.defaultVars == nil {
				d.defaultVars = map[STR]STR{}
			}

			if _, exists := d.defaultVars[internStr(v.VarName)]; !exists {
				d.defaultVars[internStr(v.VarName)] = internStr(value)
				d.defaultVarOrder = append(d.defaultVarOrder, internStr(v.VarName))
			}

			perlMask := perlInputMask(v.NameEnv)
			perlChanged := perlMask != 0 && !bound

			env.setDefaultString(v.NameEnv, value)

			if !bound && slices.Contains(perlConfigOutputs, v.NameEnv) {
				if d.setVars == nil {
					d.setVars = map[STR]STR{}
				}

				d.setVars[internStr(v.VarName)] = internStr(value)
			}

			if perlChanged {
				recalcPerlEnv(env, platform, d.setVars, perlMask)
			}
		case *ConfigureFileStmt:
			expanded := *v

			expanded.Src = expandStmtToken(v.Src, env)
			expanded.Dst = expandStmtToken(v.Dst, env)

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
				IsSplit:        false,
				Grammar:        expandStmtToken(v.Grammar.string(), env),
				Options:        anyStrs(expandStmtTokens(v.Options, env)),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: anyStrs(expandStmtTokens(v.OutputIncludes, env)),
			})
		case *RunAntlr4CppSplitStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, Antlr4GrammarInfo{
				IsSplit:        true,
				Lexer:          expandStmtToken(v.Lexer.string(), env),
				Parser:         expandStmtToken(v.Parser.string(), env),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: anyStrs(expandStmtTokens(v.OutputIncludes, env)),
			})
		case *RunAntlrStmt:
			expanded := AntlrRunInfo{
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

			d.baseCodegens = append(d.baseCodegens, &expanded)

			for _, p := range v.Peerdirs {
				d.peerdirs = append(d.peerdirs, p)
			}
		case *RunPythonStmt:
			expanded := *v

			expanded.ScriptPath = expandStmtTokenAny(v.ScriptPath, env)
			expanded.Args = expandCommandTokens(v.Args, env)
			expanded.INFiles, expanded.INNoParse = expandInFiles(v.INFiles, v.INNoParse, env, false)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokens(v.EnvPairs, env)
			expanded.ToolPaths = expandStmtTokens(v.ToolPaths, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)

			if v.StdoutFile != nil {
				stdout := expandStmtTokenAny((*v.StdoutFile), env)

				expanded.StdoutFile = &stdout
			}

			if v.CWD != nil {
				cwd := expandStmtTokenAny(*v.CWD, env)

				expanded.CWD = &cwd
			}

			expanded.DeclSeq = d.nextDeclSeq()
			d.runPython = append(d.runPython, &expanded)
		case *FromSandboxStmt:
			expanded := *v

			expanded.ResourceId = expandStmtTokenAny(v.ResourceId, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.Renames = expandStmtTokens(v.Renames, env)
			expanded.DeclSeq = d.nextDeclSeq()

			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *ResourceStmt:
			pairs := expandResourcePairs(v.Pairs, env)

			if isGoModuleType(d.unit.Type) {
				entries := make([]ResourceEntry, len(pairs))

				for i, pair := range pairs {
					entries[i] = ResourceEntry{Path: pair.Path, Key: pair.Key}
				}

				applyGoResource(fs, modulePath, entries, v.Line, d)

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
					DontCompress: v.DontCompress,
				})
			}
		case *ResourceFilesStmt:
			applyResourceFiles(fs, modulePath, expandResourceFileArgs(v.Args, env), v.Line, d)
		case *AllResourceFilesStmt:
			if isGoModuleType(d.unit.Type) {
				entries := expandAllResourceFiles(d, nil, fs, modulePath, env, v)

				applyGoResource(fs, modulePath, entries, v.Line, d)

				break
			}

			d.resourcePeerDeferred = true

			before := len(d.resources)

			d.resources = expandAllResourceFiles(d, d.resources, fs, modulePath, env, v)

			if len(d.resources) > before {
				d.resources[len(d.resources)-1].EndsBatch = true
			}

		case *DeclareResourceStmt:

			expanded := *v

			expanded.Args = expandStmtTokens(v.Args, env)
			d.resourceDeclStmts = append(d.resourceDeclStmts, &expanded)
		case *IfStmt:
			taken := v.Then

			if !evalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(ctx, modulePath, kind, language, taken, env, d, onWarn, platform)
		case *UnknownStmt:

			expanded := *v

			if v.Name == tokDllFor {
				withDllForStmts(ctx, modulePath, v, func(stmts []Stmt) {
					collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
				})
			} else {
				if v.Name == tokRun {
					expanded.Args = expandTestRunArgs(v.Args, env)
				} else if v.Name == tokSetListPreSuf {
					expanded.Args = expandListPreSufArgs(v.Args, env)
				} else {
					expanded.Args = expandStmtTokens(v.Args, env)
				}

				var perlMask uint16

				if v.Name == tokEnable || v.Name == tokDisable || v.Name == tokSetAppend {
					for i, arg := range expanded.Args {
						if v.Name == tokSetAppend && i > 0 {
							break
						}

						key := internEnv(arg.string())
						mask := perlInputMask(key)

						if mask == 0 {
							continue
						}

						if v.Name == tokEnable && env.string(key) == "yes" || v.Name == tokDisable && env.string(key) == "no" {
							continue
						}

						if v.Name == tokSetAppend && len(expanded.Args) < 2 {
							continue
						}

						perlMask |= mask
					}
				}

				applyUnknownStmt(fs, modulePath, expanded, d, env, onWarn)

				if perlMask != 0 {
					recalcPerlEnv(env, platform, d.setVars, perlMask)
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

func swigJNIPeerdirs(srcs []ANY, env Environment) []ANY {
	for _, src := range srcs {
		if !extIsSwg(src.string()) {
			continue
		}

		if env.bool(envUSE_SYSTEM_JDK) || env.bool(envOS_ANDROID) {
			return []ANY{strContribToolsSwigLibJava.any()}
		}

		return []ANY{strContribToolsSwigLibJava.any(), strContribLibsJdk.any()}
	}

	return nil
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
	if language == LangPy2 {
		switch name {
		case tokPy23Library:
			return ModuleUnit{Type: tokPy2Library, Tag: unitTagPy2, CCTag: tagPy2, ObjSuf: tagPy2, ARPrefix: "libpy", GlobalARTag: tagPy2Global, SbomLang: unitSbomCpp}
		case tokPy23NativeLibrary:
			return ModuleUnit{Type: tokLibrary, Tag: unitTagPy2Native, CCTag: tagPy2Native, ObjSuf: tagPy2, ARPrefix: "lib", GlobalARTag: tagPy2NativeGlobal, SbomLang: unitSbomCpp}
		}
	}

	if name == tokYqlUdfTest {
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
	case tokPackage:
		return packageModuleUnit(language)
	case tokRosLibrary:
		return rosModuleUnit(language)
	case tokMlaunchLibrary, tokMlaunchComponent, tokMlaunchPipeline, tokGolaunchProgram, tokGolaunchComposite:
		return mlaunchModuleUnit(name, language)
	case tokSdcDiagsLibrary:
		return sdcDiagsModuleUnit(language)
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
		return ModuleUnit{Type: name, Tag: unitTagPy3, CCTag: tagPy3Native, ObjSuf: tagPy3, ARPrefix: "libpy3c", GlobalARTag: tagPy3NativeGlobal, SbomLang: unitSbomCpp}
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
		if language == LangGo {
			return ModuleUnit{Type: tokGoLibrary, Tag: strGoFbs, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		return ModuleUnit{Type: name, CCTag: tagCppFbs, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokProtoLibrary:
		if language == LangProtoDescriptions {
			return resolveModuleUnitBare(tokProtoDescriptions, kind, language)
		}

		if language == LangGo {
			return ModuleUnit{Type: tokGoLibrary, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
		}

		if language == LangPy {
			return ModuleUnit{Type: name, Tag: unitTagPy3Proto, ARPrefix: "libpy3", GlobalARTag: tagPy3ProtoGlobal, SbomLang: unitSbomCpp}
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
	addGeneratedHeaderInclude(modulePath, dst, d)
}

func appendRunProgram(v *RunProgramStmt, env Environment, d *ModuleData) {
	expanded := *v

	expanded.ToolPath = expandStmtTokenAny(v.ToolPath, env)
	expanded.INFiles, expanded.INNoParse = expandInFiles(v.INFiles, v.INNoParse, env, true)
	expanded.OUTFiles = expandTokens(v.OUTFiles, env, false, tokenExpandArguments)
	expanded.OUTNoAutoFiles = expandTokens(v.OUTNoAutoFiles, env, false, tokenExpandArguments)
	expanded.OutputIncludes = expandTokens(v.OutputIncludes, env, false, tokenExpandArguments)
	expanded.ToolPaths = expandTokens(v.ToolPaths, env, false, tokenExpandArguments)

	if v.StdoutFile != nil {
		stdout := expandStmtTokenAny((*v.StdoutFile), env)

		expanded.StdoutFile = &stdout
	}

	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)

	expanded.Args = expandTokens(v.Args, env, true, tokenExpandArguments)
	expanded.EnvPairs = expandTokens(v.EnvPairs, env, false, tokenExpandArguments)

	if v.CWD != nil {
		cwd := expandStmtTokenAny(*v.CWD, env)

		expanded.CWD = &cwd
	}

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)

	expanded.DeclSeq = d.nextDeclSeq()
	d.runPrograms = append(d.runPrograms, &expanded)
}

func warnUnmodelledMacro(onWarn func(Warn), modulePath string, name TOK) {
	onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q not modelled — implement its upstream semantics (see yatool/build/conf, yatool/build/ymake.core.conf); skipped", modulePath, name.string())})
}

func applyUnknownStmt(fs FS, modulePath string, v UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn)) {
	if applyTestMacro(v, d) {
		return
	}

	if d.nots != nil && applyNotsStmt(v, d.nots) {
		return
	}

	if d.docs != nil && d.docs.apply(v, modulePath) {
		recordHandledMacro(v.Name, v.Args)

		return
	}

	if v.Name == tokInvalid {
		onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q is not in the modelled set (line %d); skipped", modulePath, v.Raw.string(), v.Line)})

		return
	}

	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name, v.Args)
		}
	}()

	switch v.Name {
	case tokPireInline, tokPireInlineCmd:
		if v.Name == tokPireInlineCmd && len(v.Args) != 1 {
			throwFmt("PIRE_INLINE_CMD expects one source file")
		}

		d.pireInline = append(d.pireInline, v.Args...)
	case tokGenerateYtRecord:
		applyYtRecord(modulePath, v, d, env)
	case tokYtRecordDisablePeerdir:
		if len(v.Args) != 0 {
			throwFmt("YT_RECORD_DISABLE_PEERDIR expects no arguments")
		}

		env.setString(envYtRecordPeerdir, "")
	case tokGolaunchSrcs, tokGolaunchSrcdir, tokRuntimePeerdir, tokResourcePeerdir, tokGolaunchFunction, tokGolaunchOverride, tokNoGolaunchSrcs, tokGenerateGolaunchCallbacks, tokGolaunchCompositeComps:
		applyMlaunchStmt(modulePath, v, env, d)
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

		d.pchFiles = append(d.pchFiles, PchFileStmt{Header: v.Args[0], Output: v.Args[1]})

	case tokSdcResourceInfo, tokSdcSbResourceInfo, tokSdcRosnodeInfo, tokSdcSolibInfo:
		d.sdcMetas = append(d.sdcMetas, parseSdcMeta(v.Name, anyStrs(v.Args)))
	case tokFromArchive:
		options := parseFromSandbox(v.Args, v.Line)

		d.fromArchives = append(d.fromArchives, FromArchiveEntry{Source: options.ResourceId, ArchiveExtraction: options.ArchiveExtraction})
	case tokDebVersion:
		if len(v.Args) != 1 {
			throwFmt("DEB_VERSION expects one changelog file")
		}

		d.runPython = append(d.runPython, &RunPythonStmt{
			ScriptPath: buildScriptsMkverPy.any(), Args: v.Args, INFiles: v.Args,
			StdoutFile: ptr(strDebianversionH.any()), KV: &debVersionKV,
		})
	case tokFiles:
		if d.docs != nil || d.moduleStmt != nil && (isUnionModule(d.moduleStmt.Name) || d.moduleStmt.Name == tokCiGroup || d.moduleStmt.Name == tokPackageFinal) {
			d.addFiles(v.Args)
		} else {
			handled = false
		}
	case tokDeclareInDirs:
		applyDeclareInDirs(fs, modulePath, v, env)
	case tokYaToolsConf:
		if len(v.Args) != 1 {
			throwFmt("%s: YA_TOOLS_CONF expects one directory, got %d arguments", modulePath, len(v.Args))
		}

		for _, args := range yaToolsConfResourceFiles(fs, expandStmtTokenAny(v.Args[0], env).string()) {
			applyResourceFiles(fs, modulePath, args, v.Line, d)
		}
	case tokAddInclSelf:

		self := source(modulePath)

		switch {
		case len(v.Args) >= 2 && v.Args[0].string() == "FOR" && v.Args[1].string() == "cython":
			d.cythonAddIncl = append(d.cythonAddIncl, self)
		case len(v.Args) >= 2 && v.Args[0].string() == "FOR" && v.Args[1].string() == "asm":
			d.asmAddIncl = append(d.asmAddIncl, self)
		default:
			d.addLocalIncl(prioAddInclSelf, self)
		}
	case tokSetResourceUriFromJson:

		if len(v.Args) != 2 {
			throwFmt("gen: %s: SET_RESOURCE_URI_FROM_JSON expects 2 args (var json), got %d", modulePath, len(v.Args))
		}

		bundle := readResourceBundleJSON(fs, moduleSourceRel(modulePath, v.Args[1].string()))

		if uri, ok := resolveResourceURIFromBundle(bundle, env); ok {
			env.setString(internEnv(v.Args[0].string()), uri)
		}
	case tokNoPlatformResources:

		env.setBool(envNoplatformResources, true)
	case tokPrimaryOutput:

		if len(v.Args) >= 1 {
			d.primaryOutput = v.Args[0].string()
		}
	case tokNoLibc:

		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case tokNoUtil:
		d.flags.NoUtil = true
	case tokUseCxx:
		d.flags.NoRuntime = false
	case tokUseUtil:
		d.flags.NoRuntime = false
		d.flags.NoUtil = false
	case tokNoRuntime:

		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case tokNoPlatform:

		d.flags.NoPlatform = true
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case tokNoCompilerWarnings:
		if !d.flags.NoCompilerWarnings {
			id := envCudaNvccFlags

			env.setString(id, env.string(id)+" -w")
		}

		d.flags.NoCompilerWarnings = true
	case tokNoWshadow:
		d.flags.NoWShadow = true
	case tokNoExportDynamicSymbols:
		d.flags.NoExportDynSymbols = true
	case tokUseLlvmBc16:
		env.setString(envCLANG_BC_ROOT, "$"+envCLANG16_RESOURCE_GLOBAL.string())
		env.setString(envLLVM_LLC_TOOL, "contrib/libs/llvm16/tools/llc")
	case tokUseLlvmBc18:
		env.setString(envCLANG_BC_ROOT, "$"+envCLANG18_RESOURCE_GLOBAL.string())
		env.setString(envLLVM_LLC_TOOL, "contrib/libs/llvm18/tools/llc")
	case tokUseLlvmBc20:
		env.setString(envCLANG_BC_ROOT, "$"+envCLANG20_RESOURCE_GLOBAL.string())
		env.setString(envLLVM_LLC_TOOL, "contrib/libs/llvm20/tools/llc")
	case tokBpf, tokBpfStatic:
		args := expandStmtTokens(v.Args, env)

		if len(args) < 2 {
			throwFmt("%s requires input and output", v.Name.string())
		}

		d.llvmCompile = append(d.llvmCompile, LlvmCompileStmt{Kind: v.Name, Source: args[0].string(), Output: args[1].string(), Options: args[2:]})
		d.peerdirs = append(d.peerdirs, strBuildPlatformClang.any())

		if v.Name == tokBpfStatic {
			d.peerdirs = append(d.peerdirs, argBuildPlatformLld.any())
		}
	case tokLlvmCompileCxx:
		if len(v.Args) < 2 || env.string(envCLANG_BC_ROOT) == "" {
			throwFmt("LLVM_COMPILE_CXX requires input, output and USE_LLVM_BC16/18/20")
		}

		d.llvmCompile = append(d.llvmCompile, LlvmCompileStmt{
			Source: v.Args[0].string(), Output: v.Args[1].string(),
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

		d.luaCompile = append(d.luaCompile, stmt)
		d.peerdirs = append(d.peerdirs, internStr(runtime).any())
	case tokSplitDwarf:
		d.splitDwarf = true
	case tokNoSplitDwarf:
		d.splitDwarf = false
	case tokNoPythonIncludes:

		d.noPythonIncl = true
	case tokNoImportTracing:
		d.noImportTracing = true
	case tokNoExtendedSourceSearch:
		d.noExtendedPySearch = true
	case tokStylePython, tokStyleRuff:
		d.collectExplicitPythonStyle(v, env)
	case tokLlvmBc:

		if env.string(envCLANG_BC_ROOT) == "" || env.string(envLLVM_LLC_TOOL) == "" {
			throwFmt("LLVM_BC requires USE_LLVM_BC16/18/20 before invocation")
		}

		stmt := &LlvmBcStmt{ClangBCRoot: env.string(envCLANG_BC_ROOT)}
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
				i++
			default:
				stmt.Sources = append(stmt.Sources, v.Args[i].string())
				i++
			}
		}

		if stmt.Name == "" {
			throwFmt("LLVM_BC: NAME keyword is required (got args %v)", anyStrs(v.Args))
		}

		d.llvmBc = append(d.llvmBc, stmt)

	case tokBundleOutput:
		if len(v.Args) < 2 {
			throwFmt("BUNDLE_OUTPUT expects a target and an output name")
		}

		name := v.Args[1].string()

		if len(v.Args) >= 4 && v.Args[2].string() == "RENAME" {
			name = v.Args[3].string()
		}

		d.bundles = append(d.bundles, BundleEntry{Target: v.Args[0].string(), Name: name, Output: v.Args[1].string()})
	case tokBundle:

		i := 0

		for i < len(v.Args) {
			target := v.Args[i].string()

			i++

			suffix := ""

			if i+1 < len(v.Args) && v.Args[i].string() == "SUFFIX" {
				suffix = v.Args[i+1].string()
				i += 2
			}

			name := path.Base(target) + suffix

			if i+1 < len(v.Args) && v.Args[i].string() == "NAME" {
				name = v.Args[i+1].string()
				i += 2
			}

			d.bundles = append(d.bundles, BundleEntry{Target: target, Name: name, Suffix: suffix})
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

			if imp := env.string(env_GO_IMPORT_PATH); imp != "" {
				importPath = imp
			}

			d.goEmbeds = append(d.goEmbeds, newGoEmbed(fs, v.Name == tokGoXTestEmbedPattern, importPath, v.Args[0].string()))
		}
	case tokGoEmbedBindir, tokGoTestEmbedBindir, tokGoXtestEmbedBindir:
		if v.Name == tokGoEmbedBindir || d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			importPath := env.string(env_GO_IMPORT_PATH)

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

			if imp := env.string(env_GO_IMPORT_PATH); imp != "" {
				importPath = imp
			}

			d.goEmbeds = append(d.goEmbeds, newGoEmbedDir(fs, v.Name == tokGoEmbedXtestDir, importPath, args[0], all))
		}
	case tokAllGoTestSrcs:
		if d.moduleStmt != nil && d.moduleStmt.Name == tokGoTest {
			dir := modulePath

			if imp := env.string(env_GO_IMPORT_PATH); imp != "" {
				dir = imp
			}

			for _, name := range goDirFiles(fs, dir, true) {
				d.goTestSrcs = append(d.goTestSrcs, source(dir+"/"+name).any())
			}
		}
	case tokAllGoSrcs:
		dir := modulePath

		if imp := env.string(env_GO_IMPORT_PATH); imp != "" {
			dir = imp
		}

		for _, name := range goDirSources(fs, dir) {
			d.srcs = append(d.srcs, SrcMeta{Source: source(dir + "/" + name).any(), Prio: stmtPrioDefault})
		}
	case tokNoLint:
		d.noLint = "none"

		if len(v.Args) > 0 && v.Args[0].string() == "ktlint" {
			d.noLint = "ktlint"
		}
	case tokStyleCpp:
		d.collectCPPStyle(v, env)
		env.setBool(envUseAllSrcs, true)
		applyHeaders(fs, modulePath, []ANY{str4.any()}, d)

		if d.noLint != "none" && d.noLint != "none_internal" {
			d.peerdirs = append(d.peerdirs, strBuildPlatformClangFormat.any())
		}
	case tokHeaders:
		applyHeaders(fs, modulePath, v.Args, d)
	case tokStyleCppUserSessions, tokStyleCppYt:
		d.collectCPPStyle(v, env)
		env.setBool(envUseAllSrcs, true)
		applyHeaders(fs, modulePath, []ANY{str4.any()}, d)
	case tokTsProtoPackageName:
		if len(v.Args) != 1 {
			throwFmt("TS_PROTO_PACKAGE_NAME expects one package name")
		}

		env.setString(envTsProtoAutoPackageName, v.Args[0].string())
	case tokSetCustomClangTidy:
		if len(v.Args) != 2 {
			throwFmt("SET_CUSTOM_CLANG_TIDY expects a resource module and variable name")
		}

		env.setBool(envCustomClangTidy, true)
		d.peerdirs = append(d.peerdirs, v.Args[0])

		root := "${" + v.Args[1].string() + "_RESOURCE_GLOBAL}/bin/"

		env.setString(envClangTidyBin, root+"clang-tidy")
		env.setString(envYaml2jsonBin, root+"yaml2json")
	case tokGoProtoPlugin:
		positional, lists := goMacroLists(v.Args, "DEPS")

		d.goProtoOpts = append(d.goProtoOpts, GoProtoOpt{plugin: positional[0].string(), ext: positional[1].string(), tool: positional[2].string()})
		d.peerdirs = append(d.peerdirs, lists["DEPS"]...)
	case tokYtOrmProtoYson:
		applyYtOrmProtoYson(modulePath, v.Args, d, env)
	case tokUsrvGenProtoStructs, tokUsrvGenGrpcServiceV2, tokUsrvGenGrpcClientV2,
		tokUsrvGenGrpcServiceV2Structs, tokUsrvGenGrpcClientV2Structs:
		applyUserverProto(v, d)
	case tokGoGrpcGatewayV2OpenapiSrcs, tokGoGrpcGatewayV2Srcs, tokGoGrpcGatewaySrcs, tokGoGrpcGatewaySwagger:
		applyGoGrpcGatewayStmt(v, d, env)
	case tokGoMockgenFrom, tokGoMockgenContribFrom, tokGoMockgenTypes, tokGoMockgenExcludeIfaces, tokGoMockgenPackage,
		tokGoMockgenReflect, tokGoMockgenMocks, tokGoMockgenSource, tokGoOapiCodegen, tokGoOapiCodegenV2, tokGoOapiCodegenTaxi:
		applyGoCodegenStmt(modulePath, v, d, env)
	case tokCgoSrcs:

		d.cgoSrcs = append(d.cgoSrcs, expandStmtTokens(v.Args, env)...)
		d.peerdirs = append(d.peerdirs, internV(env.string(envGOSTD), "/syscall").any())
	case tokCgoLdflags:

		d.cgoLdflags = append(d.cgoLdflags, expandStmtTokens(v.Args, env)...)
	case tokCgoCflags:

		expanded := expandStmtTokens(v.Args, env)

		d.cgoCflags = append(d.cgoCflags, expanded...)
		d.cFlags = append(d.cFlags, expanded...)
	case tokMavenGroupId, tokAddDllsToJar:

	case tokLicense:

		d.hasLicense = true
	case tokVersion:

		components := anyStrs(expandStmtTokens(v.Args, env))
		value := strings.Join(components, " ")

		env.setFromString(envModver, value)

		if d.setVars == nil {
			d.setVars = map[STR]STR{}
		}

		d.setVars[strModver] = internStr(value)
		d.modver = strings.Join(components, ".")
	case tokToolchain:

		if len(v.Args) == 1 {
			d.toolchainName = v.Args[0].string()
		}
	case tokCheckConfigH:
		if len(v.Args) != 1 {
			throwFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, v.Args[0])
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
		d.decimalMD5 = append(d.decimalMD5, stmt)
	case tokCythonFlags:
		applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokSetAppend, Args: append([]ANY{strCythonOptions.any()}, v.Args...)}, d, env, onWarn)
	case tokBuildwithCythonCpp:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: anyStrs(v.Args[1:]),
			Seq:     d.nextDeclSeq(),
		})

		d.cythonNumpyBeforeInclude = true
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

		d.cythonNumpyBeforeInclude = true
	case tokBisonGenC:
		d.bisonGenExt = strC
	case tokBisonGenCpp:
		d.bisonGenExt = strCpp
	case tokBisonFlags:

		for _, a := range v.Args {
			d.bisonFlags = append(d.bisonFlags, internArg(a.string()).any())
		}
	case tokGrpc:
		if !d.grpc {
			d.grpcOutputIndex = len(d.cppProtoPlugins)
		}

		d.grpc = true

		if isGoModuleType(d.unit.Type) {
			for _, p := range goGrpcImports(env.string(envGOSTD)) {
				d.peerdirs = append(d.peerdirs, internStr(p).any())
			}
		} else {
			d.peerdirs = append(d.peerdirs, strContribLibsGrpc.any())
		}
	case tokPyNamespace:
		if len(v.Args) != 1 {
			throwFmt("gen: PY_NAMESPACE expects exactly 1 argument, got %d", len(v.Args))
		}

		d.pyNamespace = ptr(v.Args[0])
		d.pyModuleNamespace = d.pyNamespace
	case tokProtoToNamespace:
		collectProtoToNamespace(fs, modulePath, v, d, env, onWarn)
	case tokRegisterYqlPythonUdf:
		registerYqlPythonUdf(fs, modulePath, v, d, env, onWarn)
	case tokUdfNoProbe:
		env.setBool(envUdfNoProbe, true)
	case tokYqlLastAbiVersion:
		if len(v.Args) != 0 {
			throwFmt("YQL_LAST_ABI_VERSION expects exactly 0 arguments, got %d", len(v.Args))
		}

		if d.yqlStableABI {
			applyYqlABI(d, env, false)
		} else {
			d.cxxFlags = append(d.cxxFlags, argDuseCurrentUdfAbiVersion.any())
		}
	case tokYqlCurrentAbiVersion, tokYqlStableAbiVersion:
		if len(v.Args) != 0 {
			throwFmt("%s expects no arguments", v.Name.string())
		}

		applyYqlABI(d, env, v.Name == tokYqlCurrentAbiVersion)
	case tokYqlAbiVersion:
		if len(v.Args) != 3 {
			throwFmt("YQL_ABI_VERSION expects exactly 3 arguments, got %d", len(v.Args))
		}

		prefix := "-DUDF_ABI_VERSION_"

		if d.yqlStableABI {
			applyYqlABI(d, env, false)
			prefix = "-DMIN_UDF_ABI_VERSION_"
		}

		d.cxxFlags = append(d.cxxFlags,
			internArg(prefix+"MAJOR="+v.Args[0].string()).any(),
			internArg(prefix+"MINOR="+v.Args[1].string()).any(),
			internArg(prefix+"PATCH="+v.Args[2].string()).any(),
		)
	case tokProtocFatalWarnings:
		if len(v.Args) != 0 {
			throwFmt("PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.protocFlags = append(d.protocFlags, argFatalWarnings.any())
	case tokUseCommonGoogleApis:

		d.useCommonGoogleAPIs = true

		if env.bool(envGO_PROTO) {
			applyGoCommonGoogleApis(v.Args, d)

			break
		}

		const googleapisPeer = "contrib/libs/googleapis-common-protos"
		d.peerdirs = append([]ANY{internStr(googleapisPeer).any()}, d.peerdirs...)
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
	case tokFlatcFlags:
		d.flatcFlags = append(d.flatcFlags, v.Args...)
	case tokCopyFile, tokCopyFileWithContext:
		entry := parseCopyFileEntry(anyStrs(v.Args), v.Name == tokCopyFileWithContext, v.Line)

		d.copyFiles = append(d.copyFiles, entry)

		if entry.Auto {
			dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
			prefix := modulePath + "/"

			if strings.HasPrefix(dstVFS.relString(), prefix) {
				dstRel := strings.TrimPrefix(dstVFS.relString(), prefix)

				if isSourceEligibleForCopyAuto(dstRel) && !d.hasSrc(dstRel) {
					d.srcs = append(d.srcs, SrcMeta{Source: internStr(dstRel).any(), Prio: stmtPrioDefault})
				}

				if d.copyFileAutoOutputs == nil {
					d.copyFileAutoOutputs = make(map[STR]CopyFileEntry)
				}

				d.copyFileAutoOutputs[internStr(dstRel)] = entry
			}
		}
	case tokCopy:
		for _, entry := range parseCopyEntries(anyStrs(v.Args), v.Line) {
			d.copyFiles = append(d.copyFiles, entry)

			if entry.Auto {
				dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
				prefix := modulePath + "/"

				if strings.HasPrefix(dstVFS.relString(), prefix) {
					dstRel := strings.TrimPrefix(dstVFS.relString(), prefix)

					if isSourceEligibleForCopyAuto(dstRel) && !d.hasSrc(dstRel) {
						d.srcs = append(d.srcs, SrcMeta{Source: internStr(dstRel).any(), Prio: stmtPrioDefault})
					}

					if d.copyFileAutoOutputs == nil {
						d.copyFileAutoOutputs = make(map[STR]CopyFileEntry)
					}

					d.copyFileAutoOutputs[internStr(dstRel)] = entry
				}
			}
		}
	case tokProtoNamespace:
		if len(v.Args) == 0 {
			throwFmt("gen: PROTO_NAMESPACE expects at least 1 argument")
		}

		withGen := false

		for _, arg := range v.Args[:len(v.Args)-1] {
			withGen = withGen || arg.string() == "WITH_GEN"
		}

		applyProtoNamespace(d, v.Args[len(v.Args)-1], withGen)
	case tokProtoAddIncl:
		applyProtoAddIncl(d, v.Args)
	case tokUseSdcMeta:
		if d.setVars == nil {
			d.setVars = map[STR]STR{}
		}

		d.setVars[strUSE_SDC_META] = strYes
	case tokGoPackageName:
		if d.setVars == nil {
			d.setVars = map[STR]STR{}
		}

		d.setVars[str_GO_PACKAGE_VALUE] = v.Args[0].str()
	case tokExportYmapsProto:
		applyProtoNamespace(d, mapsDocProtoNS.any(), false)

		d.protoAddInclGlobal = append(d.protoAddInclGlobal, mapsDocProto)
	case tokYmapsSproto:
		if env.bool(envGO_PROTO) {
			break
		}

		for _, argTok := range v.Args {
			if !extIsProto(argTok.string()) {
				throwFmt("gen: %s: YMAPS_SPROTO expects .proto arguments, got %q", modulePath, argTok.string())
			}

			d.ymapsSprotoSrcs = append(d.ymapsSprotoSrcs, argTok)
		}

		if len(v.Args) > 0 {
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

		d.yaConfJSON = append(d.yaConfJSON, v.Args[0])
	case tokAllocator:
		applyAllocatorStmt(v, d)
	case tokArchive:

		applyArchiveStmt(v, d)
	case tokArchiveByKeys:
		applyArchiveByKeysStmt(v, d)
	case tokArchiveAsm:
		applyArchiveAsmStmt(v, d)
	case tokLj21Archive:
		applyLj21ArchiveStmt(v, d)
	case tokDisableDataValidation:
		env.setBool(envValidateData, false)
	case tokEnable:

		for _, aTok := range v.Args {
			a := aTok.string()

			env.setBool(internEnv(a), true)

			switch a {
			case "MUSL_LITE":
				d.muslLite = true
			case "PYBUILD_NO_PYC":
				d.pyBuildNoPYC = true
			case "PYBUILD_NO_PY":
				d.pyBuildNoPY = true
			case "PY_PROTO_MYPY_ENABLED":
				d.noMypy = false
			case "PYTHON_SQLITE3":
				d.pythonSQLite3 = true
			case "USE_LIGHT_PY2CC":
				d.useLightPy2cc = true
			case "USE_ASMLIB":
				d.useAsmlib = true
			}
		}
	case tokDisable:

		for _, aTok := range v.Args {
			a := aTok.string()

			env.setBool(internEnv(a), false)

			if d.setVars == nil {
				d.setVars = map[STR]STR{}
			}

			d.setVars[internStr(a)] = strNo

			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
			}

			if a == "USE_LIGHT_PY2CC" {
				d.useLightPy2cc = false
			}

			if a == "NEED_GOOGLE_PROTO_PEERDIRS" {
				d.needGoogleProtoPeerdirs = false
			}

			if a == "USE_ASMLIB" {
				d.useAsmlib = false
			}
		}
	case tokNoMypy:
		d.noMypy = true
	case tokNoOptimize:
		d.noOptimize = true
	case tokStrip:
		d.strip = true
	case tokNoOptimizePyProtos:
		d.optimizePyProtos = false
		d.optimizePyProtosSet = true
	case tokOptimizePyProtos:
		d.optimizePyProtos = true
		d.optimizePyProtosSet = true
	case tokSrc:

		if len(v.Args) == 0 {
			throwFmt("gen: SRC() requires at least 1 argument (filename); got 0 at line %d", v.Line)
		}

		filename := v.Args[0]

		var extras []ANY

		if len(v.Args) > 1 {
			extras = v.Args[1:]
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

			return
		}

		d.buildMns = append(d.buildMns, &BuildMnStmt{Info: v.Args[0], Name: v.Args[1].string(), Seq: d.nextDeclSeq()})
	case tokLdPlugin:

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case tokArPlugin:

		if len(v.Args) != 1 {
			throwFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}

		d.arPlugin = ptr(internV(v.Args[0].string(), ".pyplugin").any())
	case tokWithDynamicLibs:
		env.setBool(envSoOutputs, true)
	case tokDynamicLibraryFrom:
		if len(v.Args) == 0 {
			throwFmt("gen: DYNAMIC_LIBRARY_FROM expects at least 1 argument")
		}

		d.dynamicLibraryFrom = append(d.dynamicLibraryFrom, v.Args...)
		d.peerdirs = append(d.peerdirs, v.Args...)
	case tokExportsScript:
		if len(v.Args) != 1 {
			throwFmt("gen: EXPORTS_SCRIPT expects exactly 1 argument, got %d", len(v.Args))
		}

		d.exportsScript = ptr(v.Args[0])
	case tokLinkerScript:
		if !isProgramModuleType(d.moduleStmt.Name) {
			throwFmt("%s: LINKER_SCRIPT outside a PROGRAM is not modelled", modulePath)
		}

		for _, arg := range expandStmtTokens(v.Args, env) {
			if !strings.HasSuffix(arg.string(), ".ld") {
				throwFmt("%s: LINKER_SCRIPT %s: only .ld scripts are modelled", modulePath, arg.string())
			}

			d.linkerScripts = append(d.linkerScripts, arg)
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
	case tokSelectClangSAConfig:
		if len(v.Args) != 1 {
			throwFmt("SELECT_CLANG_SA_CONFIG expects one configuration file")
		}

		env.setString(envClangSAConfig, v.Args[0].string())
	case tokUseSAPlugins:
		applySAPlugins(v.Args, d, env)
	case tokUseNasm:
		env.setBool(envUseNasmAssembler, true)
		d.peerdirs = append(d.peerdirs, strBuildExternalResourcesNasm.any())
	case tokUsePython2:
		d.peerdirs = append(d.peerdirs, strContribLibsPython.any())

		usePython2(env, d, unitTagPy2)
	case tokPython2Addincl:
		usePython2(env, d, unitTagPy2Native)
	case tokUsePython3:

		d.peerdirs = append(d.peerdirs,
			strContribLibsPython.any(),
			strLibraryPythonRuntimePy3.any(),
		)

		d.usePython3 = true
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
			enabled = enabled || env.bool(internEnv(arg.string()))
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
	case tokStyleJSON, tokStyleYAML:
		d.collectStyleCheck(fs, modulePath, v, env)
	case tokStyleYAMLConf:
		d.collectYAMLStyleWithConfig(modulePath, env)
	case tokUsrvBuild:
		d.collectUsrvFiles(anyStrs(v.Args))
	case tokUsrvAllSrcs:
		for _, f := range usrvAllSources(fs, modulePath, anyStrs(v.Args)) {
			d.srcs = append(d.srcs, SrcMeta{Source: internStr(f).any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq()})
		}
	case tokTestSrcs:
		d.test.testFiles = append(d.test.testFiles, anyStrs(v.Args)...)

		for _, a := range v.Args {
			if a.string() == "NAMESPACE" || a.string() == "TOP_LEVEL" || a.string() == "__main__.py" {
				throwFmt("TEST_SRCS: %s is not allowed", a.string())
			}
		}

		applyInlinePySrcs(fs, modulePath, append([]ANY{kwNAMESPACE.any(), strTests.any()}, v.Args...), d, env, onWarn)
	case tokPySrcs:
		stmt := v

		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{Statement: &stmt, ResourceOffset: len(d.resources), Prio: stmtPrioSrcs})
	case tokAllPySrcs:
		stmt := v

		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{Statement: &stmt, Prio: stmtPrioSrcs})
	case tokPyMain:

		if len(v.Args) != 1 {
			throwFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}

		arg := strings.ReplaceAll(v.Args[0].string(), "/", ".")

		if !strings.Contains(arg, ":") {
			arg += ":main"
		}

		d.pyMain = ptr(internStr(arg).any())
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

		d.resources = append(d.resources, ResourceEntry{Path: "-", Key: "py/constructors/" + arg})
	case tokNoCheckImports:

		if len(v.Args) > 0 {
			d.noCheckImports = append(d.noCheckImports, v.Args...)
		} else {
			d.noCheckImportsDisabled = true
		}
	case tokCppProtoPlugin0, tokCppProtoPlugin, tokCppProtoPlugin2, tokCppEvProtoPlugin:
		addCPPProtoPlugin(d, parseCPPProtoPlugin(v))
	case tokWithKotlinGrpc:
		d.peerdirs = append(d.peerdirs, strBuildPlatformJavaKotlinGrpc.any())
	case tokCompileNlg:
		applyCompileNlg(modulePath, v, d)
	case tokGenerateTopology:
		_, lists := goMacroLists(v.Args, "ROS_MESSAGE_HEADERS")

		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:           "golaunch_generator",
			ToolPath:       "sdg/sdc/ros/configuration/golaunch/topology/generator",
			OutputSuffixes: []string{".topology.pb.h"},
			Deps:           []string{"sdg/sdc/ros/configuration/golaunch/topology/common_deps"},
			ExtraOutFlag:   strings.Join(anyStrs(lists["ROS_MESSAGE_HEADERS"]), ","),
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

		values := make([]string, 0, len(v.Args)-3)

		for _, value := range v.Args[3:] {
			values = append(values, v.Args[1].string()+value.string()+v.Args[2].string())
		}

		value := strings.Join(values, " ")
		name := v.Args[0].string()

		env.setFromString(internEnv(name), value)

		if d.setVars == nil {
			d.setVars = map[STR]STR{}
		}

		d.setVars[internStr(name)] = internStr(value)
	case tokSetAppend:

		if len(v.Args) >= 1 {
			switch v.Args[0].string() {
			case "GO_PROTO_OPTS":
				for _, a := range v.Args[1:] {
					d.goProtoOpts = append(d.goProtoOpts, GoProtoOpt{arg: a})
				}
			case "SFLAGS":
				d.sFlags = append(d.sFlags, v.Args[1:]...)
			case "_PROTOC_FLAGS":
				d.protocFlags = append(d.protocFlags, v.Args[1:]...)
			case "RPATH_GLOBAL":
				for _, argTok := range v.Args[1:] {
					arg := argTok.string()

					arg = strings.ReplaceAll(arg, `${"$"}`, "$")
					d.rpathFlagsGlobal = append(d.rpathFlagsGlobal, internArg(arg).any())
				}
			}

			name := v.Args[0].string()
			value := strings.Join(anyStrs(v.Args[1:]), " ")

			if prev, ok := env.lookup(name); ok && prev != "" {
				value = prev + " " + value
			}

			env.setFromString(internEnv(name), value)

			if d.setVars == nil {
				d.setVars = map[STR]STR{}
			}

			d.setVars[internStr(name)] = internStr(value)
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
		d.implibs = append(d.implibs, parseImplib(v.Args))
		d.linkExcludeLibraries = append(d.linkExcludeLibraries, v.Args[0])
	case tokLinkExcludeLibraries:
		d.linkExcludeLibraries = append(d.linkExcludeLibraries, v.Args...)
	case tokCudaSrcs:
		arches := env.string(envCUDA_ARCHITECTURES)

		if arches == "" {
			arches = "sm_50"
		}

		d.cudaSrcs = append(d.cudaSrcs, CudaSrcsStmt{Sources: v.Args, Architectures: arches, EmbedPTX: !env.bool(envCUDA_NO_EMBED_PTX)})
	case tokNvccDeviceLink:
		d.nvccDeviceSrcs = append(d.nvccDeviceSrcs, v.Args...)

		if fs.isFilePathClean("build/internal/platform/cuda/ya.make") {
			d.peerdirs = append(d.peerdirs, strBuildInternalPlatformCuda.any())
		}
	case tokCudaNvccFlags:
		id := envCudaNvccFlags

		env.setString(id, strings.TrimSpace(env.string(id)+" "+strings.Join(anyStrs(v.Args), " ")))
	default:

		handled = false

		if !acknowledgedTokSet.has(uint32(v.Name)) {
			warnUnmodelledMacro(onWarn, modulePath, v.Name)

			return
		}

		recordIgnoredMacro(v.Name)
	}
}

type LlvmBcStmt struct {
	Sources             []string
	Name                string
	Suffix              string
	Symbols             []string
	GenerateMachineCode bool
	ClangBCRoot         string
}

func isLlvmBcKeyword(s string) bool {
	switch s {
	case "NAME", "SUFFIX", "SYMBOLS", "GENERATE_MACHINE_CODE", "NO_COMPILE":
		return true
	}

	return false
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
	default:
		return 4
	}
}

func reorderCythonVariantBuckets(d *ModuleData, start int, regIdx []int) {
	n := len(d.cythonCpp) - start

	if n < 2 {
		return
	}

	perm := make([]int, n)

	for i := range perm {
		perm[i] = i
	}

	slices.SortStableFunc(perm, func(a, b int) int {
		return cythonVariantBucket(d.cythonCpp[start+a]) - cythonVariantBucket(d.cythonCpp[start+b])
	})

	identity := true

	for i, p := range perm {
		if i != p {
			identity = false

			break
		}
	}

	if identity {
		return
	}

	stmts := make([]*CythonStmt, n)

	for i, p := range perm {
		stmts[i] = d.cythonCpp[start+p]
	}

	copy(d.cythonCpp[start:], stmts)

	registrations := make([]PyRegistration, n)

	for i, p := range perm {
		registrations[i] = d.pyRegister[regIdx[p]]
	}

	for i, idx := range regIdx {
		d.pyRegister[idx] = registrations[i]
	}
}

func appendPyRegister(d *ModuleData, name string, renameInit bool) {
	d.pyRegister = append(d.pyRegister, PyRegistration{Name: internStr(name), RenameInit: renameInit})
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
			section = STR(0)
		case kwFILES.any():
			section = kwFILES
		case kwEXPERIMENTAL.any():
			section = kwEXPERIMENTAL
		default:
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

func collectPySrcGroup(fs FS, modulePath string, v UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn), group int) {
	if env.bool(envGO_PROTO) {
		return
	}

	topLevel := false
	mainNext := false
	cythonizePy := false
	cythonPlainCpp := false
	cythonCMode := false
	cythonHeader := false
	cythonApiHeader := false
	swigCMode := false
	namespace := d.pyModuleNamespace

	if namespace != nil && namespace.string() == "." {
		topLevel = true
		namespace = nil
	}

	var groupSrcs []string
	var groupScopes []PyScope

	cythonStmtStart := len(d.cythonCpp)
	pyiBefore := len(d.pyPyiResources)

	var cythonDirectives []string

	var cythonRegIdx []int

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
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
		case strCythonCApiH.any():

			cythonCMode = true
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

		src := a.string()
		modNameOverride := ""

		if eq := strings.IndexByte(src, '='); eq >= 0 {
			modNameOverride = src[eq+1:]
			src = src[:eq]
		}

		if extIsPyx(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, src, topLevel, namespace)
			}

			stmt := &CythonStmt{
				Src:       src,
				CMode:     cythonCMode,
				Header:    cythonHeader,
				ApiHeader: cythonApiHeader,
				Options: []string{
					"--module-name", modName,
					"--init-suffix", pythonInitSuffix(modName),
					"--source-root", "$(S)",
					"-X", "set_initial_path=" + modulePath + "/" + src,
				},
			}

			if cythonPlainCpp && !d.usePython2 {
				stmt.Generated = ptr(src + ".cpp")
			}

			d.cythonCpp = append(d.cythonCpp, stmt)
			appendPyRegister(d, modName, false)
			cythonRegIdx = append(cythonRegIdx, len(d.pyRegister)-1)
			mainNext = false

			continue
		}

		if cythonizePy && extIsPy(src) {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, src, topLevel, namespace)
			}

			d.cythonCpp = append(d.cythonCpp, &CythonStmt{
				Src:       src,
				CMode:     cythonCMode,
				Header:    cythonHeader,
				ApiHeader: cythonApiHeader,

				Pxd: strings.ReplaceAll(modName, ".", "/") + ".pxd",
				Options: []string{
					"--module-name", modName,
					"--init-suffix", pythonInitSuffix(modName),
					"--source-root", "$(S)",
					"-X", "set_initial_path=" + modulePath + "/" + src,
				},
			})

			appendPyRegister(d, modName, false)
			cythonRegIdx = append(cythonRegIdx, len(d.pyRegister)-1)
			mainNext = false

			continue
		}

		if extIsSwg(src) {
			modName := modNameOverride

			if modName == "" {
				ns := strings.ReplaceAll(modulePath, "/", ".") + "."

				if topLevel {
					ns = ""
				}

				modName = ns + strings.ReplaceAll(strings.TrimSuffix(src, ".swg"), "/", ".")
			}

			if swigCMode {
				d.swigC = append(d.swigC, SwigSrc{Src: src, Module: modName})
				appendPyRegister(d, modName+"_swg", true)
			}

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

			d.pyPyiResources = expandResourceFiles(d, d.pyPyiResources, []string{"DEST", dest, src})
			mainNext = false

			continue
		}

		if strings.Contains(a.string(), "=") && !extIsPy(src) {
			continue
		}

		d.pySrcs = append(d.pySrcs, internStr(src).any())
		groupSrcs = append(groupSrcs, src)
		groupScopes = append(groupScopes, PyScope{TopLevel: topLevel, Namespace: namespace})

		if mainNext {
			modName := modNameOverride

			if modName == "" {
				modName = pythonModuleName(modulePath, src, topLevel, namespace)
			}

			d.pyMain = ptr(internV(modName, ":main").any())
			mainNext = false
		} else if d.pyMain == nil && d.moduleStmt != nil &&
			(d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) &&
			(src == "__main__.py" || strings.HasSuffix(src, "/__main__.py")) {
			d.pyMain = ptr(internStr(pythonModuleName(modulePath, src, topLevel, namespace)).any())
		}
	}

	if len(cythonDirectives) > 0 {
		for j := cythonStmtStart; j < len(d.cythonCpp); j++ {
			d.cythonCpp[j].Options = append(d.cythonCpp[j].Options, cythonDirectives...)
		}
	}

	reorderCythonVariantBuckets(d, cythonStmtStart, cythonRegIdx)

	d.pySrcGroups[group].Srcs = internAnys(groupSrcs)
	d.pySrcGroups[group].Scopes = groupScopes
	d.pySrcGroups[group].PyiStart = pyiBefore
	d.pySrcGroups[group].PyiEnd = len(d.pyPyiResources)
}

func pythonModuleName(modulePath, src string, topLevel bool, namespace *ANY) string {
	ns := strings.ReplaceAll(modulePath, "/", ".") + "."

	if namespace != nil {
		ns = strings.TrimSuffix(namespace.string(), ".") + "."
	}

	if topLevel {
		ns = ""
	}

	modName := strings.TrimSuffix(src, ".py")

	modName = strings.TrimSuffix(modName, ".pyx")
	modName = strings.ReplaceAll(modName, "/", ".")

	return ns + modName
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

func applyProtoNamespace(d *ModuleData, namespace ANY, withGen bool) {
	d.protoNamespace = ptr(namespace)

	protoSourceRoot := protoIncludeDir(namespace.string())
	protoBuildRoot := protoSourceRoot.rel().build()

	if withGen {
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, protoBuildRoot)
	}

	if protoSourceRoot.relString() != "" {
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, protoSourceRoot)
	}

	d.addLocalIncl(prioAddIncl, protoBuildRoot)
	d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
	d.addInclUserGlobal = append(d.addInclUserGlobal, protoBuildRoot)
}

func protoIncludeDir(rel string) VFS {
	rel = filepath.ToSlash(filepath.Clean(rel))

	if rel == "." {
		rel = ""
	}

	return source(rel)
}

func applyProtoAddIncl(d *ModuleData, args []ANY) {
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

	if withGen {
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, bld)
	}

	if src.relString() != "" {
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, src)
	}

	d.addLocalIncl(prioAddIncl, bld)

	if global {
		d.addInclGlobal = append(d.addInclGlobal, bld)
		d.addInclUserGlobal = append(d.addInclUserGlobal, bld)
	}
}

func applyArchiveStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		seenName   bool
		inNameSlot bool
	)

	for _, aTok := range v.Args {
		a := aTok.string()

		switch {
		case inNameSlot:
			entry.Name = a
			inNameSlot = false
			seenName = true
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

	if !seenName || entry.Name == "" {
		throwFmt("gen: ARCHIVE expects `NAME <output>` (line %d)", v.Line)
	}

	if len(entry.Files) == 0 {
		throwFmt("gen: ARCHIVE(NAME %s) has no input files (line %d)", entry.Name, v.Line)
	}

	d.archives = append(d.archives, entry)
}

func applyArchiveByKeysStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		seenName   bool
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
			seenName = true
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

	if inKeysSlot {
		throwFmt("gen: ARCHIVE_BY_KEYS(KEYS ...) missing value after KEYS (line %d)", v.Line)
	}

	if !seenName || entry.Name == "" {
		throwFmt("gen: ARCHIVE_BY_KEYS expects `NAME <output>` (line %d)", v.Line)
	}

	if len(entry.Files) == 0 {
		throwFmt("gen: ARCHIVE_BY_KEYS(NAME %s) has no input files (line %d)", entry.Name, v.Line)
	}

	d.archives = append(d.archives, entry)
}

func applyArchiveAsmStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveAsmEntry
		seenName   bool
		inNameSlot bool
	)

	for _, aTok := range v.Args {
		a := aTok.string()

		switch {
		case inNameSlot:
			entry.Name = a
			inNameSlot = false
			seenName = true
		case a == "NAME":
			inNameSlot = true
		case a == "DONTCOMPRESS":
			entry.DontCompress = true
		default:
			entry.Files = append(entry.Files, a)
		}
	}

	if inNameSlot {
		throwFmt("gen: ARCHIVE_ASM(NAME ...) missing value after NAME (line %d)", v.Line)
	}

	if !seenName || entry.Name == "" {
		throwFmt("gen: ARCHIVE_ASM expects `NAME <output>` (line %d)", v.Line)
	}

	if len(entry.Files) == 0 {
		throwFmt("gen: ARCHIVE_ASM(NAME %s) has no input files (line %d)", entry.Name, v.Line)
	}

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

	if len(luas) == 0 {
		throwFmt("gen: LJ_21_ARCHIVE has no .lua files (line %d)", v.Line)
	}

	d.lj21 = &Lj21Archive{Luas: luas}

	raws := make([]string, len(luas))

	for i, lua := range luas {
		raws[i] = strings.TrimSuffix(lua, ".lua") + ".raw"
	}

	d.archives = append(d.archives,
		ArchiveEntry{Name: "LuaScripts.inc", DontCompress: true, Files: raws, Keys: luas},
		ArchiveEntry{Name: "LuaSources.inc", DontCompress: true, Files: append([]string(nil), luas...), Keys: luas},
	)
}

func applyAllocatorStmt(v UnknownStmt, d *ModuleData) {
	if len(v.Args) != 1 {
		throwFmt("gen: ALLOCATOR expects exactly 1 argument, got %d (line %d)", len(v.Args), v.Line)
	}

	name := v.Args[0]

	if _, ok := allocatorPeers[name.string()]; !ok {
		throwFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name.string(), v.Line)
	}

	d.hadAllocator = true
	d.allocatorName = name
}

func isProgramModuleType(name TOK) bool {
	switch name {
	case tokProgram, tokYqlPrebuiltProgram, tokPy2Program, tokPy3Program, tokPy3ProgramBin, tokPy3Test, tokPy2Test, tokPytestBin, tokUnittestFor, tokUnittest, tokUnittestWithCustomEntryPoint, tokYtUnittest, tokGtest, tokGBenchmark, tokFuzz, tokBoosttest, tokBoosttestWithMain, tokGoProgram, tokGoTest, tokGoDll:
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

func pyLibraryAutoPythonPeer(name TOK) bool {
	switch name {
	case tokPy3Library, tokPy23Library, tokPy2Library, tokPy3ProgramBin,
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
			out = append(out, v)

			continue
		}

		out = append(out, path.str().source())
	}

	return out
}

func parseModulePathVFS(path string) VFS {
	if vfsHasPrefix(path) {
		return intern(path)
	}

	return source(path)
}

func expandStmtToken(s string, env Environment) string {
	return expandToken(s, env, tokenExpandLazy)
}

type TokenExpansion uint8

func (phase TokenExpansion) clearsUnbound(name string) bool {
	return phase == tokenExpandFinal || phase == tokenExpandArguments && !keepDeferredVar(name) || phase == tokenExpandAssignment && name != envBINDIR.string() && name != envCURDIR.string()
}

func keepDeferredVar(name string) bool {
	switch name {
	case envBINDIR.string(), envCURDIR.string(), "S", "B", "MANAGED_PEERS", "MANAGED_PEERS_CLOSURE", "APPLIED_EXCLUDES":
		return true
	}

	return strings.HasSuffix(name, resourceGlobalSuffix) || strings.HasPrefix(name, "_") && strings.HasSuffix(name, "__INCLUDE")
}

func expandToken(s string, env Environment, phase TokenExpansion) string {
	if strings.Contains(s, "\\$") {
		s = strings.ReplaceAll(s, "\\$", "$")
	}

	if s == "$S" {
		return "$(S)"
	}

	if s == "$B" {
		return "$(B)"
	}

	for i := 0; i < 8; i++ {
		prev := s

		if strings.HasPrefix(s, "$") && !strings.HasPrefix(s, "${") {
			name := strings.TrimPrefix(s, "$")

			if isExpandVarName(name) {
				if val, ok := env.lookup(name); ok || phase.clearsUnbound(name) {
					s = val
				}
			}
		}

		s = expandEmbeddedDollarVars(s, env, phase)
		s = expandBracedVars(s, env, phase)

		if s == prev {
			break
		}
	}

	return s
}

func expandBracedVars(s string, env Environment, phase TokenExpansion) string {
	searchFrom := 0

	for {
		start := strings.Index(s[searchFrom:], "${")

		if start < 0 {
			return s
		}

		start += searchFrom

		end := strings.IndexByte(s[start+2:], '}')

		if end < 0 {
			return s
		}

		end += start + 2

		name := s[start+2 : end]
		val, ok := env.lookup(name)

		if !ok && phase.clearsUnbound(name) && name != "" && !strings.ContainsAny(name, " ${}\"';") {
			ok = true
		}

		if name == "" || strings.ContainsAny(name, "${") || !ok {
			searchFrom = end + 1

			continue
		}

		s = s[:start] + val + s[end+1:]
		searchFrom = start + len(val)
	}
}

func expandEmbeddedDollarVars(s string, env Environment, phase TokenExpansion) string {
	if !strings.Contains(s, "$") {
		return s
	}

	var buf [192]byte

	b := buf[:0]
	changed := false

	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) || s[i+1] == '{' || s[i+1] == '(' {
			b = append(b, s[i])
			i++

			continue
		}

		j := i + 1

		for j < len(s) {
			c := s[j]

			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
				j++

				continue
			}

			break
		}

		if j == i+1 {
			b = append(b, s[i])
			i++

			continue
		}

		name := s[i+1 : j]
		val, ok := env.lookup(name)

		if !ok && !phase.clearsUnbound(name) {
			b = append(b, s[i:j]...)
			i = j

			continue
		}

		b = append(b, val...)
		i = j
		changed = true
	}

	if !changed {
		return s
	}

	return string(b)
}

func expandInFiles(files []ANY, noParse int, env Environment, evaluated bool) ([]ANY, int) {
	split := len(files) - noParse
	phase := tokenExpandLazy

	if evaluated {
		phase = tokenExpandArguments
	}

	parsed := expandTokens(files[:split:split], env, false, phase)

	if noParse == 0 {
		return parsed, 0
	}

	text := expandTokens(files[split:], env, false, phase)

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
	anyDollar := false

	for _, item := range items {
		if strHasDollar(item.str().any()) {
			anyDollar = true

			break
		}
	}

	if !anyDollar {
		return items
	}

	out := make([]ANY, 0, len(items))

	for _, item := range items {
		if !strHasDollar(item.str().any()) {
			out = append(out, item)

			continue
		}

		expanded := expandToken(item.string(), env, phase)

		var words []string

		if command {
			words = splitShellWords(expanded)
		} else {
			words = strings.Fields(expanded)
		}

		for _, f := range words {
			out = append(out, internAny(f))
		}
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
	matcher := regexp.MustCompile("^(?:" + globSegmentRegex(pattern, true) + ")$")

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

	matchDir := func(dirRel string) {
		view := fs.listdir(dirKey(dirRel))
		names := make([]string, 0, len(view.names))

		for _, packed := range view.names {
			if packed&1 != 0 {
				continue
			}

			name := STR(packed >> 1).string()

			if !matcher.MatchString(name) || name == "ya.make" || name == "a.yaml" || excluded(dirRel, name) {
				continue
			}

			names = append(names, name)
		}

		sort.Strings(names)

		for _, name := range names {
			files = append(files, filePrefix(dirRel)+name)
		}
	}

	subDirsSorted := func(dirRel string) []string {
		view := fs.listdir(dirKey(dirRel))

		var subs []string

		for _, packed := range view.names {
			if packed&1 == 0 {
				continue
			}

			subs = append(subs, dirRel+"/"+STR(packed>>1).string())
		}

		sort.Strings(subs)

		return subs
	}

	for _, dir := range dirs {
		root := rootRel(dir)

		if !recursive {
			matchDir(root)

			continue
		}

		for queue := []string{root}; len(queue) > 0; queue = queue[1:] {
			matchDir(queue[0])
			queue = append(queue, subDirsSorted(queue[0])...)
		}
	}

	env.setFromString(internEnv(prefix+"_FILES"), strings.Join(files, " "))
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
	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)
	value := expandToken(s, env, tokenExpandAssignment)

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)

	return value
}

func applyAllPySrcs(fs FS, modulePath string, v UnknownStmt, d *ModuleData, group int) {
	dirs := []string{"."}
	noTestFiles := false
	recursive := false

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a.string() {
		case "TOP_LEVEL":
			d.pyTopLevel = true
		case "NAMESPACE":
			i++

			if i >= len(v.Args) {
				throwFmt("ALL_PY_SRCS NAMESPACE expects a value")
			}

			d.pyNamespace = ptr(v.Args[i])
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

	moduleRootRel := modulePath

	appendDirPyFiles := func(dirRel string) {
		view := fs.listdir(dirKey(dirRel))
		names := make([]string, 0, len(view.names))

		for _, packed := range view.names {
			if packed&1 != 0 {
				continue
			}

			name := STR(packed >> 1).string()

			if filepath.Ext(name) != ".py" {
				continue
			}

			if noTestFiles && (strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")) {
				continue
			}

			names = append(names, name)
		}

		sort.Strings(names)

		for _, name := range names {
			files = append(files, strings.TrimPrefix(dirRel+"/"+name, moduleRootRel+"/"))
		}
	}

	subDirsSorted := func(dirRel string) []string {
		view := fs.listdir(dirKey(dirRel))

		var subs []string

		for _, packed := range view.names {
			if packed&1 == 0 {
				continue
			}

			child := dirRel + "/" + STR(packed>>1).string()

			if fs.isFile(dirKey(child), "ya.make") {
				continue
			}

			subs = append(subs, child)
		}

		sort.Strings(subs)

		return subs
	}

	for _, dir := range dirs {
		walkRoot := filepath.ToSlash(filepath.Join(moduleRootRel, dir))

		if !recursive {
			appendDirPyFiles(walkRoot)

			continue
		}

		order := make([]string, 0, 16)

		for queue := []string{walkRoot}; len(queue) > 0; queue = queue[1:] {
			subs := subDirsSorted(queue[0])

			order = append(order, subs...)
			queue = append(queue, subs...)
		}

		order = append(order, walkRoot)

		for _, dirRel := range order {
			appendDirPyFiles(dirRel)
		}
	}

	d.pySrcs = append(d.pySrcs, internAnys(files)...)

	if len(files) > 0 {
		if d.pyYapycSuffix == "" {
			d.pyYapycSuffix = pySrcYapycSuffix(modulePath)
		}

		scopes := make([]PyScope, len(files))

		for i := range scopes {
			scopes[i] = PyScope{TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}
		}

		d.pySrcGroups[group].Srcs = internAnys(files)
		d.pySrcGroups[group].Scopes = scopes
	}
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
	ctx, parent, d := e.ctx, e.instance, e.d
	demand := e.peerDemand()
	language := peerEntryLanguage(parent, d)

	if language == LangPy2 && !(peerYaMakeExists(ctx.fs, peerVFS) && hasPy2Variant(ctx.moduleName(peerVFS.relString()))) {
		language = LangCPP
	}

	if isGoModuleType(d.moduleStmt.Name) && peerYaMakeExists(ctx.fs, peerVFS) {
		switch peerName := ctx.moduleName(peerVFS.relString()); {
		case peerName == tokProtoLibrary || peerName == tokFbsLibrary || peerName == tokRosLibrary || peerName == tokMlaunchLibrary || peerName == tokMlaunchComponent || peerName == tokSdcDiagsLibrary:
			language = LangGo

			fallthrough
		case isGoModuleType(peerName):
			if demand == demandNone {
				demand = demandSelf
			}
		}
	}

	if d.ros != nil && peerYaMakeExists(ctx.fs, peerVFS) && ctx.moduleName(peerVFS.relString()) == tokRosLibrary {
		language = parent.Language
	}

	if d.multimodule == tokMlaunchLibrary && parent.Language == LangPackage && ctx.moduleName(peerVFS.relString()) == tokMlaunchLibrary {
		language = LangPackage
	}

	if peerYaMakeExists(ctx.fs, peerVFS) && ctx.moduleName(peerVFS.relString()) == tokPackage {
		language = LangPackage
	}

	peer := ModuleInstance{
		Path:     peerVFS,
		Kind:     KindLib,
		Language: language,
		Platform: parent.Platform,
		Demand:   demand,
	}

	if d.yqlCurrent && peerYaMakeExists(ctx.fs, peerVFS) {
		name := ctx.moduleName(peerVFS.relString())

		peer.YqlCurrent = name == tokYqlLibrary || isYqlUdfStaticModule(name)
	}

	if demand == demandNone && ctx.roots[peer] {
		peer.Demand = ctx.rootDemand
	}

	return peer
}

func hasPy2Variant(name TOK) bool {
	switch name {
	case tokPy23Library, tokPy23NativeLibrary, tokProtoLibrary:
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

	if d.setVars[strCOMPILE_NLG_EMPTY_CPP] == strYes {
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

	d.srcs = append(d.srcs, SrcMeta{Source: str("register.cpp"), Prio: stmtPrioDefault, Global: true})
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

func appendCompilerFlags(args []ANY, env Environment, global, own []ANY) ([]ANY, []ANY) {
	nextGlobal := false

	appendArg := func(arg ANY) {
		if arg == kwGLOBAL.any() {
			nextGlobal = true

			return
		}

		if nextGlobal {
			global = append(global, arg)
		} else {
			own = append(own, arg)
		}

		nextGlobal = false
	}

	for _, arg := range args {
		arg = expandStmtTokenAny(arg, env)

		text := arg.string()

		if text == "" {
			continue
		}

		if strings.IndexByte(text, ' ') < 0 {
			appendArg(arg)

			continue
		}

		for _, word := range strings.Split(text, " ") {
			if word != "" {
				appendArg(internStr(word).any())
			}
		}
	}

	return global, own
}
