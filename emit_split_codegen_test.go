package main

import (
	"slices"
	"testing"
)

func TestParseSplitCodegen_KeywordsAnywhere(t *testing.T) {
	args := anysOf("OUT_NUM", "30", "tools/codegen", "factors_gen", "NTop", "OUTPUT_INCLUDES", "a.h", "b.h")
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
	stmt := parseSplitCodegen(anysOf("tools/codegen", "factors_gen", "NTop"), 1)

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

	if !slices.Contains(graphForeignDeps(g, sc), tool.Ref) {
		t.Fatalf("SC node foreign deps missing tool LD ref %d: %v", tool.Ref, graphForeignDeps(g, sc))
	}

	for _, ccOut := range []string{
		"$(B)/lib/factors_gen.cpp.o",
		"$(B)/lib/factors_gen.0.cpp.o",
		"$(B)/lib/factors_gen.24.cpp.o",
	} {
		cc := mustNodeByOutput(t, g, ccOut)

		if !slices.Contains(graphDeps(g, cc), sc.Ref) {
			t.Fatalf("%s deps missing SC producer ref %d: %v", ccOut, sc.Ref, graphDeps(g, cc))
		}
	}
}

func TestGen_SplitCodegenShardInputWiring(t *testing.T) {
	files := map[string]string{}
	writeJdk17Resource(files)

	writeToolProgram(files, "contrib/tools/protoc/bin", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	files["split/ya.make"] = `LIBRARY()
SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
SET(antlr_templates ${antlr_output}/org/antlr/v4/tool/templates/codegen)
SET(sql_grammar ${antlr_output}/Grammar.g)
SET(PROTOC_PATH contrib/tools/protoc/bin)

CONFIGURE_FILE(${ARCADIA_ROOT}/grammars/Java.stg.in ${antlr_templates}/Java/Java.stg)
CONFIGURE_FILE(${ARCADIA_ROOT}/grammars/Grammar.g.in ${sql_grammar})

RUN_ANTLR4(
    ${sql_grammar}
    -lib .
    -no-listener
    -o ${antlr_output}
    -Dlanguage=Java
    IN ${sql_grammar} ${antlr_templates}/Java/Java.stg
    OUT_NOAUTO Proto.proto
    CWD ${antlr_output}
)

RUN_PROGRAM(
    $PROTOC_PATH
    -I=${CURDIR} -I=${ARCADIA_ROOT} -I=${ARCADIA_BUILD_ROOT} -I=${ARCADIA_ROOT}/contrib/libs/protobuf/src
    --cpp_out=${ARCADIA_BUILD_ROOT} --cpp_styleguide_out=${ARCADIA_BUILD_ROOT}
    --plugin=protoc-gen-cpp_styleguide=contrib/tools/protoc/plugins/cpp_styleguide
    Proto.proto
    IN Proto.proto
    TOOL contrib/tools/protoc/plugins/cpp_styleguide
    OUT_NOAUTO Proto.pb.h Proto.pb.cc
    CWD ${antlr_output}
)

RUN_PYTHON3(
    ${ARCADIA_ROOT}/tools/multiproto.py Proto
    IN Proto.pb.h
    IN Proto.pb.cc
    OUT_NOAUTO
    Proto.pb.code0.cc
    Proto.pb.code1.cc
    Proto.pb.data.cc
    Proto.pb.classes.h
    Proto.pb.main.h
    CWD ${antlr_output}
)

SRCS(
    Proto.pb.code0.cc
    Proto.pb.code1.cc
    Proto.pb.data.cc
)

END()
`
	files["grammars/Java.stg.in"] = "java template\n"
	files["grammars/Grammar.g.in"] = "grammar Proto;\n"
	files["tools/multiproto.py"] = "print('ok')\n"
	files["build/scripts/configure_file.py"] = "print('cfg')\n"
	files["build/scripts/stdout2stderr.py"] = "print('stderr')\n"
	files["contrib/java/antlr/antlr4/antlr.jar"] = ""
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"

	g := testGen(newMemFS(files), "split")

	ccShard := findGraphNodeByOutputs(t, g, "$(B)/split/Proto.pb.code0.cc.o")

	for _, forbidden := range []string{
		"$(B)/split/Proto.pb.cc",
		"$(B)/split/Proto.pb.h",
	} {
		if nodeHasInput(ccShard, forbidden) {
			t.Errorf("shard CC node input must not include %q (got build-generated proto source instead of source-level generator chain)", forbidden)
		}
	}

	for _, want := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/build/scripts/stdout2stderr.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/grammars/Java.stg.in",
		"$(S)/grammars/Grammar.g.in",
	} {
		if !nodeHasInput(ccShard, want) {
			t.Errorf("shard CC node input missing source-level generator input %q", want)
		}
	}

	for _, nonFirstShard := range []string{
		"$(B)/split/Proto.pb.code1.cc.o",
		"$(B)/split/Proto.pb.data.cc.o",
	} {
		shardNode := findGraphNodeByOutputs(t, g, nonFirstShard)

		if !nodeHasInput(shardNode, "$(B)/split/Proto.pb.code0.cc") {
			t.Errorf("non-first shard %q must carry code0.cc as an input (upstream pattern)", nonFirstShard)
		}

		for _, forbidden := range []string{"$(B)/split/Proto.pb.cc", "$(B)/split/Proto.pb.h"} {
			if nodeHasInput(shardNode, forbidden) {
				t.Errorf("non-first shard %q must not include %q as input", nonFirstShard, forbidden)
			}
		}
	}
}
