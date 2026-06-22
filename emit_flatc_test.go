package main

import (
	"slices"
	"testing"
)

func TestEmitFL_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no")
	instance := ModuleInstance{
		Path:     source("mod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := newBufferedEmitter()
	_, header, cpp, bfbs := emitFL(
		instance,
		"mod/File.fbs",
		intern("$(S)/mod/File.fbs"),
		NodeRef(9),
		intern("$(B)/contrib/libs/flatbuffers/flatc/flatc"),
		internArgs([]string{"--scoped-enums"}),
		[]VFS{intern("$(S)/mod/Schema.fbs")},
		tagCppFbs,
		testToolchain(),
		e,
		&flatcVariantFL,
		nil,
	)

	if header.string() != "$(B)/mod/File.fbs.h" {
		t.Fatalf("header = %q", header)
	}

	if cpp.string() != "$(B)/mod/File.fbs.cpp" {
		t.Fatalf("cpp = %q", cpp)
	}

	if bfbs.string() != "$(B)/mod/File.bfbs" {
		t.Fatalf("bfbs = %q", bfbs)
	}

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	node := e.nodes[0]

	if node.KV.P != pkFL {
		t.Fatalf("kv.p = %q, want FL", node.KV.P)
	}

	if got := node.Cmds[0].CmdArgs.flat(); !contains(got, "--scoped-enums") {
		t.Fatalf("cmd args missing --scoped-enums: %v", got)
	}

	if got := node.Cmds[0].CmdArgs.flat(); got[len(got)-3].string() != "-o" || got[len(got)-2].string() != "$(B)/mod/File.fbs.h" || got[len(got)-1].string() != "$(S)/mod/File.fbs" {
		t.Fatalf("unexpected cmd arg tail: %v", got[len(got)-5:])
	}

	if len(node.ForeignDepRefs) != 1 || node.ForeignDepRefs[0] != 9 {
		t.Fatalf("ForeignDepRefs = %#v, want flatc dep", node.ForeignDepRefs)
	}
}

func TestGen_FlatcSourcesEmitConsumerInputsAndDeps(t *testing.T) {
	files := map[string]string{}

	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `LIBRARY()
FLATC_FLAGS(--scoped-enums)
SRCS(
    File.fbs
    Schema.fbs
    consumer.cpp
)
END()
`)
	mkdirWrite("mod/consumer.cpp", `#include "File.fbs.h"
int consume() { return 0; }
`)
	mkdirWrite("mod/Schema.fbs", `namespace test;
table Foo {
  value:int;
}
`)
	mkdirWrite("mod/File.fbs", `include "Schema.fbs";
namespace test;
table Bar {
  foo:Foo;
}
root_type Bar;
`)
	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/ya.make", "LIBRARY()\nSRCS(fb.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/fb.cpp", "int fb() { return 0; }\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(newMemFS(files), "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.h", "$(B)/mod/File.fbs.cpp", "$(B)/mod/File.bfbs")
	findGraphNodeByOutputs(t, g, "$(B)/mod/Schema.fbs.h", "$(B)/mod/Schema.fbs.cpp", "$(B)/mod/Schema.bfbs")

	fileCC := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.cpp.o")
	wantFileInputs := []string{
		"$(B)/mod/File.fbs.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}

	if got := vfsStringsT3(fileCC.flatInputs()); !vfsInputsContainAll(got, wantFileInputs) {
		t.Fatalf("File.fbs.cpp inputs = %v, want all of %v", got, wantFileInputs)
	}

	if len(graphDeps(g, fileCC)) != 2 {
		t.Fatalf("len(File.fbs.cpp deps) = %d, want 2 (self + imported schema)", len(graphDeps(g, fileCC)))
	}

	consumerCC := findGraphNodeByOutputs(t, g, "$(B)/mod/consumer.cpp.o")
	wantConsumerInputs := []string{
		"$(S)/mod/consumer.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}

	if got := vfsStringsT3(consumerCC.flatInputs()); !vfsInputsContainAll(got, wantConsumerInputs) {
		t.Fatalf("consumer.cpp inputs = %v, want all of %v", got, wantConsumerInputs)
	}

	if len(graphDeps(g, consumerCC)) != 2 {
		t.Fatalf("len(consumer.cpp deps) = %d, want 2 (reachable flatc producers)", len(graphDeps(g, consumerCC)))
	}
}

func TestGen_FbsLibraryCarriesCppFbsModuleTag(t *testing.T) {
	files := map[string]string{}
	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `FBS_LIBRARY()
SRCS(
    File.fbs
)
END()
`)
	mkdirWrite("mod/File.fbs", `namespace test;
table Bar {
  value:int;
}
root_type Bar;
`)
	mkdirWrite("plain/ya.make", `LIBRARY()
SRCS(
    plain.cpp
)
END()
`)
	mkdirWrite("plain/plain.cpp", "int plain() { return 0; }\n")

	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/ya.make", "LIBRARY()\nSRCS(fb.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/fb.cpp", "int fb() { return 0; }\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	fs := newMemFS(files)
	g := testGen(fs, "mod")

	producer := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.h", "$(B)/mod/File.fbs.cpp", "$(B)/mod/File.bfbs")

	if got := producer.TargetProperties.ModuleTag; got != tagCppFbs {
		t.Fatalf("FL producer module_tag = %q, want cpp_fbs", got.string())
	}

	consumer := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.cpp.o")

	if got := consumer.TargetProperties.ModuleTag; got != tagCppFbs {
		t.Fatalf(".fbs.cpp.o consumer module_tag = %q, want cpp_fbs", got.string())
	}

	archive := findGraphNodeByOutputs(t, g, "$(B)/mod/libmod.a")

	if got := archive.TargetProperties.ModuleTag; got != tagCppFbs {
		t.Fatalf("fbs archive module_tag = %q, want cpp_fbs", got.string())
	}

	gp := testGen(fs, "plain")
	plain := findGraphNodeByOutputs(t, gp, "$(B)/plain/plain.cpp.o")

	if got := plain.TargetProperties.ModuleTag; got != 0 {
		t.Fatalf("plain C++ .o module_tag = %q, want empty", got.string())
	}
}

func TestGen_RunProgramFbsStdoutBridgesToFlatc(t *testing.T) {
	files := map[string]string{}
	mkdirWrite := func(rel, body string) { files[rel] = body }

	writeToolProgram(files, "mod/gen_tool", "gen_tool")

	mkdirWrite("mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen_tool Fbs
    STDOUT
        schema.fbs
)
END()
`)

	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/ya.make", "LIBRARY()\nSRCS(fb.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/fb.cpp", "int fb() { return 0; }\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(newMemFS(files), "mod")

	pr := mustNodeByOutput(t, g, "$(B)/mod/schema.fbs")

	if pr.KV.P != pkPR {
		t.Fatalf("schema.fbs producer kv.p = %q, want PR", pr.KV.P)
	}

	fl := findGraphNodeByOutputs(t, g, "$(B)/mod/schema.fbs.h", "$(B)/mod/schema.fbs.cpp", "$(B)/mod/schema.bfbs")

	if fl.KV.P != pkFL {
		t.Fatalf("schema flatc producer kv.p = %q, want FL", fl.KV.P)
	}

	if !nodeHasInput(fl, "$(B)/mod/schema.fbs") {
		t.Fatalf("flatc node inputs missing build-root schema.fbs: %#v", fl.flatInputs())
	}

	if !slices.Contains(graphDeps(g, fl), pr.UID) {
		t.Fatalf("flatc node deps missing PR producer uid %q: %v", pr.UID, graphDeps(g, fl))
	}

	cppO := findGraphNodeByOutputs(t, g, "$(B)/mod/schema.fbs.cpp.o")

	for _, want := range []string{"$(B)/mod/schema.fbs.cpp", "$(B)/mod/schema.fbs.h"} {
		if !nodeHasInput(cppO, want) {
			t.Fatalf("schema.fbs.cpp.o inputs missing %q: %#v", want, cppO.flatInputs())
		}
	}

	if nodeHasInput(cppO, "$(B)/mod/schema.fbs") {
		t.Fatalf("schema.fbs.cpp.o must not carry the build-root schema.fbs as an input: %#v", cppO.flatInputs())
	}

	archive := findGraphNodeByOutputs(t, g, "$(B)/mod/libmod.a")

	if !nodeHasInput(archive, "$(B)/mod/schema.fbs.cpp.o") {
		t.Fatalf("libmod.a missing schema.fbs.cpp.o member: %#v", archive.flatInputs())
	}
}

func TestEmitFL64_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no")
	instance := ModuleInstance{
		Path:     source("mod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := newBufferedEmitter()
	_, header, cpp, bfbs := emitFL(
		instance,
		"mod/File.fbs64",
		intern("$(S)/mod/File.fbs64"),
		NodeRef(9),
		intern("$(B)/contrib/libs/flatbuffers64/flatc/flatc"),
		internArgs([]string{"--scoped-enums"}),
		[]VFS{intern("$(S)/mod/Schema.fbs64")},
		tagCppFbs,
		testToolchain(),
		e,
		&flatcVariantFL64,
		nil,
	)

	if header.string() != "$(B)/mod/File.fbs64.h" {
		t.Fatalf("header = %q", header)
	}

	if cpp.string() != "$(B)/mod/File.fbs64.cpp" {
		t.Fatalf("cpp = %q", cpp)
	}

	if bfbs.string() != "$(B)/mod/File.bfbs64" {
		t.Fatalf("bfbs = %q", bfbs)
	}

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	node := e.nodes[0]

	if node.KV.P != pkFL64 {
		t.Fatalf("kv.p = %q, want FL64", node.KV.P)
	}

	got := node.Cmds[0].CmdArgs.flat()

	if !contains(got, "--filename-suffix") || !contains(got, ".fbs64") {
		t.Fatalf("cmd args missing --filename-suffix .fbs64: %v", got)
	}

	if contains(got, "--gen-object-api") {
		t.Fatalf("FL64 must not pass --gen-object-api: %v", got)
	}

	if !contains(got, "--scoped-enums") {
		t.Fatalf("cmd args missing FLATC_FLAGS --scoped-enums: %v", got)
	}

	tail := got[len(got)-7:]
	want := []string{"-I", "$(S)", "-I", "$(B)", "-o", "$(B)/mod/File.fbs64.h", "$(S)/mod/File.fbs64"}

	for i := range want {
		if tail[i].string() != want[i] {
			gotTail := make([]string, len(tail))

			for j, s := range tail {
				gotTail[j] = s.string()
			}

			t.Fatalf("cmd arg tail = %v, want %v", gotTail, want)
		}
	}

	if len(node.ForeignDepRefs) != 1 || node.ForeignDepRefs[0] != 9 {
		t.Fatalf("ForeignDepRefs = %#v, want flatc64 dep", node.ForeignDepRefs)
	}
}

func TestGen_Flatc64SourcesEmitConsumerInputsAndDeps(t *testing.T) {
	files := map[string]string{}
	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `LIBRARY()
SRCS(
    File.fbs64
    Schema.fbs64
    consumer.cpp
)
END()
`)
	mkdirWrite("mod/consumer.cpp", `#include "File.fbs64.h"
int consume() { return 0; }
`)
	mkdirWrite("mod/Schema.fbs64", `namespace test;
table Foo {
  value:int;
}
`)
	mkdirWrite("mod/File.fbs64", `include "Schema.fbs64";
namespace test;
table Bar {
  foo:Foo;
}
root_type Bar;
`)
	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers64/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers64/ya.make", "LIBRARY()\nSRCS(fb.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers64/fb.cpp", "int fb() { return 0; }\n")
	mkdirWrite("contrib/libs/flatbuffers64/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers64/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(newMemFS(files), "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs64.h", "$(B)/mod/File.fbs64.cpp", "$(B)/mod/File.bfbs64")
	findGraphNodeByOutputs(t, g, "$(B)/mod/Schema.fbs64.h", "$(B)/mod/Schema.fbs64.cpp", "$(B)/mod/Schema.bfbs64")

	fileCC := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs64.cpp.o")
	wantFileInputs := []string{
		"$(B)/mod/File.fbs64.cpp",
		"$(B)/mod/File.fbs64.h",
		"$(B)/mod/Schema.fbs64.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs64",
		"$(S)/mod/Schema.fbs64",
		"$(S)/contrib/libs/flatbuffers64/include/flatbuffers/flatbuffers.h",
	}

	if got := vfsStringsT3(fileCC.flatInputs()); !vfsInputsContainAll(got, wantFileInputs) {
		t.Fatalf("File.fbs64.cpp inputs = %v, want all of %v", got, wantFileInputs)
	}

	consumerCC := findGraphNodeByOutputs(t, g, "$(B)/mod/consumer.cpp.o")
	wantConsumerInputs := []string{
		"$(S)/mod/consumer.cpp",
		"$(B)/mod/File.fbs64.h",
		"$(B)/mod/Schema.fbs64.h",
		"$(S)/contrib/libs/flatbuffers64/include/flatbuffers/flatbuffers.h",
	}

	if got := vfsStringsT3(consumerCC.flatInputs()); !vfsInputsContainAll(got, wantConsumerInputs) {
		t.Fatalf("consumer.cpp inputs = %v, want all of %v", got, wantConsumerInputs)
	}
}
