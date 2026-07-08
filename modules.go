package main

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

type CppProtoPlugin struct {
	Name                      string
	ToolPath                  string
	OutputSuffixes            []string
	Deps                      []string
	ExtraOutFlag              string
	Experimental              []string
	Files                     []string
	DeclaredBeforeLiteHeaders bool
}

func (p CppProtoPlugin) isYaff() bool {
	return p.ToolPath == yaffPluginPath
}

func (p CppProtoPlugin) isExperimental(protoBaseName string) bool {
	for _, e := range p.Experimental {
		if e == protoBaseName {
			return true
		}
	}

	return false
}

func (p CppProtoPlugin) processesFile(protoBaseName string) bool {
	if len(p.Files) == 0 {
		return true
	}

	for _, f := range p.Files {
		if f == protoBaseName {
			return true
		}
	}

	return false
}

func addCPPProtoPlugin(d *ModuleData, plugin CppProtoPlugin) {
	d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	d.peerdirs = append(d.peerdirs, internAnys(plugin.Deps)...)
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

type ModuleData struct {
	moduleStmt               *ModuleStmt
	modver                   string
	hasLicense               bool
	cgoSrcs                  []ANY
	cgoLdflags               []ANY
	cgoCflags                []ANY
	hasBisonY                bool
	toolchainName            string
	srcs                     []ANY
	srcExtraFlat             []SrcFlatEntry
	globalSrcs               []ANY
	pySrcs                   []ANY
	pySrcGroups              []PySrcGroup
	pyPyiResources           []ResourceEntry
	pyBuildNoPYC             bool
	pyBuildNoPY              bool
	pyTopLevel               bool
	pyYapycSuffix            string
	noExtendedPySearch       bool
	enumSrcs                 []*GenerateEnumSerializationStmt
	peerdirs                 []ANY
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
	asmAddIncl               []VFS
	protoAddInclGlobal       []VFS
	llvmBc                   []*LlvmBcStmt
	cFlags                   []ANY
	cFlagsGlobal             []ANY
	cxxFlags                 []ANY
	cxxFlagsGlobal           []ANY
	cOnlyFlags               []ANY
	cOnlyFlagsGlobal         []ANY
	sFlags                   []ANY
	protocFlags              []ANY
	flatcFlags               []ANY
	clangWarnings            []ANY
	ldFlags                  []ANY
	rpathFlagsGlobal         []ANY
	objAddLibsGlobal         []ANY
	srcDirs                  []VFS
	flags                    FlagSet
	hadAllocator             bool
	allocatorName            ANY
	muslLite                 bool
	cudaNvccFlags            []ANY
	muslEnabled              bool
	useAsmlib                bool
	useArcadiaLibm           bool
	splitDwarf               bool
	noPythonIncl             bool
	noImportTracing          bool
	usePython3               bool
	useCommonGoogleAPIs      bool
	moduleScopeCFlags        []ANY
	pythonSQLite3            bool
	pyNamespace              *ANY
	protoNamespace           *ANY
	ymapsSprotoSrcs          []ANY
	noMypy                   bool
	noOptimize               bool
	optimizePyProtos         bool
	optimizePyProtosSet      bool
	needGoogleProtoPeerdirs  bool
	cppProtoPlugins          []CppProtoPlugin
	excludeTags              map[STR]bool
	dynamicLibraryFrom       []ANY
	exportsScript            *ANY
	ldPlugins                []ANY
	arPlugin                 *ANY
	perSrcCFlags             map[ANY][]ANY
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
	runPython                []*RunPythonStmt
	fromSandboxes            []*FromSandboxStmt
	checkConfigHeaders       []ANY
	cythonCpp                []*CythonStmt
	cythonNumpyBeforeInclude bool
	swigC                    []SwigSrc
	bisonGenExt              STR
	grpc                     bool
	yaConfJSON               []ANY
	allPySrcs                []UnknownStmt
	archives                 []ArchiveEntry
	archiveAsm               []ArchiveAsmEntry
	lj21                     *Lj21Archive
	copyFiles                []CopyFileEntry
	copyFileAutoOutputs      map[STR]CopyFileEntry
	flatSrcs                 map[ANY]struct{}
	srcMeta                  map[ANY]SrcMeta
	declSeq                  int
	resources                []ResourceEntry
	bundles                  []BundleEntry
	pyMain                   *ANY
	noStrip                  bool
	unit                     ModuleUnit
	noCheckImports           []ANY
	noCheckImportsDisabled   bool
	pyRegister               []STR
	pyRegisterExplicit       []bool
	simdSrcs                 []SimdSrc
	ragel6Flags              []ANY
	bisonFlags               []ANY
	conflictMod              *ModuleStmt
	resourceDeclStmts        []*DeclareResourceStmt
	primaryOutput            string
	inducedDeps              ParsedIncludeSet
	setVars                  map[STR]STR
	tc                       ModuleToolchain
	cc                       ModuleCompileEnv
}

func (d *ModuleData) perSrcCFlagsFor(src ANY) *[]ANY {
	if len(d.perSrcCFlags) == 0 {
		return nil
	}

	if v, ok := d.perSrcCFlags[src]; ok {
		return &v
	}

	return nil
}

func (d *ModuleData) flatSrc(src ANY) bool {
	if len(d.flatSrcs) == 0 {
		return false
	}

	_, ok := d.flatSrcs[src]

	return ok
}

type SrcMeta struct {
	Source      ANY
	Prio        int
	Seq         int
	Generated   bool
	SecondLevel bool
	Global      bool
	Bucket      int
}

func (m SrcMeta) sortKey() uint64 {
	var round uint64

	if m.Generated {
		round = 1
	}

	if m.SecondLevel {
		round = 2
	}

	return round<<48 | uint64(m.Prio)<<40 | uint64(uint32(m.Seq))<<8 | uint64(uint8(m.Bucket))
}

func (d *ModuleData) nextDeclSeq() int {
	d.declSeq++

	return d.declSeq
}

func (d *ModuleData) setSrcMeta(src ANY, prio, seq int) {
	if d.srcMeta == nil {
		d.srcMeta = map[ANY]SrcMeta{}
	}

	d.srcMeta[src] = SrcMeta{Prio: prio, Seq: seq}
}

func (d *ModuleData) srcMetaOf(src ANY) SrcMeta {
	m, ok := d.srcMeta[src]

	if !ok {
		m = SrcMeta{Prio: stmtPrioDefault}
	}

	m.Source = src

	return m
}

func muslCFlags(on bool) []ANY {
	if on {
		return []ANY{argDMusl.any()}
	}

	return nil
}

type BundleEntry struct {
	Target string
	Name   string
	Suffix string
}

type ResourceEntry struct {
	Path      string
	Key       string
	EndsBatch bool
}

type PySrcGroup struct {
	Srcs      []ANY
	TopLevel  bool
	Namespace *ANY
}

type SrcFlatEntry struct {
	Src   ANY
	Flags []ANY
	Seq   int
}

type ArchiveEntry struct {
	Name                   string
	DontCompress           bool
	Files                  []string
	Keys                   []string
	PropagateSourceMembers bool
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
	Text           bool
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
	text := false

	for i < len(args) {
		switch args[i] {
		case "AUTO":
			auto = true
			i++
		case "TEXT":
			text = true
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
		WithContext: withContext || text,
		Text:        text,
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

		out = append(out, CopyFileEntry{
			Src:            src,
			Dst:            file,
			Auto:           auto,
			WithContext:    withContext,
			OutputIncludes: append([]string(nil), outputIncludes...),
		})
	}

	return out
}

func sourceInputVFS(fs FS, moduleDir VFS, path string) *VFS {
	modulePath := moduleDir.relString()

	if vfs := moduleRootedVFS(modulePath, path); vfs != nil {
		return vfs
	}

	clean := filepath.ToSlash(filepath.Clean(path))

	if clean == "." || clean == "" {
		return ptr(moduleDir)
	}

	if clean == modulePath ||
		(len(clean) > len(modulePath) && clean[len(modulePath)] == '/' && clean[:len(modulePath)] == modulePath) {
		return ptr(source(clean))
	}

	if fs != nil {
		if fs.isFile(moduleDir.rel(), clean) {
			if strings.Contains(clean, "..") {
				return ptr(source(filepath.ToSlash(filepath.Clean(modulePath + "/" + clean))))
			}

			return ptr(source(modulePath, "/", clean))
		}

		if fs.isFile(srcRootRel, clean) {
			return ptr(source(clean))
		}
	}

	return nil
}

func copyFileInputVFS(fs FS, moduleDir VFS, src string) VFS {
	if vfs := sourceInputVFS(fs, moduleDir, src); vfs != nil {
		return *vfs
	}

	return sourceJoinClean(moduleDir.relString(), src)
}

func moduleRootedVFS(modulePath string, path string) *VFS {
	if vfsHasPrefix(path) {
		return ptr(intern(path))
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}/"):
		return ptr(sourceClean(strings.TrimPrefix(path, "${ARCADIA_ROOT}/")))
	case strings.HasPrefix(path, "${CURDIR}/"):
		return ptr(sourceJoinClean(modulePath, strings.TrimPrefix(path, "${CURDIR}/")))
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}/"):
		return ptr(buildClean(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}/")))
	case strings.HasPrefix(path, "${BINDIR}/"):
		return ptr(buildJoinClean(modulePath, strings.TrimPrefix(path, "${BINDIR}/")))
	default:
		return nil
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs := moduleRootedVFS(modulePath, dst); vfs != nil {
		return *vfs
	}

	return buildJoinClean(modulePath, dst)
}

func resourceOutputVFS(modulePath string, path string) VFS {
	if vfs := moduleRootedVFS(modulePath, path); vfs != nil {
		return *vfs
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

func collectModuleInto(pm *IncludeParserManager, dd *DeDuper, instance ModuleInstance, stmts []Stmt, env Environment, onWarn func(Warn), d *ModuleData) *ModuleData {
	fs := pm.fs
	modulePath := instance.Path.relString()
	kind := instance.Kind

	env.setString(envMODDIR, modulePath)
	env.setVFS(envCURDIR, source(modulePath))
	env.setVFS(envBINDIR, build(modulePath))

	d.reset()

	d.pythonSQLite3 = true
	d.useAsmlib = true
	d.bisonGenExt = strCpp
	d.needGoogleProtoPeerdirs = true

	collectStmts(fs, modulePath, kind, instance.Language, stmts, env, d)

	if len(d.pySrcs) > 0 {
		d.pyYapycSuffix = pySrcYapycSuffix(modulePath)
	}

	d.materializeAddIncl()

	d.srcDirs = append([]VFS{instance.Path}, d.srcDirs...)
	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = nil
	d.cfAddInclGlobal = nil
	filterInvalidAddIncl(fs, dd, d, modulePath, onWarn)

	if d.unit.Tag == unitTagPy3BinLib {
		d.pyMain = nil
	}

	d.muslEnabled = env.bool(envMUSL)
	d.useArcadiaLibm = env.bool(envUSE_ARCADIA_LIBM) && !env.bool(envOS_EMSCRIPTEN)
	d.noStrip = env.bool(envNO_STRIP)

	if d.muslLite {
		d.flags.NoUtil = true
	}

	if env.bool(envPY3_PROTO) {
		d.usePython3 = true
	}

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)
	applyArchiveAddIncl(modulePath, d)
	applyCythonHeaderAddIncl(modulePath, d)

	cflagPrefix := append(muslCFlags(d.muslEnabled && !effectiveNoPlatform(d.flags)), sseBaseCFlags(env.bool(envARCH_X86_64))...)

	d.moduleScopeCFlags = append(cflagPrefix, d.moduleScopeCFlags...)
	d.addIncl = dedup(d.addIncl, nil)
	d.addInclGlobal = dedup(d.addInclGlobal, nil)

	for _, a := range d.addIncl {
		pm.indexAddincl(a)
	}

	for _, a := range d.addInclGlobal {
		pm.indexAddincl(a)
	}

	hasProto := false
	hasSc := false
	hasCfgProto := false

	for _, src := range d.srcs {
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
		}
	}

	evInduced, protoInduced := false, false

	for _, src := range d.srcs {
		switch srcExtClassOf(src) {
		case srcExtEv:
			if !evInduced {
				evInduced = true
				d.peerdirs = append(d.peerdirs, strLibraryCppEventlog.any(), strContribLibsProtobuf.any())
			}
		case srcExtProto:
			if !protoInduced {
				protoInduced = true

				isProtoLibrary := d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary

				if !isProtoLibrary || !env.bool(envPY3_PROTO) {
					d.peerdirs = append(d.peerdirs, strContribLibsProtobuf.any())
				}
			}
		}
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

	if d.moduleStmt != nil && (d.moduleStmt.Name == tokProtoLibrary || hasProto) {
		d.protoCmdPeers = protoCmdPeers(d)
	}

	if len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 || len(d.pyRegister) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	if len(d.antlr4Grammars) > 0 || len(d.antlrRuns) > 0 {
		d.peerdirs = append(d.peerdirs, internStr("build/platform/java/jdk/jdk17").any())
	}

	return d
}

func appendGlobalSrcEvent(d *ModuleData, src ANY) {
	d.globalSrcs = append(d.globalSrcs, src)
}

func appendGlobalSrcGroup(d *ModuleData, srcs []ANY) {
	d.globalSrcs = append(d.globalSrcs, srcs...)
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

func applyPython3AddIncl(modulePath string, d *ModuleData) {
	if d.moduleStmt == nil {
		return
	}

	if !d.usePython3 && !pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		return
	}

	if modulePath == "contrib/libs/python" {
		return
	}

	d.usePython3 = true
	d.moduleScopeCFlags = append(d.moduleScopeCFlags, argDusePython3.any())
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
	case tokPy3Library, tokPy3Program, tokPy3ProgramBin,
		tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}

func collectStmts(fs FS, modulePath string, kind ModuleKind, language Language, stmts []Stmt, env Environment, d *ModuleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			if v.Name == tokPy3Program && kind == KindBin {
				d.peerdirs = append([]ANY{internStr(modulePath).any()}, d.peerdirs...)
			}

			if v.Name == tokUnittestFor {
				const unittestMainPeer = "library/cpp/testing/unittest_main"

				d.peerdirs = append(d.peerdirs, internStr(unittestMainPeer).any())

				if len(v.Args) > 0 {
					d.peerdirs = append(d.peerdirs, internStr(path.Clean(v.Args[0].string())).any())
				}
			}

			if isYqlUdfStaticModule(v.Name) {
				d.peerdirs = append(d.peerdirs, internAnys(yqlUdfImplicitPeers())...)
			}

			d.unit = resolveModuleUnit(v.Name, kind, language)

			if v.Schema && d.unit.Tag == unitTagPy3Proto {
				d.unit.HashTag = internStr("PY_PROTO_FROM_SCHEMA")
			}

			d.moduleStmt = moduleStmtForKind(v, d.unit.Type)

			if d.moduleStmt.Name == tokProtoLibrary {
				if language == LangPy {
					env.setBool(envPY3_PROTO, true)
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

			env.setString(envMODULE_LANG, moduleLang)

		case *SrcsStmt:

			routeAllToGlobal := d.moduleStmt != nil && isYqlUdfStaticModule(d.moduleStmt.Name)
			globalNext := false
			globalSrcs := make([]ANY, 0, len(v.Sources))

			for _, srcTok := range expandStmtTokens(v.Sources, env) {
				if srcTok == kwGLOBAL.any() {
					globalNext = true

					continue
				}

				if strHasDollar(srcTok) && strings.Contains(srcTok.string(), "${") {
					continue
				}

				if routeAllToGlobal {
					globalSrcs = append(globalSrcs, srcTok)
				} else if globalNext {
					appendGlobalSrcEvent(d, srcTok)
					globalNext = false
				} else {
					d.srcs = append(d.srcs, srcTok)
					d.setSrcMeta(srcTok, stmtPrioSrcs, d.nextDeclSeq())
				}

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

			if routeAllToGlobal {
				appendGlobalSrcGroup(d, globalSrcs)
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
			}
		case *SetStmt:

			value := expandScalarVarRef(v.Value, env)

			env.setFromString(v.NameEnv, value)

			if d.setVars == nil {
				d.setVars = map[STR]STR{}
			}

			d.setVars[internStr(v.Name)] = internStr(value)

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
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
			d.protoAddInclGlobal = append(d.protoAddInclGlobal, expandConfigVFSPaths(v.ProtoGlobalPaths, env)...)
		case *CFlagsStmt:

			d.cFlagsGlobal = append(d.cFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cFlags = append(d.cFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *CXXFlagsStmt:

			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cxxFlags = append(d.cxxFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *CONLYFlagsStmt:

			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cOnlyFlags = append(d.cOnlyFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, expandStmtTokens(v.Flags, env)...)
		case *SrcDirStmt:

			for _, dirTok := range expandStmtTokens(v.Dirs, env) {
				dir := dirTok.string()

				d.srcDirs = append(d.srcDirs, dirKey(dir).source())
			}
		case *GlobalSrcsStmt:
			appendGlobalSrcGroup(d, expandStmtTokens(v.Sources, env))
		case *GenerateEnumSerializationStmt:
			expandedEN := *v

			expandedEN.DeclSeq = d.nextDeclSeq()
			d.enumSrcs = append(d.enumSrcs, &expandedEN)

			const enumSerPeer = "tools/enum_parser/enum_serialization_runtime"

			if modulePath != enumSerPeer {
				d.peerdirs = append(d.peerdirs, internStr(enumSerPeer).any())
			}
		case *DefaultVarStmt:

			if d.defaultVars == nil {
				d.defaultVars = map[STR]STR{}
			}

			if _, exists := d.defaultVars[internStr(v.VarName)]; !exists {
				d.defaultVars[internStr(v.VarName)] = internStr(expandScalarVarRef(v.Value, env))
				d.defaultVarOrder = append(d.defaultVarOrder, internStr(v.VarName))
			}

			env.setDefaultString(v.NameEnv, expandScalarVarRef(v.Value, env))
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
			expanded := *v

			expanded.ToolPath = expandStmtTokenAny(v.ToolPath, env)
			expanded.Args = expandStmtTokens(v.Args, env)
			expanded.INFiles = expandStmtTokens(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokens(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.ToolPaths = expandStmtTokens(v.ToolPaths, env)

			if v.StdoutFile != nil {
				stdout := expandStmtTokenAny((*v.StdoutFile), env)

				expanded.StdoutFile = &stdout
			}

			if v.CWD != nil {
				cwd := expandStmtTokenAny(*v.CWD, env)

				expanded.CWD = &cwd
			}

			expanded.DeclSeq = d.nextDeclSeq()
			d.runPrograms = append(d.runPrograms, &expanded)
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
			expanded.Args = expandStmtTokens(v.Args, env)
			expanded.INFiles = expandStmtTokens(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokens(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)

			if v.StdoutFile != nil {
				stdout := expandStmtTokenAny((*v.StdoutFile), env)

				expanded.StdoutFile = &stdout
			}

			if v.CWD != nil {
				cwd := expandStmtTokenAny(*v.CWD, env)

				expanded.CWD = &cwd
			}

			d.runPython = append(d.runPython, &expanded)
		case *FromSandboxStmt:
			expanded := *v

			expanded.ResourceId = expandStmtTokenAny(v.ResourceId, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.Renames = expandStmtTokens(v.Renames, env)

			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *ResourceStmt:
			ensureResourcePeer(modulePath, d)

			for i, pair := range v.Pairs {
				d.resources = append(d.resources, ResourceEntry{
					Path:      pair.Path,
					Key:       pair.Key,
					EndsBatch: i == len(v.Pairs)-1,
				})
			}
		case *ResourceFilesStmt:
			ensureResourcePeer(modulePath, d)

			expanded := expandResourceFiles(anyStrs(expandStmtTokens(v.Args, env)))

			for i, e := range expanded {
				if i == len(expanded)-1 {
					e.EndsBatch = true
				}

				d.resources = append(d.resources, e)
			}
		case *AllResourceFilesStmt:
			ensureResourcePeer(modulePath, d)

			expanded := expandAllResourceFiles(fs, modulePath, env, v)

			for i, e := range expanded {
				if i == len(expanded)-1 {
					e.EndsBatch = true
				}

				d.resources = append(d.resources, e)
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

			collectStmts(fs, modulePath, kind, language, taken, env, d)
		case *UnknownStmt:

			expanded := *v

			expanded.Args = expandStmtTokens(v.Args, env)
			applyUnknownStmt(fs, modulePath, expanded, d, env)
		default:
			throwFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

type ModuleUnit struct {
	Type        TOK
	Tag         STR
	HashTag     STR
	CCTag       STR
	ARPrefix    string
	GlobalARTag STR
	SbomLang    STR
}

func resolveModuleUnit(name TOK, kind ModuleKind, language Language) ModuleUnit {
	u := resolveModuleUnitBare(name, kind, language)

	u.HashTag = u.Tag

	return u
}

func resolveModuleUnitBare(name TOK, kind ModuleKind, language Language) ModuleUnit {
	if name == tokPy3Program && kind == KindLib {
		name = tokPy3Library

		return ModuleUnit{Type: name, Tag: unitTagPy3BinLib, ARPrefix: "libpy3", GlobalARTag: tagPy3BinLibGlobal, SbomLang: unitSbomPy3}
	}

	switch name {
	case tokGoLibrary, tokGoProgram:
		return ModuleUnit{Type: name, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy3Program:
		return ModuleUnit{Type: name, Tag: unitTagPy3Bin, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	case tokPy2Program, tokPy3ProgramBin:
		tag := STR(0)

		if name == tokPy3ProgramBin {
			tag = unitTagPy3
		}

		return ModuleUnit{Type: name, Tag: tag, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomPy3}
	case tokPy3Library:
		return ModuleUnit{Type: name, Tag: unitTagPy3, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy2Library:
		return ModuleUnit{Type: name, ARPrefix: "libpy3", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokPy23Library:
		return ModuleUnit{Type: name, Tag: unitTagPy3, CCTag: tagPy3, ARPrefix: "libpy3", GlobalARTag: tagPy3Global, SbomLang: unitSbomCpp}
	case tokPy23NativeLibrary:
		return ModuleUnit{Type: name, Tag: unitTagPy3, CCTag: tagPy3Native, ARPrefix: "libpy3c", GlobalARTag: tagPy3NativeGlobal, SbomLang: unitSbomCpp}
	case tokYqlUdfYdb, tokYqlUdfContrib:
		return ModuleUnit{Type: name, CCTag: tagYqlUdfStatic, ARPrefix: "lib", GlobalARTag: tagYqlUdfStaticGlobal, SbomLang: unitSbomCpp}
	case tokFbsLibrary:
		return ModuleUnit{Type: name, CCTag: tagCppFbs, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
	case tokProtoLibrary:
		if language == LangPy {
			return ModuleUnit{Type: name, Tag: unitTagPy3Proto, ARPrefix: "libpy3", GlobalARTag: tagPy3ProtoGlobal, SbomLang: unitSbomCpp}
		}

		return ModuleUnit{Type: name, Tag: strCPPProto, CCTag: tagCppProto, ARPrefix: "lib", GlobalARTag: tagCppProtoGlobal, SbomLang: unitSbomCpp}
	}

	return ModuleUnit{Type: name, ARPrefix: "lib", GlobalARTag: tagGlobal, SbomLang: unitSbomCpp}
}

func moduleStmtForKind(stmt *ModuleStmt, unitType TOK) *ModuleStmt {
	if stmt.Name == unitType {
		return stmt
	}

	out := *stmt

	out.Name = unitType

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

func applyUnknownStmt(fs FS, modulePath string, v UnknownStmt, d *ModuleData, env Environment) {
	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name, v.Args)
		}
	}()

	switch v.Name {
	case tokDeclareInDirs:
		applyDeclareInDirs(fs, modulePath, v, env)
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

		jsonRel := v.Args[1].string()

		if suffix, ok := strings.CutPrefix(jsonRel, "$(S)/"); ok {
			jsonRel = cleanRel(suffix)
		} else {
			jsonRel = cleanRel(joinRel(modulePath, jsonRel))
		}

		bundle := readResourceBundleJSON(fs, jsonRel)

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
	case tokNoRuntime:

		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case tokNoPlatform:

		d.flags.NoPlatform = true
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case tokNoCompilerWarnings:
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
	case tokStyleRuff:

		for i := 0; i < len(v.Args); i++ {
			switch v.Args[i].string() {
			case "CONFIG_TYPE":
				i++
			case "CHECK_FORMAT":
			case "RUN_IN_SOURCE_ROOT":
			}
		}
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

		if stmt.Name == "" {
			throwFmt("LLVM_BC: NAME keyword is required (got args %v)", v.Args)
		}

		d.llvmBc = append(d.llvmBc, stmt)

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

	case tokGoTestSrcs, tokGoXtestSrcs, tokGoSkipTests, tokGoEmbedPattern:

	case tokCgoSrcs:

		d.cgoSrcs = append(d.cgoSrcs, expandStmtTokens(v.Args, env)...)
		d.peerdirs = append(d.peerdirs, internStr(goStdPrefix+"/syscall").any())
	case tokCgoLdflags:

		d.cgoLdflags = append(d.cgoLdflags, expandStmtTokens(v.Args, env)...)
	case tokCgoCflags:

		expanded := expandStmtTokens(v.Args, env)

		d.cgoCflags = append(d.cgoCflags, expanded...)
		d.cFlags = append(d.cFlags, expanded...)
	case tokMavenGroupId:

	case tokLicense:

		d.hasLicense = true
	case tokVersion:

		d.modver = strings.Join(anyStrs(expandStmtTokens(v.Args, env)), ".")
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
	case tokBuildwithCythonCpp:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: anyStrs(v.Args[1:]),
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
		d.grpc = true
		d.peerdirs = append(d.peerdirs, strContribLibsGrpc.any())
	case tokPyNamespace:
		if len(v.Args) != 1 {
			throwFmt("gen: PY_NAMESPACE expects exactly 1 argument, got %d", len(v.Args))
		}

		d.pyNamespace = ptr(v.Args[0])
	case tokYqlLastAbiVersion:
		if len(v.Args) != 0 {
			throwFmt("YQL_LAST_ABI_VERSION expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags, argDuseCurrentUdfAbiVersion.any())
	case tokYqlAbiVersion:
		if len(v.Args) != 3 {
			throwFmt("YQL_ABI_VERSION expects exactly 3 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags,
			internArg("-DUDF_ABI_VERSION_MAJOR="+v.Args[0].string()).any(),
			internArg("-DUDF_ABI_VERSION_MINOR="+v.Args[1].string()).any(),
			internArg("-DUDF_ABI_VERSION_PATCH="+v.Args[2].string()).any(),
		)
	case tokProtocFatalWarnings:
		if len(v.Args) != 0 {
			throwFmt("PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.protocFlags = append(d.protocFlags, argFatalWarnings.any())
	case tokUseCommonGoogleApis:

		d.useCommonGoogleAPIs = true
		const googleapisPeer = "contrib/libs/googleapis-common-protos"
		d.peerdirs = append([]ANY{internStr(googleapisPeer).any()}, d.peerdirs...)
	case tokClangWarnings:
		d.clangWarnings = append(d.clangWarnings, expandStmtTokens(v.Args, env)...)
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

				if isSourceEligibleForCopyAuto(dstRel) && !strsContain(d.srcs, dstRel) {
					d.srcs = append(d.srcs, internStr(dstRel).any())
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

					if isSourceEligibleForCopyAuto(dstRel) && !strsContain(d.srcs, dstRel) {
						d.srcs = append(d.srcs, internStr(dstRel).any())
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

		applyProtoNamespace(d, v.Args[len(v.Args)-1])
	case tokExportYmapsProto:

		d.protoAddInclGlobal = append(d.protoAddInclGlobal, mapsDocProto)

		applyProtoNamespace(d, mapsDocProtoNS.any())
	case tokYmapsSproto:

		for _, argTok := range v.Args {
			if !extIsProto(argTok.string()) {
				throwFmt("gen: %s: YMAPS_SPROTO expects .proto arguments, got %q", modulePath, argTok.string())
			}

			d.ymapsSprotoSrcs = append(d.ymapsSprotoSrcs, argTok)
		}

		if len(v.Args) > 0 {
			d.peerdirs = append(d.peerdirs, strMapsLibsSproto.any())
		}
	case tokExcludeTags:

		if d.excludeTags == nil {
			d.excludeTags = make(map[STR]bool)
		}

		for _, argTok := range v.Args {
			arg := argTok.string()

			switch arg {
			case "GO_PROTO", "JAVA_PROTO":
			}

			d.excludeTags[internStr(arg)] = true
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
			case "USE_ASMLIB":
				d.useAsmlib = true
			}
		}
	case tokDisable:

		for _, aTok := range v.Args {
			a := aTok.string()

			env.setBool(internEnv(a), false)

			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
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

		if slices.Contains(d.srcs, filename) {
			d.srcExtraFlat = append(d.srcExtraFlat, SrcFlatEntry{Src: filename, Flags: extras, Seq: d.nextDeclSeq()})

			break
		}

		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[ANY]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
		d.setSrcMeta(filename, stmtPrioDefault, d.nextDeclSeq())

		if extras != nil {
			if d.perSrcCFlags == nil {
				d.perSrcCFlags = map[ANY][]ANY{}
			}

			d.perSrcCFlags[filename] = append(d.perSrcCFlags[filename], extras...)
		}
	case tokSrcCNoLto:

		if len(v.Args) != 1 {
			throwFmt("gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got %d at line %d", len(v.Args), v.Line)
		}

		filename := v.Args[0]

		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[ANY]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
		d.setSrcMeta(filename, stmtPrioDefault, d.nextDeclSeq())
	case tokSrcCAvx, tokSrcCAvx2, tokSrcCAvx512, tokSrcCAmx, tokSrcCSse2, tokSrcCSse3, tokSrcCSsse3,
		tokSrcCSse4, tokSrcCSse41, tokSrcCXop:

		variant, ok := simdVariantFor(v.Name)

		if !ok {
			throwFmt("gen: unrecognised SIMD-permutation macro %q at line %d (simdVariants table out of sync)", v.Name, v.Line)
		}

		if len(v.Args) == 0 {
			throwFmt("gen: %s() requires at least 1 argument (filename); got 0 at line %d", v.Name, v.Line)
		}

		filename := v.Args[0]
		flags := concat(variant.CFlags, anyStrs(v.Args[1:]))

		d.simdSrcs = append(d.simdSrcs, SimdSrc{
			Src:     filename,
			Variant: variant.Suffix,
			CFlags:  flags,
			Seq:     d.nextDeclSeq(),
		})
	case tokBuildMn:

		if len(v.Args) != 2 {
			throwFmt("gen: %s: BUILD_MN with options is not modelled, expected (MnInfo MnName), got %d args", modulePath, len(v.Args))
		}

		d.buildMns = append(d.buildMns, &BuildMnStmt{Info: v.Args[0], Name: v.Args[1].string(), Seq: d.nextDeclSeq()})
	case tokLdPlugin:

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case tokArPlugin:

		if len(v.Args) != 1 {
			throwFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}

		d.arPlugin = ptr(internV(v.Args[0].string(), ".pyplugin").any())
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
	case tokExtralibs:

		libs := make([]string, 0, len(v.Args))

		for _, argTok := range v.Args {
			lib := argTok.string()

			if !strings.HasPrefix(lib, "-") {
				lib = "-l" + lib
			}

			libs = append(libs, lib)
		}

		if len(libs) > 0 {
			d.objAddLibsGlobal = append(d.objAddLibsGlobal, internArg(strings.Join(libs, " ")).any())
		}
	case tokUsePython3:

		d.peerdirs = append(d.peerdirs,
			strContribLibsPython.any(),
			strLibraryPythonRuntimePy3.any(),
		)

		d.usePython3 = true
	case tokPySrcs:

		topLevel := false
		mainNext := false
		cythonizePy := false
		cythonPlainCpp := false
		cythonCMode := false
		cythonHeader := false
		cythonApiHeader := false
		swigCMode := false

		var namespace *ANY
		var groupSrcs []string

		cythonStmtStart := len(d.cythonCpp)

		var cythonDirectives []string

		var cythonRegIdx []int

		for i := 0; i < len(v.Args); i++ {
			a := v.Args[i]

			switch a {
			case kwTOP_LEVEL.any():
				topLevel = true
				d.pyTopLevel = true

				continue
			case kwNAMESPACE.any():
				i++

				if i >= len(v.Args) {
					throwFmt("PY_SRCS NAMESPACE expects a value")
				}

				namespace = ptr(v.Args[i])
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

				if cythonPlainCpp {
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
					appendPyRegister(d, modName+"_swg", false)
				}

				mainNext = false

				continue
			}

			if extIsPyi(src) {
				modName := modNameOverride

				if modName == "" {
					modName = pythonModuleName(modulePath, strings.TrimSuffix(src, ".pyi"), topLevel, namespace)
				}

				dest := "py/" + strings.ReplaceAll(modName, ".", "/") + ".pyi"

				d.pyPyiResources = append(d.pyPyiResources, expandResourceFiles([]string{"DEST", dest, src})...)
				mainNext = false

				continue
			}

			if strings.Contains(a.string(), "=") && !extIsPy(src) {
				continue
			}

			d.pySrcs = append(d.pySrcs, internStr(src).any())
			groupSrcs = append(groupSrcs, src)

			if mainNext {
				ns := strings.ReplaceAll(modulePath, "/", ".") + "."

				if topLevel {
					ns = ""
				}

				modName := modNameOverride

				if modName == "" {
					modName = strings.TrimSuffix(src, ".py")
					modName = strings.ReplaceAll(modName, "/", ".")
					modName = ns + modName
				}

				d.pyMain = ptr(internV(modName, ":main").any())
				mainNext = false
			} else if d.pyMain == nil && d.moduleStmt != nil &&
				(d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) &&
				(src == "__main__.py" || strings.HasSuffix(src, "/__main__.py")) {
				ns := strings.ReplaceAll(modulePath, "/", ".") + "."

				if topLevel {
					ns = ""
				}

				modName := strings.TrimSuffix(src, ".py")

				modName = strings.ReplaceAll(modName, "/", ".")
				d.pyMain = ptr(internV(ns, modName).any())
			}
		}

		if len(cythonDirectives) > 0 {
			for j := cythonStmtStart; j < len(d.cythonCpp); j++ {
				d.cythonCpp[j].Options = append(d.cythonCpp[j].Options, cythonDirectives...)
			}
		}

		reorderCythonVariantBuckets(d, cythonStmtStart, cythonRegIdx)

		if len(groupSrcs) > 0 {
			d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{
				Srcs:      internAnys(groupSrcs),
				TopLevel:  topLevel,
				Namespace: namespace,
			})
		}
	case tokAllPySrcs:
		d.allPySrcs = append(d.allPySrcs, v)
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
	case tokCppProtoPlugin0, tokCppProtoPlugin, tokCppProtoPlugin2:
		addCPPProtoPlugin(d, parseCPPProtoPlugin(v))
	case tokAliceCapability:
		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:           "alice_capability_cpp",
			ToolPath:       "yandex_io/tools/capability_gen",
			OutputSuffixes: []string{".cap.h"},
			Deps:           []string{"yandex_io/libs/protobuf_utils"},
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
				throwFmt("gen: %s: APPHOST: unexpected argument %q", modulePath, v.Args[i])
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
		plugin := parseYAFF(v)

		plugin.DeclaredBeforeLiteHeaders = protoTransitiveHeadersEnabled(d)
		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	case tokYaffSchema:
		plugin := parseYAFFSchema(v)

		plugin.DeclaredBeforeLiteHeaders = protoTransitiveHeadersEnabled(d)
		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	case tokPyRegister:

		for _, nameTok := range v.Args {
			name := nameTok.string()

			appendPyRegister(d, name, true)
		}
	case tokSetAppend:

		if len(v.Args) >= 1 {
			switch v.Args[0].string() {
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
	case tokCudaNvccFlags:
		d.cudaNvccFlags = append(d.cudaNvccFlags, v.Args...)
	default:

		handled = false

		if !acknowledgedTokSet.has(uint32(v.Name)) {
			throwFmt("gen: macro %q not modelled — implement its upstream semantics (see yatool/build/conf, yatool/build/ymake.core.conf)", v.Name.string())
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
	NoCompile           bool
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

	if len(regIdx) != n {
		return
	}

	names := make([]STR, n)
	explicit := make([]bool, n)

	for i, p := range perm {
		names[i] = d.pyRegister[regIdx[p]]
		explicit[i] = d.pyRegisterExplicit[regIdx[p]]
	}

	for i, idx := range regIdx {
		d.pyRegister[idx] = names[i]
		d.pyRegisterExplicit[idx] = explicit[i]
	}
}

func appendPyRegister(d *ModuleData, name string, explicit bool) {
	d.pyRegister = append(d.pyRegister, internStr(name))
	d.pyRegisterExplicit = append(d.pyRegisterExplicit, explicit)

	dot := strings.LastIndexByte(name, '.')

	if dot < 0 {
		return
	}

	shortname := name[dot+1:]
	mangled := pythonInitSuffix(name)

	d.cFlags = append(d.cFlags,
		internArg("-DPyInit_"+shortname+"=PyInit_"+mangled).any(),
		internArg("-Dinit_module_"+shortname+"=init_module_"+mangled).any(),
	)
}

func parseCPPProtoPlugin(v UnknownStmt) CppProtoPlugin {
	requiredArgs := 0
	outputSuffixes := 0

	switch v.Name {
	case tokCppProtoPlugin0:
		requiredArgs = 2
	case tokCppProtoPlugin:
		requiredArgs = 3
		outputSuffixes = 1
	case tokCppProtoPlugin2:
		requiredArgs = 4
		outputSuffixes = 2
	default:
		throwFmt("gen: internal error: parseCPPProtoPlugin called for %q", v.Name)
	}

	if len(v.Args) < requiredArgs {
		throwFmt("gen: %s expects at least %d arguments, got %d", v.Name, requiredArgs, len(v.Args))
	}

	plugin := CppProtoPlugin{
		Name:     v.Args[0].string(),
		ToolPath: v.Args[1].string(),
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
			throwFmt("gen: %s got unexpected tail token %q; supported suffixes are DEPS and EXTRA_OUT_FLAG", v.Name, v.Args[tail])
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

func parseYAFF(v UnknownStmt) CppProtoPlugin {
	s := parseYAFFSections(v)

	if len(s.positional) != 0 {
		throwFmt("gen: YAFF got unexpected positional argument %q", s.positional[0])
	}

	return CppProtoPlugin{
		Name:           "yaff",
		ToolPath:       yaffPluginPath,
		OutputSuffixes: []string{".yaff.h", ".yaff.cpp"},
		ExtraOutFlag:   yaffExtraOutFlag("namespace="+s.namespace, s),
		Experimental:   s.experimental,
		Files:          s.files,
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
		Experimental:   s.experimental,
		Files:          s.files,
	}
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
	segs := strings.Split(name, ".")

	if len(segs) == 1 {
		return name
	}

	var mangled strings.Builder

	for _, seg := range segs {
		fmt.Fprintf(&mangled, "%d%s", len(seg), seg)
	}

	return mangled.String()
}

func applyProtoNamespace(d *ModuleData, namespace ANY) {
	d.protoNamespace = ptr(namespace)

	protoBuildRoot := build(filepath.ToSlash(filepath.Clean(namespace.string())))

	d.addIncl = append(d.addIncl, protoBuildRoot)
	d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
	d.addInclUserGlobal = append(d.addInclUserGlobal, protoBuildRoot)
}

func applyArchiveStmt(v UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		seenName   bool
		inNameSlot bool
	)

	entry.PropagateSourceMembers = true

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
	entry.PropagateSourceMembers = true

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
		ArchiveEntry{Name: "LuaScripts.inc", DontCompress: true, Files: raws, Keys: luas, PropagateSourceMembers: true},
		ArchiveEntry{Name: "LuaSources.inc", DontCompress: true, Files: append([]string(nil), luas...), Keys: luas, PropagateSourceMembers: true},
	)
}

func applyAllocatorStmt(v UnknownStmt, d *ModuleData) {
	if len(v.Args) != 1 {
		throwFmt("gen: ALLOCATOR expects exactly 1 argument, got %d (line %d)", len(v.Args), v.Line)
	}

	name := v.Args[0]

	if _, ok := allocatorPeers[name.string()]; !ok {
		throwFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name, v.Line)
	}

	d.hadAllocator = true
	d.allocatorName = name
}

func isProgramModuleType(name TOK) bool {
	switch name {
	case tokProgram, tokPy2Program, tokPy3Program, tokPy3ProgramBin, tokUnittestFor, tokGoProgram:
		return true
	}

	return false
}

func isYqlUdfStaticModule(name TOK) bool {
	switch name {
	case tokYqlUdfYdb, tokYqlUdfContrib:
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
		tokPy2Program, tokPy3Program:
		return true
	}

	return false
}

func pyLibraryAutoPythonPeer(name TOK) bool {
	switch name {
	case tokPy3Library, tokPy23Library, tokPy2Library, tokPy3ProgramBin,
		tokPy2Program, tokPy3Program:
		return true
	}

	return false
}

func isPythonModuleType(name TOK) bool {
	return isPyLibraryType(name) || name == tokPy3ProgramBin
}

func isSpecializedLibraryType(name TOK) bool {
	switch name {
	case tokProtoLibrary,
		tokDll, tokSoProgram, tokDynamicLibrary:
		return true
	}

	return false
}

func isResourceContainerType(name TOK) bool {
	switch name {
	case tokPackage, tokUnion, tokResourcesLibrary:
		return true
	}

	return false
}

func buildIfEnv(instance ModuleInstance) Environment {
	env := instance.Platform.ifEnv.clone()

	if instance.Path != 0 {
		env.setVFS(envCURDIR, instance.Path)
		env.setVFS(envBINDIR, instance.Path.rel().build())
		env.setStringID(envMODDIR, instance.Path.rel())
	}

	return env
}

func expandConfigVFSPaths(paths []ANY, env Environment) []VFS {
	expanded := expandStmtTokens(paths, env)
	out := make([]VFS, 0, len(expanded))

	for _, path := range expanded {
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
				if val, ok := env.lookup(name); ok {
					s = val
				}
			}
		}

		s = expandEmbeddedDollarVars(s, env)
		s = expandBracedVars(s, env)

		if s == prev {
			break
		}
	}

	return s
}

func expandBracedVars(s string, env Environment) string {
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

		if !isExpandVarName(name) || !ok {
			searchFrom = end + 1

			continue
		}

		s = s[:start] + val + s[end+1:]
		searchFrom = start + len(val)
	}
}

func expandEmbeddedDollarVars(s string, env Environment) string {
	if !strings.Contains(s, "$") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	changed := false

	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) || s[i+1] == '{' || s[i+1] == '(' {
			b.WriteByte(s[i])
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
			b.WriteByte(s[i])
			i++

			continue
		}

		name := s[i+1 : j]
		val, ok := env.lookup(name)

		if !ok {
			b.WriteString(s[i:j])
			i = j

			continue
		}

		b.WriteString(val)
		i = j
		changed = true
	}

	if !changed {
		return s
	}

	return b.String()
}

func expandStmtTokens(items []ANY, env Environment) []ANY {
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

		for _, f := range strings.Fields(expandStmtToken(item.string(), env)) {
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

	rootRel := func(dir string) string {
		switch {
		case srcdir == "${ARCADIA_ROOT}":
			return path.Clean(dir)
		case srcdir == "":
			return path.Clean(modulePath + "/" + dir)
		default:
			return path.Clean(modulePath + "/" + srcdir + "/" + dir)
		}
	}

	filePrefix := func(dirRel string) string {
		if srcdir == "${ARCADIA_ROOT}" {
			return "${ARCADIA_ROOT}/" + dirRel + "/"
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

			if ok, _ := path.Match(pattern, name); !ok || excluded(dirRel, name) {
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

func expandStmtTokensStrings(items []string, env Environment) []string {
	out := make([]string, 0, len(items))

	for _, item := range items {
		if !strings.Contains(item, "$") {
			out = append(out, item)

			continue
		}

		out = append(out, strings.Fields(expandStmtToken(item, env))...)
	}

	return out
}

func isExpandVarName(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		b := s[i]

		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' {
			continue
		}

		return false
	}

	return true
}

func expandScalarVarRef(s string, env Environment) string {
	return expandStmtToken(s, env)
}

func applyAllPySrcs(fs FS, modulePath string, v UnknownStmt, d *ModuleData) {
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
		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{
			Srcs:      internAnys(files),
			TopLevel:  d.pyTopLevel,
			Namespace: d.pyNamespace,
		})
	}
}

func peerEntryLanguage(parent ModuleInstance, parentModuleName TOK) Language {
	if isPythonModuleType(parentModuleName) {
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
	_, parent, d := e.ctx, e.instance, e.d

	return ModuleInstance{
		Path:     peerVFS,
		Kind:     KindLib,
		Language: peerEntryLanguage(parent, d.moduleStmt.Name),
		Platform: parent.Platform,
	}
}

func moduleExcludesTag(d *ModuleData, tag string) bool {
	return d != nil && d.excludeTags != nil && d.excludeTags[internStr(tag)]
}
