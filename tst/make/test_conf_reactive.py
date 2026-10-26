from tst.make.lib import MakeTest


class ConfReactiveTest(MakeTest):
    def test_cuda_nvprune_macro(self):
        self.write("ya.make", "PROGRAM(answer)\nNO_CUDA_NVPRUNE()\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        node = self.node(self.graph(), "answer")
        args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertEqual(args.count("-L/CUDA:PRUNE=False"), 1)

    def test_conditional_append_is_reset(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for setters, expected in (
            ("DISABLE(CUDA_NVPRUNE)\nDISABLE(CUDA_NVPRUNE)", 1),
            ("DISABLE(CUDA_NVPRUNE)\nENABLE(CUDA_NVPRUNE)", 0),
            ("SET(CUDA_NVPRUNE no)", 1),
            ("SET(CUDA_NVPRUNE no)\nSET_APPEND(CUDA_NVPRUNE)", 0),
            ("SET(CUDA_NVPRUNE)\nDEFAULT(CUDA_NVPRUNE no)", 0),
        ):
            with self.subTest(setters=setters):
                self.write("ya.make", f"PROGRAM(answer)\n{setters}\nSRCS(main.cpp)\nEND()\n")
                node = self.node(self.graph(), "answer")
                args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
                self.assertEqual(args.count("-L/CUDA:PRUNE=False"), expected)

    def test_global_condition_and_otherwise(self):
        self.write("yes.cpp", "int answer() { return 42; }\n")
        self.write("no.cpp", "int other() { return 17; }\n")
        for setters, chosen in (
            ("SET(SDC_PLATFORM car-jp6)", "yes"),
            ("SET(SDC_PLATFORM car-jp6)\nSET(SDC_PLATFORM x86_64)", "no"),
        ):
            with self.subTest(setters=setters):
                self.write("ya.make", f'LIBRARY()\n{setters}\nIF(JETSON == "yes")\nSRCS(yes.cpp)\nELSE()\nSRCS(no.cpp)\nENDIF()\nEND()\n')
                self.node(self.graph(), chosen + ".cpp.o")

    def test_derived_jdk_condition(self):
        self.write("yes.cpp", "int answer() { return 42; }\n")
        self.write("no.cpp", "int other() { return 17; }\n")
        self.write("ya.make", 'LIBRARY()\nSET(JDK_REAL_VERSION 26)\nIF(KOTLINC_ENABLE_NATIVE_ACCESS != "")\nSRCS(yes.cpp)\nELSE()\nSRCS(no.cpp)\nENDIF()\nEND()\n')
        self.node(self.graph(), "yes.cpp.o")

    def test_conditional_append_preserves_originals(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for setters, expected in (
            ("SET(LDFLAGS -Le2e)\nDISABLE(CUDA_NVPRUNE)", 1),
            ("DISABLE(CUDA_NVPRUNE)\nSET_APPEND(LDFLAGS -Le2e)\nENABLE(CUDA_NVPRUNE)", 0),
        ):
            with self.subTest(setters=setters):
                self.write("ya.make", f"PROGRAM(answer)\n{setters}\nSRCS(main.cpp)\nEND()\n")
                args = [arg for cmd in self.node(self.graph(), "answer")["cmds"] for arg in cmd["cmd_args"]]
                self.assertIn("-Le2e", args)
                self.assertEqual(args.count("-L/CUDA:PRUNE=False"), expected)

    def test_cuda_nvprune_in_test_multimodule(self):
        self.write("ya.make", "PY3TEST()\nNO_CUDA_NVPRUNE()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42\n")
        args = [arg for node in self.graph()["graph"] if node.get("kv", {}).get("p") == "LD" for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertIn("-L/CUDA:PRUNE=False", args)
