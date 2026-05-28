package main

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
	cfAddIncl            []VFS
	cfAddInclGlobal      []VFS
	cythonAddIncl        []VFS
	asmAddIncl           []VFS
	cFlags               []string
	cFlagsGlobal         []string
	cxxFlags             []string
	cxxFlagsGlobal       []string
	cOnlyFlags           []string
	cOnlyFlagsGlobal     []string
	sFlags               []string
	protocFlags          []string
	flatcFlags           []string
	ldFlags              []string
	rpathFlagsGlobal     []string
	objAddLibsGlobal     []string
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
	moduleScopeCFlags    []string
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

	perSrcCFlags map[string][]string

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

	flatSrcs map[string]struct{}

	resources []resourceEntry

	pyMain *string

	noCheckImports []string

	noCheckImportsDisabled bool

	pyRegister []string

	pyRegisterExplicit []bool

	simdSrcs []simdSrc

	ragel6Flags []string
	conflictMod *ModuleStmt

	inducedDeps []string

	setVars map[string]string
}

func muslCFlags(on bool) []string {
	if on {
		return []string{"-D_musl_"}
	}

	return nil
}

type resourceEntry struct {
	Path string
	Key  string
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
	Src            string
	Dst            string
	Auto           bool
	WithContext    bool
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

func sourceInputVFS(fs *FS, modulePath string, path string) *VFS {
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
		if fs.IsFile(moduleRel) {
			return vfsPtr(Source(moduleRel))
		}
		if fs.IsFile(clean) {
			return vfsPtr(Source(clean))
		}
	}

	return nil
}

func copyFileInputVFS(fs *FS, modulePath string, src string) VFS {
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

func collectModule(pm *includeParserManager, modulePath string, kind ModuleKind, stmts []Stmt, env Environment) *moduleData {
	fs := pm.fs

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

	d.addIncl = append(d.addIncl, d.cfAddIncl...)
	d.addInclGlobal = append(d.addInclGlobal, d.cfAddInclGlobal...)
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
	d.muslEnabled = env.Bool("MUSL")
	if d.muslLite {
		d.flags.NoUtil = true
	}

	if env.Bool("PY3_PROTO") {
		d.usePython3 = true
	}

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)

	cflagPrefix := append(muslCFlags(d.muslEnabled && !effectiveNoPlatform(d.flags)), sseBaseCFlags(env.Bool("ARCH_X86_64"))...)
	d.moduleScopeCFlags = append(cflagPrefix, d.moduleScopeCFlags...)

	d.addIncl = mergeDedupVFS(d.addIncl, nil)
	d.addInclGlobal = mergeDedupVFS(d.addInclGlobal, nil)

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

	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" {

		if !env.Bool("PY3_PROTO") {
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
		if shouldCheckSourceDir(path) && !fs.IsDir(path.Rel()) {
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

	d.moduleScopeCFlags = append(d.moduleScopeCFlags, "-DUSE_PYTHON3")

	d.addInclGlobal = append(d.addInclGlobal, Intern("$(S)/contrib/libs/python/Include"))
	d.addIncl = append(d.addIncl, Intern("$(S)/contrib/libs/python/Include"))

	if modulePath == "library/python/runtime_py3" {
		d.addIncl = append(d.addIncl, Intern("$(B)/library/python/runtime_py3"))
	}
}

func applyBuildInfoAddIncl(modulePath string, d *moduleData) {
	if d.createBuildInfoFor == nil {
		return
	}
	biDir := Build(modulePath)
	d.addIncl = append(d.addIncl, biDir)
	d.addInclGlobal = append(d.addInclGlobal, biDir)
}

func pyModuleTypeUsesPython3(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY3_PROGRAM", "PY3_PROGRAM_BIN",
		"PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
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

			if v.Name == "PY3_PROGRAM" && kind == KindBin {
				d.peerdirs = append([]string{modulePath}, d.peerdirs...)
			}

			if v.Name == "UNITTEST_FOR" {

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
			env.SetFromString(v.Name, value)

			if d.setVars == nil {
				d.setVars = map[string]string{}
			}
			d.setVars[v.Name] = value

			if v.Name == "RAGEL6_FLAGS" {

				d.ragel6Flags = []string{value}
			}
		case *EndStmt:

		case *JoinSrcsStmt:
			expanded := *v
			expanded.Sources = expandStmtTokens(v.Sources, env)
			d.joinSrcs = append(d.joinSrcs, &expanded)
		case *AddInclStmt:

			d.addInclGlobal = append(d.addInclGlobal, expandConfigVFSPaths(v.GlobalPaths, env)...)
			d.addInclOneLevel = append(d.addInclOneLevel, expandConfigVFSPaths(v.OneLevelPaths, env)...)
			d.addIncl = append(d.addIncl, expandConfigVFSPaths(v.AllPaths, env)...)
			d.cythonAddIncl = append(d.cythonAddIncl, expandConfigVFSPaths(v.CythonPaths, env)...)
			d.asmAddIncl = append(d.asmAddIncl, expandConfigVFSPaths(v.AsmPaths, env)...)
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

			env.SetDefaultString(v.VarName, expandScalarVarRef(v.Value, env))
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
			if d.firstResourceEvent < 0 {
				d.firstResourceEvent = d.globalEventSeq
			}
			d.globalEventSeq++

			ensureResourcePeer(modulePath, d)

			for _, pair := range v.Pairs {
				d.resources = append(d.resources, resourceEntry{
					Path: expandStmtToken(pair.Path, env),
					Key:  pair.Key,
				})
			}
		case *ResourceFilesStmt:
			if d.firstResourceEvent < 0 {
				d.firstResourceEvent = d.globalEventSeq
			}
			d.globalEventSeq++
			ensureResourcePeer(modulePath, d)

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

	if stmt.Name == "PY3_PROGRAM" && kind == KindLib {
		out := *stmt
		out.Name = "PY3_LIBRARY"
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
	switch v.Name {
	case "NO_LIBC":

		d.flags.NoLibc = true
		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_UTIL":
		d.flags.NoUtil = true
	case "NO_RUNTIME":

		d.flags.NoRuntime = true
		d.flags.NoUtil = true
	case "NO_PLATFORM":

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

		d.noPythonIncl = true
	case "NO_IMPORT_TRACING":
		d.noImportTracing = true
	case "NO_EXTENDED_SOURCE_SEARCH":
		d.noExtendedPySearch = true
	case "STYLE_RUFF":

	case "LLVM_BC":
		if len(v.Args) < 2 {
			ThrowFmt("LLVM_BC expects at least source and output, got %d", len(v.Args))
		}
		if env.String("CLANG_BC_ROOT") == "" || env.String("LLVM_LLC_TOOL") == "" {
			ThrowFmt("LLVM_BC requires USE_LLVM_BC16/18/20 before invocation")
		}

	case "MAVEN_GROUP_ID":

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
	case "COPY":
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

		applyArchiveStmt(v, d)
	case "ENABLE":

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

		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case "AR_PLUGIN":

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

		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)

		d.usePython3 = true
	case "PY_SRCS":

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
				(d.moduleStmt.Name == "PY3_PROGRAM" || d.moduleStmt.Name == "PY3_PROGRAM_BIN") &&
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
	case "ALL_PY_SRCS":
		d.allPySrcs = append(d.allPySrcs, v)
	case "PY_MAIN":

		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := strings.ReplaceAll(v.Args[0], "/", ".")
		if !strings.Contains(arg, ":") {
			arg += ":main"
		}
		d.pyMain = stringPtr(arg)
	case "PY_CONSTRUCTOR":

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

		for _, name := range v.Args {
			appendPyRegister(d, name, true)
		}
	case "SET_APPEND":

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

	"FAKE":    nil,
	"SYSTEM":  {"library/cpp/malloc/system"},
	"DEFAULT": nil,
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

func isPyLibraryType(name string) bool {
	switch name {
	case "PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

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

func isResourceContainerType(name string) bool {
	switch name {
	case "PACKAGE", "UNION", "RESOURCES_LIBRARY":
		return true
	}

	return false
}

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

	if (instance.Platform.ISA == ISAX8664 || env.Bool("ARCH_I386")) &&
		!env.Bool("DISABLE_INSTRUCTION_SETS") {
		env.SetString("SSE41_CFLAGS", "-msse4.1")
		env.SetString("SSE42_CFLAGS", "-msse4.2")
		env.SetString("POPCNT_CFLAGS", "-mpopcnt")
		env.SetString("CX16_FLAGS", "-mcx16")
		env.SetString("AVX_CFLAGS", "-mavx -mpclmul")
		env.SetString("AVX2_CFLAGS", "-mavx2 -mfma -mbmi -mbmi2")
		env.SetString("AVX512_CFLAGS", "-mavx512f -mavx512cd -mavx512bw -mavx512dq -mavx512vl")
		env.SetString("SSE_CFLAGS", "-msse2 -msse3 -mssse3")
		env.SetString("SSE4_CFLAGS", "-msse4.1 -msse4.2 -mpopcnt -mcx16")
		env.SetString("AMX_CFLAGS", "-mamx-tile -mamx-int8 -mavx512f -mavx512cd -mavx512bw -mavx512dq -mavx512vl")
	}

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
	if vfsHasPrefix(path) {
		return Intern(path)
	}

	return Source(path)
}

func expandConfigString(s string, env Environment) string {
	s = strings.ReplaceAll(s, "${COMPILER_VERSION}", env.String("COMPILER_VERSION"))
	s = strings.ReplaceAll(s, "${ARCADIA_BUILD_ROOT}", "$(B)")
	s = strings.ReplaceAll(s, "${ARCADIA_ROOT}", "$(S)")
	s = strings.ReplaceAll(s, "${MODDIR}", env.String("MODDIR"))

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
		if !env.HasBinding(name) {
			break
		}
		s = s[:start] + env.String(name) + s[end+1:]
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
	d := collectModule(ctx.parsers, instance.Path, instance.Kind, mf.Stmts, env)
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

func derivePeerInstance(ctx *genCtx, parent ModuleInstance, d *moduleData, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Kind:     KindLib,
		Language: peerLanguageFor(ctx, parent, d.moduleStmt.Name, peerPath),
		Platform: parent.Platform,
	}
}
