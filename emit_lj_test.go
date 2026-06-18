package main

import (
	"strings"
	"testing"
)

// TestEmitLj21Archive_RawCompilationAndArchive covers LJ_21_ARCHIVE end to end:
// each declared .lua compiles to a .raw via an LJ node (luajit_21 compiler, cwd
// $(S)/contrib/libs/luajit_21, kv p=LJ), and the LuaScripts.inc archive_by_keys
// consumes those raws (plain members + `-k <keys>`), depending on the producers.
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

	// (1) the LJ node for a.lua: exact command, cwd, inputs, outputs, kv.
	lj := mustNodeByOutput(t, g, "$(B)/mod/a.raw")

	if lj.KV.P != pkLJ || lj.KV.PC != pcLightCyan {
		t.Errorf("LJ node kv = {p:%q pc:%q}, want {LJ light-cyan}", lj.KV.P.string(), lj.KV.PC.string())
	}
	if len(lj.Cmds) != 1 {
		t.Fatalf("LJ node has %d cmds, want 1", len(lj.Cmds))
	}
	gotCmd := strStrs(lj.Cmds[0].CmdArgs.flat())
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

	// the nested source resolves under the module dir, not a doubled prefix.
	ljB := mustNodeByOutput(t, g, "$(B)/mod/sub/b.raw")
	if !nodeHasInput(ljB, "$(S)/mod/sub/b.lua") {
		t.Errorf("nested LJ inputs %v missing $(S)/mod/sub/b.lua", vfsStringsT3(ljB.flatInputs()))
	}

	// (2) the LuaScripts.inc archive consumes the raws plain, keyed by lua names,
	// and depends on the LJ producers.
	ar := mustNodeByOutput(t, g, "$(B)/mod/LuaScripts.inc")
	if ar.KV.P != pkAR {
		t.Errorf("archive node kv.p = %q, want AR", ar.KV.P.string())
	}
	arCmd := strings.Join(strStrs(ar.Cmds[0].CmdArgs.flat()), " ")
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
		if dep == lj.UID {
			arDepsLJ = true
			break
		}
	}
	if !arDepsLJ {
		t.Errorf("graphDeps(archive) %v does not include the LJ producer uid %q", graphDeps(g, ar), lj.UID)
	}

	// (3) LuaSources.inc archives the .lua sources themselves.
	src := mustNodeByOutput(t, g, "$(B)/mod/LuaSources.inc")
	srcCmd := strings.Join(strStrs(src.Cmds[0].CmdArgs.flat()), " ")
	if !strings.Contains(srcCmd, "$(S)/mod/a.lua $(S)/mod/sub/b.lua") || !strings.Contains(srcCmd, "-k a.lua:sub/b.lua") {
		t.Errorf("LuaSources.inc cmd %q missing the lua sources / keys", srcCmd)
	}
}
