from tst.make.lib import MakeTest, PROJECT


class UnittestForContextsTest(MakeTest):
    def autocheck_graph(self):
        return self.graph("--dist", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--target-platform-tests", "--output-only-tests", "--add-modules-to-results", "-DAUTOCHECK=yes", "--target-platform-flag=AUTOCHECK=yes", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes", "--test-size=small", "--test-size=medium", env={"YA_STRIP_IDLE_BUILD_RESULTS": "0"})

    def test_recursed_tests_for_final_targets(self):
        self.write("ya.make", "RECURSE(program suite)\n")
        self.write("program/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\nRECURSE_FOR_TESTS(ut)\n")
        self.write("program/main.cpp", "int main() { return 0; }\n")
        self.write("suite/ya.make", "UNITTEST()\nSRCS(parent_ut.cpp)\nEND()\nRECURSE_FOR_TESTS(ut)\n")
        test_source = '#include <library/cpp/testing/unittest/registar.h>\n#include "answer.h"\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT_VALUES_EQUAL(ANSWER, 42); } }\n'
        self.write("suite/parent_ut.cpp", test_source)
        for parent in ("program", "suite"):
            self.write(f"{parent}/answer.h", "#define ANSWER 42\n")
            self.write(f"{parent}/child_ut.cpp", test_source)
            self.write(f"{parent}/ut/ya.make", f"UNITTEST_FOR({PROJECT}/{parent})\nSIZE(MEDIUM)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nSRCS(child_ut.cpp)\nEND()\n")
        graph = self.autocheck_graph()
        for parent in ("program", "suite"):
            with self.subTest(parent=parent):
                compiled = self.node(graph, f"{parent}/ut/__/child_ut.cpp.o")
                self.assertIn(f"$(S)/{PROJECT}/{parent}/child_ut.cpp", compiled["inputs"])
                self.assertIn(f"$(S)/{PROJECT}/{parent}/answer.h", compiled["inputs"])
                self.node(graph, f"{parent}/ut/{PROJECT}-{parent}-ut")
                for chunk in range(2):
                    self.node(graph, f"{parent}/ut/test-results/unittest/chunk{chunk}/meta.json")

    def test_filtered_tests_keep_binaries_and_proto_peers(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\nRECURSE_FOR_TESTS(small large)\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("sizes.inc", "IF(SANITIZER_TYPE)\nSIZE(MEDIUM)\nENDIF()\n")
        self.write("large.inc", "TAG(ya:fat)\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        for size in ("small", "large"):
            metadata = f"INCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/sizes.inc)" if size == "small" else f"SIZE(LARGE)\nINCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/large.inc)"
            self.write(f"{size}/ya.make", f"UNITTEST_FOR({PROJECT})\n{metadata}\nFORK_SUBTESTS()\nPEERDIR({PROJECT}/proto)\nSRCS(test.cpp)\nEND()\n")
            self.write(f"{size}/test.cpp", f'#include <library/cpp/testing/unittest/registar.h>\n#include <{PROJECT}/proto/answer.pb.h>\nY_UNIT_TEST_SUITE(Answer) {{ Y_UNIT_TEST(Value) {{ UNIT_ASSERT_VALUES_EQUAL(e2e::Answer().value(), 0); }} }}\n')
        graph = self.autocheck_graph()
        self.node(graph, "proto/answer.pb.cc")
        for size in ("small", "large"):
            link = self.node(graph, f"{size}/{PROJECT}-{size}")
            self.assertIn(link["uid"], graph["result"])
            self.assertTrue(any(path.endswith(".a") and f"/{PROJECT}/proto/" in path for path in link["inputs"]))
        for chunk in range(10):
            self.node(graph, f"small/test-results/unittest/chunk{chunk}/meta.json")
        self.assertFalse(any(f"/{PROJECT}/large/test-results/unittest/" in output for node in graph["graph"] for output in node["outputs"]))

    def test_explicit_incompatible_peer_keeps_consumer(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/tool)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.assertNotEqual(self.make().returncode, 0)
        graph = self.graph("-k")
        link = self.node(graph, "answer")
        self.assertNotIn(f"$(B)/{PROJECT}/tool/tool", link["inputs"])

    def test_explicit_unittest_for_peer_is_diagnosed(self):
        self.write("ya.make", f"UNITTEST_FOR({PROJECT}/program)\nPEERDIR({PROJECT}/program)\nSRCS(test.cpp)\nEND()\n")
        self.write("program/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("program/main.cpp", "int main() { return 0; }\n")
        self.write("test.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n')
        self.assertNotEqual(self.make("-t").returncode, 0)
        self.node(self.graph("-k", "-t"), "test-results/unittest/meta.json")

