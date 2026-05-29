package main

import (
	"reflect"
	"strings"
	"testing"
)

var testToolchainFlags = map[string]string{
	"BUILD_PYTHON_BIN":        "/bin/python3",
	"BUILD_PYTHON3_BIN":       "/bin/python3",
	"CLANG_TOOL":              "/bin/clang",
	"CLANG_pl_pl_TOOL":        "/bin/clang++",
	"OBJCOPY_TOOL":            "/bin/llvm-objcopy",
	"AR_TOOL":                 "/bin/llvm-ar",
	"STRIP_TOOL":              "/bin/llvm-strip",
	"LLD_TOOL":                "/bin/lld",
	"CLANG16_RESOURCE_GLOBAL": "CLANG16_RESOURCE_GLOBAL::$(CLANG16-1380963495)",
}

var (
	testHostP   = newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	testTargetP = newTestPlatform(OSLinux, ISAAArch64, "no", nil)
)

func newTestPlatform(os OS, isa ISA, pic string, tags []string) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = pic
	return NewPlatform(os, isa, flags, tags, "", "")
}

func targetInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}
}

func hostInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
}

func testPlatformFor(i ModuleInstance) *Platform {
	return i.Platform
}

func vfsStrings(vs []VFS) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.String()
	}
	return out
}

func TestEmitAR_LengthMismatchPanics(t *testing.T) {
	e := NewBufferedEmitter()

	objRefs := []NodeRef{e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{}),
		KV:               map[string]interface{}{},
		Outputs:          ToVFSSlice([]string{"$(B)/build/cow/on/lib.c.o"}),
		Platform:         "default-linux-aarch64",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})}
	objPaths := []VFS{Intern("$(B)/o1.o"), Intern("$(B)/o2.o")}

	exc := Try(func() {
		EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	})

	if exc == nil {
		t.Fatal("expected exception for length mismatch")
	}

	if !strings.Contains(exc.Error(), "length mismatch") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestArchiveName(t *testing.T) {
	cases := []struct {
		moduleDir string
		want      string
	}{

		{"util", "libyutil.a"},

		{"tools/archiver", "libtools-archiver.a"},
		{"foo/bar", "libfoo-bar.a"},

		{"foo", "libfoo.a"},

		{"build/cow/on", "libbuild-cow-on.a"},
		{"util/charset", "libutil-charset.a"},

		{"contrib/libs/cxxsupp/libcxxrt", "liblibs-cxxsupp-libcxxrt.a"},
		{"library/cpp/getopt/small", "libcpp-getopt-small.a"},
		{"devtools/ymake/diag/common_display", "libymake-diag-common_display.a"},
	}

	for _, tc := range cases {
		got := ArchiveName(tc.moduleDir)

		if got != tc.want {
			t.Errorf("ArchiveName(%q) = %q, want %q", tc.moduleDir, got, tc.want)
		}
	}
}

func TestArchiveName_AllReferenceAR(t *testing.T) {
	cases := []struct {
		moduleDir string
		want      string
	}{
		{"build/cow/on", "libbuild-cow-on.a"},
		{"contrib/libs/asmglibc", "libcontrib-libs-asmglibc.a"},
		{"contrib/libs/asmlib", "libcontrib-libs-asmlib.a"},
		{"contrib/libs/base64/avx2", "liblibs-base64-avx2.a"},
		{"contrib/libs/base64/neon32", "liblibs-base64-neon32.a"},
		{"contrib/libs/base64/neon64", "liblibs-base64-neon64.a"},
		{"contrib/libs/base64/plain32", "liblibs-base64-plain32.a"},
		{"contrib/libs/base64/plain64", "liblibs-base64-plain64.a"},
		{"contrib/libs/base64/ssse3", "liblibs-base64-ssse3.a"},
		{"contrib/libs/cxxsupp/builtins", "liblibs-cxxsupp-builtins.a"},
		{"contrib/libs/cxxsupp/libcxx", "liblibs-cxxsupp-libcxx.a"},
		{"contrib/libs/cxxsupp/libcxxabi-parts", "liblibs-cxxsupp-libcxxabi-parts.a"},
		{"contrib/libs/cxxsupp/libcxxrt", "liblibs-cxxsupp-libcxxrt.a"},
		{"contrib/libs/double-conversion", "libcontrib-libs-double-conversion.a"},
		{"contrib/libs/jemalloc", "libcontrib-libs-jemalloc.a"},
		{"contrib/libs/libc_compat", "libcontrib-libs-libc_compat.a"},
		{"contrib/libs/libunwind", "libcontrib-libs-libunwind.a"},
		{"contrib/libs/linuxvdso", "libcontrib-libs-linuxvdso.a"},
		{"contrib/libs/linuxvdso/original", "liblibs-linuxvdso-original.a"},
		{"contrib/libs/mimalloc", "libcontrib-libs-mimalloc.a"},
		{"contrib/libs/foolib", "libcontrib-libs-foolib.a"},
		{"contrib/libs/foolib/full", "liblibs-foolib-full.a"},
		{"contrib/libs/foolib_extra", "libcontrib-libs-foolib_extra.a"},
		{"contrib/libs/nayuki_md5", "libcontrib-libs-nayuki_md5.a"},
		{"contrib/libs/tcmalloc/malloc_extension", "liblibs-tcmalloc-malloc_extension.a"},
		{"contrib/libs/tcmalloc/no_percpu_cache", "liblibs-tcmalloc-no_percpu_cache.a"},
		{"contrib/libs/zlib", "libcontrib-libs-zlib.a"},
		{"contrib/restricted/abseil-cpp", "libcontrib-restricted-abseil-cpp.a"},
		{"library/cpp/archive", "liblibrary-cpp-archive.a"},
		{"library/cpp/colorizer", "liblibrary-cpp-colorizer.a"},
		{"library/cpp/digest/md5", "libcpp-digest-md5.a"},
		{"library/cpp/getopt/small", "libcpp-getopt-small.a"},
		{"library/cpp/malloc/api", "libcpp-malloc-api.a"},
		{"library/cpp/malloc/mimalloc", "libcpp-malloc-mimalloc.a"},
		{"library/cpp/malloc/tcmalloc", "libcpp-malloc-tcmalloc.a"},
		{"library/cpp/string_utils/base64", "libcpp-string_utils-base64.a"},
		{"util", "libyutil.a"},
		{"util/charset", "libutil-charset.a"},
	}

	for _, tc := range cases {
		got := ArchiveName(tc.moduleDir)

		if got != tc.want {
			t.Errorf("ArchiveName(%q) = %q, want %q", tc.moduleDir, got, tc.want)
		}
	}
}

func TestEmitAR_PeerArchives_NotInCmdArgs(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out VFS) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]interface{}{},
			Outputs:          []VFS{out},
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	o1 := Intern("$(B)/build/cow/on/a.c.o")
	o2 := Intern("$(B)/build/cow/on/b.c.o")
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []VFS{o1, o2}

	peer1 := makeLeaf(Intern("$(B)/some/peer/libsome-peer.a"))
	peer2 := makeLeaf(Intern("$(B)/other/peer/libother-peer.a"))
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, testHostP, e)
	got := e.nodes[arRef.id]

	cmdArgs := got.Cmds[0].CmdArgs
	wantLen := 9 + 1 + len(objPaths)

	if len(cmdArgs) != wantLen {
		t.Errorf("cmd_args len = %d, want %d (9 prefix + 1 archive + %d .o)", len(cmdArgs), wantLen, len(objPaths))
	}

	peerPaths := []string{
		"$(B)/some/peer/libsome-peer.a",
		"$(B)/other/peer/libother-peer.a",
	}

	for _, pp := range peerPaths {
		for _, arg := range cmdArgs {
			if arg == pp {
				t.Errorf("peer archive path %q unexpectedly present in cmd_args", pp)
			}
		}
	}
}

func TestEmitAR_PeerArchives_InDepRefs(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out VFS) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]interface{}{},
			Outputs:          []VFS{out},
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	o1 := Intern("$(B)/build/cow/on/a.c.o")
	o2 := Intern("$(B)/build/cow/on/b.c.o")
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []VFS{o1, o2}

	peer1 := makeLeaf(Intern("$(B)/some/peer/libsome-peer.a"))
	peer2 := makeLeaf(Intern("$(B)/other/peer/libother-peer.a"))
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, testHostP, e)
	got := e.nodes[arRef.id]

	wantDepRefs := len(objRefs) + len(peerArchiveRefs)

	if len(got.DepRefs) != wantDepRefs {
		t.Errorf("DepRefs len = %d, want %d (objRefs=%d + peerArchiveRefs=%d)",
			len(got.DepRefs), wantDepRefs, len(objRefs), len(peerArchiveRefs))
	}
}

func TestEmitAR_InputsLeadWithObjPaths(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out VFS) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]interface{}{},
			Outputs:          []VFS{out},
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	z := Intern("$(B)/build/cow/on/z.c.o")
	m := Intern("$(B)/build/cow/on/m.c.o")
	a := Intern("$(B)/build/cow/on/a.c.o")
	objPaths := []VFS{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	got := e.nodes[arRef.id]

	inputs := got.Inputs
	if len(inputs) != 4 {
		t.Fatalf("inputs len = %d, want 4", len(inputs))
	}

	inputObjs := vfsStrings(inputs[:3])

	wantInputObjs := []string{z.String(), m.String(), a.String()}

	if !reflect.DeepEqual(inputObjs, wantInputObjs) {
		t.Errorf("inputs .o mismatch:\n  want %v\n  got  %v", wantInputObjs, inputObjs)
	}
}

func TestEmitAR_CmdArgsPreservesDeclarationOrder(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out VFS) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]interface{}{},
			Outputs:          []VFS{out},
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	z := Intern("$(B)/build/cow/on/z.c.o")
	m := Intern("$(B)/build/cow/on/m.c.o")
	a := Intern("$(B)/build/cow/on/a.c.o")
	objPaths := []VFS{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	got := e.nodes[arRef.id]

	cmdArgs := got.Cmds[0].CmdArgs
	if len(cmdArgs) != 13 {
		t.Fatalf("cmd_args len = %d, want 13", len(cmdArgs))
	}

	trailing := cmdArgs[10:]
	wantTrailing := []string{z.String(), m.String(), a.String()}

	if !reflect.DeepEqual(trailing, wantTrailing) {
		t.Errorf("cmd_args .o order mismatch:\n  want %v\n  got  %v", wantTrailing, trailing)
	}
}
