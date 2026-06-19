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
	// Experimental holds the YaFF EXPERIMENTAL proto names (basenames as written
	// in the macro). Mirrors the plugin's `experimental=` whitelist: a proto in
	// this list gets the experiments C++ generator, whose generated .yaff.h
	// additionally #includes the library/cpp/yaff/experiments runtime.
	Experimental []string
	// Files holds the YaFF FILES proto names (basenames). Mirrors the plugin's
	// `file=` whitelist (FileWhitelist): when non-empty, the plugin runs its
	// generators — and thus writes any .yaff.h content — only for these protos;
	// every other <proto>.yaff.h is opened but left empty.
	Files []string
}

// isYaff reports whether this plugin is the YaFF protoc plugin (YAFF /
// YAFF_SCHEMA), which emits a <proto>.yaff.h / .yaff.cpp pair.
func (p CppProtoPlugin) isYaff() bool {
	return p.ToolPath == yaffPluginPath
}

// isExperimental matches upstream NeedExpApi: the experiments generator runs for
// a proto whose basename is in the plugin's experimental whitelist.
func (p CppProtoPlugin) isExperimental(protoBaseName string) bool {
	for _, e := range p.Experimental {
		if e == protoBaseName {
			return true
		}
	}

	return false
}

// processesFile matches upstream NeedToProcessFile: the plugin writes generated
// content into <proto>.yaff.h only when the FILES whitelist is empty, or
// contains the proto's basename. A non-whitelisted header is emitted empty.
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

// event2cpp is the C++ proto plugin CPP_EVLOG() registers via
// CPP_PROTO_PLUGIN0(event2cpp tools/event2cpp DEPS library/cpp/eventlog)
// (build/conf/proto.conf:605).
const (
	event2cppPluginName = "event2cpp"
	event2cppToolPath   = "tools/event2cpp"
)

// addCPPProtoPlugin registers a C++ proto plugin on the module: the plugin
// participates in the .proto PB producer command/inputs/generator-refs, and its
// DEPS become module PEERDIRs (upstream: CPP_PROTOBUF_PEERS folded into the
// CPP_PROTO submodule's PEERDIR).
func addCPPProtoPlugin(d *ModuleData, plugin CppProtoPlugin) {
	d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
	d.peerdirs = append(d.peerdirs, STRS(plugin.Deps...)...)
}

// protoCmdPeers returns the C++ proto plugin DEPS (plugin-runtime peers) a
// PROTO_LIBRARY induces, in plugin order, deduped. These peers must lead the
// module's GLOBAL ADDINCL (`-I`) order, ahead of the declared PEERDIR closure.
//
// Upstream models proto codegen with a per-source command (proto.conf
// `_CPP_PROTO_EVLOG_CMD .PEERDIR=library/cpp/eventlog …`, grpc's plugin
// `PEERDIR(contrib/libs/grpc)`) whose plugin-runtime peer is induced ahead of the
// declared PEERDIR for include-dir propagation. For CPP_EVLOG that runtime is
// library/cpp/eventlog, whose GLOBAL ADDINCL closure
// (blockcodecs → codecs/brotli, codecs/snappy, then eventlog/proto → protobuf,
// abseil, re2) must precede the declared proto peers' protobuf/abseil — without
// this, declared proto peerdirs reach protobuf first and brotli/snappy land late.
//
// The base proto runtime contrib/libs/protobuf is deliberately NOT front-loaded:
// for a plain or grpc proto its declared/induced placement already matches
// upstream (grpc's runtime leads, protobuf trails), and for CPP_EVLOG the
// eventlog subtree already emits protobuf at the correct position. Only the
// plugin DEPS move; archive/link closure order keeps the declared d.peerdirs
// order, so this list is kept separate from d.peerdirs (see
// walkPeersForGlobalAddIncl).
func protoCmdPeers(d *ModuleData) []STR {
	front := make([]STR, 0, len(d.cppProtoPlugins))
	seen := map[STR]struct{}{}

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
	modver        string // VERSION() args joined by "." (MODVER); "" means default "unknown"
	hasLicense    bool   // LICENSE() present — gates the _GEN_SBOM_COMPONENT DX node
	hasBisonY     bool   // a .y bison source present — induces PEERDIR build/induced/by_bison
	toolchainName string // TOOLCHAIN(Name) arg — gates the toolchain SBOM DX node
	srcs          []STR
	// srcExtraFlat holds SRC(file flags…) where `file` is also in SRCS: SRCS
	// yields the regular non-flat object (default flags), SRC adds a separate FLAT
	// object with its own flags (e.g. glibcasm's strstr.c + SRC(strstr.c
	// -fgnu89-inline) → both _/…/strstr.c.o and …/strstr.c.o).
	srcExtraFlat       []SrcFlatEntry
	globalSrcs         []STR
	pySrcs             []STR
	pySrcGroups        []PySrcGroup
	pyPyiResources     []ResourceEntry
	pyBuildNoPYC       bool
	pyBuildNoPY        bool
	pyTopLevel         bool
	noExtendedPySearch bool
	enumSrcs           []*GenerateEnumSerializationStmt
	peerdirs           []STR
	// protoCmdPeers are the proto plugin DEPS (plugin-runtime peers, e.g. CPP_EVLOG
	// → library/cpp/eventlog) a PROTO_LIBRARY induces; they lead the GLOBAL ADDINCL
	// order, ahead of the declared PEERDIR closure. See protoCmdPeers(). Subset of
	// peerdirs; affects ADDINCL order only, not archive/link order.
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
	// clangWarnings is CLANG_WARNINGS(...) — the _CLANG_USER_WARNINGS_VALUE the
	// autoincluded linters.make.inc contributes; emitted on C/C++ compiles between
	// GCC_COMPILE_FLAGS and CXXFLAGS (gnu_compiler.conf:284).
	clangWarnings    []ARG
	ldFlags          []ARG
	rpathFlagsGlobal []ARG
	objAddLibsGlobal []ARG
	// srcDirs is the cumulative SRCDIR search path as directory VFS (the type
	// fs.IsFile consumes). collectModule seeds index 0 with the module's own dir,
	// then appends explicit SRCDIRs in declaration order; searched in reverse.
	srcDirs              []VFS
	flags                FlagSet
	hadAllocator         bool
	allocatorName        STR
	muslLite             bool
	muslEnabled          bool
	splitDwarf           bool
	noPythonIncl         bool
	noImportTracing      bool
	usePython3           bool
	useCommonGoogleAPIs  bool
	moduleScopeCFlags    []ARG
	pythonSQLite3        bool
	pyNamespace          *STR
	protoNamespace       *STR
	protoNamespaceGlobal bool
	// ymapsSprotoSrcs holds the .proto sources named by YMAPS_SPROTO(...) (maps
	// sproto.conf). Each gets a .sproto.h PB/yellow producer run through
	// maps/libs/sproto/sprotoc, and the macro's SET(PROTO_HEADER_EXTS .pb.h
	// .sproto.h) makes proto imports in this module induce the .sproto.h sibling
	// header in addition to .pb.h. See emitYmapsSprotoHeaders.
	ymapsSprotoSrcs []STR
	noMypy               bool
	optimizePyProtos     bool
	optimizePyProtosSet  bool
	// needGoogleProtoPeerdirs (NEED_GOOGLE_PROTO_PEERDIRS, default yes) drives the
	// PEERDIR to protobuf/builtin_proto/protos_from_protoc — a PY-only proto whose
	// GLOBAL PROTO_NAMESPACE(contrib/libs/protoc/src) injects -I=$(S)/.../protoc/src
	// into the py-proto cmdline. The protobuf builtins DISABLE it (no self-peer).
	needGoogleProtoPeerdirs bool
	cppProtoPlugins         []CppProtoPlugin
	excludeTags             map[STR]bool
	dynamicLibraryFrom      []STR
	exportsScript           *STR
	ldPlugins               []STR
	arPlugin                *STR

	perSrcCFlags map[STR][]ARG

	// hasFbs marks a .fbs in d.srcs (set in collectModule's suffix pass) —
	// genModule's flatbuffers auto-peer gate, so it needn't rescan srcs.
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
	// srcMeta records, per source, its declaring macro's StatementPriority (ymake
	// module_loader.cpp:38 — SRCS/PY_SRCS=4, everything else including
	// SRC/JOIN_SRCS/codegen=2 by default) and a module-global declaration sequence
	// (declSeq, monotonic across the module's statements AND its INCLUDEs — a plain
	// per-file line does not compose across includes). ymake processes statements
	// in (priority, name) order, so the AR member order is (prio, seq): SRC/JOIN/
	// codegen (prio 2) ahead of plain SRCS (prio 4); within a priority, by seq.
	srcMeta   map[STR]SrcMeta
	declSeq   int
	resources []ResourceEntry

	// bundles are the module's BUNDLE(<Dir> [SUFFIX s] [NAME n]) groups in
	// declaration order. Each emits a BN node that renames the bundled module's
	// primary output into $(B)/<mod>/<name>; a RESOURCE/embed of <name> then
	// resolves to that build output (see emit_bundle.go).
	bundles []BundleEntry

	pyMain *STR

	// noStrip mirrors ENABLE(NO_STRIP); see ymake.core.conf:2669 for the
	// upstream guard that masks STRIP_FLAG when this is set.
	noStrip bool

	// programPairedLib marks the KindLib half of a PY3_PROGRAM multimodule.
	// Upstream PY3_PROGRAM is a multimodule with two submodules: PY3_BIN
	// (PROGRAM-side, emits PY_MAIN) and PY3_BIN_LIB (LIBRARY-side, emits
	// pysrc + namespace + RESOURCE_FILES). The submodule's MODULE_TAG defaults
	// to its own name (lang/confreader.cpp:847-848). We need this flag to
	// emit PY3_BIN_LIB-tagged resource objcopy from the LIBRARY twin, while
	// the PROGRAM-side keeps tag PY3_BIN for PY_MAIN.
	programPairedLib bool

	noCheckImports []STR

	noCheckImportsDisabled bool

	pyRegister []STR

	pyRegisterExplicit []bool

	simdSrcs []SimdSrc

	ragel6Flags []ARG
	conflictMod *ModuleStmt

	// resourceDeclStmts are the RESOURCES_LIBRARY's DECLARE_EXTERNAL_RESOURCE /
	// _HOST_RESOURCES_BUNDLE[_BY_JSON] calls in declaration order; host-uri
	// selection and json reading happen at gen time (genResourcesLibrary).
	resourceDeclStmts []*DeclareResourceStmt

	// primaryOutput is PREBUILT_PROGRAM's PRIMARY_OUTPUT: the fetched binary path
	// (${<NAME>_RESOURCE_GLOBAL}/<name>) copied to the module's program output.
	primaryOutput string

	// inducedDeps are the module's INDUCED_DEPS, bucketed by the macro's consumer
	// type: (cpp …) -> parsedIncludesCpp, (h …) -> parsedIncludesHeader, (h+cpp …) ->
	// both. resolveInducedDeps reads one bucket per generated output's kind.
	inducedDeps ParsedIncludeSet

	setVars map[STR]STR

	// tc carries the module's tool-invocation paths (compiler/archiver/objcopy/
	// strip/linker/python), derived in genModule from the resource-global closure
	// (the build/platform/* peers) rather than from ambient platform flags.
	tc ModuleToolchain
}

// perSrcCFlagsFor / flatSrc gate the sparse per-source attribute maps on len, so
// modules with no SRC-level CFLAGS and no flat-output markers (the vast majority)
// skip the probe. Identical to a direct probe — an empty/nil map yields not-found.
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

// StatementPriority values mirror ymake's TModuleDef::StatementPriority
// (devtools/ymake/module_loader.cpp:38): statements run in (priority, name)
// order, so a lower number is processed (and its objects archived) first.
const (
	stmtPrioDefault = 2 // SRC, SRC_C_*, JOIN_SRCS, RUN_PROGRAM, codegen macros…
	stmtPrioSrcs    = 4 // SRCS, PY_SRCS
)

// SrcMeta carries a source's AR-ordering key: its declaring macro's
// StatementPriority, a module-global declaration sequence, and whether the
// compiled object's input is an in-module generated file (codegen/JOIN). ymake's
// FIFO defers a generated compile to a later round, so generated objects archive
// after the direct ones; within each group the order is (Prio, Seq).
type SrcMeta struct {
	Prio      int
	Seq       int
	Generated bool
}

// sortKey packs the AR-ordering key into one comparable uint64 (high→low:
// generated-flag, StatementPriority, declaration sequence) — direct compiles
// before generated-source compiles (deferred a FIFO round in ymake), then by
// (priority, seq).
func (m SrcMeta) sortKey() uint64 {
	var gen uint64
	if m.Generated {
		gen = 1
	}

	return gen<<60 | uint64(m.Prio)<<32 | uint64(uint32(m.Seq))
}

// nextDeclSeq returns the next module-global declaration sequence number. It is
// bumped once per source/statement as collection walks the ya.make and its
// INCLUDEs in order, so it composes across includes where a per-file line cannot.
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

// srcMetaOf returns a source's recorded (prio, seq); sources without an entry
// (e.g. COPY_FILE auto-srcs) default to the macro priority (2), seq 0.
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

// BundleEntry is one BUNDLE group: the bundled module Dir (arcadia-root
// relative), the collected output Name (basename(Dir)+Suffix by default), and
// the optional Suffix selecting a secondary module output.
type BundleEntry struct {
	Target string
	Name   string
	Suffix string
}

type ResourceEntry struct {
	Path string
	Key  string
	// EndsBatch is true on the LAST entry of one RESOURCE / RESOURCE_FILES
	// statement. Forces the emit loop to flush the accumulating
	// objcopy batch — upstream's TObjCopyResourcePacker is constructed
	// fresh per macro invocation and flushed at its end, so each
	// statement maps to its own objcopy_<hash>.o (see pg_catalog with
	// 13 single-entry RESOURCE() calls → 13 objcopies in REF).
	EndsBatch bool
}

type PySrcGroup struct {
	Srcs      []STR
	TopLevel  bool
	Namespace *STR
}

// SrcFlatEntry is a SRC(file flags…) whose file is also in SRCS — an extra FLAT
// compile object carrying its own per-source flags.
type SrcFlatEntry struct {
	Src   STR
	Flags []ARG
	Seq   int
}

type ArchiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
	// Keys is the ordered ARCHIVE_BY_KEYS key list. Non-nil selects the
	// `${input:Files} -k <:joined keys>` command shape (ymake.core.conf
	// ARCHIVE_BY_KEYS); nil keeps the plain ARCHIVE `${suf=\:;input:Files}` form.
	Keys []string
	// PropagateSourceMembers registers each direct source member (a member with no
	// producer in the codegen registry) as a closure leaf of the archive output, so
	// a C++ unit that #includes the generated header receives the archived sources
	// in its input closure. Generated members propagate via their SourceInputs
	// regardless; this adds the direct-source case used by LJ's LuaSources.inc.
	PropagateSourceMembers bool
}

// ArchiveAsmEntry holds an ARCHIVE_ASM(NAME <n> [DONTCOMPRESS] Files...) call
// (ymake.core.conf). Unlike ARCHIVE it emits a `<NAME>.rodata` resource via the
// archiver (`-q [-p]`) which ymake re-feeds as a generated module source, then
// compiled by the .rodata yasm pipeline into `<NAME>.rodata$OBJECT_SUF`.
type ArchiveAsmEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

// Lj21Archive holds the ordered module-relative .lua names declared by a
// LJ_21_ARCHIVE call. The emit phase (emitLuaJit21) compiles each to a .raw and
// wires the LuaScripts.inc/LuaSources.inc archives.
type Lj21Archive struct {
	Luas []string
}

type CopyFileEntry struct {
	Src         string
	Dst         string
	Auto        bool
	WithContext bool
	// Text marks COPY_FILE(TEXT) — a textual substitution copy. Unlike
	// COPY(WITH_CONTEXT) of a .cpp+sibling.h (single-module, source-dir-relative
	// quoted includes), TEXT copies are shared codegen templates (e.g. minikql
	// llvm16 *.h.txt) copied by several sibling modules, so their includes must
	// resolve in each consumer's own context — see copyFileParsedIncludes.
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

	// COPY_FILE(TEXT src dst …) semantically substitutes src's content into dst
	// — consumers of dst must depend on src (and src's transitive #include
	// closure) for any change to retrigger them. The closure plumbing matches
	// COPY(WITH_CONTEXT …), so route TEXT through the same flag.
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

// resourceOutputVFS canonicalizes a RESOURCE/RESOURCE_FILES file argument to the
// build-output VFS the codegen registry keys producers by. Unlike
// copyFileOutputVFS (which always prepends the module dir, matching producer-side
// OUT tokens), a RESOURCE path may be arcadia-root-relative yet rooted at the
// module dir (e.g. a FROM_SANDBOX OUT embedded as
// yt/yt/library/ytprof/bundle/llvm-symbolizer): such a path is used verbatim
// instead of doubled, mirroring the module-rooted branch of sourceInputVFS on the
// $(B) side. A genuinely module-relative path still resolves under the module dir.
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

// prioVFS is a local ADDINCL dir tagged with the insertion priority of the
// statement that contributed it. The module's -I list is the addInclP entries
// stable-sorted by prio: ymake processes statements in TMakeFileMap priority
// order (module_loader.cpp:StatementPriority), and within one priority merges
// non-multi statements in declaration order. We use dense priorities (not
// ymake's (base<<24)+size() packing) since only relative order matters; the
// stable sort supplies the declaration-order tiebreak.
type prioVFS struct {
	prio int
	vfs  VFS
}

const (
	// prioAddIncl: explicit ADDINCL(...), PEERDIR ADDINCL, and generated-header
	// dirs — non-multi statements, kept in declaration order by the stable sort.
	prioAddIncl = 0
	// prioAddInclSelf: ADDINCLSELF is a user macro (multi in ymake), so its
	// ${MODDIR} dir always sorts after the module's non-multi ADDINCL(...) dirs.
	// See docs/drafts/20260615-1922-addincl-ordering-addinclself-last.md.
	prioAddInclSelf = 1
)

// addLocalIncl records one local (-I) dir at the given statement priority.
func (d *ModuleData) addLocalIncl(prio int, v VFS) {
	d.addInclP = append(d.addInclP, prioVFS{prio: prio, vfs: v})
}

// materializeAddIncl flattens the priority-tagged local ADDINCL contributions
// into d.addIncl in (prio, declaration) order. Called once after collectStmts,
// before the post-collect ADDINCL appends (CF, python3, build-info).
func (d *ModuleData) materializeAddIncl() {
	sort.SliceStable(d.addInclP, func(i, j int) bool {
		return d.addInclP[i].prio < d.addInclP[j].prio
	})

	for _, p := range d.addInclP {
		d.addIncl = append(d.addIncl, p.vfs)
	}

	d.addInclP = nil
}

func collectModule(pm *IncludeParserManager, dd *DeDuper, modulePath string, kind ModuleKind, stmts []Stmt, env Environment) *ModuleData {
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

	// Flatten the priority-tagged local ADDINCL contributions into d.addIncl
	// (ADDINCLSELF's own dir floats after explicit ADDINCL(...)) before the
	// post-collect ADDINCL appends below feed the materialized list.
	d.materializeAddIncl()

	// Seed the SRCDIR search path with the module's own dir at index 0, ahead of
	// the explicit SRCDIRs collectStmts appended. The list is then never empty
	// and the module dir is its base, so resolveSourceVFS and the module-base
	// consumers need no "is there a SRCDIR?" special case.
	d.srcDirs = append([]VFS{dirKey(modulePath)}, d.srcDirs...)

	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	// CF-generated include dirs join UserGlobal in the same deferred step as
	// addInclGlobal, matching upstream ymake where addincl;output on CONFIGURE_FILE
	// outputs is resolved after all explicit ADDINCL statements.
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = nil
	d.cfAddInclGlobal = nil
	filterInvalidAddIncl(fs, dd, d)

	// The PY3_PROGRAM multimodule's BIN half emits PY_MAIN; clear it on the paired
	// LIB half to avoid a duplicate. A standalone PY3_LIBRARY with an explicit
	// PY_SRCS(MAIN …) (e.g. contrib/python/cffi/py3/gen/lib) keeps its PY_MAIN.
	if kind == KindLib && d.programPairedLib {
		d.pyMain = nil
	}

	// Previously cleared d.pySrcs / d.pySrcGroups / d.pyPyiResources /
	// d.pyRegister / d.pyRegisterExplicit / d.allPySrcs when kind==KindBin &&
	// PY3_PROGRAM. That suppressed emitResourceObjcopy on the PROGRAM
	// genModule (its only `hasKvOnly` trigger was len(d.pySrcs)>0), so the
	// PROGRAM's LD never threaded its objcopy_<hash>.o output into the link
	// command — even though the LIBRARY genModule still emitted the objcopy
	// node. Upstream's PY3_PROGRAM LD links the objcopy directly via the
	// objcopyPaths slot (between -Wl,--no-whole-archive and vcs_o); keeping
	// pySrcs populated on the PROGRAM lets emitResourceObjcopy return the
	// same hash-derived path (dedup'd by Emitter on output path) so the
	// LIBRARY's already-emitted node is reused, just with its ref/path now
	// reaching LD.
	d.muslEnabled = env.bool(envMUSL)
	// ENABLE(NO_STRIP) and BUILD_TYPE-driven STRIP_FLAG suppression
	// (ymake.core.conf:2669 — when ($STRIP == "yes" && $NO_STRIP != "yes"))
	// both clear -Wl,--strip-all. Track the effective NO_STRIP env value
	// here so the LD emitter can honour it without re-reading env.
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

	// Pure id-space triage (memoized class); .fbs detection rides the same
	// pass for genModule's flatbuffers auto-peer.
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
		}
	}

	if hasEv {
		d.peerdirs = append(d.peerdirs, strLibraryCppEventlog, strContribLibsProtobuf)
	}

	if hasSc {
		// _SRC("sc").PEERDIR=library/cpp/domscheme — the runtime the generated
		// .sc.h includes.
		d.peerdirs = append(d.peerdirs, strLibraryCppDomscheme)
	}

	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary {
		if !env.bool(envPY3_PROTO) {
			d.peerdirs = append(d.peerdirs, strContribLibsProtobuf)
		}

		if !d.optimizePyProtosSet {
			d.optimizePyProtos = true
		}
	}

	if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary {
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

func filterInvalidAddIncl(fs FS, dd *DeDuper, d *ModuleData) {
	d.addIncl = filterExistingSourceDirs(fs, d.addIncl)
	d.addInclGlobal = filterExistingSourceDirs(fs, d.addInclGlobal)
	d.cythonAddIncl = filterExistingSourceDirs(fs, d.cythonAddIncl)
	d.asmAddIncl = filterExistingSourceDirs(fs, d.asmAddIncl)

	// Rebuild addInclUserGlobal in declaration order, keeping only paths that
	// survived the addInclGlobal filter (for GLOBAL paths) or are in
	// addInclOneLevel (ONE_LEVEL paths, which are never filtered).
	if len(d.addInclUserGlobal) > 0 {
		// One union set through the deduper: the filter below only tests
		// membership in addInclGlobal ∪ addInclOneLevel.
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

func filterExistingSourceDirs(fs FS, paths []VFS) []VFS {
	if len(paths) == 0 {
		return paths
	}

	out := paths[:0]

	for _, path := range paths {
		if shouldCheckSourceDir(path) && !fs.isDir(path, "") {
			continue
		}

		out = append(out, path)
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

// applyArchiveAddIncl reproduces ARCHIVE's ${addincl;noauto;output:NAME} side
// effect (ymake.core.conf). Upstream's `addincl` modifier resolves to
// Module.IncDirs.Add(Parent(output), EIncDirScope::Global) — the output's build
// directory enters the module's local, user-global, and global include buckets,
// reaching the declaring module's own compiles and propagating to its PEERDIR
// consumers (the same Global-scope effect CONFIGURE_FILE's ${addincl;output:Dst}
// produces). Applied after applyPython3AddIncl so the build-root dir follows the
// python include in declaration order, matching upstream where ARCHIVE is
// processed after the USE_PYTHON3 include setup. This subsumes the former
// runtime_py3-specific local include.
func applyArchiveAddIncl(modulePath string, d *ModuleData) {
	for _, a := range d.archives {
		include := build(generatedIncludeDir(modulePath, a.Name))
		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	}

	// LJ_21_ARCHIVE expands to two ARCHIVE_BY_KEYS calls (LuaScripts.inc,
	// LuaSources.inc), whose ${addincl;noauto;output:NAME} outputs land flat in the
	// module build dir. Their archive entries are synthesized later (emitLuaJit21),
	// so drive the same Global-scope addincl side effect from d.lj21 here.
	if d.lj21 != nil {
		include := build(modulePath)
		d.addIncl = append(d.addIncl, include)
		d.addInclGlobal = append(d.addInclGlobal, include)
		d.addInclUserGlobal = append(d.addInclUserGlobal, include)
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

			// Bind MODULE_LANG for IF evaluation of the autoincluded
			// linters.make.inc (appended after the module body in moduleStmts):
			// upstream's `IF (MODULE_LANG == CPP)` gate around CLANG_WARNINGS only
			// fires once the module language is known. sbomComponentLang already
			// maps the module TOK to the uppercase MODULE_LANG token (PY3/CPP/...).
			env.setString(envMODULE_LANG, sbomComponentLang(d.moduleStmt.Name))

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

				// An unresolved ${VAR} stays literal through expansion (upstream
				// Deref keeps unknown refs); ymake's source consumer then warns
				// and ignores it — mirror that, like the PEERDIR arm below. Only
				// a $-bearing token (the memoized bit) can carry one.
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

				// Only a $-bearing token can carry an unresolved ${VAR};
				// plain paths append by id without a view.
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
				d.ragel6Flags = []ARG{internArg(value)}
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
			// SRCDIR is cumulative: each expanded arg (a ${VAR} may be a SET
			// holding many whitespace-separated dirs, e.g. SRCDIR(${__dirs_}))
			// becomes a directory VFS appended to the search path.
			for _, dirTok := range expandStmtTokensSTR(v.Dirs, env) {
				dir := dirTok.string()
				d.srcDirs = append(d.srcDirs, dirKey(dir))
			}
		case *GlobalSrcsStmt:
			appendGlobalSrcGroup(d, expandStmtTokensSTR(v.Sources, env))
		case *GenerateEnumSerializationStmt:
			d.enumSrcs = append(d.enumSrcs, v)
			// Upstream's GENERATE_ENUM_SERIALIZATION macro expands inline to
			// `PEERDIR(tools/enum_parser/enum_serialization_runtime)` (see
			// yatool/build/ymake.core.conf:4518), so the peer enters d.peerdirs
			// at the macro's textual position — BEFORE any later explicit
			// PEERDIR block. We previously appended at the END of genModule,
			// which shifted enum_serialization_runtime to a wrong slot in
			// every consumer's LD link line. Dedup happens later in the
			// genModule peer collector (gen.go:854 seen-set).
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

			d.baseCodegens = append(d.baseCodegens, &expanded)
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
			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *ResourceStmt:
			ensureResourcePeer(modulePath, d)

			for i, pair := range v.Pairs {
				// Upstream's TObjCopyResourcePacker stores RESOURCE() pairs raw
				// (RAW ${BINDIR}/... form, not expanded), so the objcopy_<hash>
				// is computed against ${BINDIR}/<name>. Pre-expanding here
				// drifts the hash vs REF (e.g. yt provider yql_yt_op_settings).
				// RESOURCE_FILES already stores raw — keep them aligned.
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
		case *DeclareResourceStmt:
			// Expand args (ya.make argument semantics): PREBUILT_PROGRAM's
			// DECLARE_EXTERNAL_RESOURCE(NAME ${SANDBOX_RESOURCE_URI}) carries the var
			// SET_RESOURCE_URI_FROM_JSON bound; build/platform's literal sbr:/FOR/json
			// args contain no ${VAR}, so expansion is a no-op for them.
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
			// Expand args like the typed-macro cases (ya.make argument-expansion
			// semantics): a ${VAR} that holds a SET-list (e.g. PY_SRCS(${SRCS}))
			// must substitute and split into the individual tokens before the
			// macro handler reads them.
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

// generatedIncludeDir resolves the build-root include directory for a generated
// output `dst` written into `modulePath` (the parent dir of $(B)/<mod>/<dst>,
// falling back to the module dir when dst is flat).
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
	// recordHandledMacro fires only when a typed case handles the macro —
	// we deferr it and the default branch flips `handled = false` to
	// suppress it. Logging service-keyword args of macros gen does NOT
	// model (LICENSE, VERSION, …) would only generate noise — the right
	// list for those is the one upstream's own macro parser implements,
	// not ours.
	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name, v.Args)
		}
	}()

	switch v.Name {
	case tokAddInclSelf:
		// ADDINCLSELF([FOR preset]) adds -I<own source dir> to the module's
		// compile flags (ymake.core.conf:3177: ADDINCL += [FOR $FOR] ${MODDIR}).
		// ${MODDIR} is the module path, so the added path is Source(modulePath) —
		// the same VFS an explicit ADDINCL(${MODDIR}) would resolve to. A FOR
		// preset routes it to that preset's bucket (cython/asm), matching
		// splitAddInclPaths.
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
		// SET_RESOURCE_URI_FROM_JSON(VarName file.json) (ymake builtin): bind VarName
		// to the by_platform[<current platform>].uri entry of file.json. The current
		// platform here is the instance's (a tool is collected on the host), so the
		// json key is the host-canonized name (linux / linux-aarch64 / darwin / …).
		// Drives the prebuilt-tool contour: IF(VarName != "") gates PREBUILT_PROGRAM.
		// Absent host entry -> VarName left unset (== "") -> from-source path taken.
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
		// NO_PLATFORM_RESOURCES() (ymake.core.conf:4360) is exactly
		// ENABLE(NOPLATFORM_RESOURCES) — a RESOURCES_LIBRARY (e.g. build/platform/
		// linux_sdk) marks itself so it carries no platform resources of its own.
		env.setBool(internEnv("NOPLATFORM_RESOURCES"), true)
	case tokPrimaryOutput:
		// PRIMARY_OUTPUT(path): the module's main output (PREBUILT_PROGRAM copies a
		// fetched binary to ${TARGET}). The arg holds ${<NAME>_RESOURCE_GLOBAL}/...,
		// which binds only after DECLARE_EXTERNAL_RESOURCE — genPrebuiltProgram
		// re-collects with the global bound so the stored value is fully expanded.
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
		// upstream (yatool/build/conf/python.conf:390-398) defines
		// STYLE_RUFF as an optional-kwarg linter macro:
		//   STYLE_RUFF([CONFIG_TYPE config_type] [CHECK_FORMAT]
		//              [RUN_IN_SOURCE_ROOT])
		// We don't model the python lint pipeline today, so the call is a
		// no-op on the emitted graph; walk the args just to acknowledge
		// every legal kwarg as a known service-keyword.
		for i := 0; i < len(v.Args); i++ {
			switch v.Args[i].string() {
			case "CONFIG_TYPE":
				i++
			case "CHECK_FORMAT":
			case "RUN_IN_SOURCE_ROOT":
			}
		}
	case tokLlvmBc:
		// upstream's LLVM_BC is implemented as a Python plugin
		// (yatool/build/plugins/llvm_bc.py): args split via sort_by_keywords
		// into {SYMBOLS: -1, NAME: 1, GENERATE_MACHINE_CODE: 0,
		// NO_COMPILE: 0, SUFFIX: 1} plus the free-arg source list. The plugin
		// then drives llvm_compile_c/cxx/ll → llvm_link → llvm_opt and
		// finally either llvm_llc (GENERATE_MACHINE_CODE) or resource embed.
		// We parse the keywords identically and stash the result on the
		// moduleData; actual node emission is left to a follow-up.
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
		// BUNDLE(<Dir> [SUFFIX s] [NAME n]>…) — build/plugins/bundle.py splits each
		// group and calls on_bundle_target([target, name, suffix]) with
		// name = basename(target)+suffix when NAME is absent. Each group becomes a
		// BN node bringing the bundled module's primary output under <name>
		// (emit_bundle.go). We only collect here; node emission needs the emitter.
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
		// LICENSE() invokes _CONTRIB_MODULE_HOOKS (build/conf/license.conf), which
		// (under _NEED_SBOM_INFO, non-JAVA) attaches _GEN_SBOM_COMPONENT. So the
		// presence of LICENSE — not a contrib/ path — is what gates the DX node.
		d.hasLicense = true
	case tokVersion:
		// SET(MODVER ${Flags}); _GEN_SBOM_COMPONENT renders it as join(".", MODVER).
		// Expand ${COMPILER_VERSION}/${LLD_VERSION} (toolchain VERSION refs).
		d.modver = strings.Join(strStrings(expandStmtTokensSTR(v.Args, env)), ".")
	case tokToolchain:
		// TOOLCHAIN(Name) (contrib_hooks.py) attaches _GEN_SBOM_TOOLCHAIN_COMPONENT:
		// a DX node producing <dir>/toolchain.component.sbom for the named toolchain.
		if len(v.Args) == 1 {
			d.toolchainName = v.Args[0].string()
		}
	case tokCheckConfigH:
		if len(v.Args) != 1 {
			throwFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, v.Args[0])
	case tokDecimalMd5Lower32Bits:
		// DECIMAL_MD5_LOWER_32_BITS(File, FUNCNAME="", Opts...): decimal_md5.py
		// hashes Opts and emits File as a build-root .cpp (ymake.core.conf:4239).
		// Args arrive already expanded (the ${ATLAS_SOURCES} list is split).
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
		// upstream's _CPP_PROTO module-definition body (proto.conf:741-743)
		// runs `PEERDIR += contrib/libs/googleapis-common-protos` so the
		// googleapis dir lands first in the resolved peer list — ahead of
		// even the LIBRARY-chain default peers (linux-headers, libcxx, …).
		// The USE_COMMON_GOOGLE_APIS macro itself just toggles the gate
		// variable _COMMON_GOOGLE_APIS; the actual PEERDIR sits at the
		// module definition. We mirror that:
		//   * d.useCommonGoogleAPIs drives the GLOBAL-ADDINCL walk to visit
		//     googleapis BEFORE language defaults (see gen.go), and the CC
		//     emit floats `-I$(B)/contrib/libs/googleapis-common-protos`
		//     ahead of the linux-headers suffix.
		//   * Prepending into d.peerdirs preserves consumer peer-chain
		//     propagation through PROTO_LIBRARY's specialized early-return
		//     path (gen.go:Peerdirs).
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

		global := false

		for _, arg := range v.Args[:len(v.Args)-1] {
			if arg.string() == "GLOBAL" {
				global = true
			}
		}

		applyProtoNamespace(d, v.Args[len(v.Args)-1], global)
	case tokExportYmapsProto:
		// build/internal/conf/project_specific/maps/sproto.conf:
		//   macro EXPORT_YMAPS_PROTO() { PROTO_NAMESPACE(maps/doc/proto) }
		// PROTO_NAMESPACE always calls PROTO_ADDINCL with a literal GLOBAL
		// (proto.conf), whose SOURCE arm is `GLOBAL FOR proto $(S)/maps/doc/proto`.
		// That GLOBAL proto addincl rides the CPP_PROTO peer closure and renders as
		// -I=$(S)/maps/doc/proto in every transitive consumer's protoc command via
		// ${pre=-I=:_PROTO__INCLUDE} (the include-propagation half, T-30; same
		// protoAddInclGlobal carrier as a parsed `GLOBAL FOR proto X`).
		d.protoAddInclGlobal = append(d.protoAddInclGlobal, mapsDocProto)
		// SET(PROTO_NAMESPACE maps/doc/proto) plus PROTO_ADDINCL's
		// `ADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/$Path)`: applyProtoNamespace sets the
		// output root (so the maps module's own protoc commands and outputs root at
		// maps/doc/proto, T-35) and contributes the GLOBAL build-root C++ ADDINCL
		// that rides the peer closure into transitive consumers' C++ compiles
		// (-I$(B)/maps/doc/proto, T-32). The bare (non-GLOBAL) call matches
		// PROTO_NAMESPACE(maps/doc/proto). The protoc-only source arm stays in
		// protoAddInclGlobal above, so no source-root C++ leakage.
		applyProtoNamespace(d, mapsDocProtoNS, false)
	case tokYmapsSproto:
		// build/internal/conf/project_specific/maps/sproto.conf:
		//   macro YMAPS_SPROTO(FILES...) {
		//       SET(PROTO_HEADER_EXTS .pb.h .sproto.h)
		//       foreach (FILE : $FILES) { _YMAPS_SPROTO_DISPATCH(${lastext:FILE} $FILE) }
		//   }
		// _YMAPS_SPROTO_DISPATCH only matches the "proto" lastext arm, so non-.proto
		// args are dropped upstream; the maps corpus names .proto files exclusively,
		// so an unexpected extension is a fail-fast modelling gap rather than silent.
		for _, argTok := range v.Args {
			if !strings.HasSuffix(argTok.string(), ".proto") {
				throwFmt("gen: %s: YMAPS_SPROTO expects .proto arguments, got %q", modulePath, argTok.string())
			}

			d.ymapsSprotoSrcs = append(d.ymapsSprotoSrcs, argTok)
		}
	case tokExcludeTags:
		// upstream uses EXCLUDE_TAGS to drop submodules of a multimodule
		// from the build (per the PROTO_LIBRARY definition at
		// yatool/build/conf/proto.conf:916-973: PROTO_LIBRARY emits CPP_PROTO
		// + JAVA_PROTO + PY3_PROTO + GO_PROTO + … submodules; ydb's ya.makes
		// disable GO_PROTO / JAVA_PROTO via EXCLUDE_TAGS so the build skips
		// them). Our gen models only CPP_PROTO submodules so the call is a
		// no-op on the graph; record the tag set for parity inspection.
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
		// upstream pybuild plugins translate ENABLE(X) into
		// `unit.set([X, 'yes'])` — a plain boolean env var, picked up by
		// `when ($X == "yes") { … }` clauses. Args are user-defined flag
		// NAMES (not structural keywords), so they're excluded from the
		// strict service-keyword check in recordHandledMacro via
		// macrosAcceptingUserFlags. The cases below keep the few flags
		// whose ENABLE has a direct module-data side-effect.
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
		// Counterpart to ENABLE: clears the env var (and the few specific
		// module-data flags). Generic for the same reasons as ENABLE.
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
		// this SRC adds a separate FLAT object with its own flags. Routing it to
		// srcExtraFlat keeps the SRCS occurrence non-flat and unflagged.
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
		// ymake stores one EXTRALIBS(...) call as a single space-joined OBJADDE_LIB
		// value (TVarStr.Name), and cross-peer collection dedups by that whole-value
		// string, not per token. So two libraries each contributing "-lrt" as part of
		// a DIFFERENT EXTRALIBS value both survive (util's "-lrt -ldl" vs bdb's
		// "-lrt"). Model each call as one group ARG; the cmd_args boundary splits it
		// back into tokens.
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
			case internStr("CYTHON_CPP_H"):
				// C++ mode + companion public header (_BUILDWITH_CYTHON_CPP_H):
				// noext naming and an extra generated .h output.
				cythonCMode = false
				cythonPlainCpp = false
				cythonHeader = true
				cythonApiHeader = false

				continue
			case internStr("CYTHON_C_H"):
				// C mode + companion public header (_BUILDWITH_CYTHON_C_H):
				// noext naming and an extra generated .h output.
				cythonCMode = true
				cythonPlainCpp = false
				cythonHeader = true
				cythonApiHeader = false

				continue
			case internStr("CYTHON_C_API_H"):
				// C mode + public api header (_BUILDWITH_CYTHON_C_API_H): noext
				// naming and extra generated .h plus _api.h outputs.
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
				mainNext = false

				continue
			}

			if cythonizePy && strings.HasSuffix(src, ".py") {
				modName := modNameOverride

				if modName == "" {
					modName = pythonModuleName(modulePath, src, topLevel, namespace)
				}

				// Upstream CYTHONIZE_PY only flips a flag; the .py source rides
				// whatever pyxs list the last CYTHON_C/CYTHON_CPP[_H] directive
				// selected (default C++), so it inherits that variant's mode.
				d.cythonCpp = append(d.cythonCpp, &CythonStmt{
					Src:       src,
					CMode:     cythonCMode,
					Header:    cythonHeader,
					ApiHeader: cythonApiHeader,
					// Upstream: dep=<mod-as-path>.pxd when it resolves (pybuild.py).
					Pxd: strings.ReplaceAll(modName, ".", "/") + ".pxd",
					Options: []string{
						"--module-name", modName,
						"--init-suffix", pythonInitSuffix(modName),
						"--source-root", "$(S)",
						"-X", "set_initial_path=" + modulePath + "/" + src,
					},
				})
				appendPyRegister(d, modName, false)
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
				// Upstream's pybuild.py:397-398 auto-sets PY_MAIN for
				// `__main__.py` in a PY3 PROGRAM-kind unit:
				//   elif py3 and unit_needs_main and main_py:
				//       py_main(unit, mod)
				// `mod` is the dotted module path (no ":main" suffix —
				// onpy_main appends that only on explicit PY_MAIN(...) calls).
				// Without this, expr_nodes_gen/gen's PY3_PROGRAM had no
				// d.pyMain, emitPyMainObjcopy skipped, and the LD missed the
				// `--kvs PY_MAIN=...` objcopy_<hash>.o entry REF emits.
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
		// CPP_EVLOG() expands to CPP_PROTO_PLUGIN0(event2cpp tools/event2cpp
		// DEPS library/cpp/eventlog) + ENABLE(_BUILD_PROTO_AS_EVLOG)
		// (build/conf/proto.conf:605). The CPP_PROTO_PLUGIN0 half is the whole
		// observable behavior: event2cpp becomes an ordinary C++ proto plugin on
		// the .proto PB producer — its --plugin=/--event2cpp_out= command tokens,
		// its tool input, the library/cpp/eventlog peer (whose transitive GLOBAL
		// ADDINCL reaches the proto compile and consumers), and the plugin LD ref
		// rides the PB outputs' GeneratorRefs so event2cpp's INDUCED_DEPS(h+cpp)
		// attach to the .pb.h/.pb.cc closure. _BUILD_PROTO_AS_EVLOG itself only
		// drives an ASSERT vs USE_VANILLA_PROTOC (proto.conf:733), so it needs no
		// model here.
		addCPPProtoPlugin(d, CppProtoPlugin{
			Name:     event2cppPluginName,
			ToolPath: event2cppToolPath,
			Deps:     []string{strLibraryCppEventlog.string()},
		})
	case tokYaff:
		d.cppProtoPlugins = append(d.cppProtoPlugins, parseYAFF(v))
	case tokYaffSchema:
		d.cppProtoPlugins = append(d.cppProtoPlugins, parseYAFFSchema(v))
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

			// Bind the variable like SET does (case *SetStmt), with upstream's
			// append semantics — SET_APPEND(VAR x) sets VAR to "$VAR x"
			// (macro_processor.cpp). The accumulated value then expands wherever a
			// later macro references ${VAR} (FROM_SANDBOX OUT, RESOURCE_FILES, …).
			// Args arrive already expanded, matching upstream's eval-then-join.
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
			// INDUCED_DEPS(h …) -> header consumers; (cpp …) -> translation units;
			// (h+cpp …) -> both. resolveInducedDeps then reads a single bucket per output.
			toHeader := v.Args[0].string() != "cpp"
			toCpp := v.Args[0].string() != "h"

			for _, pTok := range v.Args[1:] {
				p := pTok.string()
				// Args arrive expanded; a rooted spelling ($(S)/... or $(B)/...,
				// from the reserved ${ARCADIA_ROOT}-family refs) stays in the
				// target as is — sc.resolve binds it to its root directly, the
				// way upstream's ResolveAsKnownWithoutCheck classifies rooted
				// paths instead of include-searching them. A bare rel keeps
				// quoted-include search semantics (upstream delays those to
				// include resolution).
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
		// Acknowledged macro: stash its expanded args on the moduleData
		// under d.unhandledMacros so later passes can inspect what was
		// declared. Also record into the audit visible via
		// --dump-ignored-macros. Anything outside acknowledgedMacros is
		// considered a gen bug — open the upstream macro definition in
		// yatool/build/conf or yatool/build/ymake.core.conf and add a typed
		// handler.
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

// llvmBcStmt mirrors upstream's LLVM_BC keyword parse (build/plugins/llvm_bc.py).
// Sources are the free args, Name is mandatory, Suffix overrides the default
// OBJ_SUF, Symbols feeds the -internalize-public-api-list opt pass, and the
// two booleans gate llvm-llc emission (GENERATE_MACHINE_CODE) and per-input
// llvm_compile_* dispatch (NO_COMPILE).
type LlvmBcStmt struct {
	Sources             []string
	Name                string
	Suffix              string
	Symbols             []string
	GenerateMachineCode bool
	NoCompile           bool
	// ClangBCRoot is CLANG_BC_ROOT captured at collection time (set by
	// USE_LLVM_BC{16,18,20}). It holds the deferred reference "$CLANG16_RESOURCE_GLOBAL";
	// emitLLVMBC expands it against the module's resource-global closure (the value
	// declared by the build/platform/clang PEERDIR) to the "$(CLANG16-<id>)" bin-root
	// for clang++/llvm-link/opt.
	ClangBCRoot string
}

func isLlvmBcKeyword(s string) bool {
	switch s {
	case "NAME", "SUFFIX", "SYMBOLS", "GENERATE_MACHINE_CODE", "NO_COMPILE":
		return true
	}

	return false
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

// yaffPluginPath mirrors upstream's YAFF_PLUGIN_PATH (yabs.conf): the YaFF
// protoc plugin lives at this PROGRAM, whose binary is `protoc_plugin`.
const yaffPluginPath = "library/cpp/yaff/tools/protoc_plugin"

// yaffSections collects the NAMESPACE scalar and the FILES / EXPERIMENTAL lists
// shared by YAFF and YAFF_SCHEMA. `positional` receives any leading non-keyword
// arguments (YAFF_SCHEMA's SCHEMA_NAME and NAMESPACE).
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

// yaffExtraOutFlag composes the comma-joined EXTRA_OUT_FLAG exactly as the
// upstream macro does: a leading group (`namespace=…` for YAFF, `tag=…,
// namespace=…` for YAFF_SCHEMA), then `file=` per FILES entry, then
// `experimental=` per EXPERIMENTAL entry. Empty groups stay as empty
// comma-separated pieces; the protoc command builder drops them when splitting.
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

	// NAMESPACE may arrive positionally (YAFF_SCHEMA(schema ns)) or as the
	// NAMESPACE keyword; the keyword form already populated s.namespace.
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

// applyProtoNamespace models PROTO_NAMESPACE (yatool/build/conf/proto.conf):
//   SET(PROTO_NAMESPACE $Namespace)
//   PROTO_ADDINCL(GLOBAL $Namespace)  -> ADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/$Path)
// It records the namespace output root and the build-root C++ ADDINCL. For GLOBAL
// or PROTO_LIBRARY modules the build-root include also rides the GLOBAL/user-global
// peer addincl closure into transitive consumers. The protoc source arm is carried
// separately by the caller via d.protoAddInclGlobal. EXPORT_YMAPS_PROTO reuses this
// so the two macros cannot drift.
func applyProtoNamespace(d *ModuleData, namespace STR, global bool) {
	d.protoNamespace = strPtr(namespace)

	if global {
		d.protoNamespaceGlobal = true
	}

	protoBuildRoot := build(filepath.ToSlash(filepath.Clean(namespace.string())))
	d.addIncl = append(d.addIncl, protoBuildRoot)

	if d.protoNamespaceGlobal || (d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary) {
		d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
		d.addInclUserGlobal = append(d.addInclUserGlobal, protoBuildRoot)
	}
}

func applyArchiveStmt(v *UnknownStmt, d *ModuleData) {
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

// applyArchiveByKeysStmt parses ARCHIVE_BY_KEYS(NAME <name> KEYS <keys> [DONTCOMPRESS]
// files...) (ymake.core.conf). It differs from ARCHIVE only in the command shape — the
// archiver lists members plain and receives the key list via `-k $KEYS` — so it lands in
// the same d.archives slot with Keys set (non-nil selects the keyed form in emitArchive).
// KEYS is a single positional: the colon-joined key list authored verbatim in the ya.make.
// PropagateSourceMembers rides each direct SRCDIR-backed member into the source closure of
// a C++ unit that #includes the generated archive header, matching upstream's flat input
// model (the archive's member inputs reach the consumer through the addincl'd output).
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

// applyArchiveAsmStmt parses ARCHIVE_ASM(NAME <name> [DONTCOMPRESS] files...)
// (ymake.core.conf). It lands in its own d.archiveAsm slot — the emit phase
// produces a `<NAME>.rodata` archiver resource plus the .rodata→asm→object
// compile, distinct from ARCHIVE's addincl `.inc` output.
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

// applyLj21ArchiveStmt parses LJ_21_ARCHIVE(NAME Name LuaFiles...). Mirroring
// build/plugins/lj_archive.py, the lua list is every arg ending in `.lua` (the
// NAME keyword and its value slot do not end in `.lua`, so they fall out). The
// emit phase compiles each to a .raw and archives the raws/sources.
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
		// opensource.conf:28-30 — the open-source contour ships these libs shared
		// (.so via DYNAMIC_LIBRARY); the internal contour leaves them unset, so the
		// <lib>/ya.make selectors fall through to the static PEERDIR.
		env.setString(env_USE_AIO, "dynamic")
		env.setString(env_USE_ICONV, "dynamic")
		env.setString(env_USE_IDN, "dynamic")
	}

	// USE_PREBUILT_TOOLS is defaulted to yes in DefaultIfEnv (build/conf/settings.conf:3)
	// and overridden to "no" by the opensource snapshots' ya.conf (flowing in via the
	// Platform.Flags copy above). It gates each tool's `IF (USE_PREBUILT_TOOLS)
	// INCLUDE(ya.make.prebuilt)` (protoc, rescompiler, rescompressor, py3cc, …):
	// yes -> a fetched prebuilt binary, no -> a from-source host build.

	switch instance.Platform.ISA {
	case ISAX8664:
		env.setBool(envARCH_X86_64, true)
		env.setBool(envARCH_TYPE_64, true)
	case ISAAArch64:
		env.setBool(envARCH_AARCH64, true)
		env.setBool(envARCH_ARM64, true)
		env.setBool(envARCH_TYPE_64, true)
	}

	// HAVE_MKL — the BLAS/LAPACK contrib selectors (contrib/libs/clapack,
	// contrib/libs/cblas) branch IF(HAVE_MKL) between
	// PEERDIR(contrib/libs/intel/mkl) and the source-build fallback
	// (clapack/cblas/libf2c). Upstream binds it in two ordered steps:
	//   1. ymake.core.conf:373 `when ($HAVE_MKL == "")` — default yes iff a
	//      non-sanitized linux/x86_64 contour; a ya.conf flag binding wins.
	//   2. opensource.conf:19 — forced no *unconditionally* under OPENSOURCE,
	//      applied after the default guard, so it overrides even an explicit
	//      HAVE_MKL=yes flag.
	if !env.hasBindingID(envHAVE_MKL) {
		haveMkl := env.bool(envOS_LINUX) && env.bool(envARCH_X86_64) &&
			env.string(envSANITIZER_TYPE) == ""
		env.setBool(envHAVE_MKL, haveMkl)
	}
	if env.bool(envOPENSOURCE) {
		env.setBool(envHAVE_MKL, false)
	}

	// The module-relative path vars upstream's macro expansion provides in
	// every macro: braced (${CURDIR}) and bare ($CURDIR mid-token, via
	// expandEmbeddedDollarVars) forms both resolve through this binding.
	// RESOURCE() pairs are exempt by construction — their collect case stores
	// raw strings without running the expansion (objcopy hashes the raw form).
	// A pathless instance (IF-evaluation in tests) carries no module dir.
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
	// Expand+split through the shared arg-expansion primitive: a ${VAR} holding a
	// SET-list (e.g. ADDINCL(${__dirs_})) yields one VFS per dir, matching the
	// typed-macro argument semantics rather than a single space-joined path.
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

// expandStmtToken substitutes ${NAME} references in one ya.make statement
// argument — upstream ymake's EvalExpr over TEvalContext vars (lang/
// expansion.rl6): a single left-to-right pass, an unresolved reference stays
// literal and scanning continues past it. SET assigns eagerly (the value is
// expanded at assignment), so bound values carry no ${} and one pass reaches
// the fixpoint by construction.
func expandStmtToken(s string, env Environment) string {
	if s == "$S" {
		return "$(S)"
	}

	if s == "$B" {
		return "$(B)"
	}

	for i := 0; i < 8; i++ {
		prev := s

		// Bare $NAME is not part of the ya.make statement layer upstream (the
		// expansion grammar requires braces); it belongs to the conf command
		// language (.CMD), which ay flattens into the same pass — RUN_PROGRAM
		// pipelines carry $PROTOC_PATH-style refs.
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

// expandBracedVars resolves ${NAME} references left to right; an unresolved
// or malformed reference is kept literal and the scan continues after it
// (upstream keeps unknown refs verbatim — expansion.rl6's macro action).
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

// expandStmtTokens expands a ya.make statement's argument list — upstream
// ymake's TEvalContext::Deref (lang/eval_context.cpp): an argument without '$'
// passes through verbatim (a quoted multi-word literal survives whole); an
// argument with '$' gets one substitution pass, and the result is split on
// whitespace in place (a multi-word value contributes several arguments);
// empty results are dropped.
// expandStmtTokensSTR expands a parsed (interned) token list. The fast path —
// no '$' anywhere in the token (the memoized strHasDollar bit) — returns the
// SAME id with zero string work; only tokens with references go through the
// string expansion and re-intern.
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

// expandStmtTokenSTR is the single-token form (no re-splitting): identity for
// $-free tokens.
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
				// A subdir with its own ya.make is a separate module; ALL_PY_SRCS
				// (a GLOB) does not descend into it.
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

	if len(files) > 0 {
		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{
			Srcs:      STRS(files...),
			TopLevel:  d.pyTopLevel,
			Namespace: d.pyNamespace,
		})
	}
}

// peerEntryLanguage is the variant the CONSUMER requests of a peer, derived
// from the consumer alone — the peer is NOT pre-parsed. Only a python-ish
// consumer (or a py-variant PROTO_LIBRARY) requests the py variant; requesting
// py of an arbitrary peer is safe because genModule re-enters as LangCPP when
// the peer turns out to have no python variant (it is not a PROTO_LIBRARY) and
// aliases the py memo key to the C++ result.
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
