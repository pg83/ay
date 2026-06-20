package main

import (
	"slices"
	"strings"
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

// A RUN_PROGRAM with no IN, OUT ${BINDIR}/gen.cpp (a build-rooted auto cc-source
// after env expansion) and OUTPUT_INCLUDES naming a generated .pb.h exported by a
// reached PROTO_LIBRARY whose .proto transitively imports a second one. Upstream's
// ${hide;output_include:OUTPUT_INCLUDES} records induced deps on the OUT, surfaced
// on the DOWNSTREAM consumer that recompiles that auto cc-source — not on the PR
// node (which, lacking any IN, lists only the tool). So:
//   - the CC node compiling gen.cpp must carry both the named a.pb.h and the
//     transitively-imported b.pb.h, each exactly once;
//   - its output path must be the plain $(B)/gen/gen.cpp.o (the ${BINDIR}-expanded
//     $(B)/gen/gen.cpp OUT must not be re-rooted under the module dir again);
//   - the PR node producing gen.cpp must NOT carry a.pb.h (OUTPUT_INCLUDES is not
//     a PR input; there is no IN to scan).
// This is the caesar features.gen.cpp class: the generated source's protobuf
// header closure must reach the run-program consumer, while the producer stays
// at the tool only.
func TestGen_RunProgramOutputIncludesPbHReachConsumerNotProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// q: leaf PROTO_LIBRARY, produces q/b.pb.h.
	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// p: PROTO_LIBRARY importing q/b.proto, so p/a.pb.h #includes "q/b.pb.h".
	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nmessage A { q.B b = 1; }\n")

	// gen: RUN_PROGRAM with no IN, ${BINDIR}/gen.cpp OUT, OUTPUT_INCLUDES p/a.pb.h.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/genhdr emit
    OUTPUT_INCLUDES
        p/a.pb.h
    OUT
        ${BINDIR}/gen.cpp
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	const aPbH = "$(B)/p/a.pb.h"
	const bPbH = "$(B)/q/b.pb.h"

	// No node may carry the double-$(B) re-rooted generated source path.
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.Contains(o.string(), "/gen/$(B)/") {
				t.Fatalf("generated source re-rooted under module dir: %q", o.string())
			}
		}
	}

	// The downstream CC compiling the generated gen.cpp.
	var cc *Node
	for _, n := range g.Graph {
		if n.KV.P != pkCC || len(n.Outputs) == 0 {
			continue
		}
		if o := n.Outputs[0].string(); strings.HasPrefix(o, "$(B)/gen/gen.cpp.") &&
			(strings.HasSuffix(o, ".o") || strings.HasSuffix(o, ".pic.o")) {
			cc = n
			break
		}
	}
	if cc == nil {
		t.Fatal("no CC node compiling $(B)/gen/gen.cpp emitted")
	}

	countInput := func(n *Node, want string) int {
		c := 0
		for _, in := range n.flatInputs() {
			if in.string() == want {
				c++
			}
		}
		return c
	}

	for _, want := range []string{aPbH, bPbH} {
		if got := countInput(cc, want); got != 1 {
			t.Fatalf("CC consumer %q lists %q %d times, want exactly 1: %#v",
				cc.Outputs[0].string(), want, got, vfsStrings(cc.flatInputs()))
		}
	}

	// The PR producer of gen.cpp carries no OUTPUT_INCLUDES (no IN to scan).
	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cpp, got %v", pr.KV.P)
	}
	if nodeHasInput(pr, aPbH) {
		t.Fatalf("PR producer must not carry OUTPUT_INCLUDES %q as input: %#v", aPbH, vfsStrings(pr.flatInputs()))
	}
}

// A RUN_PROGRAM with no IN whose only OUT is a HEADER (no auto cc-source OUT, no
// cc-source STDOUT) — the plutonium dsp.yaff.h class. Its OUTPUT_INCLUDES
// (${hide;output_include:...}) closure has no downstream compile to surface on,
// so upstream realizes it on the PRODUCER command node: the full $(S) include
// closure of every OUTPUT_INCLUDES file — the codegen .pb.h's transitive .proto
// import sources (NOT the intermediate $(B) .pb.h) plus its protobuf C closure,
// and a source-tree OUTPUT_INCLUDES header's own C closure. Unrelated proto
// families and other generated headers must stay absent.
func TestGen_RunProgramHeaderOnlyOutputIncludesImportClosureOnProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// q: leaf PROTO_LIBRARY (q/b.pb.h <- q/b.proto).
	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// p: PROTO_LIBRARY importing q/b.proto, so p/a.pb.h #includes "q/b.pb.h".
	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nmessage A { q.B b = 1; }\n")

	// r: unrelated PROTO_LIBRARY, never imported — must not appear.
	writeTestModuleFile(files, "r/ya.make", `PROTO_LIBRARY()
SRCS(c.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "r/c.proto", "syntax = \"proto3\";\npackage r;\nmessage C {}\n")

	// A source-tree OUTPUT_INCLUDES header with its own include.
	writeTestModuleFile(files, "lib/h1.h", "#pragma once\n#include <lib/h2.h>\n")
	writeTestModuleFile(files, "lib/h2.h", "#pragma once\n")

	// gen: RUN_PROGRAM, no IN, header-only OUT gen.yaff.h, OUTPUT_INCLUDES the
	// codegen p/a.pb.h and the source-tree lib/h1.h.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/genhdr emit
    OUTPUT_INCLUDES
        p/a.pb.h
        lib/h1.h
    OUT
        gen.yaff.h
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.yaff.h")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.yaff.h, got %v", pr.KV.P)
	}

	countInput := func(want string) int {
		c := 0
		for _, in := range pr.flatInputs() {
			if in.string() == want {
				c++
			}
		}
		return c
	}

	for _, want := range []string{
		"$(S)/p/a.proto",
		"$(S)/q/b.proto",
		"$(S)/lib/h1.h",
		"$(S)/lib/h2.h",
	} {
		if got := countInput(want); got != 1 {
			t.Fatalf("PR producer lists %q %d times, want exactly 1: %#v",
				want, got, vfsStrings(pr.flatInputs()))
		}
	}

	for _, absent := range []string{
		"$(B)/p/a.pb.h", // intermediate generated header, not a producer input
		"$(B)/q/b.pb.h",
		"$(S)/r/c.proto", // unrelated proto family
		"$(B)/r/c.pb.h",
	} {
		if countInput(absent) != 0 {
			t.Fatalf("PR producer must not carry %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}
}

// A multi-output RUN_PROGRAM produces ONE build node keyed on its MAIN output —
// FindMainElemOrDefault(GetOutput(), 0) picks the first OUT (ymake
// macro_processor.cpp). Every other output becomes an EDT_OutTogether sibling
// whose OutTogetherDependency points at the main output. When a node consumes a
// non-main output, TJSONVisitor::PrepareLeaving adds that OutTogether dependency
// (the main output) to the consumer's inputs (json_visitor.cpp:658-661). So the
// compile of a generated cc-source OUT carries the producer's main output as an
// input even though it is NOT in OUTPUT_INCLUDES. This is the caesar
// features.gen.cpp.pic.o class: OUT lists features.gen.h (main) before
// features.gen.cpp, and the cpp's .o lists features.gen.h.
func TestGen_RunProgramMainOutputSiblingHeaderRidesGeneratedCppConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	// gen: RUN_PROGRAM with no IN; OUT lists gen.h FIRST (the main output) then
	// the compiled sibling gen.cpp.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr emit
    OUT
        ${BINDIR}/gen.h
        ${BINDIR}/gen.cpp
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	const genH = "$(B)/gen/gen.h"

	// The downstream CC compiling the generated gen.cpp.
	var cc *Node
	for _, n := range g.Graph {
		if n.KV.P != pkCC || len(n.Outputs) == 0 {
			continue
		}
		if o := n.Outputs[0].string(); strings.HasPrefix(o, "$(B)/gen/gen.cpp.") &&
			(strings.HasSuffix(o, ".o") || strings.HasSuffix(o, ".pic.o")) {
			cc = n
			break
		}
	}
	if cc == nil {
		t.Fatal("no CC node compiling $(B)/gen/gen.cpp emitted")
	}

	c := 0
	for _, in := range cc.flatInputs() {
		if in.string() == genH {
			c++
		}
	}
	if c != 1 {
		t.Fatalf("CC consumer %q lists main-output sibling %q %d times, want exactly 1: %#v",
			cc.Outputs[0].string(), genH, c, vfsStrings(cc.flatInputs()))
	}

	// The PR producer still emits both outputs, gen.h first (main).
	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cpp, got %v", pr.KV.P)
	}
	if pr.Outputs[0].string() != genH {
		t.Fatalf("PR main output must be %q (first OUT), got %q", genH, pr.Outputs[0].string())
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

// A multi-output RUN_PROGRAM (the transitive_proto "with_transitive_headers"
// wrapper shape): one command emits several generated .pb.h headers as OUT.
// Upstream models the outputs with EDT_OutTogether edges and designates the
// FIRST OUT as the command's "main output" (FindMainElemOrDefault default index
// 0). Depending on ANY non-main output pulls the main output along
// (json_visitor PrepareLeaving's "AlsoBuilt" edge), so a consumer that #includes
// only ONE sibling wrapper still sees the main output as an input. A sibling
// that nobody includes (and is not the main output) must NOT ride.
//
// This is the caesar with_transitive_headers/advm_banner.pb.h class: advm_banner
// is the first OUT (main) and rides onto every consumer of any wrapper, while no
// source #includes it directly.
func TestGen_RunProgramMainOutputRidesWithSiblingOutput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	// wrap: a LIBRARY whose RUN_PROGRAM emits three .pb.h headers. first.pb.h is
	// the first OUT => the command's main output.
	writeTestModuleFile(files, "wrap/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr generate
    OUT
        first.pb.h
        second.pb.h
        third.pb.h
)
END()
`)

	// cons: includes ONLY the non-main sibling second.pb.h.
	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(wrap)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", `#include <wrap/second.pb.h>
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

	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	// The gate compares NORMALIZED graphs, whose inputs are a set
	// (normSortedStrings sort+dedups). A closure leaf rides as a bare member the
	// splice may append more than once, so "exact-once" is a property of the
	// deduped input set, not the raw chunk multiset.
	seen := map[string]int{}
	for _, in := range use.flatInputs() {
		seen[in.string()]++
	}

	const first = "$(B)/wrap/first.pb.h"   // main output: must ride
	const second = "$(B)/wrap/second.pb.h" // directly included
	const third = "$(B)/wrap/third.pb.h"   // unrelated sibling: must NOT ride

	if seen[second] == 0 {
		t.Fatalf("use.cpp.o missing directly-included %q: %#v", second, vfsStrings(use.flatInputs()))
	}
	if seen[first] == 0 {
		t.Fatalf("use.cpp.o missing main output %q (OutTogether): %#v", first, vfsStrings(use.flatInputs()))
	}
	if seen[third] != 0 {
		t.Fatalf("use.cpp.o must not list unrelated sibling %q (got %d): %#v", third, seen[third], vfsStrings(use.flatInputs()))
	}
}

// The sys_const shape: a RUN_PROGRAM with an IN template and two OUTs — a header
// (first OUT = main output) and a generated cc-source sibling. prInputClosure
// walks the cc-source OUT to surface its OUTPUT_INCLUDES, so the OutTogether
// main-output leaf attached to the .cpp would otherwise ride back onto the
// PRODUCER's own input list. A command never inputs its own outputs: the
// producer must list neither gen.h nor gen.cpp among its inputs, while a
// downstream consumer of gen.cpp still gets the main output gen.h.
func TestGen_RunProgramProducerExcludesOwnOutTogetherOutput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	writeTestModuleFile(files, "prod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr generate prod/tmpl.h
    IN prod/tmpl.h
    OUT
        gen.h
        gen.cpp
)
END()
`)
	writeTestModuleFile(files, "prod/tmpl.h", "#pragma once\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(prod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	prod := mustNodeByOutput(t, g, "$(B)/prod/gen.h")

	for _, in := range prod.flatInputs() {
		s := in.string()
		if s == "$(B)/prod/gen.h" || s == "$(B)/prod/gen.cpp" {
			t.Fatalf("RUN_PROGRAM producer must not input its own output %q: %#v", s, vfsStrings(prod.flatInputs()))
		}
	}
}

// A self-consuming RUN_PROGRAM producer (LIBRARY with OUT gen.h gen.cpp, the
// auto cc-source gen.cpp compiled in the producing module) owns its generated
// outputs. Upstream's Node2Module records the producing module on the first
// DFS-leave; post-order processes the producer (a peer of every consumer) —
// compiling gen.cpp.o and leaving the OutTogether main output gen.h with it —
// before any consumer is visited. So gen.h keeps module_dir = producer even
// though an external module include-resolves it. This runs through the -G
// finalize where overrideGeneratedModuleDir applies. (T-41: profile_traits.h /
// all_profiles.h class.)
func TestGen_RunProgramSelfConsumingProducerOwnsGeneratedModuleDir(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr
        gen.h
        gen.cpp
    OUT
        gen.h
        gen.cpp
)
END()
`)

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

	g := testGenDumpGraph(newMemFS(files), "app")

	// The external consumer include-resolves the generated header and claims it;
	// without producer-ownership the override would re-attribute gen.h to "cons".
	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	if got := genH.TargetProperties.ModuleDir; got != "gen" {
		t.Fatalf("generated header module_dir = %q, want %q (producer owns its self-consumed output)", got, "gen")
	}
	// gen.h and gen.cpp ride together on one PR node; both stay with the producer.
	if !slices.Contains(vfsStrings(genH.Outputs), "$(B)/gen/gen.cpp") {
		t.Fatalf("gen.h PR node missing sibling gen.cpp output: %v", vfsStrings(genH.Outputs))
	}
}

// Negative: a header-only RUN_PROGRAM producer (OUT_NOAUTO geninc.inc — nothing
// compiled in the producing module) does NOT self-consume. Upstream's first
// DFS-leave is the external consumer, so the consumer-claim override still
// re-attributes the generated .inc to the including module. This is the LLVM
// `contrib/libs/llvm16/include/*.inc` shape; T-41 must not disturb it.
func TestGen_HeaderOnlyRunProgramKeepsConsumerModuleDir(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	writeTestModuleFile(files, "geninc/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr
        geninc.inc
    OUT_NOAUTO
        geninc.inc
)
END()
`)

	writeTestModuleFile(files, "cons2/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(geninc)
SRCS(use2.cpp)
END()
`)
	writeTestModuleFile(files, "cons2/use2.cpp", `#include <geninc/geninc.inc>
int use2() { return 0; }
`)

	writeTestModuleFile(files, "app2/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons2)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app2/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app2")

	genInc := mustNodeByOutput(t, g, "$(B)/geninc/geninc.inc")
	if got := genInc.TargetProperties.ModuleDir; got != "cons2" {
		t.Fatalf("header-only generated .inc module_dir = %q, want %q (consumer claims)", got, "cons2")
	}
}
