package main

import (
	"slices"
	"strings"
	"testing"
)

const referenceLDOutput = "$(B)/tools/archiver/archiver"

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
	"contrib/libs/foolib/libcontrib-libs-foolib.a",
	"contrib/libs/foolib/full/liblibs-foolib-full.a",
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

var archiverPluginPaths = []string{
	"$(B)/contrib/libs/foolib/include/foolib.py.pyplugin",
}

var archiverGlobalPaths = []string{
	"contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.global.a",
}

func TestEmitLD_SyntheticPROGRAM(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{
		KV: KV{P: pkSTUB},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := EmitLD(
		instance,
		"",
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		false,
		"",
		testHostP,
		nil,
		emit,
	)

	got := emit.nodes[ldRef]

	if len(got.Cmds) != 4 {
		t.Fatalf("Cmds = %d, want 4", len(got.Cmds))
	}

	if got.Cmds[0].CmdArgs[1] != "$(S)/build/scripts/vcs_info.py" {
		t.Errorf("cmd[0] does not invoke vcs_info.py: %q", got.Cmds[0].CmdArgs[1])
	}

	wantCC := testTargetP.Tools.CC
	if got.Cmds[1].CmdArgs[0] != wantCC {
		t.Errorf("cmd[1][0] = %q, want %q", got.Cmds[1].CmdArgs[0], wantCC)
	}

	if got.Cmds[2].CmdArgs[1] != "$(S)/build/scripts/link_exe.py" {
		t.Errorf("cmd[2] does not invoke link_exe.py: %q", got.Cmds[2].CmdArgs[1])
	}

	if got.Cmds[2].Cwd != "$(B)" {
		t.Errorf("cmd[2].cwd = %q, want $(B)", got.Cmds[2].Cwd)
	}

	if got.Cmds[3].CmdArgs[1] != "$(S)/build/scripts/fs_tools.py" {
		t.Errorf("cmd[3] does not invoke fs_tools.py: %q", got.Cmds[3].CmdArgs[1])
	}

	wantOut := "$(B)/some/prog/prog"
	if len(got.Outputs) != 1 || got.Outputs[0].String() != wantOut {
		t.Errorf("outputs = %#v, want [%q]", got.Outputs, wantOut)
	}

	startIdx := slices.Index(got.Cmds[2].CmdArgs, "--start-plugins")
	endIdx := slices.Index(got.Cmds[2].CmdArgs, "--end-plugins")
	if startIdx < 0 || endIdx != startIdx+1 {
		t.Fatalf("synthetic LD plugin markers = %v, want adjacent empty --start-plugins/--end-plugins", got.Cmds[2].CmdArgs)
	}

	if got.KV.P != pkLD || got.KV.PC != pcLightBlue || got.KV.ShowOut != "yes" {
		t.Errorf("kv = %+v, want {P:LD PC:light-blue ShowOut:yes}", got.KV)
	}

	if got.TargetProperties.ModuleType != "bin" {
		t.Errorf("target_properties.module_type = %q, want bin", got.TargetProperties.ModuleType)
	}

	if len(got.DepRefs) != 1 {
		t.Errorf("DepRefs = %d, want 1", len(got.DepRefs))
	}
}

func TestEmitLD_SplitDwarfCommandsCarryDistbuildEnv(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{
		KV: KV{P: pkSTUB},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := EmitLD(
		instance,
		"",
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		true,
		"",
		testHostP,
		nil,
		emit,
	)

	got := emit.nodes[ldRef]

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
		if got.Cmds[idx].Env[0] != (EnvVar{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}) {
			t.Fatalf("cmd[%d].env = %#v, want ARCADIA_ROOT_DISTBUILD=$(S)", idx, got.Cmds[idx].Env)
		}
		if got.Cmds[idx].Cwd != "" {
			t.Fatalf("cmd[%d].cwd = %q, want empty", idx, got.Cmds[idx].Cwd)
		}
	}
}

func TestEmitLD_AcceptsHostPIC(t *testing.T) {
	emit := NewBufferedEmitter()
	stub := emit.Emit(&Node{KV: KV{P: pkSTUB}})

	ref := EmitLD(
		hostInstance("some/prog"),
		"",
		[]NodeRef{stub}, []VFS{Intern("$(B)/some/prog/main.cpp.o")},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		false,
		"",
		testHostP,
		nil,
		emit,
	)

	got := emit.nodes[ref]

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

	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "", nil)

	got := composeProgramLinkTrailer(
		p,
		"",
		nil,
		nil,
		nil,
		internArgs([]string{"-Wl,-rpath,$ORIGIN"}),
		internArgs([]string{"-Wl,-rpath,$ORIGIN"}),
		nil,
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
	mainRef := emit.Emit(&Node{KV: KV{P: pkSTUB}})
	// A whole-archive lib is one of the peer archives (linked with --whole-archive),
	// so its ref is in BOTH peerLDRefs and wholeArchiveRefs — the same node.
	wholeRef := emit.Emit(&Node{KV: KV{P: pkSTUB}})

	instance := targetInstance("some/prog")
	wholeArchivePath := "some/prog/libproto_cpp.a"

	ldRef := EmitLD(
		instance,
		"",
		[]NodeRef{mainRef}, []VFS{Intern("$(B)/some/prog/main.cpp.o")},
		[]NodeRef{wholeRef}, []VFS{Build(wholeArchivePath)},
		nil,
		nil, nil,
		nil, nil,
		[]NodeRef{wholeRef}, []VFS{Build(wholeArchivePath)},
		nil,
		nil, nil,
		nil, nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		false,
		"",
		testHostP,
		nil,
		emit,
	)

	got := emit.nodes[ldRef]
	if !slices.Contains(got.Inputs, Build(wholeArchivePath)) {
		t.Fatalf("inputs do not contain whole-archive path %q: %#v", wholeArchivePath, got.Inputs)
	}

	// The lib is a peer, so it is in DepRefs — exactly ONCE (whole-archive is a
	// link attribute, not a second dep source).
	depCount := 0
	for _, r := range got.DepRefs {
		if r == wholeRef {
			depCount++
		}
	}
	if depCount != 1 {
		t.Fatalf("whole-archive/peer ref in DepRefs %d times, want 1: %#v", depCount, got.DepRefs)
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
	mainRef := emit.Emit(&Node{KV: KV{P: pkSTUB}})
	// Same node reached as both a peer archive and a whole-archive lib.
	peerRef := emit.Emit(&Node{KV: KV{P: pkSTUB}})

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
		[]NodeRef{peerRef}, []VFS{dupPath},
		nil,
		nil, nil,
		nil, nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
		false,
		"",
		testHostP,
		nil,
		emit,
	)

	got := emit.nodes[ldRef]
	count := 0
	for _, input := range got.Inputs {
		if input == dupPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("inputs contain %d copies of %q, want 1: %#v", count, dupPath.String(), got.Inputs)
	}

	depCount := 0
	for _, r := range got.DepRefs {
		if r == peerRef {
			depCount++
		}
	}
	if depCount != 1 {
		t.Fatalf("peer/whole-archive ref in DepRefs %d times, want 1: %#v", depCount, got.DepRefs)
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

func TestEmitLD_LengthMismatchPanics(t *testing.T) {
	tests := []struct {
		name                                                string
		ccRefs, peerRefs, pluginRefs, globalRefs, wholeRefs []NodeRef
		ccPaths                                             []VFS
		peerPaths, pluginPaths, globalPaths, wholePaths     []VFS
		wantSubstr                                          string
	}{
		{name: "ccRefs vs ccPaths", ccRefs: []NodeRef{0}, wantSubstr: "ccRefs"},
		{name: "peerLDRefs vs peerLibPaths", peerRefs: []NodeRef{0}, wantSubstr: "peerLD"},
		{name: "pluginRefs vs pluginPaths", pluginRefs: []NodeRef{0}, wantSubstr: "plugin"},
		{name: "globalRefs vs globalPaths", globalRefs: []NodeRef{0}, wantSubstr: "global"},
		{name: "wholeArchiveRefs vs wholeArchivePaths", wholeRefs: []NodeRef{0}, wantSubstr: "wholeArchive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewBufferedEmitter()
			instance := targetInstance("test/prog")

			exc := Try(func() {
				EmitLD(instance, "prog", tc.ccRefs, tc.ccPaths, tc.peerRefs, tc.peerPaths, nil, tc.pluginRefs, tc.pluginPaths, tc.globalRefs, tc.globalPaths, tc.wholeRefs, tc.wholePaths, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, false, false, "", testHostP, nil, e)
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
