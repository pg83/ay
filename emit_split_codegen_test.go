package main

import (
	"slices"
	"testing"
)

func TestParseSplitCodegen_KeywordsAnywhere(t *testing.T) {
	args := sTRS("OUT_NUM", "30", "tools/codegen", "factors_gen", "NTop", "OUTPUT_INCLUDES", "a.h", "b.h")
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

func TestParseSplitCodegen_DefaultOutNum(t *testing.T) {
	stmt := parseSplitCodegen(sTRS("tools/codegen", "factors_gen", "NTop"), 1)

	if stmt.OutNum != splitCodegenDefaultOutNum {
		t.Fatalf("OutNum = %d, want %d", stmt.OutNum, splitCodegenDefaultOutNum)
	}
}

func TestGen_SplitCodegenGeneratedClosure(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/codegen", "codegen")

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(
    GLOBAL ${BINDIR}/factors_gen.cpp
    GLOBAL factor_names.cpp
)
SPLIT_CODEGEN(
    tools/codegen
    factors_gen
    NToponymClassifier
)
END()
`)
	writeTestModuleFile(files, "lib/factors_gen.in", "// codegen input\n")
	writeTestModuleFile(files, "lib/factor_names.cpp", "#include \"factor_names.h\"\nint fn() { return 0; }\n")
	writeTestModuleFile(files, "lib/factor_names.h", "#include <lib/factors_gen.h>\n")

	g := testGen(newMemFS(files), "lib")

	part0 := "$(B)/lib/factors_gen.0.cpp"
	inputIn := "$(S)/lib/factors_gen.in"
	genHeader := "$(B)/lib/factors_gen.h"

	for _, ccOut := range []string{
		"$(B)/lib/factors_gen.1.cpp.o",
		"$(B)/lib/factors_gen.cpp.o",
	} {
		cc := mustNodeByOutput(t, g, ccOut)

		if !nodeHasInput(cc, part0) {
			t.Errorf("%s inputs missing %q: %v", ccOut, part0, cc.flatInputs())
		}

		if !nodeHasInput(cc, inputIn) {
			t.Errorf("%s inputs missing %q: %v", ccOut, inputIn, cc.flatInputs())
		}

		if nodeHasInput(cc, genHeader) {
			t.Errorf("%s inputs must not include the generated header %q: %v", ccOut, genHeader, cc.flatInputs())
		}
	}

	fn := mustNodeByOutput(t, g, "$(B)/lib/factor_names.cpp.o")

	if !nodeHasInput(fn, part0) {
		t.Errorf("factor_names.cpp.o inputs missing %q: %v", part0, fn.flatInputs())
	}

	if !nodeHasInput(fn, inputIn) {
		t.Errorf("factor_names.cpp.o inputs missing %q: %v", inputIn, fn.flatInputs())
	}
}

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

	if sc.KV.PC != pcYellow {
		t.Fatalf("SC node pc = %v, want yellow", sc.KV.PC)
	}

	if !nodeHasInput(sc, "$(S)/lib/factors_gen.in") {
		t.Fatalf("SC node inputs missing $(S)/lib/factors_gen.in: %v", sc.flatInputs())
	}

	tool := mustNodeByOutput(t, g, "$(B)/tools/codegen/codegen")

	if !nodeHasInput(sc, "$(B)/tools/codegen/codegen") {
		t.Fatalf("SC node inputs missing the codegen tool binary: %v", sc.flatInputs())
	}

	if !slices.Contains(graphForeignDeps(g, sc), tool.UID) {
		t.Fatalf("SC node foreign deps missing tool LD uid %q: %v", tool.UID, graphForeignDeps(g, sc))
	}

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
