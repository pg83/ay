import unittest

import lib


class EmitSplitCodegenTest(unittest.TestCase):
    def test_generated_closure_reaches_shards_and_consumer(self):
        files = {
            "lib/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(\n"
                "  GLOBAL ${BINDIR}/factors_gen.cpp\n"
                "  GLOBAL factor_names.cpp\n"
                ")\n"
                "SPLIT_CODEGEN(tools/codegen factors_gen NToponymClassifier)\n"
                "END()\n"
            ),
            "lib/factors_gen.in": "// codegen input\n",
            "lib/factor_names.cpp": (
                '#include "factor_names.h"\nint fn() { return 0; }\n'
            ),
            "lib/factor_names.h": "#include <lib/factors_gen.h>\n",
        }
        lib.tool_program(files, "tools/codegen", "codegen")
        graph = lib.make(files, "lib")
        for output in (
            "$(B)/lib/factors_gen.1.cpp.o",
            "$(B)/lib/factors_gen.cpp.o",
            "$(B)/lib/factor_names.cpp.o",
        ):
            node = lib.node_by_output(graph, output)
            self.assertIn("$(B)/lib/factors_gen.0.cpp", node["inputs"])
            self.assertIn("$(S)/lib/factors_gen.in", node["inputs"])
            if output != "$(B)/lib/factor_names.cpp.o":
                self.assertNotIn("$(B)/lib/factors_gen.h", node["inputs"])

    def test_producer_shape_and_shard_dependencies(self):
        files = {
            "lib/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(GLOBAL ${BINDIR}/factors_gen.cpp)\n"
                "SPLIT_CODEGEN(tools/codegen factors_gen NToponymClassifier)\n"
                "END()\n"
            ),
            "lib/factors_gen.in": "// codegen input\n",
        }
        lib.tool_program(files, "tools/codegen", "codegen")
        graph = lib.make(files, "lib")
        producer = lib.only_node_by_kind(graph, "SC")
        for output in (
            "$(B)/lib/factors_gen.0.cpp",
            "$(B)/lib/factors_gen.24.cpp",
            "$(B)/lib/factors_gen.cpp",
            "$(B)/lib/factors_gen.h",
        ):
            self.assertIn(output, producer["outputs"])
        self.assertEqual(len(producer["outputs"]), 27)
        self.assertEqual(producer["kv"]["pc"], "yellow")
        self.assertIn("$(S)/lib/factors_gen.in", producer["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/codegen/codegen")
        self.assertIn("$(B)/tools/codegen/codegen", producer["inputs"])
        self.assertIn(tool["uid"], producer["deps"])
        for output in (
            "$(B)/lib/factors_gen.cpp.o",
            "$(B)/lib/factors_gen.0.cpp.o",
            "$(B)/lib/factors_gen.24.cpp.o",
        ):
            self.assertIn(
                producer["uid"],
                lib.node_by_output(graph, output)["deps"],
            )

    def test_generated_proto_shards_keep_source_level_generator_chain(self):
        files = {
            "build/platform/java/jdk/jdk17/ya.make": (
                "RESOURCES_LIBRARY()\n"
                "DECLARE_EXTERNAL_RESOURCE(JDK17 sbr:1)\n"
                "END()\n"
            ),
            "split/ya.make": r'''LIBRARY()
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
SRCS(Proto.pb.code0.cc Proto.pb.code1.cc Proto.pb.data.cc)
END()
''',
            "grammars/Java.stg.in": "java template\n",
            "grammars/Grammar.g.in": "grammar Proto;\n",
            "tools/multiproto.py": "print('ok')\n",
            "build/scripts/configure_file.py": "print('cfg')\n",
            "build/scripts/stdout2stderr.py": "print('stderr')\n",
            "contrib/java/antlr/antlr4/antlr.jar": "",
            "contrib/libs/protobuf/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "NO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
            ),
            "contrib/libs/protobuf/p.cpp": "int p(){return 0;}\n",
        }
        lib.tool_program(files, "contrib/tools/protoc/bin", "protoc")
        lib.tool_program(
            files,
            "contrib/tools/protoc/plugins/cpp_styleguide",
            "cpp_styleguide",
        )
        graph = lib.make(files, "split")
        first = lib.node_by_output(graph, "$(B)/split/Proto.pb.code0.cc.o")
        for forbidden in ("$(B)/split/Proto.pb.cc", "$(B)/split/Proto.pb.h"):
            self.assertNotIn(forbidden, first["inputs"])
        for expected in (
            "$(S)/tools/multiproto.py",
            "$(S)/build/scripts/stdout2stderr.py",
            "$(S)/contrib/java/antlr/antlr4/antlr.jar",
            "$(S)/build/scripts/configure_file.py",
            "$(S)/grammars/Java.stg.in",
            "$(S)/grammars/Grammar.g.in",
        ):
            self.assertIn(expected, first["inputs"])
        for output in (
            "$(B)/split/Proto.pb.code1.cc.o",
            "$(B)/split/Proto.pb.data.cc.o",
        ):
            shard = lib.node_by_output(graph, output)
            self.assertIn("$(B)/split/Proto.pb.code0.cc", shard["inputs"])
            self.assertNotIn("$(B)/split/Proto.pb.cc", shard["inputs"])
            self.assertNotIn("$(B)/split/Proto.pb.h", shard["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
