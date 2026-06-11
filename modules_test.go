package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyUnknownStmt_ExcludeTagsAcceptsTagNames(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	d := &ModuleData{}

	// EXCLUDE_TAGS args are submodule tag names (data), not service-keywords
	// that split the arg list, so the audit must not reject an unmodelled one.
	// Build the tag at runtime: a literal here would be mined into
	// knownServiceTokens (macro_audit embeds *.go, _test.go included) and mask
	// the very check this test exercises.
	tag := "PY" + "_" + "PROTO"

	err := Try(func() {
		applyUnknownStmt("mod", &UnknownStmt{Name: tokExcludeTags, Args: []string{tag}}, d, env)
	})

	if err != nil {
		t.Fatalf("EXCLUDE_TAGS(%s) rejected: %v", tag, err)
	}

	if !d.excludeTags[tag] {
		t.Fatalf("excludeTags = %v, want %s", d.excludeTags, tag)
	}
}

func TestApplyUnknownStmt_AddInclSelf(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	// ADDINCLSELF() adds -I<own source dir> (ymake.core.conf:3177:
	// ADDINCL += ${MODDIR}). It must resolve to Source(<modulePath>).
	d := &ModuleData{}
	applyUnknownStmt("contrib/libs/foo", &UnknownStmt{Name: internTok("ADDINCLSELF")}, d, env)

	want := Source("contrib/libs/foo")
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
	applyUnknownStmt("contrib/libs/bar", &UnknownStmt{Name: internTok("ADDINCLSELF"), Args: []string{"FOR", "cython"}}, dc, env)

	if len(dc.cythonAddIncl) != 1 || dc.cythonAddIncl[0] != Source("contrib/libs/bar") {
		t.Fatalf("ADDINCLSELF(FOR cython): cythonAddIncl = %v, want [%v]", dc.cythonAddIncl, Source("contrib/libs/bar"))
	}
}

func TestApplyUnknownStmt_LLVMBCRequiresConfiguredVersion(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	err := Try(func() {
		applyUnknownStmt("mod", &UnknownStmt{Name: tokLlvmBc, Args: []string{"src.cpp", "generated.cpp"}}, &ModuleData{}, env)
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

			platform := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "", "")
			env := buildIfEnv(ModuleInstance{Platform: platform})
			data := &ModuleData{}

			applyUnknownStmt("mod", &UnknownStmt{Name: internTok(tt.useMacro)}, data, env)
			// CLANG_BC_ROOT holds the deferred "$<NAME>_RESOURCE_GLOBAL" reference;
			// emitLLVMBC expands it against the resource-global closure (the value
			// declared by the build/platform/clang PEERDIR), not eagerly here.
			if got, want := env.string(envCLANG_BC_ROOT), "$"+tt.resourceKey; got != want {
				t.Fatalf("CLANG_BC_ROOT = %q, want %q", got, want)
			}
			if got := env.string(envLLVM_LLC_TOOL); got != tt.wantLLCTool {
				t.Fatalf("LLVM_LLC_TOOL = %q, want %q", got, tt.wantLLCTool)
			}
			if err := Try(func() {
				// LLVM_BC requires NAME per upstream (build/plugins/llvm_bc.py:8).
				applyUnknownStmt("mod", &UnknownStmt{Name: tokLlvmBc, Args: []string{"src.cpp", "generated.cpp", "NAME", "Bytecode"}}, data, env)
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
			got = Intern(c.input).rel()
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

// The ya.make argument-expansion semantics mirror upstream ymake's
// TEvalContext::Deref (devtools/ymake/lang/eval_context.cpp): per argument,
// an arg without '$' passes through verbatim; an arg with '$' gets one
// substitution pass (${NAME} only, unresolved refs stay literal), and if the
// result contains a space it is split into fields; empty results are dropped.
// SET assigns eagerly (value expanded at assignment), so one pass reaches the
// fixpoint by construction.

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
	// '$' — upstream passes it through untouched.
	env := expandTestEnv(nil)

	got := expandStmtTokens([]string{"a b", "c"}, env)
	want := []string{"a b", "c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandStmtTokens = %q, want %q", got, want)
	}
}

func TestExpandStmtTokens_QuotedArgWithDollarResplits(t *testing.T) {
	// Upstream strips quotes at lex time, so a quoted arg containing ${V}
	// whose value has spaces IS re-split after substitution.
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
	// the typed cases. (yt/python/yt/wrapper builds SRCS via SET then PY_SRCS it.)
	src := "LIBRARY()\nSET(SRCS\n    a.py\n    b.py\n)\nPY_SRCS(${SRCS})\nEND()\n"

	mf, err := Parse(testParserFS, "ya.make", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	d := collectModule(newIncludeParserManagerFS(newMemFS(nil), newSharedParseCache()), &DeDuper{}, "mod", KindLib,
		mf.Stmts, buildIfEnv(ModuleInstance{Path: Source("mod"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.pySrcs, []string{"a.py", "b.py"}) {
		t.Fatalf("pySrcs = %v, want [a.py b.py]", d.pySrcs)
	}
}

func TestExpandConfigVFSPaths_SplitsSetList(t *testing.T) {
	// ADDINCL(${__dirs_}) where __dirs_ is a SET-list must expand+split into one
	// VFS per dir (bdb: src + src/dbinc + …), via the same expandStmtTokens
	// primitive the typed-macro args use — not a single path with embedded spaces.
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	env.setFromString(internEnv("DIRS"), "contrib/deprecated/bdb/src contrib/deprecated/bdb/src/dbinc")

	got := expandConfigVFSPaths([]string{"${DIRS}"}, env)
	want := []VFS{Source("contrib/deprecated/bdb/src"), Source("contrib/deprecated/bdb/src/dbinc")}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandConfigVFSPaths = %v, want %v", got, want)
	}
}

func TestCatboostOpenSourceDefineGating(t *testing.T) {
	osP := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{"OPENSOURCE": "yes", "PIC": "no"}, nil, "", "")
	if len(catboostOpenSourceDefineFor(osP)) == 0 {
		t.Error("OPENSOURCE=yes must include -DCATBOOST_OPENSOURCE")
	}

	intP := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{"PIC": "no"}, nil, "", "")
	if catboostOpenSourceDefineFor(intP) != nil {
		t.Error("non-OPENSOURCE build must omit -DCATBOOST_OPENSOURCE")
	}
}
