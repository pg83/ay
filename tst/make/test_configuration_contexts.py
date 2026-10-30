import json

from tst.make.lib import MakeTest, PROJECT


class ConfigurationContextsTest(MakeTest):
    def test_quoted_condition_operands(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for expression in ('prefix"suffix" == prefixsuffix', "prefix'suffix' == prefixsuffix", r'first\ second == first\ second', 'name"-part@domain" != other'):
            with self.subTest(expression=expression):
                self.write("ya.make", f"LIBRARY()\nIF({expression})\nSRCS(answer.cpp)\nELSE()\nSRCS(answer.cpp)\nENDIF()\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_json_command_escaping(self):
        self.write("generate.py", "print(42)\n")
        for value in ('текст\u2028продолжение\u2029конец', 'start\b\f\tend', 'quote\\slash'):
            with self.subTest(value=value):
                self.write("ya.make", f'LIBRARY()\nRUN_PYTHON3(generate.py "{value}" STDOUT value.txt)\nRESOURCE(value.txt /value)\nEND()\n')
                self.node(self.graph(), "value.txt")

    def test_obsolete_ya_conf_json_macro(self):
        self.write("ya.make", "PY3_LIBRARY()\nYA_CONF_JSON(config.json)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "answer = 42\n")
        self.write("config.json", json.dumps({"tool_chain": {"test": {"tools": {"answer": {"bottle": "answer"}}}}, "bottles": {"answer": {"formula": "__ay_e2e__/formula.json"}}}))
        self.write("formula.json", "{}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_obsolete_ya_conf_json_with_missing_formula(self):
        self.write("ya.make", "PY3_LIBRARY()\nYA_CONF_JSON(__ay_e2e__/config.json)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "answer = 42\n")
        self.write("config.json", json.dumps({
            "bottles": {
                "missing": {"formula": "__ay_e2e__/missing.json"},
                "duplicate": {"formula": "__ay_e2e__/missing.json"},
                "existing": {"formula": "__ay_e2e__/existing.json"},
                "repeated_suffix": {"formula": "__ay_e2e__/existing.jsonx.json"},
                "other_extension": {"formula": "ignored.txt"},
                "empty": {"formula": ""},
                "inline": {"formula": {}},
            },
            "simple_tools": {
                "missing": {"resource": "ay_e2e_missing_resource"},
                "present": {"resource": "ay_e2e_fixture"},
                "duplicate": {"resource": "ay_e2e_fixture"},
                "empty": {"resource": ""},
                "inline": {"resource": {}},
            },
        }, indent=2))
        self.write("existing.json", "{}\n")
        self.write("existing.jsonx.json", "{}\n")
        self.write_tree("build/external_resources/ay_e2e_fixture/resources.json", "{}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_obsolete_ya_conf_json_with_truncated_document(self):
        self.write("ya.make", "PY3_LIBRARY()\nYA_CONF_JSON(__ay_e2e__/config.json)\nEND()\n")
        prefix = '{"bottles":{"missing":{"formula":"__ay_e2e__/missing.json"}},'
        suffixes = ('"formula"', '"formula" ', '"formula":', '"formula": ',
                    '"formula": "', '"formula": "unfinished.', '"formula": "unfinished.jso',
                    '"resource"', '"resource" ', '"resource":', '"resource": ',
                    '"resource": "', '"resource": "unfinished')
        for suffix in suffixes:
            with self.subTest(suffix=suffix):
                self.write("config.json", prefix + suffix)
                self.assertNotEqual(self.make().returncode, 0)

    def test_generated_header_include_flags(self):
        self.write("ya.make", "LIBRARY()\nADDINCL(${ARCADIA_BUILD_ROOT}/__ay_e2e__)\nCOPY_FILE_WITH_CONTEXT(value.h generated/value.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("value.h", "#define ANSWER 42\n")
        self.write("answer.cpp", '#include "generated/value.h"\nint answer() { return ANSWER; }\n')
        self.node(self.graph(), "generated/value.h")

    def test_dynamic_dependency_chain(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/middle)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("middle/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/shared)\nSRCS(middle.cpp)\nEND()\n")
        self.write("middle/middle.cpp", "int middle() { return 42; }\n")
        self.write("shared/ya.make", f"DYNAMIC_LIBRARY(shared)\nDYNAMIC_LIBRARY_FROM({PROJECT}/leaf)\nEXPORTS_SCRIPT(symbols.exports)\nEND()\n")
        self.write("shared/symbols.exports", "C shared\n")
        self.write("leaf/ya.make", "LIBRARY()\nSRCS(shared.cpp)\nEND()\n")
        self.write("leaf/shared.cpp", 'extern "C" int shared() { return 42; }\n')
        self.node(self.graph(), "answer")

    def test_java_tagged_proto(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.node(self.graph(), "answer.jar")

    def test_tasklet_proto_plugin(self):
        self.write("ya.make", "PROTO_LIBRARY()\nTASKLET()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.node(self.graph(), "answer.tasklet.h")
