package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyUnknownStmt_ExcludeTagsAcceptsTagNames(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	d := &ModuleData{}

	// EXCLUDE_TAGS args are submodule tag names (data), not service-keywords, so
	// the audit must not reject an unmodelled one. Build the tag at runtime: a
	// literal would be mined into knownServiceTokens (the audit embeds *.go,
	// _test.go included) and mask the very check this test exercises.
	tag := "PY" + "_" + "PROTO"

	err := try(func() {
		applyUnknownStmt(nil, "mod", &UnknownStmt{Name: tokExcludeTags, Args: STRS(tag)}, d, env)
	})

	if err != nil {
		t.Fatalf("EXCLUDE_TAGS(%s) rejected: %v", tag, err)
	}

	if !d.excludeTags[internStr(tag)] {
		t.Fatalf("excludeTags = %v, want %s", d.excludeTags, tag)
	}
}

func TestApplyUnknownStmt_AddInclSelf(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	// ADDINCLSELF() adds -I<own source dir> (ADDINCL += ${MODDIR}). It must
	// resolve to Source(<modulePath>).
	d := &ModuleData{}
	applyUnknownStmt(nil, "contrib/libs/foo", &UnknownStmt{Name: internTok("ADDINCLSELF")}, d, env)
	d.materializeAddIncl() // local ADDINCL is priority-tagged then flattened into addIncl

	want := source("contrib/libs/foo")
	found := false

	for _, v := range d.addIncl {
		if v == want {
			found = true
		}
	}

	if !found {
		t.Fatalf("ADDINCLSELF(): d.addIncl = %v, want it to contain %v", d.addIncl, want)
	}

	// ADDINCLSELF(FOR cython) routes the own dir to the cython bucket.
	dc := &ModuleData{}
	applyUnknownStmt(nil, "contrib/libs/bar", &UnknownStmt{Name: internTok("ADDINCLSELF"), Args: STRS("FOR", "cython")}, dc, env)

	if len(dc.cythonAddIncl) != 1 || dc.cythonAddIncl[0] != source("contrib/libs/bar") {
		t.Fatalf("ADDINCLSELF(FOR cython): cythonAddIncl = %v, want [%v]", dc.cythonAddIncl, source("contrib/libs/bar"))
	}
}

// internalContourPlatform builds a platform whose Flags omit OPENSOURCE, so
// buildIfEnv reaches the upstream HAVE_MKL default rather than the opensource
// HAVE_MKL=no override (testTargetP/testHostP both carry OPENSOURCE=yes).
func internalContourPlatform(os OS, isa ISA, sanitizer string) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		if k == "OPENSOURCE" {
			continue
		}
		flags[k] = v
	}
	if sanitizer != "" {
		flags["SANITIZER_TYPE"] = sanitizer
	}
	return newPlatform(newMemFS(nil), os, isa, flags, "", "")
}

// opensourceHaveMklYesPlatform builds an OPENSOURCE=yes platform that *also*
// carries an explicit HAVE_MKL=yes flag. It pins the override ordering: the
// opensource override is unconditional and applied after the default guard, so
// it must beat an existing HAVE_MKL=yes binding rather than skip an already-set
// HAVE_MKL.
func opensourceHaveMklYesPlatform(os OS, isa ISA) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["HAVE_MKL"] = "yes"
	return newPlatform(newMemFS(nil), os, isa, flags, "", "")
}

// TestBuildIfEnv_HaveMklFollowsUpstreamEnv pins the upstream HAVE_MKL default
// (yes iff OS_LINUX && ARCH_X86_64 && !SANITIZER_TYPE, forced no under
// OPENSOURCE) and proves an IF(HAVE_MKL) module selects the MKL PEERDIR exactly
// when the configured environment selects MKL.
func TestBuildIfEnv_HaveMklFollowsUpstreamEnv(t *testing.T) {
	const mklPeer = "contrib/libs/intel/mkl"
	const fallbackPeer = "contrib/libs/clapack/part1"

	mklYaMake := `LIBRARY()
NO_UTIL()
NO_RUNTIME()
IF (HAVE_MKL)
PEERDIR(` + mklPeer + `)
ELSE()
PEERDIR(` + fallbackPeer + `)
ENDIF()
END()
`

	tests := []struct {
		name     string
		platform *Platform
		wantMkl  bool
	}{
		{"linux_x86_64_internal", internalContourPlatform(OSLinux, ISAX8664, ""), true},
		{"linux_aarch64_internal", internalContourPlatform(OSLinux, ISAAArch64, ""), false},
		{"linux_x86_64_sanitizer", internalContourPlatform(OSLinux, ISAX8664, "address"), false},
		{"linux_x86_64_opensource", newTestPlatform(OSLinux, ISAX8664, "no"), false},
		{"linux_x86_64_opensource_have_mkl_flag", opensourceHaveMklYesPlatform(OSLinux, ISAX8664), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := buildIfEnv(ModuleInstance{Kind: KindLib, Platform: tc.platform})

			if got := env.bool(envHAVE_MKL); got != tc.wantMkl {
				t.Fatalf("HAVE_MKL = %v, want %v", got, tc.wantMkl)
			}

			fs := newMemFS(map[string]string{"contrib/libs/cblas/ya.make": mklYaMake})
			mf := throw2(parseFile(fs, "contrib/libs/cblas/ya.make"))
			d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "contrib/libs/cblas", KindLib, mf.Stmts, env)

			hasMkl, hasFallback := false, false
			for _, p := range d.peerdirs {
				switch p.string() {
				case mklPeer:
					hasMkl = true
				case fallbackPeer:
					hasFallback = true
				}
			}

			if hasMkl != tc.wantMkl || hasFallback == tc.wantMkl {
				t.Fatalf("peerdirs=%v: MKL=%v fallback=%v, want MKL=%v fallback=%v",
					d.peerdirs, hasMkl, hasFallback, tc.wantMkl, !tc.wantMkl)
			}
		})
	}
}

func TestApplyUnknownStmt_LLVMBCRequiresConfiguredVersion(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	err := try(func() {
		applyUnknownStmt(nil, "mod", &UnknownStmt{Name: tokLlvmBc, Args: STRS("src.cpp", "generated.cpp")}, &ModuleData{}, env)
	})
	if err == nil {
		t.Fatal("applyUnknownStmt unexpectedly accepted LLVM_BC without USE_LLVM_BC*")
	}
	if !strings.Contains(err.Error(), "LLVM_BC requires USE_LLVM_BC16/18/20") {
		t.Fatalf("applyUnknownStmt error = %q, want USE_LLVM_BC guidance", err.Error())
	}
}

func TestApplyUnknownStmt_LLVMBCAcceptsConfiguredVersion(t *testing.T) {
	tests := []struct {
		name        string
		useMacro    string
		resourceKey string
		resourceVal string
		wantLLCTool string
	}{
		{
			name:        "16",
			useMacro:    "USE_LLVM_BC16",
			resourceKey: "CLANG16_RESOURCE_GLOBAL",
			resourceVal: "clang16-resource",
			wantLLCTool: "contrib/libs/llvm16/tools/llc",
		},
		{
			name:        "18",
			useMacro:    "USE_LLVM_BC18",
			resourceKey: "CLANG18_RESOURCE_GLOBAL",
			resourceVal: "clang18-resource",
			wantLLCTool: "contrib/libs/llvm18/tools/llc",
		},
		{
			name:        "20",
			useMacro:    "USE_LLVM_BC20",
			resourceKey: "CLANG20_RESOURCE_GLOBAL",
			resourceVal: "clang20-resource",
			wantLLCTool: "contrib/libs/llvm20/tools/llc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := make(map[string]string, len(testToolchainFlags)+2)
			for k, v := range testToolchainFlags {
				flags[k] = v
			}
			flags["PIC"] = "no"
			flags[tt.resourceKey] = tt.resourceVal

			platform := newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "", "")
			env := buildIfEnv(ModuleInstance{Platform: platform})
			data := &ModuleData{}

			applyUnknownStmt(nil, "mod", &UnknownStmt{Name: internTok(tt.useMacro)}, data, env)
			// CLANG_BC_ROOT holds the deferred "$<NAME>_RESOURCE_GLOBAL" reference;
			// emitLLVMBC expands it against the resource-global closure, not here.
			if got, want := env.string(envCLANG_BC_ROOT), "$"+tt.resourceKey; got != want {
				t.Fatalf("CLANG_BC_ROOT = %q, want %q", got, want)
			}
			if got := env.string(envLLVM_LLC_TOOL); got != tt.wantLLCTool {
				t.Fatalf("LLVM_LLC_TOOL = %q, want %q", got, tt.wantLLCTool)
			}
			if err := try(func() {
				// LLVM_BC requires NAME per upstream.
				applyUnknownStmt(nil, "mod", &UnknownStmt{Name: tokLlvmBc, Args: STRS("src.cpp", "generated.cpp", "NAME", "Bytecode")}, data, env)
			}); err != nil {
				t.Fatalf("applyUnknownStmt rejected configured LLVM_BC: %v", err)
			}
			if len(data.llvmBc) != 1 || data.llvmBc[0].Name != "Bytecode" {
				t.Fatalf("LLVM_BC parse: data.llvmBc = %+v", data.llvmBc)
			}
			if got := data.llvmBc[0].Sources; !equalStrings(got, []string{"src.cpp", "generated.cpp"}) {
				t.Fatalf("LLVM_BC sources = %v, want [src.cpp generated.cpp]", got)
			}
		})
	}
}

func TestExpandStmtToken_SetVar(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	env.setFromString(envMODDIR, "mymod")
	env.setFromString(envORIG_SRC_DIR, "$(S)/mylib/src")

	got := expandStmtToken("${ORIG_SRC_DIR}", env)
	if got != "$(S)/mylib/src" {
		t.Fatalf("expandStmtToken(${ORIG_SRC_DIR}) = %q, want $(S)/mylib/src", got)
	}

	got = expandStmtToken("${UNKNOWN_VAR}", env)
	if got != "${UNKNOWN_VAR}" {
		t.Fatalf("expandStmtToken(${UNKNOWN_VAR}) = %q, want ${UNKNOWN_VAR} (no change)", got)
	}

	env.setFromString(envSRCDIR_RAW, "${ARCADIA_ROOT}/some/path")
	got = expandStmtToken("${SRCDIR_RAW}", env)
	if got != "$(S)/some/path" {
		t.Fatalf("expandConfigString with raw ARCADIA_ROOT in SET = %q, want $(S)/some/path", got)
	}
}

func TestPrEmitsIncludes_OutputIncludesVFSPrefixStripped(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"$(S)/util/generic/hash_set.h", "util/generic/hash_set.h"},
		{"$(S)/yql/essentials/core/expr_nodes_gen/yql_expr_nodes_gen.h", "yql/essentials/core/expr_nodes_gen/yql_expr_nodes_gen.h"},
		{"$(B)/generated/foo.pb.h", "generated/foo.pb.h"},
		{"util/generic/string.h", "util/generic/string.h"},
	}
	for _, c := range cases {
		got := c.input
		if vfsHasPrefix(c.input) {
			got = intern(c.input).rel()
		}
		if got != c.want {
			t.Errorf("VFS prefix strip(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestCopyFileInputVFS_ResolvesSourceRootPaths(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/local.txt":        "stub\n",
		"shared/generated.txt": "stub\n",
		"pkg/sub/codecs.h":     "stub\n",
	})

	if got := copyFileInputVFS(fs, "mod", "local.txt").string(); got != "$(S)/mod/local.txt" {
		t.Fatalf("local copy input = %q, want $(S)/mod/local.txt", got)
	}
	if got := copyFileInputVFS(fs, "mod", "shared/generated.txt").string(); got != "$(S)/shared/generated.txt" {
		t.Fatalf("root copy input = %q, want $(S)/shared/generated.txt", got)
	}
	if got := copyFileInputVFS(fs, "pkg/sub", "pkg/sub/codecs.h").string(); got != "$(S)/pkg/sub/codecs.h" {
		t.Fatalf("module-qualified copy input = %q, want $(S)/pkg/sub/codecs.h", got)
	}
}

// The ya.make argument-expansion semantics mirror upstream's Deref: an arg
// without '$' passes through verbatim; an arg with '$' gets one substitution
// pass (${NAME} only, unresolved refs stay literal), and a result containing a
// space is split into fields; empty results are dropped. SET assigns eagerly, so
// one pass reaches the fixpoint.

func expandTestEnv(bindings map[string]string) Environment {
	env := DefaultIfEnv.clone()

	for k, v := range bindings {
		env.setString(internEnv(k), v)
	}

	return env
}

func TestExpandStmtTokens_GateExample(t *testing.T) {
	env := expandTestEnv(map[string]string{"C": "C", "D": "D"})

	got := expandStmtTokens([]string{"B", "${C}/${D}", "E"}, env)
	want := []string{"B", "C/D", "E"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_MultiWordValueResplits(t *testing.T) {
	env := expandTestEnv(map[string]string{"C": "x y", "D": "D"})

	got := expandStmtTokens([]string{"B", "${C}/${D}", "E"}, env)
	want := []string{"B", "x", "y/D", "E"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_QuotedLiteralWithoutDollarKeptWhole(t *testing.T) {
	// A quoted "a b" reaches the arg list as one element with a space and no
	// '$' — passed through untouched.
	env := expandTestEnv(nil)

	got := expandStmtTokens([]string{"a b", "c"}, env)
	want := []string{"a b", "c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_QuotedArgWithDollarResplits(t *testing.T) {
	// Quotes are stripped at lex time, so a quoted arg containing ${V} whose
	// value has spaces IS re-split after substitution.
	env := expandTestEnv(map[string]string{"V": "p q"})

	got := expandStmtTokens([]string{"x ${V} y"}, env)
	want := []string{"x", "p", "q", "y"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_UnresolvedRefStaysLiteral(t *testing.T) {
	env := expandTestEnv(map[string]string{"C": "C"})

	got := expandStmtTokens([]string{"${UNDEFINED_VAR_42}", "${UNDEFINED_VAR_42}/${C}"}, env)
	want := []string{"${UNDEFINED_VAR_42}", "${UNDEFINED_VAR_42}/C"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_EmptyResultDropped(t *testing.T) {
	env := expandTestEnv(map[string]string{"EMPTY": ""})

	got := expandStmtTokens([]string{"a", "${EMPTY}", "b", "x${EMPTY}y"}, env)
	want := []string{"a", "b", "xy"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtToken_ArcadiaRootsViaEnv(t *testing.T) {
	env := expandTestEnv(nil)

	if got := expandStmtToken("${ARCADIA_ROOT}/x", env); got != "$(S)/x" {
		t.Errorf("ARCADIA_ROOT: got %q, want $(S)/x", got)
	}

	if got := expandStmtToken("${ARCADIA_BUILD_ROOT}/y", env); got != "$(B)/y" {
		t.Errorf("ARCADIA_BUILD_ROOT: got %q, want $(B)/y", got)
	}
}

func TestExpandStmtToken_UnresolvedRefDoesNotBlockLaterRefs(t *testing.T) {
	env := expandTestEnv(map[string]string{"C": "C"})

	if got := expandStmtToken("${UNDEFINED_VAR_42}/${C}", env); got != "${UNDEFINED_VAR_42}/C" {
		t.Errorf("got %q, want ${UNDEFINED_VAR_42}/C", got)
	}
}

func TestCollectModule_PySrcsExpandsSetList(t *testing.T) {
	// PY_SRCS(${SRCS}) where SRCS is a SET-list must expand+split into the
	// individual py sources — UnknownStmt-handled macros need arg-expansion like
	// the typed cases.
	src := "LIBRARY()\nSET(SRCS\n    a.py\n    b.py\n)\nPY_SRCS(${SRCS})\nEND()\n"

	mf, err := parse(testParserFS, "ya.make", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	d := collectModule(newIncludeParserManagerFS(newMemFS(nil), newSharedParseCache()), &DeDuper{}, "mod", KindLib,
		mf.Stmts, buildIfEnv(ModuleInstance{Path: source("mod"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(strStrings(d.pySrcs), []string{"a.py", "b.py"}) {
		t.Fatalf("pySrcs = %v, want [a.py b.py]", d.pySrcs)
	}
}

func TestCollectModule_SetAppendExpandsResourceAndSandboxInputs(t *testing.T) {
	// A module may build its file list with SET_APPEND(VAR …) then reference
	// ${VAR} in FROM_SANDBOX(OUT_NOAUTO …) and RESOURCE_FILES(…). SET_APPEND(VAR x)
	// binds VAR to "$VAR x", so the reference expands before resource/sandbox input
	// resolution; without the binding the literal ${VAR} survives as a nonexistent
	// path segment.
	src := "LIBRARY()\n" +
		"SET_APPEND(VAR\n    d/2.dict\n    d/3.dict\n)\n" +
		"FROM_SANDBOX(123 OUT_NOAUTO\n    ${VAR}\n)\n" +
		"RESOURCE_FILES(${VAR})\n" +
		"END()\n"

	mf, err := parse(testParserFS, "ya.make", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	d := collectModule(newIncludeParserManagerFS(newMemFS(nil), newSharedParseCache()), &DeDuper{}, "mod", KindLib,
		mf.Stmts, buildIfEnv(ModuleInstance{Path: source("mod"), Kind: KindLib, Platform: testTargetP}))

	if len(d.fromSandboxes) != 1 {
		t.Fatalf("fromSandboxes = %d, want 1", len(d.fromSandboxes))
	}

	if !equalStrings(strStrings(d.fromSandboxes[0].OUTNoAutoFiles), []string{"d/2.dict", "d/3.dict"}) {
		t.Fatalf("OUT_NOAUTO = %v, want [d/2.dict d/3.dict]", d.fromSandboxes[0].OUTNoAutoFiles)
	}

	var paths []string

	for _, e := range d.resources {
		if e.Path != "-" {
			paths = append(paths, e.Path)
		}
	}

	if !equalStrings(paths, []string{"d/2.dict", "d/3.dict"}) {
		t.Fatalf("RESOURCE_FILES paths = %v, want [d/2.dict d/3.dict]", paths)
	}
}

func TestExpandConfigVFSPaths_SplitsSetList(t *testing.T) {
	// ADDINCL(${__dirs_}) where __dirs_ is a SET-list must expand+split into one
	// VFS per dir, via the same expandStmtTokens primitive the typed-macro args
	// use — not a single path with embedded spaces.
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	env.setFromString(internEnv("DIRS"), "contrib/deprecated/bdb/src contrib/deprecated/bdb/src/dbinc")

	got := expandConfigVFSPaths(STRS("${DIRS}"), env)
	want := []VFS{source("contrib/deprecated/bdb/src"), source("contrib/deprecated/bdb/src/dbinc")}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandConfigVFSPaths = %v, want %v", got, want)
	}
}

func TestCatboostOpenSourceDefineGating(t *testing.T) {
	osP := newPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{"OPENSOURCE": "yes", "PIC": "no"}, "", "")
	if len(catboostOpenSourceDefineFor(osP)) == 0 {
		t.Error("OPENSOURCE=yes must include -DCATBOOST_OPENSOURCE")
	}

	intP := newPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{"PIC": "no"}, "", "")
	if catboostOpenSourceDefineFor(intP) != nil {
		t.Error("non-OPENSOURCE build must omit -DCATBOOST_OPENSOURCE")
	}
}

// testToolchain builds the module toolchain the way genModule does — from a
// resource-global closure declaring the build/platform/* resources — so tests
// that drive the emitters directly get the same compiler/linker/python tool
// paths without an ambient platform. The compiler comes from the version-specific
// toolchain resource (ClangVer "20").
func testToolchain() ModuleToolchain {
	return resolveModuleToolchain([]ResourceDecl{
		makeResourceDecl(resourcePatternClang20, "sbr:test-clang"),
		makeResourceDecl(resourcePatternLLDRoot, "sbr:test-lld"),
		makeResourceDecl(resourcePatternYMakePython3, "sbr:test-python"),
	}, "20")
}

// addToolchainPeers injects the synthetic build/platform/* RESOURCES_LIBRARYs
// every module implicitly PEERDIRs, so a gen test's memFS yields a populated
// module toolchain (d.tc) — the source of compiler/python/objcopy/linker paths.
// Without them the closure is empty and tool-emitting nodes carry blank paths.
func addToolchainPeers(files map[string]string) {
	const json = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test"}}}`

	files["build/platform/clang/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG16 clang16.json)\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG20 clang20.json)\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG clang16.json)\nEND()\n"
	files["build/platform/clang/clang16.json"] = json
	// CLANG binds to clang${CLANG_VER}.json; same sbr here so golden output is
	// version-agnostic.
	files["build/platform/clang/clang20.json"] = json
	files["build/platform/lld/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(LLD_ROOT lld.json)\nEND()\n"
	files["build/platform/lld/lld.json"] = json
	files["build/platform/python/ymake_python3/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(YMAKE_PYTHON3 python.json)\nEND()\n"
	files["build/platform/python/ymake_python3/python.json"] = json
}
