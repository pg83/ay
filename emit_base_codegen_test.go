package main

import (
	"slices"
	"testing"
)

// TestParseBaseCodegen pins the positional Tool/Prefix/Opts split of
// BASE_CODEGEN(Tool, Prefix, Opts...).
func TestParseBaseCodegen(t *testing.T) {
	stmt := parseBaseCodegen(STRS("kernel/fill_factors_codegen", "fill_factors", "NTop"), 1)

	if stmt.ToolPath.string() != "kernel/fill_factors_codegen" {
		t.Fatalf("ToolPath = %q", stmt.ToolPath.string())
	}

	if stmt.Prefix.string() != "fill_factors" {
		t.Fatalf("Prefix = %q", stmt.Prefix.string())
	}

	if got := strStrings(stmt.Opts); !slices.Equal(got, []string{"NTop"}) {
		t.Fatalf("Opts = %v, want [NTop]", got)
	}
}

// TestGen_BaseCodegenGeneratedClosure pins the generated-header include closure
// for BASE_CODEGEN consumers. The sibling prefix.cpp and the prefix.in source
// ride as closure leaves of prefix.h, so every CC node that includes the
// generated header inherits them.
//
// Without it prefix.h has no closure leaves, so a consumer of base_gen.h sees the
// header but neither base_gen.cpp nor base_gen.in.
func TestGen_BaseCodegenGeneratedClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tool", "base_gen")

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
BASE_CODEGEN(tool base_gen)
SRCS(
    GLOBAL ${BINDIR}/base_gen.cpp
    GLOBAL use.cpp
)
END()
`)
	writeTestModuleFile(files, "lib/base_gen.in", "// base codegen input\n")
	writeTestModuleFile(files, "lib/use.cpp", "#include <lib/base_gen.h>\nint use() { return 0; }\n")

	g := testGen(newMemFS(files), "lib")

	cc := mustNodeByOutput(t, g, "$(B)/lib/use.cpp.o")

	for _, want := range []string{"$(B)/lib/base_gen.cpp", "$(S)/lib/base_gen.in"} {
		if !nodeHasInput(cc, want) {
			t.Errorf("use.cpp.o inputs missing generated-from leaf %q: %v", want, cc.flatInputs())
		}
	}
}

// TestGen_BaseCodegenReachability guards the reachability rule: BASE_CODEGEN must
// emit a BC producer taking a tool dependency on the tool PROGRAM's LD, and that
// dependency must bring the tool's ordinary PEERDIR closure into the target graph.
// Here the tool PEERDIRs a child module (hidden/factors) otherwise unreachable
// from the target; the child uses SPLIT_CODEGEN, so post-fix the graph must
// contain the child's SC producer and its generated CC consumers.
//
// When BASE_CODEGEN is a no-op this fails: no BC node exists and hidden/factors is
// absent, so its SC producer / generated CC are missing.
func TestGen_BaseCodegenReachability(t *testing.T) {
	files := map[string]string{}

	// tool PROGRAM whose PEERDIR reaches the unreachable child module.
	writeTestModuleFile(files, "tool/ya.make", `PROGRAM(tool)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(hidden/factors)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tool/main.cpp", "int main(){return 0;}\n")

	// tool2: the SPLIT_CODEGEN tool used by the child module.
	writeToolProgram(files, "tool2", "tool2")

	// child module reached only through the tool's PEERDIR closure.
	writeTestModuleFile(files, "hidden/factors/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    GLOBAL ${BINDIR}/factors_gen.cpp
)
SPLIT_CODEGEN(
    tool2
    factors_gen
    NHidden
)
END()
`)
	writeTestModuleFile(files, "hidden/factors/factors_gen.in", "// codegen input\n")

	// consumer: the BASE_CODEGEN user. It is the build target.
	writeTestModuleFile(files, "consumer/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
BASE_CODEGEN(tool fill_factors)
SRCS(
    GLOBAL ${BINDIR}/fill_factors.cpp
)
END()
`)
	writeTestModuleFile(files, "consumer/fill_factors.in", "// base codegen input\n")

	g := testGen(newMemFS(files), "consumer")

	// 1. exactly one BC producer node, for the consumer's prefix outputs.
	var bc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkBC {
			if bc != nil {
				t.Fatalf("expected exactly one BC node, found a second producing %v", n.Outputs)
			}

			bc = n
		}
	}

	if bc == nil {
		t.Fatalf("no BASE_CODEGEN producer (kv p=BC) node in graph")
	}

	if bc.KV.PC != pcYellow {
		t.Fatalf("BC node pc = %v, want yellow", bc.KV.PC)
	}

	// outputs: fill_factors.cpp (noauto) + fill_factors.h, nothing else.
	wantOuts := []string{"$(B)/consumer/fill_factors.cpp", "$(B)/consumer/fill_factors.h"}
	gotOuts := make([]string, 0, len(bc.Outputs))

	for _, o := range bc.Outputs {
		gotOuts = append(gotOuts, o.string())
	}

	if !slices.Equal(gotOuts, wantOuts) {
		t.Fatalf("BC outputs = %v, want %v", gotOuts, wantOuts)
	}

	// command: tool bin, $(S) .in, $(B) .cpp, $(B) .h (no --cpp-parts).
	wantCmd := []string{
		"$(B)/tool/tool",
		"$(S)/consumer/fill_factors.in",
		"$(B)/consumer/fill_factors.cpp",
		"$(B)/consumer/fill_factors.h",
	}
	if got := strStrings(bc.Cmds[0].CmdArgs.flat()); !slices.Equal(got, wantCmd) {
		t.Fatalf("BC cmd = %v, want %v", got, wantCmd)
	}

	// 2. BC takes a tool (foreign) dependency on the tool PROGRAM's LD node.
	tool := mustNodeByOutput(t, g, "$(B)/tool/tool")

	if !nodeHasInput(bc, "$(B)/tool/tool") {
		t.Fatalf("BC node inputs missing the tool binary: %v", bc.flatInputs())
	}

	if !slices.Contains(graphForeignDeps(g, bc), tool.UID) {
		t.Fatalf("BC node foreign deps missing tool LD uid %q: %v", tool.UID, graphForeignDeps(g, bc))
	}

	// 3. the tool PEERDIR closure pulled hidden/factors in, so its
	// SPLIT_CODEGEN producer and a generated CC consumer exist.
	scH := mustNodeByAnyOutput(t, g, "$(B)/hidden/factors/factors_gen.h")

	if scH.KV.P != pkSC {
		t.Fatalf("hidden/factors factors_gen.h producer kv = %v, want SC", scH.KV.P)
	}

	cc := mustNodeByOutput(t, g, "$(B)/hidden/factors/factors_gen.0.cpp.pic.o")

	if !slices.Contains(graphDeps(g, cc), scH.UID) {
		t.Fatalf("generated CC deps missing SC producer uid %q: %v", scH.UID, graphDeps(g, cc))
	}
}

// TestGen_StructCodegenProducer pins STRUCT_CODEGEN as the BASE_CODEGEN
// specialization. STRUCT_CODEGEN(gen) must (1) emit a BC producer running the
// fixed codegen tool over gen.in -> gen.cpp + gen.h, with a tool dep on the
// codegen tool's LD; (2) attach the seven STRUCT_CODEGEN_OUTPUT_INCLUDES to gen.h
// so a consumer of gen.h inherits them; (3) pull the two implicit PEERDIRs
// (metadata, reflection) into the module closure.
//
// When STRUCT_CODEGEN is a no-op none of these hold — the test fails for the right
// reason (missing producer).
func TestGen_StructCodegenProducer(t *testing.T) {
	files := map[string]string{}

	// The fixed codegen tool.
	writeToolProgram(files, "kernel/struct_codegen/codegen_tool", "codegen_tool")

	// The two implicit peerdir libraries, plus the referenced reflection headers.
	writeTestModuleFile(files, "kernel/struct_codegen/metadata/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(metadata.cpp)
END()
`)
	writeTestModuleFile(files, "kernel/struct_codegen/metadata/metadata.cpp", "int metadata(){return 0;}\n")
	writeTestModuleFile(files, "kernel/struct_codegen/reflection/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(reflection.cpp)
END()
`)
	writeTestModuleFile(files, "kernel/struct_codegen/reflection/reflection.cpp", "int reflection(){return 0;}\n")
	writeTestModuleFile(files, "kernel/struct_codegen/reflection/reflection.h", "#pragma once\n")
	writeTestModuleFile(files, "kernel/struct_codegen/reflection/floats.h", "#pragma once\n")

	// The util headers referenced by the output includes.
	for _, h := range []string{
		"util/generic/singleton.h", "util/generic/strbuf.h", "util/generic/vector.h",
		"util/generic/ptr.h", "util/generic/yexception.h",
	} {
		writeTestModuleFile(files, h, "#pragma once\n")
	}

	// Consumer module: STRUCT_CODEGEN(gen) plus a source that includes gen.h.
	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
STRUCT_CODEGEN(gen)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "lib/gen.in", "// struct codegen input\n")
	writeTestModuleFile(files, "lib/use.cpp", "#include <lib/gen.h>\nint use(){return 0;}\n")

	g := testGen(newMemFS(files), "lib")

	// 1. exactly one BC producer node for the consumer's gen outputs.
	var bc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkBC {
			if bc != nil {
				t.Fatalf("expected exactly one BC node, found a second producing %v", n.Outputs)
			}

			bc = n
		}
	}

	if bc == nil {
		t.Fatalf("no STRUCT_CODEGEN producer (kv p=BC) node in graph")
	}

	wantOuts := []string{"$(B)/lib/gen.cpp", "$(B)/lib/gen.h"}
	gotOuts := make([]string, 0, len(bc.Outputs))

	for _, o := range bc.Outputs {
		gotOuts = append(gotOuts, o.string())
	}

	if !slices.Equal(gotOuts, wantOuts) {
		t.Fatalf("BC outputs = %v, want %v", gotOuts, wantOuts)
	}

	// command: fixed codegen tool bin, $(S) .in, $(B) .cpp, $(B) .h (no opts).
	wantCmd := []string{
		"$(B)/kernel/struct_codegen/codegen_tool/codegen_tool",
		"$(S)/lib/gen.in",
		"$(B)/lib/gen.cpp",
		"$(B)/lib/gen.h",
	}
	if got := strStrings(bc.Cmds[0].CmdArgs.flat()); !slices.Equal(got, wantCmd) {
		t.Fatalf("BC cmd = %v, want %v", got, wantCmd)
	}

	// 2. tool (foreign) dep on the codegen tool LD.
	tool := mustNodeByOutput(t, g, "$(B)/kernel/struct_codegen/codegen_tool/codegen_tool")

	if !slices.Contains(graphForeignDeps(g, bc), tool.UID) {
		t.Fatalf("BC node foreign deps missing codegen tool LD uid %q: %v", tool.UID, graphForeignDeps(g, bc))
	}

	// 3. the OUTPUT_INCLUDES ride gen.h: a consumer of gen.h inherits them.
	cc := mustNodeByOutput(t, g, "$(B)/lib/use.cpp.o")

	for _, want := range []string{
		"$(S)/util/generic/singleton.h",
		"$(S)/kernel/struct_codegen/reflection/reflection.h",
		"$(S)/kernel/struct_codegen/reflection/floats.h",
	} {
		if !nodeHasInput(cc, want) {
			t.Errorf("use.cpp.o closure missing STRUCT_CODEGEN output_include %q: %v", want, cc.flatInputs())
		}
	}

	// 4. the two implicit PEERDIRs are in the module closure.
	mustNodeByAnyOutput(t, g, "$(B)/kernel/struct_codegen/metadata/metadata.cpp.o")
	mustNodeByAnyOutput(t, g, "$(B)/kernel/struct_codegen/reflection/reflection.cpp.o")
}
