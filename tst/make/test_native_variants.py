from tst.make.lib import MakeTest, PROJECT


class NativeVariantsTest(MakeTest):
    def source(self, declaration="PROGRAM(answer)", macros=""):
        self.write("ya.make", f"{declaration}\n{macros}\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")

    def test_release_and_lto(self):
        self.source()
        for flags in (("-r",), ("-r", "-DTHINLTO=yes")):
            with self.subTest(flags=flags):
                self.assertIn(self.node(self.graph(*flags), "answer")["outputs"][0], [f"$(B)/{PROJECT}/answer"])

    def test_cross_architecture(self):
        self.source()
        for target in ("default-linux-aarch64", "default-darwin-arm64", "default-win-x86_64"):
            with self.subTest(target=target):
                graph = self.graph("--target-platform=" + target)
                self.assertTrue(any(path.endswith(("/answer", "/answer.exe")) for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_sanitizers(self):
        self.source()
        for sanitizer in ("address", "undefined", "thread"):
            with self.subTest(sanitizer=sanitizer):
                graph = self.graph("-DSANITIZER_TYPE=" + sanitizer)
                self.node(graph, "answer")

    def test_sanitizer_instrumentation_flags(self):
        for macros, enabled in (("SET(SANITIZER_CFLAGS -DE2E_INSTRUMENTED)", True),
                                ("NO_SANITIZE()", False)):
            with self.subTest(macros=macros):
                self.source(macros=macros)
                graph = self.graph("-DSANITIZER_TYPE=memory")
                args = self.node(graph, "main.cpp.o")["cmds"][0]["cmd_args"]
                for flag in ("-fsanitize=memory", "-Dmemory_sanitizer_enabled", "-DE2E_INSTRUMENTED"):
                    self.assertEqual(flag in args, enabled, flag)
                if enabled:
                    self.assertIn("-fno-omit-frame-pointer", args)

    def test_dll_exports(self):
        self.source("DLL(answer 1 2 PREFIX e2e)", "EXPORTS_SCRIPT(symbols.exports)")
        self.write("symbols.exports", "C main\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/symbols.exports" in node["inputs"] for node in graph["graph"]))

    def test_dynamic_library_from(self):
        self.write("ya.make", f"DYNAMIC_LIBRARY(answer)\nDYNAMIC_LIBRARY_FROM({PROJECT}/lib)\nEXPORTS_SCRIPT(symbols.exports)\nEND()\n")
        self.write("symbols.exports", "C answer\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.node(self.graph(), "libanswer.so")

    def test_cuda(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cu)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.cu" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"]) for node in graph["graph"]))

    def test_assembly(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", "section .text\nglobal answer\nanswer:\nmov eax, 42\nret\n")
        self.node(self.graph(), "answer.o")
