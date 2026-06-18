package main

import (
	"slices"
	"testing"
)

// TestParseBaseCodegen pins the positional Tool/Prefix/Opts split of BASE_CODEGEN
// (build/internal/conf/codegen.conf: macro BASE_CODEGEN(Tool, Prefix, Opts...)).
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
// for BASE_CODEGEN consumers (T-29). Upstream carries the generated-from sibling
// prefix.cpp and the prefix.in source as closure leaves of prefix.h, so every CC
// node that includes the generated header inherits them. This is the BC half of
// the sg7 kernel/factor_slices evidence (factor_slices_gen.h → factor_slices_gen.cpp
// + factor_slices_gen.in on kernel/fill_factors_codegen/main.cpp.pic.o).
//
// Pre-T-29 prefix.h has no closure leaves, so a consumer of base_gen.h sees the
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

// TestGen_BaseCodegenReachability is the T-22 regression. It guards the
// reachability rule verified by T-21: BASE_CODEGEN must emit a BC producer that
// takes a tool dependency on the tool PROGRAM's LD, and that tool dependency must
// bring the tool's ordinary PEERDIR closure into the target graph. Here the tool
// PEERDIRs a child module (hidden/factors) that is otherwise unreachable from the
// target; that child uses SPLIT_CODEGEN, so post-fix the graph must contain the
// child's SC producer and its immediate generated CC consumers.
//
// Pre-fix (BASE_CODEGEN acknowledged as a no-op) this fails: no BC node exists
// and hidden/factors is absent, so its SC producer / generated CC are missing.
func TestGen_BaseCodegenReachability(t *testing.T) {
	files := map[string]string{}

	// tool PROGRAM whose PEERDIR reaches the otherwise-unreachable child module.
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

	// 3. reachability: the tool PEERDIR closure pulled hidden/factors into the
	// graph, so its SPLIT_CODEGEN producer and a generated CC consumer exist.
	scH := mustNodeByAnyOutput(t, g, "$(B)/hidden/factors/factors_gen.h")

	if scH.KV.P != pkSC {
		t.Fatalf("hidden/factors factors_gen.h producer kv = %v, want SC", scH.KV.P)
	}

	cc := mustNodeByOutput(t, g, "$(B)/hidden/factors/factors_gen.0.cpp.pic.o")

	if !slices.Contains(graphDeps(g, cc), scH.UID) {
		t.Fatalf("generated CC deps missing SC producer uid %q: %v", scH.UID, graphDeps(g, cc))
	}
}
