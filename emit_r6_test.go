package main

import (
	"strings"
	"testing"
)

func TestEmitR6_RagelHostRecursion_Synthetic(t *testing.T) {
	e := newStreamingEmitter(nil)

	ragel6LD := e.emitNode(Node{
		Cmds:         []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"link"}))}, Env: nil}},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{})},
		KV:           &r6TestKV,
		Outputs:      ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
		Platform:     &Platform{Target: "default-linux-x86_64"},
		Requirements: Requirements{},
	})

	inst := targetInstance("util")
	r6Ref := e.reserve()
	emitR6(inst, "datetime/parser.rl6", source("util/datetime/parser.rl6"), ragel6LD, intern("$(B)/contrib/tools/ragel6/ragel6"), nil, nil, nil, r6Ref, e)
	outPath := ragel6OutVFS(inst, "datetime/parser.rl6")

	wantOut := "$(B)/util/_/datetime/parser.rl6.cpp"

	if outPath.string() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath.string(), wantOut)
	}

	got := e.nodes.s[r6Ref]

	if len(got.Cmds[0].CmdArgs.flat()) != 7 {
		t.Errorf("cmd_args length = %d, want 7", len(got.Cmds[0].CmdArgs.flat()))
	}

	wantCmd := []string{
		"$(B)/contrib/tools/ragel6/ragel6",
		"-CT0",
		"-L",
		"-I$(S)",
		"-o",
		wantOut,
		"$(S)/util/datetime/parser.rl6",
	}

	for i, w := range wantCmd {
		if got.Cmds[0].CmdArgs.flat()[i].string() != w {
			t.Errorf("cmd_args[%d] = %q, want %q", i, got.Cmds[0].CmdArgs.flat()[i].string(), w)
		}
	}

	if got.KV.P != pkR6 {
		t.Errorf("kv.p = %q, want R6", got.KV.P)
	}

	if got.KV.PC != pcYellow {
		t.Errorf("kv.pc = %q, want yellow", got.KV.PC)
	}

	if string(got.Platform.Target) != string(PlatformDefaultLinuxAArch64) {
		t.Errorf("platform = %q, want %q", string(got.Platform.Target), PlatformDefaultLinuxAArch64)
	}

	if got.ForeignDepRefs[0] != ragel6LD {
		t.Errorf("ForeignDepRefs[0] = %v, want %v", got.ForeignDepRefs[0], ragel6LD)
	}

	if len(got.ForeignDepRefs) != 1 {
		t.Errorf("ForeignDepRefs len = %d, want 1 (PR-L4-C/07 restores foreign_deps[tool])", len(got.ForeignDepRefs))
	} else if len(got.ForeignDepRefs) != 1 || got.ForeignDepRefs[0] != ragel6LD {
		t.Errorf("ForeignDepRefs[tool] = %v, want [%v]", got.ForeignDepRefs, ragel6LD)
	}

	if got.Requirements.Network != nwRestricted {
		t.Errorf("requirements.network = %v, want restricted", got.Requirements.Network.string())
	}
}

func TestCollectModule_Ragel6FlagsMultiTokenSplit(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(\n    RAGEL6_FLAGS\n    -L\n    -G2\n)\nSRCS(x.rl6)\nEND()\n",
		"mod/x.rl6":   "%%{ machine x; }%%\n",
	})
	d := collectTestModule(fs, "mod")
	got := argStrs(d.ragel6Flags)
	want := []string{"-L", "-G2"}

	if !equalStrings(got, want) {
		t.Fatalf("ragel6Flags = %v, want %v (SET-list must expand as separate argv tokens)", got, want)
	}
}

func TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags(t *testing.T) {
	e := newStreamingEmitter(nil)

	ragel6LD := e.emitNode(Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"link"}))}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      &r6TestKV,
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	r6Ref := e.reserve()
	emitR6(
		targetInstance("devtools/ymake/lang/makelists"),
		"makefile_lang.rl6",
		source("devtools/ymake/lang/makelists/makefile_lang.rl6"),
		ragel6LD,
		intern("$(B)/contrib/tools/ragel6/ragel6"),
		internArgs([]string{"-lF1"}),
		nil,
		nil,
		r6Ref,
		e,
	)

	got := e.nodes.s[r6Ref]

	if got.Cmds[0].CmdArgs.flat()[1].string() != "-lF1" {
		t.Errorf("cmd_args[1] = %q, want -lF1 (per-module SET(RAGEL6_FLAGS) override)", got.Cmds[0].CmdArgs.flat()[1].string())
	}

	for i, a := range got.Cmds[0].CmdArgs.flat() {
		if a.string() == "-CT0" || a.string() == "-CG2" {
			t.Errorf("cmd_args[%d] = %q — default flag leaked through the SET override", i, a.string())
		}
	}

	if len(got.Cmds[0].CmdArgs.flat()) != 7 {
		t.Errorf("cmd_args length = %d, want 7 (SET(RAGEL6_FLAGS -lF1) is a 1-arg replacement, not an append)", len(got.Cmds[0].CmdArgs.flat()))
	}
}

func TestEmitR6_X8664HostDefault_PR_M3_ragel_flags(t *testing.T) {
	e := newStreamingEmitter(nil)

	ragel6LD := e.emitNode(Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"link"}))}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      &r6TestKV,
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	releaseHostFlags := map[string]string{}

	for k, v := range testToolchainFlags {
		releaseHostFlags[k] = v
	}

	releaseHostFlags["PIC"] = "yes"
	releaseHostFlags["GG_BUILD_TYPE"] = "release"
	releaseHost := newPlatform(newMemFS(nil), OSLinux, ISAX8664, releaseHostFlags, "", "")

	r6Ref := e.reserve()
	emitR6(
		ModuleInstance{
			Path:     source("util"),
			Kind:     KindLib,
			Language: LangCPP,
			Platform: releaseHost,
		},
		"datetime/parser.rl6",
		source("util/datetime/parser.rl6"),
		ragel6LD,
		intern("$(B)/contrib/tools/ragel6/ragel6"),
		nil,
		nil,
		nil,
		r6Ref,
		e,
	)

	got := e.nodes.s[r6Ref]

	if got.Cmds[0].CmdArgs.flat()[1].string() != "-CG2" {
		t.Errorf("cmd_args[1] = %q, want -CG2 (x86_64 host = release = optimized)", got.Cmds[0].CmdArgs.flat()[1].string())
	}
}

func TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z(t *testing.T) {
	e := newStreamingEmitter(nil)

	ragel6LD := e.emitNode(Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"link"}))}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      &r6TestKV,
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	closure := []VFS{
		intern("$(S)/util/datetime/parser.rl6"),
		intern("$(S)/util/datetime/parser.h"),
		intern("$(S)/util/generic/ymath.h"),
	}

	r6Ref := e.reserve()
	emitR6(targetInstance("util"), "datetime/parser.rl6", source("util/datetime/parser.rl6"), ragel6LD, intern("$(B)/contrib/tools/ragel6/ragel6"), nil, closure, nil, r6Ref, e)

	got := e.nodes.s[r6Ref]

	wantInputs := []string{
		"$(B)/contrib/tools/ragel6/ragel6",
		"$(S)/util/datetime/parser.rl6",
		"$(S)/util/datetime/parser.h",
		"$(S)/util/generic/ymath.h",
	}

	if len(got.flatInputs()) != len(wantInputs) {
		t.Fatalf("R6 inputs len = %d, want %d (got=%v)", len(got.flatInputs()), len(wantInputs), got.flatInputs())
	}

	for i, w := range wantInputs {
		if got.flatInputs()[i].string() != w {
			t.Errorf("R6 inputs[%d] = %q, want %q", i, got.flatInputs()[i].string(), w)
		}
	}
}

func TestGen_HostToolRecursion_R6(t *testing.T) {
	fs := newMemFS(map[string]string{
		"contrib/tools/ragel6/ya.make": "PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n",
		"consumer/ya.make":             "LIBRARY()\nSRCS(parser.rl6)\nEND()\n",
	})

	g := testGen(fs, "consumer")

	counts := make(map[string]int)
	platforms := make(map[string]int)

	for _, n := range g.Graph {
		p := n.KV.P.string()
		counts[p]++
		platforms[string(n.Platform.Target)]++
	}

	if counts["R6"] != 1 {
		t.Errorf("R6 count = %d, want 1", counts["R6"])
	}

	if counts["LD"] != 1 {
		t.Errorf("LD count = %d, want 1 (host ragel6 LD)", counts["LD"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1 (target consumer AR)", counts["AR"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (host ragel6/main.cpp + target generated parser.rl6.cpp)", counts["CC"])
	}

	if platforms[string(PlatformDefaultLinuxAArch64)] != 3 {
		t.Errorf("target nodes = %d, want 3", platforms[string(PlatformDefaultLinuxAArch64)])
	}

	if platforms[string(PlatformDefaultLinuxX8664)] != 3 {
		t.Errorf("host nodes (by platform) = %d, want 3 (vcs.json is host-bound)", platforms[string(PlatformDefaultLinuxX8664)])
	}

	var (
		r6Node *Node
		ldNode *Node
	)

	for _, n := range g.Graph {
		switch n.KV.P.string() {
		case "R6":
			r6Node = n
		case "LD":
			ldNode = n
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node found")
	}

	if ldNode == nil {
		t.Fatal("no host ragel6 LD node found")
	}

	if len(graphDeps(g, r6Node)) != 1 || graphDeps(g, r6Node)[0] != ldNode.Ref {
		t.Errorf("R6 Deps = %v, want [%q]", graphDeps(g, r6Node), ldNode.Ref)
	}

	if len(graphForeignDeps(g, r6Node)) != 1 || len(graphForeignDeps(g, r6Node)) != 1 || graphForeignDeps(g, r6Node)[0] != ldNode.Ref {
		t.Errorf("R6 ForeignDeps = %v, want {tool: [%d]}", graphForeignDeps(g, r6Node), ldNode.Ref)
	}

	if len(r6Node.Cmds) == 0 || len(r6Node.Cmds[0].CmdArgs.flat()) == 0 {
		t.Fatalf("R6 node has no Cmds[0].CmdArgs.flat(); got Cmds=%v", r6Node.Cmds)
	}

	if len(ldNode.Outputs) == 0 {
		t.Fatalf("host LD node has no Outputs; got Outputs=%v", ldNode.Outputs)
	}

	wantCmd0 := ldNode.Outputs[0].string()

	if r6Node.Cmds[0].CmdArgs.flat()[0].string() != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want host ragel6 LD outputs[0] = %q (raw outputs[0] = %q)",
			r6Node.Cmds[0].CmdArgs.flat()[0], wantCmd0, ldNode.Outputs[0].string())
	}
}

func TestRagel6OutVFS_DefextNoext(t *testing.T) {
	inst := targetInstance("library/cpp/config")

	cases := []struct {
		srcRel string
		want   string
	}{
		{"parser.rl6", "$(B)/library/cpp/config/parser.rl6.cpp"},

		{"datetime/parser.rl6", "$(B)/library/cpp/config/_/datetime/parser.rl6.cpp"},

		{"markupfsm.h.rl6", "$(B)/library/cpp/config/markupfsm.h"},
	}

	for _, c := range cases {
		got := ragel6OutVFS(inst, c.srcRel).string()

		if got != c.want {
			t.Errorf("ragel6OutVFS(%q) = %q, want %q", c.srcRel, got, c.want)
		}
	}
}

func TestGen_Ragel6HeaderOutputNotCompiled(t *testing.T) {
	fs := newMemFS(map[string]string{
		"contrib/tools/ragel6/ya.make": "PROGRAM(ragel6)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCS(main.cpp)\nEND()\n",
		"mod/ya.make":                  "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(markupfsm.h.rl6 parser.rl6)\nEND()\n",
		"mod/markupfsm.h.rl6":          "%%{ machine m; }%%\n",
		"mod/parser.rl6":               "%%{ machine p; }%%\n",
	})

	g := testGen(fs, "mod")

	var (
		r6Outs   []string
		ccOuts   []string
		headerR6 bool
	)

	for _, n := range g.Graph {
		switch n.KV.P.string() {
		case "R6":
			for _, o := range n.Outputs {
				r6Outs = append(r6Outs, o.string())

				if o.string() == "$(B)/mod/markupfsm.h" {
					headerR6 = true
				}
			}
		case "CC":
			for _, o := range n.Outputs {
				ccOuts = append(ccOuts, o.string())
			}
		}
	}

	if !headerR6 {
		t.Errorf("no R6 node producing $(B)/mod/markupfsm.h; R6 outputs = %v", r6Outs)
	}

	for _, o := range r6Outs {
		if strings.Contains(o, "markupfsm.h.rl6.cpp") {
			t.Errorf("R6 produced %q, want the header $(B)/mod/markupfsm.h (defext suppressed by .h stem)", o)
		}
	}

	for _, o := range ccOuts {
		if strings.Contains(o, "markupfsm") {
			t.Errorf("header ragel output was compiled (%q); upstream does not compile a .h artifact", o)
		}
	}

	wantParserCC := false

	for _, o := range ccOuts {
		if strings.Contains(o, "parser.rl6.cpp") {
			wantParserCC = true
		}
	}

	if !wantParserCC {
		t.Errorf("sibling parser.rl6 lost its compiled parser.rl6.cpp object; CC outputs = %v", ccOuts)
	}
}

func TestGen_GeneratorWiredIntoDepRefs_R6(t *testing.T) {
	fs := newMemFS(map[string]string{
		"r6mod/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(thing.rl6)
END()
`,
		"contrib/tools/ragel6/ya.make": `PROGRAM(ragel6)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`,
	})

	g := testGen(fs, "r6mod")

	var r6Node, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV.P.string() {
		case "R6":
			r6Node = n
		case "CC":

			ip := ""

			if len(n.flatInputs()) > 0 {
				ip = n.flatInputs()[0].string()
			}

			if ccNode == nil && strings.HasPrefix(ip, "$(B)/") {
				ccNode = n
			}
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node emitted")
	}

	if ccNode == nil {
		t.Fatal("no R6-derived CC node emitted")
	}

	found := false

	for _, dep := range graphDeps(g, ccNode) {
		if dep == r6Node.Ref {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("R6-derived graphDeps(g, CC) = %v, want to contain R6 ref %d (PR-30 D04 Generator wiring)", graphDeps(g, ccNode), r6Node.Ref)
	}
}

var (
	r6TestKV = KV{P: pkLD}
)

func TestGen_Ragel6ScansSiblingGeneratedHeader(t *testing.T) {
	files := map[string]string{}
	writeBisonProducer(files)
	writeToolProgram(files, "contrib/tools/ragel6", "ragel6")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    lexer.rl6
    parser.y
)
END()
`)
	writeTestModuleFile(files, "mod/lexer.rl6", `#include "parser.h"
%%{
    machine x;
    main := 'a';
}%%
`)
	writeTestModuleFile(files, "mod/parser.y", "%%\n")

	_, warns := testGenWarns(newMemFS(files), "mod")

	assertNoMissingInclude(t, warns, "parser.h")
}
