package main

// modules.go — module-level parsed-statement collection and
// per-statement application onto moduleData. collectModule walks the
// parsed ya.make statements (IF branches resolved via macros.go) and
// lowers each into moduleData. applyUnknownStmt routes project-specific
// macros (PY_REGISTER, PY_MAIN, RUN_PROGRAM, RESOURCE, ARCHIVE, ANTLR4,
// ...) into their slots. buildIfEnv seeds the per-instance Environment
// from axis flags and CLI defines; derivePeerInstance builds a peer's
// ModuleInstance preserving the parent's language/platform axes.

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type cppProtoPlugin struct {
	Name           string
	ToolPath       string
	OutputSuffixes []string
	Deps           []string
	ExtraOutFlag   string
}

type moduleData struct {
	moduleStmt           *ModuleStmt
	srcs                 []string
	globalSrcs           []string
	globalEventSeq       int
	firstResourceEvent   int
	firstGlobalSrcsEvent int
	pySrcs               []string // .py filenames from PY_SRCS(...)
	pySrcGroups          []pySrcGroup
	pyGeneratedSrcs      map[string][]VFS
	pyPyiResources       []resourceEntry
	pyBuildNoPYC         bool // ENABLE(PYBUILD_NO_PYC); suppresses yapyc3 node emission from PY_SRCS
	pyBuildNoPY          bool // ENABLE(PYBUILD_NO_PY); suppresses raw .py resfs embedding (only the yapyc3 form is embedded)
	pyTopLevel           bool // TOP_LEVEL prefix in PY_SRCS(...); resfs key omits the dotted module-path prefix
	noExtendedPySearch   bool
	enumSrcs             []*GenerateEnumSerializationStmt
	peerdirs             []string
	joinSrcs             []*JoinSrcsStmt
	addIncl              []VFS    // non-GLOBAL ADDINCL paths
	addInclGlobal        []VFS    // ADDINCL(GLOBAL ...); peer-propagated to consumers
	cythonAddIncl        []VFS    // ADDINCL(FOR cython ...); consumed by CY command, not downstream CC
	asmAddIncl           []VFS    // ADDINCL(FOR asm ...); assembler-only include search path, not CC/CXX
	cFlags               []string // non-GLOBAL CFLAGS (own C+C++ sources)
	cFlagsGlobal         []string // CFLAGS(GLOBAL ...); peer-propagated to consumers' C+C++ sources
	cxxFlags             []string // non-GLOBAL CXXFLAGS (own C++ sources)
	cxxFlagsGlobal       []string // CXXFLAGS(GLOBAL ...); peer-propagated to consumers' C++ sources
	cOnlyFlags           []string // non-GLOBAL CONLYFLAGS (own C / .S sources)
	cOnlyFlagsGlobal     []string // CONLYFLAGS(GLOBAL ...); peer-propagated to consumers' C / .S sources
	sFlags               []string // SET_APPEND(SFLAGS ...); appended to AS compiles only
	protocFlags          []string // SET_APPEND(_PROTOC_FLAGS ...); appended to protoc invocations
	flatcFlags           []string // FLATC_FLAGS(...); appended to flatc invocations for `.fbs` sources
	ldFlags              []string
	rpathFlagsGlobal     []string // SET_APPEND(RPATH_GLOBAL ...); peer-propagated to PROGRAM LD rpath slots
	objAddLibsGlobal     []string // EXTRALIBS / PY_EXTRALIBS → OBJADDE_LIB_GLOBAL, peer-propagated to final link steps
	srcDir               *string  // last SRCDIR setting (nil = module dir)
	flags                FlagSet  // FlagSet derived from parsed macro bools
	hadAllocator         bool     // set by applyAllocatorStmt; PROGRAM-default-allocator routing fires only when this is false
	allocatorName        string   // ALLOCATOR(...) name; empty when no ALLOCATOR macro. Used to suppress malloc/api when ALLOCATOR(FAKE)
	muslLite             bool     // ENABLE(MUSL_LITE); flips the default-program-peers musl/full → musl gate
	muslEnabled          bool     // module-local MUSL value after SET()/DEFAULT()/CLI env evaluation
	splitDwarf           bool     // SPLIT_DWARF(); PROGRAM LD emits a separate <binary>.debug output
	noPythonIncl         bool     // NO_PYTHON_INCLUDES(); suppresses PY*_LIBRARY-implicit PEERDIR+=contrib/libs/python (build/conf/python.conf:741-743)
	noImportTracing      bool     // NO_IMPORT_TRACING(); suppresses PY*_PROGRAM implicit import_tracing constructor peer
	usePython3           bool     // USE_PYTHON3() or PY3-family module types; normalised by applyPython3AddIncl. Triggers _PYTHON3_ADDINCL (python.conf:1018-1023): -DUSE_PYTHON3 + contrib/libs/python/Include
	pythonSQLite3        bool     // default-on; DISABLE(PYTHON_SQLITE3) flips off the implicit `_sqlite` peer for PY*_PROGRAM modules
	pyNamespace          *string  // PY_NAMESPACE(...); used by py-proto resource key layout
	protoNamespace       *string  // PROTO_NAMESPACE(...); drives py-proto --ns and output layout
	protoNamespaceGlobal bool
	noMypy               bool // NO_MYPY(); suppresses mypy plugin and .pyi outputs for py-proto
	optimizePyProtos     bool // mirrors OPTIMIZE_PY_PROTOS_FLAG; default-on for PY{2,3}_PROTO variants
	optimizePyProtosSet  bool
	cppProtoPlugins      []cppProtoPlugin // CPP_PROTO_PLUGIN* lowers into extra protoc plugin args + host tool deps on PB nodes
	excludeTags          map[string]bool
	dynamicLibraryFrom   []string
	exportsScript        *string
	ldPlugins            []string // LD_PLUGIN(name.py); each becomes a CP node and feeds `--start-plugins ... --end-plugins` in consumer LDs
	arPlugin             *string  // AR_PLUGIN(name); resolves to `$(S)/<modulePath>/<name>.pyplugin`, injected into AR cmd_args as `--plugin <path>` (ymake.core.conf:3396-3398, ld.conf:366-368)
	// Per-source extra CFLAGS from `SRC(filename extra_cflags...)`,
	// appended right before the input path in ModuleCCInputs.PerSourceCFlags.
	perSrcCFlags map[string][]string
	// DEFAULT(name value) bindings used by ConfigureFileStmt to expand
	// $CFG_VARS. Empty string for DEFAULT(name "").
	defaultVars map[string]string
	// Ordered DEFAULT var names for deterministic $CFG_VARS expansion.
	defaultVarOrder    []string
	configureFiles     []*ConfigureFileStmt
	createBuildInfoFor *string
	antlr4Grammars     []antlr4GrammarInfo
	antlrRuns          []antlrRunInfo
	runPrograms        []*RunProgramStmt
	runPython          []*RunPythonStmt
	checkConfigHeaders []string
	cythonCpp          []*CythonStmt
	// Standalone BUILDWITH_CYTHON_* injects numpy at macro-body position,
	// before the later global python include; PY_SRCS modules keep numpy
	// appended after their regular ADDINCL set.
	cythonNumpyBeforeInclude bool
	swigC                    []swigSrc
	bisonGenExt              string
	grpc                     bool
	yaConfJSON               []string
	allPySrcs                []*UnknownStmt
	// ARCHIVE(NAME <out> [DONTCOMPRESS] files...) in declaration order;
	// each becomes an AR node invoking `$(B)/tools/archiver/archiver`.
	archives []archiveEntry
	// PR-emitted output filename → producing PR NodeRef; consumed by
	// emitArchives to wire AR dep sets to the producer.
	prOutputProducer map[string]NodeRef
	// PR-emitted output filename → PR node inputs[]. RESOURCE consumers
	// keep the generated file as the only objcopy cmd input, but upstream
	// still threads the producer's source inputs into node inputs and
	// enclosing global archive inputs.
	prOutputInputs map[string][]VFS
	// COPY_FILE / COPY_FILE_WITH_CONTEXT declarations in parse order.
	copyFiles []copyFileEntry
	// AUTO copied outputs that should re-enter the source loop as
	// BUILD_ROOT-backed generated sources/headers. Key = module-relative
	// dest path (e.g. `codegen.cpp`).
	copyFileAutoOutputs map[string]copyFileEntry
	// SRC(...) / SRC_C_NO_LTO(...) declarations emit a FLAT output path
	// (no `_/` infix) — unlike SRCS(subdir/foo.cpp) which emits
	// `<modulePath>/_/subdir/foo.cpp.o`. Consulted by emitOneSource.
	flatSrcs map[string]struct{}
	// RESOURCE() / RESOURCE_FILES() pairs, expanded inline at collect
	// time; canonical view for the objcopy packer in resource.go.
	resources []resourceEntry
	// PY_MAIN(<arg>) or the MAIN <src.py> modifier of PY_SRCS(...).
	// Produces a single `PY_MAIN=<dotted-mod>:<func>` kv (build/plugins/pybuild.py:759).
	pyMain *string
	// NO_CHECK_IMPORTS(args...) verbatim arg list; emitNoCheckImportsObjcopy
	// joins by ' ' for the resfs kv (build/plugins/ytest.py:811).
	noCheckImports []string
	// Explicit empty NO_CHECK_IMPORTS() sets NO_CHECK_IMPORTS_FOR_VALUE=""
	// upstream, suppressing ADD_CHECK_PY_IMPORTS without emitting any kv.
	noCheckImportsDisabled bool
	// PY_REGISTER(args...) dotted module names; emitPyRegister emits
	// `<arg>.reg3.cpp` via gen_py3_reg.py plus a CC compiling it
	// (ymake.core.conf:4086-4089).
	pyRegister []string
	// pyRegisterExplicit[i] is true when pyRegister[i] came from an explicit
	// PY_REGISTER() macro (vs implicit cython .pyx / swig .swg
	// auto-registration). Only explicit args carry the onpy_register
	// PyInit_/init_module_ defines that the per-register reg3 snapshot keeps
	// for earlier args; cython/swig reg3 compiles drop all such defines.
	pyRegisterExplicit []bool
	// SRC_C_<VARIANT> entries in declaration order; share the FLAT bucket
	// with SRC()/SRC_C_NO_LTO and are hoisted to the front of the archive.
	simdSrcs []simdSrc
	// SET(RAGEL6_FLAGS ...) override; ymake.core.conf:3284 expands
	// `$RAGEL6_FLAGS ${SRCFLAGS}` ahead of the rest of the ragel6 cmd line.
	ragel6Flags []string
	conflictMod *ModuleStmt
	// INDUCED_DEPS(<exts> headers...) repo-relative header paths. When
	// this module runs as a PROGRAM via RUN_PROGRAM, the headers seed the
	// PR output's EmitsIncludes; the scanner walks their real #include
	// graph to reach the full closure.
	inducedDeps []string
	// SET(name value) bindings (last-write-wins); higher-precedence source
	// for $CFG_VARS expansion (SET overrides DEFAULT). Captures vars set in
	// taken IF branches and INCLUDEd .inc files.
	setVars map[string]string
}

// resourceEntry is one packer input as produced by upstream
// `TObjCopyResourcePacker::HandleResource`. Path == "-" marks a kv-only
// entry (--kvs); otherwise Path is the source path and Key is the
// pre-base64 raw key (the packer applies Base64 encoding when building
// the hash list / cmd_args).
type resourceEntry struct {
	Path string
	Key  string
}

type pySrcGroup struct {
	Srcs      []string
	TopLevel  bool
	Namespace *string
}

// archiveEntry captures one `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)`
// invocation. Files are in declaration order; each is either a module-
// relative source path or the basename of a build-tree artifact
// produced by another macro in the same module (e.g. `__res.pyc` from
// a RUN_PROGRAM).
type archiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

type copyFileEntry struct {
	Src            string
	Dst            string
	Auto           bool
	WithContext    bool
	OutputIncludes []string
}

// antlr4GrammarInfo captures a RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT
// invocation for JV node emission. IsSplit picks the lexer+parser form.
// OutputIncludes (from the macro's OUTPUT_INCLUDES keyword) are
// registered as CP `.g4.cpp` EmitsIncludes so CC consumers walk them.
type antlr4GrammarInfo struct {
	IsSplit        bool
	Lexer          string   // .g4 file (IsSplit=true)
	Parser         string   // .g4 file (IsSplit=true)
	Grammar        string   // .g4 file (IsSplit=false)
	Options        []string // extra antlr4 cmd_args (e.g. ["-package", "NConfReader"])
	Visitor        bool
	Listener       bool
	OutputIncludes []string // repo-relative
}

type antlrRunInfo struct {
	Macro          string
	Args           []string
	INFiles        []string
	OUTFiles       []string
	OUTNoAutoFiles []string
	CWD            *string
	OutputIncludes []string
}

func parseCopyFileEntry(args []string, withContext bool, line int) copyFileEntry {
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
		ThrowFmt("gen: COPY_FILE at line %d expects at least source and destination, got %d args", line, len(args))
	}

	entry := copyFileEntry{
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

func parseCopyEntries(args []string, line int) []copyFileEntry {
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
		ThrowFmt("gen: COPY at line %d expects FROM <dir>", line)
	}
	i++
	if i >= len(args) {
		ThrowFmt("gen: COPY at line %d expects source directory after FROM", line)
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

	out := make([]copyFileEntry, 0, len(files))
	for _, file := range files {
		src := filepath.ToSlash(filepath.Clean(fromDir + "/" + file))
		out = append(out, copyFileEntry{
			Src:            src,
			Dst:            file,
			Auto:           auto,
			WithContext:    withContext,
			OutputIncludes: append([]string(nil), outputIncludes...),
		})
	}

	return out
}

func copyFileInputVFS(modulePath string, src string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, src); ok {
		return vfs
	}

	return Source(filepath.ToSlash(filepath.Clean(modulePath + "/" + src)))
}

func moduleRootedVFS(modulePath string, path string) (VFS, bool) {
	if vfs, ok := ParseVFS(path); ok {
		return vfs, true
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}/"):
		return Source(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_ROOT}/")))), true
	case strings.HasPrefix(path, "${CURDIR}/"):
		return Source(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${CURDIR}/")))), true
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}/"):
		return Build(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}/")))), true
	case strings.HasPrefix(path, "${BINDIR}/"):
		return Build(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${BINDIR}/")))), true
	default:
		return VFS{}, false
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs, ok := moduleRootedVFS(modulePath, dst); ok {
		return vfs
	}

	return Build(filepath.ToSlash(filepath.Clean(modulePath + "/" + dst)))
}

func copyFileIncludeTarget(modulePath string, target string) string {
	if vfs, ok := ParseVFS(target); ok {
		return vfs.Rel
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

// collectModule walks `stmts` (IF branches resolved against `env`) and
// returns a moduleData with all macros classified. IfStmts are
// recursively inlined; nested statements inside taken branches are
// treated as top-level. INCLUDE was already inlined by the parser.
//
// The returned moduleData's `flags` accumulates from the parsed NO_*
// macros — starting from a zero FlagSet.
func collectModule(fs *FS, modulePath string, kind ModuleKind, stmts []Stmt, env Environment) *moduleData {
	env.SetString("MODDIR", modulePath)
	env.SetString("CURDIR", "${ARCADIA_ROOT}/"+modulePath)
	env.SetString("BINDIR", "${ARCADIA_BUILD_ROOT}/"+modulePath)

	d := &moduleData{
		pythonSQLite3:        true,
		bisonGenExt:          ".cpp",
		firstResourceEvent:   -1,
		firstGlobalSrcsEvent: -1,
	}

	collectStmts(modulePath, kind, stmts, env, d)
	filterInvalidAddIncl(fs, d)
	if kind == KindLib {
		// PY_MAIN belongs to the executable half of PY3_PROGRAM. The
		// library half is a self-peer that contributes importable code, not
		// the program entrypoint resource. No-op for standalone PY3_LIBRARY
		// (which never declares PY_MAIN).
		d.pyMain = nil
	} else if kind == KindBin && d.moduleStmt != nil && d.moduleStmt.Name == "PY3_PROGRAM" {
		// The Bin half of PY3_PROGRAM owns the executable wrapper
		// (PY_MAIN/resource objcopy + final LD). The self-peer Lib half owns
		// the importable PY_SRCS-derived artifacts. Keeping PY_SRCS on both
		// sides double-emits the same `.yapyc3` / py-resource outputs.
		d.pySrcs = nil
		d.pySrcGroups = nil
		d.pyPyiResources = nil
		d.pyRegister = nil
		d.pyRegisterExplicit = nil
		d.allPySrcs = nil
	}
	d.muslEnabled = env.Bool("MUSL")

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)

	// _CPP_EVLOG_CMD / _CPP_PROTO_EVLOG_CMD (build/conf/proto.conf:480-491)
	// append `.PEERDIR=library/cpp/eventlog contrib/libs/protobuf` to
	// the owning module for every `.ev` source. Visiting eventlog's
	// transitive chain from `.ev`-bearing modules places those peers
	// ahead of sparsehash in the consumer's ADDINCL aggregation.
	hasEv := false
	hasProto := false
	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src, ".ev"):
			hasEv = true
		case strings.HasSuffix(src, ".proto"):
			hasProto = true
		}
	}

	if hasEv {
		d.peerdirs = append(d.peerdirs, "library/cpp/eventlog", "contrib/libs/protobuf")
	}

	// `_CPP_PROTO_CMD(File)` (build/conf/proto.conf:461-465) fires per
	// `.proto` source and appends `.PEERDIR=contrib/libs/protobuf` to
	// the owning module. Guarded on PROTO_LIBRARY only — other module
	// types may declare .proto sources for codegen without compiling as
	// protobuf-runtime consumers.
	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" {
		d.peerdirs = append(d.peerdirs, "contrib/libs/protobuf")
		if !d.optimizePyProtosSet {
			d.optimizePyProtos = true
		}
	}

	// pybuild lowers PY_SRCS / PY_REGISTER / .pyi resources into
	// RESOURCE() calls. Keep this fallback at module-finalisation time
	// for the synthetic pybuild resources; explicit RESOURCE macros add
	// the peer at their declaration site in collectStmts.
	if len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 || len(d.pyRegister) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	return d
}

func appendGlobalSrcEvent(d *moduleData, src string) {
	if d.firstGlobalSrcsEvent < 0 {
		d.firstGlobalSrcsEvent = d.globalEventSeq
	}
	d.globalEventSeq++
	d.globalSrcs = append(d.globalSrcs, src)
}

func appendGlobalSrcGroup(d *moduleData, srcs []string) {
	if len(srcs) == 0 {
		return
	}
	if d.firstGlobalSrcsEvent < 0 {
		d.firstGlobalSrcsEvent = d.globalEventSeq
	}
	d.globalEventSeq++
	d.globalSrcs = append(d.globalSrcs, srcs...)
}

func ensureResourcePeer(modulePath string, d *moduleData) {
	const resourcePeer = "library/cpp/resource"
	if modulePath == resourcePeer {
		return
	}

	for _, p := range d.peerdirs {
		if p == resourcePeer {
			return
		}
	}

	d.peerdirs = append(d.peerdirs, resourcePeer)
}

func filterInvalidAddIncl(fs *FS, d *moduleData) {
	d.addIncl = filterExistingSourceDirs(fs, d.addIncl)
	d.addInclGlobal = filterExistingSourceDirs(fs, d.addInclGlobal)
	d.cythonAddIncl = filterExistingSourceDirs(fs, d.cythonAddIncl)
	d.asmAddIncl = filterExistingSourceDirs(fs, d.asmAddIncl)
}

func filterExistingSourceDirs(fs *FS, paths []VFS) []VFS {
	if len(paths) == 0 {
		return paths
	}

	out := paths[:0]
	for _, path := range paths {
		if shouldCheckSourceDir(path) && !fs.IsDir(path.Rel) {
			continue
		}

		out = append(out, path)
	}

	return out
}

func shouldCheckSourceDir(path VFS) bool {
	if !path.IsSource() {
		return false
	}
	if path.Rel == "" {
		return false
	}
	if strings.Contains(path.Rel, "$") {
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

// applyPython3AddIncl mirrors _PYTHON3_ADDINCL's USE_ARCADIA_PYTHON=="yes"
// branch (build/conf/python.conf:1018-1023): `CFLAGS+=-DUSE_PYTHON3` and
// `ADDINCL+=GLOBAL contrib/libs/python/Include`. The Include path goes to
// BOTH d.addInclGlobal (peer-propagated) AND d.addIncl (own).
//
// contrib/libs/python skips this to break a cycle with its own ya.make.
//
// NO_PYTHON_INCLUDES() does NOT gate this — upstream gates only the
// implicit `PEERDIR+=contrib/libs/python` (python.conf:741-743). Witness:
// library/python/runtime_py3 declares NO_PYTHON_INCLUDES() yet still
// carries both flag and include on its CC nodes.
func applyPython3AddIncl(modulePath string, d *moduleData) {
	if d.moduleStmt == nil {
		return
	}

	if !d.usePython3 && !pyModuleTypeUsesPython3(d.moduleStmt.Name) {
		return
	}

	if modulePath == "contrib/libs/python" {
		return
	}

	// Normalise so downstream code (defaultPeerCFlags' AutoPeerCFlags
	// slot injection) reads d.usePython3 instead of re-checking the
	// module-type set.
	d.usePython3 = true

	d.addInclGlobal = append(d.addInclGlobal, Source("contrib/libs/python/Include"))
	d.addIncl = append(d.addIncl, Source("contrib/libs/python/Include"))

	// ARCHIVE(NAME ...) in library/python/runtime_py3 auto-injects
	// `${addincl;noauto;output:NAME}` (ymake.core.conf:4143): owner-
	// scoped AND peer-propagated to USE_PYTHON3 consumers. Owner gets
	// it via d.addIncl; consumers see it via genModule's post-merge
	// splice (after abseil-cpp).
	if modulePath == "library/python/runtime_py3" {
		d.addIncl = append(d.addIncl, Build("library/python/runtime_py3"))
	}
}

// applyBuildInfoAddIncl mirrors the implicit `ADDINCL(<build_info_dir>)`
// upstream CREATE_BUILDINFO_FOR emits. The implicit ADDINCL is GLOBAL — the
// generated header must be visible to PEER consumers too.
func applyBuildInfoAddIncl(modulePath string, d *moduleData) {
	if d.createBuildInfoFor == nil {
		return
	}
	biDir := Build(modulePath)
	d.addIncl = append(d.addIncl, biDir)
	d.addInclGlobal = append(d.addInclGlobal, biDir)
}

// pyModuleTypeUsesPython3 returns true for module types whose upstream
// definition in build/conf/python.conf invokes _PYTHON3_ADDINCL
// (directly or via _ARCADIA_PYTHON3_ADDINCL / PYTHON3_ADDINCL):
// PY3_LIBRARY, PY3_PROGRAM[_BIN] / _BASE_PY3_PROGRAM, PY23_LIBRARY's
// PY3 sub-module, PY23_NATIVE_LIBRARY's PY3 sub-module. PY2_* are
// excluded — they use _ARCADIA_PYTHON_ADDINCL and do not emit
// -DUSE_PYTHON3.
func pyModuleTypeUsesPython3(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY3_PROGRAM", "PY3_PROGRAM_BIN",
		"PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return true
	}

	return false
}

// collectStmts is the shared walker collectModule and IfStmt-branch
// expansion both use. It mutates `d` in place.
func collectStmts(modulePath string, kind ModuleKind, stmts []Stmt, env Environment, d *moduleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			// PY3_PROGRAM is sugar for `PY3_PROGRAM_BIN + PEERDIR(self at Kind=Lib)`:
			// the BIN-half PEERDIRs its own LIB-variant. derivePeerInstance
			// resolves the peer with Kind=Lib + Lang=Py, so the same path
			// re-enters genModule as the LIB-variant. Standalone
			// PY3_PROGRAM_BIN() declarations carry no self-peer.
			if v.Name == "PY3_PROGRAM" && kind == KindBin {
				d.peerdirs = append([]string{modulePath}, d.peerdirs...)
			}

			if v.Name == "UNITTEST_FOR" {
				// Mirror `module UNITTEST_FOR: UNITTEST`
				// (ymake.core.conf:1877). MRO order: UNITTEST's
				// PEERDIR(library/cpp/testing/unittest_main) first, then
				// UNITTEST_FOR's tested-dir peer. Real source resolution
				// already rebases through the macro argument; threading that
				// path into own ADDINCL would over-inject `-I$(S)/<dir>` into
				// the compile closure.
				const unittestMainPeer = "library/cpp/testing/unittest_main"

				d.peerdirs = append(d.peerdirs, unittestMainPeer)
				if len(v.Args) > 0 {
					d.peerdirs = append(d.peerdirs, path.Clean(v.Args[0]))
				}
			}
			if isYqlUdfStaticModule(v.Name) {
				d.peerdirs = append(d.peerdirs, yqlUdfImplicitPeers()...)
			}

			d.moduleStmt = moduleStmtForKind(v, kind)
		case *SrcsStmt:
			// SRCS(GLOBAL foo.cpp) uses GLOBAL as a per-source modifier
			// (symbols exported globally, equivalent to GLOBAL_SRCS).
			// Strip GLOBAL tokens and route the following sources to
			// d.globalSrcs.
			routeAllToGlobal := d.moduleStmt != nil && isYqlUdfStaticModule(d.moduleStmt.Name)
			globalNext := false
			globalSrcs := make([]string, 0, len(v.Sources))

			for _, src := range expandStmtTokens(v.Sources, env) {
				if src == "GLOBAL" {
					globalNext = true

					continue
				}

				if routeAllToGlobal {
					globalSrcs = append(globalSrcs, src)
				} else if globalNext {
					appendGlobalSrcEvent(d, src)
					globalNext = false
				} else {
					d.srcs = append(d.srcs, src)
				}
				if strings.HasSuffix(src, ".h.in") {
					addGeneratedHeaderInclude(modulePath, strings.TrimSuffix(src, ".in"), d)
				} else if strings.HasSuffix(src, ".y") {
					addGeneratedOwnHeaderInclude(modulePath, strings.TrimSuffix(src, filepath.Ext(src))+".h", d)
				}
			}
			if routeAllToGlobal {
				appendGlobalSrcGroup(d, globalSrcs)
			}
		case *PeerdirStmt:
			// The ADDINCL modifier means "peer this AND add the same
			// path to this module's own ADDINCL list". Used by
			// `PEERDIR(ADDINCL contrib/libs/protobuf …)` in
			// tools/event2cpp/bin/ya.make.
			addInclNext := false
			for _, p := range expandStmtTokens(v.Paths, env) {
				// Skip unexpanded variable references (e.g.
				// ${STUB_PEERDIRS}). SET-driven optional peerdirs
				// resolve to empty in open-source; no SET evaluator
				// here, so the variable-ref path would fail to resolve.
				if strings.Contains(p, "${") {
					continue
				}
				if p == "ADDINCL" {
					addInclNext = true
					continue
				}
				if p == "GLOBAL" {
					continue
				}
				if addInclNext {
					d.addIncl = append(d.addIncl, parseModulePathVFS(p))
					addInclNext = false
				}
				d.peerdirs = append(d.peerdirs, p)
			}
		case *SetStmt:
			// SET updates the module-local env for following IF branches
			// and config-derived defaults. yes/no stay bools so bare
			// IF(MUSL) and IF(MUSL == "no") both behave as expected.
			value := expandScalarVarRef(v.Value, env)
			env.SetFromString(v.Name, value)

			if d.setVars == nil {
				d.setVars = map[string]string{}
			}
			d.setVars[v.Name] = value

			if v.Name == "RAGEL6_FLAGS" {
				// `_SRC("rl6", ...)` (ymake.core.conf:3284)
				// interpolates $RAGEL6_FLAGS before everything else, so
				// SET replaces the default (last-write-wins).
				d.ragel6Flags = []string{value}
			}
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			expanded := *v
			expanded.Sources = expandStmtTokens(v.Sources, env)
			d.joinSrcs = append(d.joinSrcs, &expanded)
		case *AddInclStmt:
			// GLOBAL paths peer-propagate to consumers via PEERDIR
			// (d.addInclGlobal); own-cmd_args emission uses d.addIncl
			// which includes BOTH GLOBAL and non-GLOBAL in positional
			// declaration order (AllPaths). Empirically the reference
			// emits own GLOBAL ADDINCL on the module's own CC compiles
			// (libcxx algorithm.cpp.o cmd_args[9..11]).
			d.addInclGlobal = append(d.addInclGlobal, expandConfigVFSPaths(v.GlobalPaths, env)...)
			d.addIncl = append(d.addIncl, expandConfigVFSPaths(v.AllPaths, env)...)
			d.cythonAddIncl = append(d.cythonAddIncl, expandConfigVFSPaths(v.CythonPaths, env)...)
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
		case *CFlagsStmt:
			// GLOBAL flags peer-propagate (d.cFlagsGlobal); non-GLOBAL
			// applies to own C+C++ sources only (d.cFlags). composeCC
			// emits the GLOBAL bucket flanking catboost-redux.
			d.cFlagsGlobal = append(d.cFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cFlags = append(d.cFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *CXXFlagsStmt:
			// GLOBAL CXXFLAGS peer-propagate to consumers' C++
			// compiles; non-GLOBAL applies to own C++ sources only.
			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cxxFlags = append(d.cxxFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *CONLYFlagsStmt:
			// GLOBAL CONLYFLAGS peer-propagate to consumers' C / .S
			// compiles; non-GLOBAL applies to own C / .S only.
			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, expandStmtTokens(v.GlobalFlags, env)...)
			d.cOnlyFlags = append(d.cOnlyFlags, expandStmtTokens(v.OwnFlags, env)...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, expandStmtTokens(v.Flags, env)...)
		case *SrcDirStmt:
			// SRCDIR shifts source resolution base for per-source CC /
			// AS / R6 / JOIN_SRCS nodes. LD/AR stay at instance.Path —
			// the binary/archive lives where declared, even if sources
			// are elsewhere.
			d.srcDir = &v.Dir
		case *GlobalSrcsStmt:
			appendGlobalSrcGroup(d, expandStmtTokens(v.Sources, env))
		case *GenerateEnumSerializationStmt:
			d.enumSrcs = append(d.enumSrcs, v)
		case *DefaultVarStmt:
			// Track DEFAULT(name value) for $CFG_VARS expansion.
			if d.defaultVars == nil {
				d.defaultVars = map[string]string{}
			}
			if _, exists := d.defaultVars[v.VarName]; !exists {
				d.defaultVars[v.VarName] = expandScalarVarRef(v.Value, env)
				d.defaultVarOrder = append(d.defaultVarOrder, v.VarName)
			}
			// Bridge the binding into the per-module IF env so later
			// IF (NAME) / IF (NAME == "v") predicates see it. Mirrors
			// TEvalContext::SetStatement's NMacro::DEFAULT branch
			// (devtools/ymake/lang/eval_context.cpp:335-339): only sets
			// when the variable has no prior binding. Env is a per-
			// module clone, so this does not leak across modules.
			env.SetDefaultString(v.VarName, expandScalarVarRef(v.Value, env))
		case *ConfigureFileStmt:
			expanded := *v
			expanded.Src = expandStmtToken(v.Src, env)
			expanded.Dst = expandStmtToken(v.Dst, env)
			d.configureFiles = append(d.configureFiles, &expanded)
			if strings.HasSuffix(expanded.Src, ".h.in") || strings.HasSuffix(expanded.Dst, ".h") {
				addGeneratedHeaderInclude(modulePath, expanded.Dst, d)
			}
		case *CreateBuildInfoStmt:
			d.createBuildInfoFor = &v.OutputHeader
		case *RunAntlr4CppStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        false,
				Grammar:        expandStmtToken(v.Grammar, env),
				Options:        expandStmtTokens(v.Options, env),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: expandStmtTokens(v.OutputIncludes, env),
			})
		case *RunAntlr4CppSplitStmt:
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        true,
				Lexer:          expandStmtToken(v.Lexer, env),
				Parser:         expandStmtToken(v.Parser, env),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: expandStmtTokens(v.OutputIncludes, env),
			})
		case *RunAntlrStmt:
			expanded := antlrRunInfo{
				Macro:          v.Macro,
				Args:           expandStmtTokens(v.Args, env),
				INFiles:        expandStmtTokens(v.INFiles, env),
				OUTFiles:       expandStmtTokens(v.OUTFiles, env),
				OUTNoAutoFiles: expandStmtTokens(v.OUTNoAutoFiles, env),
				OutputIncludes: expandStmtTokens(v.OutputIncludes, env),
			}
			if v.CWD != nil {
				cwd := expandStmtToken(*v.CWD, env)
				expanded.CWD = &cwd
			}
			d.antlrRuns = append(d.antlrRuns, expanded)
		case *RunProgramStmt:
			expanded := *v
			expanded.ToolPath = expandStmtToken(v.ToolPath, env)
			expanded.Args = expandStmtTokens(v.Args, env)
			expanded.INFiles = expandStmtTokens(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokens(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.ToolPaths = expandStmtTokens(v.ToolPaths, env)
			if v.StdoutFile != nil {
				stdout := expandStmtToken(*v.StdoutFile, env)
				expanded.StdoutFile = &stdout
			}
			if v.CWD != nil {
				cwd := expandStmtToken(*v.CWD, env)
				expanded.CWD = &cwd
			}
			d.runPrograms = append(d.runPrograms, &expanded)
		case *RunPythonStmt:
			expanded := *v
			expanded.ScriptPath = expandStmtToken(v.ScriptPath, env)
			expanded.Args = expandStmtTokens(v.Args, env)
			expanded.INFiles = expandStmtTokens(v.INFiles, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.EnvPairs = expandStmtTokens(v.EnvPairs, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			if v.StdoutFile != nil {
				stdout := expandStmtToken(*v.StdoutFile, env)
				expanded.StdoutFile = &stdout
			}
			if v.CWD != nil {
				cwd := expandStmtToken(*v.CWD, env)
				expanded.CWD = &cwd
			}
			d.runPython = append(d.runPython, &expanded)
		case *ResourceStmt:
			if d.firstResourceEvent < 0 {
				d.firstResourceEvent = d.globalEventSeq
			}
			d.globalEventSeq++
			// RESOURCE() has an immediate `.PEERDIR=library/cpp/resource`
			// side effect in ymake.core.conf. Preserve statement order:
			// RESOURCE before PEERDIR must place resource's GLOBAL
			// ADDINCL before later explicit peers.
			ensureResourcePeer(modulePath, d)
			// RESOURCE pairs feed the objcopy packer as-is. Path "-" marks
			// kv-only entries; otherwise (source path, raw key).
			for _, pair := range v.Pairs {
				d.resources = append(d.resources, resourceEntry{Path: pair.Path, Key: pair.Key})
			}
		case *ResourceFilesStmt:
			if d.firstResourceEvent < 0 {
				d.firstResourceEvent = d.globalEventSeq
			}
			d.globalEventSeq++
			ensureResourcePeer(modulePath, d)
			// Expand RESOURCE_FILES into resource entries per
			// build/plugins/res.py:onresource_files. For each path P
			// (after DONT_COMPRESS / PREFIX / DEST / STRIP keywords are
			// processed), append a kv-only entry plus a source entry.
			// The ${rootrel;...} placeholder is preserved verbatim
			// because resource.go:objcopyHash needs the pre-expansion
			// form.
			for _, e := range expandResourceFiles(v.Args) {
				d.resources = append(d.resources, e)
			}
		case *IfStmt:
			taken := v.Then

			if !EvalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(modulePath, kind, taken, env, d)
		case *UnknownStmt:
			applyUnknownStmt(modulePath, v, d, env)
		default:
			ThrowFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

func moduleStmtForKind(stmt *ModuleStmt, kind ModuleKind) *ModuleStmt {
	// PY3_PROGRAM enters genModule twice — once with Kind=Bin and once
	// with Kind=Lib (the latter reached via the self-PEERDIR injected
	// in collectStmts). The Bin visit keeps the original "PY3_PROGRAM"
	// name so it stays distinguishable from a standalone
	// PY3_PROGRAM_BIN(); the Lib visit is renamed to PY3_LIBRARY so it
	// shares the PY3_LIBRARY emit codepath with standalone PY3_LIBRARY.
	if stmt.Name == "PY3_PROGRAM" && kind == KindLib {
		out := *stmt
		out.Name = "PY3_LIBRARY"
		return &out
	}
	return stmt
}

func addGeneratedHeaderInclude(modulePath, dst string, d *moduleData) {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.Rel)))
	rel := dir
	if dir != "." && dir != "" {
		rel = filepath.ToSlash(filepath.Clean(dir))
	} else {
		rel = modulePath
	}

	include := Build(rel)
	d.addIncl = append(d.addIncl, include)
	d.addInclGlobal = append(d.addInclGlobal, include)
}

func addGeneratedOwnHeaderInclude(modulePath, dst string, d *moduleData) {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.Rel)))
	rel := dir
	if dir != "." && dir != "" {
		rel = filepath.ToSlash(filepath.Clean(dir))
	} else {
		rel = modulePath
	}

	d.addIncl = append(d.addIncl, Build(rel))
}

// applyUnknownStmt routes an UnknownStmt by name. NO_LIBC / NO_UTIL /
// NO_RUNTIME / NO_PLATFORM / NO_COMPILER_WARNINGS flip the matching
// FlagSet bits. ALLOCATOR(NAME) resolves to an implicit PEERDIR
// (ymake.core.conf:961-1035). Anything else must be in the metadata
// whitelist; unknown names throw so a new ya.make macro surfaces
// immediately rather than being silently dropped.
func applyUnknownStmt(modulePath string, v *UnknownStmt, d *moduleData, env Environment) {
	switch v.Name {
	case "NO_LIBC":
		// build/ymake.core.conf: NO_LIBC() calls NO_RUNTIME() which calls NO_UTIL().
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_UTIL":
		d.flags.NoUtil = true
	case "NO_RUNTIME":
		// build/ymake.core.conf: NO_RUNTIME() calls NO_UTIL().
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_PLATFORM":
		// build/ymake.core.conf: NO_PLATFORM() calls NO_LIBC() → NO_RUNTIME() → NO_UTIL().
		d.flags.NoPlatform = true
		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_COMPILER_WARNINGS":
		d.flags.NoCompilerWarnings = true
	case "NO_WSHADOW":
		d.flags.NoWShadow = true
	case "USE_LLVM_BC16":
		env.SetString("CLANG_BC_ROOT", env.String("CLANG16_RESOURCE_GLOBAL"))
		env.SetString("LLVM_LLC_TOOL", "contrib/libs/llvm16/tools/llc")
	case "USE_LLVM_BC18":
		env.SetString("CLANG_BC_ROOT", env.String("CLANG18_RESOURCE_GLOBAL"))
		env.SetString("LLVM_LLC_TOOL", "contrib/libs/llvm18/tools/llc")
	case "USE_LLVM_BC20":
		env.SetString("CLANG_BC_ROOT", env.String("CLANG20_RESOURCE_GLOBAL"))
		env.SetString("LLVM_LLC_TOOL", "contrib/libs/llvm20/tools/llc")
	case "SPLIT_DWARF":
		d.splitDwarf = true
	case "NO_SPLIT_DWARF":
		d.splitDwarf = false
	case "NO_PYTHON_INCLUDES":
		// NO_PYTHON_INCLUDES() sets NO_PYTHON_INCLS=yes
		// (build/conf/python.conf:928-929). The PY*_LIBRARY implicit
		// `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }`
		// at python.conf:741-743 is gated by this; the implicit-peer
		// code in genModule reads d.noPythonIncl.
		d.noPythonIncl = true
	case "NO_IMPORT_TRACING":
		d.noImportTracing = true
	case "NO_EXTENDED_SOURCE_SEARCH":
		d.noExtendedPySearch = true
	case "STYLE_RUFF":
		// Linter-only macro. It does not emit build nodes for `ay make`
		// target graphs.
	case "LLVM_BC":
		if len(v.Args) < 2 {
			ThrowFmt("LLVM_BC expects at least source and output, got %d", len(v.Args))
		}
		if env.String("CLANG_BC_ROOT") == "" || env.String("LLVM_LLC_TOOL") == "" {
			ThrowFmt("LLVM_BC requires USE_LLVM_BC16/18/20 before invocation")
		}
		// The generated pg_kernels.* and registration .inc files are
		// checked into the tree and compiled through the regular SRCS
		// path. Recognize LLVM_BC so the upstream preconditions still
		// hold, but it does not add extra graph nodes in the current
		// source-root-driven model.
	case "MAVEN_GROUP_ID":
		// Java export metadata. build/conf/java.conf documents no effect
		// on regular builds.
	case "CHECK_CONFIG_H":
		if len(v.Args) != 1 {
			ThrowFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, expandStmtToken(v.Args[0], env))
	case "BUILDWITH_CYTHON_CPP":
		if len(v.Args) == 0 {
			ThrowFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     expandStmtToken(v.Args[0], env),
			Options: expandStmtTokens(v.Args[1:], env),
		})
		d.cythonNumpyBeforeInclude = true
	case "BUILDWITH_CYTHON_C":
		if len(v.Args) == 0 {
			ThrowFmt("BUILDWITH_CYTHON_C expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     expandStmtToken(v.Args[0], env),
			Options: expandStmtTokens(v.Args[1:], env),
			CMode:   true,
		})
		d.cythonNumpyBeforeInclude = true
	case "BISON_GEN_C":
		d.bisonGenExt = ".c"
	case "BISON_GEN_CPP":
		d.bisonGenExt = ".cpp"
	case "GRPC":
		d.grpc = true
		d.peerdirs = append(d.peerdirs, "contrib/libs/grpc")
	case "PY_NAMESPACE":
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_NAMESPACE expects exactly 1 argument, got %d", len(v.Args))
		}
		d.pyNamespace = stringPtr(expandStmtToken(v.Args[0], env))
	case "YQL_LAST_ABI_VERSION":
		if len(v.Args) != 0 {
			ThrowFmt("YQL_LAST_ABI_VERSION expects exactly 0 arguments, got %d", len(v.Args))
		}
		d.cxxFlags = append(d.cxxFlags, "-DUSE_CURRENT_UDF_ABI_VERSION")
	case "YQL_ABI_VERSION":
		if len(v.Args) != 3 {
			ThrowFmt("YQL_ABI_VERSION expects exactly 3 arguments, got %d", len(v.Args))
		}
		d.cxxFlags = append(d.cxxFlags,
			"-DUDF_ABI_VERSION_MAJOR="+v.Args[0],
			"-DUDF_ABI_VERSION_MINOR="+v.Args[1],
			"-DUDF_ABI_VERSION_PATCH="+v.Args[2],
		)
	case "PROTOC_FATAL_WARNINGS":
		if len(v.Args) != 0 {
			ThrowFmt("PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got %d", len(v.Args))
		}
		d.protocFlags = append(d.protocFlags, "--fatal_warnings")
	case "USE_COMMON_GOOGLE_APIS":
		d.peerdirs = append(d.peerdirs, "contrib/libs/googleapis-common-protos")
	case "FLATC_FLAGS":
		d.flatcFlags = append(d.flatcFlags, expandListVars(v.Args, env)...)
	case "COPY_FILE", "COPY_FILE_WITH_CONTEXT":
		args := expandListVars(v.Args, env)
		for i := range args {
			args[i] = expandConfigString(args[i], env)
		}
		entry := parseCopyFileEntry(args, v.Name == "COPY_FILE_WITH_CONTEXT", v.Line)
		d.copyFiles = append(d.copyFiles, entry)
		if entry.Auto {
			dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
			prefix := modulePath + "/"
			if strings.HasPrefix(dstVFS.Rel, prefix) {
				dstRel := strings.TrimPrefix(dstVFS.Rel, prefix)
				if isSourceEligibleForCopyAuto(dstRel) && !flagsContain(d.srcs, dstRel) {
					d.srcs = append(d.srcs, dstRel)
				}
				if d.copyFileAutoOutputs == nil {
					d.copyFileAutoOutputs = make(map[string]copyFileEntry)
				}
				d.copyFileAutoOutputs[dstRel] = entry
			}
		}
	case "COPY":
		for _, entry := range parseCopyEntries(expandListVars(v.Args, env), v.Line) {
			d.copyFiles = append(d.copyFiles, entry)
			if entry.Auto {
				dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
				prefix := modulePath + "/"
				if strings.HasPrefix(dstVFS.Rel, prefix) {
					dstRel := strings.TrimPrefix(dstVFS.Rel, prefix)
					if isSourceEligibleForCopyAuto(dstRel) && !flagsContain(d.srcs, dstRel) {
						d.srcs = append(d.srcs, dstRel)
					}
					if d.copyFileAutoOutputs == nil {
						d.copyFileAutoOutputs = make(map[string]copyFileEntry)
					}
					d.copyFileAutoOutputs[dstRel] = entry
				}
			}
		}
	case "PROTO_NAMESPACE":
		if len(v.Args) == 0 {
			ThrowFmt("gen: PROTO_NAMESPACE expects at least 1 argument")
		}
		d.protoNamespace = stringPtr(expandStmtToken(v.Args[len(v.Args)-1], env))
		for _, arg := range v.Args[:len(v.Args)-1] {
			if arg == "GLOBAL" {
				d.protoNamespaceGlobal = true
			}
		}
		protoBuildRoot := Build(filepath.ToSlash(filepath.Clean(*d.protoNamespace)))
		d.addIncl = append(d.addIncl, protoBuildRoot)
		if d.protoNamespaceGlobal || (d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY") {
			d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
		}
	case "EXCLUDE_TAGS":
		if d.excludeTags == nil {
			d.excludeTags = make(map[string]bool)
		}
		for _, arg := range v.Args {
			d.excludeTags[arg] = true
		}
	case "YA_CONF_JSON":
		if len(v.Args) != 1 {
			ThrowFmt("YA_CONF_JSON expects exactly 1 argument, got %d", len(v.Args))
		}

		d.yaConfJSON = append(d.yaConfJSON, expandStmtToken(v.Args[0], env))
	case "ALLOCATOR":
		applyAllocatorStmt(v, d)
	case "ARCHIVE":
		// `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)` per
		// ymake.core.conf:4142-4145.
		applyArchiveStmt(v, d)
	case "ENABLE":
		// MUSL_LITE flips defaultProgramPeerdirsFor to musl (not musl/full),
		// breaking the yasm → musl/full → asmlib → yasm cycle. PYBUILD_NO_PYC
		// suppresses yapyc3 node emission; PYBUILD_NO_PY (no 'C') suppresses
		// raw .py resfs embedding but keeps yapyc3 running.
		for _, a := range v.Args {
			if a == "MUSL_LITE" {
				d.muslLite = true
			}
			if a == "PYBUILD_NO_PYC" {
				d.pyBuildNoPYC = true
			}
			if a == "PYBUILD_NO_PY" {
				d.pyBuildNoPY = true
			}
			if a == "PY_PROTO_MYPY_ENABLED" {
				d.noMypy = false
			}
			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = true
			}
		}
	case "DISABLE":
		for _, a := range v.Args {
			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
			}
		}
	case "NO_MYPY":
		d.noMypy = true
	case "NO_OPTIMIZE_PY_PROTOS":
		d.optimizePyProtos = false
		d.optimizePyProtosSet = true
	case "OPTIMIZE_PY_PROTOS":
		d.optimizePyProtos = true
		d.optimizePyProtosSet = true
	case "SRC":
		// SRC(filename [extra_cflags...]) registers a single source and
		// attaches per-source extra CFLAGS. Output path is FLAT (no
		// `_/` infix) — see flatSrcs.
		if len(v.Args) == 0 {
			ThrowFmt("gen: SRC() requires at least 1 argument (filename); got 0 at line %d", v.Line)
		}

		filename := expandStmtToken(v.Args[0], env)
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}

		if len(v.Args) > 1 {
			if d.perSrcCFlags == nil {
				d.perSrcCFlags = map[string][]string{}
			}

			extras := expandStmtTokens(v.Args[1:], env)
			d.perSrcCFlags[filename] = append(d.perSrcCFlags[filename], extras...)
		}
	case "SRC_C_NO_LTO":
		// SRC_C_NO_LTO(filename) disables LTO for the named source.
		// LTO is already off in the debug build, so this reduces to
		// plain SRCS in the current closure. Output path is FLAT.
		if len(v.Args) != 1 {
			ThrowFmt("gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got %d at line %d", len(v.Args), v.Line)
		}

		filename := v.Args[0]
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
	case "SRC_C_AVX", "SRC_C_AVX2", "SRC_C_AVX512", "SRC_C_AMX", "SRC_C_SSE2", "SRC_C_SSE3", "SRC_C_SSSE3",
		"SRC_C_SSE4", "SRC_C_SSE41", "SRC_C_XOP":
		// SRC_C_<V>(filename [extra_flags...]) emits one CC node with
		// the variant's -m<flag> bundle plus extras, into a FLAT
		// `<src>.<variant>.pic.o`. Per ymake.core.conf:3848-3923, the
		// variant CFLAGS come first then the macro's trailing args.
		variant, ok := simdVariantFor(v.Name)
		if !ok {
			ThrowFmt("gen: unrecognised SIMD-permutation macro %q at line %d (simdVariants table out of sync)", v.Name, v.Line)
		}
		if len(v.Args) == 0 {
			ThrowFmt("gen: %s() requires at least 1 argument (filename); got 0 at line %d", v.Name, v.Line)
		}

		filename := expandStmtToken(v.Args[0], env)
		flags := make([]string, 0, len(variant.CFlags)+len(v.Args)-1)
		flags = append(flags, variant.CFlags...)
		flags = append(flags, expandStmtTokens(v.Args[1:], env)...)

		d.simdSrcs = append(d.simdSrcs, simdSrc{
			Src:     filename,
			Variant: variant.Suffix,
			CFlags:  flags,
			Line:    v.Line,
		})
	case "LD_PLUGIN":
		// LD_PLUGIN(name.py) declares a python plugin passed to the
		// linker via `--start-plugins ... --end-plugins` in consumer
		// PROGRAM LDs. The file is copied via CP from
		// `$(S)/<modulePath>/name.py` to
		// `$(B)/<modulePath>/name.py.pyplugin`.
		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case "AR_PLUGIN":
		// AR_PLUGIN(name) registers a python plugin for the module's AR
		// step. ymake.core.conf:3396-3398 does
		// `SET(_AR_PLUGIN $name.pyplugin)`; ld.conf:366-368 injects
		// `--plugin ${input:_AR_PLUGIN}` between the inner `-- ... --`
		// of _LD_ARCHIVER.
		if len(v.Args) != 1 {
			ThrowFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}
		d.arPlugin = stringPtr(v.Args[0] + ".pyplugin")
	case "DYNAMIC_LIBRARY_FROM":
		if len(v.Args) == 0 {
			ThrowFmt("gen: DYNAMIC_LIBRARY_FROM expects at least 1 argument")
		}
		d.dynamicLibraryFrom = append(d.dynamicLibraryFrom, v.Args...)
		d.peerdirs = append(d.peerdirs, v.Args...)
	case "EXPORTS_SCRIPT":
		if len(v.Args) != 1 {
			ThrowFmt("gen: EXPORTS_SCRIPT expects exactly 1 argument, got %d", len(v.Args))
		}
		d.exportsScript = stringPtr(v.Args[0])
	case "EXTRALIBS":
		for _, arg := range v.Args {
			lib := arg
			if !strings.HasPrefix(lib, "-") {
				lib = "-l" + lib
			}
			if !flagsContain(d.objAddLibsGlobal, lib) {
				d.objAddLibsGlobal = append(d.objAddLibsGlobal, lib)
			}
		}
	case "USE_PYTHON3":
		// Implicit PEERDIRs per python.conf:1064-1071. contrib/tools/python3
		// is intentionally absent: PROGRAM/LIBRARY callers reach it
		// transitively via contrib/libs/python, and adding it here would
		// reorder peer-AddInclGlobal across the closure.
		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)
		// applyPython3AddIncl runs the -DUSE_PYTHON3 + Include injection
		// in collectModule's post-pass.
		d.usePython3 = true
	case "PY_SRCS":
		// PY_SRCS accepts TOP_LEVEL (ns="" instead of dotted modulePath)
		// and MAIN (flags the next path as program entry point — in py3
		// emits a PY_MAIN=<dotted-mod>:main kv resource per
		// pybuild.py:362-396). pyMain is captured at parse time;
		// resource.go consumes it.
		topLevel := false
		mainNext := false
		cythonizePy := false
		cythonPlainCpp := false
		cythonCMode := false
		swigCMode := false
		var namespace *string
		var groupSrcs []string
		cythonStmtStart := len(d.cythonCpp)
		var cythonDirectives []string
		for i := 0; i < len(v.Args); i++ {
			a := v.Args[i]

			switch a {
			case "TOP_LEVEL":
				topLevel = true
				d.pyTopLevel = true
				continue
			case "NAMESPACE":
				i++
				if i >= len(v.Args) {
					ThrowFmt("PY_SRCS NAMESPACE expects a value")
				}
				namespace = stringPtr(v.Args[i])
				d.pyNamespace = namespace

				continue
			case "CYTHONIZE_PY":
				cythonizePy = true
				continue
			case "CYTHON_CPP":
				cythonPlainCpp = true
				cythonCMode = false
				continue
			case "CYTHON_C":
				cythonCMode = true
				cythonPlainCpp = false
				continue
			case "CYTHON_DIRECTIVE":
				i++
				if i >= len(v.Args) {
					ThrowFmt("PY_SRCS CYTHON_DIRECTIVE expects a value")
				}
				cythonDirectives = append(cythonDirectives, "-X", v.Args[i])
				continue
			case "SWIG_C":
				swigCMode = true
				continue
			case "SWIG_CPP":
				swigCMode = false
				continue
			case "MAIN":
				mainNext = true
				continue
			}

			src := a
			modNameOverride := ""
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				src = a[:eq]
				modNameOverride = a[eq+1:]
			}

			if strings.HasSuffix(src, ".pyx") {
				modName := modNameOverride
				if modName == "" {
					modName = pythonModuleName(modulePath, src, topLevel, namespace)
				}
				stmt := &CythonStmt{
					Src:   src,
					CMode: cythonCMode,
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
				d.cythonCpp = append(d.cythonCpp, &CythonStmt{
					Src: src,
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
					d.swigC = append(d.swigC, swigSrc{Src: src, Module: modName})
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

			if strings.Contains(a, "=") && !strings.HasSuffix(src, ".py") {
				continue
			}

			d.pySrcs = append(d.pySrcs, src)
			groupSrcs = append(groupSrcs, src)
			if mainNext {
				// Compute the dotted module name per pybuild.py:289,385:
				//   ns = upath.replace('/','.') + '.'   (default)
				//   ns = ''                              (TOP_LEVEL)
				//   mod_name = stripext(arg).replace('/','.')
				//   mod = ns + mod_name
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
				d.pyMain = stringPtr(modName + ":main")
				mainNext = false
			}
		}
		if len(cythonDirectives) > 0 {
			for j := cythonStmtStart; j < len(d.cythonCpp); j++ {
				d.cythonCpp[j].Options = append(d.cythonCpp[j].Options, cythonDirectives...)
			}
		}
		if len(groupSrcs) > 0 {
			d.pySrcGroups = append(d.pySrcGroups, pySrcGroup{
				Srcs:      groupSrcs,
				TopLevel:  topLevel,
				Namespace: namespace,
			})
		}
	case "ALL_PY_SRCS":
		d.allPySrcs = append(d.allPySrcs, v)
	case "PY_MAIN":
		// PY_MAIN(<arg>) per build/plugins/pybuild.py:762.
		// Normalise: `/` → `.`, append `:main` when there's no colon.
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := strings.ReplaceAll(v.Args[0], "/", ".")
		if !strings.Contains(arg, ":") {
			arg += ":main"
		}
		d.pyMain = stringPtr(arg)
	case "PY_CONSTRUCTOR":
		// PY_CONSTRUCTOR(<module[:func]>) per pybuild.py:onpy_constructor:
		// emits a kv-only resource under py/constructors/, defaulting
		// to "=init" when no function is specified.
		if d.firstResourceEvent < 0 {
			d.firstResourceEvent = d.globalEventSeq
		}
		d.globalEventSeq++
		ensureResourcePeer(modulePath, d)
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_CONSTRUCTOR expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := v.Args[0]
		if strings.Contains(arg, ":") {
			arg = strings.Replace(arg, ":", "=", 1)
		} else {
			arg += "=init"
		}
		d.resources = append(d.resources, resourceEntry{Path: "-", Key: "py/constructors/" + arg})
	case "NO_CHECK_IMPORTS":
		// NO_CHECK_IMPORTS(args...) per ytest.py:on_register_no_check_imports
		// (build/plugins/ytest.py:808). Args joined by ' ' in declaration
		// order become both the resfs value and the pathid() input
		// (md5 → lower-cased unpadded base32). Empty args are not a no-op:
		// upstream sets NO_CHECK_IMPORTS_FOR_VALUE="" and suppresses
		// ADD_CHECK_PY_IMPORTS.
		if len(v.Args) > 0 {
			d.noCheckImports = append(d.noCheckImports, v.Args...)
		} else {
			d.noCheckImportsDisabled = true
		}
	case "CPP_PROTO_PLUGIN0", "CPP_PROTO_PLUGIN", "CPP_PROTO_PLUGIN2":
		plugin := parseCPPProtoPlugin(v)
		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
		d.peerdirs = append(d.peerdirs, plugin.Deps...)
	case "PY_REGISTER":
		// PY_REGISTER(args...) dotted module names. emitPyRegister
		// later emits one PY node generating `<arg>.reg3.cpp` plus a
		// CC compiling it; both flow into the module's `.global.a`
		// (mirror of SRCS(GLOBAL $Func.reg3.cpp) inside _PY3_REGISTER
		// at ymake.core.conf:4086-4089).
		for _, name := range v.Args {
			appendPyRegister(d, name, true)
		}
	case "SET_APPEND":
		// SET_APPEND(<var> <values...>) appends to a ymake variable.
		// SFLAGS threads between CFLAGS and `-c -o` in AS cmd_args
		// (ymake.core.conf:3217). RPATH_GLOBAL propagates on its own axis
		// so PROGRAM LD emission can place it separately from LDFLAGS.
		if len(v.Args) >= 2 {
			switch v.Args[0] {
			case "SFLAGS":
				d.sFlags = append(d.sFlags, expandStmtTokens(v.Args[1:], env)...)
			case "_PROTOC_FLAGS":
				d.protocFlags = append(d.protocFlags, expandStmtTokens(v.Args[1:], env)...)
			case "RPATH_GLOBAL":
				for _, arg := range expandStmtTokens(v.Args[1:], env) {
					arg = strings.ReplaceAll(arg, `${"$"}`, "$")
					d.rpathFlagsGlobal = append(d.rpathFlagsGlobal, arg)
				}
			}
		}
	case "INDUCED_DEPS":
		// INDUCED_DEPS(<ext-filter> headers...): the first arg filters
		// the generated output kinds; remaining args are
		// ${ARCADIA_ROOT}-rooted header paths. Strip the prefix so the
		// stored paths are repo-relative.
		if len(v.Args) >= 2 {
			for _, p := range v.Args[1:] {
				p = strings.TrimPrefix(p, "${ARCADIA_ROOT}/")
				d.inducedDeps = append(d.inducedDeps, p)
			}
		}
	default:
		if _, ok := whitelistedMetadataMacros[v.Name]; !ok {
			ThrowFmt("gen: does not yet support macro %q (extend whitelistedMetadataMacros or add a typed Stmt)", v.Name)
		}
	}
}

func appendPyRegister(d *moduleData, name string, explicit bool) {
	d.pyRegister = append(d.pyRegister, name)
	d.pyRegisterExplicit = append(d.pyRegisterExplicit, explicit)

	// Mirror pybuild.py:740-750 — for each dotted PY_REGISTER arg,
	// inject the two -D macro renames so every CC in the same
	// module compiles with them.
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return
	}

	shortname := name[dot+1:]
	mangled := pythonInitSuffix(name)
	d.cFlags = append(d.cFlags,
		"-DPyInit_"+shortname+"=PyInit_"+mangled,
		"-Dinit_module_"+shortname+"=init_module_"+mangled,
	)
}

func parseCPPProtoPlugin(v *UnknownStmt) cppProtoPlugin {
	requiredArgs := 0
	outputSuffixes := 0

	switch v.Name {
	case "CPP_PROTO_PLUGIN0":
		requiredArgs = 2
	case "CPP_PROTO_PLUGIN":
		requiredArgs = 3
		outputSuffixes = 1
	case "CPP_PROTO_PLUGIN2":
		requiredArgs = 4
		outputSuffixes = 2
	default:
		ThrowFmt("gen: internal error: parseCPPProtoPlugin called for %q", v.Name)
	}

	if len(v.Args) < requiredArgs {
		ThrowFmt("gen: %s expects at least %d arguments, got %d", v.Name, requiredArgs, len(v.Args))
	}

	plugin := cppProtoPlugin{
		Name:     v.Args[0],
		ToolPath: v.Args[1],
	}

	tail := 2
	if outputSuffixes > 0 {
		plugin.OutputSuffixes = append(plugin.OutputSuffixes, v.Args[tail:tail+outputSuffixes]...)
		tail += outputSuffixes
	}

	for tail < len(v.Args) {
		switch v.Args[tail] {
		case "DEPS":
			tail++
			for tail < len(v.Args) && v.Args[tail] != "EXTRA_OUT_FLAG" {
				plugin.Deps = append(plugin.Deps, v.Args[tail])
				tail++
			}
		case "EXTRA_OUT_FLAG":
			tail++
			if tail >= len(v.Args) {
				ThrowFmt("gen: %s EXTRA_OUT_FLAG expects exactly 1 argument", v.Name)
			}
			if plugin.ExtraOutFlag != "" {
				ThrowFmt("gen: %s repeated EXTRA_OUT_FLAG", v.Name)
			}
			plugin.ExtraOutFlag = v.Args[tail]
			tail++
		default:
			ThrowFmt("gen: %s got unexpected tail token %q; supported suffixes are DEPS and EXTRA_OUT_FLAG", v.Name, v.Args[tail])
		}
	}

	return plugin
}

func pythonModuleName(modulePath, src string, topLevel bool, namespace *string) string {
	ns := strings.ReplaceAll(modulePath, "/", ".") + "."
	if namespace != nil {
		ns = strings.TrimSuffix(*namespace, ".") + "."
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
	// Single-segment (top-level) module names pass through verbatim; only
	// dotted (namespaced) names get the `<len><seg>` per-segment mangling.
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

// allocatorPeers maps `ALLOCATOR(<name>)` to implicit PEERDIR additions
// per ymake.core.conf:961-1035. nil entries intentionally add no peer
// (FAKE, DEFAULT). ALLOCATOR(SYSTEM) unconditionally adds
// library/cpp/malloc/system per ymake.core.conf:1038-1040 (the MUSL
// gate at lines 954-958 applies to select($ALLOCATOR), not the
// when-clause).
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
	// FAKE / DEFAULT add no peer; SYSTEM unconditionally peers
	// library/cpp/malloc/system per ymake.core.conf:1038-1040.
	"FAKE":    nil,
	"SYSTEM":  {"library/cpp/malloc/system"},
	"DEFAULT": nil,
}

// applyArchiveStmt parses `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)`
// per ymake.core.conf:4142-4145. NAME is required; DONTCOMPRESS maps to
// the archiver's `-p` switch; remaining positional args are inputs.
// Throws on missing/malformed NAME (no sensible default exists).
func applyArchiveStmt(v *UnknownStmt, d *moduleData) {
	var (
		entry      archiveEntry
		seenName   bool
		inNameSlot bool
	)
	for _, a := range v.Args {
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
		ThrowFmt("gen: ARCHIVE(NAME ...) missing value after NAME (line %d)", v.Line)
	}

	if !seenName || entry.Name == "" {
		ThrowFmt("gen: ARCHIVE expects `NAME <output>` (line %d)", v.Line)
	}

	if len(entry.Files) == 0 {
		ThrowFmt("gen: ARCHIVE(NAME %s) has no input files (line %d)", entry.Name, v.Line)
	}

	d.archives = append(d.archives, entry)
}

// applyAllocatorStmt resolves `ALLOCATOR(<name>)` to a PEERDIR addition
// per `build/ymake.core.conf:961-1035`. Multi-arg or unknown allocator
// names throw.
func applyAllocatorStmt(v *UnknownStmt, d *moduleData) {
	if len(v.Args) != 1 {
		ThrowFmt("gen: ALLOCATOR expects exactly 1 argument, got %d (line %d)", len(v.Args), v.Line)
	}

	name := v.Args[0]

	if _, ok := allocatorPeers[name]; !ok {
		ThrowFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name, v.Line)
	}

	// Allocator peers go into the program-default slot (between
	// build/cow/on and musl/full) via defaultProgramPeerdirsFor, NOT
	// d.peerdirs. Appending to d.peerdirs would land mimalloc after
	// musl/full's transitive closure, reversing the reference LD order.
	d.hadAllocator = true
	d.allocatorName = name
}

func isProgramModuleType(name string) bool {
	switch name {
	case "PROGRAM", "PY2_PROGRAM", "PY3_PROGRAM", "PY3_PROGRAM_BIN", "UNITTEST_FOR":
		return true
	}

	return false
}

func isYqlUdfStaticModule(name string) bool {
	switch name {
	case "YQL_UDF_YDB", "YQL_UDF_CONTRIB":
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

// isPyLibraryType returns true for Python library/program module names
// that behave as LIBRARY-shaped modules: they take the regular genModule
// pipeline (peer walk → emit codegen → emit own CC if any → emit AR if
// any CC outputs), distinct from the specialized-types branch.
func isPyLibraryType(name string) bool {
	switch name {
	case "PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

// pyLibraryAutoPythonPeer returns true for Python module types whose
// upstream definition in build/conf/python.conf auto-PEERDIRs
// contrib/libs/python (gated by NO_PYTHON_INCLUDES). Strict subset of
// isPyLibraryType: PY23_NATIVE_LIBRARY is excluded (its PY2/PY3 sub-
// modules inherit from plain LIBRARY, not PY*_LIBRARY).
func pyLibraryAutoPythonPeer(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY", "PY3_PROGRAM_BIN",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

func isPythonModuleType(name string) bool {
	return isPyLibraryType(name) || name == "PY3_PROGRAM_BIN"
}

func isSpecializedLibraryType(name string) bool {
	switch name {
	case "PROTO_LIBRARY",
		"DLL", "SO_PROGRAM", "DYNAMIC_LIBRARY":
		return true
	}

	return false
}

// isResourceContainerType returns true for module types that carry only
// RESOURCE/RESOURCE_FILES/PY_SRCS and no compilable C++ SRCS of their
// own. They take the regular genModule path; len(ccRefs)==0 naturally
// suppresses the AR emission, and the `.global.a` from objcopy outputs
// is emitted via the regular path's globalRefs.
func isResourceContainerType(name string) bool {
	switch name {
	case "PACKAGE", "UNION", "RESOURCES_LIBRARY":
		return true
	}

	return false
}

// buildIfEnv builds the per-instance IF predicate environment from
// DefaultIfEnv (every ARCH_* false), flipping exactly one ISA's bit
// based on instance.Platform.ISA. The returned Environment is caller-
// mutable. ARCH_ARM64 is the upstream alias for ARCH_AARCH64; flipping
// both keeps `IF (ARCH_ARM64 ...)` predicates in agreement.
func buildIfEnv(instance ModuleInstance) Environment {
	env := DefaultIfEnv.Clone()

	for k, v := range instance.Platform.Flags {
		env.SetFromString(k, v)
	}

	if env.Bool("OPENSOURCE") || env.String("OPENSOURCE_PROJECT") == "ymake" || env.String("OPENSOURCE_PROJECT") == "ya" {
		env.SetBool("YA_OPENSOURCE", true)
	}
	if env.Bool("OPENSOURCE") {
		env.SetBool("CATBOOST_OPENSOURCE", true)
	}

	switch instance.Platform.ISA {
	case ISAX8664:
		env.SetBool("ARCH_X86_64", true)
	case ISAAArch64:
		env.SetBool("ARCH_AARCH64", true)
		env.SetBool("ARCH_ARM64", true)
	}

	useRuntime := instance.Platform.Flags["USE_ARCADIA_COMPILER_RUNTIME"]
	env.SetBool("USE_ARCADIA_COMPILER_RUNTIME", useRuntime != "no")
	env.SetString("COMPILER_VERSION", instance.Platform.ClangVer)
	env.SetString("BUILD_TYPE", strings.ToUpper(instance.Platform.BuildType))

	return env
}

func expandConfigPaths(paths []string, env Environment) []string {
	out := make([]string, 0, len(paths))

	for _, path := range paths {
		out = append(out, expandConfigString(path, env))
	}

	return out
}

func expandConfigVFSPaths(paths []string, env Environment) []VFS {
	out := make([]VFS, 0, len(paths))

	for _, path := range paths {
		out = append(out, parseModulePathVFS(expandConfigString(path, env)))
	}

	return out
}

func parseModulePathVFS(path string) VFS {
	if v, ok := ParseVFS(path); ok {
		return v
	}

	return Source(path)
}

func expandConfigString(s string, env Environment) string {
	s = strings.ReplaceAll(s, "${COMPILER_VERSION}", env.String("COMPILER_VERSION"))
	s = strings.ReplaceAll(s, "${ARCADIA_BUILD_ROOT}", "$(B)")
	s = strings.ReplaceAll(s, "${ARCADIA_ROOT}", "$(S)")
	s = strings.ReplaceAll(s, "${MODDIR}", env.String("MODDIR"))

	return s
}

func expandStmtToken(s string, env Environment) string {
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
			if isExpandVarName(name) && env.HasBinding(name) {
				s = env.String(name)
			}
		}
		s = expandEmbeddedDollarVars(s, env)

		for {
			start := strings.Index(s, "${")
			if start < 0 {
				break
			}
			end := strings.IndexByte(s[start+2:], '}')
			if end < 0 {
				break
			}
			end += start + 2
			name := s[start+2 : end]
			if !isExpandVarName(name) || !env.HasBinding(name) {
				break
			}
			s = s[:start] + env.String(name) + s[end+1:]
		}

		s = expandConfigString(s, env)
		if s == prev {
			break
		}
	}
	return s
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
		if !env.HasBinding(name) {
			b.WriteString(s[i:j])
			i = j
			continue
		}

		b.WriteString(env.String(name))
		i = j
		changed = true
	}

	if !changed {
		return s
	}

	return b.String()
}

func expandStmtTokens(items []string, env Environment) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		expanded := expandStmtToken(item, env)
		if fullVarRef(item) && expanded == item {
			continue
		}
		if expanded == "" || expanded == "no" {
			if fullVarRef(item) || (fullDollarVarRef(item) && env.HasBinding(item[1:])) {
				continue
			}
		}

		fields := []string{expanded}
		if fullVarRef(item) || (fullDollarVarRef(item) && env.HasBinding(item[1:])) {
			fields = strings.Fields(expanded)
		}
		for _, field := range fields {
			if field == "" {
				continue
			}
			out = append(out, field)
		}
	}
	return out
}

func fullVarRef(s string) bool {
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && isExpandVarName(s[2:len(s)-1])
}

func fullDollarVarRef(s string) bool {
	return strings.HasPrefix(s, "$") && !strings.HasPrefix(s, "${") && isExpandVarName(s[1:])
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

func expandListVars(items []string, env Environment) []string {
	out := make([]string, 0, len(items))

	for _, item := range items {
		if strings.HasPrefix(item, "${") && strings.HasSuffix(item, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(item, "${"), "}")
			if !env.HasBinding(name) {
				continue
			}

			value := env.String(name)
			if value == "" || value == "no" {
				continue
			}

			out = append(out, strings.Fields(value)...)

			continue
		}

		out = append(out, item)
	}

	return out
}

func applyAllPySrcs(fs *FS, modulePath string, v *UnknownStmt, d *moduleData) {
	dirs := []string{"."}
	noTestFiles := false

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
		case "TOP_LEVEL":
			d.pyTopLevel = true
		case "NAMESPACE":
			i++
			if i >= len(v.Args) {
				ThrowFmt("ALL_PY_SRCS NAMESPACE expects a value")
			}
			d.pyNamespace = stringPtr(v.Args[i])
		case "RECURSIVE":
		case "NO_TEST_FILES":
			noTestFiles = true
		default:
			dirs = append(dirs, a)
		}
	}

	if len(dirs) > 1 {
		dirs = dirs[1:]
	}

	var files []string
	moduleRootRel := modulePath
	for _, dir := range dirs {
		walkRoot := filepath.ToSlash(filepath.Join(moduleRootRel, dir))

		fs.Walk(walkRoot, func(rel string, isDir bool) {
			if isDir {
				return
			}
			if filepath.Ext(rel) != ".py" {
				return
			}

			base := filepath.Base(rel)
			if noTestFiles && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) {
				return
			}

			// rel is module-root-rooted (e.g. "modulePath/subdir/x.py");
			// the consumer wants module-relative ("subdir/x.py").
			files = append(files, strings.TrimPrefix(rel, moduleRootRel+"/"))
		})
	}

	sort.Strings(files)
	d.pySrcs = append(d.pySrcs, files...)
	if len(files) > 0 {
		d.pySrcGroups = append(d.pySrcGroups, pySrcGroup{
			Srcs:      files,
			TopLevel:  d.pyTopLevel,
			Namespace: d.pyNamespace,
		})
	}
}

type moduleTypeCacheKey struct {
	Path     string
	Kind     ModuleKind
	Platform *Platform
}

type moduleTypeInfo struct {
	Name        string
	ExcludeTags map[string]bool
}

func moduleInfoForInstance(ctx *genCtx, instance ModuleInstance) moduleTypeInfo {
	if ctx.moduleTypeCache == nil {
		ctx.moduleTypeCache = make(map[moduleTypeCacheKey]moduleTypeInfo)
	}

	key := moduleTypeCacheKey{
		Path:     instance.Path,
		Kind:     instance.Kind,
		Platform: instance.Platform,
	}
	if info, ok := ctx.moduleTypeCache[key]; ok {
		return info
	}

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path, "ya.make")
	mf := Throw2(ParseFile(ctx.fs, yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(ctx.fs, instance.Path, instance.Kind, mf.Stmts, env)
	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, d.moduleStmt.Name, d.conflictMod.Name)
	}
	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

	info := moduleTypeInfo{Name: d.moduleStmt.Name}
	if len(d.excludeTags) > 0 {
		info.ExcludeTags = make(map[string]bool, len(d.excludeTags))
		for k, v := range d.excludeTags {
			info.ExcludeTags[k] = v
		}
	}

	ctx.moduleTypeCache[key] = info

	return info
}

func moduleTypeForInstance(ctx *genCtx, instance ModuleInstance) string {
	return moduleInfoForInstance(ctx, instance).Name
}

func peerLanguageFor(ctx *genCtx, parent ModuleInstance, parentModuleName, peerPath string) Language {
	if !peerYaMakeExists(ctx.fs, peerPath) {
		return LangCPP
	}

	peerSeed := ModuleInstance{
		Path:     peerPath,
		Kind:     KindLib,
		Language: LangCPP,
		Platform: parent.Platform,
	}

	peerInfo := moduleInfoForInstance(ctx, peerSeed)
	if peerInfo.Name != "PROTO_LIBRARY" {
		return LangCPP
	}

	if isPythonModuleType(parentModuleName) {
		return LangPy
	}

	if parentModuleName == "PROTO_LIBRARY" && parent.Language == LangPy {
		return LangPy
	}

	return LangCPP
}

// derivePeerInstance builds the peer's ModuleInstance. Peer language is
// explicit rather than inherited: only Python modules entering a
// PROTO_LIBRARY (and py-addressed PROTO_LIBRARY -> PROTO_LIBRARY hops)
// use LangPy; every other peer walk stays LangCPP. Platform (which
// carries PIC) flows from the parent. FlagSet stays empty here — macro
// overlay (NO_LIBC / NO_UTIL / ...) happens inside genModule when the
// peer's ya.make is parsed.
func derivePeerInstance(ctx *genCtx, parent ModuleInstance, d *moduleData, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Kind:     KindLib,
		Language: peerLanguageFor(ctx, parent, d.moduleStmt.Name, peerPath),
		Platform: parent.Platform,
	}
}
