package main

import (
	"slices"
	"testing"
)

func TestParseBaseCodegen(t *testing.T) {
	stmt := parseBaseCodegen(anysOf("kernel/fill_factors_codegen", "fill_factors", "NTop"), 1)

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

func TestGen_BaseCodegenReachability(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "tool/ya.make", `PROGRAM(tool)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(hidden/factors)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tool/main.cpp", "int main(){return 0;}\n")

	writeToolProgram(files, "tool2", "tool2")

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

	wantOuts := []string{"$(B)/consumer/fill_factors.cpp", "$(B)/consumer/fill_factors.h"}
	gotOuts := make([]string, 0, len(bc.Outputs))

	for _, o := range bc.Outputs {
		gotOuts = append(gotOuts, o.string())
	}

	if !slices.Equal(gotOuts, wantOuts) {
		t.Fatalf("BC outputs = %v, want %v", gotOuts, wantOuts)
	}

	wantCmd := []string{
		"$(B)/tool/tool",
		"$(S)/consumer/fill_factors.in",
		"$(B)/consumer/fill_factors.cpp",
		"$(B)/consumer/fill_factors.h",
	}

	if got := anyStrs(bc.Cmds[0].CmdArgs.flat()); !slices.Equal(got, wantCmd) {
		t.Fatalf("BC cmd = %v, want %v", got, wantCmd)
	}

	tool := mustNodeByOutput(t, g, "$(B)/tool/tool")

	if !nodeHasInput(bc, "$(B)/tool/tool") {
		t.Fatalf("BC node inputs missing the tool binary: %v", bc.flatInputs())
	}

	if !slices.Contains(graphForeignDeps(g, bc), tool.Ref) {
		t.Fatalf("BC node foreign deps missing tool LD ref %d: %v", tool.Ref, graphForeignDeps(g, bc))
	}

	scH := mustNodeByAnyOutput(t, g, "$(B)/hidden/factors/factors_gen.h")

	if scH.KV.P != pkSC {
		t.Fatalf("hidden/factors factors_gen.h producer kv = %v, want SC", scH.KV.P)
	}

	cc := mustNodeByOutput(t, g, "$(B)/hidden/factors/factors_gen.0.cpp.pic.o")

	if !slices.Contains(graphDeps(g, cc), scH.Ref) {
		t.Fatalf("generated CC deps missing SC producer ref %d: %v", scH.Ref, graphDeps(g, cc))
	}
}

func TestGen_StructCodegenProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "kernel/struct_codegen/codegen_tool", "codegen_tool")

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

	for _, h := range []string{
		"util/generic/singleton.h", "util/generic/strbuf.h", "util/generic/vector.h",
		"util/generic/ptr.h", "util/generic/yexception.h",
	} {
		writeTestModuleFile(files, h, "#pragma once\n")
	}

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

	wantCmd := []string{
		"$(B)/kernel/struct_codegen/codegen_tool/codegen_tool",
		"$(S)/lib/gen.in",
		"$(B)/lib/gen.cpp",
		"$(B)/lib/gen.h",
	}

	if got := anyStrs(bc.Cmds[0].CmdArgs.flat()); !slices.Equal(got, wantCmd) {
		t.Fatalf("BC cmd = %v, want %v", got, wantCmd)
	}

	tool := mustNodeByOutput(t, g, "$(B)/kernel/struct_codegen/codegen_tool/codegen_tool")

	if !slices.Contains(graphForeignDeps(g, bc), tool.Ref) {
		t.Fatalf("BC node foreign deps missing codegen tool LD ref %d: %v", tool.Ref, graphForeignDeps(g, bc))
	}

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

	mustNodeByAnyOutput(t, g, "$(B)/kernel/struct_codegen/metadata/metadata.cpp.o")
	mustNodeByAnyOutput(t, g, "$(B)/kernel/struct_codegen/reflection/reflection.cpp.o")
}
