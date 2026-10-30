import json

from tst.make.lib import MakeTest, PROJECT


class GeneratorInputsTest(MakeTest):
    def test_split_codegen_tail_options_reach_the_generator(self):
        self.tool()
        self.write("ya.make", f"LIBRARY()\nSPLIT_CODEGEN({PROJECT}/generator answer --e2e-mode=checks OUT_NUM 6)\nEND()\n")
        node = self.node(self.graph(), "answer.0.cpp")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--e2e-mode=checks", args)
        self.assertIn(f"$(S)/{PROJECT}/answer.in", node["inputs"])

    def test_struct_codegen_keeps_its_schema_and_generated_header(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSTRUCT_CODEGEN(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.in", "struct Answer { ui32 Value; }\n")
        graph = self.graph()
        generated = self.node(graph, "answer.cpp")
        self.assertIn(f"$(S)/{PROJECT}/answer.in", generated["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/answer.h", generated["outputs"])
        compiled = self.node(graph, "answer.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])

    def test_obsolete_enum_serialization_noutf_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nGENERATE_ENUM_SERIALIZATION_NOUTF(answer.h)\nEND()\n")
        self.write("answer.h", "#pragma once\nenum class Answer { Value = 42 };\n")
        self.assertNotEqual(self.make().returncode, 0)

    def tool(self):
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'import pathlib, sys\npathlib.Path(sys.argv[2]).write_text("int answer() { return 42; }\\n")\npathlib.Path(sys.argv[3]).write_text("#pragma once\\n")\nif "--cpp-parts" in sys.argv:\n    count = int(sys.argv[sys.argv.index("--cpp-parts") + 1]) + 5\n    for i in range(count):\n        pathlib.Path(sys.argv[2].removesuffix(".cpp") + f".{i}.cpp").write_text(f"int part{i}() {{ return {i}; }}\\n")\n')
        self.write("answer.in", "42\n")

    def test_split_codegen(self):
        self.tool()
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSPLIT_CODEGEN({PROJECT}/generator answer OUT_NUM 6 OUTPUT_INCLUDES dependency.h)\nEND()\n")
        self.write("dependency.h", "#pragma once\n")
        graph = self.graph()
        self.node(graph, "answer.0.cpp")
        self.node(graph, "answer.5.cpp.o")

    def test_base_codegen(self):
        self.tool()
        self.write("ya.make", f"LIBRARY()\nBASE_CODEGEN({PROJECT}/generator answer)\nSRCS(answer.cpp)\nEND()\n")
        self.node(self.graph(), "answer.cpp")

    def test_python_flatbuffers(self):
        for module in ("PY3_PROGRAM", "PY2_PROGRAM"):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}(answer)\nPY_SRCS(MAIN main.py answer.fbs other.fbs)\nEND()\n")
                self.write("main.py", "print(42)\n")
                self.write("answer.fbs", 'include "other.fbs"; namespace e2e; table Answer { value: Other; } root_type Answer;\n')
                self.write("other.fbs", "namespace e2e; table Other { value: int; }\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.fbs" in node["inputs"] and "flatc" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_sql_schema(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nPEERDIR(metrika/core/libs/mysqlxx metrika/core/libs/sqxxl)\nSRCS(answer.ssqls base.ssqls)\nEND()\n")
        self.write("answer.ssqls", '<DbObject><namespace>e2e</namespace><name>Answer</name><only_sqxxl>true</only_sqxxl><include>dependency.h</include><ancestors><ancestor path="base.ssqls">Base</ancestor></ancestors><fields><field type="Int8" name="Value"/></fields></DbObject>\n')
        self.write("base.ssqls", '<DbObject><namespace>e2e</namespace><name>Base</name><only_sqxxl>true</only_sqxxl><fields><field type="Int8" name="Key"/></fields></DbObject>\n')
        self.write("dependency.h", "#pragma once\n")
        graph = self.graph()
        self.node(graph, "answer.h")
        self.node(graph, "answer.cpp.o")

    def test_proto_to_flatbuffers(self):
        self.write("ya.make", "LIBRARY()\nPROTO2FBS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "answer.fbs")

    def test_archive_asm(self):
        self.write("value.txt", "42\n")
        for options in ("", "DONTCOMPRESS"):
            with self.subTest(options=options):
                self.write("ya.make", f"LIBRARY()\nCOPY_FILE(value.txt generated.txt)\nARCHIVE_ASM(NAME answer {options} value.txt generated.txt)\nEND()\n")
                graph = self.graph()
                self.node(graph, "answer.rodata")

    def test_copy_files(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT} {PROJECT}/src/sub)\nCOPY(sub/answer.cpp FROM src DESTINATION generated KEEP_DIR_STRUCT AUTO WITH_CONTEXT RESULT COPIED)\nEND()\n")
        self.write("src/sub/answer.cpp", '#include "dependency.h"\nint answer() { return 42; }\n')
        self.write("src/sub/dependency.h", "#pragma once\n")
        self.node(self.graph(), "generated/sub/answer.cpp")

    def test_files_feed_generator_inputs(self):
        self.write("ya.make", "RECURSE(queries)\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        self.write("schema/ya.make", "UNION()\nFILES(schema.sql)\nEND()\n")
        self.write("schema/schema.sql", "create table answer (value int);\n")
        self.write("queries/ya.make", f"GO_LIBRARY()\nFILES(config.yaml)\nRUN_PROGRAM({PROJECT}/generator -f ${{BINDIR}}/config.yaml IN ${{BINDIR}}/config.yaml IN ${{BINDIR}}/../schema/schema.sql OUT answer.go)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("queries/config.yaml", "version: 2\n")
        graph = self.graph()
        config = self.node(graph, "queries/config.yaml")
        schema = next(node for node in graph["graph"] if node["outputs"] == [f"$(B)/{PROJECT}/schema/schema.sql"])
        generated = self.node(graph, "queries/answer.go")
        self.assertIn(f"$(S)/{PROJECT}/queries/config.yaml", config["inputs"])
        for producer in (config, schema):
            self.assertIn(producer["outputs"][0], generated["inputs"])
            self.assertIn(producer["uid"], generated["deps"])

    def test_files_without_consumer(self):
        self.write("ya.make", "LIBRARY(answer)\nFILES(value.txt)\nSRCS(answer.cpp)\nEND()\n")
        self.write("value.txt", "42\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "libanswer.a")
        self.assertFalse(any(f"$(B)/{PROJECT}/value.txt" in node["outputs"] for node in graph["graph"]))
