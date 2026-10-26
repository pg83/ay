from tst.make.lib import MakeTest, PROJECT


class ExtendedModulesTest(MakeTest):
    def test_mlaunch_component_peer(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/component)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("component/ya.make", "MLAUNCH_COMPONENT(component)\nGOLAUNCH_SRCS(component.go)\nNODE_BUILDER(Build NAME answer TARGET program HEADER value.h ROS_SPINNER_THREADS 1)\nEND()\n")
        self.write("component/component.go", "package component\nfunc Build() {}\n")
        self.node(self.graph(), "answer")

    def test_mlaunch_library_target_link(self):
        self.write("ya.make", "RECURSE(component program)\n")
        self.write("program/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/component)\nEND()\n")
        self.write("program/main.cpp", "int main() { return 0; }\n")
        self.write("component/ya.make", f"MLAUNCH_LIBRARY(component)\nGOLAUNCH_FUNCTION(Launch)\nGOLAUNCH_SRCS(component.go)\nRUNTIME_PEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("component/component.go", "package component\nfunc Launch() {}\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(GLOBAL lib.cpp)\nEND()\n")
        self.write("lib/lib.cpp", "int lib() { return 42; }\n")
        link = self.node(self.graph(), "program/answer")
        self.assertIn(f"$(B)/{PROJECT}/lib/lib{PROJECT}-lib.global.a", link["inputs"])
        self.assertNotIn(f"$(B)/{PROJECT}/component/component.pkg.fake", link["inputs"])

    def test_sandbox_variants(self):
        self.write("__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")
        for kind in ("SANDBOX_TASK", "SANDBOX_PY23_TASK", "SANDBOX_PY23_3_TASK"):
            with self.subTest(kind=kind):
                self.write("ya.make", f"{kind}(answer)\nPY_SRCS(__init__.py)\nEND()\n")
                self.graph()

    def test_test_depends_on_sandbox_task(self):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/task)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/task/answer)\nEND()\n")
        self.write("task/ya.make", "SANDBOX_PY3_TASK(answer)\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("task/__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")
        self.graph("-t")

    def test_py23_test(self):
        self.write("ya.make", "PY23_TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        self.graph("-t")

    def test_unittest_for(self):
        self.write("ya.make", f"UNITTEST_FOR({PROJECT}/lib)\nSRCS(test.cpp)\nEND()\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        self.write("test.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n')
        self.graph("-t", "-DUT_SKIP_EXCEPTIONS=yes")

    def test_udf_base(self):
        self.write("ya.make", "UDF_BASE(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.graph()

    def test_raw_antlr_commands(self):
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        for macro in ("RUN_ANTLR", "RUN_ANTLR4"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}(Answer.g4 -Dlanguage=Cpp IN Answer.g4 OUT_NOAUTO Answer.cpp CWD ${{BINDIR}})\nRESOURCE(Answer.cpp /generated)\nEND()\n")
                self.node(self.graph(), "Answer.cpp")

    def test_antlr_python_subdirectory(self):
        self.write("ya.make", "PY3_LIBRARY()\nRUN_ANTLR4_PYTHON3(Answer.g4 SUBDIR generated EXTRA_OUTS Answer.tokens VISITOR LISTENER)\nPY_SRCS(generated/AnswerParser.py)\nEND()\n")
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        self.node(self.graph(), "generated/AnswerParser.py")

    def test_python_external_headers(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for selector in ("PYTHON2_ADDINCL", "PYTHON3_ADDINCL"):
            with self.subTest(selector=selector):
                self.write("ya.make", f"LIBRARY()\n{selector}()\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph("-DUSE_ARCADIA_PYTHON=no", "-DUSE_LOCAL_PYTHON=yes", "-DPYTHON_INCLUDE=-I/usr/include/python3.13"), "answer.cpp.o")
