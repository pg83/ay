package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// helper to construct the canonical target instance for a path.
// PR-M3-platform-pair-step12: canonical (host, target) Platform values
// for tests. Constructed once via defaultLinuxPlatforms(nil) so every
// test exercises the exact pair the production CLI builds.
var (
	testHostP, testTargetP = defaultLinuxPlatforms(nil)
)

func targetInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Language: LangCPP,
		Platform: testTargetP,
		Flags:    inferFlagsFromPath(path, false),
	}
}

// helper to construct the canonical host instance for a path.
func hostInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Language: LangCPP,
		Platform: testHostP,
		Flags:    inferFlagsFromPath(path, true),
	}
}

// testPlatformFor mirrors ctx.platformFor for tests.
func testPlatformFor(i ModuleInstance) *Platform {
	return i.Platform
}

// vfsStrings materialises a []VFS as a []string of canonical VFS forms
// — convenience for test assertions that compare against literal
// "$(SOURCE_ROOT)/..." / "$(BUILD_ROOT)/..." strings.
func vfsStrings(vs []VFS) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.String()
	}
	return out
}

// TestEmitAR_BuildCowOn_Target_ByteExact verifies that EmitAR
// produces a node that is field-for-field identical to the
// reference TARGET AR node in /home/pg/monorepo/yatool_orig/sg.json
// for the build/cow/on module.
// TestEmitAR_BuildCowOn_Host_ByteExact verifies the host AR for
// build/cow/on. Reference uses the SAME archive name
// (libbuild-cow-on.a, not .pic.a), but with platform=x86_64,
// host_platform=true, tags=["tool"], and the .pic.o leaf path.
// TestEmitAR_LengthMismatchPanics verifies that EmitAR throws when
// objRefs and objPaths have different lengths.
func TestEmitAR_LengthMismatchPanics(t *testing.T) {
	e := NewBufferedEmitter()

	objRefs := []NodeRef{e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{}),
		KV:               map[string]string{},
		Outputs:          ToVFSSlice([]string{"$(BUILD_ROOT)/build/cow/on/lib.c.o"}),
		Platform:         "default-linux-aarch64",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})}
	objPaths := []string{"$(BUILD_ROOT)/o1.o", "$(BUILD_ROOT)/o2.o"}

	exc := Try(func() {
		EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, nil, e)
	})

	if exc == nil {
		t.Fatal("expected exception for length mismatch")
	}

	if !strings.Contains(exc.Error(), "length mismatch") {
		t.Errorf("unexpected error: %v", exc)
	}
}

// TestArchiveName pins the ArchiveName rule for representative paths.
// Rule: last min(3, depth) components joined with "-", prefixed "lib",
// suffixed ".a". Sole exception: "util" → "libyutil.a".
// Source: devtools/ymake/module_confs.cpp:48-57 (ThreeDirNames /
// SetDefaultRealprjnameImpl with depth=2).
func TestArchiveName(t *testing.T) {
	cases := []struct {
		moduleDir string
		want      string
	}{
		// Special case: asymmetric "y" prefix hard-coded for util root.
		{"util", "libyutil.a"},
		// depth-2 path: all components used.
		{"tools/archiver", "libtools-archiver.a"},
		{"foo/bar", "libfoo-bar.a"},
		// depth-1 path: sole component used.
		{"foo", "libfoo.a"},
		// depth-3 paths: all three components used.
		{"build/cow/on", "libbuild-cow-on.a"},
		{"util/charset", "libutil-charset.a"},
		// depth-4+ paths: last 3 components (drop leading part).
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

// TestArchiveName_AllReferenceAR asserts that ArchiveName produces
// the correct archive base name for all 38 unique module_dirs found
// in the reference g.json.
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
		{"contrib/libs/musl", "libcontrib-libs-musl.a"},
		{"contrib/libs/musl/full", "liblibs-musl-full.a"},
		{"contrib/libs/musl_extra", "libcontrib-libs-musl_extra.a"},
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

// TestEmitAR_TcmallocGlobal_ByteExact loads g.json, locates the
// contrib/libs/tcmalloc/no_percpu_cache global AR node, and asserts
// that EmitARGlobal produces a field-by-field match.
// TestEmitAR_PeerArchives_NotInCmdArgs verifies that peer archive
// paths do NOT appear in cmd_args.
func TestEmitAR_PeerArchives_NotInCmdArgs(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out string) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]string{},
			Outputs:          ToVFSSlice([]string{out}),
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	o1 := "$(BUILD_ROOT)/build/cow/on/a.c.o"
	o2 := "$(BUILD_ROOT)/build/cow/on/b.c.o"
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []string{o1, o2}

	peer1 := makeLeaf("$(BUILD_ROOT)/some/peer/libsome-peer.a")
	peer2 := makeLeaf("$(BUILD_ROOT)/other/peer/libother-peer.a")
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, nil, e)
	got := e.nodes[arRef.id]

	cmdArgs := got.Cmds[0].CmdArgs
	wantLen := 9 + 1 + len(objPaths)

	if len(cmdArgs) != wantLen {
		t.Errorf("cmd_args len = %d, want %d (9 prefix + 1 archive + %d .o)", len(cmdArgs), wantLen, len(objPaths))
	}

	peerPaths := []string{
		"$(BUILD_ROOT)/some/peer/libsome-peer.a",
		"$(BUILD_ROOT)/other/peer/libother-peer.a",
	}

	for _, pp := range peerPaths {
		for _, arg := range cmdArgs {
			if arg == pp {
				t.Errorf("peer archive path %q unexpectedly present in cmd_args", pp)
			}
		}
	}
}

// TestEmitAR_PeerArchives_InDepRefs verifies that peer archive
// NodeRefs are included in the node's DepRefs.
func TestEmitAR_PeerArchives_InDepRefs(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out string) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]string{},
			Outputs:          ToVFSSlice([]string{out}),
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	o1 := "$(BUILD_ROOT)/build/cow/on/a.c.o"
	o2 := "$(BUILD_ROOT)/build/cow/on/b.c.o"
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []string{o1, o2}

	peer1 := makeLeaf("$(BUILD_ROOT)/some/peer/libsome-peer.a")
	peer2 := makeLeaf("$(BUILD_ROOT)/other/peer/libother-peer.a")
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, nil, e)
	got := e.nodes[arRef.id]

	wantDepRefs := len(objRefs) + len(peerArchiveRefs)

	if len(got.DepRefs) != wantDepRefs {
		t.Errorf("DepRefs len = %d, want %d (objRefs=%d + peerArchiveRefs=%d)",
			len(got.DepRefs), wantDepRefs, len(objRefs), len(peerArchiveRefs))
	}
}

// TestEmitAR_InputsSorted verifies that EmitAR sorts the .o paths
// alphabetically in inputs, regardless of caller-supplied order.
func TestEmitAR_InputsSorted(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out string) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]string{},
			Outputs:          ToVFSSlice([]string{out}),
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	z := "$(BUILD_ROOT)/build/cow/on/z.c.o"
	m := "$(BUILD_ROOT)/build/cow/on/m.c.o"
	a := "$(BUILD_ROOT)/build/cow/on/a.c.o"
	objPaths := []string{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, nil, e)
	got := e.nodes[arRef.id]

	inputs := got.Inputs
	if len(inputs) != 4 {
		t.Fatalf("inputs len = %d, want 4", len(inputs))
	}

	inputObjs := vfsStrings(inputs[:3])

	if !sort.StringsAreSorted(inputObjs) {
		t.Errorf("inputs .o paths not sorted: %v", inputObjs)
	}

	wantInputObjs := []string{a, m, z}

	if !reflect.DeepEqual(inputObjs, wantInputObjs) {
		t.Errorf("inputs .o mismatch:\n  want %v\n  got  %v", wantInputObjs, inputObjs)
	}
}

// TestEmitAR_CmdArgsPreservesDeclarationOrder verifies that EmitAR
// preserves the caller-supplied (SRCS declaration) order in
// cmd_args[10:], not sorted.
func TestEmitAR_CmdArgsPreservesDeclarationOrder(t *testing.T) {
	e := NewBufferedEmitter()

	makeLeaf := func(out string) NodeRef {
		return e.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
			Env:              map[string]string{},
			Inputs:           ToVFSSlice([]string{}),
			KV:               map[string]string{},
			Outputs:          ToVFSSlice([]string{out}),
			Platform:         "default-linux-aarch64",
			Requirements:     map[string]interface{}{},
			Tags:             []string{},
			TargetProperties: map[string]string{},
		})
	}

	z := "$(BUILD_ROOT)/build/cow/on/z.c.o"
	m := "$(BUILD_ROOT)/build/cow/on/m.c.o"
	a := "$(BUILD_ROOT)/build/cow/on/a.c.o"
	objPaths := []string{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, nil, e)
	got := e.nodes[arRef.id]

	cmdArgs := got.Cmds[0].CmdArgs
	if len(cmdArgs) != 13 {
		t.Fatalf("cmd_args len = %d, want 13", len(cmdArgs))
	}

	trailing := cmdArgs[10:]
	wantTrailing := []string{z, m, a}

	if !reflect.DeepEqual(trailing, wantTrailing) {
		t.Errorf("cmd_args .o order mismatch:\n  want %v\n  got  %v", wantTrailing, trailing)
	}
}
