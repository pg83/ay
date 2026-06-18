package main

import (
	"slices"
	"testing"
)

// TestParseSplitCodegen_KeywordsAnywhere pins that OUT_NUM / OUTPUT_INCLUDES are
// keyword sections that may precede the positional tool/prefix (mirroring the
// Python macro's keyword args): tool and prefix are the first two NON-keyword
// tokens. A naive args[0]/args[1] split mis-binds the tool to "OUT_NUM".
func TestParseSplitCodegen_KeywordsAnywhere(t *testing.T) {
	args := STRS("OUT_NUM", "30", "tools/codegen", "factors_gen", "NTop", "OUTPUT_INCLUDES", "a.h", "b.h")
	stmt := parseSplitCodegen(args, 1)

	if stmt.ToolPath.string() != "tools/codegen" {
		t.Fatalf("ToolPath = %q, want tools/codegen", stmt.ToolPath.string())
	}

	if stmt.Prefix.string() != "factors_gen" {
		t.Fatalf("Prefix = %q, want factors_gen", stmt.Prefix.string())
	}

	if stmt.OutNum != 30 {
		t.Fatalf("OutNum = %d, want 30", stmt.OutNum)
	}

	if got := strStrings(stmt.Opts); !slices.Equal(got, []string{"NTop"}) {
		t.Fatalf("Opts = %v, want [NTop]", got)
	}

	if got := strStrings(stmt.OutputIncludes); !slices.Equal(got, []string{"a.h", "b.h"}) {
		t.Fatalf("OutputIncludes = %v, want [a.h b.h]", got)
	}
}

// TestParseSplitCodegen_DefaultOutNum: no OUT_NUM keyword → default 25.
func TestParseSplitCodegen_DefaultOutNum(t *testing.T) {
	stmt := parseSplitCodegen(STRS("tools/codegen", "factors_gen", "NTop"), 1)

	if stmt.OutNum != splitCodegenDefaultOutNum {
		t.Fatalf("OutNum = %d, want %d", stmt.OutNum, splitCodegenDefaultOutNum)
	}
}

// TestGen_SplitCodegenProducer pins SPLIT_CODEGEN as a generated-output producer
// (kv p=SC): the macro must emit one SC node whose outputs are the OUT_NUM
// numbered .cpp parts plus prefix.cpp + prefix.h, depend on the codegen tool, and
// feed the generated sources back into the module build so the CC compiles of the
// numbered parts and the ${BINDIR}/prefix.cpp re-fed source carry the producer dep.
func TestGen_SplitCodegenProducer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/codegen", "codegen")

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    GLOBAL ${BINDIR}/factors_gen.cpp
)
SPLIT_CODEGEN(
    tools/codegen
    factors_gen
    NToponymClassifier
)
END()
`)
	writeTestModuleFile(files, "lib/factors_gen.in", "// codegen input\n")

	g := testGen(newMemFS(files), "lib")

	var sc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkSC {
			if sc != nil {
				t.Fatalf("expected exactly one SC node, found a second producing %v", n.Outputs)
			}

			sc = n
		}
	}

	if sc == nil {
		t.Fatalf("no SPLIT_CODEGEN producer (kv p=SC) node in graph")
	}

	// Outputs: factors_gen.0.cpp .. factors_gen.24.cpp + factors_gen.cpp + factors_gen.h.
	wantOuts := []string{
		"$(B)/lib/factors_gen.0.cpp",
		"$(B)/lib/factors_gen.24.cpp",
		"$(B)/lib/factors_gen.cpp",
		"$(B)/lib/factors_gen.h",
	}

	for _, want := range wantOuts {
		found := false

		for _, o := range sc.Outputs {
			if o.string() == want {
				found = true

				break
			}
		}

		if !found {
			t.Fatalf("SC node missing output %q: %v", want, sc.Outputs)
		}
	}

	if got := len(sc.Outputs); got != 27 {
		t.Fatalf("SC node output count = %d, want 27 (25 parts + cpp + h)", got)
	}

	// kv pc=yellow.
	if sc.KV.PC != pcYellow {
		t.Fatalf("SC node pc = %v, want yellow", sc.KV.PC)
	}

	// Inputs: the codegen tool binary + the $(S) .in source.
	if !nodeHasInput(sc, "$(S)/lib/factors_gen.in") {
		t.Fatalf("SC node inputs missing $(S)/lib/factors_gen.in: %v", sc.flatInputs())
	}

	tool := mustNodeByOutput(t, g, "$(B)/tools/codegen/codegen")

	if !nodeHasInput(sc, "$(B)/tools/codegen/codegen") {
		t.Fatalf("SC node inputs missing the codegen tool binary: %v", sc.flatInputs())
	}

	// The tool is a foreign (tool) dep.
	if !slices.Contains(graphForeignDeps(g, sc), tool.UID) {
		t.Fatalf("SC node foreign deps missing tool LD uid %q: %v", tool.UID, graphForeignDeps(g, sc))
	}

	// Generated-source consumption: every generated cpp compiles to a CC node that
	// depends on the SC producer.
	for _, ccOut := range []string{
		"$(B)/lib/factors_gen.cpp.o",
		"$(B)/lib/factors_gen.0.cpp.o",
		"$(B)/lib/factors_gen.24.cpp.o",
	} {
		cc := mustNodeByOutput(t, g, ccOut)

		if !slices.Contains(graphDeps(g, cc), sc.UID) {
			t.Fatalf("%s deps missing SC producer uid %q: %v", ccOut, sc.UID, graphDeps(g, cc))
		}
	}
}
