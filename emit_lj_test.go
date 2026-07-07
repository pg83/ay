package main

import (
	"strings"
	"testing"
)

func TestEmitLj21Archive_RawCompilationAndArchive(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/libs/luajit_21/compiler", "compiler")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "mod/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"LJ_21_ARCHIVE(\n    NAME LuaScripts.inc\n    a.lua\n    sub/b.lua\n)\nEND()\n")
	writeTestModuleFile(files, "mod/a.lua", "return 1\n")
	writeTestModuleFile(files, "mod/sub/b.lua", "return 2\n")

	g := testGen(newMemFS(files), "mod")

	const compilerBin = "$(B)/contrib/libs/luajit_21/compiler/compiler"

	lj := mustNodeByOutput(t, g, "$(B)/mod/a.raw")

	if lj.KV.P != pkLJ || lj.KV.PC != pcLightCyan {
		t.Errorf("LJ node kv = {p:%q pc:%q}, want {LJ light-cyan}", lj.KV.P.string(), lj.KV.PC.string())
	}

	if len(lj.Cmds) != 1 {
		t.Fatalf("LJ node has %d cmds, want 1", len(lj.Cmds))
	}

	gotCmd := anyStrs(lj.Cmds[0].CmdArgs.flat())
	wantCmd := []string{compilerBin, "-b", "-g", "$(S)/mod/a.lua", "$(B)/mod/a.raw"}

	if strings.Join(gotCmd, " ") != strings.Join(wantCmd, " ") {
		t.Errorf("LJ cmd = %v, want %v", gotCmd, wantCmd)
	}

	if lj.Cmds[0].Cwd.string() != "$(S)/contrib/libs/luajit_21" {
		t.Errorf("LJ cwd = %q, want $(S)/contrib/libs/luajit_21", lj.Cmds[0].Cwd.string())
	}

	if !nodeHasInput(lj, "$(S)/mod/a.lua") || !nodeHasInput(lj, compilerBin) {
		t.Errorf("LJ inputs %v missing the lua source or compiler binary", vfsStringsT3(lj.flatInputs()))
	}

	ljB := mustNodeByOutput(t, g, "$(B)/mod/sub/b.raw")

	if !nodeHasInput(ljB, "$(S)/mod/sub/b.lua") {
		t.Errorf("nested LJ inputs %v missing $(S)/mod/sub/b.lua", vfsStringsT3(ljB.flatInputs()))
	}

	ar := mustNodeByOutput(t, g, "$(B)/mod/LuaScripts.inc")

	if ar.KV.P != pkAR {
		t.Errorf("archive node kv.p = %q, want AR", ar.KV.P.string())
	}

	arCmd := strings.Join(anyStrs(ar.Cmds[0].CmdArgs.flat()), " ")

	for _, want := range []string{
		"$(B)/mod/a.raw $(B)/mod/sub/b.raw",
		"-k a.lua:sub/b.lua",
		"-o $(B)/mod/LuaScripts.inc",
	} {
		if !strings.Contains(arCmd, want) {
			t.Errorf("archive cmd %q missing %q", arCmd, want)
		}
	}

	if strings.Contains(arCmd, "$(B)/mod/a.raw:") {
		t.Errorf("archive_by_keys must list members plain, got colon-suffixed: %q", arCmd)
	}

	arDepsLJ := false

	for _, dep := range graphDeps(g, ar) {
		if dep == lj.Ref {
			arDepsLJ = true

			break
		}
	}

	if !arDepsLJ {
		t.Errorf("graphDeps(archive) %v does not include the LJ producer ref %d", graphDeps(g, ar), lj.Ref)
	}

	src := mustNodeByOutput(t, g, "$(B)/mod/LuaSources.inc")
	srcCmd := strings.Join(anyStrs(src.Cmds[0].CmdArgs.flat()), " ")

	if !strings.Contains(srcCmd, "$(S)/mod/a.lua $(S)/mod/sub/b.lua") || !strings.Contains(srcCmd, "-k a.lua:sub/b.lua") {
		t.Errorf("LuaSources.inc cmd %q missing the lua sources / keys", srcCmd)
	}
}

func TestEmitLj21Archive_ArchiveOutputAddInclAndClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/libs/luajit_21/compiler", "compiler")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "mod/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"SRCS(\n    templates.cpp\n    templates_sources.cpp\n)\n"+
			"LJ_21_ARCHIVE(\n    NAME LuaScripts.inc\n    a.lua\n    sub/b.lua\n)\nEND()\n")
	writeTestModuleFile(files, "mod/a.lua", "return 1\n")
	writeTestModuleFile(files, "mod/sub/b.lua", "return 2\n")
	writeTestModuleFile(files, "mod/templates.cpp", "#include \"LuaScripts.inc\"\n")
	writeTestModuleFile(files, "mod/templates_sources.cpp", "#include \"LuaSources.inc\"\n")

	g := testGen(newMemFS(files), "mod")

	ccBySuffix := func(suffix string) *Node {
		t.Helper()

		for _, n := range g.Graph {
			if n.KV.P == pkCC && len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), suffix) {
				return n
			}
		}

		t.Fatalf("no CC node with output suffix %q", suffix)

		return nil
	}

	const buildInc = "-I$(B)/mod"

	scripts := ccBySuffix("/templates.cpp.o")

	if !contains(scripts.Cmds[0].CmdArgs.flat(), buildInc) {
		t.Errorf("templates.cpp.o cmd missing %q; got %v", buildInc, anyStrs(scripts.Cmds[0].CmdArgs.flat()))
	}

	sources := ccBySuffix("/templates_sources.cpp.o")

	if !contains(sources.Cmds[0].CmdArgs.flat(), buildInc) {
		t.Errorf("templates_sources.cpp.o cmd missing %q; got %v", buildInc, anyStrs(sources.Cmds[0].CmdArgs.flat()))
	}

	for _, lua := range []string{"$(S)/mod/a.lua", "$(S)/mod/sub/b.lua"} {
		if !nodeHasInput(scripts, lua) {
			t.Errorf("templates.cpp.o inputs %v missing LuaScripts.inc closure leaf %q", vfsStringsT3(scripts.flatInputs()), lua)
		}

		if !nodeHasInput(sources, lua) {
			t.Errorf("templates_sources.cpp.o inputs %v missing LuaSources.inc closure leaf %q", vfsStringsT3(sources.flatInputs()), lua)
		}
	}
}
