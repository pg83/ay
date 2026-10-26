import json

from tst.make.lib import MakeTest, PROJECT


class TestVariantsTest(MakeTest):
    def test_zero_ram_disk_requirement_cancels_previous_value(self):
        self.write("test_answer.py", "def test_answer():\n    assert 6 * 7 == 42\n")
        for size in (0, 1):
            with self.subTest(size=size):
                self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nREQUIREMENTS(ram_disk:4)\nREQUIREMENTS(ram_disk:{size})\nEND()\n")
                node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertEqual("--requires-ram-disk" in args, size > 0)

    def test_fuzzer_dictionary(self):
        self.write("ya.make", f"FUZZ()\nSRCS(fuzz.cpp)\nFUZZ_DICTS({PROJECT}/words.dict)\nEND()\n")
        self.write("fuzz.cpp", '#include <stddef.h>\n#include <stdint.h>\nextern "C" int LLVMFuzzerTestOneInput(const uint8_t*, size_t) { return 0; }\n')
        self.write("words.dict", 'answer="42"\n')
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn(f"/{PROJECT}/words.dict", commands)

    def test_exectest_dependencies(self):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/tool {PROJECT}/java)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/tool/answer)\nDATA(arcadia/{PROJECT}/data.txt)\nENV(E2E=42)\nTEST_CWD({PROJECT})\nSIZE(SMALL)\nEND()\n")
        self.write("data.txt", "42\n")
        self.write("tool/ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "print(42)\n")
        self.write("java/ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("java/Answer.java", "package e2e; public class Answer {}\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph("-t", "--cache-tests", "--tests-retries=2")
        self.node(graph, "tool/answer")
        self.node(graph, "java/answer.jar")
        self.assertTrue(any("run_exectest" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_style_json_and_yaml(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nSTYLE_JSON(DIRS data)\nSTYLE_YAML(DIRS data)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("data/answer.json", '{"answer": 42}\n')
        self.write("data/answer.yaml", "answer: 42\n")
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn("clang_format_json", commands)
        self.assertIn("yamlfmt", commands)

    def test_google_test(self):
        self.write("ya.make", "GTEST()\nSRCS(test.cpp)\nEND()\n")
        self.write("test.cpp", '#include <library/cpp/testing/gtest/gtest.h>\nTEST(Example, Answer) { EXPECT_EQ(42, 6 * 7); }\n')
        graph = self.graph("-t", "--test-size=small", "--test-traceback=long")
        self.assertTrue(any("gtest" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_fuzzer(self):
        self.write("ya.make", "FUZZ()\nSRCS(fuzz.cpp)\nFUZZ_OPTS(-max_len=16)\nEND()\n")
        self.write("fuzz.cpp", '#include <stddef.h>\n#include <stdint.h>\nextern "C" int LLVMFuzzerTestOneInput(const uint8_t*, size_t) { return 0; }\n')
        self.graph("-t")

    def test_benchmark(self):
        self.write("ya.make", "G_BENCHMARK()\nSRCS(bench.cpp)\nBENCHMARK_OPTS(--benchmark_min_time=0.01s)\nEND()\n")
        self.write("bench.cpp", '#include <benchmark/benchmark.h>\nstatic void Answer(benchmark::State& s) { for (auto _: s) { benchmark::DoNotOptimize(42); } }\nBENCHMARK(Answer);\n')
        self.graph("-t")

    def test_python_canonical_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    return 42\n")
        self.write("canondata/result.json", json.dumps({"test_answer.test_answer": {"uri": "file://answer.txt", "checksum": "a1b2"}}))
        self.write("canondata/answer.txt", "42\n")
        self.graph("-t")
