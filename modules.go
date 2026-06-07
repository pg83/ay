package main

import (
	"fmt"
	"path"
	"path/filepath"
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
	pySrcs               []string
	pySrcGroups          []pySrcGroup
	pyGeneratedSrcs      map[string][]VFS
	pyPyiResources       []resourceEntry
	pyBuildNoPYC         bool
	pyBuildNoPY          bool
	pyTopLevel           bool
	noExtendedPySearch   bool
	enumSrcs             []*GenerateEnumSerializationStmt
	peerdirs             []string
	joinSrcs             []*JoinSrcsStmt
	addIncl              []VFS
	addInclGlobal        []VFS
	addInclOneLevel      []VFS
	addInclUserGlobal    []VFS
	cfAddIncl            []VFS
	cfAddInclGlobal      []VFS
	cythonAddIncl        []VFS
	asmAddIncl           []VFS
	protoAddInclGlobal   []VFS
	unhandledMacros      map[string][]string
	llvmBc               []*llvmBcStmt
	cFlags               []ARG
	cFlagsGlobal         []ARG
	cxxFlags             []ARG
	cxxFlagsGlobal       []ARG
	cOnlyFlags           []ARG
	cOnlyFlagsGlobal     []ARG
	sFlags               []ARG
	protocFlags          []ARG
	flatcFlags           []ARG
	ldFlags              []ARG
	rpathFlagsGlobal     []ARG
	objAddLibsGlobal     []ARG
	srcDir               *string
	flags                FlagSet
	hadAllocator         bool
	allocatorName        string
	muslLite             bool
	muslEnabled          bool
	splitDwarf           bool
	noPythonIncl         bool
	noImportTracing      bool
	usePython3           bool
	useCommonGoogleAPIs  bool
	moduleScopeCFlags    []ARG
	pythonSQLite3        bool
	pyNamespace          *string
	protoNamespace       *string
	protoNamespaceGlobal bool
	noMypy               bool
	optimizePyProtos     bool
	optimizePyProtosSet  bool
	cppProtoPlugins      []cppProtoPlugin
	excludeTags          map[string]bool
	dynamicLibraryFrom   []string
	exportsScript        *string
	ldPlugins            []string
	arPlugin             *string

	perSrcCFlags map[string][]ARG

	defaultVars map[string]string

	defaultVarOrder    []string
	configureFiles     []*ConfigureFileStmt
	createBuildInfoFor *string
	antlr4Grammars     []antlr4GrammarInfo
	antlrRuns          []antlrRunInfo
	runPrograms        []*RunProgramStmt
	runPython          []*RunPythonStmt
	checkConfigHeaders []string
	cythonCpp          []*CythonStmt

	cythonNumpyBeforeInclude bool
	swigC                    []swigSrc
	bisonGenExt              string
	grpc                     bool
	yaConfJSON               []string
	allPySrcs                []*UnknownStmt

	archives []archiveEntry

	prOutputProducer map[string]NodeRef

	prOutputInputs map[string][]VFS

	copyFiles []copyFileEntry

	copyFileAutoOutputs map[string]copyFileEntry
	flatSrcs            map[string]struct{}
	resources           []resourceEntry

	pyMain *string

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

	noCheckImports []string

	noCheckImportsDisabled bool

	pyRegister []string

	pyRegisterExplicit []bool

	simdSrcs []simdSrc

	ragel6Flags []ARG
	conflictMod *ModuleStmt

	inducedDeps []string

	setVars map[string]string
}

// perSrcCFlagsFor / flatSrc gate the sparse per-source attribute maps on len, so
// modules with no SRC-level CFLAGS and no flat-output markers (the vast majority)
// skip the probe. Identical to a direct probe — an empty/nil map yields not-found.
func (d *moduleData) perSrcCFlagsFor(src string) *[]ARG {
	if len(d.perSrcCFlags) == 0 {
		return nil
	}

	if v, ok := d.perSrcCFlags[src]; ok {
		return &v
	}

	return nil
}

func (d *moduleData) flatSrc(src string) bool {
	if len(d.flatSrcs) == 0 {
		return false
	}

	_, ok := d.flatSrcs[src]

	return ok
}

func muslCFlags(on bool) []ARG {
	if on {
		return []ARG{argDMusl}
	}

	return nil
}

type resourceEntry struct {
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

type pySrcGroup struct {
	Srcs      []string
	TopLevel  bool
	Namespace *string
}

type archiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

type copyFileEntry struct {
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

type antlr4GrammarInfo struct {
	IsSplit        bool
	Lexer          string
	Parser         string
	Grammar        string
	Options        []string
	Visitor        bool
	Listener       bool
	OutputIncludes []string
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
		ThrowFmt("gen: COPY_FILE at line %d expects at least source and destination, got %d args", line, len(args))
	}

	// COPY_FILE(TEXT src dst …) semantically substitutes src's content into dst
	// — consumers of dst must depend on src (and src's transitive #include
	// closure) for any change to retrigger them. The closure plumbing matches
	// COPY(WITH_CONTEXT …), so route TEXT through the same flag.
	entry := copyFileEntry{
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

func sourceInputVFS(fs FS, modulePath string, path string) *VFS {
	if vfs := moduleRootedVFS(modulePath, path); vfs != nil {
		return vfs
	}

	clean := filepath.ToSlash(filepath.Clean(path))

	if clean == "." || clean == "" {
		return vfsPtr(Source(modulePath))
	}

	if clean == modulePath || strings.HasPrefix(clean, modulePath+"/") {
		return vfsPtr(Source(clean))
	}

	if fs != nil {
		moduleRel := filepath.ToSlash(filepath.Clean(modulePath + "/" + clean))

		if fs.IsFile(dirKey(modulePath), clean) {
			return vfsPtr(Source(moduleRel))
		}

		if fs.IsFile(srcRootVFS, clean) {
			return vfsPtr(Source(clean))
		}
	}

	return nil
}

func copyFileInputVFS(fs FS, modulePath string, src string) VFS {
	if vfs := sourceInputVFS(fs, modulePath, src); vfs != nil {
		return *vfs
	}

	return Source(filepath.ToSlash(filepath.Clean(modulePath + "/" + src)))
}

func moduleRootedVFS(modulePath string, path string) *VFS {
	if vfsHasPrefix(path) {
		return vfsPtr(Intern(path))
	}

	switch {
	case strings.HasPrefix(path, "${ARCADIA_ROOT}/"):
		return vfsPtr(Source(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_ROOT}/")))))
	case strings.HasPrefix(path, "${CURDIR}/"):
		return vfsPtr(Source(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${CURDIR}/")))))
	case strings.HasPrefix(path, "${ARCADIA_BUILD_ROOT}/"):
		return vfsPtr(Build(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "${ARCADIA_BUILD_ROOT}/")))))
	case strings.HasPrefix(path, "${BINDIR}/"):
		return vfsPtr(Build(filepath.ToSlash(filepath.Clean(modulePath + "/" + strings.TrimPrefix(path, "${BINDIR}/")))))
	default:
		return nil
	}
}

func copyFileOutputVFS(modulePath string, dst string) VFS {
	if vfs := moduleRootedVFS(modulePath, dst); vfs != nil {
		return *vfs
	}

	return Build(filepath.ToSlash(filepath.Clean(modulePath + "/" + dst)))
}

func copyFileIncludeTarget(modulePath string, target string) string {
	if vfsHasPrefix(target) {
		return Intern(target).Rel()
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

func collectModule(pm *includeParserManager, dd *deDuper, modulePath string, kind ModuleKind, stmts []Stmt, env Environment) *moduleData {
	fs := pm.fs

	env.SetString(envMODDIR, modulePath)
	env.SetString(envCURDIR, "${ARCADIA_ROOT}/"+modulePath)
	env.SetString(envBINDIR, "${ARCADIA_BUILD_ROOT}/"+modulePath)

	d := &moduleData{
		pythonSQLite3: true,
		bisonGenExt:   ".cpp",
	}

	collectStmts(modulePath, kind, stmts, env, d)

	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
	// CF-generated include dirs join UserGlobal in the same deferred step as
	// addInclGlobal, matching upstream ymake where addincl;output on CONFIGURE_FILE
	// outputs is resolved after all explicit ADDINCL statements.
	d.addInclUserGlobal = append(d.addInclUserGlobal, d.cfAddInclGlobal...)
	d.cfAddIncl = nil
	d.cfAddInclGlobal = nil
	filterInvalidAddIncl(fs, d)

	if kind == KindLib {
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
	d.muslEnabled = env.Bool(envMUSL)
	// ENABLE(NO_STRIP) and BUILD_TYPE-driven STRIP_FLAG suppression
	// (ymake.core.conf:2669 — when ($STRIP == "yes" && $NO_STRIP != "yes"))
	// both clear -Wl,--strip-all. Track the effective NO_STRIP env value
	// here so the LD emitter can honour it without re-reading env.
	d.noStrip = env.Bool(envNO_STRIP)

	if d.muslLite {
		d.flags.NoUtil = true
	}

	if env.Bool(envPY3_PROTO) {
		d.usePython3 = true
	}

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)

	cflagPrefix := append(muslCFlags(d.muslEnabled && !effectiveNoPlatform(d.flags)), sseBaseCFlags(env.Bool(envARCH_X86_64))...)
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

	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary {
		if !env.Bool(envPY3_PROTO) {
			d.peerdirs = append(d.peerdirs, "contrib/libs/protobuf")
		}

		if !d.optimizePyProtosSet {
			d.optimizePyProtos = true
		}
	}

	if len(d.pyPyiResources) > 0 || len(d.pySrcs) > 0 || len(d.pyRegister) > 0 {
		ensureResourcePeer(modulePath, d)
	}

	return d
}

func appendGlobalSrcEvent(d *moduleData, src string) {
	d.globalSrcs = append(d.globalSrcs, src)
}

func appendGlobalSrcGroup(d *moduleData, srcs []string) {
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

func filterInvalidAddIncl(fs FS, d *moduleData) {
	d.addIncl = filterExistingSourceDirs(fs, d.addIncl)
	d.addInclGlobal = filterExistingSourceDirs(fs, d.addInclGlobal)
	d.cythonAddIncl = filterExistingSourceDirs(fs, d.cythonAddIncl)
	d.asmAddIncl = filterExistingSourceDirs(fs, d.asmAddIncl)

	// Rebuild addInclUserGlobal in declaration order, keeping only paths that
	// survived the addInclGlobal filter (for GLOBAL paths) or are in
	// addInclOneLevel (ONE_LEVEL paths, which are never filtered).
	if len(d.addInclUserGlobal) > 0 {
		validGlobal := make(map[VFS]struct{}, len(d.addInclGlobal))

		for _, p := range d.addInclGlobal {
			validGlobal[p] = struct{}{}
		}

		validOneLevel := make(map[VFS]struct{}, len(d.addInclOneLevel))

		for _, p := range d.addInclOneLevel {
			validOneLevel[p] = struct{}{}
		}

		out := d.addInclUserGlobal[:0]

		for _, p := range d.addInclUserGlobal {
			if _, ok := validGlobal[p]; ok {
				out = append(out, p)
			} else if _, ok := validOneLevel[p]; ok {
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
		if shouldCheckSourceDir(path) && !fs.IsDir(path, "") {
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

	if path.Rel() == "" {
		return false
	}

	if strings.Contains(path.Rel(), "$") {
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

	d.usePython3 = true

	d.moduleScopeCFlags = append(d.moduleScopeCFlags, argDusePython3)

	d.addInclGlobal = append(d.addInclGlobal, pythonIncludeDir)
	d.addInclUserGlobal = append(d.addInclUserGlobal, pythonIncludeDir)
	d.addIncl = append(d.addIncl, pythonIncludeDir)

	if modulePath == "library/python/runtime_py3" {
		d.addIncl = append(d.addIncl, bldLibraryPythonRuntimePy3)
	}
}

func applyBuildInfoAddIncl(modulePath string, d *moduleData) {
	if d.createBuildInfoFor == nil {
		return
	}

	biDir := Build(modulePath)
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

func collectStmts(modulePath string, kind ModuleKind, stmts []Stmt, env Environment, d *moduleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			if v.Name == tokPy3Program && kind == KindBin {
				d.peerdirs = append([]string{modulePath}, d.peerdirs...)
			}

			if v.Name == tokUnittestFor {
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

			if v.Name == tokPy3Program && kind == KindLib {
				d.programPairedLib = true
			}
		case *SrcsStmt:

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

			addInclNext := false

			for _, p := range expandStmtTokens(v.Paths, env) {
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

			value := expandScalarVarRef(v.Value, env)
			env.SetFromString(v.NameEnv, value)

			if d.setVars == nil {
				d.setVars = map[string]string{}
			}

			d.setVars[v.Name] = value

			if v.Name == "RAGEL6_FLAGS" {
				d.ragel6Flags = []ARG{internArg(value)}
			}
		case *EndStmt:

		case *JoinSrcsStmt:
			expanded := *v
			expanded.Sources = expandStmtTokens(v.Sources, env)
			d.joinSrcs = append(d.joinSrcs, &expanded)
		case *AddInclStmt:

			d.addInclGlobal = append(d.addInclGlobal, expandConfigVFSPaths(v.GlobalPaths, env)...)
			d.addInclOneLevel = append(d.addInclOneLevel, expandConfigVFSPaths(v.OneLevelPaths, env)...)
			d.addInclUserGlobal = append(d.addInclUserGlobal, expandConfigVFSPaths(v.UserGlobalPaths, env)...)
			d.addIncl = append(d.addIncl, expandConfigVFSPaths(v.AllPaths, env)...)
			d.cythonAddIncl = append(d.cythonAddIncl, expandConfigVFSPaths(v.CythonPaths, env)...)
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
			d.protoAddInclGlobal = append(d.protoAddInclGlobal, expandConfigVFSPaths(v.ProtoGlobalPaths, env)...)
		case *CFlagsStmt:

			d.cFlagsGlobal = append(d.cFlagsGlobal, internArgs(expandStmtTokens(v.GlobalFlags, env))...)
			d.cFlags = append(d.cFlags, internArgs(expandStmtTokens(v.OwnFlags, env))...)
		case *CXXFlagsStmt:

			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, internArgs(expandStmtTokens(v.GlobalFlags, env))...)
			d.cxxFlags = append(d.cxxFlags, internArgs(expandStmtTokens(v.OwnFlags, env))...)
		case *CONLYFlagsStmt:

			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, internArgs(expandStmtTokens(v.GlobalFlags, env))...)
			d.cOnlyFlags = append(d.cOnlyFlags, internArgs(expandStmtTokens(v.OwnFlags, env))...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, internArgs(expandStmtTokens(v.Flags, env))...)
		case *SrcDirStmt:

			d.srcDir = &v.Dir
		case *GlobalSrcsStmt:
			appendGlobalSrcGroup(d, expandStmtTokens(v.Sources, env))
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
				d.peerdirs = append(d.peerdirs, enumSerPeer)
			}
		case *DefaultVarStmt:

			if d.defaultVars == nil {
				d.defaultVars = map[string]string{}
			}

			if _, exists := d.defaultVars[v.VarName]; !exists {
				d.defaultVars[v.VarName] = expandScalarVarRef(v.Value, env)
				d.defaultVarOrder = append(d.defaultVarOrder, v.VarName)
			}

			env.SetDefaultString(v.NameEnv, expandScalarVarRef(v.Value, env))
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
			ensureResourcePeer(modulePath, d)

			for i, pair := range v.Pairs {
				// Upstream's TObjCopyResourcePacker stores RESOURCE() pairs raw
				// (RAW ${BINDIR}/... form, not expanded), so the objcopy_<hash>
				// is computed against ${BINDIR}/<name>. Pre-expanding here
				// drifts the hash vs REF (e.g. yt provider yql_yt_op_settings).
				// RESOURCE_FILES already stores raw — keep them aligned.
				d.resources = append(d.resources, resourceEntry{
					Path:      pair.Path,
					Key:       pair.Key,
					EndsBatch: i == len(v.Pairs)-1,
				})
			}
		case *ResourceFilesStmt:
			ensureResourcePeer(modulePath, d)

			expanded := expandResourceFiles(v.Args)

			for i, e := range expanded {
				if i == len(expanded)-1 {
					e.EndsBatch = true
				}

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
	if stmt.Name == tokPy3Program && kind == KindLib {
		out := *stmt
		out.Name = tokPy3Library
		return &out
	}

	return stmt
}

func addGeneratedHeaderInclude(modulePath, dst string, d *moduleData) {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.Rel())))
	rel := dir

	if dir != "." && dir != "" {
		rel = filepath.ToSlash(filepath.Clean(dir))
	} else {
		rel = modulePath
	}

	include := Build(rel)
	d.addIncl = append(d.addIncl, include)
	d.addInclGlobal = append(d.addInclGlobal, include)
	d.addInclUserGlobal = append(d.addInclUserGlobal, include)
}

func addGeneratedHeaderIncludeCF(modulePath, dst string, d *moduleData) {
	outVFS := copyFileOutputVFS(modulePath, dst)
	dir := filepath.ToSlash(filepath.Clean(filepath.Dir(outVFS.Rel())))
	rel := dir

	if dir != "." && dir != "" {
		rel = filepath.ToSlash(filepath.Clean(dir))
	} else {
		rel = modulePath
	}

	include := Build(rel)
	d.cfAddIncl = append(d.cfAddIncl, include)
	d.cfAddInclGlobal = append(d.cfAddInclGlobal, include)
}

func addGeneratedOwnHeaderInclude(modulePath, dst string, d *moduleData) {
	addGeneratedHeaderInclude(modulePath, dst, d)
}

func applyUnknownStmt(modulePath string, v *UnknownStmt, d *moduleData, env Environment) {
	// recordHandledMacro fires only when a typed case handles the macro —
	// we deferr it and the default branch flips `handled = false` to
	// suppress it. Logging service-keyword args of macros gen does NOT
	// model (LICENSE, VERSION, …) would only generate noise — the right
	// list for those is the one upstream's own macro parser implements,
	// not ours.
	handled := true

	defer func() {
		if handled {
			recordHandledMacro(v.Name.String(), v.Args)
		}
	}()

	switch v.Name {
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
		env.SetString(envCLANG_BC_ROOT, env.String(envCLANG16_RESOURCE_GLOBAL))
		env.SetString(envLLVM_LLC_TOOL, "contrib/libs/llvm16/tools/llc")
	case tokUseLlvmBc18:
		env.SetString(envCLANG_BC_ROOT, env.String(envCLANG18_RESOURCE_GLOBAL))
		env.SetString(envLLVM_LLC_TOOL, "contrib/libs/llvm18/tools/llc")
	case tokUseLlvmBc20:
		env.SetString(envCLANG_BC_ROOT, env.String(envCLANG20_RESOURCE_GLOBAL))
		env.SetString(envLLVM_LLC_TOOL, "contrib/libs/llvm20/tools/llc")
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
			switch v.Args[i] {
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
		if env.String(envCLANG_BC_ROOT) == "" || env.String(envLLVM_LLC_TOOL) == "" {
			ThrowFmt("LLVM_BC requires USE_LLVM_BC16/18/20 before invocation")
		}

		stmt := &llvmBcStmt{ClangBCRoot: env.String(envCLANG_BC_ROOT)}
		i := 0

		for i < len(v.Args) {
			switch v.Args[i] {
			case "NAME":
				if i+1 >= len(v.Args) {
					ThrowFmt("LLVM_BC NAME expects a value")
				}

				stmt.Name = v.Args[i+1]
				i += 2
			case "SUFFIX":
				if i+1 >= len(v.Args) {
					ThrowFmt("LLVM_BC SUFFIX expects a value")
				}

				stmt.Suffix = expandStmtToken(v.Args[i+1], env)
				i += 2
			case "SYMBOLS":
				i++

				for i < len(v.Args) && !isLlvmBcKeyword(v.Args[i]) {
					stmt.Symbols = append(stmt.Symbols, v.Args[i])
					i++
				}
			case "GENERATE_MACHINE_CODE":
				stmt.GenerateMachineCode = true
				i++
			case "NO_COMPILE":
				stmt.NoCompile = true
				i++
			default:
				stmt.Sources = append(stmt.Sources, v.Args[i])
				i++
			}
		}

		if stmt.Name == "" {
			ThrowFmt("LLVM_BC: NAME keyword is required (got args %v)", v.Args)
		}

		d.llvmBc = append(d.llvmBc, stmt)

	case tokMavenGroupId:

	case tokCheckConfigH:
		if len(v.Args) != 1 {
			ThrowFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, expandStmtToken(v.Args[0], env))
	case tokBuildwithCythonCpp:
		if len(v.Args) == 0 {
			ThrowFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     expandStmtToken(v.Args[0], env),
			Options: expandStmtTokens(v.Args[1:], env),
		})
		d.cythonNumpyBeforeInclude = true
	case tokBuildwithCythonC:
		if len(v.Args) == 0 {
			ThrowFmt("BUILDWITH_CYTHON_C expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{
			Src:     expandStmtToken(v.Args[0], env),
			Options: expandStmtTokens(v.Args[1:], env),
			CMode:   true,
		})
		d.cythonNumpyBeforeInclude = true
	case tokBisonGenC:
		d.bisonGenExt = ".c"
	case tokBisonGenCpp:
		d.bisonGenExt = ".cpp"
	case tokGrpc:
		d.grpc = true
		d.peerdirs = append(d.peerdirs, "contrib/libs/grpc")
	case tokPyNamespace:
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_NAMESPACE expects exactly 1 argument, got %d", len(v.Args))
		}

		d.pyNamespace = stringPtr(expandStmtToken(v.Args[0], env))
	case tokYqlLastAbiVersion:
		if len(v.Args) != 0 {
			ThrowFmt("YQL_LAST_ABI_VERSION expects exactly 0 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags, argDuseCurrentUdfAbiVersion)
	case tokYqlAbiVersion:
		if len(v.Args) != 3 {
			ThrowFmt("YQL_ABI_VERSION expects exactly 3 arguments, got %d", len(v.Args))
		}

		d.cxxFlags = append(d.cxxFlags,
			internArg("-DUDF_ABI_VERSION_MAJOR="+v.Args[0]),
			internArg("-DUDF_ABI_VERSION_MINOR="+v.Args[1]),
			internArg("-DUDF_ABI_VERSION_PATCH="+v.Args[2]),
		)
	case tokProtocFatalWarnings:
		if len(v.Args) != 0 {
			ThrowFmt("PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got %d", len(v.Args))
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
		d.peerdirs = append([]string{googleapisPeer}, d.peerdirs...)
	case tokFlatcFlags:
		d.flatcFlags = append(d.flatcFlags, internArgs(expandListVars(v.Args, env))...)
	case tokCopyFile, tokCopyFileWithContext:
		args := expandListVars(v.Args, env)

		for i := range args {
			args[i] = expandConfigString(args[i], env)
		}

		entry := parseCopyFileEntry(args, v.Name == tokCopyFileWithContext, v.Line)
		d.copyFiles = append(d.copyFiles, entry)

		if entry.Auto {
			dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
			prefix := modulePath + "/"

			if strings.HasPrefix(dstVFS.Rel(), prefix) {
				dstRel := strings.TrimPrefix(dstVFS.Rel(), prefix)

				if isSourceEligibleForCopyAuto(dstRel) && !flagsContain(d.srcs, dstRel) {
					d.srcs = append(d.srcs, dstRel)
				}

				if d.copyFileAutoOutputs == nil {
					d.copyFileAutoOutputs = make(map[string]copyFileEntry)
				}

				d.copyFileAutoOutputs[dstRel] = entry
			}
		}
	case tokCopy:
		for _, entry := range parseCopyEntries(expandListVars(v.Args, env), v.Line) {
			d.copyFiles = append(d.copyFiles, entry)

			if entry.Auto {
				dstVFS := copyFileOutputVFS(modulePath, entry.Dst)
				prefix := modulePath + "/"

				if strings.HasPrefix(dstVFS.Rel(), prefix) {
					dstRel := strings.TrimPrefix(dstVFS.Rel(), prefix)

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
	case tokProtoNamespace:
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

		if d.protoNamespaceGlobal || (d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary) {
			d.addInclGlobal = append(d.addInclGlobal, protoBuildRoot)
			d.addInclUserGlobal = append(d.addInclUserGlobal, protoBuildRoot)
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
			d.excludeTags = make(map[string]bool)
		}

		for _, arg := range v.Args {
			switch arg {
			case "GO_PROTO", "JAVA_PROTO":
				// known multimodule submodule tags
			}

			d.excludeTags[arg] = true
		}
	case tokYaConfJson:
		if len(v.Args) != 1 {
			ThrowFmt("YA_CONF_JSON expects exactly 1 argument, got %d", len(v.Args))
		}

		d.yaConfJSON = append(d.yaConfJSON, expandStmtToken(v.Args[0], env))
	case tokAllocator:
		applyAllocatorStmt(v, d)
	case tokArchive:

		applyArchiveStmt(v, d)
	case tokEnable:
		// upstream pybuild plugins translate ENABLE(X) into
		// `unit.set([X, 'yes'])` — a plain boolean env var, picked up by
		// `when ($X == "yes") { … }` clauses. Args are user-defined flag
		// NAMES (not structural keywords), so they're excluded from the
		// strict service-keyword check in recordHandledMacro via
		// macrosAcceptingUserFlags. The cases below keep the few flags
		// whose ENABLE has a direct module-data side-effect.
		for _, a := range v.Args {
			env.SetBool(internEnv(a), true)

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
		for _, a := range v.Args {
			env.SetBool(internEnv(a), false)

			if a == "PYTHON_SQLITE3" {
				d.pythonSQLite3 = false
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
				d.perSrcCFlags = map[string][]ARG{}
			}

			extras := internArgs(expandStmtTokens(v.Args[1:], env))
			d.perSrcCFlags[filename] = append(d.perSrcCFlags[filename], extras...)
		}
	case tokSrcCNoLto:

		if len(v.Args) != 1 {
			ThrowFmt("gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got %d at line %d", len(v.Args), v.Line)
		}

		filename := v.Args[0]
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}
	case tokSrcCAvx, tokSrcCAvx2, tokSrcCAvx512, tokSrcCAmx, tokSrcCSse2, tokSrcCSse3, tokSrcCSsse3,
		tokSrcCSse4, tokSrcCSse41, tokSrcCXop:

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
	case tokLdPlugin:

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case tokArPlugin:

		if len(v.Args) != 1 {
			ThrowFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}

		d.arPlugin = stringPtr(v.Args[0] + ".pyplugin")
	case tokDynamicLibraryFrom:
		if len(v.Args) == 0 {
			ThrowFmt("gen: DYNAMIC_LIBRARY_FROM expects at least 1 argument")
		}

		d.dynamicLibraryFrom = append(d.dynamicLibraryFrom, v.Args...)
		d.peerdirs = append(d.peerdirs, v.Args...)
	case tokExportsScript:
		if len(v.Args) != 1 {
			ThrowFmt("gen: EXPORTS_SCRIPT expects exactly 1 argument, got %d", len(v.Args))
		}

		d.exportsScript = stringPtr(v.Args[0])
	case tokExtralibs:
		for _, arg := range v.Args {
			lib := arg

			if !strings.HasPrefix(lib, "-") {
				lib = "-l" + lib
			}

			d.objAddLibsGlobal = append(d.objAddLibsGlobal, internArg(lib))
		}
	case tokUsePython3:

		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)

		d.usePython3 = true
	case tokPySrcs:

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
				d.pyMain = stringPtr(ns + modName)
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
	case tokAllPySrcs:
		d.allPySrcs = append(d.allPySrcs, v)
	case tokPyMain:

		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}

		arg := strings.ReplaceAll(v.Args[0], "/", ".")

		if !strings.Contains(arg, ":") {
			arg += ":main"
		}

		d.pyMain = stringPtr(arg)
	case tokPyConstructor:

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
	case tokNoCheckImports:

		if len(v.Args) > 0 {
			d.noCheckImports = append(d.noCheckImports, v.Args...)
		} else {
			d.noCheckImportsDisabled = true
		}
	case tokCppProtoPlugin0, tokCppProtoPlugin, tokCppProtoPlugin2:
		plugin := parseCPPProtoPlugin(v)
		d.cppProtoPlugins = append(d.cppProtoPlugins, plugin)
		d.peerdirs = append(d.peerdirs, plugin.Deps...)
	case tokPyRegister:

		for _, name := range v.Args {
			appendPyRegister(d, name, true)
		}
	case tokSetAppend:

		if len(v.Args) >= 2 {
			switch v.Args[0] {
			case "SFLAGS":
				d.sFlags = append(d.sFlags, internArgs(expandStmtTokens(v.Args[1:], env))...)
			case "_PROTOC_FLAGS":
				d.protocFlags = append(d.protocFlags, internArgs(expandStmtTokens(v.Args[1:], env))...)
			case "RPATH_GLOBAL":
				for _, arg := range expandStmtTokens(v.Args[1:], env) {
					arg = strings.ReplaceAll(arg, `${"$"}`, "$")
					d.rpathFlagsGlobal = append(d.rpathFlagsGlobal, internArg(arg))
				}
			}
		}
	case tokInducedDeps:

		if len(v.Args) >= 2 {
			for _, p := range v.Args[1:] {
				p = strings.TrimPrefix(p, "${ARCADIA_ROOT}/")
				d.inducedDeps = append(d.inducedDeps, p)
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

		if _, ok := acknowledgedMacros[v.Name.String()]; !ok {
			ThrowFmt("gen: macro %q not modelled — implement its upstream semantics (see yatool/build/conf, yatool/build/ymake.core.conf)", v.Name.String())
		}

		if d.unhandledMacros == nil {
			d.unhandledMacros = map[string][]string{}
		}

		d.unhandledMacros[v.Name.String()] = append(d.unhandledMacros[v.Name.String()], expandStmtTokens(v.Args, env)...)
		recordIgnoredMacro(v.Name.String(), v.Args)
	}
}

// llvmBcStmt mirrors upstream's LLVM_BC keyword parse (build/plugins/llvm_bc.py).
// Sources are the free args, Name is mandatory, Suffix overrides the default
// OBJ_SUF, Symbols feeds the -internalize-public-api-list opt pass, and the
// two booleans gate llvm-llc emission (GENERATE_MACHINE_CODE) and per-input
// llvm_compile_* dispatch (NO_COMPILE).
type llvmBcStmt struct {
	Sources             []string
	Name                string
	Suffix              string
	Symbols             []string
	GenerateMachineCode bool
	NoCompile           bool
	// ClangBCRoot is CLANG_BC_ROOT captured at parse time (set by
	// USE_LLVM_BC{16,18,20}). Looks like "CLANG16_RESOURCE_GLOBAL::$(CLANG16-...)".
	// Strip everything before "::" to get the bin-root for clang++/llvm-link/opt.
	ClangBCRoot string
}

func isLlvmBcKeyword(s string) bool {
	switch s {
	case "NAME", "SUFFIX", "SYMBOLS", "GENERATE_MACHINE_CODE", "NO_COMPILE":
		return true
	}

	return false
}

func appendPyRegister(d *moduleData, name string, explicit bool) {
	d.pyRegister = append(d.pyRegister, name)
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

func parseCPPProtoPlugin(v *UnknownStmt) cppProtoPlugin {
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

func applyAllocatorStmt(v *UnknownStmt, d *moduleData) {
	if len(v.Args) != 1 {
		ThrowFmt("gen: ALLOCATOR expects exactly 1 argument, got %d (line %d)", len(v.Args), v.Line)
	}

	name := v.Args[0]

	if _, ok := allocatorPeers[name]; !ok {
		ThrowFmt("gen: unknown allocator %q (line %d); extend allocatorPeers in gen.go", name, v.Line)
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
	env := DefaultIfEnv.Clone()

	for k, v := range instance.Platform.Flags {
		env.SetFromStringID(k, v)
	}

	if env.Bool(envOPENSOURCE) || env.String(envOPENSOURCE_PROJECT) == "ymake" || env.String(envOPENSOURCE_PROJECT) == "ya" {
		env.SetBool(envYA_OPENSOURCE, true)
	}

	if env.Bool(envOPENSOURCE) {
		env.SetBool(envCATBOOST_OPENSOURCE, true)
	}

	switch instance.Platform.ISA {
	case ISAX8664:
		env.SetBool(envARCH_X86_64, true)
	case ISAAArch64:
		env.SetBool(envARCH_AARCH64, true)
		env.SetBool(envARCH_ARM64, true)
	}

	useRuntime := instance.Platform.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]
	env.SetBool(envUSE_ARCADIA_COMPILER_RUNTIME, useRuntime != strNo)
	env.SetStringID(envCOMPILER_VERSION, instance.Platform.ClangVerSTR)
	env.SetStringID(envBUILD_TYPE, instance.Platform.BuildTypeUpperSTR)

	if (instance.Platform.ISA == ISAX8664 || env.Bool(envARCH_I386)) &&
		!env.Bool(envDISABLE_INSTRUCTION_SETS) {
		env.SetStringID(envSSE41_CFLAGS, strSSE41CFlags)
		env.SetStringID(envSSE42_CFLAGS, strSSE42CFlags)
		env.SetStringID(envPOPCNT_CFLAGS, strPopcntCFlags)
		env.SetStringID(envCX16_FLAGS, strCX16CFlags)
		env.SetStringID(envAVX_CFLAGS, strAVXCFlags)
		env.SetStringID(envAVX2_CFLAGS, strAVX2CFlags)
		env.SetStringID(envAVX512_CFLAGS, strAVX512CFlags)
		env.SetStringID(envSSE_CFLAGS, strSSECFlags)
		env.SetStringID(envSSE4_CFLAGS, strSSE4CFlags)
		env.SetStringID(envAMX_CFLAGS, strAMXCFlags)
	}

	return env
}

func expandConfigVFSPaths(paths []string, env Environment) []VFS {
	out := make([]VFS, 0, len(paths))

	for _, path := range paths {
		out = append(out, parseModulePathVFS(expandConfigString(path, env)))
	}

	return out
}

func parseModulePathVFS(path string) VFS {
	if vfsHasPrefix(path) {
		return Intern(path)
	}

	return Source(path)
}

func expandConfigString(s string, env Environment) string {
	s = strings.ReplaceAll(s, "${COMPILER_VERSION}", env.String(envCOMPILER_VERSION))
	s = strings.ReplaceAll(s, "${ARCADIA_BUILD_ROOT}", "$(B)")
	s = strings.ReplaceAll(s, "${ARCADIA_ROOT}", "$(S)")
	s = strings.ReplaceAll(s, "${MODDIR}", env.String(envMODDIR))

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

		val, ok := env.Lookup(name)

		if !ok {
			break
		}

		s = s[:start] + val + s[end+1:]
	}

	s = strings.ReplaceAll(s, "${ARCADIA_BUILD_ROOT}", "$(B)")
	s = strings.ReplaceAll(s, "${ARCADIA_ROOT}", "$(S)")
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

			if isExpandVarName(name) {
				if val, ok := env.Lookup(name); ok {
					s = val
				}
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

			val, ok := env.Lookup(name)

			if !isExpandVarName(name) || !ok {
				break
			}

			s = s[:start] + val + s[end+1:]
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

		val, ok := env.Lookup(name)

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

			value, ok := env.Lookup(name)

			if !ok {
				continue
			}

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

func applyAllPySrcs(fs FS, modulePath string, v *UnknownStmt, d *moduleData) {
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
	Name        TOK
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
	d := collectModule(ctx.parsers, &deduper, instance.Path, instance.Kind, mf.Stmts, env)

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

func peerLanguageFor(ctx *genCtx, parent ModuleInstance, parentModuleName TOK, peerPath string) Language {
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

	if peerInfo.Name != tokProtoLibrary {
		return LangCPP
	}

	if isPythonModuleType(parentModuleName) {
		return LangPy
	}

	if parentModuleName == tokProtoLibrary && parent.Language == LangPy {
		return LangPy
	}

	return LangCPP
}

func derivePeerInstance(ctx *genCtx, parent ModuleInstance, d *moduleData, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Kind:     KindLib,
		Language: peerLanguageFor(ctx, parent, d.moduleStmt.Name, peerPath),
		Platform: parent.Platform,
	}
}
