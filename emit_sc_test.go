package main

import "testing"

func TestGen_ScSourceEmitsDomschemecProducer(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
SRCS(
    options.sc
)
END()
`)
	files["mod/options.sc"] = "struct TOptions { ui32 X; };\n"

	writeToolProgram(files, "tools/domschemec", "domschemec")

	writeTestModuleFile(files, "library/cpp/domscheme/ya.make", "LIBRARY()\nSRCS(domscheme.cpp)\nEND()\n")
	files["library/cpp/domscheme/domscheme.cpp"] = "int domscheme() { return 0; }\n"
	files["library/cpp/domscheme/runtime.h"] = "#pragma once\n"

	g := testGen(newMemFS(files), "mod")

	sc := findGraphNodeByOutputs(t, g, "$(B)/mod/options.sc.h")

	if sc.KV.P != pkSC {
		t.Fatalf("kv.p = %q, want SC", sc.KV.P)
	}

	if sc.KV.PC != pcYellow {
		t.Fatalf("kv.pc = %q, want yellow", sc.KV.PC)
	}

	cmd := sc.Cmds[0].CmdArgs.flat()
	wantCmd := []string{
		"$(B)/tools/domschemec/domschemec",
		"--in",
		"$(S)/mod/options.sc",
		"--out",
		"$(B)/mod/options.sc.h",
	}

	if len(cmd) != len(wantCmd) {
		t.Fatalf("cmd = %v, want %v", strStrings(cmd), wantCmd)
	}

	for i, w := range wantCmd {
		if cmd[i].string() != w {
			t.Fatalf("cmd[%d] = %q, want %q (full %v)", i, cmd[i].string(), w, strStrings(cmd))
		}
	}

	if len(sc.ForeignDepRefs) != 1 {
		t.Fatalf("ForeignDepRefs = %#v, want exactly the domschemec tool dep", sc.ForeignDepRefs)
	}

	wantInputs := []string{
		"$(B)/tools/domschemec/domschemec",
		"$(S)/mod/options.sc",
		"$(S)/library/cpp/domscheme/runtime.h",
	}

	if got := vfsStringsT3(sc.flatInputs()); !vfsInputsContainAll(got, wantInputs) {
		t.Fatalf("SC inputs = %v, want all of %v", got, wantInputs)
	}

	d := collectTestModule(newMemFS(files), "mod")

	if !peerdirsContain(d, "library/cpp/domscheme") {
		t.Fatalf("module peerdirs = %v, want library/cpp/domscheme", strStrings(d.peerdirs))
	}
}

func TestGen_ScSourceAddsNoAddIncl(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mod/ya.make", "LIBRARY()\nSRCS(options.sc)\nEND()\n")
	files["mod/options.sc"] = "struct TOptions { ui32 X; };\n"

	writeToolProgram(files, "tools/domschemec", "domschemec")
	writeTestModuleFile(files, "library/cpp/domscheme/ya.make", "LIBRARY()\nSRCS(domscheme.cpp)\nEND()\n")
	files["library/cpp/domscheme/domscheme.cpp"] = "int domscheme() { return 0; }\n"
	files["library/cpp/domscheme/runtime.h"] = "#pragma once\n"

	d := collectTestModule(newMemFS(files), "mod")

	genDir := build("mod")

	for _, scope := range []struct {
		name string
		dirs []VFS
	}{
		{"addIncl", d.addIncl},
		{"addInclGlobal", d.addInclGlobal},
		{"addInclUserGlobal", d.addInclUserGlobal},
	} {
		for _, v := range scope.dirs {
			if v == genDir {
				t.Fatalf("%s contains the .sc generated build dir %q; _SRC(\"sc\") adds no addincl (got %v)",
					scope.name, genDir.str().string(), vfsStrings(scope.dirs))
			}
		}
	}
}

func collectTestModule(fs FS, modulePath string) *ModuleData {
	mf := throw2(parseFile(fs, modulePath+"/ya.make"))

	return collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, modulePath, KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source(modulePath), Kind: KindLib, Platform: testTargetP}), noWarn)
}

func peerdirsContain(d *ModuleData, want string) bool {
	for _, p := range d.peerdirs {
		if p.string() == want {
			return true
		}
	}

	return false
}
