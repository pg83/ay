package main

import (
	"strings"
	"testing"
)

func TestApplyUnknownStmt_LLVMBCRequiresConfiguredVersion(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	err := Try(func() {
		applyUnknownStmt("mod", &UnknownStmt{Name: tokLlvmBc, Args: []string{"src.cpp", "generated.cpp"}}, &moduleData{}, env)
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

			platform := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "", "", nil)
			env := buildIfEnv(ModuleInstance{Platform: platform})
			data := &moduleData{}

			applyUnknownStmt("mod", &UnknownStmt{Name: internTok(tt.useMacro)}, data, env)
			if got := env.String(envCLANG_BC_ROOT); got != tt.resourceVal {
				t.Fatalf("CLANG_BC_ROOT = %q, want %q", got, tt.resourceVal)
			}
			if got := env.String(envLLVM_LLC_TOOL); got != tt.wantLLCTool {
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

func TestExpandConfigString_SetVar(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	env.SetFromString(envMODDIR, "mymod")
	env.SetFromString(envORIG_SRC_DIR, "$(S)/mylib/src")

	got := expandConfigString("${ORIG_SRC_DIR}", env)
	if got != "$(S)/mylib/src" {
		t.Fatalf("expandConfigString(${ORIG_SRC_DIR}) = %q, want $(S)/mylib/src", got)
	}

	got = expandConfigString("${UNKNOWN_VAR}", env)
	if got != "${UNKNOWN_VAR}" {
		t.Fatalf("expandConfigString(${UNKNOWN_VAR}) = %q, want ${UNKNOWN_VAR} (no change)", got)
	}

	env.SetFromString(envSRCDIR_RAW, "${ARCADIA_ROOT}/some/path")
	got = expandConfigString("${SRCDIR_RAW}", env)
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
			got = Intern(c.input).Rel()
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

	if got := copyFileInputVFS(fs, "mod", "local.txt").String(); got != "$(S)/mod/local.txt" {
		t.Fatalf("local copy input = %q, want $(S)/mod/local.txt", got)
	}
	if got := copyFileInputVFS(fs, "mod", "shared/generated.txt").String(); got != "$(S)/shared/generated.txt" {
		t.Fatalf("root copy input = %q, want $(S)/shared/generated.txt", got)
	}
	if got := copyFileInputVFS(fs, "pkg/sub", "pkg/sub/codecs.h").String(); got != "$(S)/pkg/sub/codecs.h" {
		t.Fatalf("module-qualified copy input = %q, want $(S)/pkg/sub/codecs.h", got)
	}
}
