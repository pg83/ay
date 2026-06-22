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

type CppProtoPlugin struct {
	Name           string
	ToolPath       string
	OutputSuffixes []string
	Deps           []string
	ExtraOutFlag   string
	// Experimental holds the YaFF EXPERIMENTAL proto basenames: such a proto gets the
	// experiments C++ generator, whose .yaff.h additionally #includes the experiments
	// runtime.
	Experimental []string
	// Files holds the YaFF FILES proto basenames: when non-empty, the plugin writes
	// .yaff.h content only for these protos; every other one is opened but left empty.
	Files []string
	// DeclaredBeforeLiteHeaders records whether PROTOC_TRANSITIVE_HEADERS was still on
	// when this plugin was parsed; CPP_PROTO_OUTS accumulates in statement order, so a
	// plugin declared before SET(...,"no") appends ahead of the cpp_out group. See emitPB.
	DeclaredBeforeLiteHeaders bool
}

// isYaff reports whether this is the YaFF protoc plugin, emitting a .yaff.h/.yaff.cpp pair.
func (p CppProtoPlugin) isYaff() bool {
	return p.ToolPath == yaffPluginPath
}

// isExperimental reports whether the proto's basename is in the experimental whitelist.
func (p CppProtoPlugin) isExperimental(protoBaseName string) bool {
	for _, e := range p.Experimental {
		if e == protoBaseName {
			return true
		}
	}

	return false
}

// processesFile reports whether the plugin writes content for the proto (FILES
// whitelist empty or containing its basename); a non-whitelisted header is emitted empty.
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

// event2cpp is the C++ proto plugin CPP_EVLOG() registers.
const (
	event2cppPluginName = "event2cpp"
	event2cppToolPath   = "tools/event2cpp"
)

// addCPPProtoPlugin registers a C++ proto plugin on the module; its DEPS become
// module PEERDIRs.
func addCPPProtoPlugin(d *ModuleData, plugin CppProtoPlugin) {
	d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	d.peerdirs = append(d.peerdirs, STRS(plugin.Deps...)...)
}

// protoCmdPeers returns the C++ proto plugin DEPS (plugin-runtime peers) a
// PROTO_LIBRARY induces, in plugin order, deduped. These peers lead the module's GLOBAL
// ADDINCL order, ahead of the declared PEERDIR closure, because proto codegen runs as a
// per-source command whose plugin-runtime peer is induced ahead for include-dir
// propagation. The base proto runtime is NOT front-loaded; only the plugin DEPS move,
// so this list is kept separate from d.peerdirs.
func protoCmdPeers(d *ModuleData) []STR {
	front := make([]STR, 0, len(d.cppProtoPlugins))
	seen := map[STR]struct{}{}

	// GRPC()'s contrib/libs/grpc is a plugin-runtime DEP that leads the peer order like
	// any other. We model GRPC as d.grpc + a plain contrib/libs/grpc peerdir, so surface
	// it here as a front peer too.
	if d.grpc {
		seen[strContribLibsGrpc] = struct{}{}
		front = append(front, strContribLibsGrpc)
	}

	for _, plugin := range d.cppProtoPlugins {
		for _, dep := range plugin.Deps {
			p := internStr(dep)

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			front = append(front, p)
		}
	}

	return front
}

type ModuleData struct {
	moduleStmt    *ModuleStmt
	modver        string // VERSION() args joined by "." (MODVER); "" means "unknown"
	hasLicense    bool   // LICENSE() present — gates the _GEN_SBOM_COMPONENT DX node
	hasBisonY     bool   // a .y bison source — induces PEERDIR build/induced/by_bison
	toolchainName string // TOOLCHAIN(Name) arg — gates the toolchain SBOM DX node
	srcs          []STR
	// srcExtraFlat holds SRC(file flags…) where `file` is also in SRCS: SRC adds a
	// separate FLAT object with its own flags alongside the regular SRCS object.
	srcExtraFlat []SrcFlatEntry
	globalSrcs   []STR
	pySrcs       []STR
	// pySrcsFullName parallels pySrcs: true when the py3cc module-name argument is the
	// full root-relative path, false for the bare token. Irrelevant for source-tree
	// entries (full path either way).
	pySrcsFullName     []bool
	pySrcGroups        []PySrcGroup
	pyPyiResources     []ResourceEntry
	pyBuildNoPYC       bool
	pyBuildNoPY        bool
	pyTopLevel         bool
	noExtendedPySearch bool
	enumSrcs           []*GenerateEnumSerializationStmt
	peerdirs           []STR
	// protoCmdPeers are the proto plugin DEPS leading the GLOBAL ADDINCL order, ahead of
	// the declared PEERDIR closure. Subset of peerdirs; affects ADDINCL order only.
	protoCmdPeers      []STR
	joinSrcs           []*JoinSrcsStmt
	addIncl            []VFS
	addInclP           []prioVFS
	addInclGlobal      []VFS
	addInclOneLevel    []VFS
	addInclUserGlobal  []VFS
	cfAddIncl          []VFS
	cfAddInclGlobal    []VFS
	cythonAddIncl      []VFS
	asmAddIncl         []VFS
	protoAddInclGlobal []VFS
	unhandledMacros    map[STR][]STR
	llvmBc             []*LlvmBcStmt
	cFlags             []ARG
	cFlagsGlobal       []ARG
	cxxFlags           []ARG
	cxxFlagsGlobal     []ARG
	cOnlyFlags         []ARG
	cOnlyFlagsGlobal   []ARG
	sFlags             []ARG
	protocFlags        []ARG
	flatcFlags         []ARG
	// clangWarnings is CLANG_WARNINGS(...) from the autoincluded linter config; emitted
	// on C/C++ compiles between GCC_COMPILE_FLAGS and CXXFLAGS.
	clangWarnings    []ARG
	ldFlags          []ARG
	rpathFlagsGlobal []ARG
	objAddLibsGlobal []ARG
	// srcDirs is the cumulative SRCDIR search path: index 0 is the module dir, then
	// explicit SRCDIRs in declaration order; searched in reverse.
	srcDirs             []VFS
	flags               FlagSet
	hadAllocator        bool
	allocatorName       STR
	muslLite            bool
	muslEnabled         bool
	useArcadiaLibm      bool
	splitDwarf          bool
	noPythonIncl        bool
	noImportTracing     bool
	usePython3          bool
	useCommonGoogleAPIs bool
	moduleScopeCFlags   []ARG
	pythonSQLite3       bool
	pyNamespace         *STR
	protoNamespace      *STR
	// ymapsSprotoSrcs holds the .proto sources named by YMAPS_SPROTO(...); each gets a
	// .sproto.h producer, and proto imports induce the .sproto.h sibling besides .pb.h.
	ymapsSprotoSrcs     []STR
	noMypy              bool
	noOptimize          bool
	optimizePyProtos    bool
	optimizePyProtosSet bool
	// needGoogleProtoPeerdirs (default yes) drives the PEERDIR to the protobuf
	// builtin-proto peer for a PY-only proto; the protobuf builtins DISABLE it.
	needGoogleProtoPeerdirs bool
	cppProtoPlugins         []CppProtoPlugin
	excludeTags             map[STR]bool
	dynamicLibraryFrom      []STR
	exportsScript           *STR
	ldPlugins               []STR
	arPlugin                *STR

	perSrcCFlags map[STR][]ARG

	// hasFbs marks a .fbs in d.srcs — genModule's flatbuffers auto-peer gate.
	hasFbs bool

	// hasFbs64 is the .fbs64 twin (flatbuffers64 auto-peer gate).
	hasFbs64 bool

	defaultVars map[STR]STR

	defaultVarOrder    []STR
	configureFiles     []*ConfigureFileStmt
	createBuildInfoFor *STR
	antlr4Grammars     []Antlr4GrammarInfo
	antlrRuns          []AntlrRunInfo
	runPrograms        []*RunProgramStmt
	decimalMD5         []*DecimalMD5Lower32BitsStmt
	splitCodegens      []*SplitCodegenStmt
	baseCodegens       []*BaseCodegenStmt
	runPython          []*RunPythonStmt
	fromSandboxes      []*FromSandboxStmt
	checkConfigHeaders []STR
	cythonCpp          []*CythonStmt

	cythonNumpyBeforeInclude bool
	swigC                    []SwigSrc
	bisonGenExt              STR
	grpc                     bool
	yaConfJSON               []STR
	allPySrcs                []*UnknownStmt

	archives []ArchiveEntry

	archiveAsm []ArchiveAsmEntry

	lj21 *Lj21Archive

	copyFiles []CopyFileEntry

	copyFileAutoOutputs map[STR]CopyFileEntry
	flatSrcs            map[STR]struct{}
	// srcMeta records, per source, its declaring macro's StatementPriority (SRCS=4,
	// SRC/JOIN/codegen=2) and a module-global declaration sequence. AR member order is
	// (prio, seq): SRC/JOIN/codegen ahead of plain SRCS, then by seq.
	srcMeta   map[STR]SrcMeta
	declSeq   int
	resources []ResourceEntry

	// bundles are the module's BUNDLE(...) groups; each emits a BN node renaming the
	// bundled module's primary output into $(B)/<mod>/<name>.
	bundles []BundleEntry

	pyMain *STR

	// noStrip mirrors ENABLE(NO_STRIP), masking STRIP_FLAG.
	noStrip bool

	// programPairedLib marks the KindLib half of a PY3_PROGRAM multimodule (PY3_BIN
	// PROGRAM-side emits PY_MAIN; PY3_BIN_LIB LIBRARY-side emits pysrc + RESOURCE_FILES);
	// this flag emits the PY3_BIN_LIB-tagged resource objcopy from the LIBRARY twin.
	programPairedLib bool

	noCheckImports []STR

	noCheckImportsDisabled bool

	pyRegister []STR

	pyRegisterExplicit []bool

	simdSrcs []SimdSrc

	ragel6Flags []ARG
	bisonFlags  []ARG
	conflictMod *ModuleStmt

	// resourceDeclStmts are the RESOURCES_LIBRARY's DECLARE_EXTERNAL_RESOURCE /
	// _HOST_RESOURCES_BUNDLE[_BY_JSON] calls in declaration order; resolved at gen time.
	resourceDeclStmts []*DeclareResourceStmt

	// primaryOutput is PREBUILT_PROGRAM's PRIMARY_OUTPUT: the fetched binary path
	// copied to the module's program output.
	primaryOutput string

	// inducedDeps are the module's INDUCED_DEPS, bucketed by consumer type (cpp/h/h+cpp);
	// resolveInducedDeps reads one bucket per generated output's kind.
	inducedDeps ParsedIncludeSet

	setVars map[STR]STR

	// tc carries the module's tool-invocation paths, derived in genModule from the
	// resource-global closure rather than ambient platform flags.
	tc ModuleToolchain
}

// perSrcCFlagsFor / flatSrc gate the sparse per-source maps on len, so modules with
// none skip the probe.
func (d *ModuleData) perSrcCFlagsFor(src STR) *[]ARG {
	if len(d.perSrcCFlags) == 0 {
		return nil
	}

	if v, ok := d.perSrcCFlags[src]; ok {
		return &v
	}

	return nil
}

func (d *ModuleData) flatSrc(src STR) bool {
	if len(d.flatSrcs) == 0 {
		return false
	}

	_, ok := d.flatSrcs[src]

	return ok
}

// StatementPriority values: statements run in (priority, name) order, lower first.
const (
	stmtPrioDefault = 2 // SRC, SRC_C_*, JOIN_SRCS, RUN_PROGRAM, codegen macros…
	stmtPrioSrcs    = 4 // SRCS, PY_SRCS
)

// SrcMeta carries a source's AR-ordering key: StatementPriority, a module-global
// declaration sequence, and whether the input is an in-module generated file. Generated
// objects archive after direct ones; within each group the order is (Prio, Seq).
type SrcMeta struct {
	Prio      int
	Seq       int
	Generated bool
	// SecondLevel marks a generated source produced from another in-module generated
	// source (a deeper round); it archives after every first-level generated member.
	SecondLevel bool
}

// sortKey packs the AR-ordering key into one comparable uint64 (high→low: generation
// round, StatementPriority, declaration sequence).
func (m SrcMeta) sortKey() uint64 {
	var round uint64

	if m.Generated {
		round = 1
	}

	if m.SecondLevel {
		round = 2
	}

	return round<<60 | uint64(m.Prio)<<32 | uint64(uint32(m.Seq))
}

// nextDeclSeq returns the next module-global declaration sequence number, bumped once
// per source/statement, so it composes across INCLUDEs where a per-file line cannot.
func (d *ModuleData) nextDeclSeq() int {
	d.declSeq++

	return d.declSeq
}

func (d *ModuleData) setSrcMeta(src STR, prio, seq int) {
	if d.srcMeta == nil {
		d.srcMeta = map[STR]SrcMeta{}
	}

	d.srcMeta[src] = SrcMeta{Prio: prio, Seq: seq}
}

// srcMetaOf returns a source's recorded (prio, seq); sources without an entry default
// to the macro priority (2), seq 0.
func (d *ModuleData) srcMetaOf(src STR) SrcMeta {
	if m, ok := d.srcMeta[src]; ok {
		return m
	}

	return SrcMeta{Prio: stmtPrioDefault}
}

func muslCFlags(on bool) []ARG {
	if on {
		return []ARG{argDMusl}
	}

	return nil
}

// BundleEntry is one BUNDLE group: the bundled module Target, the collected output
// Name, and the optional Suffix selecting a secondary module output.
type BundleEntry struct {
	Target string
	Name   string
	Suffix string
}

type ResourceEntry struct {
	Path string
	Key  string
	// EndsBatch is true on the LAST entry of one RESOURCE / RESOURCE_FILES statement,
	// flushing the objcopy batch: each statement maps to its own objcopy_<hash>.o.
	EndsBatch bool
}

type PySrcGroup struct {
	Srcs      []STR
	TopLevel  bool
	Namespace *STR
}

// SrcFlatEntry is a SRC(file flags…) whose file is also in SRCS — an extra FLAT object
// with its own flags.
type SrcFlatEntry struct {
	Src   STR
	Flags []ARG
	Seq   int
}

type ArchiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
	// Keys is the ordered ARCHIVE_BY_KEYS key list. Non-nil selects the keyed command
	// shape (key list via `-k`); nil keeps the plain ARCHIVE form.
	Keys []string
	// PropagateSourceMembers registers each direct source member as a closure leaf of
	// the archive output, so a unit #including the generated header receives the archived
	// sources in its input closure. (Generated members propagate via SourceInputs anyway.)
	PropagateSourceMembers bool
}

// ArchiveAsmEntry holds an ARCHIVE_ASM(...) call: unlike ARCHIVE it emits a
// `<NAME>.rodata` resource re-fed as a generated source through the .rodata yasm pipeline.
type ArchiveAsmEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

// Lj21Archive holds the ordered .lua names from a LJ_21_ARCHIVE call; the emit phase
// compiles each to a .raw and wires the LuaScripts.inc/LuaSources.inc archives.
type Lj21Archive struct {
	Luas []string
}

type CopyFileEntry struct {
	Src         string
	Dst         string
	Auto        bool
	WithContext bool
	// Text marks COPY_FILE(TEXT) — a textual substitution copy of a shared codegen
	// template, whose includes must resolve in each consumer's own context.
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
	Args           []STR
	INFiles        []STR
	OUTFiles       []STR
	OUTNoAutoFiles []STR
	CWD            *STR
	OutputIncludes []STR
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

	// COPY_FILE(TEXT src dst) substitutes src into dst — consumers of dst must depend on
	// src and its #include closure. The plumbing matches COPY(WITH_CONTEXT), so route through it.
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

func sourceInputVFS(fs FS, modulePath string, path string) *VFS {
	if vfs := moduleRootedVFS(modulePath, path); vfs != nil {
		return vfs
	}

	clean := filepath.ToSlash(filepath.Clean(path))

	if clean == "." || clean == "" {
		return vfsPtr(source(modulePath))
	}

	if clean == modulePath || strings.HasPrefix(clean, modulePath+"/") {
		return vfsPtr(source(clean))
	}

	if fs != nil {
		moduleRel := filepath.ToSlash(filepath.Clean(modulePath + "/" + clean))

		if fs.isFile(dirKey(modulePath), clean) {
			return vfsPtr(source(moduleRel))
		}

		if fs.isFile(srcRootVFS, clean) {
			return vfsPtr(source(clean))
		}
	}

	return nil
}

func copyFileInputVFS(fs FS, modulePath string, src string) VFS {
	if vfs := sourceInputVFS(fs, modulePath, src); vfs != nil {
		return *vfs
	}

	return source(filepath.ToSlash(filepath.Clean(modulePath + "/" + src)))
}

func moduleRootedVFS(modulePath string, path string) *VFS {
	if vfsHasPrefix(path) {
		return vfsPtr(intern(path))
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}/"):
		return vfsPtr(source(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_ROOT}/")))))
	case strings.HasPrefix(path, "${CURDIR}/"):
		return vfsPtr(source(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${CURDIR}/")))))
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}/"):
		return vfsPtr(build(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}/")))))
	case strings.HasPrefix(path, "${BINDIR}/"):
		return vfsPtr(build(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${BINDIR}/")))))
	default:
		return nil
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs := moduleRootedVFS(modulePath, dst); vfs != nil {
		return *vfs
	}

	return build(filepath.ToSlash(filepath.Clean(modulePath + "/" + dst)))
}

// resourceOutputVFS canonicalizes a RESOURCE/RESOURCE_FILES file argument to its
// build-output VFS. Unlike copyFileOutputVFS, a RESOURCE path already rooted at the
// module dir is used verbatim instead of doubled; a module-relative path resolves under
// the module dir.
func resourceOutputVFS(modulePath string, path string) VFS {
	if vfs := moduleRootedVFS(modulePath, path); vfs != nil {
		return *vfs
	}

	clean := filepath.ToSlash(filepath.Clean(path))

	if clean == modulePath || strings.HasPrefix(clean, modulePath+"/") {
		return build(clean)
	}

	return build(filepath.ToSlash(filepath.Clean(modulePath + "/" + clean)))
}

func copyFileIncludeTarget(modulePath string, target string) string {
	if vfsHasPrefix(target) {
		return intern(target).rel()
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

// prioVFS is a local ADDINCL dir tagged with its contributing statement's priority.
// The module's -I list is the addInclP entries stable-sorted by prio, with the stable
// sort supplying the declaration-order tiebreak.
type prioVFS struct {
	prio int
	vfs  VFS
}

const (
	// prioAddIncl: explicit ADDINCL(...), PEERDIR ADDINCL, and generated-header dirs.
	prioAddIncl = 0
	// prioAddInclSelf: ADDINCLSELF is a multi macro, so its ${MODDIR} dir sorts after
	// the non-multi ADDINCL(...) dirs.
	prioAddInclSelf = 1
)

// addLocalIncl records one local (-I) dir at the given statement priority.
func (d *ModuleData) addLocalIncl(prio int, v VFS) {
	d.addInclP = append(d.addInclP, prioVFS{prio: prio, vfs: v})
}

// materializeAddIncl flattens the priority-tagged local ADDINCL contributions into
// d.addIncl in (prio, declaration) order.
func (d *ModuleData) materializeAddIncl() {
	sort.SliceStable(d.addInclP, func(i, j int) bool {
		return d.addInclP[i].prio < d.addInclP[j].prio
	})

	for _, p := range d.addInclP {
		d.addIncl = append(d.addIncl, p.vfs)
	}

	d.addInclP = nil
}

func collectModule(pm *IncludeParserManager, dd *DeDuper, modulePath string, kind ModuleKind, stmts []Stmt, env Environment, onWarn func(Warn)) *ModuleData {
	fs := pm.fs

	env.setString(envMODDIR, modulePath)
	env.setString(envCURDIR, "$(S)/"+modulePath)
	env.setString(envBINDIR, "$(B)/"+modulePath)

	d := &ModuleData{
		pythonSQLite3:           true,
		bisonGenExt:             strCpp,
		needGoogleProtoPeerdirs: true,
	}

	collectStmts(fs, modulePath, kind, stmts, env, d)

	// Flatten the priority-tagged local ADDINCL contributions into d.addIncl before the
	// post-collect ADDINCL appends below.
	d.materializeAddIncl()

	// Seed the SRCDIR search path with the module's own dir at index 0, so the list is
	// never empty and consumers need no "is there a SRCDIR?" special case.
	d.srcDirs = append([]VFS{dirKey(modulePath)}, d.srcDirs...)

	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	// CF-generated include dirs join UserGlobal in the same deferred step, after all
	// explicit ADDINCL statements.
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = nil
	d.cfAddInclGlobal = nil
	filterInvalidAddIncl(fs, dd, d, modulePath, onWarn)

	// The PY3_PROGRAM BIN half emits PY_MAIN; clear it on the paired LIB half to avoid a
	// duplicate. A standalone PY3_LIBRARY with explicit PY_SRCS(MAIN …) keeps its PY_MAIN.
	if kind == KindLib && d.programPairedLib {
		d.pyMain = nil
	}

	// Keep pySrcs on the PROGRAM half: clearing it suppressed emitResourceObjcopy (its
	// only trigger was len(d.pySrcs)>0), so the PROGRAM's LD never threaded its
	// objcopy_<hash>.o into the link command.
	d.muslEnabled = env.bool(envMUSL)
	// USE_ARCADIA_LIBM adds the implicit libm PEERDIR on every non-Emscripten unit;
	// captured here so the default-peer machinery reads the effective env.
	d.useArcadiaLibm = env.bool(envUSE_ARCADIA_LIBM) && !env.bool(envOS_EMSCRIPTEN)
	// NO_STRIP and BUILD_TYPE-driven STRIP_FLAG suppression both clear -Wl,--strip-all;
	// track the effective value so the LD emitter honours it without re-reading env.
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

	d.addIncl = dd.dedupVFS(d.addIncl, nil)
	d.addInclGlobal = dd.dedupVFS(d.addInclGlobal, nil)

	for _, a := range d.addIncl {
		pm.indexAddincl(a)
	}

	for _, a := range d.addInclGlobal {
		pm.indexAddincl(a)
	}

	hasEv := false
	hasProto := false
	hasSc := false
	hasCfgProto := false

	// Pure id-space triage; .fbs detection rides the same pass.
	for _, src := range d.srcs {
		switch srcExtClassOf(src) {
		case srcExtEv:
			hasEv = true
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

	if hasEv {
		d.peerdirs = append(d.peerdirs, strLibraryCppEventlog, strContribLibsProtobuf)
	}

	if hasSc {
		// _SRC("sc").PEERDIR — the runtime the generated .sc.h includes.
		d.peerdirs = append(d.peerdirs, strLibraryCppDomscheme)
	}

	if hasCfgProto {
		// _SRC("cfgproto") induces the config-proto plugin runtime/codegen peers the
		// generated .cfgproto.pb.* needs, regardless of module kind.
		d.peerdirs = append(d.peerdirs, strLibraryCppProtoConfigCodegen, strLibraryCppProtoConfigProtos, strContribLibsProtobuf)
	}

	// Every C++ proto compile peers contrib/libs/protobuf, so any module compiling a
	// .proto to C++ inherits protobuf's GLOBAL ADDINCL band (protobuf/src + abseil),
	// which must reach the module's C++ sources and propagate downstream. The .ev /
	// .cfgproto arms add protobuf via their own PEERDIRs, so this excludes hasEv.
	if hasProto && !hasEv {
		isProtoLibrary := d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary

		// A python-only PROTO_LIBRARY (PY3_PROTO) runs no C++ compile, so protobuf is not
		// peered; a plain LIBRARY with an inline .proto peers it unconditionally.
		if !isProtoLibrary || !env.bool(envPY3_PROTO) {
			d.peerdirs = append(d.peerdirs, strContribLibsProtobuf)
		}

		if isProtoLibrary && !d.optimizePyProtosSet {
			d.optimizePyProtos = true
		}
	}

	// The proto plugin DEPS lead the peer order whenever the module compiles a .proto to
	// C++; protoCmdPeers() returns empty (no-op) for an inline proto without grpc/plugins.
	if d.moduleStmt != nil && (d.moduleStmt.Name == tokProtoLibrary || hasProto) {
		d.protoCmdPeers = protoCmdPeers(d)
	}

	if len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 || len(d.pyRegister) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	return d
}

func appendGlobalSrcEvent(d *ModuleData, src STR) {
	d.globalSrcs = append(d.globalSrcs, src)
}

func appendGlobalSrcGroup(d *ModuleData, srcs []STR) {
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

	d.peerdirs = append(d.peerdirs, internStr(resourcePeer))
}

func filterInvalidAddIncl(fs FS, dd *DeDuper, d *ModuleData, modulePath string, onWarn func(Warn)) {
	d.addIncl = filterOwnAddIncl(fs, d.addIncl, modulePath, onWarn)
	d.addInclGlobal = filterOwnAddIncl(fs, d.addInclGlobal, modulePath, onWarn)
	d.cythonAddIncl = filterOwnAddIncl(fs, d.cythonAddIncl, modulePath, onWarn)
	d.asmAddIncl = filterOwnAddIncl(fs, d.asmAddIncl, modulePath, onWarn)

	// Rebuild addInclUserGlobal in declaration order, keeping only paths that survived
	// the addInclGlobal filter or are in addInclOneLevel.
	if len(d.addInclUserGlobal) > 0 {
		dd.reset()

		for _, p := range d.addInclGlobal {
			dd.add(p)
		}

		for _, p := range d.addInclOneLevel {
			dd.add(p)
		}

		out := d.addInclUserGlobal[:0]

		for _, p := range d.addInclUserGlobal {
			if dd.has(p) {
				out = append(out, p)
			}
		}

		d.addInclUserGlobal = out
	}
}

// filterOwnAddIncl drops a module's own ADDINCL dirs that point at a non-existent
// source directory, reporting each via onWarn (fatal, or warn under --keep-going).
// Mirrors ymake AddIncdir(checkDir=true). Deduplication of repeated reports is the
// warning sink's concern, not this emitter's.
func filterOwnAddIncl(fs FS, paths []VFS, modulePath string, onWarn func(Warn)) []VFS {
	out := paths
	copied := false

	for i, path := range paths {
		if shouldCheckSourceDir(path) && !fs.isDir(path, "") {
			onWarn(Warn{
				Kind:    WarnMissingAddincl,
				Message: fmt.Sprintf("%s: ADDINCL to non existent source directory %s", modulePath, path.rel()),
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

	if path.rel() == "" {
		return false
	}

	if strings.Contains(path.rel(), "$") {
		return false
	}

	return true
}

func flagsContain(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}

	return false
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

	d.moduleScopeCFlags = append(d.moduleScopeCFlags, argDusePython3)

	d.addInclGlobal = append(d.addInclGlobal, pythonIncludeDir)
	d.addInclUserGlobal = append(d.addInclUserGlobal, pythonIncludeDir)
	d.addIncl = append(d.addIncl, pythonIncludeDir)
}

// applyArchiveAddIncl reproduces ARCHIVE's ${addincl;noauto;output:NAME} side effect:
// the output's build directory enters the module's local/user-global/global include
// buckets at Global scope, reaching own compiles and PEERDIR consumers.
func applyArchiveAddIncl(modulePath string, d *ModuleData) {
	for _, a := range d.archives {
		include := build(generatedIncludeDir(modulePath, a.Name))
		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	}

	// LJ_21_ARCHIVE's addincl outputs land flat in the module build dir; its archive
	// entries are synthesized later, so drive the Global-scope addincl from d.lj21 here.
	if d.lj21 != nil {
		include := build(modulePath)
		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	}
}

// applyCythonHeaderAddIncl reproduces the _BUILDWITH_CYTHON_*_H / _API_H addincl side
// effect: the generated .h / _api.h output's build dir enters the module's include dirs
// at Global scope, so the cython source and generated .c both resolve the bare header.
func applyCythonHeaderAddIncl(modulePath string, d *ModuleData) {
	for _, stmt := range d.cythonCpp {
		if !stmt.Header {
			continue
		}

		dir := build(pathDir(modulePath + "/" + cythonNoExt(stmt.Src)))

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

func collectStmts(fs FS, modulePath string, kind ModuleKind, stmts []Stmt, env Environment, d *ModuleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			if v.Name == tokPy3Program && kind == KindBin {
				d.peerdirs = append([]STR{internStr(modulePath)}, d.peerdirs...)
			}

			if v.Name == tokUnittestFor {
				const unittestMainPeer = "library/cpp/testing/unittest_main"

				d.peerdirs = append(d.peerdirs, internStr(unittestMainPeer))

				if len(v.Args) > 0 {
					d.peerdirs = append(d.peerdirs, internStr(path.Clean(v.Args[0].string())))
				}
			}

			if isYqlUdfStaticModule(v.Name) {
				d.peerdirs = append(d.peerdirs, STRS(yqlUdfImplicitPeers()...)...)
			}

			d.moduleStmt = moduleStmtForKind(v, kind)

			// Bind MODULE_LANG for IF evaluation of the autoincluded linters.make.inc:
			// the `IF (MODULE_LANG == CPP)` gate around CLANG_WARNINGS only fires once
			// the module language is known.
			moduleLang := sbomComponentLang(d.moduleStmt.Name)

			// A PROTO_LIBRARY multimodule's _CPP_PROTO submodule is MODULE_LANG==CPP, its
			// _PY3_PROTO is PY3. genModule selects the submodule by binding PY3_PROTO, so
			// the language gate must follow that selection (else the py side's aux C++
			// wrongly inherits CPP CLANG_WARNINGS).
			if d.moduleStmt.Name == tokProtoLibrary && env.bool(envPY3_PROTO) {
				moduleLang = moduleLangTokenPy3
			}

			env.setString(envMODULE_LANG, moduleLang)

			if v.Name == tokPy3Program && kind == KindLib {
				d.programPairedLib = true
			}
		case *SrcsStmt:

			routeAllToGlobal := d.moduleStmt != nil && isYqlUdfStaticModule(d.moduleStmt.Name)
			globalNext := false
			globalSrcs := make([]STR, 0, len(v.Sources))

			for _, srcTok := range expandStmtTokensSTR(v.Sources, env) {
				if srcTok == kwGLOBAL {
					globalNext = true

					continue
				}

				// An unresolved ${VAR} stays literal; the source consumer ignores it,
				// so mirror that (like the PEERDIR arm below).
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
					// _SRC for flex adds the old-flex tool dir to the module's local
					// include dirs (User scope) so the generated lexer's `<FlexLexer.h>`
					// resolves, in SRCS declaration order so it interleaves with a
					// sibling .y's generated-header dir.
					d.addLocalIncl(prioAddIncl, source(argContribToolsFlexOld.string()))
				}
			}

			if routeAllToGlobal {
				appendGlobalSrcGroup(d, globalSrcs)
			}
		case *PeerdirStmt:

			addInclNext := false

			for _, pTok := range expandStmtTokensSTR(v.Paths, env) {
				if pTok == kwADDINCL {
					addInclNext = true

					continue
				}

				if pTok == kwGLOBAL {
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
				// $RAGEL6_FLAGS reaches the ragel command as one arg per whitespace
				// token, not one quoted blob.
				d.ragel6Flags = internArgs(strings.Fields(value))
			}
		case *EndStmt:

		case *JoinSrcsStmt:
			expanded := *v
			expanded.Sources = expandStmtTokensSTR(v.Sources, env)
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

			d.cFlagsGlobal = append(d.cFlagsGlobal, internArgsFromSTR(expandStmtTokensSTR(v.GlobalFlags, env))...)
			d.cFlags = append(d.cFlags, internArgsFromSTR(expandStmtTokensSTR(v.OwnFlags, env))...)
		case *CXXFlagsStmt:

			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, internArgsFromSTR(expandStmtTokensSTR(v.GlobalFlags, env))...)
			d.cxxFlags = append(d.cxxFlags, internArgsFromSTR(expandStmtTokensSTR(v.OwnFlags, env))...)
		case *CONLYFlagsStmt:

			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, internArgsFromSTR(expandStmtTokensSTR(v.GlobalFlags, env))...)
			d.cOnlyFlags = append(d.cOnlyFlags, internArgsFromSTR(expandStmtTokensSTR(v.OwnFlags, env))...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, internArgsFromSTR(expandStmtTokensSTR(v.Flags, env))...)
		case *SrcDirStmt:
			// SRCDIR is cumulative: each expanded arg becomes a directory VFS appended to
			// the search path.
			for _, dirTok := range expandStmtTokensSTR(v.Dirs, env) {
				dir := dirTok.string()
				d.srcDirs = append(d.srcDirs, dirKey(dir))
			}
		case *GlobalSrcsStmt:
			appendGlobalSrcGroup(d, expandStmtTokensSTR(v.Sources, env))
		case *GenerateEnumSerializationStmt:
			expandedEN := *v
			expandedEN.DeclSeq = d.nextDeclSeq()
			d.enumSrcs = append(d.enumSrcs, &expandedEN)
			// GENERATE_ENUM_SERIALIZATION expands inline to a PEERDIR on the
			// enum-serialization runtime at the macro's textual position, before any later
			// explicit PEERDIR block (else its LD link-line slot shifts). Deduped later.
			const enumSerPeer = "tools/enum_parser/enum_serialization_runtime"

			if modulePath != enumSerPeer {
				d.peerdirs = append(d.peerdirs, internStr(enumSerPeer))
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
			d.createBuildInfoFor = strPtr(internStr(v.OutputHeader))
		case *RunAntlr4CppStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, Antlr4GrammarInfo{
				IsSplit:        false,
				Grammar:        expandStmtToken(v.Grammar.string(), env),
				Options:        strStrings(expandStmtTokensSTR(v.Options, env)),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: strStrings(expandStmtTokensSTR(v.OutputIncludes, env)),
			})
		case *RunAntlr4CppSplitStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, Antlr4GrammarInfo{
				IsSplit:        true,
				Lexer:          expandStmtToken(v.Lexer.string(), env),
				Parser:         expandStmtToken(v.Parser.string(), env),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: strStrings(expandStmtTokensSTR(v.OutputIncludes, env)),
			})
		case *RunAntlrStmt:
			expanded := AntlrRunInfo{
				Macro:          v.Macro,
				Args:           expandStmtTokensSTR(v.Args, env),
				INFiles:        expandStmtTokensSTR(v.INFiles, env),
				OUTFiles:       expandStmtTokensSTR(v.OUTFiles, env),
				OUTNoAutoFiles: expandStmtTokensSTR(v.OUTNoAutoFiles, env),
				OutputIncludes: expandStmtTokensSTR(v.OutputIncludes, env),
			}

			if v.CWD != nil {
				cwd := expandStmtTokenSTR(*v.CWD, env)
				expanded.CWD = &cwd
			}

			d.antlrRuns = append(d.antlrRuns, expanded)
		case *RunProgramStmt:
			expanded := *v
			expanded.ToolPath = expandStmtTokenSTR(v.ToolPath, env)
			expanded.Args = expandStmtTokensSTR(v.Args, env)
			expanded.INFiles = expandStmtTokensSTR(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokensSTR(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokensSTR(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokensSTR(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokensSTR(v.OutputIncludes, env)
			expanded.ToolPaths = expandStmtTokensSTR(v.ToolPaths, env)

			if v.StdoutFile != nil {
				stdout := expandStmtTokenSTR(*v.StdoutFile, env)
				expanded.StdoutFile = &stdout
			}

			if v.CWD != nil {
				cwd := expandStmtTokenSTR(*v.CWD, env)
				expanded.CWD = &cwd
			}

			expanded.DeclSeq = d.nextDeclSeq()
			d.runPrograms = append(d.runPrograms, &expanded)
		case *SplitCodegenStmt:
			expanded := *v
			expanded.ToolPath = expandStmtTokenSTR(v.ToolPath, env)
			expanded.Prefix = expandStmtTokenSTR(v.Prefix, env)
			expanded.Opts = expandStmtTokensSTR(v.Opts, env)
			expanded.OutputIncludes = expandStmtTokensSTR(v.OutputIncludes, env)

			d.splitCodegens = append(d.splitCodegens, &expanded)
		case *BaseCodegenStmt:
			expanded := *v
			expanded.ToolPath = expandStmtTokenSTR(v.ToolPath, env)
			expanded.Prefix = expandStmtTokenSTR(v.Prefix, env)
			expanded.Opts = expandStmtTokensSTR(v.Opts, env)
			expanded.OutputIncludes = expandStmtTokensSTR(v.OutputIncludes, env)

			d.baseCodegens = append(d.baseCodegens, &expanded)

			// STRUCT_CODEGEN's implicit PEERDIRs enter at the macro's textual position;
			// plain BASE_CODEGEN carries none.
			for _, p := range v.Peerdirs {
				d.peerdirs = append(d.peerdirs, p)
			}
		case *RunPythonStmt:
			expanded := *v
			expanded.ScriptPath = expandStmtTokenSTR(v.ScriptPath, env)
			expanded.Args = expandStmtTokensSTR(v.Args, env)
			expanded.INFiles = expandStmtTokensSTR(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokensSTR(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokensSTR(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokensSTR(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokensSTR(v.OutputIncludes, env)

			if v.StdoutFile != nil {
				stdout := expandStmtTokenSTR(*v.StdoutFile, env)
				expanded.StdoutFile = &stdout
			}

			if v.CWD != nil {
				cwd := expandStmtTokenSTR(*v.CWD, env)
				expanded.CWD = &cwd
			}

			d.runPython = append(d.runPython, &expanded)
		case *FromSandboxStmt:
			expanded := *v
			expanded.ResourceId = expandStmtTokenSTR(v.ResourceId, env)
			expanded.OUTFiles = expandStmtTokensSTR(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokensSTR(v.OUTNoAutoFiles, env)
			expanded.OutputIncludes = expandStmtTokensSTR(v.OutputIncludes, env)
			expanded.Renames = expandStmtTokensSTR(v.Renames, env)
			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *ResourceStmt:
			ensureResourcePeer(modulePath, d)

			for i, pair := range v.Pairs {
				// RESOURCE() pairs are stored raw (${BINDIR}/... unexpanded) so the
				// objcopy_<hash> matches; pre-expanding drifts the hash.
				d.resources = append(d.resources, ResourceEntry{
					Path:      pair.Path,
					Key:       pair.Key,
					EndsBatch: i == len(v.Pairs)-1,
				})
			}
		case *ResourceFilesStmt:
			ensureResourcePeer(modulePath, d)

			expanded := expandResourceFiles(strStrings(expandStmtTokensSTR(v.Args, env)))

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
			// Expand args: DECLARE_EXTERNAL_RESOURCE(NAME ${SANDBOX_RESOURCE_URI}) carries
			// the SET_RESOURCE_URI_FROM_JSON-bound var; literal args expand to a no-op.
			expanded := *v
			expanded.Args = expandStmtTokensSTR(v.Args, env)
			d.resourceDeclStmts = append(d.resourceDeclStmts, &expanded)
		case *IfStmt:
			taken := v.Then

			if !evalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(fs, modulePath, kind, taken, env, d)
		case *UnknownStmt:
			// Expand args: a ${VAR} holding a SET-list must substitute and split into
			// individual tokens before the macro handler reads them.
			expanded := *v
			expanded.Args = expandStmtTokensSTR(v.Args, env)
			applyUnknownStmt(fs, modulePath, &expanded, d, env)
		default:
			throwFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

func moduleStmtForKind(stmt *ModuleStmt, kind ModuleKind) *ModuleStmt {
	if stmt.Name == tokPy3Program && kind == KindLib {
		out := *stmt
		out.Name = tokPy3Library

		return &out
	}

	return stmt
}

// generatedIncludeDir resolves the build-root include dir for a generated output `dst`
// (the parent of $(B)/<mod>/<dst>, falling back to the module dir when dst is flat).
func generatedIncludeDir(modulePath, dst string) string {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.rel())))

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

func applyUnknownStmt(fs FS, modulePath string, v *UnknownStmt, d *ModuleData, env Environment) {
	// recordHandledMacro fires only when a typed case handles the macro; the default
	// branch flips `handled = false` to suppress noise from unmodelled macros.
	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name, v.Args)
		}
	}()

	switch v.Name {
	case tokAddInclSelf:
		// ADDINCLSELF([FOR preset]) adds -I<own source dir>; a FOR preset routes it to
		// that preset's bucket (cython/asm).
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
		// SET_RESOURCE_URI_FROM_JSON(VarName file.json) binds VarName to the
		// by_platform[<current platform>].uri entry of file.json, gating PREBUILT_PROGRAM
		// (IF(VarName != "")). An absent entry leaves VarName unset → from-source.
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
		// NO_PLATFORM_RESOURCES() == ENABLE(NOPLATFORM_RESOURCES).
		env.setBool(envNoplatformResources, true)
	case tokPrimaryOutput:
		// PRIMARY_OUTPUT(path): the module's main output. The arg holds
		// ${<NAME>_RESOURCE_GLOBAL}/..., binding only after DECLARE_EXTERNAL_RESOURCE, so
		// genPrebuiltProgram re-collects with the global bound.
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
		// STYLE_RUFF is an optional-kwarg linter macro; we don't model the python lint
		// pipeline (no-op), so just walk the args to acknowledge each legal kwarg.
		for i := 0; i < len(v.Args); i++ {
			switch v.Args[i].string() {
			case "CONFIG_TYPE":
				i++
			case "CHECK_FORMAT":
			case "RUN_IN_SOURCE_ROOT":
			}
		}
	case tokLlvmBc:
		// LLVM_BC args split into keywords plus a free-arg source list driving
		// compile → link → opt and then llvm-llc or resource embed. We parse the
		// keywords and stash the result; node emission is a follow-up.
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
		// BUNDLE(<Dir> [SUFFIX s] [NAME n]…): name = basename(target)+suffix when NAME is
		// absent. We only collect here; node emission needs the emitter.
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

	case tokMavenGroupId:

	case tokLicense:
		// LICENSE() presence — not a contrib/ path — gates the _GEN_SBOM_COMPONENT DX node.
		d.hasLicense = true
	case tokVersion:
		// SET(MODVER ${Flags}); rendered as join(".", MODVER). Expand toolchain refs.
		d.modver = strings.Join(strStrings(expandStmtTokensSTR(v.Args, env)), ".")
	case tokToolchain:
		// TOOLCHAIN(Name) attaches _GEN_SBOM_TOOLCHAIN_COMPONENT (a toolchain.component.sbom).
		if len(v.Args) == 1 {
			d.toolchainName = v.Args[0].string()
		}
	case tokCheckConfigH:
		if len(v.Args) != 1 {
			throwFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, v.Args[0])
	case tokDecimalMd5Lower32Bits:
		// DECIMAL_MD5_LOWER_32_BITS(File, FUNCNAME="", Opts...) hashes Opts and emits File
		// as a build-root .cpp.
		if len(v.Args) == 0 {
			throwFmt("gen: %s: DECIMAL_MD5_LOWER_32_BITS expects at least 1 argument (File)", modulePath)
		}

		stmt := &DecimalMD5Lower32BitsStmt{File: v.Args[0].string()}

		rest := v.Args[1:]

		if len(rest) >= 1 && rest[0] == kwFUNCNAME {
			if len(rest) < 2 {
				throwFmt("gen: %s: DECIMAL_MD5_LOWER_32_BITS FUNCNAME requires a value", modulePath)
			}

			stmt.FuncName = rest[1].string()
			rest = rest[2:]
		}

		stmt.Opts = append([]STR(nil), rest...)
		d.decimalMD5 = append(d.decimalMD5, stmt)
	case tokBuildwithCythonCpp:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: strStrings(v.Args[1:]),
		})
		d.cythonNumpyBeforeInclude = true
	case tokBuildwithCythonC:
		if len(v.Args) == 0 {
			throwFmt("BUILDWITH_CYTHON_C expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     v.Args[0].string(),
			Options: strStrings(v.Args[1:]),
			CMode:   true,
		})
		d.cythonNumpyBeforeInclude = true
	case tokBisonGenC:
		d.bisonGenExt = strC
	case tokBisonGenCpp:
		d.bisonGenExt = strCpp
	case tokBisonFlags:
		// BISON_FLAGS(Flags...) appends; the bison command's $BISON_FLAGS = default -v + these.
		for _, a := range v.Args {
			d.bisonFlags = append(d.bisonFlags, internArg(a.string()))
		}
	case tokGrpc:
		d.grpc = true
		d.peerdirs = append(d.peerdirs, strContribLibsGrpc)
	case tokPyNamespace:
		if len(v.Args) != 1 {
			throwFmt("gen: PY_NAMESPACE expects exactly 1 argument, got %d", len(v.Args))
		}

		d.pyNamespace = strPtr(v.Args[0])
	case tokYqlLastAbiVersion:
		if len(v.Args) != 0 {
			throwFmt("YQL_LAST_ABI_VERSION expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags, argDuseCurrentUdfAbiVersion)
	case tokYqlAbiVersion:
		if len(v.Args) != 3 {
			throwFmt("YQL_ABI_VERSION expects exactly 3 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags,
			internArg("-DUDF_ABI_VERSION_MAJOR="+v.Args[0].string()),
			internArg("-DUDF_ABI_VERSION_MINOR="+v.Args[1].string()),
			internArg("-DUDF_ABI_VERSION_PATCH="+v.Args[2].string()),
		)
	case tokProtocFatalWarnings:
		if len(v.Args) != 0 {
			throwFmt("PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.protocFlags = append(d.protocFlags, argFatalWarnings)
	case tokUseCommonGoogleApis:
		// Peer contrib/libs/googleapis-common-protos so the googleapis dir lands first in
		// the resolved peer list, ahead of even the LIBRARY-chain defaults. d.useCommonGoogleAPIs
		// drives the GLOBAL-ADDINCL walk to visit it before language defaults; prepending
		// into d.peerdirs preserves consumer peer-chain propagation.
		d.useCommonGoogleAPIs = true
		const googleapisPeer = "contrib/libs/googleapis-common-protos"
		d.peerdirs = append([]STR{internStr(googleapisPeer)}, d.peerdirs...)
	case tokClangWarnings:
		d.clangWarnings = append(d.clangWarnings, internArgsFromSTR(expandStmtTokensSTR(v.Args, env))...)
	case tokFlatcFlags:
		d.flatcFlags = append(d.flatcFlags, internArgsFromSTR(v.Args)...)
	case tokCopyFile, tokCopyFileWithContext:
		entry := parseCopyFileEntry(strStrings(v.Args), v.Name == tokCopyFileWithContext, v.Line)
		d.copyFiles = append(d.copyFiles, entry)

		if entry.Auto {
			dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
			prefix := modulePath + "/"

			if strings.HasPrefix(dstVFS.rel(), prefix) {
				dstRel := strings.TrimPrefix(dstVFS.rel(), prefix)

				if isSourceEligibleForCopyAuto(dstRel) && !strsContain(d.srcs, dstRel) {
					d.srcs = append(d.srcs, internStr(dstRel))
				}

				if d.copyFileAutoOutputs == nil {
					d.copyFileAutoOutputs = make(map[STR]CopyFileEntry)
				}

				d.copyFileAutoOutputs[internStr(dstRel)] = entry
			}
		}
	case tokCopy:
		for _, entry := range parseCopyEntries(strStrings(v.Args), v.Line) {
			d.copyFiles = append(d.copyFiles, entry)

			if entry.Auto {
				dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
				prefix := modulePath + "/"

				if strings.HasPrefix(dstVFS.rel(), prefix) {
					dstRel := strings.TrimPrefix(dstVFS.rel(), prefix)

					if isSourceEligibleForCopyAuto(dstRel) && !strsContain(d.srcs, dstRel) {
						d.srcs = append(d.srcs, internStr(dstRel))
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
		// EXPORT_YMAPS_PROTO() is PROTO_NAMESPACE(maps/doc/proto), whose `GLOBAL FOR proto
		// $(S)/maps/doc/proto` addincl renders as -I=$(S)/maps/doc/proto in every
		// transitive consumer's protoc command.
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, mapsDocProto)
		// applyProtoNamespace sets the output root and contributes the GLOBAL build-root
		// C++ ADDINCL (-I$(B)/maps/doc/proto); the protoc-only source arm stays above, so
		// no source-root C++ leakage.
		applyProtoNamespace(d, mapsDocProtoNS)
	case tokYmapsSproto:
		// YMAPS_SPROTO(FILES...) dispatches per .proto file; a non-.proto arg is a
		// fail-fast modelling gap.
		for _, argTok := range v.Args {
			if !strings.HasSuffix(argTok.string(), ".proto") {
				throwFmt("gen: %s: YMAPS_SPROTO expects .proto arguments, got %q", modulePath, argTok.string())
			}

			d.ymapsSprotoSrcs = append(d.ymapsSprotoSrcs, argTok)
		}

		// The sproto-header producer declares a command-level .PEERDIR on
		// maps/libs/sproto, so the module peers the target-side library (its non-PIC
		// archive reaches the program link separately from the host sprotoc tool's PIC archive).
		if len(v.Args) > 0 {
			d.peerdirs = append(d.peerdirs, strMapsLibsSproto)
		}
	case tokExcludeTags:
		// EXCLUDE_TAGS drops multimodule submodules from the build. We model only
		// CPP_PROTO submodules (no-op on the graph); record the tag set for parity.
		if d.excludeTags == nil {
			d.excludeTags = make(map[STR]bool)
		}

		for _, argTok := range v.Args {
			arg := argTok.string()

			switch arg {
			case "GO_PROTO", "JAVA_PROTO":
				// known multimodule submodule tags
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
		// ENABLE(X) sets a boolean env var. Args are user-defined flag NAMES (excluded
		// from the service-keyword check); the cases below keep the few with a direct
		// module-data side-effect.
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
			}
		}
	case tokDisable:
		// Counterpart to ENABLE: clears the env var and the few specific module-data flags.
		for _, aTok := range v.Args {
			a := aTok.string()
			env.setBool(internEnv(a), false)

			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
			}

			if a == "NEED_GOOGLE_PROTO_PEERDIRS" {
				d.needGoogleProtoPeerdirs = false
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

		var extras []ARG

		if len(v.Args) > 1 {
			extras = internArgsFromSTR(v.Args[1:])
		}

		// A file already in SRCS: SRCS yields its non-flat object (default flags);
		// this SRC adds a separate FLAT object with its own flags. srcExtraFlat
		// keeps the SRCS occurrence non-flat and unflagged.
		if slices.Contains(d.srcs, filename) {
			d.srcExtraFlat = append(d.srcExtraFlat, SrcFlatEntry{Src: filename, Flags: extras, Seq: d.nextDeclSeq()})

			break
		}

		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[STR]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
		d.setSrcMeta(filename, stmtPrioDefault, d.nextDeclSeq())

		if extras != nil {
			if d.perSrcCFlags == nil {
				d.perSrcCFlags = map[STR][]ARG{}
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
			d.flatSrcs = map[STR]struct{}{}
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
		flags := make([]string, 0, len(variant.CFlags)+len(v.Args)-1)
		flags = append(flags, variant.CFlags...)
		flags = append(flags, strStrings(v.Args[1:])...)

		d.simdSrcs = append(d.simdSrcs, SimdSrc{
			Src:     filename,
			Variant: variant.Suffix,
			CFlags:  flags,
			Seq:     d.nextDeclSeq(),
		})
	case tokLdPlugin:

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case tokArPlugin:

		if len(v.Args) != 1 {
			throwFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}

		d.arPlugin = strPtr(internStr(v.Args[0].string() + ".pyplugin"))
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

		d.exportsScript = strPtr(v.Args[0])
	case tokExtralibs:
		// One EXTRALIBS(...) call is stored as a single space-joined OBJADDE_LIB value,
		// deduped cross-peer by the whole-value string. Model each call as one group ARG;
		// the cmd_args boundary splits it back into tokens.
		libs := make([]string, 0, len(v.Args))

		for _, argTok := range v.Args {
			lib := argTok.string()

			if !strings.HasPrefix(lib, "-") {
				lib = "-l" + lib
			}

			libs = append(libs, lib)
		}

		if len(libs) > 0 {
			d.objAddLibsGlobal = append(d.objAddLibsGlobal, internArg(strings.Join(libs, " ")))
		}
	case tokUsePython3:

		d.peerdirs = append(d.peerdirs,
			strContribLibsPython,
			strLibraryPythonRuntimePy3,
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
		var namespace *STR
		var groupSrcs []string
		cythonStmtStart := len(d.cythonCpp)
		var cythonDirectives []string
		// cythonRegIdx records, in textual order, the d.pyRegister index of each cython
		// statement's implicit registration, so the variant-bucket reorder below can move
		// those entries while leaving interleaved swig registrations anchored.
		var cythonRegIdx []int

		for i := 0; i < len(v.Args); i++ {
			a := v.Args[i]

			switch a {
			case kwTOP_LEVEL:
				topLevel = true
				d.pyTopLevel = true

				continue
			case kwNAMESPACE:
				i++

				if i >= len(v.Args) {
					throwFmt("PY_SRCS NAMESPACE expects a value")
				}

				namespace = strPtr(v.Args[i])
				d.pyNamespace = namespace

				continue
			case kwCYTHONIZE_PY:
				cythonizePy = true

				continue
			case kwCYTHON_CPP:
				cythonPlainCpp = true
				cythonCMode = false
				cythonHeader = false
				cythonApiHeader = false

				continue
			case kwCYTHON_C:
				cythonCMode = true
				cythonPlainCpp = false
				cythonHeader = false
				cythonApiHeader = false

				continue
			case strCythonCppH:
				// C++ mode + companion public header: noext naming, extra .h output.
				cythonCMode = false
				cythonPlainCpp = false
				cythonHeader = true
				cythonApiHeader = false

				continue
			case strCythonCH:
				// C mode + companion public header: noext naming, extra .h output.
				cythonCMode = true
				cythonPlainCpp = false
				cythonHeader = true
				cythonApiHeader = false

				continue
			case strCythonCApiH:
				// C mode + public api header: noext naming, extra .h plus _api.h outputs.
				cythonCMode = true
				cythonPlainCpp = false
				cythonHeader = true
				cythonApiHeader = true

				continue
			case kwCYTHON_DIRECTIVE:
				i++

				if i >= len(v.Args) {
					throwFmt("PY_SRCS CYTHON_DIRECTIVE expects a value")
				}

				cythonDirectives = append(cythonDirectives, "-X", v.Args[i].string())

				continue
			case kwSWIG_C:
				swigCMode = true

				continue
			case kwSWIG_CPP:
				swigCMode = false

				continue
			case kwMAIN:
				mainNext = true

				continue
			}

			src := a.string()
			modNameOverride := ""

			if eq := strings.IndexByte(src, '='); eq >= 0 {
				modNameOverride = src[eq+1:]
				src = src[:eq]
			}

			if strings.HasSuffix(src, ".pyx") {
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
					stmt.Generated = stringPtr(src + ".cpp")
				}

				d.cythonCpp = append(d.cythonCpp, stmt)
				appendPyRegister(d, modName, false)
				cythonRegIdx = append(cythonRegIdx, len(d.pyRegister)-1)
				mainNext = false

				continue
			}

			if cythonizePy && strings.HasSuffix(src, ".py") {
				modName := modNameOverride

				if modName == "" {
					modName = pythonModuleName(modulePath, src, topLevel, namespace)
				}

				// CYTHONIZE_PY inherits whichever variant the last CYTHON_C/CYTHON_CPP[_H]
				// directive selected (default C++).
				d.cythonCpp = append(d.cythonCpp, &CythonStmt{
					Src:       src,
					CMode:     cythonCMode,
					Header:    cythonHeader,
					ApiHeader: cythonApiHeader,
					// dep=<mod-as-path>.pxd when it resolves.
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

			if strings.HasSuffix(src, ".swg") {
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

			if strings.HasSuffix(src, ".pyi") {
				modName := modNameOverride

				if modName == "" {
					modName = pythonModuleName(modulePath, strings.TrimSuffix(src, ".pyi"), topLevel, namespace)
				}

				dest := "py/" + strings.ReplaceAll(modName, ".", "/") + ".pyi"
				d.pyPyiResources = append(d.pyPyiResources, expandResourceFiles([]string{"DEST", dest, src})...)
				mainNext = false

				continue
			}

			if strings.Contains(a.string(), "=") && !strings.HasSuffix(src, ".py") {
				continue
			}

			d.pySrcs = append(d.pySrcs, internStr(src))
			// A build-root-rooted token keeps its full root-relative path as the module
			// name; a bare token uses the raw token.
			d.pySrcsFullName = append(d.pySrcsFullName, strings.HasPrefix(src, "${ARCADIA_BUILD_ROOT}/") || strings.HasPrefix(src, "${ARCADIA_ROOT}/") || strings.HasPrefix(src, "$B/"))
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

				d.pyMain = strPtr(internStr(modName + ":main"))
				mainNext = false
			} else if d.pyMain == nil && d.moduleStmt != nil &&
				(d.moduleStmt.Name == tokPy3Program || d.moduleStmt.Name == tokPy3ProgramBin) &&
				(src == "__main__.py" || strings.HasSuffix(src, "/__main__.py")) {
				// A PY3 PROGRAM-kind unit auto-sets PY_MAIN for `__main__.py` to the
				// dotted module path (no ":main" suffix — that is only for explicit
				// PY_MAIN). Without this the LD would miss the `--kvs PY_MAIN=...` entry.
				ns := strings.ReplaceAll(modulePath, "/", ".") + "."

				if topLevel {
					ns = ""
				}

				modName := strings.TrimSuffix(src, ".py")
				modName = strings.ReplaceAll(modName, "/", ".")
				d.pyMain = strPtr(internStr(ns + modName))
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
				Srcs:      STRS(groupSrcs...),
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

		d.pyMain = strPtr(internStr(arg))
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
	case tokCppEvlog:
		// CPP_EVLOG() == CPP_PROTO_PLUGIN0(event2cpp … DEPS eventlog) +
		// ENABLE(_BUILD_PROTO_AS_EVLOG). The plugin half is the whole observable behavior:
		// event2cpp becomes a C++ proto plugin on the PB producer (command tokens, tool
		// input, eventlog peer, and its INDUCED_DEPS(h+cpp) via GeneratorRefs).
		// _BUILD_PROTO_AS_EVLOG only drives an ASSERT, so it needs no model.
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
				d.sFlags = append(d.sFlags, internArgsFromSTR(v.Args[1:])...)
			case "_PROTOC_FLAGS":
				d.protocFlags = append(d.protocFlags, internArgsFromSTR(v.Args[1:])...)
			case "RPATH_GLOBAL":
				for _, argTok := range v.Args[1:] {
					arg := argTok.string()
					arg = strings.ReplaceAll(arg, `${"$"}`, "$")
					d.rpathFlagsGlobal = append(d.rpathFlagsGlobal, internArg(arg))
				}
			}

			// Bind the variable with append semantics — SET_APPEND(VAR x) sets VAR to
			// "$VAR x" — so it expands wherever a later macro references ${VAR}.
			name := v.Args[0].string()
			value := strings.Join(strStrings(v.Args[1:]), " ")

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
			// INDUCED_DEPS(h …) -> header consumers; (cpp …) -> TUs; (h+cpp …) -> both.
			toHeader := v.Args[0].string() != "cpp"
			toCpp := v.Args[0].string() != "h"

			for _, pTok := range v.Args[1:] {
				p := pTok.string()
				// A rooted spelling binds directly; a bare rel keeps quoted-include
				// search semantics (delayed to include resolution).
				dir := IncludeDirective{kind: includeQuoted, target: internStr(p)}

				if toHeader {
					d.inducedDeps = appendParsedDirectives(d.inducedDeps, parsedIncludesHeader, dir)
				}

				if toCpp {
					d.inducedDeps = appendParsedDirectives(d.inducedDeps, parsedIncludesCpp, dir)
				}
			}
		}
	default:
		// Acknowledged macro: stash its expanded args under d.unhandledMacros and record
		// it into the audit. Anything outside acknowledgedMacros is a gen bug.
		handled = false

		if !acknowledgedTokSet.has(uint32(v.Name)) {
			throwFmt("gen: macro %q not modelled — implement its upstream semantics (see yatool/build/conf, yatool/build/ymake.core.conf)", v.Name.string())
		}

		if d.unhandledMacros == nil {
			d.unhandledMacros = map[STR][]STR{}
		}

		name := v.Name.str()
		d.unhandledMacros[name] = append(d.unhandledMacros[name], v.Args...)
		recordIgnoredMacro(v.Name)
	}
}

// LlvmBcStmt mirrors the LLVM_BC keyword parse. Sources are the free args; Name is
// mandatory; Suffix overrides OBJ_SUF; Symbols feeds the -internalize-public-api-list
// pass; the booleans gate llvm-llc emission and per-input compile dispatch.
type LlvmBcStmt struct {
	Sources             []string
	Name                string
	Suffix              string
	Symbols             []string
	GenerateMachineCode bool
	NoCompile           bool
	// ClangBCRoot is CLANG_BC_ROOT captured at collection time: a deferred resource-global
	// reference emitLLVMBC expands to the toolchain bin-root for clang++/llvm-link/opt.
	ClangBCRoot string
}

func isLlvmBcKeyword(s string) bool {
	switch s {
	case "NAME", "SUFFIX", "SYMBOLS", "GENERATE_MACHINE_CODE", "NO_COMPILE":
		return true
	}

	return false
}

// cythonVariantBucket maps a PY_SRCS cython statement to its fixed emission-order
// bucket (five lists), so archive members group by bucket rather than textual PY_SRCS
// order. CYTHONIZE_PY .py sources inherit the last directive's bucket (default pyxs_cpp).
func cythonVariantBucket(s *CythonStmt) int {
	switch {
	case s.CMode && !s.Header:
		return 0 // CYTHON_C
	case s.CMode && s.Header && !s.ApiHeader:
		return 1 // CYTHON_C_H
	case s.CMode && s.Header && s.ApiHeader:
		return 2 // CYTHON_C_API_H
	case !s.CMode && !s.Header:
		return 3 // CYTHON_CPP (default)
	default: // !s.CMode && s.Header
		return 4 // CYTHON_CPP_H
	}
}

// reorderCythonVariantBuckets stable-sorts one PY_SRCS call's cython statements
// (d.cythonCpp[start:]) into variant-bucket order and applies the same permutation to
// their paired pyRegister entries (rewritten in place at regIdx), so interleaved swig
// or explicit registrations keep their textual positions.
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
		internArg("-DPyInit_"+shortname+"=PyInit_"+mangled),
		internArg("-Dinit_module_"+shortname+"=init_module_"+mangled),
	)
}

func parseCPPProtoPlugin(v *UnknownStmt) CppProtoPlugin {
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
		plugin.OutputSuffixes = append(plugin.OutputSuffixes, strStrings(v.Args[tail:tail+outputSuffixes])...)
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

// yaffPluginPath is the YaFF protoc plugin PROGRAM.
const yaffPluginPath = "library/cpp/yaff/tools/protoc_plugin"

// yaffSections collects the NAMESPACE scalar and the FILES / EXPERIMENTAL lists shared
// by YAFF and YAFF_SCHEMA; `positional` receives any leading non-keyword arguments.
type yaffSections struct {
	positional   []string
	namespace    string
	files        []string
	experimental []string
}

func parseYAFFSections(v *UnknownStmt) yaffSections {
	var s yaffSections

	section := STR(0) // 0 = positional/leading

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
		case kwNAMESPACE:
			i++

			if i >= len(v.Args) {
				throwFmt("gen: %s NAMESPACE expects a value", v.Name)
			}

			s.namespace = v.Args[i].string()
			section = STR(0)
		case kwFILES:
			section = kwFILES
		case kwEXPERIMENTAL:
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

// yaffExtraOutFlag composes the comma-joined EXTRA_OUT_FLAG: the lead group, then
// `file=` per FILES entry, then `experimental=` per EXPERIMENTAL entry.
func yaffExtraOutFlag(lead string, s yaffSections) string {
	groups := []string{
		lead,
		strings.Join(prefixEach("file=", s.files), ","),
		strings.Join(prefixEach("experimental=", s.experimental), ","),
	}

	return strings.Join(groups, ",")
}

func prefixEach(prefix string, items []string) []string {
	if len(items) == 0 {
		return nil
	}

	out := make([]string, len(items))

	for i, it := range items {
		out[i] = prefix + it
	}

	return out
}

func parseYAFF(v *UnknownStmt) CppProtoPlugin {
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

func parseYAFFSchema(v *UnknownStmt) CppProtoPlugin {
	s := parseYAFFSections(v)

	if len(s.positional) < 1 {
		throwFmt("gen: YAFF_SCHEMA expects SCHEMA_NAME, got %d positional args", len(s.positional))
	}

	schemaName := s.positional[0]

	// NAMESPACE may arrive positionally or via the NAMESPACE keyword.
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

func pythonModuleName(modulePath, src string, topLevel bool, namespace *STR) string {
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

// applyProtoNamespace models PROTO_NAMESPACE (SET(PROTO_NAMESPACE) + PROTO_ADDINCL(GLOBAL)):
// it records the namespace output root and the build-root C++ ADDINCL, which rides the
// peer addincl closure into transitive consumers. The protoc source arm is carried
// separately by the caller via d.protoAddInclGlobal. EXPORT_YMAPS_PROTO reuses this.
func applyProtoNamespace(d *ModuleData, namespace STR) {
	d.protoNamespace = strPtr(namespace)

	// PROTO_NAMESPACE's C++ build-root arm is `ADDINCL(GLOBAL $(B)/<ns>)`
	// unconditionally; the UserGlobal arm lands the dir first in a consumer's -I band.
	protoBuildRoot := build(filepath.ToSlash(filepath.Clean(namespace.string())))
	d.addIncl = append(d.addIncl, protoBuildRoot)
	d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
	d.addInclUserGlobal = append(d.addInclUserGlobal, protoBuildRoot)
}

func applyArchiveStmt(v *UnknownStmt, d *ModuleData) {
	var (
		entry      ArchiveEntry
		seenName   bool
		inNameSlot bool
	)

	// A direct SRCDIR-backed archive member rides into the include closure of a C++ unit
	// #including the generated archive header. Same model as ARCHIVE_BY_KEYS.
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

// applyArchiveByKeysStmt parses ARCHIVE_BY_KEYS(NAME <name> KEYS <keys> [DONTCOMPRESS]
// files...). It differs from ARCHIVE only in the command shape (key list via `-k $KEYS`),
// landing in the same d.archives slot with Keys set.
func applyArchiveByKeysStmt(v *UnknownStmt, d *ModuleData) {
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

// applyArchiveAsmStmt parses ARCHIVE_ASM(NAME <name> [DONTCOMPRESS] files...), landing
// in d.archiveAsm — the emit phase produces a `<NAME>.rodata` resource plus the
// .rodata→asm→object compile.
func applyArchiveAsmStmt(v *UnknownStmt, d *ModuleData) {
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

// applyLj21ArchiveStmt parses LJ_21_ARCHIVE(NAME Name LuaFiles...): the lua list is
// every arg ending in `.lua`. The emit phase compiles each to a .raw.
func applyLj21ArchiveStmt(v *UnknownStmt, d *ModuleData) {
	var luas []string

	for _, aTok := range v.Args {
		a := aTok.string()

		if strings.HasSuffix(a, ".lua") {
			luas = append(luas, a)
		}
	}

	if len(luas) == 0 {
		throwFmt("gen: LJ_21_ARCHIVE has no .lua files (line %d)", v.Line)
	}

	d.lj21 = &Lj21Archive{Luas: luas}
}

func applyAllocatorStmt(v *UnknownStmt, d *ModuleData) {
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
	case tokProgram, tokPy2Program, tokPy3Program, tokPy3ProgramBin, tokUnittestFor:
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
	env := DefaultIfEnv.clone()

	for k, v := range instance.Platform.Flags {
		env.setFromStringID(k, v)
	}

	if env.bool(envOPENSOURCE) || env.string(envOPENSOURCE_PROJECT) == "ymake" || env.string(envOPENSOURCE_PROJECT) == "ya" {
		env.setBool(envYA_OPENSOURCE, true)
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envCATBOOST_OPENSOURCE, true)
		// The open-source contour ships these libs shared; the internal contour leaves
		// them unset, falling through to the static PEERDIR.
		env.setString(env_USE_AIO, "dynamic")
		env.setString(env_USE_ICONV, "dynamic")
		env.setString(env_USE_IDN, "dynamic")
	}

	// USE_PREBUILT_TOOLS defaults to yes (opensource ya.conf sets "no"), gating each
	// tool's `IF (USE_PREBUILT_TOOLS) INCLUDE(ya.make.prebuilt)`.

	switch instance.Platform.ISA {
	case ISAX8664:
		env.setBool(envARCH_X86_64, true)
		env.setBool(envARCH_TYPE_64, true)
	case ISAAArch64:
		env.setBool(envARCH_AARCH64, true)
		env.setBool(envARCH_ARM64, true)
		env.setBool(envARCH_TYPE_64, true)
	}

	// HAVE_MKL gates the BLAS/LAPACK selectors. Bound in two steps: default yes iff a
	// non-sanitized linux/x86_64 contour (a flag binding wins), then forced no under
	// OPENSOURCE (overriding even an explicit HAVE_MKL=yes).
	if !env.hasBindingID(envHAVE_MKL) {
		haveMkl := env.bool(envOS_LINUX) && env.bool(envARCH_X86_64) &&
			env.string(envSANITIZER_TYPE) == ""
		env.setBool(envHAVE_MKL, haveMkl)
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envHAVE_MKL, false)
	}

	// The module-relative path vars; braced and bare forms both resolve through this
	// binding. A pathless instance (IF-evaluation in tests) carries no module dir.
	env.setString(envARCADIA_ROOT, "$(S)")
	env.setString(envARCADIA_BUILD_ROOT, "$(B)")

	if instance.Path != 0 {
		env.setString(envCURDIR, instance.Path.string())
		env.setString(envBINDIR, build(instance.Path.rel()).string())
		env.setString(envMODDIR, instance.Path.rel())
	}

	useRuntime := instance.Platform.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]
	env.setBool(envUSE_ARCADIA_COMPILER_RUNTIME, useRuntime != strNo)
	env.setStringID(envCOMPILER_VERSION, instance.Platform.ClangVerSTR)
	env.setStringID(envBUILD_TYPE, instance.Platform.BuildTypeUpperSTR)

	if (instance.Platform.ISA == ISAX8664 || env.bool(envARCH_I386)) &&
		!env.bool(envDISABLE_INSTRUCTION_SETS) {
		env.setStringID(envSSE41_CFLAGS, strSSE41CFlags)
		env.setStringID(envSSE42_CFLAGS, strSSE42CFlags)
		env.setStringID(envPOPCNT_CFLAGS, strPopcntCFlags)
		env.setStringID(envCX16_FLAGS, strCX16CFlags)
		env.setStringID(envAVX_CFLAGS, strAVXCFlags)
		env.setStringID(envAVX2_CFLAGS, strAVX2CFlags)
		env.setStringID(envAVX512_CFLAGS, strAVX512CFlags)
		env.setStringID(envSSE_CFLAGS, strSSECFlags)
		env.setStringID(envSSE4_CFLAGS, strSSE4CFlags)
		env.setStringID(envAMX_CFLAGS, strAMXCFlags)
	}

	return env
}

func expandConfigVFSPaths(paths []STR, env Environment) []VFS {
	// Expand+split so a ${VAR} holding a SET-list yields one VFS per dir.
	expanded := expandStmtTokensSTR(paths, env)
	out := make([]VFS, 0, len(expanded))

	for _, path := range expanded {
		out = append(out, parseModulePathVFS(path.string()))
	}

	return out
}

func parseModulePathVFS(path string) VFS {
	if vfsHasPrefix(path) {
		return intern(path)
	}

	return source(path)
}

// expandStmtToken substitutes ${NAME} references in one ya.make statement argument; an
// unresolved reference stays literal. SET assigns eagerly, so one pass reaches the fixpoint.
func expandStmtToken(s string, env Environment) string {
	if s == "$S" {
		return "$(S)"
	}

	if s == "$B" {
		return "$(B)"
	}

	for i := 0; i < 8; i++ {
		prev := s

		// Bare $NAME belongs to the conf command language (.CMD), flattened into the
		// same pass — RUN_PROGRAM pipelines carry $PROTOC_PATH-style refs.
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

// expandBracedVars resolves ${NAME} references left to right; an unresolved one stays literal.
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

// expandStmtTokensSTR expands a parsed (interned) token list. The fast path (no '$')
// returns the SAME id with zero string work; tokens with references re-intern after
// expansion, splitting on whitespace.
func expandStmtTokensSTR(items []STR, env Environment) []STR {
	out := make([]STR, 0, len(items))

	for _, item := range items {
		if !strHasDollar(item) {
			out = append(out, item)

			continue
		}

		for _, f := range strings.Fields(expandStmtToken(item.string(), env)) {
			out = append(out, internStr(f))
		}
	}

	return out
}

// expandStmtTokenSTR is the single-token form (no re-splitting); identity for $-free tokens.
func expandStmtTokenSTR(item STR, env Environment) STR {
	if !strHasDollar(item) {
		return item
	}

	return internStr(expandStmtToken(item.string(), env))
}

func expandStmtTokens(items []string, env Environment) []string {
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

func applyAllPySrcs(fs FS, modulePath string, v *UnknownStmt, d *ModuleData) {
	dirs := []string{"."}
	noTestFiles := false

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

			d.pyNamespace = strPtr(v.Args[i])
		case "RECURSIVE":
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

	for _, dir := range dirs {
		walkRoot := filepath.ToSlash(filepath.Join(moduleRootRel, dir))

		fs.walk(walkRoot, func(rel string, isDir bool) bool {
			if isDir {
				// A subdir with its own ya.make is a separate module; don't descend.
				return rel == walkRoot || !fs.isFile(dirKey(rel), "ya.make")
			}

			if filepath.Ext(rel) != ".py" {
				return false
			}

			base := filepath.Base(rel)

			if noTestFiles && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) {
				return false
			}

			files = append(files, strings.TrimPrefix(rel, moduleRootRel+"/"))

			return false
		})
	}

	sort.Strings(files)
	d.pySrcs = append(d.pySrcs, STRS(files...)...)
	// ALL_PY_SRCS globs source-tree files; their module name is the full path.
	d.pySrcsFullName = append(d.pySrcsFullName, make([]bool, len(files))...)

	if len(files) > 0 {
		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{
			Srcs:      STRS(files...),
			TopLevel:  d.pyTopLevel,
			Namespace: d.pyNamespace,
		})
	}
}

// peerEntryLanguage is the variant the CONSUMER requests of a peer, derived from the
// consumer alone (the peer is NOT pre-parsed). Requesting py of an arbitrary peer is
// safe: genModule re-enters as LangCPP when the peer has no python variant.
func peerEntryLanguage(parent ModuleInstance, parentModuleName TOK) Language {
	if isPythonModuleType(parentModuleName) {
		return LangPy
	}

	if parentModuleName == tokProtoLibrary && parent.Language == LangPy {
		return LangPy
	}

	return LangCPP
}

func derivePeerInstance(ctx *GenCtx, parent ModuleInstance, d *ModuleData, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     source(peerPath),
		Kind:     KindLib,
		Language: peerEntryLanguage(parent, d.moduleStmt.Name),
		Platform: parent.Platform,
	}
}
