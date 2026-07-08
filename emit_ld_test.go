package main

import (
	"slices"
	"strings"
	"testing"
)

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

const referenceLDOutput = "$(B)/tools/archiver/archiver"

func TestEmitLD_SyntheticPROGRAM(t *testing.T) {
	emit := newStreamingEmitter(nil)
	mainRef := emit.emitNode(Node{Platform: &Platform{},
		KV: &ldTestKV,
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
		false,
		0,
		unitSbomCpp,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes.s[ldRef]

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

	startIdx := slices.Index(anyStrs(got.Cmds[2].CmdArgs.flat()), "--start-plugins")
	endIdx := slices.Index(anyStrs(got.Cmds[2].CmdArgs.flat()), "--end-plugins")

	if startIdx < 0 || endIdx != startIdx+1 {
		t.Fatalf("synthetic LD plugin markers = %v, want adjacent empty --start-plugins/--end-plugins", got.Cmds[2].CmdArgs.flat())
	}

	if got.KV.P != pkLD || got.KV.PC != pcLightBlue || !got.KV.ShowOut {
		t.Errorf("kv = %+v, want {P:LD PC:light-blue ShowOut:true}", got.KV)
	}

	if len(got.DepRefs) != 2 {
		t.Errorf("DepRefs = %d, want 2", len(got.DepRefs))
	}
}

func TestEmitLD_SplitDwarfCommandsCarryDistbuildEnv(t *testing.T) {
	emit := newStreamingEmitter(nil)
	mainRef := emit.emitNode(Node{Platform: &Platform{},
		KV: &ldTestKV,
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
		true,
		0,
		unitSbomCpp,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes.s[ldRef]

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

	if !slices.Equal(anyStrs(got.Cmds[4].CmdArgs.flat()), []string{testToolchain().Objcopy.string(), "--only-keep-debug", "$(B)/some/prog/prog", "$(B)/some/prog/prog.debug"}) {
		t.Fatalf("cmd[4].cmd_args = %#v", got.Cmds[4].CmdArgs.flat())
	}

	if !slices.Equal(anyStrs(got.Cmds[5].CmdArgs.flat()), []string{testToolchain().Strip.string(), "--strip-debug", "$(B)/some/prog/prog"}) {
		t.Fatalf("cmd[5].cmd_args = %#v", got.Cmds[5].CmdArgs.flat())
	}

	if !slices.Equal(anyStrs(got.Cmds[6].CmdArgs.flat()), []string{testToolchain().Objcopy.string(), "--remove-section=.gnu_debuglink", "--add-gnu-debuglink", "$(B)/some/prog/prog.debug", "$(B)/some/prog/prog"}) {
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
	emit := newStreamingEmitter(nil)
	stub := emit.emitNode(Node{Platform: &Platform{}, KV: &ldTestKV})

	ref := emitLD(
		hostInstance("some/prog"),
		"prog",
		[]NodeRef{stub}, []VFS{build("some/prog/main.cpp.o")},
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
		false,
		0,
		unitSbomCpp,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes.s[ref]

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
		internAnys([]string{"-Wl,-rpath,$ORIGIN"}),
		internAnys([]string{"-Wl,-rpath,$ORIGIN"}),
		nil,
		nil,
		false,
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

	if !slices.Equal(anyStrs(got), want) {
		t.Fatalf("composeProgramLinkTrailer mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEmitLD_ThreadsWholeArchiveLibsToInputsAndDeps(t *testing.T) {
	emit := newStreamingEmitter(nil)
	mainRef := emit.emitNode(Node{Platform: &Platform{}, KV: &ldTestKV})

	wholeRef := emit.emitNode(Node{Platform: &Platform{}, KV: &ldTestKV})

	instance := targetInstance("some/prog")
	wholeArchivePath := "some/prog/libproto_cpp.a"

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{build("some/prog/main.cpp.o")},
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
		false,
		0,
		unitSbomCpp,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes.s[ldRef]

	if !slices.Contains(got.flatInputs(), build(wholeArchivePath)) {
		t.Fatalf("inputs do not contain whole-archive path %q: %#v", wholeArchivePath, got.flatInputs())
	}

	depCount := 0

	for _, r := range got.DepRefs {
		if r == wholeRef {
			depCount++
		}
	}

	if depCount != 1 {
		t.Fatalf("whole-archive/peer ref in DepRefs %d times, want 1: %#v", depCount, got.DepRefs)
	}

	cmdArgs := anyStrs(got.Cmds[2].CmdArgs.flat())
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
	emit := newStreamingEmitter(nil)
	mainRef := emit.emitNode(Node{Platform: &Platform{}, KV: &ldTestKV})

	peerRef := emit.emitNode(Node{Platform: &Platform{}, KV: &ldTestKV})

	instance := targetInstance("some/prog")
	dupPath := build("some/prog/libproto_cpp.a")

	ldRef := emitLD(
		instance,
		"prog",
		[]NodeRef{mainRef}, []VFS{build("some/prog/main.cpp.o")},
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
		false,
		0,
		unitSbomCpp,
		false,
		testToolchain(),
		testHostP,
		nil,
		emit,
		emitVCSNode(emit, testHostP),
	)

	got := emit.nodes.s[ldRef]
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

	cmdArgs := anyStrs(got.Cmds[2].CmdArgs.flat())
	found := false

	for i := 0; i+1 < len(cmdArgs); i++ {
		if cmdArgs[i] == "--whole-archive-libs" && cmdArgs[i+1] == dupPath.relString() {
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("cmd[2] missing whole-archive marker for %q: %#v", dupPath.relString(), cmdArgs)
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
			e := newStreamingEmitter(nil)
			instance := targetInstance("test/prog")

			exc := try(func() {
				emitLD(instance, "prog", tc.ccRefs, tc.ccPaths, tc.peerRefs, tc.peerPaths, nil, tc.pluginRefs, tc.pluginPaths, tc.globalRefs, tc.globalPaths, tc.wholeRefs, tc.wholePaths, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, false, false, false, false, false, 0, unitSbomCpp, false, testToolchain(), testHostP, nil, e, emitVCSNode(e, testHostP))
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

	if g.Result[0] != ld.Ref {
		t.Errorf("result Ref = %d, want LD ref %d", g.Result[0], ld.Ref)
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

func TestGen_FbsSrcsInduceFlatbuffersLinkDep(t *testing.T) {
	files := map[string]string{
		"prog/ya.make":  "PROGRAM()\nPEERDIR(arrowlike)\nSRCS(main.cpp)\nEND()\n",
		"prog/main.cpp": "int main() { return 0; }\n",

		"arrowlike/ya.make":                                          "LIBRARY()\nPEERDIR(peer1)\nSRCS(lib.cpp Schema.fbs)\nEND()\n",
		"arrowlike/lib.cpp":                                          "int f() { return 0; }\n",
		"arrowlike/Schema.fbs":                                       "namespace test; table Foo { value:int; }\n",
		"peer1/ya.make":                                              "LIBRARY()\nSRCS(p1.cpp)\nEND()\n",
		"peer1/p1.cpp":                                               "int p1() { return 0; }\n",
		"contrib/libs/flatbuffers/ya.make":                           "LIBRARY()\nSRCS(flatbuffers.cpp)\nEND()\n",
		"contrib/libs/flatbuffers/flatbuffers.cpp":                   "int fb() { return 0; }\n",
		"contrib/libs/flatbuffers/flatc/ya.make":                     "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n",
		"contrib/libs/flatbuffers/flatc/main.cpp":                    "int main() { return 0; }\n",
		"contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h": "#pragma once\n",
		"build/scripts/cpp_flatc_wrapper.py":                         "print('stub')\n",
	}

	g := testGen(newMemFS(files), "prog")

	ldNode := resultRootNode(g)

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

	if peer1Idx > fbIdx {
		t.Errorf("peer1 [%d] appears after flatbuffers [%d] in link args; want peer1 before flatbuffers", peer1Idx, fbIdx)
	}

	if fbIdx > arrowlikeIdx {
		t.Errorf("flatbuffers [%d] appears after arrowlike [%d] in link args; want flatbuffers before the owning library", fbIdx, arrowlikeIdx)
	}
}

func TestGen_EnumSerializationRuntimePrecedesProtoLibraryArchive(t *testing.T) {
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

		"library/cpp/malloc/jemalloc/ya.make": "LIBRARY()\nSRCS(je.cpp)\nEND()\n",
		"library/cpp/malloc/jemalloc/je.cpp":  "int je(){return 0;}\n",
		"library/cpp/malloc/api/ya.make":      "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/api/api.cpp":      "int api(){return 0;}\n",
		"contrib/libs/jemalloc/ya.make":       "LIBRARY()\nSRCS(c.cpp)\nEND()\n",
		"contrib/libs/jemalloc/c.cpp":         "int c(){return 0;}\n",
		"build/cow/on/ya.make":                "LIBRARY()\nSRCS(cow.cpp)\nEND()\n",
		"build/cow/on/cow.cpp":                "int cow(){return 0;}\n",

		"contrib/libs/python/ya.make":                       "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/runtime_py3/main/ya.make":           "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/import_tracing/constructor/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/testing/import_test/ya.make":        "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",

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

	var linkArgs []ANY

	for _, c := range resultRootNode(g).Cmds {
		args := c.CmdArgs.flat()

		if indexOfArg(args, "-Wl,--start-group") >= 0 {
			linkArgs = args

			break
		}
	}

	if linkArgs == nil {
		t.Fatal("no program-link LD node with -Wl,--start-group found in graph")
	}

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

func argStrs2[T interface {
	~uint32
	string() string
}](args []T) []string {
	out := make([]string, len(args))

	for i, a := range args {
		out[i] = a.string()
	}

	return out
}

func libmProgramFiles(enable bool) map[string]string {
	files := map[string]string{}

	enableStmt := ""

	if enable {
		enableStmt = "ENABLE(USE_ARCADIA_LIBM)\n"
	}

	writeTestModuleFile(files, "app/ya.make", "PROGRAM(app)\n"+enableStmt+"SRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/libm/ya.make",
		"LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nADDINCL(GLOBAL contrib/libs/libm/include\nGLOBAL contrib/libs/libm/platform)\nSRCS(e_exp.c)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/libm/e_exp.c", "double e_exp(double x){return x;}\n")

	writeTestModuleFile(files, "contrib/libs/libm/include/math.h", "#pragma once\n")
	writeTestModuleFile(files, "contrib/libs/libm/platform/platform.h", "#pragma once\n")

	return files
}

func libmOrderingProgramFiles() map[string]string {
	files := libmProgramFiles(true)

	writeTestModuleFile(files, "util/ya.make", "LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(library/early)\nEND()\n")
	writeTestModuleFile(files, "util/u.cpp", "int u(){return 0;}\n")

	writeTestModuleFile(files, "library/early/ya.make",
		"LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nADDINCL(GLOBAL library/early/include)\nSRCS(early.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/early/early.cpp", "int early(){return 0;}\n")
	writeTestModuleFile(files, "library/early/include/early.h", "#pragma once\n")

	return files
}

func ccArgsOfSuffix(t *testing.T, g *Graph, suffix string) []ANY {
	t.Helper()

	n := mustNodeByOutputSuffix(t, g, suffix)

	if len(n.Cmds) == 0 {
		t.Fatalf("CC node %q has no Cmds", suffix)
	}

	return n.Cmds[0].CmdArgs.flat()
}

func linkArgsOf(t *testing.T, g *Graph) []ANY {
	t.Helper()

	for _, n := range g.Graph {
		if n.KV.P != pkLD {
			continue
		}

		for _, c := range n.Cmds {
			flat := c.CmdArgs.flat()

			if indexOfArg(flat, "$(S)/build/scripts/link_exe.py") >= 0 {
				return flat
			}
		}
	}

	t.Fatal("no link_exe.py command found on any LD node")

	return nil
}

func TestGen_UseArcadiaLibm_PeersLibmArchive(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(true)), "app")

	mustNodeByOutput(t, g, "$(B)/contrib/libs/libm/libcontrib-libs-libm.a")

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	linkArgs := linkArgsOf(t, g)

	if indexOfArg(linkArgs, libmLinkArg) < 0 {
		t.Fatalf("program link closure missing %s; link args = %v", libmLinkArg, linkArgs)
	}
}

func TestGen_UseArcadiaLibm_AbsentWithoutEnable(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(false)), "app")

	if n := nodeByOutput(g, "$(B)/contrib/libs/libm/libcontrib-libs-libm.a"); n != nil {
		t.Fatalf("libm archive must not be reachable without ENABLE(USE_ARCADIA_LIBM)")
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	linkArgs := linkArgsOf(t, g)

	if indexOfArg(linkArgs, libmLinkArg) >= 0 {
		t.Fatalf("link closure must not contain %s without the enable", libmLinkArg)
	}
}

func TestGen_UseArcadiaLibm_NoSelfPeer(t *testing.T) {
	mi := ModuleInstance{
		Path:     source("contrib/libs/libm"),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := (&EmitContext{instance: mi, d: &ModuleData{useArcadiaLibm: true}}).defaultProgramPeerdirsForWithState(false)

	for _, p := range got {
		if p == "contrib/libs/libm" {
			t.Fatalf("contrib/libs/libm must not peer itself; got %v", got)
		}
	}
}

func TestGen_UseArcadiaLibm_AddInclOrderAfterTransitive(t *testing.T) {
	g := testGen(newMemFS(libmOrderingProgramFiles()), "app")

	args := ccArgsOfSuffix(t, g, "app/main.cpp.o")

	earlyIdx := indexOfArg(args, "-I$(S)/library/early/include")
	libmIdx := indexOfArg(args, "-I$(S)/contrib/libs/libm/include")

	if earlyIdx < 0 {
		t.Fatalf("program compile missing the language-default transitive addincl; args = %v", args)
	}

	if libmIdx < 0 {
		t.Fatalf("program compile missing the libm addincl; args = %v", args)
	}

	if earlyIdx > libmIdx {
		t.Fatalf("libm addincl (%d) must come after the language-default transitive addincl (%d); args = %v", libmIdx, earlyIdx, args)
	}
}

func TestGen_UseArcadiaLibm_NoSystemLm(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(true)), "app")

	linkArgs := linkArgsOf(t, g)

	if indexOfArg(linkArgs, "-lm") >= 0 {
		t.Fatalf("USE_ARCADIA_LIBM=yes link must not emit system -lm; link args = %v", linkArgs)
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"

	if indexOfArg(linkArgs, libmLinkArg) < 0 {
		t.Fatalf("USE_ARCADIA_LIBM=yes link must contain %s; link args = %v", libmLinkArg, linkArgs)
	}
}

func TestGen_UseArcadiaLibm_KeepsSystemLmWithoutEnable(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(false)), "app")

	linkArgs := linkArgsOf(t, g)

	if indexOfArg(linkArgs, "-lm") < 0 {
		t.Fatalf("default USE_ARCADIA_LIBM=no link must keep system -lm; link args = %v", linkArgs)
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"

	if indexOfArg(linkArgs, libmLinkArg) >= 0 {
		t.Fatalf("default link must not gain the Arcadia libm archive; link args = %v", linkArgs)
	}
}

var (
	ldTestKV = KV{P: pkSTUB}
)
