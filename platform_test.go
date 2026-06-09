package main

import (
	"reflect"
	"testing"
)

func TestParseCompilerFlags(t *testing.T) {
	got := parseCompilerFlags(`-O2 -DNAME="hello world" '-DOTHER=two words' -DQUOTE=\"x\" trailing\ slash`)
	want := []string{
		"-O2",
		"-DNAME=hello world",
		"-DOTHER=two words",
		`-DQUOTE="x"`,
		"trailing slash",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCompilerFlags mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNewPlatform_ParsesCompilerFlags(t *testing.T) {
	flags := map[string]string{
		"PIC": "no",
	}

	p := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, `-O2 -DNAME="hello world"`, `-stdlib=libc++ -DCPP=1`)

	if !reflect.DeepEqual(argStrs(p.CFlags), []string{"-O2", "-DNAME=hello world"}) {
		t.Fatalf("CFlags = %#v", argStrs(p.CFlags))
	}

	if !reflect.DeepEqual(argStrs(p.CXXFlags), []string{"-stdlib=libc++", "-DCPP=1"}) {
		t.Fatalf("CXXFlags = %#v", argStrs(p.CXXFlags))
	}
}

func TestPlatformMultiarchLibPath_UsesCompilerRoot(t *testing.T) {
	p := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{
		"PIC":              "yes",
		"BUILD_PYTHON_BIN": "$(YMAKE_PYTHON3)/bin/python3",
		"CLANG_TOOL":       "$(CLANG)/bin/clang",
		"CLANG_pl_pl_TOOL": "$(CLANG)/bin/clang++",
		"AR_TOOL":          "$(CLANG)/bin/llvm-ar",
		"LLD_TOOL":         "$(LLD_ROOT)/bin/ld.lld",
	}, []string{"tool"}, "", "")

	if got, want := p.MultiarchLibPath(), "$(B)/resources/CLANG/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu"; got != want {
		t.Fatalf("MultiarchLibPath = %q, want %q", got, want)
	}
}

func TestPlatformLinkerSelectionTailFlags_UsesConfiguredLLDPath(t *testing.T) {
	p := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, map[string]string{
		"PIC":              "no",
		"CLANG_TOOL":       "$(CLANG)/bin/clang",
		"CLANG_pl_pl_TOOL": "$(CLANG)/bin/clang++",
		"AR_TOOL":          "$(CLANG)/bin/llvm-ar",
		"LLD_TOOL":         "$(LLD_ROOT)/bin/ld.lld",
	}, nil, "", "")

	// The lld linker flags now come from build/platform/lld's propagated
	// LDFLAGS_GLOBAL (the implicit toolchain peer), not the Platform.
	if got := p.LinkerSelectionTailFlags(); got != nil {
		t.Fatalf("LinkerSelectionTailFlags = %#v, want nil", got)
	}
}
