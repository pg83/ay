package main

import (
	"slices"
	"strings"
	"testing"
)

// Chained RUN_PROGRAMs over generated OUT_NOAUTO `.bin` artifacts. The opaque `.bin`
// IN must carry BOTH the producer's direct source leaves (SourceInputs: root.dat) AND
// its transitive parsed source closure (ProducerSourceClosure: leaf.proto via import).
func TestGen_RunProgramGeneratedBinInProducerSourceClosurePropagates(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/gp root.proto root.dat first.bin
    IN
        root.proto
        root.dat
    OUT_NOAUTO first.bin
)
RUN_PROGRAM(
    tools/gp ${BINDIR}/first.bin second.bin
    IN
        ${BINDIR}/first.bin
    OUT_NOAUTO second.bin
)
RUN_PROGRAM(
    tools/gp ${BINDIR}/first.bin ${BINDIR}/second.bin third.bin
    IN
        ${BINDIR}/first.bin
        ${BINDIR}/second.bin
    OUT_NOAUTO third.bin
)
END()
`)
	writeTestModuleFile(files, "gen/root.proto",
		"syntax = \"proto3\";\npackage gen;\nimport \"gen/leaf.proto\";\nmessage R { gen.L l = 1; }\n")
	writeTestModuleFile(files, "gen/leaf.proto",
		"syntax = \"proto3\";\npackage gen;\nmessage L {}\n")
	writeTestModuleFile(files, "gen/root.dat", "opaque\n")

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

	// leaf.proto rides via ProducerSourceClosure (transitive import); root.dat via
	// SourceInputs (unparsed direct IN). Both must reach each downstream consumer.
	for _, out := range []string{"$(B)/gen/second.bin", "$(B)/gen/third.bin"} {
		node := mustNodeByOutput(t, g, out)
		for _, want := range []string{
			"$(S)/gen/root.proto",
			"$(S)/gen/leaf.proto",
			"$(S)/gen/root.dat",
		} {
			if !nodeHasInput(node, want) {
				t.Fatalf("%s inputs missing %q: %#v", out, want, node.flatInputs())
			}
		}
	}
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

// A RUN_PROGRAM with no IN, OUT ${BINDIR}/gen.cpp and OUTPUT_INCLUDES naming a
// generated .pb.h whose .proto transitively imports a second. The induced deps surface
// on the DOWNSTREAM consumer recompiling the cc-source: the CC node carries a.pb.h and
// b.pb.h once each, its output is the plain $(B)/gen/gen.cpp.o, and the PR node carries
// the .proto SOURCE closure but NOT the $(B) codegen .pb.h intermediate.
func TestGen_RunProgramOutputIncludesPbHReachConsumerNotProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// p imports q/b.proto, so p/a.pb.h #includes "q/b.pb.h".
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

	// The PR producer carries the OUTPUT_INCLUDES $(S) source closure but not the $(B)
	// codegen .pb.h intermediate.
	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cpp, got %v", pr.KV.P)
	}
	if nodeHasInput(pr, aPbH) {
		t.Fatalf("PR producer must not carry $(B) codegen header %q as input: %#v", aPbH, vfsStrings(pr.flatInputs()))
	}
	for _, want := range []string{"$(S)/p/a.proto", "$(S)/q/b.proto"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("PR producer inputs missing OUTPUT_INCLUDES source %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

// A RUN_PROGRAM with no IN whose MAIN output is a generated C++ TU (STDOUT out.cpp)
// declaring generated OUTPUT_INCLUDES. The cc-source main output makes the includes'
// $(S) SOURCE closure ride the PRODUCER (never the $(B) intermediate header, reached
// via the dep edge); the downstream compile independently carries the $(B) header.
func TestGen_RunProgramGeneratedCppStdoutOutputIncludesClosureOnProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "mod/gen_tool", "gen_tool")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// dep imports q/b.proto, so dep/dep.pb.h #includes "q/b.pb.h".
	writeTestModuleFile(files, "dep/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(dep.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "dep/dep.proto",
		"syntax = \"proto3\";\npackage dep;\nimport \"q/b.proto\";\nmessage D { q.B b = 1; }\n")

	// A source-tree OUTPUT_INCLUDES header with its own transitive include.
	writeTestModuleFile(files, "mod/generated.h", "#pragma once\n#include <mod/sub.h>\n")
	writeTestModuleFile(files, "mod/sub.h", "#pragma once\n")

	// mod: STDOUT out.cpp (cc-source MAIN output), OUTPUT_INCLUDES a source-tree
	// header and the codegen dep/dep.pb.h.
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    mod/gen_tool Cpp
    OUTPUT_INCLUDES
        mod/generated.h
        dep/dep.pb.h
    STDOUT
        out.cpp
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/mod/out.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for out.cpp, got %v", pr.KV.P)
	}

	// The producer carries the transitive $(S) source closure of its OUTPUT_INCLUDES.
	for _, want := range []string{
		"$(S)/dep/dep.proto",
		"$(S)/q/b.proto",
		"$(S)/mod/generated.h",
		"$(S)/mod/sub.h",
	} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("PR producer inputs missing %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}

	// The $(B) codegen intermediate is reached via the producer dep edge, never a
	// producer input.
	for _, absent := range []string{"$(B)/dep/dep.pb.h", "$(B)/q/b.pb.h"} {
		if nodeHasInput(pr, absent) {
			t.Fatalf("PR producer must not carry $(B) codegen header %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}

	// The producer carries no dep on the proto codegen producer — only its generator edge.
	pbProducer := mustNodeByAnyOutput(t, g, "$(B)/dep/dep.pb.h")
	if slices.Contains(graphDeps(g, pr), pbProducer.UID) {
		t.Fatalf("PR producer must not depend on the dep.pb.h codegen producer %q: %v", pbProducer.UID, graphDeps(g, pr))
	}

	// The downstream compile of out.cpp carries the $(B) header and is archived.
	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/out.cpp.o")
	if !nodeHasInput(cppO, "$(B)/dep/dep.pb.h") {
		t.Fatalf("out.cpp.o inputs missing codegen header $(B)/dep/dep.pb.h: %#v", vfsStrings(cppO.flatInputs()))
	}
	archive := findGraphNodeByOutputs(t, g, "$(B)/mod/libmod.a")
	if !nodeHasInput(archive, "$(B)/mod/out.cpp.o") {
		t.Fatalf("libmod.a missing out.cpp.o member: %#v", vfsStrings(archive.flatInputs()))
	}
}

// Guard: a RUN_PROGRAM whose MAIN output is a HEADER with a compiled cc-source
// sibling. The OUTPUT_INCLUDES ride the header to its consumers, NOT the producer.
func TestGen_RunProgramHeaderMainOutputKeepsClosureOffProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "mod/gen_tool", "gen_tool")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "dep/ya.make", `PROTO_LIBRARY()
SRCS(dep.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "dep/dep.proto", "syntax = \"proto3\";\npackage dep;\nmessage D {}\n")

	// MAIN output is the header gen.h; gen.cpp is a compiled sibling.
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    mod/gen_tool emit
    OUTPUT_INCLUDES
        dep/dep.pb.h
    OUT
        gen.h
        gen.cpp
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/mod/gen.h")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.h, got %v", pr.KV.P)
	}
	for _, absent := range []string{"$(S)/dep/dep.proto", "$(B)/dep/dep.pb.h"} {
		if nodeHasInput(pr, absent) {
			t.Fatalf("header-main producer must not carry OUTPUT_INCLUDES closure %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}
}

// A self-consuming RUN_PROGRAM in a nested submodule (gen.cpp #includes the auto
// compiled gen.h) is the first DFS-leaver of its own outputs, so Node2Module keeps the
// header attributed to `gen`. An external OUTPUT_INCLUDES node-claim from a parent must
// NOT pre-empt the self-owned producer.
func TestGen_RunProgramSelfOwnedProducerKeepsModuleDirOverOutputIncludesClaim(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "gen/gen_tool", "gen_tool")
	writeToolProgram(files, "parent/par_tool", "par_tool")

	// gen: self-consuming RUN_PROGRAM — OUT gen.h gen.cpp, gen.cpp includes gen.h.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    gen/gen_tool emit
    OUT
        gen.h
        gen.cpp
)
END()
`)

	// parent: a different self-consuming RUN_PROGRAM that names gen/gen.h in
	// OUTPUT_INCLUDES, and PEERDIRs the producer.
	writeTestModuleFile(files, "parent/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
RUN_PROGRAM(
    parent/par_tool tmpl.cpp
    IN
        tmpl.cpp
    OUTPUT_INCLUDES
        gen/gen.h
    STDOUT
        par.cpp
)
END()
`)
	writeTestModuleFile(files, "parent/tmpl.cpp", "#pragma once\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(parent)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	// The module_dir attribution override runs only in the -G dump-graph finalize.
	g := testGenDumpGraph(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.h")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.h, got %v", pr.KV.P)
	}
	if got := pr.TargetProperties.ModuleDir; got != "gen" {
		t.Fatalf("self-owned producer module_dir = %q, want %q (producer keeps it over the parent's OUTPUT_INCLUDES claim)", got, "gen")
	}
}

// A RUN_PROGRAM with no IN whose generator tool declares INDUCED_DEPS(h …). The tool's
// header induced deps (and their transitive $(S) closure) are inputs of the generated
// header's own PRODUCER node — both the direct induced header and a transitive one.
func TestGen_RunProgramToolInducedHeaderDepsRideProducer(t *testing.T) {
	files := map[string]string{}

	// The codegen tool declares header induced deps; dep.h pulls a transitive header.
	writeTestModuleFile(files, "tools/codegen/ya.make", `PROGRAM(codegen)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(h
    ${ARCADIA_ROOT}/lib/dep.h
)
END()
`)
	writeTestModuleFile(files, "tools/codegen/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "lib/dep.h", "#pragma once\n#include <lib/transitive.h>\n")
	writeTestModuleFile(files, "lib/transitive.h", "#pragma once\n")

	// gen: RUN_PROGRAM with no IN, OUT out.h (a header), generated by the tool.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/codegen out.h
    OUT
        out.h
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

	pr := mustNodeByOutput(t, g, "$(B)/gen/out.h")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for out.h, got %v", pr.KV.P)
	}
	for _, want := range []string{"$(S)/lib/dep.h", "$(S)/lib/transitive.h"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("header producer missing tool induced-dep closure %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

// Guard the output-kind sensitivity: a RUN_PROGRAM whose tool declares ONLY a header
// induced bucket but produces a cc-source OUT must NOT fold that bucket onto its
// producer (resolveInducedDeps selects the Cpp bucket for a .cpp output).
func TestGen_RunProgramHeaderInducedBucketDoesNotRideCppProducer(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "tools/codegen/ya.make", `PROGRAM(codegen)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(h
    ${ARCADIA_ROOT}/lib/dep.h
)
END()
`)
	writeTestModuleFile(files, "tools/codegen/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "lib/dep.h", "#pragma once\n")

	// gen: a cc-source OUT (out.cpp) — the header induced bucket must not ride it.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/codegen out.cpp
    OUT
        out.cpp
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

	pr := mustNodeByOutput(t, g, "$(B)/gen/out.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for out.cpp, got %v", pr.KV.P)
	}
	if nodeHasInput(pr, "$(S)/lib/dep.h") {
		t.Fatalf("cc-source producer must not inherit header-only induced bucket: %#v", vfsStrings(pr.flatInputs()))
	}
}

// A RUN_PROGRAM with a DATA IN (no parser) and a generated .cpp/.h pair plus
// OUTPUT_INCLUDES. A data IN contributes no include edges, so the producer keeps only
// the tool + the direct $(S) data input; the generated cc-source is NOT self-scanned
// onto it. The downstream compile carries the OUTPUT_INCLUDES closure independently.
func TestGen_RunProgramDataInGeneratedCppProducerStaysToolPlusData(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	// An OUTPUT_INCLUDES source header with a transitive include — must surface on
	// the CONSUMER (gen.cpp.o), not the producer.
	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

	// mod: DATA IN, generated gen.cpp/gen.h OUTs, OUTPUT_INCLUDES the source header.
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen_tool --data data.yaml --cpp gen.cpp --header gen.h
    IN
        data.yaml
    OUTPUT_INCLUDES
        util/generic/string.h
    OUT
        gen.cpp
        gen.h
)
END()
`)
	writeTestModuleFile(files, "mod/data.yaml", "key: value\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	const toolBin = "$(B)/mod/gen_tool/gen_tool"

	pr := mustNodeByAnyOutput(t, g, "$(B)/mod/gen.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cpp, got %v", pr.KV.P)
	}

	// The producer keeps only the tool + the direct $(S) data input.
	wantInputs := []string{toolBin, "$(S)/mod/data.yaml"}
	got := vfsStrings(pr.flatInputs())
	slices.Sort(got)
	wantSorted := slices.Clone(wantInputs)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Fatalf("data-IN producer inputs = %#v, want exactly %#v", got, wantSorted)
	}

	// The OUTPUT_INCLUDES source closure must NOT ride the producer.
	for _, absent := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if nodeHasInput(pr, absent) {
			t.Fatalf("data-IN producer must not carry OUTPUT_INCLUDES source closure %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}

	// The downstream compile of the generated gen.cpp independently carries the
	// OUTPUT_INCLUDES source closure.
	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/gen.cpp.o")
	for _, want := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if !nodeHasInput(cppO, want) {
			t.Fatalf("gen.cpp.o inputs missing OUTPUT_INCLUDES closure %q: %#v", want, vfsStrings(cppO.flatInputs()))
		}
	}
}

// A RUN_PROGRAM with an unparsed data IN generating ONLY a cc-source (no header) and
// naming a source header in OUTPUT_INCLUDES. With no generated header to route them,
// the cc-source's include closure surfaces on the producer's own self-scan — the
// discriminator against the generated-header-sibling class above.
func TestGen_RunProgramDataInNoHeaderGeneratedCcProducerKeepsClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	// An OUTPUT_INCLUDES source header with a transitive include — must ride the
	// producer when the run generates no header.
	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

	// mod: DATA IN, a single generated gen.cc OUT (NO header), OUTPUT_INCLUDES the header.
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen_tool --data data.yaml --out gen.cc
    IN
        data.yaml
    OUTPUT_INCLUDES
        util/generic/string.h
    OUT
        gen.cc
)
END()
`)
	writeTestModuleFile(files, "mod/data.yaml", "key: value\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/mod/gen.cc")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cc, got %v", pr.KV.P)
	}

	// With no generated header sibling, the OUTPUT_INCLUDES closure rides the producer.
	for _, want := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("data-IN no-header producer inputs missing OUTPUT_INCLUDES closure %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

// Control: a RUN_PROGRAM whose IN is a PARSEABLE template #including a generated
// codegen .pb.h. The IN roots the producer's include graph, so the producer keeps the
// codegen header's transitive .proto sources reached through the IN's includes.
func TestGen_RunProgramParseableInGeneratedHeaderClosureRidesProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "mod/gen_tool", "gen_tool")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// dep: PROTO_LIBRARY (dep/dep.pb.h <- dep/dep.proto).
	writeTestModuleFile(files, "dep/ya.make", `PROTO_LIBRARY()
SRCS(dep.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "dep/dep.proto", "syntax = \"proto3\";\npackage dep;\nmessage D {}\n")

	// mod: RUN_PROGRAM with a PARSEABLE IN tmpl.cpp.in that #includes the codegen
	// dep/dep.pb.h; STDOUT out.cpp.
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    mod/gen_tool tmpl.cpp.in
    IN
        tmpl.cpp.in
    OUTPUT_INCLUDES
        dep/dep.pb.h
    STDOUT
        out.cpp
)
END()
`)
	writeTestModuleFile(files, "mod/tmpl.cpp.in", "#include <dep/dep.pb.h>\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/mod/out.cpp")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for out.cpp, got %v", pr.KV.P)
	}

	// The parsed IN roots the graph: producer keeps the IN and the import's .proto source.
	for _, want := range []string{"$(S)/mod/tmpl.cpp.in", "$(S)/dep/dep.proto"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("parsed-IN producer inputs missing %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

// Guard: a no-OUTPUT_INCLUDES cc-source STDOUT run, and a non-cc STDOUT run, must
// stay byte-stable — the producer lists only its generator binary.
func TestGen_RunProgramPlainStdoutProducerStaysToolOnly(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen_tool plain
    STDOUT
        plain.cpp
)
RUN_PROGRAM(
    mod/gen_tool meta
    STDOUT_NOAUTO
        meta.txt
)
END()
`)

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	const toolBin = "$(B)/mod/gen_tool/gen_tool"

	plain := mustNodeByAnyOutput(t, g, "$(B)/mod/plain.cpp")
	if got := vfsStrings(plain.flatInputs()); len(got) != 1 || got[0] != toolBin {
		t.Fatalf("plain cc STDOUT producer inputs = %#v, want only %q", got, toolBin)
	}

	meta := mustNodeByAnyOutput(t, g, "$(B)/mod/meta.txt")
	if got := vfsStrings(meta.flatInputs()); len(got) != 1 || got[0] != toolBin {
		t.Fatalf("non-cc STDOUT producer inputs = %#v, want only %q", got, toolBin)
	}
}

// A RUN_PROGRAM with no IN whose only OUT is a HEADER. Its OUTPUT_INCLUDES closure has
// no downstream compile to surface on, so it is realized on the PRODUCER: every
// OUTPUT_INCLUDES file's full $(S) closure (the codegen .pb.h's .proto sources, NOT the
// $(B) .pb.h). Unrelated proto families must stay absent.
func TestGen_RunProgramHeaderOnlyOutputIncludesImportClosureOnProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// p imports q/b.proto, so p/a.pb.h #includes "q/b.pb.h".
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

// A multi-output RUN_PROGRAM keys one node on its MAIN output (first OUT); other
// outputs are OutTogether siblings that pull the main output onto any consumer. So the
// compile of a generated cc-source OUT carries the main output gen.h even though it is
// NOT in OUTPUT_INCLUDES.
func TestGen_RunProgramMainOutputSiblingHeaderRidesGeneratedCppConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	// gen: OUT lists gen.h FIRST (main output) then the compiled sibling gen.cpp.
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

// A RUN_PROGRAM whose OUT and TOOL carry an explicit ${ARCADIA_BUILD_ROOT}/… prefix:
// the TOOL names a built module by its build-root path (root prefix stripped, not
// opened as a source ya.make) and the literal $(B)/tool/binary in the args survives
// unrewritten. The module's GLOBAL build-root ADDINCL propagates to a PEERDIR consumer.
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

	// The literal build-root binary path in the args is preserved, not rewritten.
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

// A RUN_PROGRAM auto STDOUT .asm is a module source: compiled by the assembler and
// archived, like a declared .asm SRC. Only the resulting .o member edge pulls the host
// tool's closure into the program target closure — a .asm registered but never compiled
// would leave it disconnected.
func TestGen_RunProgramAutoStdoutAsmCompiledAndArchived(t *testing.T) {
	files := map[string]string{}

	// The host tool that produces the .asm, with a library peer reached only via the
	// tool closure.
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

	// (3) The tool program — and through it its library peer — becomes reachable from
	// the program target closure.
	mustNodeByAnyOutput(t, g, "$(B)/tools/dumper/dumper")
	mustNodeByOutput(t, g, "$(B)/cookie/libcookie.a")
}

// STDOUT_NOAUTO marks the redirect as NOT a module source (like OUT_NOAUTO): the .asm
// is still a declared producer output but must NOT be assembled or archived.
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

	// The STDOUT_NOAUTO .asm is a declared producer output but is NOT compiled…
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

// RUN_PYTHON3 STDOUT_NOAUTO mirrors RUN_PROGRAM's: the noauto .asm is not a module
// source and must not be assembled or archived.
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

// RUN_PYTHON3 shares RUN_PROGRAM's auto-output mechanism, so an auto .asm STDOUT/OUT is
// equally a module source: it must be assembled and archived, not dropped by an
// isCCSourceExt filter.
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

	// (1) The .asm is compiled to an object, depending on the RUN_PYTHON3 producer.
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

// A multi-output RUN_PROGRAM (wrapper shape) emits several .pb.h as OUT. Depending on
// ANY non-main output pulls the FIRST OUT (main) along, so a consumer #including only
// one sibling still sees the main output; a sibling nobody includes must NOT ride.
func TestGen_RunProgramMainOutputRidesWithSiblingOutput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	// wrap: RUN_PROGRAM emits three .pb.h; first.pb.h (first OUT) is the main output.
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

	// The gate compares NORMALIZED graphs whose inputs are a deduped set, so "exact-once"
	// is a property of the deduped input set, not the raw chunk multiset.
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

// A RUN_PROGRAM with an IN template and two OUTs — a header (main output) and a
// cc-source sibling. The producer must list neither gen.h nor gen.cpp among its inputs
// (a command never inputs its own outputs), while a downstream consumer of gen.cpp
// still gets the main output gen.h.
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

// A self-consuming RUN_PROGRAM producer (gen.cpp compiled in the producing module)
// owns its generated outputs: post-order leaves gen.h with the producer before any
// consumer, so gen.h keeps module_dir = producer even though an external module
// include-resolves it. Runs through the -G finalize where overrideGeneratedModuleDir
// applies.
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

	// The external consumer include-resolves and claims the header; without
	// producer-ownership the override would re-attribute gen.h to "cons".
	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	if got := genH.TargetProperties.ModuleDir; got != "gen" {
		t.Fatalf("generated header module_dir = %q, want %q (producer owns its self-consumed output)", got, "gen")
	}
	// gen.h and gen.cpp ride together on one PR node; both stay with the producer.
	if !slices.Contains(vfsStrings(genH.Outputs), "$(B)/gen/gen.cpp") {
		t.Fatalf("gen.h PR node missing sibling gen.cpp output: %v", vfsStrings(genH.Outputs))
	}
}

// Negative: a header-only RUN_PROGRAM producer (OUT_NOAUTO, nothing compiled) does NOT
// self-consume, so the consumer-claim override still re-attributes the generated .inc
// to the including module.
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

// A wrapper RUN_PROGRAM reading .proto IN files and re-exporting their .pb.h: its input
// closure carries the transitive .proto sources but NOT the WKT .pb.h sibling (the IN
// .proto already roots the proto graph), and its producer is attributed to the
// profile-like OUTPUT_INCLUDES consumer. A control with a .h.in IN keeps the WKT sibling.
func TestGen_WrapperProtoRunProgramDropsWktSiblingAndClaimsConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/transitive_proto", "transitive_proto")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// wkt: a PROTO_LIBRARY shipping a checked-in .pb.h sibling — the WKT shape.
	writeTestModuleFile(files, "wkt/ya.make", `PROTO_LIBRARY()
SRCS(d.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "wkt/d.proto", "syntax = \"proto3\";\npackage wkt;\nmessage D {}\n")
	writeTestModuleFile(files, "wkt/d.pb.h", "#pragma once\n")

	// q: leaf PROTO_LIBRARY.
	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// p imports q/b.proto and the WKT wkt/d.proto, so p/a.pb.h's closure surfaces both.
	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q wkt)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nimport \"wkt/d.proto\";\nmessage A { q.B b = 1; wkt.D d = 2; }\n")

	// wrap: the wrapper — RUN_PROGRAM with a .proto IN re-exporting p/a.pb.h.
	writeTestModuleFile(files, "wrap/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/transitive_proto generate
    IN
        p/a.proto
    OUTPUT_INCLUDES
        p/a.pb.h
    OUT
        wrap.pb.h
)
END()
`)

	// prof: a profile-like consumer naming the wrapper header in OUTPUT_INCLUDES.
	writeTestModuleFile(files, "prof/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(wrap)
RUN_PROGRAM(
    tools/genhdr emit
    OUTPUT_INCLUDES
        wrap/wrap.pb.h
    OUT
        prof.yaff.h
)
END()
`)

	// ctl: the control — same OUTPUT_INCLUDES p/a.pb.h, but a .h.in IN (no .proto).
	writeTestModuleFile(files, "ctl/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/genhdr
        ctl.h.in
        ctl.h
    OUTPUT_INCLUDES
        p/a.pb.h
    IN
        ctl.h.in
    OUT
        ctl.h
)
END()
`)
	writeTestModuleFile(files, "ctl/ctl.h.in", "#pragma once\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(prof ctl)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	wrap := mustNodeByOutput(t, g, "$(B)/wrap/wrap.pb.h")
	if wrap.KV.P != pkPR {
		t.Fatalf("expected PR producer for wrap.pb.h, got %v", wrap.KV.P)
	}

	// Wrapper inputs: transitive .proto sources present, WKT .pb.h sibling absent.
	for _, want := range []string{"$(S)/p/a.proto", "$(S)/q/b.proto", "$(S)/wkt/d.proto"} {
		if !nodeHasInput(wrap, want) {
			t.Fatalf("wrapper producer inputs missing %q: %#v", want, vfsStrings(wrap.flatInputs()))
		}
	}
	if nodeHasInput(wrap, "$(S)/wkt/d.pb.h") {
		t.Fatalf("wrapper producer (.proto IN) must NOT carry WKT .pb.h sibling: %#v", vfsStrings(wrap.flatInputs()))
	}

	// Wrapper producer is attributed to the profile-like consumer, not to wrap.
	if got := wrap.TargetProperties.ModuleDir; got != "prof" {
		t.Fatalf("wrapper producer module_dir = %q, want %q (OUTPUT_INCLUDES consumer claims)", got, "prof")
	}

	// Control (no .proto IN) keeps the WKT .pb.h sibling.
	ctl := mustNodeByOutput(t, g, "$(B)/ctl/ctl.h")
	if !nodeHasInput(ctl, "$(S)/wkt/d.pb.h") {
		t.Fatalf("control producer (.h.in IN) must carry WKT .pb.h sibling: %#v", vfsStrings(ctl.flatInputs()))
	}
}

// A custom RUN_PROGRAM header generated FROM a .proto #includes the generated `.pb.h`
// of that proto's imports. We model this through the custom header's parsed-include
// window, so the import's `.pb.h` and its closure ride to every consumer. A second
// RUN_PROGRAM whose OUTPUT_INCLUDES names the custom header must carry that `.pb.h`
// closure on BOTH its producer and the generated out.cpp.o, without an unrelated
// same-basename variant.
func TestGen_CustomProtoHeaderOutputIncludesRidesGeneratedPbhClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/gen_custom", "gen_custom")
	writeToolProgram(files, "tools/emit", "emit")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// dproto: leaf PROTO_LIBRARY — d.pb.h is the distinctive transitive closure entry.
	writeTestModuleFile(files, "dproto/ya.make", "PROTO_LIBRARY()\nSRCS(d.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "dproto/d.proto", "syntax = \"proto3\";\npackage dproto;\nmessage D {}\n")

	// eproto imports dproto/d.proto — extra.pb.h #includes dproto/d.pb.h.
	writeTestModuleFile(files, "eproto/ya.make", "PROTO_LIBRARY()\nPEERDIR(dproto)\nSRCS(extra.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "eproto/extra.proto",
		"syntax = \"proto3\";\npackage eproto;\nimport \"dproto/d.proto\";\nmessage Extra { dproto.D d = 1; }\n")

	// gen: a custom header generated FROM a proto importing eproto/extra.proto; its
	// window must acquire the import's generated extra.pb.h.
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(eproto)
RUN_PROGRAM(
    tools/gen_custom src.proto custom.h
    IN
        src.proto
    OUT
        custom.h
)
END()
`)
	writeTestModuleFile(files, "gen/src.proto",
		"syntax = \"proto3\";\npackage gen;\nimport \"eproto/extra.proto\";\nmessage S { eproto.Extra e = 1; }\n")

	// cons: the producer under test — IN tmpl.cpp (#includes the custom header),
	// OUTPUT_INCLUDES the custom header, STDOUT out.cpp.
	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
RUN_PROGRAM(
    tools/emit tmpl.cpp
    IN
        tmpl.cpp
    OUTPUT_INCLUDES
        gen/custom.h
    STDOUT
        out.cpp
)
END()
`)
	writeTestModuleFile(files, "cons/tmpl.cpp", "#include \"gen/custom.h\"\nint f(){return 0;}\n")

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

	// The import's .pb.h and its distinctive sibling ride the producer AND out.cpp.o.
	want := []string{
		"$(B)/eproto/extra.pb.h", // direct import's generated header
		"$(B)/dproto/d.pb.h",     // its distinctive transitive sibling
		"$(S)/eproto/extra.proto",
		"$(S)/dproto/d.proto",
	}

	prod := mustNodeByOutput(t, g, "$(B)/cons/out.cpp")
	if prod.KV.P != pkPR {
		t.Fatalf("expected PR producer for out.cpp, got %v", prod.KV.P)
	}
	for _, w := range want {
		if !nodeHasInput(prod, w) {
			t.Fatalf("out.cpp producer missing %q: %#v", w, vfsStrings(prod.flatInputs()))
		}
	}

	obj := mustNodeByOutput(t, g, "$(B)/cons/out.cpp.o")
	for _, w := range want {
		if !nodeHasInput(obj, w) {
			t.Fatalf("out.cpp.o missing %q: %#v", w, vfsStrings(obj.flatInputs()))
		}
	}
}

// A RUN_PROGRAM with IN src.proto, OUT gen.h gen.cpp, whose generated gen.cpp
// `#include "gen.h"`. The header sibling gen.h carries its proto import's `.pb.h`
// closure, so BOTH the non-PIC and PIC compile of gen.cpp must reach that closure (the
// import's `.pb.h` and its distinctive sibling).
func TestGen_RunProgramGeneratedCppRidesHeaderSiblingPbhClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/gen_sys", "gen_sys")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// dproto: leaf PROTO_LIBRARY — d.pb.h is the distinctive transitive closure entry
	// of the import's extra.pb.h.
	writeTestModuleFile(files, "dproto/ya.make", "PROTO_LIBRARY()\nSRCS(d.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "dproto/d.proto", "syntax = \"proto3\";\npackage dproto;\nmessage D {}\n")

	// eproto: PROTO_LIBRARY importing dproto/d.proto — extra.pb.h #includes d.pb.h.
	writeTestModuleFile(files, "eproto/ya.make", "PROTO_LIBRARY()\nPEERDIR(dproto)\nSRCS(extra.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "eproto/extra.proto",
		"syntax = \"proto3\";\npackage eproto;\nimport \"dproto/d.proto\";\nmessage Extra { dproto.D d = 1; }\n")

	// gen: IN src.proto (imports eproto/extra.proto), OUT gen.h gen.cpp; gen.h
	// re-exports eproto/extra.pb.h and gen.cpp #includes "gen.h".
	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(eproto)
RUN_PROGRAM(
    tools/gen_sys src.proto gen.h gen.cpp
    IN
        src.proto
    OUT
        gen.h
        gen.cpp
)
END()
`)
	writeTestModuleFile(files, "gen/src.proto",
		"syntax = \"proto3\";\npackage gen;\nimport \"eproto/extra.proto\";\nmessage S { eproto.Extra e = 1; }\n")

	// tool PROGRAM PEERDIRs gen — a host BASE_CODEGEN tool, so gen is instantiated PIC
	// for the host (gen.cpp.pic.o) besides the non-PIC target (gen.cpp.o).
	writeTestModuleFile(files, "tool/ya.make", `PROGRAM(tool)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(tmain.cpp)
END()
`)
	writeTestModuleFile(files, "tool/tmain.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
BASE_CODEGEN(tool fill_x)
SRCS(
    GLOBAL ${BINDIR}/fill_x.cpp
    main.cpp
)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	want := []string{
		"$(B)/eproto/extra.pb.h", // direct import's generated header
		"$(B)/dproto/d.pb.h",     // its distinctive transitive sibling
		"$(B)/gen/gen.h",         // the same-producer header sibling itself
	}

	for _, suffix := range []string{".cpp.o", ".cpp.pic.o"} {
		obj := mustNodeByOutput(t, g, "$(B)/gen/gen"+suffix)
		for _, w := range want {
			if !nodeHasInput(obj, w) {
				t.Fatalf("gen%s missing %q: %#v", suffix, w, vfsStrings(obj.flatInputs()))
			}
		}

		// gen.h must appear exactly once (no double edge from the main-output leaf
		// on top of the parsed include).
		c := 0
		for _, in := range obj.flatInputs() {
			if in.string() == "$(B)/gen/gen.h" {
				c++
			}
		}
		if c != 1 {
			t.Fatalf("gen%s lists $(B)/gen/gen.h %d times, want exactly 1: %#v",
				suffix, c, vfsStrings(obj.flatInputs()))
		}
	}

	// The PR producer is IN-rooted: it depends on the proto IN closure, NOT the header
	// sibling's re-exported `.pb.h` closure, which routes to consumers only. Modeling
	// gen.cpp's `#include "gen.h"` must not leak gen.h's window onto the producer.
	prod := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	for _, leak := range []string{"$(B)/eproto/extra.pb.h", "$(B)/dproto/d.pb.h"} {
		if nodeHasInput(prod, leak) {
			t.Fatalf("PR producer leaked header-sibling closure %q: %#v", leak, vfsStrings(prod.flatInputs()))
		}
	}
}

// A RUN_PROGRAM in an INCLUDE'd macro with `CWD ${BINDIR}` and `OUT_NOAUTO
// ${OUTPUT_PATH}`, used from a CHILD module whose output is consumed only by the
// PARENT's COPY_FILE, is owned by the PARENT under the first-DFS-leave rule. Both
// module_dir AND the late-resolved ${BINDIR} cwd follow the parent; the OUT path stays
// under the child.
func TestGen_RunProgramIncludedMacroCwdFollowsCopyConsumerOwner(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genconsts", "genconsts")

	writeTestModuleFile(files, "shared/make_generated_consts.inc", `DEFAULT(OUTPUT_PATH "generated_consts.py")
RUN_PROGRAM(
    tools/genconsts
        --save_file_path ${OUTPUT_PATH}
    OUT_NOAUTO
        ${OUTPUT_PATH}
    CWD
        ${BINDIR}
)
`)

	writeTestModuleFile(files, "gen/gen_consts/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
INCLUDE(${ARCADIA_ROOT}/shared/make_generated_consts.inc)
END()
`)

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen/gen_consts)
COPY_FILE(${ARCADIA_BUILD_ROOT}/gen/gen_consts/generated_consts.py generated_consts.py)
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

	g := testGenDumpGraph(newMemFS(files), "app")

	pr := mustNodeByOutput(t, g, "$(B)/gen/gen_consts/generated_consts.py")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer, got %v", pr.KV.P)
	}

	if got := pr.TargetProperties.ModuleDir; got != "gen" {
		t.Fatalf("PR module_dir = %q, want %q (parent COPY_FILE consumer owns the OUT_NOAUTO node)", got, "gen")
	}

	if got := pr.Cmds[0].Cwd.string(); got != "$(B)/gen" {
		t.Fatalf("PR cwd = %q, want %q (${BINDIR} re-resolves against owning parent)", got, "$(B)/gen")
	}

	// The OUT path stays under the producing child, unchanged by the ownership move.
	if !slices.Contains(vfsStrings(pr.Outputs), "$(B)/gen/gen_consts/generated_consts.py") {
		t.Fatalf("PR output path moved off the child dir: %v", vfsStrings(pr.Outputs))
	}

	if got := pr.TargetProperties.ModuleTag; got != 0 {
		t.Fatalf("PR module_tag = %q, want unset (plain LIBRARY owner is not a multimodule)", got.string())
	}
}

func TestGen_RunProgramFlagArgPathsAreRooted(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/libs/libmysql_r/strings/uca9dump", "uca9dump")

	writeTestModuleFile(files, "contrib/libs/libmysql_r/strings/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    contrib/libs/libmysql_r/strings/uca9dump ja --in_file=lang_data/ja_hans.txt --out_file=uca900_ja_tbls.cc
    CWD ${ARCADIA_BUILD_ROOT}/contrib/libs/libmysql_r/strings
    IN lang_data/ja_hans.txt
    OUT uca900_ja_tbls.cc
)
END()
`)
	writeTestModuleFile(files, "contrib/libs/libmysql_r/strings/lang_data/ja_hans.txt", "data\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(contrib/libs/libmysql_r/strings)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")
	pr := mustNodeByOutput(t, g, "$(B)/contrib/libs/libmysql_r/strings/uca900_ja_tbls.cc")
	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer, got %v", pr.KV.P)
	}

	gotArgs := strStrings(pr.Cmds[0].CmdArgs.flat())
	wantIn := "--in_file=$(S)/contrib/libs/libmysql_r/strings/lang_data/ja_hans.txt"
	wantOut := "--out_file=$(B)/contrib/libs/libmysql_r/strings/uca900_ja_tbls.cc"
	if !slices.Contains(gotArgs, wantIn) {
		t.Fatalf("PR args missing rooted --in_file %q; got %v", wantIn, gotArgs)
	}
	if !slices.Contains(gotArgs, wantOut) {
		t.Fatalf("PR args missing rooted --out_file %q; got %v", wantOut, gotArgs)
	}
	if slices.Contains(gotArgs, "ja") == false {
		t.Fatalf("PR args dropped the literal positional %q; got %v", "ja", gotArgs)
	}
}

// A RUN_PROGRAM-produced header records its built PROGRAM tool as a ForeignDepRef
// carrying the tool's LD uid (a Merkle hash over its source/header link closure), so
// the producer re-fires when the tool's binary identity changes. A plain source-file
// argument stays a $(S) input, never promoted to a built-tool dep.
func TestGen_RunProgramBuiltToolDepIdentityTracksClosure(t *testing.T) {
	build := func(toolHeader string) *Graph {
		files := map[string]string{}

		// A built PROGRAM tool whose binary identity depends on its header closure:
		// main.cpp #includes tool.h, so tool.h reaches the tool's LD uid.
		writeTestModuleFile(files, "tools/genhdr/ya.make", `PROGRAM(genhdr)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
		writeTestModuleFile(files, "tools/genhdr/main.cpp", "#include \"tool.h\"\nint main(){return 0;}\n")
		writeTestModuleFile(files, "tools/genhdr/tool.h", toolHeader)

		// A LIBRARY whose RUN_PROGRAM runs the tool with a source-file argument
		// (template.h.in, declared IN) and produces gen.h.
		writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr
        template.h.in
        gen.h
    IN
        template.h.in
    OUT
        gen.h
)
END()
`)
		writeTestModuleFile(files, "gen/template.h.in", "#pragma once\n")

		writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(main.cpp)
END()
`)
		writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

		return testGen(newMemFS(files), "app")
	}

	const toolLDOut = "$(B)/tools/genhdr/genhdr"
	const producerOut = "$(B)/gen/gen.h"
	const sourceArgInput = "$(S)/gen/template.h.in"

	g1 := build("#pragma once\n")
	toolLD1 := mustNodeByOutput(t, g1, toolLDOut)
	prod1 := mustNodeByOutput(t, g1, producerOut)

	if toolLD1.KV.P != pkLD {
		t.Fatalf("tool node %q kv.p = %v, want LD", toolLDOut, toolLD1.KV.P)
	}

	// (1) The producer's tool dep IS the built tool's LD identity.
	foreign1 := graphForeignDeps(g1, prod1)
	if !slices.Contains(foreign1, toolLD1.UID) {
		t.Fatalf("producer %q foreign (tool) deps %v missing built-tool LD uid %q",
			producerOut, foreign1, toolLD1.UID)
	}

	// (2) The source argument is a plain input, NOT promoted to a tool dep.
	if !nodeHasInput(prod1, sourceArgInput) {
		t.Fatalf("producer %q inputs %v missing source arg %q",
			producerOut, prod1.flatInputs(), sourceArgInput)
	}
	for _, fd := range foreign1 {
		if fd == toolLD1.UID {
			continue
		}
		t.Fatalf("producer %q has unexpected extra foreign dep %q (source arg must not be a tool dep)",
			producerOut, fd)
	}

	// (3) Change the tool's header closure: the tool's LD uid must change and the
	// producer's tool dep must follow it, proving it tracks the closure, not the path.
	g2 := build("#pragma once\nint genhdr_marker = 1;\n")
	toolLD2 := mustNodeByOutput(t, g2, toolLDOut)
	prod2 := mustNodeByOutput(t, g2, producerOut)

	if toolLD1.UID == toolLD2.UID {
		t.Fatalf("built-tool LD uid unchanged (%q) after tool.h closure change — tool identity does not track its source/header closure",
			toolLD1.UID)
	}

	foreign2 := graphForeignDeps(g2, prod2)
	if slices.Contains(foreign2, toolLD1.UID) {
		t.Fatalf("producer %q still depends on stale tool identity %q after the tool closure changed",
			producerOut, toolLD1.UID)
	}
	if !slices.Contains(foreign2, toolLD2.UID) {
		t.Fatalf("producer %q foreign deps %v missing new built-tool LD uid %q",
			producerOut, foreign2, toolLD2.UID)
	}

	// (4) The source-only tool argument stays the same plain $(S) input.
	if !nodeHasInput(prod2, sourceArgInput) {
		t.Fatalf("producer %q lost source arg input %q after tool closure change",
			producerOut, sourceArgInput)
	}
}
