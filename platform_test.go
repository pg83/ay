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

func TestWrapccPrefixFor_GateOnOpensource(t *testing.T) {
	// Internal contour (OPENSOURCE unset) enables the wrapcc.py compile wrapper.
	head, tail := wrapccPrefixFor(map[string]string{"PIC": "no"})

	wantHead := []string{
		"$(B)/resources/YMAKE_PYTHON3/bin/python3",
		"$(S)/build/scripts/wrapcc.py",
		"--source-file",
	}
	wantTail := []string{"--source-root", "$(S)", "--build-root", "$(B)", "--wrapcc-end"}

	if !reflect.DeepEqual(strStrs(head), wantHead) {
		t.Errorf("wrapcc head = %v, want %v", strStrs(head), wantHead)
	}

	if !reflect.DeepEqual(strStrs(tail), wantTail) {
		t.Errorf("wrapcc tail = %v, want %v", strStrs(tail), wantTail)
	}

	// Opensource contour (OPENSOURCE=yes, the sg2–5 reference snapshots) disables it.
	head, tail = wrapccPrefixFor(map[string]string{"OPENSOURCE": "yes"})

	if head != nil || tail != nil {
		t.Errorf("OPENSOURCE=yes must disable wrapcc; got head=%v tail=%v", head, tail)
	}
}

func TestNewPlatform_WrapccVectorsAndResources(t *testing.T) {
	// Non-opensource platform carries the wrapcc prefix and a YMAKE_PYTHON3 CC dep.
	intP := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, map[string]string{"PIC": "no"}, nil, "", "")

	if len(intP.WrapccHead) == 0 {
		t.Fatal("non-opensource platform must populate WrapccHead")
	}

	wantRes := []string{resourcePatternClangTool + intP.ClangVer, resourcePatternYMakePython3}
	if !reflect.DeepEqual(intP.CCUsesResources, wantRes) {
		t.Errorf("CCUsesResources = %v, want %v", intP.CCUsesResources, wantRes)
	}

	// Opensource platform: no wrapper, CLANG-only CC deps.
	osP := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, map[string]string{"PIC": "no", "OPENSOURCE": "yes"}, nil, "", "")

	if len(osP.WrapccHead) != 0 {
		t.Errorf("opensource platform must not populate WrapccHead; got %v", osP.WrapccHead)
	}

	if !reflect.DeepEqual(osP.CCUsesResources, []string{resourcePatternClangTool + osP.ClangVer}) {
		t.Errorf("opensource CCUsesResources = %v, want CLANG-only", osP.CCUsesResources)
	}
}

func TestSysrootArgsFor(t *testing.T) {
	sdk := "$(B)/resources/OS_SDK_ROOT"

	// Default Linux: SDK sysroot + tool-bin dir.
	if got := strStrs(sysrootArgsFor(OSLinux, map[string]string{})); !reflect.DeepEqual(got, []string{"--sysroot=" + sdk, "-B" + sdk + "/usr/bin"}) {
		t.Errorf("linux default = %v", got)
	}

	// MUSL pins --sysroot=/nowhere but keeps the SDK tool-bin dir.
	if got := strStrs(sysrootArgsFor(OSLinux, map[string]string{"MUSL": "yes"})); !reflect.DeepEqual(got, []string{"--sysroot=/nowhere", "-B" + sdk + "/usr/bin"}) {
		t.Errorf("musl = %v", got)
	}

	// os_sdk=local uses the bare host tool-bin dir, no fetched SDK.
	if got := strStrs(sysrootArgsFor(OSLinux, map[string]string{"OS_SDK": "local"})); !reflect.DeepEqual(got, []string{"-B/usr/bin"}) {
		t.Errorf("local = %v", got)
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

	// Internal contour (no OPENSOURCE flag): the resource-resolved SDK form.
	if got, want := p.multiarchLibPath(false), "$(B)/resources/CLANG20/lib:$(B)/resources/OS_SDK_ROOT/usr/lib/x86_64-linux-gnu"; got != want {
		t.Fatalf("multiarchLibPath(internal) = %q, want %q", got, want)
	}

	// Opensource contour: the raw resource-global macro, verbatim.
	if got, want := p.multiarchLibPath(true), "$(B)/resources/CLANG20/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu"; got != want {
		t.Fatalf("multiarchLibPath(opensource) = %q, want %q", got, want)
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
	if got := p.linkerSelectionTailFlags(); got != nil {
		t.Fatalf("LinkerSelectionTailFlags = %#v, want nil", got)
	}
}
