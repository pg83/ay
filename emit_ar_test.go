package main

import (
	"reflect"
	"strings"
	"testing"
)

var (
	testHostP   = newTestPlatform(OSLinux, ISAX8664, "yes")
	testTargetP = newTestPlatform(OSLinux, ISAAArch64, "no")
)

var testToolchainFlags = map[string]string{
	"OPENSOURCE":              "yes",
	"BUILD_PYTHON_BIN":        "/bin/python3",
	"BUILD_PYTHON3_BIN":       "/bin/python3",
	"CLANG_TOOL":              "/bin/clang",
	"CLANG_pl_pl_TOOL":        "/bin/clang++",
	"OBJCOPY_TOOL":            "/bin/llvm-objcopy",
	"AR_TOOL":                 "/bin/llvm-ar",
	"STRIP_TOOL":              "/bin/llvm-strip",
	"LLD_TOOL":                "/bin/lld",
	"CLANG16_RESOURCE_GLOBAL": "CLANG16_RESOURCE_GLOBAL::$(CLANG16)",
}

func newTestPlatform(os OS, isa ISA, pic string) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+2)

	for k, v := range testToolchainFlags {
		flags[k] = v
	}

	flags["PIC"] = pic

	return newPlatform(newMemFS(nil), os, isa, flags, "", "")
}

func targetInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     source(path),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}
}

func hostInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     source(path),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
}

func vfsStrings(vs []VFS) []string {
	out := make([]string, len(vs))

	for i, v := range vs {
		out[i] = v.string()
	}

	return out
}

func TestEmitAR_LengthMismatchPanics(t *testing.T) {
	e := newStreamingEmitter(nil)

	objRefs := []NodeRef{e.emitNode(Node{
		Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"cc"}))}, Env: nil}},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{})},
		KV:           &arTestKV,
		Outputs:      ToVFSSlice([]string{"$(B)/build/cow/on/lib.c.o"}),
		Platform:     &Platform{Target: "default-linux-aarch64"},
		Requirements: Requirements{},
	})}
	objPaths := []VFS{build("o1.o"), build("o2.o")}

	exc := try(func() {
		EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	})

	if exc == nil {
		t.Fatal("expected exception for length mismatch")
	}

	if !strings.Contains(exc.Error(), "length mismatch") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestEmitAR_PeerArchives_NotInCmdArgs(t *testing.T) {
	e := newStreamingEmitter(nil)

	makeLeaf := func(out VFS) NodeRef {
		return e.emitNode(Node{
			Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"cc"}))}, Env: nil}},
			Env:          nil,
			Inputs:       InputChunks{ToVFSSlice([]string{})},
			KV:           &arTestKV,
			Outputs:      []VFS{out},
			Platform:     &Platform{Target: "default-linux-aarch64"},
			Requirements: Requirements{},
		})
	}

	o1 := build("build/cow/on/a.c.o")
	o2 := build("build/cow/on/b.c.o")
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []VFS{o1, o2}

	peer1 := makeLeaf(build("some/peer/libsome-peer.a"))
	peer2 := makeLeaf(build("other/peer/libother-peer.a"))
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, testHostP, e)
	got := e.nodes.s[arRef]

	cmdArgs := got.Cmds[0].CmdArgs.flat()
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
			if arg.string() == pp {
				t.Errorf("peer archive path %q unexpectedly present in cmd_args", pp)
			}
		}
	}
}

func TestEmitAR_PeerArchives_InDepRefs(t *testing.T) {
	e := newStreamingEmitter(nil)

	makeLeaf := func(out VFS) NodeRef {
		return e.emitNode(Node{
			Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"cc"}))}, Env: nil}},
			Env:          nil,
			Inputs:       InputChunks{ToVFSSlice([]string{})},
			KV:           &arTestKV,
			Outputs:      []VFS{out},
			Platform:     &Platform{Target: "default-linux-aarch64"},
			Requirements: Requirements{},
		})
	}

	o1 := build("build/cow/on/a.c.o")
	o2 := build("build/cow/on/b.c.o")
	objRefs := []NodeRef{makeLeaf(o1), makeLeaf(o2)}
	objPaths := []VFS{o1, o2}

	peer1 := makeLeaf(build("some/peer/libsome-peer.a"))
	peer2 := makeLeaf(build("other/peer/libother-peer.a"))
	peerArchiveRefs := []NodeRef{peer1, peer2}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, peerArchiveRefs, testHostP, e)
	got := e.nodes.s[arRef]

	wantDepRefs := len(objRefs) + len(peerArchiveRefs)

	if len(got.DepRefs) != wantDepRefs {
		t.Errorf("DepRefs len = %d, want %d (objRefs=%d + peerArchiveRefs=%d)",
			len(got.DepRefs), wantDepRefs, len(objRefs), len(peerArchiveRefs))
	}
}

func TestEmitAR_InputsLeadWithObjPaths(t *testing.T) {
	e := newStreamingEmitter(nil)

	makeLeaf := func(out VFS) NodeRef {
		return e.emitNode(Node{
			Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"cc"}))}, Env: nil}},
			Env:          nil,
			Inputs:       InputChunks{ToVFSSlice([]string{})},
			KV:           &arTestKV,
			Outputs:      []VFS{out},
			Platform:     &Platform{Target: "default-linux-aarch64"},
			Requirements: Requirements{},
		})
	}

	z := build("build/cow/on/z.c.o")
	m := build("build/cow/on/m.c.o")
	a := build("build/cow/on/a.c.o")
	objPaths := []VFS{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	got := e.nodes.s[arRef]

	inputs := got.flatInputs()

	if len(inputs) != 4 {
		t.Fatalf("inputs len = %d, want 4", len(inputs))
	}

	inputObjs := vfsStrings(inputs[:3])

	wantInputObjs := []string{z.string(), m.string(), a.string()}

	if !reflect.DeepEqual(inputObjs, wantInputObjs) {
		t.Errorf("inputs .o mismatch:\n  want %v\n  got  %v", wantInputObjs, inputObjs)
	}
}

func TestGen_ArchiveMembersFollowPicContour(t *testing.T) {
	src := map[string]string{
		"codecs/ya.make": `LIBRARY()
SRCS(bitset.cpp bytes.cpp)
END()
`,
	}

	arMembers := func(g *Graph) []string {
		var members []string

		for _, n := range g.Graph {
			if n.KV.P != pkAR {
				continue
			}

			for _, a := range anyStrs(n.Cmds[0].CmdArgs.flat()) {
				if strings.HasSuffix(a, ".o") {
					members = append(members, a)
				}
			}
		}

		return members
	}

	host := newTestPlatform(OSLinux, ISAX8664, "yes")

	picTarget := newPlatform(newMemFS(src), OSLinux, ISAAArch64, func() map[string]string {
		f := make(map[string]string, len(testToolchainFlags)+1)

		for k, v := range testToolchainFlags {
			f[k] = v
		}

		f["PIC"] = "yes"

		return f
	}(), "", "")

	picMembers := arMembers(Gen(newMemFS(src), "codecs", host, picTarget, func(Warn) {}))

	if len(picMembers) == 0 {
		t.Fatal("PIC build produced no archive members")
	}

	for _, m := range picMembers {
		if !strings.HasSuffix(m, ".pic.o") {
			t.Fatalf("PIC contour: archive member %q is not a .pic.o variant", m)
		}
	}

	nonPicMembers := arMembers(testGen(newMemFS(src), "codecs"))

	if len(nonPicMembers) == 0 {
		t.Fatal("non-PIC build produced no archive members")
	}

	for _, m := range nonPicMembers {
		if strings.HasSuffix(m, ".pic.o") {
			t.Fatalf("non-PIC contour: archive member %q is a .pic.o variant", m)
		}
	}
}

func TestEmitAR_CmdArgsPreservesDeclarationOrder(t *testing.T) {
	e := newStreamingEmitter(nil)

	makeLeaf := func(out VFS) NodeRef {
		return e.emitNode(Node{
			Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"cc"}))}, Env: nil}},
			Env:          nil,
			Inputs:       InputChunks{ToVFSSlice([]string{})},
			KV:           &arTestKV,
			Outputs:      []VFS{out},
			Platform:     &Platform{Target: "default-linux-aarch64"},
			Requirements: Requirements{},
		})
	}

	z := build("build/cow/on/z.c.o")
	m := build("build/cow/on/m.c.o")
	a := build("build/cow/on/a.c.o")
	objPaths := []VFS{z, m, a}
	objRefs := []NodeRef{makeLeaf(z), makeLeaf(m), makeLeaf(a)}

	arRef := EmitAR(targetInstance("build/cow/on"), objRefs, objPaths, nil, testHostP, e)
	got := e.nodes.s[arRef]

	cmdArgs := got.Cmds[0].CmdArgs.flat()

	if len(cmdArgs) != 13 {
		t.Fatalf("cmd_args len = %d, want 13", len(cmdArgs))
	}

	trailing := anyStrs(cmdArgs[10:])
	wantTrailing := []string{z.string(), m.string(), a.string()}

	if !reflect.DeepEqual(trailing, wantTrailing) {
		t.Errorf("cmd_args .o order mismatch:\n  want %v\n  got  %v", wantTrailing, trailing)
	}
}

func TestGen_GlobalSrcs_EmitsTwoARs(t *testing.T) {
	fs := newMemFS(map[string]string{
		"globalmod/ya.make": `LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`,
	})

	g := testGen(fs, "globalmod")

	counts := make(map[string]int)

	for _, n := range g.Graph {
		p := n.KV.P.string()
		counts[p]++
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (regular + global)", counts["CC"])
	}

	if counts["AR"] != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global)", counts["AR"])
	}

	var globalARs, regularARs int

	for _, n := range g.Graph {
		if n.KV.P != pkAR {
			continue
		}

		if len(n.Outputs) == 0 {
			continue
		}

		if strings.HasSuffix(n.Outputs[0].string(), ".global.a") {
			globalARs++
		} else if strings.HasSuffix(n.Outputs[0].string(), ".a") {
			regularARs++
		}
	}

	if globalARs != 1 {
		t.Errorf("global ARs = %d, want 1", globalARs)
	}

	if regularARs != 1 {
		t.Errorf("regular (no-tag) ARs = %d, want 1", regularARs)
	}
}

func TestEmitAR_NoPeerArchivesInDeps(t *testing.T) {
	fs := newMemFS(map[string]string{
		"lib_consumer/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(lib_peer)
SRCS(c.cpp)
END()
`,
		"lib_peer/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(p.cpp)
END()
`,
	})

	g := testGen(fs, "lib_consumer")

	var consumerAR *Node

	for _, n := range g.Graph {
		if n.KV.P == pkAR && len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].relString(), "lib_consumer/") {
			consumerAR = n

			break
		}
	}

	if consumerAR == nil {
		t.Fatal("lib_consumer AR not found")
	}

	for _, dep := range graphDeps(g, consumerAR) {
		for _, n := range g.Graph {
			if n.Ref == dep && n.KV.P == pkAR {
				t.Errorf("lib_consumer AR has AR-typed dep (peer outputs=%v); reference invariant: zero AR-on-AR deps", n.Outputs)
			}
		}
	}
}

func TestGen_PR35y_R7_JoinSrcs_SuppressBuildRootShim(t *testing.T) {
	fs := newMemFS(map[string]string{
		"joinmod/ya.make": `LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
END()
`,
	})

	g := testGen(fs, "joinmod")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV.P == pkAR {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no AR node emitted")
	}

	const forbidden = "$(B)/joinmod/all_my.cpp"

	for _, in := range arNode.flatInputs() {
		if in.string() == forbidden {
			t.Errorf("AR.flatInputs() contains %q — JS-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	for _, src := range []string{"$(S)/joinmod/src1.cpp", "$(S)/joinmod/src2.cpp"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.flatInputs() must not contain JS member source %q: %#v", src, arNode.flatInputs())
		}
	}
}

func TestGen_PR35y_R7_RagelRl6_OriginalSourcePair(t *testing.T) {
	fs := newMemFS(map[string]string{
		"consumer/ya.make":             "LIBRARY()\nSRCS(parser.rl6)\nEND()\n",
		"consumer/parser.rl6":          "// fixture\n",
		"consumer/parser.h":            "// fixture\n",
		"contrib/tools/ragel6/ya.make": "PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n",
	})

	g := testGen(fs, "consumer")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV.P == pkAR && len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].relString(), "consumer/") {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no consumer AR node emitted")
	}

	const forbidden = "$(B)/consumer/_/parser.rl6.cpp"

	for _, in := range arNode.flatInputs() {
		if in.string() == forbidden {
			t.Errorf("AR.flatInputs() contains %q — R6-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	for _, src := range []string{"$(S)/consumer/parser.rl6", "$(S)/consumer/parser.h"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.flatInputs() must not contain member source %q: %#v", src, arNode.flatInputs())
		}
	}
}

func TestGen_PR35y_R8_RegularARIncludesGlobalMemberInputs(t *testing.T) {
	fs := newMemFS(map[string]string{
		"globalmod/ya.make": `LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`,
	})

	g := testGen(fs, "globalmod")

	var (
		regularAR *Node
		globalAR  *Node
	)

	for _, n := range g.Graph {
		if n.KV.P != pkAR || len(n.Outputs) == 0 {
			continue
		}

		if strings.HasSuffix(n.Outputs[0].string(), ".global.a") {
			globalAR = n
		} else {
			regularAR = n
		}
	}

	if regularAR == nil || globalAR == nil {
		t.Fatalf("expected both regular and global AR (got regular=%v, global=%v)", regularAR != nil, globalAR != nil)
	}

	regularInputs := map[string]bool{}

	for _, in := range regularAR.flatInputs() {
		regularInputs[in.string()] = true
	}

	globalInputs := map[string]bool{}

	for _, in := range globalAR.flatInputs() {
		globalInputs[in.string()] = true
	}

	const (
		regularSrc = "$(S)/globalmod/regular.cpp"
		globalSrc  = "$(S)/globalmod/global.cpp"
	)

	for _, src := range []string{regularSrc, globalSrc} {
		if regularInputs[src] {
			t.Errorf("regular AR.flatInputs() must not contain member source %q: %#v", src, regularAR.flatInputs())
		}
	}

	if globalInputs[globalSrc] {
		t.Errorf(".global.a AR.flatInputs() must not contain member source %q: %#v", globalSrc, globalAR.flatInputs())
	}

	hasObject := func(n *Node) bool {
		for _, in := range n.flatInputs() {
			if strings.HasSuffix(in.relString(), ".o") {
				return true
			}
		}

		return false
	}

	if !hasObject(regularAR) {
		t.Errorf("regular AR.flatInputs() has no object: %#v", regularAR.flatInputs())
	}

	if !hasObject(globalAR) {
		t.Errorf(".global.a AR.flatInputs() has no object: %#v", globalAR.flatInputs())
	}
}

func TestGen_YqlUdfStatic_UsesGlobalArchiveOnly(t *testing.T) {
	files := map[string]string{}

	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("udfmod/ya.make", `YQL_UDF_CONTRIB(my_udf)
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`)
	mkdirWrite("udfmod/lib.cpp", "int udf() { return 0; }\n")
	mkdirWrite("yql/essentials/public/udf/ya.make", "LIBRARY()\nEND()\n")
	mkdirWrite("yql/essentials/public/udf/support/ya.make", "LIBRARY()\nEND()\n")

	g := testGen(newMemFS(files), "udfmod")

	cc := findGraphNodeByOutputs(t, g, "$(B)/udfmod/lib.cpp.udfs.o")

	for _, want := range []string{
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	} {
		if !contains(cc.Cmds[0].CmdArgs.flat(), want) {
			t.Fatalf("cc cmd_args missing %q: %v", want, cc.Cmds[0].CmdArgs.flat())
		}
	}

	findGraphNodeByOutputs(t, g, "$(B)/udfmod/libmy_udf.global.a")

	for _, n := range g.Graph {
		for _, out := range n.Outputs {
			if out.string() == "$(B)/udfmod/libmy_udf.a" {
				t.Fatalf("unexpected regular archive output %q present in graph", out)
			}
		}
	}
}

func TestReorderARMembers_Reg3PICVariantsTrailObjcopy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paths     []string
		wantOrder []int
	}{
		{
			name: "protobuf-style host reg3",
			paths: []string{
				"contrib/python/protobuf/py3/google.protobuf.internal._api_implementation.reg3.cpp.o",
				"contrib/python/protobuf/py3/google.protobuf.pyext._message.reg3.cpp.o",
				"contrib/python/protobuf/py3/objcopy_a.o",
				"contrib/python/protobuf/py3/objcopy_b.o",
			},
			wantOrder: []int{2, 3, 0, 1},
		},
		{
			name: "symbols/module-style host py3 reg3",
			paths: []string{
				"library/python/symbols/module/library.python.symbols.module.syms.reg3.cpp.py3.pic.o",
				"library/python/symbols/module/objcopy_a.o",
				"library/python/symbols/module/objcopy_b.o",
			},
			wantOrder: []int{1, 2, 0},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			refs := make([]NodeRef, len(tc.paths))
			paths := make([]VFS, len(tc.paths))
			metas := make([]SrcMeta, len(tc.paths))

			for i, rel := range tc.paths {
				refs[i] = NodeRef(int64(i + 1))
				paths[i] = build(rel)
				metas[i] = SrcMeta{Prio: stmtPrioDefault}

				if strings.Contains(rel, ".reg3.cpp") {
					metas[i] = SrcMeta{Prio: stmtPrioDefault, Generated: true}
				}
			}

			gotRefs, gotPaths := reorderARMembers(refs, paths, metas)

			wantRefs := make([]NodeRef, len(tc.wantOrder))
			wantPaths := make([]string, len(tc.wantOrder))

			for i, idx := range tc.wantOrder {
				wantRefs[i] = refs[idx]
				wantPaths[i] = build(tc.paths[idx]).string()
			}

			gotPathStrings := make([]string, len(gotPaths))

			for i, path := range gotPaths {
				gotPathStrings[i] = path.string()
			}

			if !reflect.DeepEqual(gotPathStrings, wantPaths) {
				t.Fatalf("paths mismatch:\n got: %v\nwant: %v", gotPathStrings, wantPaths)
			}

			if !reflect.DeepEqual(gotRefs, wantRefs) {
				t.Fatalf("refs mismatch:\n got: %v\nwant: %v", gotRefs, wantRefs)
			}
		})
	}
}

func TestGen_LibraryARIncludesResourceObjcopyMemberInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "db/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nRESOURCE(data.sql key)\nEND()\n")
	writeTestModuleFile(files, "db/main.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(files, "db/data.sql", "select 1;\n")

	g := testGen(newMemFS(files), "db")
	regularAR := mustNodeByOutput(t, g, "$(B)/db/libdb.a")
	mustNodeByOutput(t, g, "$(B)/db/libdb.global.a")

	if findNodeByOutputPrefix(g, "$(B)/db/objcopy_") == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	if !nodeHasInput(regularAR, "$(S)/build/scripts/link_lib.py") {
		t.Fatalf("libdb.a inputs missing its own script link_lib.py: %#v", regularAR.flatInputs())
	}

	for _, absent := range []string{"$(S)/db/data.sql", "$(S)/build/scripts/objcopy.py"} {
		if nodeHasInput(regularAR, absent) {
			t.Errorf("libdb.a must not list %q (not an AR input): %#v", absent, regularAR.flatInputs())
		}
	}
}

func TestGen_Archive_SrcdirMemberResolvesThroughSourcePathPlan(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(owner.cpp)
SRCDIR(data)
ARCHIVE(
    NAME out.inc
    member.txt
)
END()
`)
	writeTestModuleFile(files, "mod/owner.cpp", "int owner(){return 0;}\n")
	writeTestModuleFile(files, "data/member.txt", "payload\n")
	writeToolProgram(files, "tools/archiver", "archiver")

	g := testGen(newMemFS(files), "mod")

	ar := mustNodeByOutput(t, g, "$(B)/mod/out.inc")

	const wantMember = "$(S)/data/member.txt"
	const phantomMember = "$(S)/mod/member.txt"

	args := anyStrs(ar.Cmds[0].CmdArgs.flat())

	if !containsString(args, wantMember+":") {
		t.Errorf("ARCHIVE cmd_args missing SRCDIR-resolved member %q: %v", wantMember+":", args)
	}

	if containsString(args, phantomMember+":") {
		t.Errorf("ARCHIVE cmd_args must not list phantom module-dir member %q: %v", phantomMember+":", args)
	}

	if !nodeHasInput(ar, wantMember) {
		t.Errorf("ARCHIVE inputs missing SRCDIR-resolved member %q: %#v", wantMember, ar.flatInputs())
	}

	if nodeHasInput(ar, phantomMember) {
		t.Errorf("ARCHIVE inputs must not list phantom module-dir member %q: %#v", phantomMember, ar.flatInputs())
	}
}

func TestGen_ProtoLibrary_NamedArgUsedForArchive(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY(api-protos)
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(files, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "ydb/public/api/protos")

	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libapi-protos.a")

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o.string() == "$(B)/ydb/public/api/protos/libprotos.a" {
				t.Fatalf("path-derived archive libprotos.a should not exist; got it with named arg")
			}
		}
	}
}

func TestGen_ProtoLibrary_UnnamedArgKeepsPathDerivedArchive(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY()
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(files, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "ydb/public/api/protos")

	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libpublic-api-protos.a")
}

func TestReorderARMembers_SecondLevelTrailsFirstLevelByRound(t *testing.T) {
	fbsCpp := build("mod/formula.fbs.cpp.o")
	cpp := build("mod/formula.cpp.o")
	refs := []NodeRef{NodeRef(1), NodeRef(2)}
	paths := []VFS{fbsCpp, cpp}
	metas := []SrcMeta{
		{Prio: stmtPrioDefault, Seq: 1, Generated: true, SecondLevel: true},
		{Prio: stmtPrioDefault, Seq: 2, Generated: true},
	}

	_, gotPaths := reorderARMembers(refs, paths, metas)

	got := []string{gotPaths[0].string(), gotPaths[1].string()}
	want := []string{cpp.string(), fbsCpp.string()}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second-level ordering: got %v, want first-level .cpp.o before second-level .fbs.cpp.o %v", got, want)
	}
}

func TestGen_ARMemberOrder_PbCcAfterHSerialized(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mylib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    plain.cpp
    data.proto
)
GENERATE_ENUM_SERIALIZATION(flags.h)
END()
`)
	writeTestModuleFile(files, "mylib/plain.cpp", "int plain(){return 0;}\n")
	writeTestModuleFile(files, "mylib/data.proto", "syntax = \"proto3\";\npackage test;\nmessage Data {}\n")
	writeTestModuleFile(files, "mylib/flags.h", "enum class Flag { A = 0 };\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "mylib")

	ar := mustNodeByOutput(t, g, "$(B)/mylib/libmylib.a")

	pbPos := -1
	hSerPos := -1

	for i, arg := range anyStrs(ar.Cmds[0].CmdArgs.flat()) {
		if strings.HasSuffix(arg, ".pb.cc.o") {
			pbPos = i
		}

		if strings.HasSuffix(arg, ".h_serialized.cpp.o") {
			hSerPos = i
		}
	}

	if pbPos < 0 {
		t.Fatal("AR cmd_args missing .pb.cc.o")
	}

	if hSerPos < 0 {
		t.Fatal("AR cmd_args missing .h_serialized.cpp.o")
	}

	if hSerPos > pbPos {
		t.Errorf("AR ordering wrong: .h_serialized.cpp.o at pos %d, .pb.cc.o at pos %d — want h_serialized BEFORE pb.cc", hSerPos, pbPos)
	}
}

func TestGen_ARMemberOrder_CfgProtoAfterDirectCpp(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
SRCS(status_codes.cfgproto status_codes.cpp)
END()
`)
	writeTestModuleFile(files, "lib/status_codes.cpp", "int sc(){return 0;}\n")
	writeTestModuleFile(files, "lib/status_codes.cfgproto", `package NTest;

import "library/cpp/proto_config/protos/extensions.proto";

message Cfg {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/proto_config/plugin", "plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "library/cpp/proto_config/protos/ya.make", "LIBRARY()\nSRCS(extensions.proto)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/protos/extensions.proto", "syntax = \"proto2\";\nimport \"google/protobuf/descriptor.proto\";\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/descriptor.proto", "syntax = \"proto2\";\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/ya.make", "LIBRARY()\nSRCS(parse_value.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/parse_value.cpp", "int parse(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib")

	ar := mustNodeByOutput(t, g, "$(B)/lib/liblib.a")

	cppPos, cfgPos := -1, -1

	for i, arg := range anyStrs(ar.Cmds[0].CmdArgs.flat()) {
		if strings.HasSuffix(arg, "/status_codes.cpp.o") {
			cppPos = i
		}

		if strings.HasSuffix(arg, "/status_codes.cfgproto.pb.cc.o") {
			cfgPos = i
		}
	}

	if cppPos < 0 {
		t.Fatal("AR cmd_args missing status_codes.cpp.o")
	}

	if cfgPos < 0 {
		t.Fatal("AR cmd_args missing status_codes.cfgproto.pb.cc.o")
	}

	if cppPos > cfgPos {
		t.Errorf("AR ordering wrong: status_codes.cpp.o at pos %d, status_codes.cfgproto.pb.cc.o at pos %d — want direct .cpp.o BEFORE generated .cfgproto.pb.cc.o", cppPos, cfgPos)
	}
}

func TestGen_GlobalAR_ObjcopyBeforeGlobalSrcs(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")

	writeTestModuleFile(files, "brkmod/ya.make", "LIBRARY()\nGLOBAL_SRCS(global.cpp)\nRESOURCE(data.txt somekey)\nEND()\n")
	writeTestModuleFile(files, "brkmod/global.cpp", "int global(){return 0;}\n")
	writeTestModuleFile(files, "brkmod/data.txt", "some data\n")

	g := testGen(newMemFS(files), "brkmod")

	var globalAR *Node

	for _, n := range g.Graph {
		if n.KV.P == pkAR && len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), ".global.a") {
			globalAR = n

			break
		}
	}

	if globalAR == nil {
		t.Fatal("no global AR node in graph")
	}

	args := globalAR.Cmds[0].CmdArgs.flat()

	if len(args) < 12 {
		t.Fatalf("global AR cmd_args too short (%d): %v", len(args), args)
	}

	members := args[10:]

	objcopyIdx, globalCppIdx := -1, -1

	for i, m := range anyStrs(members) {
		if strings.Contains(m, "/objcopy_") && strings.HasSuffix(m, ".o") {
			objcopyIdx = i
		}

		if strings.HasSuffix(m, "/global.cpp.o") {
			globalCppIdx = i
		}
	}

	if objcopyIdx < 0 {
		t.Fatalf("global AR cmd_args missing objcopy member: %v", members)
	}

	if globalCppIdx < 0 {
		t.Fatalf("global AR cmd_args missing global.cpp.o: %v", members)
	}

	if objcopyIdx >= globalCppIdx {
		t.Errorf("objcopy (pos %d) must precede global.cpp.o (pos %d) in global AR cmd_args; members=%v",
			objcopyIdx, globalCppIdx, members)
	}
}

func TestGen_Archive_NameOutputDirPropagatesAsBuildInclude(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "peer/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(owner.cpp)
ARCHIVE(
    NAME data.inc
    file.txt
)
END()
`)
	writeTestModuleFile(files, "peer/owner.cpp", "int owner(){return 0;}\n")
	writeTestModuleFile(files, "peer/file.txt", "payload\n")
	writeTestModuleFile(files, "consumer/ya.make", `LIBRARY()
NO_RUNTIME()
PEERDIR(peer)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "consumer/use.cpp", "int use(){return 0;}\n")
	writeToolProgram(files, "tools/archiver", "archiver")

	g := testGen(newMemFS(files), "consumer")

	mustNodeByOutput(t, g, "$(B)/peer/data.inc")

	cc := mustNodeByOutput(t, g, "$(B)/consumer/use.cpp.o")
	args := anyStrs(cc.Cmds[0].CmdArgs.flat())

	if !flagsContain(args, "-I$(B)/peer") {
		t.Errorf("consumer use.cpp.o missing ARCHIVE-generated build include -I$(B)/peer: %v", args)
	}
}

func globalARMembers(t *testing.T, g *Graph) []string {
	t.Helper()

	var globalAR *Node

	for _, n := range g.Graph {
		if n.KV.P == pkAR && len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), ".global.a") {
			globalAR = n

			break
		}
	}

	if globalAR == nil {
		t.Fatal("no global AR node in graph")
	}

	args := anyStrs(globalAR.Cmds[0].CmdArgs.flat())

	if len(args) < 11 {
		t.Fatalf("global AR cmd_args too short (%d): %v", len(args), args)
	}

	return args[10:]
}

func classifyObjcopyMember(g *Graph, memberPath string) string {
	n := nodeByOutput(g, memberPath)

	if n == nil {
		return ""
	}

	has := func(sub string) bool {
		for _, in := range n.flatInputs() {
			if strings.Contains(in.string(), sub) {
				return true
			}
		}

		return false
	}

	switch {
	case has("/data.txt"):
		return "explicit"
	case has("/iface.pyi"):
		return "pyi"
	case has("/helper.py"):
		return "py"
	}

	return "other-objcopy"
}

func TestGen_GlobalAR_ExplicitResourceBeforeGlobal_PySrcAfter(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
SRCS(GLOBAL syms.cpp)
PY_REGISTER(mypkg.mymod)
PY_SRCS(
    TOP_LEVEL
    helper.py
    iface.pyi
)
RESOURCE_FILES(
    PREFIX res/
    data.txt
)
END()
`)
	writeTestModuleFile(files, "mod/syms.cpp", "int syms(){return 0;}\n")
	writeTestModuleFile(files, "mod/helper.py", "x = 1\n")
	writeTestModuleFile(files, "mod/iface.pyi", "x: int\n")
	writeTestModuleFile(files, "mod/data.txt", "payload\n")

	g := testGen(newMemFS(files), "mod")

	members := globalARMembers(t, g)

	explicitIdx, globalIdx, pyIdx, pyiIdx, regIdx := -1, -1, -1, -1, -1

	for i, m := range members {
		switch {
		case strings.HasSuffix(m, "/syms.cpp.o"):
			globalIdx = i
		case strings.HasSuffix(m, ".reg3.cpp.o"):
			regIdx = i
		case strings.Contains(m, "/objcopy_") && strings.HasSuffix(m, ".o"):
			switch classifyObjcopyMember(g, m) {
			case "explicit":
				explicitIdx = i
			case "py":
				pyIdx = i
			case "pyi":
				pyiIdx = i
			}
		}
	}

	for name, idx := range map[string]int{"explicit objcopy": explicitIdx, "syms.cpp.pic.o": globalIdx, "py objcopy": pyIdx, "pyi objcopy": pyiIdx, ".reg3.cpp.o": regIdx} {
		if idx < 0 {
			t.Fatalf("global AR cmd_args missing %s; members=%v", name, members)
		}
	}

	if !(explicitIdx < globalIdx) {
		t.Errorf("explicit objcopy (%d) must precede syms.cpp.pic.o (%d); members=%v", explicitIdx, globalIdx, members)
	}

	if !(globalIdx < pyIdx) {
		t.Errorf("syms.cpp.pic.o (%d) must precede py objcopy (%d); members=%v", globalIdx, pyIdx, members)
	}

	if !(globalIdx < pyiIdx) {
		t.Errorf("syms.cpp.pic.o (%d) must precede pyi objcopy (%d); members=%v", globalIdx, pyiIdx, members)
	}

	if !(pyIdx < pyiIdx) {
		t.Errorf("py objcopy (%d) must precede pyi objcopy (%d); members=%v", pyIdx, pyiIdx, members)
	}

	if !(pyiIdx < regIdx) {
		t.Errorf("pyi objcopy (%d) must precede .reg3.cpp.o (%d); members=%v", pyiIdx, regIdx, members)
	}
}

func TestGen_GlobalAR_PycryptodomeShape_ExplicitGroupBeforeGlobals(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")

	writeTestModuleFile(files, "crypto/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
SRCS(
    GLOBAL a_syms.cpp
    GLOBAL b_syms.cpp
    GLOBAL c_syms.cpp
)
PY_SRCS(
    TOP_LEVEL
    helper.py
    iface.pyi
)
RESOURCE_FILES(
    PREFIX res/
    data.txt
    meta.txt
)
END()
`)
	writeTestModuleFile(files, "crypto/a_syms.cpp", "int a(){return 0;}\n")
	writeTestModuleFile(files, "crypto/b_syms.cpp", "int b(){return 0;}\n")
	writeTestModuleFile(files, "crypto/c_syms.cpp", "int c(){return 0;}\n")
	writeTestModuleFile(files, "crypto/helper.py", "x = 1\n")
	writeTestModuleFile(files, "crypto/iface.pyi", "x: int\n")
	writeTestModuleFile(files, "crypto/data.txt", "payload\n")
	writeTestModuleFile(files, "crypto/meta.txt", "meta\n")

	g := testGen(newMemFS(files), "crypto")

	members := globalARMembers(t, g)

	firstGlobal, lastGlobal := -1, -1
	var explicitIdxs, pyIdxs []int

	for i, m := range members {
		if strings.HasSuffix(m, "_syms.cpp.o") {
			if firstGlobal < 0 {
				firstGlobal = i
			}

			lastGlobal = i

			continue
		}

		if strings.Contains(m, "/objcopy_") && strings.HasSuffix(m, ".o") {
			switch classifyObjcopyMember(g, m) {
			case "explicit":
				explicitIdxs = append(explicitIdxs, i)
			case "py", "pyi":
				pyIdxs = append(pyIdxs, i)
			}
		}
	}

	if firstGlobal < 0 || lastGlobal < 0 {
		t.Fatalf("global AR cmd_args missing GLOBAL *_syms.cpp.o members; members=%v", members)
	}

	if len(explicitIdxs) == 0 {
		t.Fatalf("global AR cmd_args missing explicit resource objcopies; members=%v", members)
	}

	if len(pyIdxs) == 0 {
		t.Fatalf("global AR cmd_args missing PY_SRCS objcopies; members=%v", members)
	}

	for _, idx := range explicitIdxs {
		if !(idx < firstGlobal) {
			t.Errorf("explicit objcopy at %d must precede first GLOBAL src at %d; members=%v", idx, firstGlobal, members)
		}
	}

	for _, idx := range pyIdxs {
		if !(idx > lastGlobal) {
			t.Errorf("PY_SRCS objcopy at %d must follow last GLOBAL src at %d; members=%v", idx, lastGlobal, members)
		}
	}
}

func EmitAR(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	hostP *Platform,
	emit *StreamingEmitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		throwFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := build(instance.Path.relString() + "/" + archiveName(instance.Path.relString()))

	return emitARNode(instance, archivePath, 0, objRefs, objPaths, peerArchiveRefs, nil, testToolchain(), hostP, emit)
}

var (
	arTestKV = KV{}
)
