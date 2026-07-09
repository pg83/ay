package main

import (
	"slices"
	"strings"
	"testing"
)

func TestGen_RunProgramGeneratedBinInSourceInputsPropagates(t *testing.T) {
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
RESOURCE(
    third.bin /third.bin
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
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

	if !slices.Contains(graphDeps(g, use), genH.Ref) {
		t.Fatalf("use.cpp.o deps missing generated-header PR ref %d: %v", genH.Ref, graphDeps(g, use))
	}
}

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

	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nmessage A { q.B b = 1; }\n")

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

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		for _, o := range n.Outputs {
			if strings.Contains(o.string(), "/gen/$(B)/") {
				t.Fatalf("generated source re-rooted under module dir: %q", o.string())
			}
		}
	}

	var cc *Node

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

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

	writeTestModuleFile(files, "dep/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(dep.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "dep/dep.proto",
		"syntax = \"proto3\";\npackage dep;\nimport \"q/b.proto\";\nmessage D { q.B b = 1; }\n")

	writeTestModuleFile(files, "mod/generated.h", "#pragma once\n#include <mod/sub.h>\n")
	writeTestModuleFile(files, "mod/sub.h", "#pragma once\n")

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

	for _, absent := range []string{"$(B)/dep/dep.pb.h", "$(B)/q/b.pb.h"} {
		if nodeHasInput(pr, absent) {
			t.Fatalf("PR producer must not carry $(B) codegen header %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}

	pbProducer := mustNodeByAnyOutput(t, g, "$(B)/dep/dep.pb.h")

	if slices.Contains(graphDeps(g, pr), pbProducer.Ref) {
		t.Fatalf("PR producer must not depend on the dep.pb.h codegen producer %q: %v", pbProducer.Ref, graphDeps(g, pr))
	}

	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/out.cpp.o")

	if !nodeHasInput(cppO, "$(B)/dep/dep.pb.h") {
		t.Fatalf("out.cpp.o inputs missing codegen header $(B)/dep/dep.pb.h: %#v", vfsStrings(cppO.flatInputs()))
	}

	archive := findGraphNodeByOutputs(t, g, "$(B)/mod/libmod.a")

	if !nodeHasInput(archive, "$(B)/mod/out.cpp.o") {
		t.Fatalf("libmod.a missing out.cpp.o member: %#v", vfsStrings(archive.flatInputs()))
	}
}

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

func TestGen_RunProgramSelfOwnedProducerKeepsModuleDirOverOutputIncludesClaim(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "gen/gen_tool", "gen_tool")
	writeToolProgram(files, "parent/par_tool", "par_tool")

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

	g := testGenDumpGraph(newMemFS(files), "app")

	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.h")

	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.h, got %v", pr.KV.P)
	}
}

func TestGen_RunProgramToolInducedHeaderDepsRideProducer(t *testing.T) {
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
	writeTestModuleFile(files, "lib/dep.h", "#pragma once\n#include <lib/transitive.h>\n")
	writeTestModuleFile(files, "lib/transitive.h", "#pragma once\n")

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
	writeTestModuleFile(files, "app/main.cpp", "#include \"gen/out.h\"\nint main(){return 0;}\n")

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

func TestGen_RunProgramDataInGeneratedCppProducerStaysToolPlusData(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

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

	wantInputs := []string{toolBin, "$(S)/mod/data.yaml"}
	got := vfsStrings(pr.flatInputs())
	slices.Sort(got)
	wantSorted := slices.Clone(wantInputs)
	slices.Sort(wantSorted)

	if !slices.Equal(got, wantSorted) {
		t.Fatalf("data-IN producer inputs = %#v, want exactly %#v", got, wantSorted)
	}

	for _, absent := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if nodeHasInput(pr, absent) {
			t.Fatalf("data-IN producer must not carry OUTPUT_INCLUDES source closure %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}

	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/gen.cpp.o")

	for _, want := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if !nodeHasInput(cppO, want) {
			t.Fatalf("gen.cpp.o inputs missing OUTPUT_INCLUDES closure %q: %#v", want, vfsStrings(cppO.flatInputs()))
		}
	}
}

func TestGen_RunProgramDataInNoHeaderGeneratedCcProducerKeepsClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	writeTestModuleFile(files, "util/generic/string.h", "#pragma once\n#include <util/generic/strbuf.h>\n")
	writeTestModuleFile(files, "util/generic/strbuf.h", "#pragma once\n")

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

	for _, want := range []string{"$(S)/util/generic/string.h", "$(S)/util/generic/strbuf.h"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("data-IN no-header producer inputs missing OUTPUT_INCLUDES closure %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

func TestGen_RunProgramParseableInGeneratedHeaderClosureRidesProducer(t *testing.T) {
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

	for _, want := range []string{"$(S)/mod/tmpl.cpp.in", "$(S)/dep/dep.proto"} {
		if !nodeHasInput(pr, want) {
			t.Fatalf("parsed-IN producer inputs missing %q: %#v", want, vfsStrings(pr.flatInputs()))
		}
	}
}

func TestGen_RunProgramPlainStdoutProducerStaysToolOnly(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen_tool", "gen_tool")
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

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
RESOURCE(
    meta.txt /meta.txt
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

	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nmessage A { q.B b = 1; }\n")

	writeTestModuleFile(files, "r/ya.make", `PROTO_LIBRARY()
SRCS(c.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "r/c.proto", "syntax = \"proto3\";\npackage r;\nmessage C {}\n")

	writeTestModuleFile(files, "lib/h1.h", "#pragma once\n#include <lib/h2.h>\n")
	writeTestModuleFile(files, "lib/h2.h", "#pragma once\n")

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
	writeTestModuleFile(files, "app/main.cpp", "#include \"gen/gen.yaff.h\"\nint main(){return 0;}\n")

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
		"$(B)/p/a.pb.h",
		"$(B)/q/b.pb.h",
		"$(S)/r/c.proto",
		"$(B)/r/c.pb.h",
	} {
		if countInput(absent) != 0 {
			t.Fatalf("PR producer must not carry %q: %#v", absent, vfsStrings(pr.flatInputs()))
		}
	}
}

func TestGen_RunProgramMainOutputSiblingHeaderRidesGeneratedCppConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

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

	var cc *Node

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

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

	pr := mustNodeByAnyOutput(t, g, "$(B)/gen/gen.cpp")

	if pr.KV.P != pkPR {
		t.Fatalf("expected PR producer for gen.cpp, got %v", pr.KV.P)
	}

	if pr.Outputs[0].string() != genH {
		t.Fatalf("PR main output must be %q (first OUT), got %q", genH, pr.Outputs[0].string())
	}
}

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

	const genHeader = "$(B)/gen/wk/sub/any.cow.pb.h"
	producer := mustNodeByAnyOutput(t, g, genHeader)

	if !contains(producer.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-custom=$(B)/tools/gen/gen") {
		t.Fatalf("producer plugin arg corrupted: %v", anyStrs(producer.Cmds[0].CmdArgs.flat()))
	}

	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	if !contains(use.Cmds[0].CmdArgs.flat(), "-I$(B)/gen/wk") {
		t.Fatalf("use.cpp.o missing -I$(B)/gen/wk: %v", anyStrs(use.Cmds[0].CmdArgs.flat()))
	}

	if !nodeHasInput(use, genHeader) {
		t.Fatalf("use.cpp.o inputs missing %q: %#v", genHeader, use.flatInputs())
	}

	if !slices.Contains(graphDeps(g, use), producer.Ref) {
		t.Fatalf("use.cpp.o deps missing producer ref %d: %v", producer.Ref, graphDeps(g, use))
	}
}

func TestGen_RunProgramAutoStdoutAsmCompiledAndArchived(t *testing.T) {
	files := map[string]string{}

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

	writeToolProgram(files, "contrib/tools/yasm", "yasm")

	g := testGen(newMemFS(files), "app")

	asmProducer := mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")

	asmObj := mustNodeByOutput(t, g, "$(B)/builtin/gen.o")

	if !slices.Contains(graphDeps(g, asmObj), asmProducer.Ref) {
		t.Fatalf("gen.o deps missing RUN_PROGRAM producer ref %d: %v", asmProducer.Ref, graphDeps(g, asmObj))
	}

	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")

	if !nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a missing member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}

	mustNodeByAnyOutput(t, g, "$(B)/tools/dumper/dumper")
	mustNodeByOutput(t, g, "$(B)/cookie/libcookie.a")
}

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
RESOURCE(
    gen.asm /gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
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
RESOURCE(
    gen.asm /gen.asm
)
SRCS(builtin.cpp)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
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

	writeToolProgram(files, "contrib/tools/yasm", "yasm")

	g := testGen(newMemFS(files), "app")

	asmProducer := mustNodeByAnyOutput(t, g, "$(B)/builtin/gen.asm")

	asmObj := mustNodeByOutput(t, g, "$(B)/builtin/gen.o")

	if !slices.Contains(graphDeps(g, asmObj), asmProducer.Ref) {
		t.Fatalf("gen.o deps missing RUN_PYTHON3 producer ref %d: %v", asmProducer.Ref, graphDeps(g, asmObj))
	}

	lib := mustNodeByOutput(t, g, "$(B)/builtin/libbuiltin.a")

	if !nodeHasInput(lib, "$(B)/builtin/gen.o") {
		t.Fatalf("libbuiltin.a missing member $(B)/builtin/gen.o: %#v", lib.flatInputs())
	}
}

func TestGen_RunProgramMainOutputRidesWithSiblingOutput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

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

	seen := map[string]int{}

	for _, in := range use.flatInputs() {
		seen[in.string()]++
	}

	const first = "$(B)/wrap/first.pb.h"
	const second = "$(B)/wrap/second.pb.h"
	const third = "$(B)/wrap/third.pb.h"

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

	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")

	if !slices.Contains(vfsStrings(genH.Outputs), "$(B)/gen/gen.cpp") {
		t.Fatalf("gen.h PR node missing sibling gen.cpp output: %v", vfsStrings(genH.Outputs))
	}
}

func TestGen_WrapperProtoRunProgramDropsWktSiblingAndClaimsConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/transitive_proto", "transitive_proto")
	writeToolProgram(files, "tools/genhdr", "genhdr")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "wkt/ya.make", `PROTO_LIBRARY()
SRCS(d.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "wkt/d.proto", "syntax = \"proto3\";\npackage wkt;\nmessage D {}\n")
	writeTestModuleFile(files, "wkt/d.pb.h", "#pragma once\n")

	writeTestModuleFile(files, "q/ya.make", `PROTO_LIBRARY()
SRCS(b.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "q/b.proto", "syntax = \"proto3\";\npackage q;\nmessage B {}\n")

	writeTestModuleFile(files, "p/ya.make", `PROTO_LIBRARY()
PEERDIR(q wkt)
SRCS(a.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "p/a.proto",
		"syntax = \"proto3\";\npackage p;\nimport \"q/b.proto\";\nimport \"wkt/d.proto\";\nmessage A { q.B b = 1; wkt.D d = 2; }\n")

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
	writeTestModuleFile(files, "app/main.cpp", "#include \"prof/prof.yaff.h\"\n#include \"ctl/ctl.h\"\nint main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	wrap := mustNodeByOutput(t, g, "$(B)/wrap/wrap.pb.h")

	if wrap.KV.P != pkPR {
		t.Fatalf("expected PR producer for wrap.pb.h, got %v", wrap.KV.P)
	}

	for _, want := range []string{"$(S)/p/a.proto", "$(S)/q/b.proto", "$(S)/wkt/d.proto"} {
		if !nodeHasInput(wrap, want) {
			t.Fatalf("wrapper producer inputs missing %q: %#v", want, vfsStrings(wrap.flatInputs()))
		}
	}

	if nodeHasInput(wrap, "$(S)/wkt/d.pb.h") {
		t.Fatalf("wrapper producer (.proto IN) must NOT carry WKT .pb.h sibling: %#v", vfsStrings(wrap.flatInputs()))
	}

	ctl := mustNodeByOutput(t, g, "$(B)/ctl/ctl.h")

	if !nodeHasInput(ctl, "$(S)/wkt/d.pb.h") {
		t.Fatalf("control producer (.h.in IN) must carry WKT .pb.h sibling: %#v", vfsStrings(ctl.flatInputs()))
	}
}

func TestGen_CustomProtoHeaderOutputIncludesRidesGeneratedPbhClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/gen_custom", "gen_custom")
	writeToolProgram(files, "tools/emit", "emit")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "dproto/ya.make", "PROTO_LIBRARY()\nSRCS(d.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "dproto/d.proto", "syntax = \"proto3\";\npackage dproto;\nmessage D {}\n")

	writeTestModuleFile(files, "eproto/ya.make", "PROTO_LIBRARY()\nPEERDIR(dproto)\nSRCS(extra.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "eproto/extra.proto",
		"syntax = \"proto3\";\npackage eproto;\nimport \"dproto/d.proto\";\nmessage Extra { dproto.D d = 1; }\n")

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

	want := []string{
		"$(B)/eproto/extra.pb.h",
		"$(B)/dproto/d.pb.h",
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

func TestGen_RunProgramGeneratedCppRidesHeaderSiblingPbhClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/gen_sys", "gen_sys")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")

	writeTestModuleFile(files, "dproto/ya.make", "PROTO_LIBRARY()\nSRCS(d.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "dproto/d.proto", "syntax = \"proto3\";\npackage dproto;\nmessage D {}\n")

	writeTestModuleFile(files, "eproto/ya.make", "PROTO_LIBRARY()\nPEERDIR(dproto)\nSRCS(extra.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "eproto/extra.proto",
		"syntax = \"proto3\";\npackage eproto;\nimport \"dproto/d.proto\";\nmessage Extra { dproto.D d = 1; }\n")

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
		"$(B)/eproto/extra.pb.h",
		"$(B)/dproto/d.pb.h",
		"$(B)/gen/gen.h",
	}

	for _, suffix := range []string{".cpp.o", ".cpp.pic.o"} {
		obj := mustNodeByOutput(t, g, "$(B)/gen/gen"+suffix)

		for _, w := range want {
			if !nodeHasInput(obj, w) {
				t.Fatalf("gen%s missing %q: %#v", suffix, w, vfsStrings(obj.flatInputs()))
			}
		}

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

	prod := mustNodeByOutput(t, g, "$(B)/gen/gen.h")

	for _, leak := range []string{"$(B)/eproto/extra.pb.h", "$(B)/dproto/d.pb.h"} {
		if nodeHasInput(prod, leak) {
			t.Fatalf("PR producer leaked header-sibling closure %q: %#v", leak, vfsStrings(prod.flatInputs()))
		}
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

	gotArgs := anyStrs(pr.Cmds[0].CmdArgs.flat())
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

func TestGen_RunProgramBuiltToolDepIdentityTracksClosure(t *testing.T) {
	build := func(toolHeader string) *Graph {
		files := map[string]string{}

		writeTestModuleFile(files, "tools/genhdr/ya.make", `PROGRAM(genhdr)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
		writeTestModuleFile(files, "tools/genhdr/main.cpp", "#include \"tool.h\"\nint main(){return 0;}\n")
		writeTestModuleFile(files, "tools/genhdr/tool.h", toolHeader)

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
		writeTestModuleFile(files, "app/main.cpp", "#include \"gen/gen.h\"\nint main(){return 0;}\n")

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

	foreign1 := graphForeignDeps(g1, prod1)

	if !slices.Contains(foreign1, toolLD1.Ref) {
		t.Fatalf("producer %q foreign (tool) deps %v missing built-tool LD ref %d",
			producerOut, foreign1, toolLD1.Ref)
	}

	if !nodeHasInput(prod1, sourceArgInput) {
		t.Fatalf("producer %q inputs %v missing source arg %q",
			producerOut, prod1.flatInputs(), sourceArgInput)
	}

	for _, fd := range foreign1 {
		if fd == toolLD1.Ref {
			continue
		}

		t.Fatalf("producer %q has unexpected extra foreign dep %q (source arg must not be a tool dep)",
			producerOut, fd)
	}
}

func TestGen_RunProgramChainDeclarationOrderIndependent(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/gp ${BINDIR}/first.bin second.bin
    IN
        ${BINDIR}/first.bin
    OUT_NOAUTO second.bin
)
RUN_PROGRAM(
    tools/gp root.dat first.bin
    IN
        root.dat
    OUT_NOAUTO first.bin
)
RESOURCE(
    second.bin /second.bin
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
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
	producer := mustNodeByOutput(t, g, "$(B)/gen/first.bin")
	consumer := mustNodeByOutput(t, g, "$(B)/gen/second.bin")

	if !slices.Contains(graphDeps(g, consumer), producer.Ref) {
		t.Fatalf("second.bin deps missing first.bin producer ref %d: %v", producer.Ref, graphDeps(g, consumer))
	}

	if !nodeHasInput(consumer, "$(S)/gen/root.dat") {
		t.Fatalf("second.bin inputs missing propagated $(S)/gen/root.dat: %#v", consumer.flatInputs())
	}
}

func TestGen_RunProgramConsumesRunPython3Output(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PYTHON3(
    gen.py
    STDOUT first.txt
)
RUN_PROGRAM(
    tools/gp ${BINDIR}/first.txt second.bin
    IN
        ${BINDIR}/first.txt
    OUT_NOAUTO second.bin
)
RESOURCE(
    second.bin /second.bin
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "gen/gen.py", "print('payload')\n")

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
	producer := mustNodeByAnyOutput(t, g, "$(B)/gen/first.txt")
	consumer := mustNodeByOutput(t, g, "$(B)/gen/second.bin")

	if !slices.Contains(graphDeps(g, consumer), producer.Ref) {
		t.Fatalf("second.bin deps missing RUN_PYTHON3 producer ref %d: %v", producer.Ref, graphDeps(g, consumer))
	}
}

func TestGen_RunCodegenMixedChainWorstDeclarationOrder(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/gp ${BINDIR}/second.bin third.bin
    IN
        ${BINDIR}/second.bin
    OUT_NOAUTO third.bin
)
RUN_PROGRAM(
    tools/gp ${BINDIR}/first.txt second.bin
    IN
        ${BINDIR}/first.txt
    OUT_NOAUTO second.bin
)
RUN_PYTHON3(
    gen.py
    STDOUT first.txt
)
RESOURCE(
    third.bin /third.bin
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "gen/gen.py", "print('payload')\n")

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
	first := mustNodeByAnyOutput(t, g, "$(B)/gen/first.txt")
	second := mustNodeByOutput(t, g, "$(B)/gen/second.bin")
	third := mustNodeByOutput(t, g, "$(B)/gen/third.bin")

	if !slices.Contains(graphDeps(g, second), first.Ref) {
		t.Fatalf("second.bin deps missing first.txt producer ref %d: %v", first.Ref, graphDeps(g, second))
	}

	if !slices.Contains(graphDeps(g, third), second.Ref) {
		t.Fatalf("third.bin deps missing second.bin producer ref %d: %v", second.Ref, graphDeps(g, third))
	}
}

func TestGen_RunProgramConsumesProtoGeneratedHeader(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(msg.proto)
RUN_PROGRAM(
    tools/gp ${BINDIR}/msg.pb.h out.txt
    IN ${BINDIR}/msg.pb.h
    OUT_NOAUTO out.txt
)
RESOURCE(
    out.txt /out.txt
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "mod/msg.proto", "syntax = \"proto3\";\npackage mod;\nmessage M {}\n")

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
	pbH := mustNodeByAnyOutput(t, g, "$(B)/mod/msg.pb.h")
	consumer := mustNodeByOutput(t, g, "$(B)/mod/out.txt")

	if !slices.Contains(graphDeps(g, consumer), pbH.Ref) {
		t.Fatalf("out.txt deps missing proto producer ref %d: %v", pbH.Ref, graphDeps(g, consumer))
	}
}

func TestGen_RunProgramConsumesEnumSerializedSource(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(real.cpp)
GENERATE_ENUM_SERIALIZATION(myenum.h)
RUN_PROGRAM(
    tools/gp ${BINDIR}/myenum.h_serialized.cpp out.txt
    IN ${BINDIR}/myenum.h_serialized.cpp
    OUT_NOAUTO out.txt
)
RESOURCE(
    out.txt /out.txt
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "mod/myenum.h", "enum class E { A = 0 };\n")
	writeTestModuleFile(files, "mod/real.cpp", "int real(){return 0;}\n")

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
	en := mustNodeByAnyOutput(t, g, "$(B)/mod/myenum.h_serialized.cpp")
	consumer := mustNodeByOutput(t, g, "$(B)/mod/out.txt")

	if !slices.Contains(graphDeps(g, consumer), en.Ref) {
		t.Fatalf("out.txt deps missing enum-serialization producer ref %d: %v", en.Ref, graphDeps(g, consumer))
	}
}

func TestGen_RunProgramConsumesArchiveOutput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/gp", "gp")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(real.cpp)
ARCHIVE(
    NAME data.inc
    payload.lst
)
RUN_PROGRAM(
    tools/gp ${BINDIR}/data.inc out.txt
    IN ${BINDIR}/data.inc
    OUT_NOAUTO out.txt
)
RESOURCE(
    out.txt /out.txt
)
END()
`)
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeTestModuleFile(files, "mod/payload.lst", "payload\n")
	writeTestModuleFile(files, "mod/real.cpp", "int real(){return 0;}\n")

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
	arc := mustNodeByAnyOutput(t, g, "$(B)/mod/data.inc")
	consumer := mustNodeByOutput(t, g, "$(B)/mod/out.txt")

	if !slices.Contains(graphDeps(g, consumer), arc.Ref) {
		t.Fatalf("out.txt deps missing archive producer ref %d: %v", arc.Ref, graphDeps(g, consumer))
	}
}
