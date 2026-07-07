package main

import (
	"slices"
	"testing"
)

func TestGen_RunLuaOutRl6FeedsRagel(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/dtr/ya.make", `LIBRARY(dtr)
SRCS(plain.cpp)
RUN_LUA(
    gen.lua patterns.rl6
    IN data.rl6
    OUT patterns.rl6
)
END()
`)
	writeTestModuleFile(files, "lib/dtr/plain.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "lib/dtr/gen.lua", "-- stub\n")
	writeTestModuleFile(files, "lib/dtr/data.rl6", "%%{ machine d; }%%\n")
	writeToolProgram(files, "tools/lua", "lua")
	writeToolProgram(files, "contrib/tools/ragel6", "ragel6")

	g := testGen(newMemFS(files), "lib/dtr")
	lu := mustNodeByOutput(t, g, "$(B)/lib/dtr/patterns.rl6")
	r6 := mustNodeByAnyOutput(t, g, "$(B)/lib/dtr/patterns.rl6.cpp")

	if !nodeHasInput(r6, "$(B)/lib/dtr/patterns.rl6") {
		t.Fatalf("ragel node inputs missing generated rl6: %v", vfsStringsT3(r6.flatInputs()))
	}

	if !slices.Contains(graphDeps(g, r6), lu.Ref) {
		t.Fatalf("ragel node deps missing lua producer %d: %v", lu.Ref, graphDeps(g, r6))
	}
}
