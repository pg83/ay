package main

import "testing"

func TestEmitFL_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no", nil)
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
