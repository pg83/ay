package main

import "testing"

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
		testToolchain(),
		e,
		&flatcVariantFL,
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
	// The flatc tooling, sources and runtime header ride to the consumer as
	// non-expanded closure leaves of the .fbs.h (registered in ensureFlatcEmission);
	// leaves bypass the closure dedup, so the raw input list is order- and
	// dup-agnostic — assert membership, mirroring the gate's normalized (sorted,
	// deduped) comparison.
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
		testToolchain(),
		e,
		&flatcVariantFL64,
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
	// IO-lead include order for FL64 is -I $(S) -I $(B) (ARCADIA_ROOT then
	// ARCADIA_BUILD_ROOT), opposite to FL's -I $(B) -I $(S).
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
