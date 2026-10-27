from tst.make.lib import MakeTest, PROJECT


class ConfReactiveTest(MakeTest):
    def test_jni_dll_inherits_link_settings(self):
        self.write("ya.make", "DLL_JAVA(answer)\nJDK_VERSION(17)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        args = [arg for cmd in self.node(self.graph(), "libanswer.so")["cmds"] for arg in cmd["cmd_args"]]
        self.assertEqual(args.count("-Wl,--gdb-index"), 1)
        self.assertEqual(args.count("-Wl,-z,notext"), 1)
        self.assertNotIn("-Wl,-Bsymbolic", args)

    def test_symbolic_flag_is_scoped_to_module_inheritance(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for declaration, output, expected in (("DLL(answer)", "libanswer.so", 0), ("PY_ANY_MODULE(answer 3)", "answer.so", 1), ("PY2MODULE(answer)", "answer.so", 1), ("PY3MODULE(answer)", "answer.so", 1), ("UDF_BASE(answer)", "libanswer.so", 1), ("YQL_UDF(answer)", "libanswer.so", 1)):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}\nSRCS(answer.cpp)\nEND()\n")
                args = [arg for cmd in self.node(self.graph(), output)["cmds"] for arg in cmd["cmd_args"]]
                self.assertEqual(args.count("-Wl,-Bsymbolic"), expected)

    def test_sandbox_task_binary_inherits_link_settings(self):
        self.write("__init__.py", "\n")
        for module in ("SANDBOX_PY3_TASK", "SANDBOX_PY23_TASK", "SANDBOX_PY23_3_TASK"):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}(answer)\nPY_SRCS(__init__.py)\nEND()\n")
                args = [arg for cmd in self.node(self.graph(), "answer")["cmds"] for arg in cmd["cmd_args"]]
                self.assertEqual(args.count("-Wl,--gdb-index"), 1)

    def test_dynamic_cuda_rpath_does_not_reach_host_tool(self):
        self.write("ya.make", "PROGRAM(answer)\nUSE_DYNAMIC_CUDA()\nSRCS(main.cpp parser.y)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("parser.y", "%token NUMBER\n%%\nexpr: NUMBER;\n%%\n")
        graph = self.graph("--musl", "--host-platform-flag=MUSL=yes", "--target-platform=default-linux-aarch64", "-DOS_SDK=local", "--host-platform-flag=OS_SDK=local", "--host-platform=default-linux-x86_64")
        bison = next(node for node in graph["graph"] if "$(B)/contrib/tools/bison/bison" in node["outputs"])
        for node, expected in ((self.node(graph, "answer"), 1), (bison, 0)):
            args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
            self.assertEqual(args.count("-Wl,-rpath,$ORIGIN"), expected)

    def test_dynamic_peer_rpath_is_rendered_once(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("shared/ya.make", f"DYNAMIC_LIBRARY(shared)\nDYNAMIC_LIBRARY_FROM({PROJECT}/static)\nEXPORTS_SCRIPT(symbols.exports)\nSET_APPEND(RPATH_GLOBAL -Wl,-rpath,e2e)\nEND()\n")
        self.write("shared/symbols.exports", "C answer\n")
        self.write("static/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("static/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        for setter in ("ENABLE(SO_OUTPUTS)", "WITH_DYNAMIC_LIBS()"):
            with self.subTest(setter=setter):
                self.write("ya.make", f"PROGRAM(answer)\n{setter}\nPEERDIR({PROJECT}/shared)\nSRCS(main.cpp)\nEND()\n")
                args = [arg for cmd in self.node(self.graph(), "answer")["cmds"] for arg in cmd["cmd_args"]]
                self.assertEqual(args.count("-Wl,-rpath,e2e"), 1)

    def test_go_program_link_settings(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        node = self.node(self.graph(), "answer")
        args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertEqual(args.count("-Wl,--no-as-needed"), 1)
        self.assertEqual(args.count("-Wl,--gdb-index"), 1)

    def test_cuda_nvprune_macro(self):
        self.write("ya.make", "PROGRAM(answer)\nNO_CUDA_NVPRUNE()\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        node = self.node(self.graph(), "answer")
        args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertEqual(args.count("-L/CUDA:PRUNE=False"), 1)

    def test_link_flags_order_and_inheritance(self):
        self.write("ya.make", "PROGRAM(answer)\nNO_CUDA_NVPRUNE()\nLDFLAGS(-Le2e)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        node = self.node(self.graph(), "answer")
        args = next(cmd["cmd_args"] for cmd in node["cmds"] if "-Le2e" in cmd["cmd_args"])
        self.assertEqual(args.count("-Wl,--no-as-needed"), 1)
        self.assertLess(args.index("-Wl,--gdb-index"), args.index("-L/CUDA:PRUNE=False"))
        self.assertLess(args.index("-L/CUDA:PRUNE=False"), args.index("-Le2e"))

    def test_shared_library_link_flags(self):
        self.write("ya.make", "DLL(answer)\nNO_CUDA_NVPRUNE()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "libanswer.so")
        args = next(cmd["cmd_args"] for cmd in node["cmds"] if "-L/CUDA:PRUNE=False" in cmd["cmd_args"])
        self.assertEqual(args.count("-Wl,--no-as-needed"), 1)
        self.assertEqual(args.count("-L/CUDA:PRUNE=False"), 1)
        self.assertLess(args.index("-Wl,--gdb-index"), args.index("-L/CUDA:PRUNE=False"))
        self.assertLess(args.index("-L/CUDA:PRUNE=False"), args.index("-Wl,-z,notext"))

    def test_conditional_append_is_reset(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for setters, expected in (
            ("DISABLE(CUDA_NVPRUNE)\nDISABLE(CUDA_NVPRUNE)", 1),
            ("DISABLE(CUDA_NVPRUNE)\nENABLE(CUDA_NVPRUNE)", 0),
            ("SET(CUDA_NVPRUNE no)", 1),
            ('SET_LIST_PRE_SUF(CUDA_NVPRUNE n "" o)', 1),
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
