import json

from tst.make.lib import MakeTest, PROJECT


class MacroContextsTest(MakeTest):
    def test_addincl_scopes(self):
        self.write("include/value.h", "#define VALUE 42\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        paths = [f"ONE_LEVEL {PROJECT}/include", f"FOR c {PROJECT}/include", f"GLOBAL FOR c {PROJECT}/include"]
        paths += [f"{scope}FOR {language} {PROJECT}/include" for language in ("proto", "flatc", "cython", "swig", "asm", "sc") for scope in ("", "GLOBAL ")]
        for path in paths:
            with self.subTest(path=path):
                self.write("ya.make", f"LIBRARY()\nADDINCL({path})\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_list_prefix_and_suffix(self):
        self.write("ya.make", 'LIBRARY()\nSET_LIST_PRE_SUF(SOURCES src/ .cpp first second)\nSRCS(${SOURCES})\nEND()\n')
        self.write("src/first.cpp", "int first() { return 1; }\n")
        self.write("src/second.cpp", "int second() { return 2; }\n")
        graph = self.graph()
        self.node(graph, "_/src/first.cpp.o")
        self.node(graph, "_/src/second.cpp.o")

    def test_quoted_word_segments(self):
        self.write("ya.make", 'LIBRARY()\r\nSET(NAME "answer")\r\nSET(SUFFIX .cpp)\r\nSRCS(${NAME}${SUFFIX})\r\nCFLAGS(-DE2E="42")\r\nEND()\r\n')
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "answer.cpp.o")

    def test_condition_operators(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for expression in ('VALUE >= 42', 'VALUE <= 42', 'VALUE != 41', 'VALUE == 42 AND NOT DISABLED', 'VALUE == 41 OR VALUE == 42'):
            with self.subTest(expression=expression):
                self.write("ya.make", f"LIBRARY()\nSET(VALUE 42)\nSET(DISABLED no)\nIF({expression})\nSRCS(answer.cpp)\nENDIF()\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_tools_configuration_resources(self):
        self.write("ya.make", f"PY3_LIBRARY()\nYA_TOOLS_CONF({PROJECT}/conf)\nEND()\n")
        formula = f"{PROJECT}/conf/formula.json"
        self.write("conf/ya.conf.json", json.dumps({"bottles": {"e2e": {"formula": formula}}, "simple_tools": {}}))
        self.write("conf/formula.json", "{}\n")
        self.write("conf/tools/internal/tiers.json", "{}\n")
        self.write("conf/tools/tools/e2e.ya_tool", json.dumps({"tool": {"type": "simple", "definition": {"formula": formula}}}))
        self.write("conf/tools/toolchains/e2e.ya_toolchain", json.dumps({"bottles": {"e2e": {"formula": formula}}}))
        inputs = {path for node in self.graph()["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/conf/ya.conf.json", inputs)
        self.assertIn(f"$(S)/{formula}", inputs)

    def test_lua_generator(self):
        self.write("ya.make", "LIBRARY()\nRUN_LUA(generate.lua IN value.txt STDOUT answer.cpp)\nEND()\n")
        self.write("generate.lua", 'print("int answer() { return 42; }")\n')
        self.write("value.txt", "42\n")
        self.node(self.graph(), "answer.cpp")

    def test_lua_runtimes(self):
        self.write("answer.lua", "return 42\n")
        for macro in ("COMPILE_LUA", "COMPILE_LUA_OPENRESTY"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}(answer.lua)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.lua" in node["inputs"] for node in graph["graph"]))

    def test_debian_version(self):
        self.write("ya.make", "LIBRARY()\nDEB_VERSION(changelog)\nRESOURCE(DebianVersion.h /version)\nEND()\n")
        self.write("changelog", "answer (1.0) unstable; urgency=low\n\n  * Answer.\n\n -- Example <example@example.com>  Sun, 25 Oct 2026 00:00:00 +0000\n")
        self.node(self.graph(), "DebianVersion.h")

    def test_python_any_module(self):
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("ya.make", "PY_ANY_MODULE(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.assertTrue(any(path.endswith(".so") for node in self.graph()["graph"] for path in node["outputs"]))

    def test_proto_schema(self):
        self.write("ya.make", "PROTO_SCHEMA(answer)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(path.endswith(".protodesc") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_protobuf_search_paths(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nPROTO_ADDINCL(GLOBAL {PROJECT}/include)\nADDINCL({PROJECT}/include)\nPEERDIR({PROJECT}/include)\nRESOLVE_PROTO()\nSRCS(answer.proto)\nEND()\n")
        self.write("include/ya.make", "PROTO_LIBRARY()\nSRCS(value.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\npackage e2e;\nimport "value.proto";\nmessage Answer { optional Value value = 1; }\n')
        self.write("include/value.proto", 'syntax = "proto2"; package e2e; message Value { optional int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/include/value.proto" in node["inputs"] for node in graph["graph"]))
