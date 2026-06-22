package main

import "testing"

// TestGen_SplitCodegenShardInputWiring reproduces the divergence where
// pb.code0.cc.o carries the monolithic $(B)/Proto.pb.cc and $(B)/Proto.pb.h as
// inputs, but upstream's shard CC nodes carry only source-level generator
// inputs. After the fix the shard CC nodes drop both monolithic build-generated
// sources, and pb.main.h carries the shard CC paths for consumers' closures.
func TestGen_SplitCodegenShardInputWiring(t *testing.T) {
	files := map[string]string{}

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

	// Shard CC nodes carry only source-level generator chain files, not the
	// monolithic build-generated protobuf sources.
	for _, forbidden := range []string{
		"$(B)/split/Proto.pb.cc",
		"$(B)/split/Proto.pb.h",
	} {
		if nodeHasInput(ccShard, forbidden) {
			t.Errorf("shard CC node input must not include %q (got build-generated proto source instead of source-level generator chain)", forbidden)
		}
	}

	// The shard CC node must have the source-level generator closure.
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

	// Non-first shards (code1.cc, data.cc) carry the first shard (code0.cc) in
	// their input closure, matching upstream.
	for _, nonFirstShard := range []string{
		"$(B)/split/Proto.pb.code1.cc.o",
		"$(B)/split/Proto.pb.data.cc.o",
	} {
		shardNode := findGraphNodeByOutputs(t, g, nonFirstShard)
		if !nodeHasInput(shardNode, "$(B)/split/Proto.pb.code0.cc") {
			t.Errorf("non-first shard %q must carry code0.cc as an input (upstream pattern)", nonFirstShard)
		}
		// Must not carry the monolithic sources either.
		for _, forbidden := range []string{"$(B)/split/Proto.pb.cc", "$(B)/split/Proto.pb.h"} {
			if nodeHasInput(shardNode, forbidden) {
				t.Errorf("non-first shard %q must not include %q as input", nonFirstShard, forbidden)
			}
		}
	}
}
