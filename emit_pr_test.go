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

// A RUN_PROGRAM auto STDOUT output with an assembler extension (.asm) is, per
// ymake auto-output semantics, a module source: it must be compiled by the
// assembler and archived into the module library, exactly like a declared .asm
// SRC. This is the connectivity the sg7 icookie blacklist .pic.o depends on —
// yabs/server/cs/libs/mkdb_info/builtin RUN_PROGRAMs dump_mkdb_info (a host
// tool whose PIC closure links the icookie libraries) STDOUT mkdb_info.asm, and
// only the resulting mkdb_info.o member edge pulls that tool closure into the
// program's target closure. Before the fix the .asm output was registered for
// include resolution but never compiled or archived, leaving the tool closure
// disconnected.
func TestGen_RunProgramAutoStdoutAsmCompiledAndArchived(t *testing.T) {
	files := map[string]string{}

	// The host tool that produces the .asm, with its own library peer (stands
	// in for the icookie-style LIBRARY reached only through the tool closure).
	writeTestModuleFile(files, "cookie/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(cookie.cpp)
END()
`)
	writeTestModuleFile(files, "cookie/cookie.cpp", "int cookie(){return 0;}\n")

	writeTestModuleFile(files, "tools/dumper/ya.make", `PROGRAM(dumper)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cookie)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/dumper/main.cpp", "int main(){return 0;}\n")

	// The LIBRARY whose RUN_PROGRAM auto STDOUT is a .asm.
	writeTestModuleFile(files, "builtin/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/dumper archive_asm
    STDOUT gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "builtin/builtin.cpp", "int builtin(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(builtin)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	// The RUN_PROGRAM auto STDOUT .asm producer.
	asmProducer := mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")

	// (1) The .asm is compiled to an object by the assembler.
	asmObj := mustNodeByOutput(t, g, "$(B)/builtin/gen.o")

	// The assembler compile depends on the RUN_PROGRAM producer (so the .asm
	// exists before it runs).
	if !slices.Contains(graphDeps(g, asmObj), asmProducer.UID) {
		t.Fatalf("gen.o deps missing RUN_PROGRAM producer uid %q: %v", asmProducer.UID, graphDeps(g, asmObj))
	}

	// (2) The object is a member of the module library.
	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")
	if !nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a missing member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}

	// (3) The RUN_PROGRAM tool program — and through it its library peer —
	// becomes reachable from the program target closure (the disconnected-tool
	// failure mode the icookie residual exhibits).
	mustNodeByAnyOutput(t, g, "$(B)/tools/dumper/dumper")
	mustNodeByOutput(t, g, "$(B)/cookie/libcookie.a")
}

// STDOUT_NOAUTO is upstream's ${stdout;noauto;output:STDOUT_NOAUTO} — the noauto
// modifier marks the redirect as NOT a module source (ymake.core.conf:4780,4832),
// exactly like OUT_NOAUTO vs OUT. A RUN_PROGRAM(... STDOUT_NOAUTO gen.asm) must
// therefore NOT be assembled or archived: the .asm is still a declared output of
// the producer (so it exists for any consumer that #includes it), but it never
// becomes a downstream module source. Before the fix the parser collapsed STDOUT
// and STDOUT_NOAUTO into one field, so the auto-output compile loop assembled the
// noauto output too.
func TestGen_RunProgramStdoutNoautoAsmNotCompiled(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "tools/dumper/ya.make", `PROGRAM(dumper)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/dumper/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "builtin/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/dumper archive_asm
    STDOUT_NOAUTO gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "builtin/builtin.cpp", "int builtin(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(builtin)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	// The STDOUT_NOAUTO .asm is a declared producer output (so a consumer that
	// includes it can resolve it), but it is NOT compiled to an object…
	mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")
	if n := nodeByOutput(g, "$(B)/builtin/gen.o"); n != nil {
		t.Fatalf("STDOUT_NOAUTO gen.asm must not be assembled, but $(B)/builtin/gen.o exists")
	}

	// …and not archived into the module library.
	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")
	if nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a must not contain noauto member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}
}

// RUN_PYTHON3 STDOUT_NOAUTO mirrors RUN_PROGRAM's: the noauto stdout assembler
// output is not a module source and must not be assembled or archived.
func TestGen_RunPython3StdoutNoautoAsmNotCompiled(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "builtin/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PYTHON3(
    gen.py archive_asm
    STDOUT_NOAUTO gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "builtin/gen.py", "print('.text')\n")
	writeTestModuleFile(files, "builtin/builtin.cpp", "int builtin(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(builtin)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")
	if n := nodeByOutput(g, "$(B)/builtin/gen.o"); n != nil {
		t.Fatalf("STDOUT_NOAUTO gen.asm must not be assembled, but $(B)/builtin/gen.o exists")
	}

	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")
	if nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a must not contain noauto member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}
}

// RUN_PYTHON3 shares RUN_PROGRAM's auto-output mechanism: ymake.core.conf:4832
// spells STDOUT as ${stdout;output:STDOUT} and OUT as ${hide;output:OUT}, the
// same modifiers RUN_PROGRAM uses, so an auto .asm/.s/.S STDOUT or OUT of a
// RUN_PYTHON3 is equally a module source — it must be assembled and archived.
// Before the fix emitRunPythonForAR filtered with !isCCSourceExt and dropped
// assembler outputs, leaving them registered for include resolution but never
// compiled or archived.
func TestGen_RunPython3AutoStdoutAsmCompiledAndArchived(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "builtin/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PYTHON3(
    gen.py archive_asm
    STDOUT gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "builtin/gen.py", "print('.text')\n")
	writeTestModuleFile(files, "builtin/builtin.cpp", "int builtin(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(builtin)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	// The RUN_PYTHON3 auto STDOUT .asm producer.
	asmProducer := mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")

	// (1) The .asm is compiled to an object by the assembler, depending on the
	// RUN_PYTHON3 producer (so the .asm exists before it runs).
	asmObj := mustNodeByOutput(t, g, "$(B)/builtin/gen.o")
	if !slices.Contains(graphDeps(g, asmObj), asmProducer.UID) {
		t.Fatalf("gen.o deps missing RUN_PYTHON3 producer uid %q: %v", asmProducer.UID, graphDeps(g, asmObj))
	}

	// (2) The object is a member of the module library.
	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")
	if !nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a missing member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}
}
