package main

import (
	"slices"
	"testing"
)

// TestStarlark_RunProgram pins the run_program() builtin: every keyword section maps to
// the matching ya.make RUN_PROGRAM section, producing an identical RunProgramStmt.
func TestStarlark_RunProgram(t *testing.T) {
	env := DefaultIfEnv.clone()

	assertSameStmts(t,
		evalStarStr(t, `library(srcs = ["a.cpp"] + run_program(
    "//tool/gen",
    args = ["--flag", "v"],
    ins = ["in.txt"],
    outs = ["out.cpp"],
    out_noauto = ["log"],
    stdout = ["s.out"],
    env = ["K=V"],
    output_includes = ["h.h"],
    induced_deps = ["d.h"],
    tools = ["//t"],
    cwd = "sub",
))`, env),
		parseMakeStr(t, "LIBRARY()\nSRCS(a.cpp)\n"+
			"RUN_PROGRAM(//tool/gen --flag v IN in.txt OUT out.cpp OUT_NOAUTO log "+
			"STDOUT s.out ENV K=V OUTPUT_INCLUDES h.h INDUCED_DEPS d.h TOOL //t CWD sub)\nEND()\n"))
}

// TestStarlark_RunPy3Program pins run_py3_program() — the same RunProgramStmt shape under
// the RUN_PY3_PROGRAM name.
func TestStarlark_RunPy3Program(t *testing.T) {
	env := DefaultIfEnv.clone()

	assertSameStmts(t,
		evalStarStr(t, `library(srcs = ["a.cpp"] + run_py3_program("//tool", args = ["x"], outs = ["o.cpp"]))`, env),
		parseMakeStr(t, "LIBRARY()\nSRCS(a.cpp)\nRUN_PY3_PROGRAM(//tool x OUT o.cpp)\nEND()\n"))
}

func TestGen_RunProgramHeaderOutputClosurePropagatesInputs(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	writeTestModuleFile(files, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(files, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(files, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/genhdr
        template.h.in
        gen.h
    OUTPUT_INCLUDES
        dep/dep.h
    IN
        template.h.in
    OUT
        gen.h
)
END()
`)
	writeTestModuleFile(files, "gen/template.h.in", "#pragma once\n")

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", `#include <gen/gen.h>
int use() { return 0; }
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")
	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	for _, want := range []string{
		"$(B)/gen/gen.h",
		"$(S)/gen/template.h.in",
		"$(S)/dep/dep.h",
	} {
		if !nodeHasInput(use, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, use.flatInputs())
		}
	}
	if !slices.Contains(graphDeps(g, use), genH.UID) {
		t.Fatalf("use.cpp.o deps missing generated-header PR uid %q: %v", genH.UID, graphDeps(g, use))
	}
}
