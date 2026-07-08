package main

import (
	"testing"
)

func TestGen_FlexOldDefaultLexerGeneration(t *testing.T) {
	fs := newMemFS(map[string]string{
		"contrib/tools/flex-old/ya.make":     "PROGRAM(flex)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n",
		"contrib/tools/flex-old/main.cpp":    "int main(){return 0;}\n",
		"contrib/tools/flex-old/FlexLexer.h": "#pragma once\n",
		"lex/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(lexer.l)
END()
`,
		"lex/lexer.l": `%{
#include "lexer.h"
%}
%%
`,
		"lex/lexer.h": "#pragma once\n#include <FlexLexer.h>\n",
	})

	g := testGen(fs, "lex")

	lx := mustNodeByOutput(t, g, "$(B)/lex/lexer.l.cpp")

	if lx.KV.P != pkLX || lx.KV.PC != pcYellow {
		t.Errorf("LX kv = {p:%q pc:%q}, want {LX yellow}", lx.KV.P.string(), lx.KV.PC.string())
	}

	wantCmd := []string{
		"$(B)/contrib/tools/flex-old/flex",
		"-o$(B)/lex/lexer.l.cpp",
		"$(S)/lex/lexer.l",
	}
	got := anyStrs(lx.Cmds[0].CmdArgs.flat())

	if len(got) != len(wantCmd) {
		t.Fatalf("LX cmd_args = %#v, want %#v", got, wantCmd)
	}

	for i, w := range wantCmd {
		if got[i] != w {
			t.Errorf("LX cmd_args[%d] = %q, want %q", i, got[i], w)
		}
	}

	if len(lx.Env) != 1 || lx.Env[0].Name != envARCADIA_ROOT_DISTBUILD || lx.Env[0].Value != strS.any() {
		t.Errorf("LX env = %#v, want [ARCADIA_ROOT_DISTBUILD=$(S)]", lx.Env)
	}

	for _, want := range []string{
		"$(B)/contrib/tools/flex-old/flex",
		"$(S)/lex/lexer.l",
	} {
		if !nodeHasInput(lx, want) {
			t.Errorf("LX inputs missing %q: %#v", want, lx.flatInputs())
		}
	}

	cc := mustNodeByOutput(t, g, "$(B)/lex/lexer.l.cpp.o")

	if cc.KV.P != pkCC {
		t.Fatalf("generated lexer.l.cpp.o kv.p = %q, want CC", cc.KV.P.string())
	}

	if !depsContain(graphDeps(g, cc), lx.Ref) {
		t.Errorf("generated CC deps %v missing LX producer ref %d", graphDeps(g, cc), lx.Ref)
	}

	if !cmdHasArg(cc, "-I$(S)/contrib/tools/flex-old") {
		t.Errorf("generated CC missing -I$(S)/contrib/tools/flex-old: %#v", anyStrs(cc.Cmds[0].CmdArgs.flat()))
	}

	ar := mustNodeByOutput(t, g, "$(B)/lex/liblex.a")

	if !depsContain(graphDeps(g, ar), cc.Ref) {
		t.Errorf("module AR deps %v missing generated object ref %d", graphDeps(g, ar), cc.Ref)
	}
}

func TestGen_FlexDoesNotPerturbBisonOrPlainCpp(t *testing.T) {
	files := map[string]string{}
	addToolchainPeers(files)
	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.relString(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.relString(), "")
	}

	writeTestModuleFile(files, "y/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(g.y)\nEND()\n")
	writeTestModuleFile(files, "y/g.y", "%%\n")
	writeTestModuleFile(files, "plain/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n")
	writeTestModuleFile(files, "plain/a.cpp", "int a(){return 0;}\n")

	gy := testGen(newMemFS(files), "y")

	if countKind(gy, pkLX) != 0 {
		t.Errorf("bison module emitted %d LX nodes, want 0", countKind(gy, pkLX))
	}

	if countKind(gy, pkYC) != 1 {
		t.Errorf("bison module emitted %d YC nodes, want 1 (unchanged)", countKind(gy, pkYC))
	}

	gp := testGen(newMemFS(files), "plain")

	if countKind(gp, pkLX) != 0 {
		t.Errorf("plain .cpp module emitted %d LX nodes, want 0", countKind(gp, pkLX))
	}
}

func cmdHasArg(n *Node, arg string) bool {
	for _, c := range n.Cmds {
		for _, a := range c.CmdArgs.flat() {
			if a.string() == arg {
				return true
			}
		}
	}

	return false
}

func countKind(g *Graph, k ProcKind) int {
	c := 0

	for _, n := range g.Graph {
		if n.KV.P == k {
			c++
		}
	}

	return c
}

func TestGen_FlexScansSiblingGeneratedHeader(t *testing.T) {
	files := map[string]string{}
	writeBisonProducer(files)
	writeToolProgram(files, "contrib/tools/flex-old", "flex-old")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    lexer.l
    parser.y
)
END()
`)
	writeTestModuleFile(files, "mod/lexer.l", `%{
#include "parser.h"
%}
%%
`)
	writeTestModuleFile(files, "mod/parser.y", "%%\n")

	_, warns := testGenWarns(newMemFS(files), "mod")

	assertNoMissingInclude(t, warns, "parser.h")
}
