import json
import os
from unittest.mock import patch

from tst.make.lib import MakeTest, PROJECT


class RemainingContextsTest(MakeTest):
    def test_malformed_command_names(self):
        for command in ('BRO"KEN"', r'BRO\KEN', 'BRO"-part@domain"', '"UNKNOWN"', '@UNKNOWN'):
            with self.subTest(command=command):
                self.write("ya.make", f"LIBRARY()\n{command}()\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_global_compiler_expressions(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in (
            'SET(WORDS -DONE=1 -DTWO=2)\nCFLAGS(GLOBAL "$WORDS")',
            'CFLAGS(GLOBAL -DUTF8=ответ -DPRICE=$5 -DQUOTED=\'value\')',
            'CFLAGS(GLOBAL -DFIRST=1 &&)',
            r'CFLAGS(GLOBAL -DBACKSLASH=\\)',
        ):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_help(self):
        result = self.make("--help", dump_graph=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--keep-going", result.stdout + result.stderr)

    def test_agent_detection_without_environment_markers(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        env = dict(os.environ)
        for key in ("OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT", "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI", "QWEN_CODE", "REPL_ID", "YA_AUTO_DETECT_AGENT"):
            env.pop(key, None)
        with patch.dict(os.environ, env, clear=True):
            self.graph("-t")

    def test_configured_nonheader(self):
        self.write("ya.make", "LIBRARY()\nSET(ANSWER 42)\nCONFIGURE_FILE(value.in generated.inc)\nRESOURCE(generated.inc /value)\nEND()\n")
        self.write("value.in", "answer=@ANSWER@\n")
        self.node(self.graph(), "generated.inc")

    def test_invalid_tools_configuration(self):
        self.write("ya.make", f"PY3_LIBRARY()\nYA_TOOLS_CONF({PROJECT}/conf)\nEND()\n")
        self.write("conf/ya.conf.json", json.dumps({"bottles": {"ignored": 42, "inline": {"formula": {}}}, "simple_tools": {"clang": {"resource": "clang"}, "missing": {}}}))
        self.write("conf/tools/tools/clang.ya_tool", json.dumps({"tool": {"type": "simple", "definition": {}}}))
        self.write("conf/tools/tools/complex.ya_tool", json.dumps({"tool": {"type": "complex", "definition": {}}}))
        self.assertNotEqual(self.make().returncode, 0)

    def test_python_gazetteer_proto(self):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.gztproto)\nEND()\n")
        self.write("proto/answer.gztproto", "package e2e; message Answer { optional int32 value = 1; }\n")
        self.node(self.graph(), "proto/answer.proto")

    def test_python_native_proto_tags(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("proto/ya.make", "PROTO_SCHEMA()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        for selector in ("PYTHON2_MODULE", "PYTHON3_MODULE"):
            with self.subTest(selector=selector):
                self.write("ya.make", f"LIBRARY()\n{selector}()\nPEERDIR({PROJECT}/proto)\nSRCS(answer.cpp)\nEND()\n")
                self.graph()

    def test_flatbuffers_multiple_consumers(self):
        self.write("ya.make", "RECURSE(native java)\n")
        self.write("native/ya.make", f"PROGRAM(native)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("java/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("java/Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"FBS_LIBRARY()\nADDINCL({PROJECT}/schema ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema)\nSRCS(answer.fbs native.cpp)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; }\n")
        self.write("schema/native.cpp", '#include "answer.fbs.h"\nint answer() { return 42; }\n')
        graph = self.graph()
        for output in ("native/native", "java/answer.tar"):
            self.assertIn(self.node(graph, output)["uid"], graph["result"])

    def test_unquoted_global_flag_expression(self):
        self.write("ya.make", 'LIBRARY()\nCFLAGS(GLOBAL -DVALUE=${quo:"a b"} -DCOMMA=${comma:"unused"})\nSRCS(answer.cpp)\nEND()\n')
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
