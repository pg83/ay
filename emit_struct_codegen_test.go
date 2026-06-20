package main

import (
	"slices"
	"testing"
)

// TestGen_StructCodegenProducer pins STRUCT_CODEGEN as the BASE_CODEGEN
// specialization defined in build/internal/conf/codegen.conf:
//
//	macro STRUCT_CODEGEN(Prefix) {
//	    .CMD=$BASE_CODEGEN(kernel/struct_codegen/codegen_tool $Prefix $STRUCT_CODEGEN_OUTPUT_INCLUDES)
//	    .PEERDIR=kernel/struct_codegen/metadata kernel/struct_codegen/reflection
//	}
//
// So STRUCT_CODEGEN(gen) must (1) emit a BC producer running the fixed
// kernel/struct_codegen/codegen_tool over gen.in -> gen.cpp + gen.h, with a tool
// dep on the codegen tool's LD; (2) attach the seven STRUCT_CODEGEN_OUTPUT_INCLUDES
// to gen.h so a consumer of gen.h inherits them; (3) pull the two implicit
// PEERDIRs (metadata, reflection) into the module closure.
//
// Pre-fix STRUCT_CODEGEN is acknowledged as a no-op, so no BC node exists and none
// of these hold — the test fails for the right reason (missing producer).
func TestGen_StructCodegenProducer(t *testing.T) {
	files := map[string]string{}

	// The fixed codegen tool.
	writeToolProgram(files, "kernel/struct_codegen/codegen_tool", "codegen_tool")

	// The two implicit peerdir libraries, plus the reflection headers referenced
	// by STRUCT_CODEGEN_OUTPUT_INCLUDES.
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

	// The util headers referenced by STRUCT_CODEGEN_OUTPUT_INCLUDES.
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

	// 3. the seven OUTPUT_INCLUDES ride gen.h: a consumer of gen.h inherits them.
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
