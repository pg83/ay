package main

import (
	"slices"
	"testing"
)

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

// A RUN_PROGRAM whose OUT and TOOL carry an explicit ${ARCADIA_BUILD_ROOT}/…
// prefix (the apphost cow well_known shape): the OUT lives under the declaring
// module's ${MODDIR} as a build output, the TOOL names a built module by its
// build-root path (resolved by stripping the root prefix — not opened as a
// source ya.make), and the literal $(B)/tool/binary already in the args must
// survive unrewritten. The module's ADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/…)
// then propagates to a PEERDIR consumer, whose include scan binds the generated
// build-root header.
func TestGen_RunProgramBuildRootOutToolAndGlobalAddInclPropagate(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/protoc", "protoc")
	writeToolProgram(files, "tools/gen", "gen")

	writeTestModuleFile(files, "gen/wk/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/protoc
        --plugin=protoc-gen-custom=${ARCADIA_BUILD_ROOT}/tools/gen/gen
        any.proto
    IN_NOPARSE
        any.proto
    TOOL
        ${ARCADIA_BUILD_ROOT}/tools/gen
    OUT
        ${ARCADIA_BUILD_ROOT}/${MODDIR}/sub/any.cow.pb.h
)
ADDINCL(
    GLOBAL ${ARCADIA_BUILD_ROOT}/gen/wk
)
END()
`)
	writeTestModuleFile(files, "gen/wk/any.proto", "// proto\n")

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen/wk)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", "#include <sub/any.cow.pb.h>\nint use(){return 0;}\n")

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

	// The build-root OUT is modeled as a real producer output.
	const genHeader = "$(B)/gen/wk/sub/any.cow.pb.h"
	producer := mustNodeByAnyOutput(t, g, genHeader)

	// The literal build-root binary path in the args is preserved, not rewritten
	// by the TOOL substitution (a $(B)/tools/gen prefix of $(B)/tools/gen/gen).
	if !contains(producer.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-custom=$(B)/tools/gen/gen") {
		t.Fatalf("producer plugin arg corrupted: %v", strStrs(producer.Cmds[0].CmdArgs.flat()))
	}

	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	// The explicit build-root GLOBAL ADDINCL reaches the PEERDIR consumer.
	if !contains(use.Cmds[0].CmdArgs.flat(), "-I$(B)/gen/wk") {
		t.Fatalf("use.cpp.o missing -I$(B)/gen/wk: %v", strStrs(use.Cmds[0].CmdArgs.flat()))
	}

	// Include scanning binds the generated build-root header as a consumer input.
	if !nodeHasInput(use, genHeader) {
		t.Fatalf("use.cpp.o inputs missing %q: %#v", genHeader, use.flatInputs())
	}
	if !slices.Contains(graphDeps(g, use), producer.UID) {
		t.Fatalf("use.cpp.o deps missing producer uid %q: %v", producer.UID, graphDeps(g, use))
	}
}
