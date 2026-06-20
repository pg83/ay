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
//   - the PR node producing gen.cpp carries the transitive .proto SOURCE closure
//     of its OUTPUT_INCLUDES (gen.cpp is the run's cc-source MAIN output, so the
//     induced includes ride the producer), but NOT the $(B) codegen .pb.h
//     intermediate (reached via the producer dep edge, not a producer input).
// The generated source's protobuf header closure reaches both the run-program
// consumer (its C-scan) and the producer (its OUTPUT_INCLUDES, as $(S) sources).
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

	// The PR producer of gen.cpp (cc-source main output) carries the OUTPUT_INCLUDES
	// $(S) source closure but not the $(B) codegen .pb.h intermediate.
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

// A RUN_PROGRAM with no IN whose MAIN output is a generated C++ translation unit
// (STDOUT out.cpp) and that declares generated OUTPUT_INCLUDES — the
// proto_flat_buf formula_parameters.cpp class. Upstream attaches the
// OUTPUT_INCLUDES induced includes to the main output; because the main output is
// itself a cc-source, the transitive $(S) SOURCE closure of those includes rides
// the PRODUCER command node (the run "needs" them to produce the .cpp), exactly
// like the header-only case. The producer keeps only its generator dep — the
// $(B) codegen intermediate header is reached via that dep edge, never a producer
// input. The downstream compile of the generated .cpp independently carries the
// $(B) header (its OUTPUT_INCLUDES C-scan), and the object is archived.
func TestGen_RunProgramGeneratedCppStdoutOutputIncludesClosureOnProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "mod/gen_tool", "gen_tool")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// q: leaf PROTO_LIBRARY (q/b.pb.h <- q/b.proto).
	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	// dep: PROTO_LIBRARY importing q/b.proto, so dep/dep.pb.h #includes "q/b.pb.h".
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

	// mod: RUN_PROGRAM Cpp, no IN, STDOUT out.cpp (cc-source MAIN output),
	// OUTPUT_INCLUDES the source-tree generated.h and the codegen dep/dep.pb.h.
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

	// The producer carries the transitive $(S) source closure of its
	// OUTPUT_INCLUDES: the codegen .pb.h's .proto import sources and the
	// source-tree header's own closure.
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

	// The producer carries no dep on the proto codegen producer — only its
	// generator binary edge. dep.pb.h's PB producer must not be a dep of the run.
	pbProducer := mustNodeByAnyOutput(t, g, "$(B)/dep/dep.pb.h")
	if slices.Contains(graphDeps(g, pr), pbProducer.UID) {
		t.Fatalf("PR producer must not depend on the dep.pb.h codegen producer %q: %v", pbProducer.UID, graphDeps(g, pr))
	}

	// The downstream compile of the generated out.cpp carries the $(B) header (its
	// OUTPUT_INCLUDES C-scan) and is archived.
	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/out.cpp.o")
	if !nodeHasInput(cppO, "$(B)/dep/dep.pb.h") {
		t.Fatalf("out.cpp.o inputs missing codegen header $(B)/dep/dep.pb.h: %#v", vfsStrings(cppO.flatInputs()))
	}
	archive := findGraphNodeByOutputs(t, g, "$(B)/mod/libmod.a")
	if !nodeHasInput(archive, "$(B)/mod/out.cpp.o") {
		t.Fatalf("libmod.a missing out.cpp.o member: %#v", vfsStrings(archive.flatInputs()))
	}
}

// Guard: a RUN_PROGRAM whose MAIN output is a HEADER (OUT gen.h gen.cpp) with a
// compiled cc-source sibling — the features.gen.h / profile_traits.h class. The
// OUTPUT_INCLUDES ride the header to its consumers, NOT the producer; the
// producer must carry no OUTPUT_INCLUDES source closure.
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

// A RUN_PROGRAM whose IN is a DATA file with no registered include parser
// (formula.in: YAML-like data) and whose OUTs are a generated .cpp/.h pair plus
// OUTPUT_INCLUDES — the ads/bsyeti/libs/features/formula.cpp class. Upstream roots
// the producer's include graph at IN; a data IN contributes no include edges, so
// the producer keeps only the tool + the direct $(S) data input. The generated
// cc-source OUT is NOT self-scanned onto the producer (that would drag the
// OUTPUT_INCLUDES source closure). The downstream compile of the generated .cpp
// still C-scans it and carries the OUTPUT_INCLUDES closure independently.
func TestGen_RunProgramDataInGeneratedCppProducerStaysToolPlusData(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	// An OUTPUT_INCLUDES source header with its own transitive include — the
	// closure that must surface on the CONSUMER (gen.cpp.o) but not the producer.
	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

	// mod: RUN_PROGRAM with a DATA IN (data.yaml has no registered parser),
	// generated gen.cpp/gen.h OUTs, OUTPUT_INCLUDES the source header.
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

// A RUN_PROGRAM with an unparsed data IN that generates ONLY a cc-source (no
// header output) and names a source header in OUTPUT_INCLUDES — the
// contrib/libs/libphonenumber generate_geocoding_data / geocoding_data.cc class.
// With no generated header to route the OUTPUT_INCLUDES through, the generated
// cc-source's include closure surfaces on the producer's own self-scan: the
// producer carries the OUTPUT_INCLUDES source closure even though the IN is data
// and the tool declares no induced C++ deps. This is the discriminator against the
// formula.cpp class above, whose generated .h sibling carries the includes off the
// producer.
func TestGen_RunProgramDataInNoHeaderGeneratedCcProducerKeepsClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	// An OUTPUT_INCLUDES source header with its own transitive include — the
	// closure that must ride the producer when the run generates no header.
	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

	// mod: RUN_PROGRAM with a DATA IN, a single generated gen.cc OUT (NO header
	// output), OUTPUT_INCLUDES the source header.
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

	// With no generated header sibling, the OUTPUT_INCLUDES source closure rides the
	// producer (self-scan of the generated gen.cc).
	for _, want := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("data-IN no-header producer inputs missing OUTPUT_INCLUDES closure %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

// Control: a RUN_PROGRAM whose IN is a PARSEABLE C++ template (tmpl.cpp.in) that
// #includes a generated codegen .pb.h — the control_board .{h,cpp}.in class. The
// IN roots the producer's include graph, so the producer must still walk the
// parsed IN and keep the closure the template names: the codegen header's
// transitive .proto sources reached through the IN's includes.
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

	// The parsed IN roots the graph: the producer keeps the IN and the codegen
	// header's transitive .proto source reached through it.
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

// A wrapper RUN_PROGRAM that reads .proto IN files and re-exports their generated
// .pb.h (the ads/caesar/.../with_transitive_headers/advm_banner.pb.h shape):
//   - its own input closure carries the transitive .proto sources but NOT the
//     protobuf WKT .pb.h sibling — the run already roots the proto graph at its IN
//     .proto, so the OUTPUT_INCLUDES walk must not re-synthesize the checked-in
//     .pb.h sibling (which it does, correctly, for a run with no .proto IN);
//   - its producer node is attributed to the profile-like module that names the
//     wrapper header in OUTPUT_INCLUDES, not to the producing module.
// A control RUN_PROGRAM with the SAME OUTPUT_INCLUDES but a .h.in IN (no .proto)
// still carries the WKT .pb.h sibling, guarding the c5549aa control_board path.
func TestGen_WrapperProtoRunProgramDropsWktSiblingAndClaimsConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/transitive_proto", "transitive_proto")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// wkt: a PROTO_LIBRARY whose .proto additionally ships a checked-in .pb.h
	// sibling on disk — the protobuf well-known-type shape (descriptor.proto +
	// committed descriptor.pb.h).
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

	// p: PROTO_LIBRARY importing q/b.proto and the WKT wkt/d.proto, so p/a.pb.h's
	// closure surfaces both .proto sources (and the checked-in wkt/d.pb.h sibling).
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

// A custom RUN_PROGRAM header generated FROM a .proto (the
// yabs/.../generated/sys_const.h shape: `IN sys_const.proto OUT sys_const.h`)
// genuinely #includes the generated `.pb.h` of that proto's imports — the custom
// generator emits `#include "<import>.pb.h"`. We cannot scan the generated body,
// so we model it as upstream does (induced deps propagate through generated
// files): the custom header's parsed-include WINDOW carries the import's
// generated `.pb.h`, whose own closure (here a distinctive transitive sibling)
// rides to every consumer.
//
// A second RUN_PROGRAM (`IN tmpl.cpp OUTPUT_INCLUDES gen/custom.h STDOUT out.cpp`,
// the by_name.cpp shape) must therefore carry the import's `.pb.h` + its closure
// on BOTH its producer node and the generated out.cpp.o — and must NOT introduce
// an unrelated same-basename `.pb.h` variant (the protobuf_old descriptor.pb.h
// over-add). Existing proto-IN safeguards (the wrapper test above) stay intact.
func TestGen_CustomProtoHeaderOutputIncludesRidesGeneratedPbhClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/gen_custom", "gen_custom")
	writeToolProgram(files, "tools/emit", "emit")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	// dproto: leaf PROTO_LIBRARY — its generated d.pb.h is the distinctive
	// transitive closure entry of the import's .pb.h (the YaFF-runtime analog).
	writeTestModuleFile(files, "dproto/ya.make", "PROTO_LIBRARY()\nSRCS(d.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "dproto/d.proto", "syntax = \"proto3\";\npackage dproto;\nmessage D {}\n")

	// eproto: PROTO_LIBRARY importing dproto/d.proto — generated extra.pb.h
	// #includes the generated dproto/d.pb.h.
	writeTestModuleFile(files, "eproto/ya.make", "PROTO_LIBRARY()\nPEERDIR(dproto)\nSRCS(extra.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "eproto/extra.proto",
		"syntax = \"proto3\";\npackage eproto;\nimport \"dproto/d.proto\";\nmessage Extra { dproto.D d = 1; }\n")

	// gen: a custom RUN_PROGRAM header generated FROM a proto that imports
	// eproto/extra.proto — registers $(B)/gen/custom.h as a pkPR codegen header
	// whose window must acquire the import's generated extra.pb.h.
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

	// cons: the producer under test — RUN_PROGRAM IN tmpl.cpp, OUTPUT_INCLUDES the
	// custom header, STDOUT out.cpp (by_name.cpp shape). tmpl.cpp #includes the
	// custom header, the way by_name_tmpl.cpp #includes sys_const.h.
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

	// The import's generated .pb.h and its distinctive transitive sibling ride the
	// producer node AND the generated out.cpp.o, alongside their .proto sources.
	want := []string{
		"$(B)/eproto/extra.pb.h", // direct import's generated header (sys_const.h -> const_options.pb.h)
		"$(B)/dproto/d.pb.h",     // its distinctive transitive sibling (-> the YaFF closure)
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

// A RUN_PROGRAM whose positional args embed an IN/OUT path after a `--flag=`:
// uca900-shaped libmysql table generation. Upstream's args_converter deep-replace
// (FillTypedArgs) roots every relative path listed in IN/OUT that appears as a
// boundary-delimited substring of an arg, so `--in_file=lang_data/ja.txt` becomes
// `--in_file=$(S)/<dir>/lang_data/ja.txt` and `--out_file=tbls.cc` becomes
// `--out_file=$(B)/<dir>/tbls.cc`. Bare-token IN/OUT args keep rooting too.
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
