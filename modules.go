package main

// modules.go — module-level parsed-statement collection and
// per-statement application onto moduleData.
//
// collectModule walks the parsed ya.make statements, resolves IF
// branches via macros.go's evaluator, and lowers each recognised
// statement (SRCS, PEERDIR, GLOBAL_SRCS, NO_*, ADDINCL, CFLAGS,
// SRCDIR, …) into the moduleData accumulator. applyUnknownStmt
// routes the long tail of project-specific macros (PY_REGISTER,
// PY_MAIN, RUN_PROGRAM, RESOURCE, ARCHIVE, ANTLR4 grammars, etc.)
// into their respective moduleData slots.
//
// buildIfEnv constructs the Environment used for IF-evaluation,
// seeded from the ModuleInstance's per-axis flags and CLI defines.
// derivePeerInstance assembles a peer's ModuleInstance preserving
// the parent's language/platform axes.

import (
	"fmt"
	"strings"
)

type moduleData struct {
	moduleStmt       *ModuleStmt
	srcs             []string
	globalSrcs       []string
	pySrcs           []string // PR-M3-A: python sources from PY_SRCS(...); each entry is a .py filename
	pyBuildNoPYC     bool     // PR-M3-A: set by ENABLE(PYBUILD_NO_PYC); suppresses yapyc3 node emission from PY_SRCS
	pyBuildNoPY      bool     // PR-M3-resource-objcopy-C: set by ENABLE(PYBUILD_NO_PY); suppresses raw .py resfs embedding from PY_SRCS (only the yapyc3 form is embedded)
	pyTopLevel       bool     // PR-M3-resource-objcopy-C: set by TOP_LEVEL prefix in PY_SRCS(...); the resfs key for each source omits the dotted module-path prefix
	enumSrcs         []*GenerateEnumSerializationStmt // PR-M3-D: GENERATE_ENUM_SERIALIZATION(*) declarations
	peerdirs         []string
	joinSrcs         []*JoinSrcsStmt
	addIncl          []string // collected non-GLOBAL ADDINCL paths
	addInclGlobal    []string // PR-31 D04: collected ADDINCL(GLOBAL ...) paths; peer-propagated to consumers
	cFlags           []string // collected non-GLOBAL CFLAGS values (apply to module's own C+C++ sources)
	cFlagsGlobal     []string // PR-32 D04: collected CFLAGS(GLOBAL ...) values; peer-propagated to consumers' C+C++ sources
	cxxFlags         []string // collected non-GLOBAL CXXFLAGS values (C++ only); PR-29-D02 threads into ModuleCCInputs.CXXFlags
	cxxFlagsGlobal   []string // PR-32 D05: collected CXXFLAGS(GLOBAL ...) values; peer-propagated to consumers' C++ sources
	cOnlyFlags       []string // collected non-GLOBAL CONLYFLAGS values (C only); PR-29-D02 threads into ModuleCCInputs.COnlyFlags
	cOnlyFlagsGlobal []string // PR-32 D06: collected CONLYFLAGS(GLOBAL ...) values; peer-propagated to consumers' C / .S sources
	sFlags           []string // PR-M3-openssl-as-cflags: SET_APPEND(SFLAGS ...) values; appended to AS compiles only.
	ldFlags          []string // collected LDFLAGS values
	srcDir           string   // last SRCDIR setting (empty = module dir)
	flags            FlagSet  // overlay of inferFlagsFromPath + macro bools
	hadAllocator     bool     // PR-30 D03: set by applyAllocatorStmt; PROGRAM-default-allocator routing fires only when this is false
	allocatorName    string   // PR-35g: name passed to ALLOCATOR(...); empty when no ALLOCATOR macro. Used to suppress malloc/api when ALLOCATOR(FAKE).
	muslLite         bool     // PR-30 D02: set by ENABLE(MUSL_LITE); flips the default-program-peers musl/full → musl gate
	noPythonIncl     bool     // PR-M3-aarch64-py-closure: set by NO_PYTHON_INCLUDES(); suppresses the PY*_LIBRARY-implicit PEERDIR+=contrib/libs/python (mirror of `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }` in build/conf/python.conf:741-743).
	usePython3      bool      // PR-M3-python-addincl-cflags: set by USE_PYTHON3() or a PY3-family module type (PY3_LIBRARY / PY3_PROGRAM / PY3_PROGRAM_BIN / PY23_LIBRARY / PY23_NATIVE_LIBRARY); normalised by applyPython3AddIncl. Triggers the `when ($USE_ARCADIA_PYTHON == "yes")` branch of `_PYTHON3_ADDINCL` (python.conf:1018-1023): -DUSE_PYTHON3 (via defaultPeerCFlags / AutoPeerCFlags slot) and contrib/libs/python/Include (own + GLOBAL ADDINCL).
	ldPlugins        []string // PR-35k: filenames declared via LD_PLUGIN(name.py); the only M2 case is contrib/libs/musl/include's `LD_PLUGIN(musl.py)`. Each entry becomes a CP node and feeds `--start-plugins ... --end-plugins` in consumer LDs.
	arPlugin         string   // PR-M3-openssl-ar-plugin-and-as-clean: name from AR_PLUGIN(name); resolves to `$(S)/<modulePath>/<name>.pyplugin` and is injected into the AR cmd_args (`--plugin <path>`) and inputs. Mirror of upstream macro `AR_PLUGIN` (ymake.core.conf:3396-3398) + `_LD_ARCHIVER_KV_PLUGIN` (ld.conf:366-368). Empty when no AR_PLUGIN macro present.
	// PR-35o: per-source extra CFLAGS keyed by source filename.
	// Populated by `SRC(filename extra_cflags...)` (e.g.
	// `util/charset/ya.make:22-25` `SRC(wide_sse41.cpp -DSSE41_STUB)`).
	// Threaded through emitOneSource into ModuleCCInputs.PerSourceCFlags
	// so the composer can append the per-source flags right before the
	// input path (matching the reference cmd_args slot for the SSE41_STUB
	// flag on `util/charset/wide_sse41.cpp.o`).
	perSrcCFlags map[string][]string
	// PR-M3-E: DEFAULT(name value) declarations collected per-module.
	// Used by ConfigureFileStmt processing to expand $CFG_VARS.
	// Keys are variable names; values are the DEFAULT values (empty
	// string for DEFAULT(name "")).
	defaultVars map[string]string
	// PR-M3-E: ordered list of DEFAULT var names (for deterministic
	// $CFG_VARS expansion matching the reference cmd_args order).
	defaultVarOrder []string
	// PR-M3-E: CONFIGURE_FILE() / .cpp.in / .c.in sources → CF nodes.
	configureFiles []*ConfigureFileStmt
	// PR-M3-E: CREATE_BUILDINFO_FOR(output_header) → BI node.
	createBuildInfoFor string
	// PR-M3-E: RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT → JV nodes.
	antlr4Grammars []antlr4GrammarInfo
	// PR-M3-E: RUN_PROGRAM → PR nodes.
	runPrograms []*RunProgramStmt
	// PR-M3-unpaired-got-closure: ARCHIVE(NAME <out> [DONTCOMPRESS] files...)
	// invocations collected in declaration order. Each entry produces one
	// AR node invoking `$(B)/tools/archiver/archiver` to pack the
	// listed files into NAME.
	archives []archiveEntry
	// PR-M3-unpaired-got-closure: map of PR-emitted output filename →
	// producing PR NodeRef. Populated by emitRunProgramsForAR as each
	// RUN_PROGRAM is emitted. Consumed by emitArchives to wire each AR
	// node's dep set to the producing PR (matching the REF shape).
	prOutputProducer map[string]NodeRef
	// PR-35o: set of source filenames declared via `SRC(...)` or
	// `SRC_C_NO_LTO(...)`. Upstream `SRC`/`SRC_C_NO_LTO` macros emit a
	// FLAT output path (no `_/` infix even when the source contains a
	// `/`), unlike `SRCS(subdir/foo.cpp)` which emits
	// `<modulePath>/_/subdir/foo.cpp.o`. Compare reference paths:
	//   - SRCS member util/digest/city.cpp → util/_/digest/city.cpp.o
	//   - SRC_C_NO_LTO util/system/compiler.cpp → util/system/compiler.cpp.o
	// emitOneSource consults this set to set
	// ModuleCCInputs.FlatOutput, which composeCCPaths uses to skip the
	// `_/` infix.
	flatSrcs    map[string]struct{}
	// PR-M3-resource-objcopy-A: RESOURCE() / RESOURCE_FILES() pair lists.
	// After collection, `resources` carries the (path, key, kv) triple list
	// that the objcopy packer in resource.go consumes; RESOURCE_FILES are
	// expanded inline at collect time so this slice is the canonical view
	// for the emitter.
	resources []resourceEntry
	// PR-M3-resource-objcopy-B: kv_only objcopy shapes (PY3-only).
	// pyMain captures the `PY_MAIN(<arg>)` macro argument or the
	// `MAIN <src.py>` modifier of `PY_SRCS(...)` — both produce a single
	// `PY_MAIN=<dotted-mod>:<func>` kv per upstream pybuild.py:py_main
	// (build/plugins/pybuild.py:759). Empty when no PY_MAIN-shape is
	// present.
	pyMain string
	// noCheckImports captures the verbatim arg list of
	// `NO_CHECK_IMPORTS(args...)` — used by emitNoCheckImportsObjcopy
	// to build a single `py/no_check_imports/<pathid>=<space-joined>` kv.
	// Args are kept in declaration order (the upstream value used in
	// pathid() and the resfs value join the args by ' ' in that order;
	// see build/plugins/ytest.py:811).
	noCheckImports []string
	// PR-M3-reg3-cpp-py-register: PY_REGISTER(args...) argument list. Each
	// arg is the dotted module name; gen_py3_reg.py(<arg>, <output>) emits a
	// `<arg>.reg3.cpp` source which is then SRCS(GLOBAL …) compiled.
	// Mirror of upstream macro _PY3_REGISTER in build/ymake.core.conf:4086-4089.
	pyRegister []string
	// PR-M3-simd-permutations: per-`SRC_C_<VARIANT>` entries in
	// declaration order. Each entry produces one CC node alongside (and
	// in addition to) any plain SRCS / SRC / SRC_C_NO_LTO listing of the
	// same file. AR-member ordering: emitted entries share the FLAT
	// bucket with SRC()/SRC_C_NO_LTO entries (no `_/` infix), so
	// reorderARMembers hoists them to the front of the archive.
	simdSrcs []simdSrc
	// PR-M3-ragel-flags-per-module: per-module RAGEL6_FLAGS override
	// captured from `SET(RAGEL6_FLAGS <value>)` (upstream
	// build/ymake.core.conf:3284 expands `$RAGEL6_FLAGS ${SRCFLAGS}`
	// before the rest of the ragel6 cmd line). Empty means the
	// platform-default fires inside EmitR6 — `-CG2` on x86_64 host
	// (release, mirroring ymake_conf.py:2274) and `-CT0` on target
	// aarch64 (debug). Empirical M3 case: devtools/ymake/lang/
	// makelists/ya.make sets `-lF1`.
	ragel6Flags []string
	conflictMod *ModuleStmt
	// PR-M3-runprogram-closure: module-level INDUCED_DEPS(<exts> headers...)
	// declarations. Each entry is a SOURCE_ROOT-rooted header path the tool
	// (when this module is a PROGRAM invoked via RUN_PROGRAM) is declared to
	// inject into its generated outputs. Consumed by emitRunProgram to seed
	// the PR output's EmitsIncludes — the scanner then walks the headers'
	// real `#include` graph to reach the full transitive closure.
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
// invocation. Name is the module-relative output filename (e.g.
// "__res.pyc.inc"); DontCompress is set when the DONTCOMPRESS keyword
// appears; Files lists the inputs in declaration order (each is either a
// module-relative source path or the basename of a build-tree artifact
// produced by another macro in the same module — e.g. `__res.pyc`
// produced by a RUN_PROGRAM emit).
type archiveEntry struct {
	Name         string
	DontCompress bool
	Files        []string
}

// antlr4GrammarInfo captures a single RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT
// invocation for later JV node emission.  IsSplit distinguishes the two-grammar
// form (lexer+parser) from the single-grammar form.
// OutputIncludes carries repo-relative headers from the macro's OUTPUT_INCLUDES
// keyword (PR-M3-jv-antlr-system-headers): they are registered as CP `.g4.cpp`
// EmitsIncludes so the CC consumer walks their transitive closure.
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

// collectModule walks `mf.Stmts` (after IF branches have been
// resolved against `env`) and returns a `moduleData` with all
// macros classified. IfStmts are recursively inlined; nested
// JOIN_SRCS / SRCS / PEERDIR / NO_*  inside an IF taken branch are
// processed as if they were top-level. INCLUDE never reaches this
// point (the parser already inlined includes).
//
// The `pathFlags` argument is the path-based heuristic seed; macro
// overlays mutate it in place on the returned moduleData so the
// caller does not need to compose two separate bags.
func collectModule(modulePath string, stmts []Stmt, env Environment, pathFlags FlagSet) *moduleData {
	d := &moduleData{flags: pathFlags}

	collectStmts(modulePath, stmts, env, d)

	applyPython3AddIncl(modulePath, d)
	applyBuildInfoAddIncl(modulePath, d)

	// PR-M3-sparsehash-slot-order: per upstream build/conf/proto.conf:480-491,
	// the _CPP_EVLOG_CMD (and _CPP_PROTO_EVLOG_CMD) macros fired for every
	// `.ev` source append `.PEERDIR=library/cpp/eventlog contrib/libs/protobuf`
	// to the owning module's PEERDIRs. Visit eventlog's transitive chain
	// (blockcodecs → codecs/brotli, codecs/snappy, contrib/libs/re2) from
	// every `.ev`-bearing module — places those peers ahead of sparsehash
	// in the consumer's transitive ADDINCL aggregation.
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

	// PR-M3-protobuf-umbrella-trigger: per upstream build/conf/proto.conf:461-465,
	// the `_CPP_PROTO_CMD(File)` macro fires once per `.proto` source and
	// appends `.PEERDIR=contrib/libs/protobuf` to the owning module. The
	// .ev branch above already covers `_CPP_EVLOG_CMD` / `_CPP_PROTO_EVLOG_CMD`
	// for .ev sources; mirror it here so .proto-only PROTO_LIBRARYs (e.g.
	// library/cpp/eventlog/proto, library/cpp/retry/protos,
	// library/cpp/protobuf/{json,util}/proto) propagate protobuf/src +
	// transitive abseil-cpp{,-tstring} -I to their downstream .pb.cc.o
	// consumers. Guarded on PROTO_LIBRARY only — other module types may
	// declare .proto sources for codegen without compiling them as
	// protobuf-runtime consumers.
	if hasProto && !hasEv && d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" {
		d.peerdirs = append(d.peerdirs, "contrib/libs/protobuf")
	}

	// PR-M3-py-srcs-resource-peerdir: upstream pybuild plugin processes
	// PY_SRCS / PY_REGISTER by emitting `RESOURCE(...)` calls (one per
	// .py source and per PY_REGISTER argument) — each `RESOURCE()` macro
	// invocation expands to `PEERDIR(library/cpp/resource)`
	// (build/ymake.core.conf:522-524). Mirror that side effect here.
	// Verified against `/home/pg/monorepo/yatool_orig/g` line 27293
	// where `library/python/symbols/module` (PY23_LIBRARY with only
	// PY_REGISTER + PY_SRCS, no explicit PEERDIRs in ya.make) carries
	// `$S/library/cpp/resource` as its first user-direct peer.
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

// applyPython3AddIncl mirrors the `when ($USE_ARCADIA_PYTHON == "yes")`
// branch of `_PYTHON3_ADDINCL` (build/conf/python.conf:1018-1023):
// `CFLAGS+=-DUSE_PYTHON3` plus `ADDINCL+=GLOBAL $PY3_BASE_INCLUDE_DIR`
// (= contrib/libs/python/Include per python.conf:96). Invoked by PY3-family
// module types and by `USE_PYTHON3()` (python.conf:738-739, 862, 1064, 1250).
//
// Empirically the reference places `-DUSE_PYTHON3` at the AutoPeerCFlags
// cmd_args slot — right after `-D_musl_`, before the second noLibcUndebug
// block copy (e.g. library/python/runtime_py3/__res.cpp.o ref:93,
// library/cpp/pybind/cast.cpp.py3.o ref:83) — even when the module declares
// `NO_PYTHON_INCLUDES()` and therefore has no peer to `contrib/libs/python`
// to propagate the flag from. We inject `-DUSE_PYTHON3` via
// `defaultPeerCFlags` so it lands at that slot, and we set `d.usePython3`
// here for `defaultPeerCFlags` to read. The `contrib/libs/python/Include`
// path goes to BOTH `d.addInclGlobal` (peer-propagated) AND `d.addIncl`
// (own ADDINCL slot), mirroring the `ADDINCL(GLOBAL X)` collector path
// (gen.go:918-919).
//
// `contrib/libs/python` itself emits these via its own ya.make IF-block
// (`ADDINCL(GLOBAL Include)` + `CFLAGS(GLOBAL -DUSE_PYTHON3)`), so skip it
// to avoid double-emit and to mirror the same cycle-guard pattern used by
// the PY*_LIBRARY auto-peerdir code at the genModule call site (line 2104).
//
// NO_PYTHON_INCLUDES() does NOT gate this injection: upstream gates only
// the implicit `PEERDIR+=contrib/libs/python` (python.conf:741-743), not
// the `_PYTHON3_ADDINCL` invocation itself. Empirical: library/python/
// runtime_py3 declares NO_PYTHON_INCLUDES() yet its CC nodes carry
// `-DUSE_PYTHON3` and `-I$(S)/contrib/libs/python/Include`.
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

	// Normalise: every code path downstream (e.g. `defaultPeerCFlags`'s
	// AutoPeerCFlags slot injection) reads `d.usePython3` rather than
	// re-checking the module-type set.
	d.usePython3 = true

	// `-DUSE_PYTHON3` is injected via `defaultPeerCFlags` so it lands at
	// the AutoPeerCFlags cmd_args slot (between catboost-redux and the
	// second noLibcUndebugBlock copy), matching the empirical reference
	// position (e.g. runtime_py3/__res.cpp.o ref:93, pybind/cast.cpp.py3.o
	// ref:83). Adding it to `d.cFlagsGlobal` instead would land it inside
	// the ownCFlags slot (position ~59), which mismatches the reference.
	d.addInclGlobal = append(d.addInclGlobal, "contrib/libs/python/Include")
	d.addIncl = append(d.addIncl, "contrib/libs/python/Include")

	// ARCHIVE(NAME ...) in library/python/runtime_py3 auto-injects
	// `${addincl;noauto;output:NAME}` per ymake.core.conf:4143. The
	// path is owner-scoped (own slot for runtime_py3) AND peer-propagated
	// to USE_PYTHON3 consumers. Owner gets it in d.addIncl (own).
	// Consumers see it via genModule's post-merge splice (placed AFTER
	// abseil-cpp).
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
// definition in build/conf/python.conf invokes `_PYTHON3_ADDINCL` (directly
// or via `_ARCADIA_PYTHON3_ADDINCL` / `PYTHON3_ADDINCL`):
//   - PY3_LIBRARY (line 738-739)
//   - PY3_PROGRAM_BIN / PY3_PROGRAM / _BASE_PY3_PROGRAM (line 862)
//   - PY23_LIBRARY's PY3 sub-module (inherits via PY3_LIBRARY)
//   - PY23_NATIVE_LIBRARY's PY3 sub-module (line 1250: PYTHON3_ADDINCL())
//
// PY2_LIBRARY / PY2_PROGRAM are intentionally excluded — they invoke
// `_ARCADIA_PYTHON_ADDINCL` (no "3"; python.conf:695), which is the
// Python2 variant and does not emit `-DUSE_PYTHON3`.
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
			// M3: SRCS(GLOBAL foo.cpp) uses GLOBAL as a per-source
			// modifier meaning the source's symbols are exported globally
			// (equivalent to GLOBAL_SRCS). PR-41+ upstream introduced
			// this inline variant. Strip GLOBAL tokens and route the
			// following sources to d.globalSrcs (PR-M3-A: treat the
			// same as regular srcs since EmitARGlobal handles global
			// archives; the correct routing matches GLOBAL_SRCS).
			globalNext := false

			for _, src := range v.Sources {
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
			// PR-M3-final-surgical (fix 3): the ADDINCL modifier on the
			// immediately-following peerdir path means "peer this AND add
			// the same path to this module's own ADDINCL list". Drives
			// `PEERDIR(ADDINCL contrib/libs/protobuf …)` in
			// tools/event2cpp/bin/ya.make, which feeds a CC -I slot for
			// the consumer's compile of proto_events.cpp.
			addInclNext := false
			for _, p := range v.Paths {
				// Skip unexpanded variable references (e.g. ${STUB_PEERDIRS}).
				// These appear in some ya.make files as SET-driven optional peerdirs
				// that resolve to empty in the standard open-source build. The walker
				// has no SET evaluator, so variable-ref paths would cause a
				// "no such file" failure; skipping them is the correct M3 behaviour.
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
			// SET is parsed but PR-25 has no evaluator. The taken
			// IF branches above already flattened any conditional
			// SET; an unconditional SET that influences downstream
			// IFs would need a real macro evaluator (PR-26+).
			//
			// PR-M3-ragel-flags-per-module: capture `SET(RAGEL6_FLAGS
			// <value>)` so emitOneSource can thread the override into
			// EmitR6. Upstream `_SRC("rl6", ...)` (build/ymake.core.conf:
			// 3284) interpolates `$RAGEL6_FLAGS` before everything else,
			// so a SET replaces the default and is not additive to other
			// SETs in the same module (last-write-wins). Empirical M3
			// witness: devtools/ymake/lang/makelists/ya.make:6 sets
			// `-lF1`, producing the ragel6 cmd_args[1] observed in the
			// reference graph's `makefile_lang.rl6.cpp` node.
			if v.Name == "RAGEL6_FLAGS" {
				d.ragel6Flags = []string{v.Value}
			}
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			d.joinSrcs = append(d.joinSrcs, v)
		case *AddInclStmt:
			// PR-31 D04/D13: GLOBAL paths peer-propagate to consumers
			// via the PEERDIR walk (kept in `d.addInclGlobal`).
			// PR-33 D02: own-cmd_args emission uses `d.addIncl` which
			// includes BOTH GLOBAL and non-GLOBAL paths in declaration
			// order — empirically the reference graph emits a module's
			// own GLOBAL ADDINCL paths on the module's own CC compiles
			// (libcxx algorithm.cpp.o cmd_args[9..11] shows
			// `libcxx/include` + `libcxx/src` + `libcxxrt/include` in
			// stmt declaration order, where include and libcxxrt/include
			// are GLOBAL and src is non-GLOBAL).
			//
			// PR-M3-cmd-arg-slot-ordering: append AllPaths (positional
			// declaration order across the GLOBAL split) instead of
			// "GLOBAL-then-OWN", which mis-orders modules whose ya.make
			// interleaves bare and GLOBAL paths (libffi, base64, ragel5
			// peer modules) — see AddInclStmt.AllPaths doc.
			d.addInclGlobal = append(d.addInclGlobal, v.GlobalPaths...)
			d.addIncl = append(d.addIncl, v.AllPaths...)
		case *CFlagsStmt:
			// PR-32 D04: GLOBAL flags peer-propagate to consumers via
			// PEERDIR (kept in `d.cFlagsGlobal`); non-GLOBAL flags apply
			// to this module's own C+C++ sources only (kept in
			// `d.cFlags`). PR-33 D02 emits the GLOBAL set separately on
			// the module's own CC compiles via the bucket model in
			// composeTargetCC / composeHostCC (own GLOBAL ∪ peer
			// GLOBAL slot, twice flanking the catboost-redux).
			d.cFlagsGlobal = append(d.cFlagsGlobal, v.GlobalFlags...)
			d.cFlags = append(d.cFlags, v.OwnFlags...)
		case *CXXFlagsStmt:
			// PR-32 D05: GLOBAL CXXFLAGS peer-propagate to consumers'
			// C++ compiles (kept in `d.cxxFlagsGlobal`); non-GLOBAL
			// CXXFLAGS apply to this module's own C++ sources only
			// (kept in `d.cxxFlags`). PR-33 D02 emits the GLOBAL set
			// separately on own compiles via the bucket model.
			d.cxxFlagsGlobal = append(d.cxxFlagsGlobal, v.GlobalFlags...)
			d.cxxFlags = append(d.cxxFlags, v.OwnFlags...)
		case *CONLYFlagsStmt:
			// PR-32 D06: GLOBAL CONLYFLAGS peer-propagate to consumers'
			// C / .S compiles (kept in `d.cOnlyFlagsGlobal`); non-GLOBAL
			// CONLYFLAGS apply to this module's own C / .S sources only
			// (kept in `d.cOnlyFlags`). PR-33 D02 emits GLOBAL via the
			// bucket model.
			d.cOnlyFlagsGlobal = append(d.cOnlyFlagsGlobal, v.GlobalFlags...)
			d.cOnlyFlags = append(d.cOnlyFlags, v.OwnFlags...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, v.Flags...)
		case *SrcDirStmt:
			// SRCDIR shifts source resolution base. PR-28-D02 threads d.srcDir
			// into emitOneSource so per-source CC/AS/R6 nodes rebase to <srcDir>;
			// JOIN_SRCS / EmitJS gap was closed by PR-28-D11. LD/AR remain at
			// instance.Path (semantic difference: the binary/archive lives where
			// declared, even if its sources are elsewhere).
			d.srcDir = v.Dir
		case *GlobalSrcsStmt:
			d.globalSrcs = append(d.globalSrcs, v.Sources...)
		case *GenerateEnumSerializationStmt:
			d.enumSrcs = append(d.enumSrcs, v)
		case *DefaultVarStmt:
			// PR-M3-E: track DEFAULT(name value) for $CFG_VARS expansion.
			if d.defaultVars == nil {
				d.defaultVars = map[string]string{}
			}
			if _, exists := d.defaultVars[v.VarName]; !exists {
				d.defaultVars[v.VarName] = v.Value
				d.defaultVarOrder = append(d.defaultVarOrder, v.VarName)
			}
			// PR-M3-pcre-jit-default-eval: also bridge the binding into the
			// per-module IF env so subsequent `IF (NAME)` / `IF (NAME == "v")`
			// predicates evaluated in this collectStmts walk see the value.
			// Mirrors upstream TEvalContext::SetStatement's NMacro::DEFAULT
			// branch (devtools/ymake/lang/eval_context.cpp:335-339): only
			// sets when the variable has no prior binding. Value typed as
			// string so bare-ident `IF (NAME)` coerces via Environment.Bool
			// (empty → false, non-empty → true) and `IF (NAME == "yes")`
			// matches via string equality. The env is the per-module clone
			// from buildIfEnv, so the mutation does not leak across modules.
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
			// PR-M3-resource-objcopy-A: expand RESOURCE_FILES into
			// resource entries per `build/plugins/res.py:onresource_files`.
			// For each path P (after DONT_COMPRESS / PREFIX / DEST / STRIP
			// keywords are processed), append:
			//   - kv-only entry: Path="-", Key=resfs/src/resfs/file/<key>=${rootrel;context=TEXT;input=TEXT:"<P>"}
			//   - source entry:  Path=<P>, Key=resfs/file/<key>
			// The ${rootrel;...} placeholder is preserved verbatim because
			// the hash formula (resource.go:objcopyHash) requires the
			// pre-expansion form (verified against REF
			// `devtools/ymake/contrib/python-rapidjson` objcopy hash).
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

// applyUnknownStmt routes an UnknownStmt by name. The five flag-
// flipping macros (NO_LIBC / NO_UTIL / NO_RUNTIME / NO_PLATFORM /
// NO_COMPILER_WARNINGS) override the inferFlagsFromPath heuristic.
// `ALLOCATOR(NAME)` is resolved to an implicit PEERDIR addition per
// `build/ymake.core.conf:961-1035` (PR-28 / D12). Anything else must
// be in the metadata whitelist; an unknown name throws so a new
// ya.make macro surfaces immediately rather than being silently
// dropped (D27 discipline extended to UnknownStmts).
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
	case "NO_PYTHON_INCLUDES":
		// PR-M3-aarch64-py-closure: NO_PYTHON_INCLUDES() sets NO_PYTHON_INCLS=yes
		// per build/conf/python.conf:928-929 (macro definition). The PY*_LIBRARY
		// implicit `when ($NO_PYTHON_INCLS != "yes") { PEERDIR+=contrib/libs/python }`
		// at python.conf:741-743 is gated by this; we capture the flip here so
		// the implicit-peer code in genModule skips contrib/libs/python for
		// modules that declare NO_PYTHON_INCLUDES (e.g. library/python/runtime_py3,
		// library/python/symbols/module).
		d.noPythonIncl = true
	case "ALLOCATOR":
		applyAllocatorStmt(v, d)
	case "ARCHIVE":
		// PR-M3-unpaired-got-closure: parse `ARCHIVE(NAME <out>
		// [DONTCOMPRESS] files...)` (upstream
		// build/ymake.core.conf:4142-4145). The NAME keyword expects
		// exactly one following argument; DONTCOMPRESS is a bare flag;
		// remaining positional args are the input files.
		applyArchiveStmt(v, d)
	case "ENABLE":
		// PR-30 D02: track ENABLE(MUSL_LITE) so the
		// defaultProgramPeerdirsFor decision sees the per-module
		// flip. yasm declares ENABLE(MUSL_LITE) inside its IF(MUSL)
		// branch; without this hook yasm pulls musl/full and the
		// resulting cross-PROGRAM cycle (yasm → musl/full →
		// asmlib's .asm sources → yasm) blows the cycle counter.
		// PR-M3-A: track ENABLE(PYBUILD_NO_PYC) so emitPySrcs
		// suppresses yapyc3 node emission for modules like
		// contrib/tools/python3/lib2/py that declare all Python
		// sources but do not want .pyc/.yapyc3 files generated.
		for _, a := range v.Args {
			if a == "MUSL_LITE" {
				d.muslLite = true
			}
			if a == "PYBUILD_NO_PYC" {
				d.pyBuildNoPYC = true
			}
			// PR-M3-resource-objcopy-C: PYBUILD_NO_PY (without the 'C')
			// is a separate flag — used by contrib/tools/python3/Lib —
			// that suppresses the raw `.py` resfs embedding while still
			// running yapyc3 compilation. Lib also has ENABLE(PYBUILD_NO_PY)
			// declared at the top of its ya.make.
			if a == "PYBUILD_NO_PY" {
				d.pyBuildNoPY = true
			}
		}
	case "SRC":
		// PR-35o: SRC(filename [extra_cflags...]) is a SRCS variant
		// that registers a single source AND attaches per-source extra
		// CFLAGS to that source's compile. The first arg is the
		// filename; remaining args are flag tokens (e.g. -DSSE41_STUB)
		// appended to the compile cmd_args at the per-source slot
		// (right before the input path), matching the reference for
		// `util/charset/wide_sse41.cpp.o`. SRC() with no args throws.
		// SRC's output path is FLAT (no `_/` infix) — see flatSrcs in
		// moduleData.
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
		// PR-35o: SRC_C_NO_LTO(filename) is a SRCS variant that
		// disables LTO for the named source. The reference cmd_args
		// for `util/system/compiler.cpp.o` show no LTO-specific
		// flag delta (LTO is already off in M2's debug build), so
		// this reduces to plain SRCS in the current closure.
		// Output path is FLAT (no `_/` infix) — see flatSrcs in
		// moduleData.
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
		// PR-M3-simd-permutations: SRC_C_<V>(filename [extra_flags...])
		// emits one CC node compiling `filename` with the variant's
		// `-m<flag>` bundle plus the extras, into a FLAT
		// `<src>.<variant>.pic.o` output. The cmd_args layout reuses the
		// existing PerSourceCFlags slot (between macroPrefixMapFlags and
		// the input path). Per `build/ymake.core.conf:3848-3923`, each
		// macro expands to `_SRC_CUSTOM_C_CPP(... $FILE .<v> $<V>_CFLAGS
		// $FLAGS)` — the variant CFLAGS come first, then the macro's
		// trailing arguments.
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
		// PR-35k: LD_PLUGIN(name.py) declares a python plugin to be
		// passed to the linker via `--start-plugins ... --end-plugins`
		// in every consumer PROGRAM's LD cmd_args. The named file is
		// copied (via a CP node) from `$(S)/<modulePath>/name.py`
		// to `$(B)/<modulePath>/name.py.pyplugin` at gen time.
		// Multiple args (multiple plugins) are accepted; each is
		// recorded verbatim and emitted as a separate CP node by the
		// owning module's `genModule` call. Only `contrib/libs/musl/
		// include` declares this in M2 (`LD_PLUGIN(musl.py)`).
		d.ldPlugins = append(d.ldPlugins, v.Args...)
	case "AR_PLUGIN":
		// PR-M3-openssl-ar-plugin-and-as-clean: AR_PLUGIN(name) registers
		// a python plugin for the module's AR step. Upstream macro
		// `AR_PLUGIN` (ymake.core.conf:3396-3398) does
		// `SET(_AR_PLUGIN $name.pyplugin)`; ld.conf:366-368 then injects
		// `--plugin ${input:_AR_PLUGIN}` between the inner `-- ... --`
		// separators of `_LD_ARCHIVER` and adds the plugin path to
		// `inputs`. Only `contrib/libs/openssl`'s `AR_PLUGIN(ar)` fires
		// in the M3 closure.
		if len(v.Args) != 1 {
			ThrowFmt("gen: AR_PLUGIN expects exactly 1 argument, got %d", len(v.Args))
		}
		d.arPlugin = v.Args[0] + ".pyplugin"
	case "USE_PYTHON3":
		// M3: USE_PYTHON3() adds implicit PEERDIRs to the Python 3 runtime
		// per build/conf/python.conf macro USE_PYTHON3 (python.conf:1064-1071):
		//   PEERDIR(contrib/libs/python)
		//   when ($USE_ARCADIA_PYTHON == "yes"): PEERDIR+=library/python/runtime_py3
		// The walker does not evaluate conf macros, so we hardcode the peers
		// here. PR-M3-use-python3-peer-split: contrib/tools/python3 and
		// contrib/tools/python3/Lib are NOT in upstream's USE_PYTHON3 macro
		// — they come from the PYTHON3_MODULE() macro (python.conf:644-647),
		// which is invoked inside PY_ANY_MODULE-shaped modules and gated by
		// USE_ARCADIA_PYTHON && (MSVC || IS_CROSS_TOOLS). Plain PROGRAM /
		// LIBRARY callers of USE_PYTHON3() (e.g. devtools/ymake,
		// devtools/ymake/bin) MUST NOT pick them up directly; they reach
		// the python3 tool transitively via contrib/libs/python's own peer
		// list (IF (USE_ARCADIA_PYTHON) ELSE branch: PEERDIR(contrib/tools/
		// python3, contrib/tools/python3/Lib) in contrib/libs/python/ya.make).
		// Adding them again at the USE_PYTHON3 site pulled their transitive
		// addincl set (lzma/openssl/libffi) into the peer-AddInclGlobal slot
		// BEFORE contrib/libs/python's own python/Include, mismatching the
		// reference cmd_args[21] cluster on ~158 nodes.
		d.peerdirs = append(d.peerdirs,
			"contrib/libs/python",
			"library/python/runtime_py3",
		)
		// PR-M3-python-addincl-cflags: USE_PYTHON3() also invokes
		// `_ARCADIA_PYTHON3_ADDINCL` → `_PYTHON3_ADDINCL` (python.conf:1064)
		// whose `when ($USE_ARCADIA_PYTHON == "yes")` branch adds
		// `CFLAGS+=-DUSE_PYTHON3` and `ADDINCL+=GLOBAL contrib/libs/python/Include`.
		// `collectModule`'s post-pass (`applyPython3AddIncl`) performs that
		// injection; we just record the request here.
		d.usePython3 = true
	case "PY_SRCS":
		// PR-M3-A: collect PY_SRCS python source files into d.pySrcs.
		// PY_SRCS accepts optional leading/per-source modifiers TOP_LEVEL
		// and MAIN. TOP_LEVEL sets namespace to "" for subsequent paths
		// (default ns is `<modulePath-dotted>.`).  MAIN flags the next
		// path as the program entry point; in py3 mode this causes
		// pybuild.py:py_main(unit, mod + ":main") to emit a
		// `PY_MAIN=<dotted-mod>:main` kv resource (pybuild.py:362-396).
		// We capture pyMain at parse time; resource.go consumes it.
		topLevel := false
		mainNext := false
		for _, a := range v.Args {
			switch a {
			case "TOP_LEVEL":
				topLevel = true
				d.pyTopLevel = true
				continue
			case "MAIN":
				mainNext = true
				continue
			}
			d.pySrcs = append(d.pySrcs, a)
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
				modName := strings.TrimSuffix(a, ".py")
				modName = strings.ReplaceAll(modName, "/", ".")
				d.pyMain = ns + modName + ":main"
				mainNext = false
			}
		}
	case "PY_MAIN":
		// PR-M3-resource-objcopy-B: PY_MAIN(<arg>) macro per upstream
		// pybuild.py:onpy_main (build/plugins/pybuild.py:762). Argument
		// gets normalised: `/` → `.`, and a `:main` suffix is appended
		// when the arg has no colon. Multiple PY_MAIN(...) on the same
		// module would each emit a separate resource entry, but the M3
		// closure contains only single-PY_MAIN modules — we keep one.
		if len(v.Args) != 1 {
			ThrowFmt("gen: PY_MAIN expects exactly 1 argument, got %d", len(v.Args))
		}
		arg := strings.ReplaceAll(v.Args[0], "/", ".")
		if !strings.Contains(arg, ":") {
			arg += ":main"
		}
		d.pyMain = arg
	case "NO_CHECK_IMPORTS":
		// PR-M3-resource-objcopy-B: NO_CHECK_IMPORTS(args...) per upstream
		// ytest.py:on_register_no_check_imports (build/plugins/ytest.py:808).
		// The args are joined by ' ' in declaration order; that string is
		// the resfs value AND the input to _common.pathid() (md5 →
		// lower-cased unpadded base32). Empty arg list = no-op (no kv).
		if len(v.Args) > 0 {
			d.noCheckImports = append(d.noCheckImports, v.Args...)
		}
	case "PY_REGISTER":
		// PR-M3-reg3-cpp-py-register: capture PY_REGISTER(args...) dotted
		// module names. emitPyRegister later emits one PY (gen_py3_reg.py)
		// node generating `<arg>.reg3.cpp` plus a CC compiling it; both
		// flow into the module's `.global.a` (mirror of the upstream
		// SRCS(GLOBAL $Func.reg3.cpp) inside macro _PY3_REGISTER at
		// build/ymake.core.conf:4086-4089).
		d.pyRegister = append(d.pyRegister, v.Args...)
		// PR-M3-final-surgical (fix 4): mirror pybuild.py:740-750 — for
		// each dotted PY_REGISTER argument inject the two -D macro
		// renames so every CC in the same module compiles with them.
		for _, name := range v.Args {
			dot := strings.LastIndexByte(name, '.')
			if dot < 0 {
				continue
			}
			shortname := name[dot+1:]
			// mangle: "a.b.c" → "1a1b1c" (len(seg)+seg per segment).
			var mangled strings.Builder
			for _, seg := range strings.Split(name, ".") {
				fmt.Fprintf(&mangled, "%d%s", len(seg), seg)
			}
			d.cFlags = append(d.cFlags,
				"-DPyInit_"+shortname+"=PyInit_"+mangled.String(),
				"-Dinit_module_"+shortname+"=init_module_"+mangled.String(),
			)
		}
	case "SET_APPEND":
		// PR-M3-openssl-as-cflags: SET_APPEND(<var> <values...>) is
		// ymake's append-to-variable macro. Only SFLAGS is wired today
		// (openssl/crypto/ya.make.inc:179-186's
		// `IF (ARCH_X86_64 AND NOT MSVC) { SET_APPEND(SFLAGS -mavx512bw
		// -mavx512ifma -mavx512vl) }`); SFLAGS is the assembler flag
		// bundle threaded between CFLAGS and `-c -o` in AS cmd_args
		// (ymake.core.conf:3217). Other targets currently no-op.
		if len(v.Args) >= 2 && v.Args[0] == "SFLAGS" {
			d.sFlags = append(d.sFlags, v.Args[1:]...)
		}
	case "INDUCED_DEPS":
		// PR-M3-runprogram-closure: capture INDUCED_DEPS(<ext-filter> headers...)
		// declared at module level. First arg is the extension filter
		// (e.g. `h+cpp`, `h`) identifying which generated output kinds the
		// listed headers apply to; remaining args are ${ARCADIA_ROOT}-
		// rooted header paths. ymake/conf/index.yaml syntax: see
		// `build/conf/_decl.conf` for the source-of-truth spec. We strip
		// the leading `${ARCADIA_ROOT}/` prefix so the stored paths are
		// repo-relative (SOURCE_ROOT-relative); the consumer rebases.
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

// allocatorPeers maps `ALLOCATOR(<name>)` arguments to the implicit
// PEERDIR additions per `build/ymake.core.conf:961-1035`. Each name
// resolves to zero or more peer paths appended to the module's
// PEERDIR list. PR-28 ships the M2-relevant subset; entries with
// resolved == nil intentionally add no peer (FAKE /
// allocator-already-handled-elsewhere).
//
// ALLOCATOR(SYSTEM) unconditionally adds library/cpp/malloc/system per
// build/ymake.core.conf:1038-1040 (`when ($ALLOCATOR == "SYSTEM")`).
// The MUSL gate at lines 954-958 applies to the select($ALLOCATOR)
// block, NOT to this when-clause.
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
// per upstream build/ymake.core.conf:4142-4145. NAME is a required
// keyword followed by exactly one argument (the output filename);
// DONTCOMPRESS is a bare flag that maps to the archiver's `-p` switch;
// the remaining positional args are the inputs in declaration order.
// Throws on a missing or malformed NAME — there is no sensible default
// output name for this shape.
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

	// PR-43: allocator peers are inserted into the program-default slot
	// (between build/cow/on and musl/full) by defaultProgramPeerdirsFor,
	// NOT into d.peerdirs (user-peer slot). Appending to d.peerdirs caused
	// the mimalloc cluster to land after musl/full's transitive closure
	// (asmlib/asmglibc/musl) in the LD archive list, reversing the
	// REF order for ragel6's ALLOCATOR(MIM) case.
	d.hadAllocator = true
	d.allocatorName = name
}

// isMultimoduleLibraryType returns true for module-declaration names that
// are NOT "LIBRARY" or "PROGRAM" but are treated as LIBRARY-shaped stubs
// in PR-M3-A. These include Python-binding native libraries, Python
// libraries, and proto libraries. Their C/C++ sources (when present) are
// compiled as normal LIBRARY sources; their non-C sources (*.py, *.proto)
// are skipped (header-only path). PR-M3-B..E introduce real emitters for
// the PY/PB/PR node kinds.
// isPyLibraryType returns true for Python library/program module names that
// behave as LIBRARY-shaped modules (emit AR/CC for their C++ SRCS, header-only
// when they have none). Unlike the multimodule types in isMultimoduleLibraryType,
// these modules are NOT unconditionally header-only — hasCompilableSource gates
// the path. They are separated so the gate check at the top of genModule can
// admit them without routing every one of them to the header-only path.
func isPyLibraryType(name string) bool {
	switch name {
	case "PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
}

// isPROGRAMForMuslDef reports whether the module type behaves as a
// terminal PROGRAM for the purposes of the `-D_musl_=1` ownCFlags
// injection. PROGRAM plus PY3_PROGRAM_BIN both link a final binary via
// EmitLD and observe the same musl-self CFLAG in the reference graph
// (tools/py3cc/slow/py3cc cmd[1] ref:44 — `-D_musl_=1` precedes the
// peer-GLOBAL CFLAGS block; PR-M3-final-LD-trailer-and-cflags).
func isPROGRAMForMuslDef(name string) bool {
	switch name {
	case "PROGRAM", "PY3_PROGRAM_BIN":
		return true
	}

	return false
}

// pyLibraryAutoPythonPeer returns true for Python module types whose
// upstream definition in build/conf/python.conf auto-PEERDIRs
// contrib/libs/python (gated by NO_PYTHON_INCLUDES). The set is a
// strict subset of isPyLibraryType — PY23_NATIVE_LIBRARY is excluded
// because its PY2/PY3 sub-modules inherit from plain LIBRARY (not
// PY*_LIBRARY) and so do not pick up the implicit peer upstream.
// PY2_PROGRAM/PY3_PROGRAM are kept in step with PY3_PROGRAM_BIN
// because _BASE_PY3_PROGRAM (their base) carries the same implicit peer.
func pyLibraryAutoPythonPeer(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY", "PY3_PROGRAM_BIN",
		"PY2_PROGRAM", "PY3_PROGRAM":
		return true
	}

	return false
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

// buildIfEnv constructs the per-instance bound-variable environment
// for IF predicates. The base set is `DefaultIfEnv` (M2 default =
// aarch64 / linux / clang / musl). For host instances (Flags.PIC),
// flip ARCH_AARCH64↔ARCH_X86_64 so the same ya.make produces the
// other architecture's branches. The result is a fresh Environment;
// the caller is free to mutate it.
//
// PR-35o: ARCH_ARM64 is the upstream alias for ARCH_AARCH64 (Arcadia
// sets both together). Flip it alongside ARCH_AARCH64 so any
// `IF (ARCH_ARM64 ...)` predicate sees the same binding as
// `ARCH_AARCH64` — required for `contrib/libs/cxxsupp/builtins`'s
// bf16 SRCS block whose gate uses `ARCH_ARM64 OR ARCH_X86_64`.
func buildIfEnv(instance ModuleInstance) Environment {
	env := DefaultIfEnv.Clone()

	if instance.Platform.Target == PlatformDefaultLinuxX8664 {
		env.SetBool("ARCH_AARCH64", false)
		env.SetBool("ARCH_ARM64", false)
		env.SetBool("ARCH_X86_64", true)
	}

	if instance.Platform.Target == PlatformDefaultLinuxAArch64 {
		env.SetBool("ARCH_AARCH64", true)
		env.SetBool("ARCH_ARM64", true)
		env.SetBool("ARCH_X86_64", false)
	}

	return env
}

// derivePeerInstance constructs the peer module's ModuleInstance.
// The peer inherits the parent's Language and Target and the PIC
// axis (host-tool peers stay on host); its FlagSet is seeded from
// `inferFlagsFromPath(peerPath, parent.PIC)` and macro-overlaid by
// `genModule` itself (so the peer's flag bag reflects its own
// ya.make's NO_LIBC / NO_UTIL declarations). Macro overlay happens
// inside `genModule` because that is where the peer's ya.make is
// parsed; this helper only builds the cycle-detection key.
func derivePeerInstance(parent ModuleInstance, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Language: parent.Language,
		Platform: parent.Platform,
		// Pass platform identity rather than the bare PIC flag so
		// inferFlagsFromPath seeds the peer's PIC from the parent's
		// platform identity, not Flags.PIC directly.
		Flags: inferFlagsFromPath(peerPath, parent.Platform.Target == PlatformDefaultLinuxX8664),
	}
}
