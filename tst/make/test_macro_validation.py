from tst.make.lib import MakeTest


class MacroValidationTest(MakeTest):
    def test_yaff_namespace_and_schema_arguments(self):
        macros = ("YAFF(NAMESPACE)", "YAFF(unexpected)", "YAFF_SCHEMA()", "YAFF_SCHEMA(first second third)")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{macro}\nSRCS(answer.proto)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_cpp_proto_plugin_tail_arguments(self):
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        self.write("plugin/ya.make", "PY3_PROGRAM(plugin)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("plugin/main.py", "import sys\nsys.stdin.buffer.read()\n")
        for tail in ("EXTRA_OUT_FLAG", "EXTRA_OUT_FLAG first EXTRA_OUT_FLAG second", "UNEXPECTED"):
            with self.subTest(tail=tail):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nCPP_PROTO_PLUGIN(extra {self.project_name}/plugin .extra.h {tail})\nSRCS(answer.proto)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_simd_source_requires_a_file(self):
        self.write("ya.make", "LIBRARY()\nSRC_C_AVX2()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_generator_tool_cannot_expand_to_an_empty_value(self):
        for macro in ("RUN_PROGRAM", "RUN_PY3_PROGRAM"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSET(E2E_EMPTY)\n{macro}(${{E2E_EMPTY}})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_allocator_and_directory_declaration_arguments(self):
        macros = ("ALLOCATOR()", "ALLOCATOR(SYSTEM SYSTEM)", "ALLOCATOR(E2E_UNKNOWN)",
                  "DECLARE_IN_DIRS()", "DECLARE_IN_DIRS(DATA)", "DECLARE_IN_DIRS(DATA *.txt unexpected)")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_python_source_keyword_arguments(self):
        macros = ("PY_SRCS(NAMESPACE)", "PY_SRCS(CYTHON_DIRECTIVE)", "PY_SRCS(unsupported.txt)")
        self.write("unsupported.txt", "42\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY3_LIBRARY()\n{macro}\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_forbidden_python_test_source_arguments(self):
        self.write("answer.py", "VALUE = 42\n")
        self.write("__main__.py", "print(42)\n")
        for args in ("NAMESPACE e2e answer.py", "TOP_LEVEL answer.py", "__main__.py"):
            with self.subTest(args=args):
                self.write("ya.make", f"PY3TEST()\nTEST_SRCS({args})\nEND()\n")
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_malformed_make_calls(self):
        sources = (
            "LIBRARY()\nSRCS", "LIBRARY()\nSRCS answer.cpp",
            "LIBRARY()\nSRCS 42", "LIBRARY()\nSRCS(answer.cpp",
            'LIBRARY()\nSRCS("unterminated', "LIBRARY()\nIF yes",
            "LIBRARY()\nINCLUDE_ONCE(yes no)\nEND()\n",
            "LIBRARY()\nINCLUDE()\nEND()\n",
            "LIBRARY()\nINCLUDE(/ay_e2e_missing.inc)\nEND()\n",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for source in sources:
            with self.subTest(source=source):
                self.write("ya.make", source)
                self.assertNotEqual(self.make().returncode, 0)

    def test_indirect_include_cycle(self):
        self.write("ya.make", "LIBRARY()\nINCLUDE(first.inc)\nEND()\n")
        self.write("first.inc", "INCLUDE_ONCE(no)\nINCLUDE(second.inc)\n")
        self.write("second.inc", "INCLUDE_ONCE(no)\nINCLUDE(first.inc)\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("first.inc", result.stderr)
        self.assertIn("second.inc", result.stderr)

    def test_mlaunch_macro_arguments(self):
        macros = (
            "GOLAUNCH_FUNCTION()", "GOLAUNCH_FUNCTION(first second)",
            "GOLAUNCH_OVERRIDE(target)", "GENERATE_GOLAUNCH_CALLBACKS()",
            "NODE_BUILDER(Build NAME)", "NODE_BUILDER(Build UNKNOWN value)",
            "NODE_BUILDER(Build NAME first NAME second TARGET app HEADER value.h)",
            "NODE_BUILDER(Build NAME first)",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_archive_keyword_arguments(self):
        macros = (
            "ARCHIVE(NAME)", "ARCHIVE_BY_KEYS(NAME)",
            "ARCHIVE_ASM(NAME first NAME second value.txt)",
            "ARCHIVE_ASM(NAME)", "ARCHIVE_ASM(NAME DONTCOMPRESS value.txt)",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("value.txt", "42\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_archives_can_have_no_input_files(self):
        for macro in ("ARCHIVE(NAME empty.inc)", "ARCHIVE_BY_KEYS(NAME empty.inc KEYS key)",
                      "ARCHIVE_BY_KEYS(NAME empty.inc KEYS)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nRESOURCE(empty.inc /data)\nEND()\n")
                self.node(self.graph(), "empty.inc")

    def test_archive_repeated_compression_flag(self):
        self.write("value.txt", "42\n")
        self.write("ya.make", "LIBRARY()\nARCHIVE_ASM(NAME result DONTCOMPRESS DONTCOMPRESS value.txt)\nEND()\n")
        node = self.node(self.graph(), "result.rodata")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args.count("-p"), 1)

    def test_luajit_archive_can_have_no_lua_files(self):
        self.write("ya.make", "LIBRARY()\nLJ_21_ARCHIVE(ignored.txt)\nRESOURCE(LuaScripts.inc /bytecode LuaSources.inc /sources)\nEND()\n")
        graph = self.graph()
        for output in ("LuaScripts.inc", "LuaSources.inc"):
            self.node(graph, output)

    def test_missing_required_macro_arguments(self):
        macros = (
            "SET()", "JOIN_SRCS()",
            "GENERATE_ENUM_SERIALIZATION()", "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION()",
            "GENERATE_ENUM_SERIALIZATION_WITH_HEADER()", "GENERATE_ENUM_SERIALIZATION_NOUTF()",
            "CREATE_BUILDINFO_FOR()", "RUN_ANTLR4_CPP()", "RUN_ANTLR4_CPP_SPLIT(grammar.g4)",
            "RUN_ANTLR4_PYTHON2()", "RUN_ANTLR4_PYTHON3()", "RUN_ANTLR()", "RUN_ANTLR4()",
            "RUN_LUA()", "SPLIT_CODEGEN(tool)", "BASE_CODEGEN(tool)", "STRUCT_CODEGEN()",
            "BUILDWITH_RAGEL6()", "PREPARE_INDUCED_DEPS()", "YA_CONF_JSON()",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_enum_serialization_for_multiple_headers(self):
        self.write("ya.make", "LIBRARY()\nCPP_ENUMS_SERIALIZATION(NAMESPACE e2e first.h second.h)\nEND()\n")
        for name in ("first", "second"):
            self.write(name + ".h", f"#pragma once\nenum class {name.title()} {{ Value = 42 }};\n")
        graph = self.graph()
        for name in ("first", "second"):
            self.node(graph, name + ".h_serialized.h")

    def test_empty_source_directory_list_preserves_local_sources(self):
        self.write("ya.make", "LIBRARY()\nSRCDIR()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "answer.cpp.o")

    def test_include_once_can_be_disabled(self):
        self.write("ya.make", "INCLUDE_ONCE(no)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "answer.cpp.o")
