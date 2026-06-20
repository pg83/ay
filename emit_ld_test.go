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
	emit := newBufferedEmitter()
	mainRef := emit.emit(&Node{Platform: &Platform{},
		KV: KV{P: pkSTUB},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
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
		false,
		false,
		0,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes[ldRef]

	if len(got.Cmds) != 4 {
		t.Fatalf("Cmds = %d, want 4", len(got.Cmds))
	}

	if got.Cmds[0].CmdArgs.flat()[1].string() != "$(S)/build/scripts/vcs_info.py" {
		t.Errorf("cmd[0] does not invoke vcs_info.py: %q", got.Cmds[0].CmdArgs.flat()[1].string())
	}

	wantCC := testToolchain().CC.string()
	if got.Cmds[1].CmdArgs.flat()[0].string() != wantCC {
		t.Errorf("cmd[1][0] = %q, want %q", got.Cmds[1].CmdArgs.flat()[0].string(), wantCC)
	}

	if got.Cmds[2].CmdArgs.flat()[1].string() != "$(S)/build/scripts/link_exe.py" {
		t.Errorf("cmd[2] does not invoke link_exe.py: %q", got.Cmds[2].CmdArgs.flat()[1].string())
	}

	if got.Cmds[2].Cwd.string() != "$(B)" {
		t.Errorf("cmd[2].cwd = %q, want $(B)", got.Cmds[2].Cwd.string())
	}

	if got.Cmds[3].CmdArgs.flat()[1].string() != "$(S)/build/scripts/fs_tools.py" {
		t.Errorf("cmd[3] does not invoke fs_tools.py: %q", got.Cmds[3].CmdArgs.flat()[1].string())
	}

	wantOut := "$(B)/some/prog/prog"
	if len(got.Outputs) != 1 || got.Outputs[0].string() != wantOut {
		t.Errorf("outputs = %#v, want [%q]", got.Outputs, wantOut)
	}

	startIdx := slices.Index(strStrs(got.Cmds[2].CmdArgs.flat()), "--start-plugins")
	endIdx := slices.Index(strStrs(got.Cmds[2].CmdArgs.flat()), "--end-plugins")
	if startIdx < 0 || endIdx != startIdx+1 {
		t.Fatalf("synthetic LD plugin markers = %v, want adjacent empty --start-plugins/--end-plugins", got.Cmds[2].CmdArgs.flat())
	}

	if got.KV.P != pkLD || got.KV.PC != pcLightBlue || !got.KV.ShowOut {
		t.Errorf("kv = %+v, want {P:LD PC:light-blue ShowOut:true}", got.KV)
	}

	if got.TargetProperties.ModuleType != mtBin {
		t.Errorf("target_properties.module_type = %q, want bin", got.TargetProperties.ModuleType.string())
	}

	// ccRef + the vcs.json producer node (emitVCSNode).
	if len(got.DepRefs) != 2 {
		t.Errorf("DepRefs = %d, want 2", len(got.DepRefs))
	}
}

func TestEmitLD_SplitDwarfCommandsCarryDistbuildEnv(t *testing.T) {
	emit := newBufferedEmitter()
	mainRef := emit.emit(&Node{Platform: &Platform{},
		KV: KV{P: pkSTUB},
	})
	mainPath := "$(B)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{ParseVFSOrSource(mainPath)},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
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
		false,
		true,
		0,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
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

	if !slices.Equal(strStrs(got.Cmds[4].CmdArgs.flat()), []string{testToolchain().Objcopy.string(), "--only-keep-debug", "$(B)/some/prog/prog", "$(B)/some/prog/prog.debug"}) {
		t.Fatalf("cmd[4].cmd_args = %#v", got.Cmds[4].CmdArgs.flat())
	}
	if !slices.Equal(strStrs(got.Cmds[5].CmdArgs.flat()), []string{testToolchain().Strip.string(), "--strip-debug", "$(B)/some/prog/prog"}) {
		t.Fatalf("cmd[5].cmd_args = %#v", got.Cmds[5].CmdArgs.flat())
	}
	if !slices.Equal(strStrs(got.Cmds[6].CmdArgs.flat()), []string{testToolchain().Objcopy.string(), "--remove-section=.gnu_debuglink", "--add-gnu-debuglink", "$(B)/some/prog/prog.debug", "$(B)/some/prog/prog"}) {
		t.Fatalf("cmd[6].cmd_args = %#v", got.Cmds[6].CmdArgs.flat())
	}

	for _, idx := range []int{4, 5, 6} {
		if len(got.Cmds[idx].Env) != 1 {
			t.Fatalf("cmd[%d].env len = %d, want 1 (env=%#v)", idx, len(got.Cmds[idx].Env), got.Cmds[idx].Env)
		}
		if got.Cmds[idx].Env[0] != (EnvVar{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}) {
			t.Fatalf("cmd[%d].env = %#v, want ARCADIA_ROOT_DISTBUILD=$(S)", idx, got.Cmds[idx].Env)
		}
		if got.Cmds[idx].Cwd != 0 {
			t.Fatalf("cmd[%d].cwd = %q, want empty", idx, got.Cmds[idx].Cwd.string())
		}
	}
}

func TestEmitLD_AcceptsHostPIC(t *testing.T) {
	emit := newBufferedEmitter()
	stub := emit.emit(&Node{Platform: &Platform{}, KV: KV{P: pkSTUB}})

	ref := emitLD(
		hostInstance("some/prog"),
		"prog",
		[]NodeRef{stub}, []VFS{intern("$(B)/some/prog/main.cpp.o")},
		nil, nil,
		nil,
		nil, nil,
		nil, nil,
		nil, nil,
		nil,
		nil, nil,
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
		false,
		false,
		0,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes[ref]

	if string(got.Platform.Target) != string(PlatformDefaultLinuxX8664) {
		t.Errorf("platform = %q, want %q", string(got.Platform.Target), PlatformDefaultLinuxX8664)
	}
}

func TestComposeProgramLinkTrailer_NonPICRPathTrailerKeepsNoPie(t *testing.T) {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["LLD_TOOL"] = "$(LLD_ROOT)/bin/ld.lld"
	flags["PIC"] = "no"

	p := newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "", "")

	got := composeProgramLinkTrailer(
		p,
		nil,
		nil,
		internArgs([]string{"-Wl,-rpath,$ORIGIN"}),
		internArgs([]string{"-Wl,-rpath,$ORIGIN"}),
		nil,
		nil,
		false,
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
		"-nodefaultlibs",
		"-lpthread",
		"-lc",
		"-lm",
		"-Wl,--gc-sections",
		"-Wl,-no-pie",
	}

	if !slices.Equal(strStrs(got), want) {
		t.Fatalf("composeProgramLinkTrailer mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEmitLD_ThreadsWholeArchiveLibsToInputsAndDeps(t *testing.T) {
	emit := newBufferedEmitter()
	mainRef := emit.emit(&Node{Platform: &Platform{}, KV: KV{P: pkSTUB}})
	// A whole-archive lib is one of the peer archives (linked with --whole-archive),
	// so its ref is in BOTH peerLDRefs and wholeArchiveRefs — the same node.
	wholeRef := emit.emit(&Node{Platform: &Platform{}, KV: KV{P: pkSTUB}})

	instance := targetInstance("some/prog")
	wholeArchivePath := "some/prog/libproto_cpp.a"

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{intern("$(B)/some/prog/main.cpp.o")},
		[]NodeRef{wholeRef}, []VFS{build(wholeArchivePath)},
		nil,
		nil, nil,
		nil, nil,
		[]NodeRef{wholeRef}, []VFS{build(wholeArchivePath)},
		nil,
		nil, nil,
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
		false,
		false,
		0,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes[ldRef]
	if !slices.Contains(got.flatInputs(), build(wholeArchivePath)) {
		t.Fatalf("inputs do not contain whole-archive path %q: %#v", wholeArchivePath, got.flatInputs())
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

	cmdArgs := strStrs(got.Cmds[2].CmdArgs.flat())
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
	emit := newBufferedEmitter()
	mainRef := emit.emit(&Node{Platform: &Platform{}, KV: KV{P: pkSTUB}})
	// Same node reached as both a peer archive and a whole-archive lib.
	peerRef := emit.emit(&Node{Platform: &Platform{}, KV: KV{P: pkSTUB}})

	instance := targetInstance("some/prog")
	dupPath := intern("$(B)/some/prog/libproto_cpp.a")

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{intern("$(B)/some/prog/main.cpp.o")},
		[]NodeRef{peerRef}, []VFS{dupPath},
		nil,
		nil, nil,
		nil, nil,
		[]NodeRef{peerRef}, []VFS{dupPath},
		nil,
		nil, nil,
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
		false,
		false,
		0,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes[ldRef]
	count := 0
	for _, input := range got.flatInputs() {
		if input == dupPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("inputs contain %d copies of %q, want 1: %#v", count, dupPath.string(), got.flatInputs())
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

	cmdArgs := strStrs(got.Cmds[2].CmdArgs.flat())
	found := false
	for i := 0; i+1 < len(cmdArgs); i++ {
		if cmdArgs[i] == "--whole-archive-libs" && cmdArgs[i+1] == dupPath.rel() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cmd[2] missing whole-archive marker for %q: %#v", dupPath.rel(), cmdArgs)
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
			e := newBufferedEmitter()
			instance := targetInstance("test/prog")

			exc := try(func() {
				emitLD(instance, "prog", tc.ccRefs, tc.ccPaths, tc.peerRefs, tc.peerPaths, nil, tc.pluginRefs, tc.pluginPaths, tc.globalRefs, tc.globalPaths, tc.wholeRefs, tc.wholePaths, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, false, false, false, false, 0, false, testToolchain(), testHostP, nil, e, emitVCSNode(e, testHostP))
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

func TestGen_SyntheticPROGRAM_EmitsLD(t *testing.T) {
	fs := newMemFS(map[string]string{
		"lone/ya.make": "PROGRAM()\nSRCS(main.cpp)\nEND()\n",
	})

	g := testGen(fs, "lone")

	if len(g.Graph) != 3 {
		t.Fatalf("Gen produced %d nodes, want 3 (1 CC + 1 LD + 1 vcs.json)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	var ld, cc *Node

	for _, n := range g.Graph {
		switch n.KV.P.string() {
		case "LD":
			ld = n
		case "CC":
			cc = n
		}
	}

	if ld == nil {
		t.Fatal("Gen produced no LD node for PROGRAM module")
	}

	if cc == nil {
		t.Fatal("Gen produced no CC node for PROGRAM module")
	}

	if len(ld.Cmds) != 4 {
		t.Errorf("LD Cmds = %d, want 4", len(ld.Cmds))
	}

	wantOut := "$(B)/lone/lone"
	if len(ld.Outputs) != 1 || ld.Outputs[0].string() != wantOut {
		t.Errorf("LD outputs = %#v, want [%q]", ld.Outputs, wantOut)
	}

	if g.Result[0] != ld.UID {
		t.Errorf("result UID = %q, want LD uid %q", g.Result[0], ld.UID)
	}
}

func TestGen_PeerGlobalArchive_ThreadsToLD(t *testing.T) {
	fs := newMemFS(map[string]string{
		"peerlib/ya.make":  "LIBRARY()\nSRCS(regular.cpp)\nGLOBAL_SRCS(global.cpp)\nEND()\n",
		"consumer/ya.make": "PROGRAM()\nSRCS(main.cpp)\nPEERDIR(peerlib)\nEND()\n",
	})

	g := testGen(fs, "consumer")

	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV.P == pkLD {
			ldNode = n
		}
	}

	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}

	arCount := 0
	for _, n := range g.Graph {
		if n.KV.P == pkAR {
			arCount++
		}
	}

	if arCount != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global from peerlib)", arCount)
	}

	if len(graphDeps(g, ldNode)) < 3 {
		t.Errorf("LD Deps = %d, want >= 3 (own CC + peer AR + peer global AR)", len(graphDeps(g, ldNode)))
	}

	expectedInput := "$(B)/peerlib/libpeerlib.global.a"
	foundInInputs := false

	for _, in := range ldNode.flatInputs() {
		if in.string() == expectedInput {
			foundInInputs = true
			break
		}
	}

	if !foundInInputs {
		t.Errorf("expected single-prefixed global archive in inputs; got: %v", ldNode.flatInputs())
	}

	for _, in := range ldNode.flatInputs() {
		if strings.Contains(in.string(), "$(B)/$(B)") {
			t.Errorf("double-prefixed input found: %q", in.string())
		}
	}

	if len(ldNode.Cmds) < 3 {
		t.Fatalf("LD node has %d cmds, want >= 3", len(ldNode.Cmds))
	}

	linkArgs := ldNode.Cmds[2].CmdArgs.flat()
	expectedCmdArg := "peerlib/libpeerlib.global.a"
	foundInCmdArgs := false

	for _, a := range linkArgs {
		if a.string() == expectedCmdArg {
			foundInCmdArgs = true
			break
		}
	}

	if !foundInCmdArgs {
		t.Errorf("expected unprefixed global archive in cmd_args[2]; got: %v", linkArgs)
	}
}

// TestGen_FbsSrcsInduceFlatbuffersLinkDep verifies that a module with .fbs SRCS
// gets contrib/libs/flatbuffers added as an induced PEERDIR (upstream's
// _CPP_FLATC_CMD has .PEERDIR=contrib/libs/flatbuffers). The induced dep must
// appear AFTER all explicit PEERDIRs so that in the LD link command flatbuffers
// lands between the last explicit peer's transitive closure and the library
// itself — matching the upstream link order that sg5 ref exhibits for arrow.
func TestGen_FbsSrcsInduceFlatbuffersLinkDep(t *testing.T) {
	files := map[string]string{
		// A program that peers a library with .fbs SRCS.
		"prog/ya.make":  "PROGRAM()\nPEERDIR(arrowlike)\nSRCS(main.cpp)\nEND()\n",
		"prog/main.cpp": "int main() { return 0; }\n",
		// arrowlike has an explicit peer (peer1) AND a .fbs source.
		// The fix must insert flatbuffers AFTER peer1 in the link order.
		"arrowlike/ya.make":    "LIBRARY()\nPEERDIR(peer1)\nSRCS(lib.cpp Schema.fbs)\nEND()\n",
		"arrowlike/lib.cpp":    "int f() { return 0; }\n",
		"arrowlike/Schema.fbs": "namespace test; table Foo { value:int; }\n",
		"peer1/ya.make":        "LIBRARY()\nSRCS(p1.cpp)\nEND()\n",
		"peer1/p1.cpp":         "int p1() { return 0; }\n",
		// flatbuffers runtime — must have a ya.make so the peerdir resolves.
		"contrib/libs/flatbuffers/ya.make":                           "LIBRARY()\nSRCS(flatbuffers.cpp)\nEND()\n",
		"contrib/libs/flatbuffers/flatbuffers.cpp":                   "int fb() { return 0; }\n",
		"contrib/libs/flatbuffers/flatc/ya.make":                     "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n",
		"contrib/libs/flatbuffers/flatc/main.cpp":                    "int main() { return 0; }\n",
		"contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h": "#pragma once\n",
		"build/scripts/cpp_flatc_wrapper.py":                         "print('stub')\n",
	}

	g := testGen(newMemFS(files), "prog")

	// Find the LD node.
	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV.P == pkLD {
			ldNode = n
			break
		}
	}
	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}

	linkArgs := ldNode.Cmds[2].CmdArgs.flat()
	peer1Idx := indexOfArg(linkArgs, "peer1/libpeer1.a")
	fbIdx := indexOfArg(linkArgs, "contrib/libs/flatbuffers/libcontrib-libs-flatbuffers.a")
	arrowlikeIdx := indexOfArg(linkArgs, "arrowlike/libarrowlike.a")

	if peer1Idx < 0 {
		t.Fatalf("link args missing peer1/libpeer1.a: %v", linkArgs)
	}
	if fbIdx < 0 {
		t.Fatalf("link args missing contrib/libs/flatbuffers/libcontrib-libs-flatbuffers.a: "+
			"induced peerdir from .fbs SRCS not added; args=%v", linkArgs)
	}
	if arrowlikeIdx < 0 {
		t.Fatalf("link args missing arrowlike/libarrowlike.a: %v", linkArgs)
	}
	// Upstream order: peer1 (explicit), then flatbuffers (induced from .fbs), then arrowlike itself.
	if peer1Idx > fbIdx {
		t.Errorf("peer1 [%d] appears after flatbuffers [%d] in link args; want peer1 before flatbuffers", peer1Idx, fbIdx)
	}
	if fbIdx > arrowlikeIdx {
		t.Errorf("flatbuffers [%d] appears after arrowlike [%d] in link args; want flatbuffers before the owning library", fbIdx, arrowlikeIdx)
	}
}

func TestGen_EnumSerializationRuntimePrecedesProtoLibraryArchive(t *testing.T) {
	// Reproduces the fs_codegen / counter_proto residual (T-50). A PROTO_LIBRARY
	// declares an ordinary PEERDIR, then GENERATE_ENUM_SERIALIZATION (which
	// expands inline to PEERDIR(tools/enum_parser/enum_serialization_runtime) at
	// that textual position). A downstream PY3_PROGRAM linking the proto library
	// must list, in its regular start-group archive sequence: the ordinary peer,
	// then enum_serialization_runtime, then the proto library's own archive — the
	// upstream UniqPeers closure order, each exactly once.
	//
	// A second program peer pulls in library/cpp/json/common AFTER the proto
	// closure. The removed PY3_PROGRAM relocation (movePathsBefore enum runtime ->
	// json/common) would yank enum_serialization_runtime out of its closure slot
	// and reinsert it just before json/common, landing it AFTER the proto archive.
	files := map[string]string{
		"app/ya.make":  "PY3_PROGRAM(app)\nDISABLE(PYTHON_SQLITE3)\nENABLE(PYBUILD_NO_PYC)\nPEERDIR(proto_mod)\nPEERDIR(jsondep)\nSRCS(main.cpp)\nEND()\n",
		"app/main.cpp": "int main(){return 0;}\n",

		"proto_mod/ya.make": "PROTO_LIBRARY()\nNO_MYPY()\nENABLE(PYBUILD_NO_PYC)\nPEERDIR(dep/first)\nSRCS(counter.proto)\n" +
			"GENERATE_ENUM_SERIALIZATION(counter.pb.h)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n",
		"proto_mod/counter.proto": "message Counter { optional int32 v = 1; }\n",

		"dep/first/ya.make":   "LIBRARY()\nSRCS(first.cpp)\nEND()\n",
		"dep/first/first.cpp": "int first(){return 0;}\n",

		"tools/enum_parser/enum_serialization_runtime/ya.make":     "LIBRARY()\nSRCS(runtime.cpp)\nEND()\n",
		"tools/enum_parser/enum_serialization_runtime/runtime.cpp": "int runtime(){return 0;}\n",
		"tools/enum_parser/enum_parser/ya.make":                    "PROGRAM(enum_parser)\nSRCS(main.cpp)\nEND()\n",
		"tools/enum_parser/enum_parser/main.cpp":                   "int main(){return 0;}\n",

		"jsondep/ya.make":                 "LIBRARY()\nPEERDIR(library/cpp/json/common)\nSRCS(j.cpp)\nEND()\n",
		"jsondep/j.cpp":                   "int j(){return 0;}\n",
		"library/cpp/json/common/ya.make": "LIBRARY()\nSRCS(jc.cpp)\nEND()\n",
		"library/cpp/json/common/jc.cpp":  "int jc(){return 0;}\n",

		// J-allocator peers (default for PY3_PROGRAM) and the malloc-relocation
		// anchors are walked unconditionally — stub them so the graph resolves.
		"library/cpp/malloc/jemalloc/ya.make": "LIBRARY()\nSRCS(je.cpp)\nEND()\n",
		"library/cpp/malloc/jemalloc/je.cpp":  "int je(){return 0;}\n",
		"library/cpp/malloc/api/ya.make":      "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/api/api.cpp":      "int api(){return 0;}\n",
		"contrib/libs/jemalloc/ya.make":       "LIBRARY()\nSRCS(c.cpp)\nEND()\n",
		"contrib/libs/jemalloc/c.cpp":         "int c(){return 0;}\n",
		"build/cow/on/ya.make":                "LIBRARY()\nSRCS(cow.cpp)\nEND()\n",
		"build/cow/on/cow.cpp":                "int cow(){return 0;}\n",

		// PY3_PROGRAM unconditionally prepends/peers the python runtime — stub the
		// leaf modules so the graph resolves. NO_* flags keep them peer-free.
		"contrib/libs/python/ya.make":                       "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/runtime_py3/main/ya.make":           "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/import_tracing/constructor/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/testing/import_test/ya.make":        "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",

		// PROTO_LIBRARY machinery (protoc + cpp plugin + runtimes).
		"contrib/tools/protoc/ya.make":                         "PROGRAM(protoc)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
		"contrib/tools/protoc/main.cpp":                        "int main(){return 0;}\n",
		"contrib/tools/protoc/plugins/cpp_styleguide/ya.make":  "PROGRAM(cpp_styleguide)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
		"contrib/tools/protoc/plugins/cpp_styleguide/main.cpp": "int main(){return 0;}\n",
		"contrib/libs/protobuf/ya.make":                        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(protobuf.cpp)\nEND()\n",
		"contrib/libs/protobuf/protobuf.cpp":                   "int protobuf(){return 0;}\n",
		"contrib/python/protobuf/py3/ya.make":                  "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"contrib/python/protobuf/ya.make":                      "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	g := testGen(newMemFS(files), "app")

	// Find the program link node: the LD node whose command holds the
	// -Wl,--start-group archive group (the fs_tools link_or_copy LD node does not).
	var linkArgs []STR
	for _, n := range g.Graph {
		if n.KV.P != pkLD {
			continue
		}
		for _, c := range n.Cmds {
			args := c.CmdArgs.flat()
			if indexOfArg(args, "-Wl,--start-group") >= 0 {
				linkArgs = args
				break
			}
		}
		if linkArgs != nil {
			break
		}
	}
	if linkArgs == nil {
		t.Fatal("no program-link LD node with -Wl,--start-group found in graph")
	}

	// Restrict the assertions to the regular -Wl,--start-group … --end-group
	// archive window (the proto library's own archive also appears in the
	// preceding --whole-archive-libs band, which is byte-identical and unrelated).
	sgStart := indexOfArg(linkArgs, "-Wl,--start-group")
	sgEnd := indexOfArg(linkArgs, "-Wl,--end-group")
	if sgStart < 0 || sgEnd < 0 || sgEnd <= sgStart {
		t.Fatalf("malformed start-group window [%d,%d]: %v", sgStart, sgEnd, argStrs2(linkArgs))
	}
	regular := linkArgs[sgStart+1 : sgEnd]

	const (
		firstA = "dep/first/libdep-first.a"
		enumA  = "tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"
		protoA = "proto_mod/libproto_mod.a"
	)

	count := func(want string) int {
		n := 0
		for _, a := range regular {
			if a.string() == want {
				n++
			}
		}
		return n
	}
	for _, p := range []string{firstA, enumA, protoA} {
		if c := count(p); c != 1 {
			t.Fatalf("archive %s appears %d times in regular start-group, want exactly 1: %v", p, c, argStrs2(regular))
		}
	}

	firstIdx := indexOfArg(regular, firstA)
	enumIdx := indexOfArg(regular, enumA)
	protoIdx := indexOfArg(regular, protoA)

	if !(firstIdx < enumIdx && enumIdx < protoIdx) {
		t.Fatalf("regular start-group archive order = first[%d] enum[%d] proto[%d]; want first < enum < proto: %v",
			firstIdx, enumIdx, protoIdx, argStrs2(regular))
	}
}

func argStrs2(args []STR) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.string()
	}
	return out
}
