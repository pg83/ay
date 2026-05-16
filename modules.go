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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type moduleData struct {
	moduleStmt          *ModuleStmt
	srcs                []string
	globalSrcs          []string
	pySrcs              []string // PR-M3-A: python sources from PY_SRCS(...); each entry is a .py filename
	pyBuildNoPYC        bool     // PR-M3-A: set by ENABLE(PYBUILD_NO_PYC); suppresses yapyc3 node emission from PY_SRCS
	pyBuildNoPY         bool     // PR-M3-resource-objcopy-C: set by ENABLE(PYBUILD_NO_PY); suppresses raw .py resfs embedding from PY_SRCS (only the yapyc3 form is embedded)
	pyTopLevel          bool     // PR-M3-resource-objcopy-C: set by TOP_LEVEL prefix in PY_SRCS(...); the resfs key for each source omits the dotted module-path prefix
	noExtendedPySearch  bool
	enumSrcs            []*GenerateEnumSerializationStmt // PR-M3-D: GENERATE_ENUM_SERIALIZATION(*) declarations
	peerdirs            []string
	joinSrcs            []*JoinSrcsStmt
	addIncl             []string // collected non-GLOBAL ADDINCL paths
	addInclGlobal       []string // PR-31 D04: collected ADDINCL(GLOBAL ...) paths; peer-propagated to consumers
	cFlags              []string // collected non-GLOBAL CFLAGS values (apply to module's own C+C++ sources)
	cFlagsGlobal        []string // PR-32 D04: collected CFLAGS(GLOBAL ...) values; peer-propagated to consumers' C+C++ sources
	cxxFlags            []string // collected non-GLOBAL CXXFLAGS values (C++ only); PR-29-D02 threads into ModuleCCInputs.CXXFlags
	cxxFlagsGlobal      []string // PR-32 D05: collected CXXFLAGS(GLOBAL ...) values; peer-propagated to consumers' C++ sources
	cOnlyFlags          []string // collected non-GLOBAL CONLYFLAGS values (C only); PR-29-D02 threads into ModuleCCInputs.COnlyFlags
	cOnlyFlagsGlobal    []string // PR-32 D06: collected CONLYFLAGS(GLOBAL ...) values; peer-propagated to consumers' C / .S sources
	sFlags              []string // PR-M3-openssl-as-cflags: SET_APPEND(SFLAGS ...) values; appended to AS compiles only.
	ldFlags             []string // collected LDFLAGS values
	srcDir              string   // last SRCDIR setting (empty = module dir)
	flags               FlagSet  // overlay of inferFlagsFromPath + macro bools
	hadAllocator        bool     // PR-30 D03: set by applyAllocatorStmt; PROGRAM-default-allocator routing fires only when this is false
	allocatorName       string   // PR-35g: name passed to ALLOCATOR(...); empty when no ALLOCATOR macro. Used to suppress malloc/api when ALLOCATOR(FAKE).
	muslLite            bool     // PR-30 D02: set by ENABLE(MUSL_LITE); flips the default-program-peers musl/full → musl gate
	muslEnabled         bool     // module-local MUSL value after SET()/DEFAULT()/CLI env evaluation.
	splitDwarf          bool     // set by SPLIT_DWARF(); PROGRAM LD emits a separate <binary>.debug output.
	noPythonIncl        bool     // set by NO_PYTHON_INCLUDES(); suppresses PY*_LIBRARY-implicit PEERDIR+=contrib/libs/python (build/conf/python.conf:741-743).
	noImportTracing     bool     // set by NO_IMPORT_TRACING(); suppresses PY*_PROGRAM implicit import_tracing constructor peer.
	usePython3          bool     // set by USE_PYTHON3() or PY3-family module types; normalised by applyPython3AddIncl. Triggers _PYTHON3_ADDINCL (python.conf:1018-1023): -DUSE_PYTHON3 + contrib/libs/python/Include.
	pythonSQLite3       bool     // default-on; DISABLE(PYTHON_SQLITE3) flips off the implicit `_sqlite` peer for PY*_PROGRAM modules.
	pyNamespace         string   // set by PY_NAMESPACE(...); used by py-proto resource key layout.
	protoNamespace      string   // set by PROTO_NAMESPACE(...); drives py-proto --ns and output layout.
	noMypy              bool     // set by NO_MYPY(); suppresses mypy plugin and .pyi outputs for py-proto.
	optimizePyProtos    bool     // mirrors OPTIMIZE_PY_PROTOS_FLAG; default-on for PY{2,3}_PROTO variants.
	optimizePyProtosSet bool
	excludeTags         map[string]bool
	ldPlugins           []string // filenames from LD_PLUGIN(name.py); each becomes a CP node and feeds `--start-plugins ... --end-plugins` in consumer LDs. Only contrib/libs/musl/include uses this in M2.
	arPlugin            string   // name from AR_PLUGIN(name); resolves to `$(S)/<modulePath>/<name>.pyplugin` and is injected into AR cmd_args (`--plugin <path>`) and inputs. Mirror of AR_PLUGIN (ymake.core.conf:3396-3398) + _LD_ARCHIVER_KV_PLUGIN (ld.conf:366-368).
	// per-source extra CFLAGS keyed by source filename, populated by
	// `SRC(filename extra_cflags...)`. Threaded into
	// ModuleCCInputs.PerSourceCFlags so the composer appends them right
	// before the input path. Example: `SRC(wide_sse41.cpp -DSSE41_STUB)`
	// at util/charset/ya.make:22-25.
	perSrcCFlags map[string][]string
	// DEFAULT(name value) declarations collected per module. Used by
	// ConfigureFileStmt processing to expand $CFG_VARS. Empty string
	// for DEFAULT(name "").
	defaultVars map[string]string
	// Ordered list of DEFAULT var names for deterministic $CFG_VARS
	// expansion matching the reference cmd_args order.
	defaultVarOrder []string
	// CONFIGURE_FILE() / .cpp.in / .c.in sources → CF nodes.
	configureFiles []*ConfigureFileStmt
	// CREATE_BUILDINFO_FOR(output_header) → BI node.
	createBuildInfoFor string
	// RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT → JV nodes.
	antlr4Grammars []antlr4GrammarInfo
	// RUN_PROGRAM → PR nodes.
	runPrograms        []*RunProgramStmt
	checkConfigHeaders []string
	cythonCpp          []*CythonStmt
	swigC              []swigSrc
	bisonGenExt        string
	grpc               bool
	yaConfJSON         []string
	allPySrcs          []*UnknownStmt
	// ARCHIVE(NAME <out> [DONTCOMPRESS] files...) invocations in
	// declaration order. Each produces an AR node invoking
	// `$(B)/tools/archiver/archiver` to pack the listed files into NAME.
	archives []archiveEntry
	// Map of PR-emitted output filename → producing PR NodeRef.
	// Populated by emitRunProgramsForAR as each RUN_PROGRAM is emitted;
	// consumed by emitArchives to wire AR dep sets to the producer.
	prOutputProducer map[string]NodeRef
	// Set of source filenames declared via `SRC(...)` or
	// `SRC_C_NO_LTO(...)`. These macros emit a FLAT output path (no
	// `_/` infix), unlike `SRCS(subdir/foo.cpp)` which emits
	// `<modulePath>/_/subdir/foo.cpp.o`. Consulted by emitOneSource to
	// set ModuleCCInputs.FlatOutput.
	flatSrcs map[string]struct{}
	// RESOURCE() / RESOURCE_FILES() pair lists, expanded inline at
	// collect time. resources is the canonical view for the objcopy
	// packer in resource.go.
	resources []resourceEntry
	// pyMain captures `PY_MAIN(<arg>)` or the `MAIN <src.py>` modifier
	// of `PY_SRCS(...)`. Produces a single `PY_MAIN=<dotted-mod>:<func>`
	// kv per pybuild.py:py_main (build/plugins/pybuild.py:759).
	pyMain string
	// NO_CHECK_IMPORTS(args...) verbatim arg list. Used by
	// emitNoCheckImportsObjcopy to build the resfs kv with args joined
	// by ' ' (pathid() + value, build/plugins/ytest.py:811).
	noCheckImports []string
	// Explicit empty NO_CHECK_IMPORTS() sets NO_CHECK_IMPORTS_FOR_VALUE=""
	// upstream, which suppresses ADD_CHECK_PY_IMPORTS without emitting
	// any resource kv.
	noCheckImportsDisabled bool
	// PY_REGISTER(args...) dotted module names. emitPyRegister emits
	// gen_py3_reg.py(<arg>, <output>) → `<arg>.reg3.cpp` plus a CC
	// compiling it; mirror of _PY3_REGISTER (ymake.core.conf:4086-4089).
	pyRegister []string
	// Per-`SRC_C_<VARIANT>` entries in declaration order. Each
	// produces one CC node alongside any plain listing of the same
	// file. Entries share the FLAT bucket with SRC()/SRC_C_NO_LTO;
	// reorderARMembers hoists them to the front of the archive.
	simdSrcs []simdSrc
	// Per-module RAGEL6_FLAGS override from `SET(RAGEL6_FLAGS <value>)`.
	// Upstream ymake.core.conf:3284 expands `$RAGEL6_FLAGS ${SRCFLAGS}`
	// before the rest of the ragel6 cmd line. Empty → platform default
	// fires inside EmitR6.
	ragel6Flags []string
	conflictMod *ModuleStmt
	// Module-level INDUCED_DEPS(<exts> headers...) declarations. Each
	// is a SOURCE_ROOT-rooted header path the tool (when this module is
	// a PROGRAM invoked via RUN_PROGRAM) injects into its generated
	// outputs. Seeds the PR output's EmitsIncludes; the scanner walks
	// the headers' real #include graph to reach the full closure.
	inducedDeps []string
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

// collectModule walks `stmts` (IF branches resolved against `env`) and
// returns a moduleData with all macros classified. IfStmts are
// recursively inlined; nested statements inside taken branches are
// treated as top-level. INCLUDE was already inlined by the parser.
//
// `pathFlags` seeds the path-based heuristic; macro overlays mutate it
// in place on the returned moduleData.
func collectModule(modulePath string, stmts []Stmt, env Environment, pathFlags FlagSet) *moduleData {
	d := &moduleData{
		flags:         pathFlags,
		pythonSQLite3: true,
	}

	collectStmts(modulePath, stmts, env, d)
	d.muslEnabled = env.Bool("MUSL")

	deriveGenericCompileFlags(d)

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

	// pybuild processes PY_SRCS / PY_REGISTER by emitting RESOURCE(...)
	// calls; each RESOURCE() expands to PEERDIR(library/cpp/resource)
	// (build/ymake.core.conf:522-524). Mirror that side effect here.
	if len(d.pySrcs) > 0 || len(d.pyRegister) > 0 {
		if modulePath != "library/cpp/resource" {
			already := false
			for _, p := range d.peerdirs {
				if p == "library/cpp/resource" {
					already = true
					break
				}
			}
			if !already {
				d.peerdirs = append(d.peerdirs, "library/cpp/resource")
			}
		}
	}

	return d
}

func deriveGenericCompileFlags(d *moduleData) {
	if flagsContain(d.cFlags, "-nostdinc") || flagsContain(d.cFlagsGlobal, "-nostdinc") {
		d.flags.NoStdInc = true
	}
}

func flagsContain(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}

	return false
}

// applyPython3AddIncl mirrors _PYTHON3_ADDINCL's
// USE_ARCADIA_PYTHON=="yes" branch (build/conf/python.conf:1018-1023):
// `CFLAGS+=-DUSE_PYTHON3` and `ADDINCL+=GLOBAL contrib/libs/python/Include`.
// Invoked by PY3-family module types and USE_PYTHON3()
// (python.conf:738-739, 862, 1064, 1250).
//
// `-DUSE_PYTHON3` is injected via defaultPeerCFlags so it lands at the
// AutoPeerCFlags cmd_args slot (after -D_musl_, before the second
// noLibcUndebug block copy). The contrib/libs/python/Include path
// flows to BOTH d.addInclGlobal (peer-propagated) AND d.addIncl (own).
//
// contrib/libs/python is skipped (its own ya.make emits both); the same
// cycle-guard mirrors the PY*_LIBRARY auto-peerdir code at the
// genModule call site.
//
// NO_PYTHON_INCLUDES() does NOT gate this — upstream gates only the
// implicit `PEERDIR+=contrib/libs/python` (python.conf:741-743), not
// _PYTHON3_ADDINCL itself. library/python/runtime_py3 declares
// NO_PYTHON_INCLUDES() yet its CC nodes carry both flag and include.
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

	d.addInclGlobal = append(d.addInclGlobal, "contrib/libs/python/Include")
	d.addIncl = append(d.addIncl, "contrib/libs/python/Include")

	// ARCHIVE(NAME ...) in library/python/runtime_py3 auto-injects
	// `${addincl;noauto;output:NAME}` (ymake.core.conf:4143): owner-
	// scoped AND peer-propagated to USE_PYTHON3 consumers. Owner gets
	// it via d.addIncl; consumers see it via genModule's post-merge
	// splice (after abseil-cpp).
	if modulePath == "library/python/runtime_py3" {
		d.addIncl = append(d.addIncl, "$(B)/library/python/runtime_py3")
	}
}

// applyBuildInfoAddIncl mirrors the implicit `ADDINCL(<build_info_dir>)`
// upstream CREATE_BUILDINFO_FOR macros emit. PR-M3-final-surgical (fix 5):
// the implicit ADDINCL is GLOBAL — the generated header must be visible to
// PEER consumers too (witnessed by `main.cpp.o` / `print.cpp.o` carrying
// `-I$(B)/library/cpp/build_info` via their peer chain).
func applyBuildInfoAddIncl(modulePath string, d *moduleData) {
	if d.createBuildInfoFor == "" {
		return
	}
	biDir := "$(B)/" + modulePath
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
func collectStmts(modulePath string, stmts []Stmt, env Environment, d *moduleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			d.moduleStmt = v
		case *SrcsStmt:
			// SRCS(GLOBAL foo.cpp) uses GLOBAL as a per-source modifier
			// (symbols exported globally, equivalent to GLOBAL_SRCS).
			// Strip GLOBAL tokens and route the following sources to
			// d.globalSrcs.
			globalNext := false

			for _, src := range expandListVars(v.Sources, env) {
				if src == "GLOBAL" {
					globalNext = true

					continue
				}

				if globalNext {
					d.globalSrcs = append(d.globalSrcs, src)
					globalNext = false
				} else {
					d.srcs = append(d.srcs, src)
				}
			}
		case *PeerdirStmt:
			// The ADDINCL modifier means "peer this AND add the same
			// path to this module's own ADDINCL list". Used by
			// `PEERDIR(ADDINCL contrib/libs/protobuf …)` in
			// tools/event2cpp/bin/ya.make.
			addInclNext := false
			for _, p := range v.Paths {
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
					d.addIncl = append(d.addIncl, p)
					addInclNext = false
				}
				d.peerdirs = append(d.peerdirs, p)
			}
		case *SetStmt:
			// SET updates the module-local env for following IF branches
			// and config-derived defaults. yes/no stay bools so bare
			// IF(MUSL) and IF(MUSL == "no") both behave as expected.
			env.SetFromString(v.Name, v.Value)

			if v.Name == "RAGEL6_FLAGS" {
				// `_SRC("rl6", ...)` (ymake.core.conf:3284)
				// interpolates $RAGEL6_FLAGS before everything else, so
				// SET replaces the default (last-write-wins).
				d.ragel6Flags = []string{v.Value}
			}
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			d.joinSrcs = append(d.joinSrcs, v)
		case *AddInclStmt:
			// GLOBAL paths peer-propagate to consumers via PEERDIR
			// (d.addInclGlobal); own-cmd_args emission uses d.addIncl
			// which includes BOTH GLOBAL and non-GLOBAL in positional
			// declaration order (AllPaths). Empirically the reference
			// emits own GLOBAL ADDINCL on the module's own CC compiles
			// (libcxx algorithm.cpp.o cmd_args[9..11]).
			d.addInclGlobal = append(d.addInclGlobal, expandConfigPaths(v.GlobalPaths, env)...)
			d.addIncl = append(d.addIncl, expandConfigPaths(v.AllPaths, env)...)
		case *CFlagsStmt:
			// GLOBAL flags peer-propagate (d.cFlagsGlobal); non-GLOBAL
			// applies to own C+C++ sources only (d.cFlags). composeCC
			// emits the GLOBAL bucket flanking catboost-redux.
			d.cFlagsGlobal = append(d.cFlagsGlobal, v.GlobalFlags...)
			d.cFlags = append(d.cFlags, v.OwnFlags...)
		case *CXXFlagsStmt:
			// GLOBAL CXXFLAGS peer-propagate to consumers' C++
			// compiles; non-GLOBAL applies to own C++ sources only.
			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, v.GlobalFlags...)
			d.cxxFlags = append(d.cxxFlags, v.OwnFlags...)
		case *CONLYFlagsStmt:
			// GLOBAL CONLYFLAGS peer-propagate to consumers' C / .S
			// compiles; non-GLOBAL applies to own C / .S only.
			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, v.GlobalFlags...)
			d.cOnlyFlags = append(d.cOnlyFlags, v.OwnFlags...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, v.Flags...)
		case *SrcDirStmt:
			// SRCDIR shifts source resolution base for per-source CC /
			// AS / R6 / JOIN_SRCS nodes. LD/AR stay at instance.Path —
			// the binary/archive lives where declared, even if sources
			// are elsewhere.
			d.srcDir = v.Dir
		case *GlobalSrcsStmt:
			d.globalSrcs = append(d.globalSrcs, v.Sources...)
		case *GenerateEnumSerializationStmt:
			d.enumSrcs = append(d.enumSrcs, v)
		case *DefaultVarStmt:
			// Track DEFAULT(name value) for $CFG_VARS expansion.
			if d.defaultVars == nil {
				d.defaultVars = map[string]string{}
			}
			if _, exists := d.defaultVars[v.VarName]; !exists {
				d.defaultVars[v.VarName] = v.Value
				d.defaultVarOrder = append(d.defaultVarOrder, v.VarName)
			}
			// Bridge the binding into the per-module IF env so later
			// IF (NAME) / IF (NAME == "v") predicates see it. Mirrors
			// TEvalContext::SetStatement's NMacro::DEFAULT branch
			// (devtools/ymake/lang/eval_context.cpp:335-339): only sets
			// when the variable has no prior binding. Env is a per-
			// module clone, so this does not leak across modules.
			env.SetDefaultString(v.VarName, v.Value)
		case *ConfigureFileStmt:
			// PR-M3-E: explicit CONFIGURE_FILE(src dst) declaration.
			d.configureFiles = append(d.configureFiles, v)
		case *CreateBuildInfoStmt:
			// PR-M3-E: CREATE_BUILDINFO_FOR(header) → BI node.
			d.createBuildInfoFor = v.OutputHeader
		case *RunAntlr4CppStmt:
			// PR-M3-E: single-grammar ANTLR4 invocation → JV node.
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        false,
				Grammar:        v.Grammar,
				Options:        append([]string(nil), v.Options...),
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: append([]string(nil), v.OutputIncludes...),
			})
		case *RunAntlr4CppSplitStmt:
			// PR-M3-E: lexer+parser split ANTLR4 invocation → JV node.
			d.antlr4Grammars = append(d.antlr4Grammars, antlr4GrammarInfo{
				IsSplit:        true,
				Lexer:          v.Lexer,
				Parser:         v.Parser,
				Visitor:        v.Visitor,
				Listener:       v.Listener,
				OutputIncludes: append([]string(nil), v.OutputIncludes...),
			})
		case *RunProgramStmt:
			// PR-M3-E: RUN_PROGRAM → PR node.
			d.runPrograms = append(d.runPrograms, v)
		case *ResourceStmt:
			// PR-M3-resource-objcopy-A: RESOURCE pairs feed the objcopy
			// packer as-is. Pairs whose path is "-" are kv-only entries;
			// non-"-" pairs are (source path, raw key) pairs.
			for _, pair := range v.Pairs {
				d.resources = append(d.resources, resourceEntry{Path: pair.Path, Key: pair.Key})
			}
		case *ResourceFilesStmt:
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

			collectStmts(modulePath, taken, env, d)
		case *UnknownStmt:
			applyUnknownStmt(modulePath, v, d)
		default:
			ThrowFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

// applyUnknownStmt routes an UnknownStmt by name. NO_LIBC / NO_UTIL /
// NO_RUNTIME / NO_PLATFORM / NO_COMPILER_WARNINGS override the
// inferFlagsFromPath heuristic. ALLOCATOR(NAME) resolves to an implicit
// PEERDIR (ymake.core.conf:961-1035). Anything else must be in the
// metadata whitelist; unknown names throw so a new ya.make macro
// surfaces immediately rather than being silently dropped.
func applyUnknownStmt(modulePath string, v *UnknownStmt, d *moduleData) {
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
		// Linter-only macro. It does not emit build nodes for `yatool make`
		// target graphs.
	case "MAVEN_GROUP_ID":
		// Java export metadata. build/conf/java.conf documents no effect
		// on regular builds.
	case "CHECK_CONFIG_H":
		if len(v.Args) != 1 {
			ThrowFmt("CHECK_CONFIG_H expects exactly 1 argument, got %d", len(v.Args))
		}

		d.checkConfigHeaders = append(d.checkConfigHeaders, v.Args[0])
	case "BUILDWITH_CYTHON_CPP":
		if len(v.Args) == 0 {
			ThrowFmt("BUILDWITH_CYTHON_CPP expects at least 1 argument")
		}

		d.cythonCpp = append(d.cythonCpp, &CythonStmt{Src: v.Args[0], Options: append([]string(nil), v.Args[1:]...)})
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
		d.pyNamespace = v.Args[0]
	case "PROTO_NAMESPACE":
		if len(v.Args) == 0 {
			ThrowFmt("gen: PROTO_NAMESPACE expects at least 1 argument")
		}
		d.protoNamespace = v.Args[len(v.Args)-1]
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

		d.yaConfJSON = append(d.yaConfJSON, v.Args[0])
	case "ALLOCATOR":
		applyAllocatorStmt(v, d)
	case "ARCHIVE":
		// `ARCHIVE(NAME <out> [DONTCOMPRESS] files...)` per
		// ymake.core.conf:4142-4145.
		applyArchiveStmt(v, d)
	case "ENABLE":
		// ENABLE(MUSL_LITE): yasm declares this inside IF(MUSL); without
		// the flip defaultProgramPeerdirsFor pulls musl/full and creates
		// a cross-PROGRAM cycle (yasm → musl/full → asmlib → yasm).
		// ENABLE(PYBUILD_NO_PYC): suppresses yapyc3 node emission for
		// modules that declare Python sources but do not want
		// .pyc/.yapyc3 generated. ENABLE(PYBUILD_NO_PY) (no 'C') is
		// separate: suppresses raw .py resfs embedding while still
		// running yapyc3 compilation (contrib/tools/python3/Lib).
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

		filename := v.Args[0]
		d.srcs = append(d.srcs, filename)

		if d.flatSrcs == nil {
			d.flatSrcs = map[string]struct{}{}
		}

		d.flatSrcs[filename] = struct{}{}

		if len(v.Args) > 1 {
			if d.perSrcCFlags == nil {
				d.perSrcCFlags = map[string][]string{}
			}

			extras := append([]string(nil), v.Args[1:]...)
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
	case "SRC_C_AVX", "SRC_C_SSE2", "SRC_C_SSE3", "SRC_C_SSSE3",
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

		filename := v.Args[0]
		flags := make([]string, 0, len(variant.CFlags)+len(v.Args)-1)
		flags = append(flags, variant.CFlags...)
		flags = append(flags, v.Args[1:]...)

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
		d.arPlugin = v.Args[0] + ".pyplugin"
	case "USE_PYTHON3":
		// USE_PYTHON3() implicit PEERDIRs per python.conf:1064-1071:
		// contrib/libs/python + library/python/runtime_py3 (under
		// USE_ARCADIA_PYTHON=="yes"). contrib/tools/python3 and
		// contrib/tools/python3/Lib are NOT added here — they come
		// from the PYTHON3_MODULE() macro (python.conf:644-647) gated
		// by USE_ARCADIA_PYTHON && (MSVC || IS_CROSS_TOOLS), and plain
		// PROGRAM / LIBRARY callers reach python3 transitively via
		// contrib/libs/python's own peer list. Adding them here would
		// reorder peer-AddInclGlobal across ~158 nodes.
		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)
		// USE_PYTHON3() also invokes _PYTHON3_ADDINCL (python.conf:1064);
		// applyPython3AddIncl performs the -DUSE_PYTHON3 + Include
		// injection in collectModule's post-pass.
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
		swigCMode := false
		for i := 0; i < len(v.Args); i++ {
			a := v.Args[i]

			switch a {
			case "TOP_LEVEL":
				topLevel = true
				d.pyTopLevel = true
				continue
			case "NAMESPACE":
				i++

				continue
			case "CYTHONIZE_PY":
				cythonizePy = true
				continue
			case "CYTHON_CPP":
				cythonPlainCpp = true
				continue
			case "CYTHON_C", "CYTHON_DIRECTIVE":
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
					modName = pythonModuleName(modulePath, src, topLevel)
				}
				stmt := &CythonStmt{
					Src: src,
					Options: []string{
						"--module-name", modName,
						"--init-suffix", pythonInitSuffix(modName),
						"--source-root", "$(S)",
						"-X", "set_initial_path=" + modulePath + "/" + src,
					},
				}
				if cythonPlainCpp {
					stmt.Generated = src + ".cpp"
				}
				d.cythonCpp = append(d.cythonCpp, stmt)
				appendPyRegister(d, modName)
				mainNext = false

				continue
			}

			if cythonizePy && strings.HasSuffix(src, ".py") {
				modName := modNameOverride
				if modName == "" {
					modName = pythonModuleName(modulePath, src, topLevel)
				}
				d.cythonCpp = append(d.cythonCpp, &CythonStmt{
					Src:       src,
					Generated: src + ".cpp",
					Options: []string{
						"--module-name", modName,
						"--init-suffix", pythonInitSuffix(modName),
						"--source-root", "$(S)",
						"-X", "set_initial_path=" + modulePath + "/" + src,
					},
				})
				appendPyRegister(d, modName)
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
					appendPyRegister(d, modName+"_swg")
				}
				mainNext = false

				continue
			}

			if strings.HasSuffix(src, ".pyi") {
				modName := modNameOverride
				if modName == "" {
					modName = pythonModuleName(modulePath, src, topLevel)
				}
				dest := "py/" + strings.ReplaceAll(modName, ".", "/") + ".pyi"
				d.resources = append(d.resources, expandResourceFiles([]string{"DEST", dest, src})...)
				mainNext = false

				continue
			}

			if strings.Contains(a, "=") && !strings.HasSuffix(src, ".py") {
				continue
			}

			d.pySrcs = append(d.pySrcs, src)
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
				d.pyMain = modName + ":main"
				mainNext = false
			}
		}
	case "ALL_PY_SRCS":
		d.allPySrcs = append(d.allPySrcs, v)
	case "PY_MAIN":
		// PY_MAIN(<arg>) per pybuild.py:onpy_main
		// (build/plugins/pybuild.py:762). Argument normalised: `/` → `.`
		// and `:main` appended when there's no colon. The M3 closure
		// contains only single-PY_MAIN modules.
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := strings.ReplaceAll(v.Args[0], "/", ".")
		if !strings.Contains(arg, ":") {
			arg += ":main"
		}
		d.pyMain = arg
	case "PY_CONSTRUCTOR":
		// PY_CONSTRUCTOR(<module[:func]>) per pybuild.py:onpy_constructor:
		// emits a kv-only resource under py/constructors/, defaulting
		// to "=init" when no function is specified.
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
	case "PY_REGISTER":
		// PY_REGISTER(args...) dotted module names. emitPyRegister
		// later emits one PY node generating `<arg>.reg3.cpp` plus a
		// CC compiling it; both flow into the module's `.global.a`
		// (mirror of SRCS(GLOBAL $Func.reg3.cpp) inside _PY3_REGISTER
		// at ymake.core.conf:4086-4089).
		for _, name := range v.Args {
			appendPyRegister(d, name)
		}
	case "SET_APPEND":
		// SET_APPEND(<var> <values...>) appends to a ymake variable.
		// Only SFLAGS is wired (openssl/crypto/ya.make.inc:179-186); it
		// threads between CFLAGS and `-c -o` in AS cmd_args
		// (ymake.core.conf:3217). Other targets no-op.
		if len(v.Args) >= 2 && v.Args[0] == "SFLAGS" {
			d.sFlags = append(d.sFlags, v.Args[1:]...)
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
			ThrowFmt("gen: PR-25 does not yet support macro %q (extend whitelistedMetadataMacros or add a typed Stmt)", v.Name)
		}
	}
}

func appendPyRegister(d *moduleData, name string) {
	d.pyRegister = append(d.pyRegister, name)

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

func pythonModuleName(modulePath, src string, topLevel bool) string {
	ns := strings.ReplaceAll(modulePath, "/", ".") + "."
	if topLevel {
		ns = ""
	}

	modName := strings.TrimSuffix(src, ".py")
	modName = strings.TrimSuffix(modName, ".pyx")
	modName = strings.ReplaceAll(modName, "/", ".")

	return ns + modName
}

func pythonInitSuffix(name string) string {
	var mangled strings.Builder
	for _, seg := range strings.Split(name, ".") {
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

// applyAllocatorStmt resolves `ALLOCATOR(<name>)` to a PEERDIR
// addition per `build/ymake.core.conf:961-1035`. The macro takes
// exactly one argument; multi-arg or unknown allocator names throw
// loudly per D27 discipline.
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
	case "PROGRAM", "PY2_PROGRAM", "PY3_PROGRAM", "PY3_PROGRAM_BIN":
		return true
	}

	return false
}

// isPyLibraryType returns true for Python library/program module names
// that behave as LIBRARY-shaped modules (emit AR/CC for their C++ SRCS,
// header-only when they have none). Unlike isMultimoduleLibraryType
// these are NOT unconditionally header-only — hasCompilableSource gates
// the path. Separated so the gate check at the top of genModule admits
// them without routing every one to the header-only path.
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

func isMultimoduleLibraryType(name string) bool {
	switch name {
	case "PROTO_LIBRARY",
		"DLL", "SO_PROGRAM",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY":
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

func expandConfigString(s string, env Environment) string {
	return strings.ReplaceAll(s, "${COMPILER_VERSION}", env.String("COMPILER_VERSION"))
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

func applyAllPySrcs(sourceRoot, modulePath string, v *UnknownStmt, d *moduleData) {
	dirs := []string{"."}
	noTestFiles := false

	for i := 0; i < len(v.Args); i++ {
		a := v.Args[i]

		switch a {
		case "TOP_LEVEL":
			d.pyTopLevel = true
		case "NAMESPACE":
			i++
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
	for _, dir := range dirs {
		moduleRoot := filepath.Join(sourceRoot, modulePath)
		root := filepath.Join(moduleRoot, dir)

		err := filepath.WalkDir(root, func(path string, ent os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ent.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".py" {
				return nil
			}

			base := filepath.Base(path)
			if noTestFiles && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) {
				return nil
			}

			rel := Throw2(filepath.Rel(moduleRoot, path))
			files = append(files, filepath.ToSlash(rel))

			return nil
		})
		Throw(err)
	}

	sort.Strings(files)
	d.pySrcs = append(d.pySrcs, files...)
}

type moduleTypeCacheKey struct {
	Path     string
	Platform *Platform
	Flags    FlagSet
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
		Platform: instance.Platform,
		Flags:    instance.Flags,
	}
	if info, ok := ctx.moduleTypeCache[key]; ok {
		return info
	}

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path, "ya.make")
	mf := Throw2(ParseFile(yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(instance.Path, mf.Stmts, env, instance.Flags)
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
	if !peerYaMakeExists(ctx.sourceRoot, peerPath) {
		return LangCPP
	}

	peerSeed := ModuleInstance{
		Path:     peerPath,
		Language: LangCPP,
		Platform: parent.Platform,
		Flags:    inferFlagsFromPath(peerPath, parent.Platform.PIC),
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
// use LangPy; every other peer walk stays LangCPP. Platform and PIC
// axis continue to flow from the parent. Macro overlay (NO_LIBC /
// NO_UTIL / ...) happens inside genModule when the peer's ya.make is
// parsed.
func derivePeerInstance(ctx *genCtx, parent ModuleInstance, d *moduleData, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Language: peerLanguageFor(ctx, parent, d.moduleStmt.Name, peerPath),
		Platform: parent.Platform,
		// PIC follows platform settings rather than ISA/host identity.
		Flags: inferFlagsFromPath(peerPath, parent.Platform.PIC),
	}
}
