package main

import (
	"slices"
	"strings"
	"testing"
)

// ld_test.go — byte-exact regression test for EmitLD against the
// reference graph for `tools/archiver/archiver` (the M2 PROGRAM
// target), plus a synthetic structural test.
//
// Strategy mirrors cc_test.go / as_test.go: the test does its own
// os.ReadFile + json.Unmarshal of the reference graph, locates the LD
// node by output path, and compares EmitLD's output field-by-field
// (UID/SelfUID/StatsUID excluded, since they are Finalize-computed).
//
// Inputs to EmitLD are reconstructed from the reference: peer
// archive paths in PEERDIR walk order extracted from cmd[2]'s
// `--start-group ... --end-group` block; pluginPaths and globalPaths
// extracted from the same cmd. Stub NodeRefs are wired through a
// fresh BufferedEmitter so that the LD node can refer to them as
// DepRefs without a real PEERDIR walk (which is PR-25's job — PR-24
// just lands the rule).

// referenceLDOutput is the output path used to locate the reference
// LD node for `tools/archiver`.
const referenceLDOutput = "$(B)/tools/archiver/archiver"

// archiverPeerLibPaths are the 32 peer LIBRARY archive paths in the
// exact PEERDIR walk order observed in the reference graph's cmd[2].
// The order is NON-ALPHABETICAL (R14): it mirrors the recursive
// PEERDIR-declaration walk that PR-25 will reproduce. PR-24 uses this
// list verbatim to bypass the walker and pin EmitLD's output byte-
// exact.
var archiverPeerLibPaths = []string{
	"contrib/libs/cxxsupp/libcxxabi-parts/liblibs-cxxsupp-libcxxabi-parts.a",
	"contrib/libs/libunwind/libcontrib-libs-libunwind.a",
	"contrib/libs/cxxsupp/libcxxrt/liblibs-cxxsupp-libcxxrt.a",
	"contrib/libs/cxxsupp/builtins/liblibs-cxxsupp-builtins.a",
	"contrib/libs/cxxsupp/libcxx/liblibs-cxxsupp-libcxx.a",
	"util/charset/libutil-charset.a",
	"contrib/libs/zlib/libcontrib-libs-zlib.a",
	"contrib/libs/double-conversion/libcontrib-libs-double-conversion.a",
	"contrib/libs/libc_compat/libcontrib-libs-libc_compat.a",
	"contrib/libs/linuxvdso/original/liblibs-linuxvdso-original.a",
	"contrib/libs/linuxvdso/libcontrib-libs-linuxvdso.a",
	"util/libyutil.a",
	"build/cow/on/libbuild-cow-on.a",
	"library/cpp/malloc/api/libcpp-malloc-api.a",
	"contrib/restricted/abseil-cpp/libcontrib-restricted-abseil-cpp.a",
	"contrib/libs/tcmalloc/malloc_extension/liblibs-tcmalloc-malloc_extension.a",
	"library/cpp/malloc/tcmalloc/libcpp-malloc-tcmalloc.a",
	"contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.a",
	"contrib/libs/musl/libcontrib-libs-musl.a",
	"contrib/libs/musl/full/liblibs-musl-full.a",
	"library/cpp/archive/liblibrary-cpp-archive.a",
	"contrib/libs/nayuki_md5/libcontrib-libs-nayuki_md5.a",
	"contrib/libs/base64/avx2/liblibs-base64-avx2.a",
	"contrib/libs/base64/ssse3/liblibs-base64-ssse3.a",
	"contrib/libs/base64/neon32/liblibs-base64-neon32.a",
	"contrib/libs/base64/neon64/liblibs-base64-neon64.a",
	"contrib/libs/base64/plain32/liblibs-base64-plain32.a",
	"contrib/libs/base64/plain64/liblibs-base64-plain64.a",
	"library/cpp/string_utils/base64/libcpp-string_utils-base64.a",
	"library/cpp/digest/md5/libcpp-digest-md5.a",
	"library/cpp/colorizer/liblibrary-cpp-colorizer.a",
	"library/cpp/getopt/small/libcpp-getopt-small.a",
}

// archiverPluginPaths is the single plugin (musl pyplugin) referenced
// by the archiver LD's `--start-plugins ... --end-plugins` block.
var archiverPluginPaths = []string{
	"$(B)/contrib/libs/musl/include/musl.py.pyplugin",
}

// archiverGlobalPaths is the single global archive (tcmalloc no-percpu
// global) referenced by the archiver LD's `-Wl,--whole-archive`
// section. BUILD_ROOT-relative (no $(B)/ prefix — link_exe.py
// resolves these against `cwd: $(B)`).
var archiverGlobalPaths = []string{
	"contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.global.a",
}

// loadReferenceLDNode reads the on-disk reference graph and returns the
// LD node whose first output is referenceLDOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// when the node is missing.
// TestEmitLD_ToolsArchiver_ByteExact pins the 4-cmd LD node against
// the reference graph entry for `tools/archiver/archiver`. The test
// supplies the peer-lib / plugin / global / cc paths directly (not
// via Gen's recursion — that lands in PR-25). Stub NodeRefs are
// minted via fresh BufferedEmitter Emit calls so the DepRef wiring
// is exercised end-to-end.
// ldFieldName builds a stable diagnostic name for a per-cmd field so
// fieldEqual's mismatch report identifies which cmd index is at fault
// without a freeform format string.
func ldFieldName(ci int, suffix string) string {
	switch ci {
	case 0:
		return "cmds[0]." + suffix
	case 1:
		return "cmds[1]." + suffix
	case 2:
		return "cmds[2]." + suffix
	case 3:
		return "cmds[3]." + suffix
	default:
		return "cmds[?]." + suffix
	}
}

// TestEmitLD_SyntheticPROGRAM verifies the structural shape of the LD
// node for a 1-source PROGRAM with zero PEERDIR. The synthetic case
// does not match any reference (no archiver-style peer closure or
// global archives), so the test asserts only that the four cmds are
// present and carry the expected scaffolding (vcs_info / clang
// compile / link_exe / fs_tools), the output path is correct, and the
// node carries the LD kv markers.
func TestEmitLD_SyntheticPROGRAM(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{
		KV: map[string]interface{}{"p": "STUB"},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := EmitLD(
		instance,
		"", // empty falls back to lastPathComponent → "prog"
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, // objcopy slot
		nil,   // moduleCFlags
		nil,   // peerCFlagsGlobal
		nil,   // moduleScopeCFlags
		nil,   // peerLDFlagsGlobal
		nil,   // ownLDFlags
		nil,   // ownRPathFlags
		nil,   // peerRPathFlagsGlobal
		nil,   // EXTRALIBS / OBJADDE_LIB_GLOBAL
		false, // noCompilerWarnings
		false, // wantsStrip (cpp PROGRAM)
		false, // SPLIT_DWARF
		testHostP,
		emit,
	)

	got := emit.nodes[ldRef.id]

	if len(got.Cmds) != 4 {
		t.Fatalf("Cmds = %d, want 4", len(got.Cmds))
	}

	// cmd[0]: vcs_info.py
	if got.Cmds[0].CmdArgs[1] != "$(S)/build/scripts/vcs_info.py" {
		t.Errorf("cmd[0] does not invoke vcs_info.py: %q", got.Cmds[0].CmdArgs[1])
	}

	// cmd[1]: clang. With -c -o.
	wantCC := testTargetP.Tools.CC
	if got.Cmds[1].CmdArgs[0] != wantCC {
		t.Errorf("cmd[1][0] = %q, want %q", got.Cmds[1].CmdArgs[0], wantCC)
	}

	// cmd[2]: link_exe.py.
	if got.Cmds[2].CmdArgs[1] != "$(S)/build/scripts/link_exe.py" {
		t.Errorf("cmd[2] does not invoke link_exe.py: %q", got.Cmds[2].CmdArgs[1])
	}

	if got.Cmds[2].Cwd != "$(B)" {
		t.Errorf("cmd[2].cwd = %q, want $(B)", got.Cmds[2].Cwd)
	}

	// cmd[3]: fs_tools.py
	if got.Cmds[3].CmdArgs[1] != "$(S)/build/scripts/fs_tools.py" {
		t.Errorf("cmd[3] does not invoke fs_tools.py: %q", got.Cmds[3].CmdArgs[1])
	}

	// Output path: $(B)/some/prog/prog.
	wantOut := "$(B)/some/prog/prog"
	if len(got.Outputs) != 1 || got.Outputs[0].String() != wantOut {
		t.Errorf("outputs = %#v, want [%q]", got.Outputs, wantOut)
	}

	startIdx := slices.Index(got.Cmds[2].CmdArgs, "--start-plugins")
	endIdx := slices.Index(got.Cmds[2].CmdArgs, "--end-plugins")
	if startIdx < 0 || endIdx != startIdx+1 {
		t.Fatalf("synthetic LD plugin markers = %v, want adjacent empty --start-plugins/--end-plugins", got.Cmds[2].CmdArgs)
	}

	// kv: p=LD, pc=light-blue, show_out=yes.
	wantKV := map[string]string{"p": "LD", "pc": "light-blue", "show_out": "yes"}
	for k, v := range wantKV {
		if got.KV[k] != v {
			t.Errorf("kv[%q] = %q, want %q", k, got.KV[k], v)
		}
	}

	// target_properties: module_dir + module_lang + module_type=bin.
	if got.TargetProperties["module_type"] != "bin" {
		t.Errorf("target_properties.module_type = %q, want bin", got.TargetProperties["module_type"])
	}

	// DepRefs: 1 (the single own .cpp.o).
	if len(got.DepRefs) != 1 {
		t.Errorf("DepRefs = %d, want 1", len(got.DepRefs))
	}
}

func TestEmitLD_SplitDwarfCommandsCarryDistbuildEnv(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{
		KV: map[string]interface{}{"p": "STUB"},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := EmitLD(
		instance,
		"", // empty falls back to lastPathComponent → "prog"
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, // objcopy slot
		nil,   // moduleCFlags
		nil,   // peerCFlagsGlobal
		nil,   // moduleScopeCFlags
		nil,   // peerLDFlagsGlobal
		nil,   // ownLDFlags
		nil,   // ownRPathFlags
		nil,   // peerRPathFlagsGlobal
		nil,   // EXTRALIBS / OBJADDE_LIB_GLOBAL
		false, // noCompilerWarnings
		false, // wantsStrip
		true,  // SPLIT_DWARF
		testHostP,
		emit,
	)

	got := emit.nodes[ldRef.id]

	if len(got.Cmds) != 7 {
		t.Fatalf("Cmds = %d, want 7", len(got.Cmds))
	}

	gotOutputs := vfsStrings(got.Outputs)
	for _, wantOut := range []string{
		"$(B)/some/prog/prog",
		"$(B)/some/prog/prog.debug",
	} {
		if !slices.Contains(gotOutputs, wantOut) {
			t.Fatalf("outputs = %#v, want to contain %q", gotOutputs, wantOut)
		}
	}

	if !slices.Equal(got.Cmds[4].CmdArgs, []string{testTargetP.Tools.Objcopy, "--only-keep-debug", "$(B)/some/prog/prog", "$(B)/some/prog/prog.debug"}) {
		t.Fatalf("cmd[4].cmd_args = %#v", got.Cmds[4].CmdArgs)
	}
	if !slices.Equal(got.Cmds[5].CmdArgs, []string{testTargetP.Tools.Strip, "--strip-debug", "$(B)/some/prog/prog"}) {
		t.Fatalf("cmd[5].cmd_args = %#v", got.Cmds[5].CmdArgs)
	}
	if !slices.Equal(got.Cmds[6].CmdArgs, []string{testTargetP.Tools.Objcopy, "--remove-section=.gnu_debuglink", "--add-gnu-debuglink", "$(B)/some/prog/prog.debug", "$(B)/some/prog/prog"}) {
		t.Fatalf("cmd[6].cmd_args = %#v", got.Cmds[6].CmdArgs)
	}

	for _, idx := range []int{4, 5, 6} {
		if len(got.Cmds[idx].Env) != 1 {
			t.Fatalf("cmd[%d].env len = %d, want 1 (env=%#v)", idx, len(got.Cmds[idx].Env), got.Cmds[idx].Env)
		}
		if got.Cmds[idx].Env["ARCADIA_ROOT_DISTBUILD"] != "$(S)" {
			t.Fatalf("cmd[%d].env[ARCADIA_ROOT_DISTBUILD] = %q, want $(S)", idx, got.Cmds[idx].Env["ARCADIA_ROOT_DISTBUILD"])
		}
		if got.Cmds[idx].Cwd != "" {
			t.Fatalf("cmd[%d].cwd = %q, want empty", idx, got.Cmds[idx].Cwd)
		}
	}
}

// TestEmitLD_AcceptsHostPIC verifies PR-25's lift of PR-24's
// host-PIC guard. Cross-platform recursion (D31) requires building
// host PROGRAM modules (ragel6/yasm), so EmitLD now accepts
// `Platform.PIC=true`. The cmd_args bundle is still the target shape;
// byte-exact host LD pinning is PR-26+ scope. This test only
// asserts the call no longer throws and the resulting node carries
// `host_platform=true`.
func TestEmitLD_AcceptsHostPIC(t *testing.T) {
	emit := NewBufferedEmitter()
	stub := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})

	ref := EmitLD(
		hostInstance("some/prog"),
		"", // empty falls back to lastPathComponent → "prog"
		[]NodeRef{stub}, []VFS{Intern("$(B)/some/prog/main.cpp.o")},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil, // objcopy slot
		nil,   // moduleCFlags
		nil,   // peerCFlagsGlobal
		nil,   // moduleScopeCFlags
		nil,   // peerLDFlagsGlobal
		nil,   // ownLDFlags
		nil,   // ownRPathFlags
		nil,   // peerRPathFlagsGlobal
		nil,   // EXTRALIBS / OBJADDE_LIB_GLOBAL
		false, // noCompilerWarnings
		false, // wantsStrip
		false, // SPLIT_DWARF
		testHostP,
		emit,
	)

	got := emit.nodes[ref.id]

	if got.Platform != string(PlatformDefaultLinuxX8664) {
		t.Errorf("platform = %q, want %q", got.Platform, PlatformDefaultLinuxX8664)
	}

	if !nodeHasHostTag(got.Tags) {
		t.Errorf("tags do not carry \"tool\" baseline (host_platform-equivalent): %v", got.Tags)
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [\"tool\"]", got.Tags)
	}
}

func TestComposeProgramLinkTrailer_NonPICRPathTrailerKeepsNoPie(t *testing.T) {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["LLD_TOOL"] = "$(LLD_ROOT)/bin/ld.lld"
	flags["PIC"] = "no"

	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")

	got := composeProgramLinkTrailer(
		p,
		nil,
		nil,
		nil,
		[]string{"-Wl,-rpath,$ORIGIN"},
		[]string{"-Wl,-rpath,$ORIGIN"},
		nil,
		false,
	)

	want := []string{
		"-rdynamic",
		"-ldl",
		"-lrt",
		"-Wl,--no-as-needed",
		"-Wl,-rpath,$ORIGIN",
		"-Wl,--gdb-index",
		"-Wl,-rpath,$ORIGIN",
		"-fuse-ld=lld",
		"--ld-path=$(LLD_ROOT)/bin/ld.lld",
		"-Wl,--no-rosegment",
		"-Wl,--build-id=sha1",
		"-nodefaultlibs",
		"-lpthread",
		"-lc",
		"-lm",
		"-Wl,--gc-sections",
		"-Wl,-no-pie",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("composeProgramLinkTrailer mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEmitLD_ThreadsWholeArchiveLibsToInputsAndDeps(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})
	wholeRef := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})

	instance := targetInstance("some/prog")
	wholeArchivePath := "some/prog/libproto_cpp.a"

	ldRef := EmitLD(
		instance,
		"",
		[]NodeRef{mainRef}, []VFS{Intern("$(B)/some/prog/main.cpp.o")},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		[]NodeRef{wholeRef}, []VFS{Build(wholeArchivePath)},
		nil,
		nil, nil,
		nil, nil,
		nil, // moduleCFlags
		nil, // peerCFlagsGlobal
		nil, // moduleScopeCFlags
		nil, // peerLDFlagsGlobal
		nil, // ownLDFlags
		nil, // ownRPathFlags
		nil, // peerRPathFlagsGlobal
		nil, // objAddLibsGlobal
		false,
		false,
		false,
		testHostP,
		emit,
	)

	got := emit.nodes[ldRef.id]
	if !slices.Contains(got.Inputs, Build(wholeArchivePath)) {
		t.Fatalf("inputs do not contain whole-archive path %q: %#v", wholeArchivePath, got.Inputs)
	}

	if !slices.Contains(got.DepRefs, wholeRef) {
		t.Fatalf("DepRefs do not contain whole-archive ref %+v: %#v", wholeRef, got.DepRefs)
	}

	cmdArgs := got.Cmds[2].CmdArgs
	found := false
	for i := 0; i+1 < len(cmdArgs); i++ {
		if cmdArgs[i] == "--whole-archive-libs" && cmdArgs[i+1] == wholeArchivePath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cmd[2] missing whole-archive marker for %q: %#v", wholeArchivePath, cmdArgs)
	}
}

func TestEmitLD_DedupsBuildRootInputsAcrossPeerAndWholeArchivePaths(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})
	peerRef := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})
	wholeRef := emit.Emit(&Node{KV: map[string]interface{}{"p": "STUB"}})

	instance := targetInstance("some/prog")
	dupPath := Intern("$(B)/some/prog/libproto_cpp.a")

	ldRef := EmitLD(
		instance,
		"",
		[]NodeRef{mainRef}, []VFS{Intern("$(B)/some/prog/main.cpp.o")},
		[]NodeRef{peerRef}, []VFS{dupPath},
		nil,
		nil, nil,
		nil, nil,
		[]NodeRef{wholeRef}, []VFS{dupPath},
		nil,
		nil, nil,
		nil, nil,
		nil, // moduleCFlags
		nil, // peerCFlagsGlobal
		nil, // moduleScopeCFlags
		nil, // peerLDFlagsGlobal
		nil, // ownLDFlags
		nil, // ownRPathFlags
		nil, // peerRPathFlagsGlobal
		nil, // objAddLibsGlobal
		false,
		false,
		false,
		testHostP,
		emit,
	)

	got := emit.nodes[ldRef.id]
	count := 0
	for _, input := range got.Inputs {
		if input == dupPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("inputs contain %d copies of %q, want 1: %#v", count, dupPath.String(), got.Inputs)
	}

	if !slices.Contains(got.DepRefs, wholeRef) {
		t.Fatalf("DepRefs do not contain whole-archive ref %+v: %#v", wholeRef, got.DepRefs)
	}

	cmdArgs := got.Cmds[2].CmdArgs
	found := false
	for i := 0; i+1 < len(cmdArgs); i++ {
		if cmdArgs[i] == "--whole-archive-libs" && cmdArgs[i+1] == dupPath.Rel() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cmd[2] missing whole-archive marker for %q: %#v", dupPath.Rel(), cmdArgs)
	}
}

// TestEmitLD_LengthMismatchPanics verifies the precondition checks on all
// four ref/path slice pairs (cc, peerLD, plugin, global).
func TestEmitLD_LengthMismatchPanics(t *testing.T) {
	tests := []struct {
		name                                                string
		ccRefs, peerRefs, pluginRefs, globalRefs, wholeRefs []NodeRef
		ccPaths                                             []VFS
		peerPaths, pluginPaths, globalPaths, wholePaths     []VFS
		wantSubstr                                          string
	}{
		{name: "ccRefs vs ccPaths", ccRefs: []NodeRef{{}}, wantSubstr: "ccRefs"},
		{name: "peerLDRefs vs peerLibPaths", peerRefs: []NodeRef{{}}, wantSubstr: "peerLD"},
		{name: "pluginRefs vs pluginPaths", pluginRefs: []NodeRef{{}}, wantSubstr: "plugin"},
		{name: "globalRefs vs globalPaths", globalRefs: []NodeRef{{}}, wantSubstr: "global"},
		{name: "wholeArchiveRefs vs wholeArchivePaths", wholeRefs: []NodeRef{{}}, wantSubstr: "wholeArchive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewBufferedEmitter()
			instance := targetInstance("test/prog")

			exc := Try(func() {
				EmitLD(instance, "prog", tc.ccRefs, tc.ccPaths, tc.peerRefs, tc.peerPaths, nil, tc.pluginRefs, tc.pluginPaths, tc.globalRefs, tc.globalPaths, tc.wholeRefs, tc.wholePaths, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, false, false, testHostP, e)
			})

			if exc == nil {
				t.Fatal("expected exception")
			}

			if !strings.Contains(exc.Error(), tc.wantSubstr) {
				t.Errorf("unexpected error: %v", exc)
			}
		})
	}
}
