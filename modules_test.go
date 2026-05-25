package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyUnknownStmt_LLVMBCRequiresConfiguredVersion(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})

	err := Try(func() {
		applyUnknownStmt("mod", &UnknownStmt{Name: "LLVM_BC", Args: []string{"src.cpp", "generated.cpp"}}, &moduleData{}, env)
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
			flags := make(map[string]string, len(testToolchainFlags)+3)
			for k, v := range testToolchainFlags {
				flags[k] = v
			}
			flags["PIC"] = "no"
			flags["MUSL"] = "yes"
			flags[tt.resourceKey] = tt.resourceVal

			platform := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")
			env := buildIfEnv(ModuleInstance{Platform: platform})
			data := &moduleData{}

			applyUnknownStmt("mod", &UnknownStmt{Name: tt.useMacro}, data, env)
			if got := env.String("CLANG_BC_ROOT"); got != tt.resourceVal {
				t.Fatalf("CLANG_BC_ROOT = %q, want %q", got, tt.resourceVal)
			}
			if got := env.String("LLVM_LLC_TOOL"); got != tt.wantLLCTool {
				t.Fatalf("LLVM_LLC_TOOL = %q, want %q", got, tt.wantLLCTool)
			}
			if err := Try(func() {
				applyUnknownStmt("mod", &UnknownStmt{Name: "LLVM_BC", Args: []string{"src.cpp", "generated.cpp"}}, data, env)
			}); err != nil {
				t.Fatalf("applyUnknownStmt rejected configured LLVM_BC: %v", err)
			}
		})
	}
}

// TestExpandConfigString_SetVar verifies Fix C5+C8: user-defined SET variables
// (e.g. ORIG_SRC_DIR) are now expanded by expandConfigString, and SET values
// that contain literal ${ARCADIA_ROOT} are also resolved to $(S).
func TestExpandConfigString_SetVar(t *testing.T) {
	env := buildIfEnv(ModuleInstance{Platform: testTargetP})
	env.SetFromString("MODDIR", "mymod")
	env.SetFromString("ORIG_SRC_DIR", "$(S)/mylib/src")

	got := expandConfigString("${ORIG_SRC_DIR}", env)
	if got != "$(S)/mylib/src" {
		t.Fatalf("expandConfigString(${ORIG_SRC_DIR}) = %q, want $(S)/mylib/src", got)
	}

	got = expandConfigString("${UNKNOWN_VAR}", env)
	if got != "${UNKNOWN_VAR}" {
		t.Fatalf("expandConfigString(${UNKNOWN_VAR}) = %q, want ${UNKNOWN_VAR} (no change)", got)
	}

	// SET value that still holds literal ${ARCADIA_ROOT}: must be re-expanded.
	env.SetFromString("SRCDIR_RAW", "${ARCADIA_ROOT}/some/path")
	got = expandConfigString("${SRCDIR_RAW}", env)
	if got != "$(S)/some/path" {
		t.Fatalf("expandConfigString with raw ARCADIA_ROOT in SET = %q, want $(S)/some/path", got)
	}
}

// TestPrEmitsIncludes_OutputIncludesVFSPrefixStripped verifies Fix C6:
// OUTPUT_INCLUDES entries that carry a $(S)/ or $(B)/ prefix (produced by
// expandStmtTokens expanding ${ARCADIA_ROOT} → $(S)/) have that prefix
// stripped before they become include directive targets.
func TestPrEmitsIncludes_OutputIncludesVFSPrefixStripped(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"$(S)/util/generic/hash_set.h", "util/generic/hash_set.h"},
		{"$(S)/yql/essentials/core/expr_nodes_gen/yql_expr_nodes_gen.h", "yql/essentials/core/expr_nodes_gen/yql_expr_nodes_gen.h"},
		{"$(B)/generated/foo.pb.h", "generated/foo.pb.h"},
		{"util/generic/string.h", "util/generic/string.h"}, // bare path unchanged
	}
	for _, c := range cases {
		got := c.input
		if vfsHasPrefix(c.input) {
			got = ParseVFS(c.input).Rel()
		}
		if got != c.want {
			t.Errorf("VFS prefix strip(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestCopyFileInputVFS_ResolvesSourceRootPaths(t *testing.T) {
	root := t.TempDir()
	writeFile := func(rel string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte("stub\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeFile("mod/local.txt")
	writeFile("shared/generated.txt")
	writeFile("pkg/sub/codecs.h")

	fs := NewFS(root)

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

func TestCollectModule_LibiconvUsesStaticPeerWithMergedYdbFlags(t *testing.T) {
	const ydbSourceRoot = "/home/pg/monorepo/ydb"
	if _, err := os.Stat(filepath.Join(ydbSourceRoot, "contrib/libs/libiconv/ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("ydb source tree not present at %s", ydbSourceRoot)
		}
		t.Fatalf("stat ydb libiconv ya.make: %v", err)
	}

	fs := NewFS(ydbSourceRoot)
	flags := make(map[string]string, len(testToolchainFlags)+8)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	for k, v := range readYaConfSections(fs, "host_platform_flags", "ya.conf", "build/internal/ya.conf") {
		flags[k] = v
	}
	flags["PIC"] = "yes"

	hostPlatform := NewPlatform(OSLinux, ISAX8664, flags, []string{"tool"}, "", "")
	inst := ModuleInstance{Path: "contrib/libs/libiconv", Kind: KindLib, Platform: hostPlatform}
	mf := Throw2(ParseFile(fs, filepath.Join(ydbSourceRoot, "contrib/libs/libiconv/ya.make")))
	d := collectModule(fs, inst.Path, inst.Kind, mf.Stmts, buildIfEnv(inst))

	hasStatic := false
	for _, peer := range d.peerdirs {
		if peer == "contrib/libs/libiconv/static" {
			hasStatic = true
		}
		if peer == "contrib/libs/libiconv/dynamic" {
			t.Fatalf("libiconv peerdirs unexpectedly include dynamic peer: %#v", d.peerdirs)
		}
	}
	if !hasStatic {
		t.Fatalf("libiconv peerdirs missing static peer: %#v", d.peerdirs)
	}
}
