from tst.make.lib import MakeTest, PROJECT


class NativeConfigTest(MakeTest):
    def test_cuda_compilation_receives_the_requested_memory_limit(self):
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        for amount in ("8", "12.5"):
            with self.subTest(amount=amount):
                self.write("ya.make", f"LIBRARY()\nSET_CUDA_NODE_RAM_REQUIEREMENT({amount})\nSRCS(answer.cu)\nEND()\n")
                graph = self.graph()
                compiled = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.cu" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
                self.assertEqual(len(compiled), 1)
                self.assertEqual(float(compiled[0]["requirements"]["ram"]), float(amount))

    def test_copy_on_write_runtime_can_be_disabled_or_unset(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for macro, off_runtime in (("NO_COW()", True), ("SET(USE_COW)", False)):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROGRAM(answer)\n{macro}\nSRCS(main.cpp)\nEND()\n")
                inputs = self.node(self.graph(), "answer")["inputs"]
                self.assertFalse(any("/build/cow/on/" in path for path in inputs))
                self.assertEqual(any("/build/cow/off/" in path for path in inputs), off_runtime)

    def test_cpu_checks_follow_disabled_instruction_sets(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for macro in ("SET(USE_SSE4 no)", "SET(DISABLE_INSTRUCTION_SETS yes)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROGRAM(answer)\n{macro}\nCFLAGS(-DE2E_CPU_CHECK=${{CPU_CHECK}})\nSRCS(main.cpp)\nEND()\n")
                args = [arg for command in self.node(self.graph(), "main.cpp.o")["cmds"] for arg in command["cmd_args"]]
                self.assertIn("-DE2E_CPU_CHECK=no", args)

    def test_detekt_configuration_preserves_native_sources(self):
        self.write("ya.make", "LIBRARY()\nSTYLE_DETEKT()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertIn(f"$(S)/{PROJECT}/answer.cpp", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_cpu_checks_follow_explicit_module_settings(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for macro in ("SET(CPU_CHECK no)", "SET(NOUTIL yes)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROGRAM(answer)\n{macro}\nCFLAGS(-DE2E_CPU_CHECK=${{CPU_CHECK}})\nSRCS(main.cpp)\nEND()\n")
                args = [arg for command in self.node(self.graph(), "main.cpp.o")["cmds"] for arg in command["cmd_args"]]
                self.assertIn("-DE2E_CPU_CHECK=no", args)

    def test_llvm_bitcode_respects_warning_suppression(self):
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        for macro, flag in (("NO_COMPILER_WARNINGS()", "-Wno-everything"), ("NO_WSHADOW()", "-Wno-shadow")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nUSE_LLVM_BC16()\n{macro}\nLLVM_BC(answer.cpp NAME answer SYMBOLS answer)\nEND()\n")
                args = [arg for command in self.node(self.graph(), "answer.cpp.bc")["cmds"] for arg in command["cmd_args"]]
                self.assertIn(flag, args)

    def test_open_source_configuration_defines_catboost_opensource(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        args = [arg for command in self.node(self.graph("-DOPENSOURCE=yes"), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DCATBOOST_OPENSOURCE=yes", args)

    def test_declarative_scalar_macros_require_one_argument(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("VALIDATE_DATA_RESTART", "SET_CUDA_NODE_RAM_REQUIEREMENT", "PACK"):
            for args in ("", "first second"):
                with self.subTest(macro=macro, args=args):
                    self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}({args})\nEND()\n")
                    self.assertNotEqual(self.make().returncode, 0)

    def test_pack_requires_a_package_module(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nPACK(tar)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_windows_manifest_macros_validate_their_argument_count(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for call in ("WINDOWS_MANIFEST()", "WINDOWS_MANIFEST(first second)", "WINDOWS_LONG_PATH_MANIFEST(unexpected)"):
            with self.subTest(call=call):
                self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\n{call}\nEND()\n")
                self.assertNotEqual(self.make("--target-platform=default-win-x86_64").returncode, 0)

    def test_windows_manifest_expands_a_module_variable(self):
        self.write("ya.make", "PROGRAM(answer)\nSET(E2E_MANIFEST build/long-path.manifest)\nWINDOWS_MANIFEST(${E2E_MANIFEST})\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph("--target-platform=default-win-x86_64")
        executable = next(node for node in graph["graph"] if any(path.removesuffix(".exe") == f"$(B)/{PROJECT}/answer" for path in node["outputs"]))
        self.assertIn("$(S)/build/long-path.manifest", executable["inputs"])

    def test_global_c_flags_reach_linker(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nCONLYFLAGS(GLOBAL -DE2E_FIRST GLOBAL -DE2E_SECOND)\nSRCS(value.c)\nEND()\n")
        self.write("lib/value.c", "int value(void) { return 42; }\n")
        node = self.node(self.graph(), "answer")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for flag in ("-DE2E_FIRST", "-DE2E_SECOND"):
            self.assertIn(flag, args)

    def test_module_cflags_variable(self):
        self.write("ya.make", "LIBRARY()\nIF (NOT MSVC)\n    SET_APPEND(CFLAGS -Wno-strict-aliasing)\nENDIF()\nSRCS(answer.cpp value.c)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("value.c", "int value(void) { return 42; }\n")
        graph = self.graph()
        for output in ("answer.cpp.o", "value.c.o"):
            with self.subTest(output=output):
                args = self.node(graph, output)["cmds"][0]["cmd_args"]
                self.assertIn("-Wno-strict-aliasing", args)
                self.assertLess(args.index("-Wno-strict-aliasing"), args.index("-msse2"))

    def test_windows_manifest(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for macros in ("WINDOWS_LONG_PATH_MANIFEST()", "WINDOWS_MANIFEST(build/long-path.manifest)"):
            with self.subTest(macros=macros):
                self.write("ya.make", f"PROGRAM(answer)\n{macros}\nSRCS(main.cpp)\nEND()\n")
                graph = self.graph("--target-platform=default-win-x86_64")
                node = next(node for node in graph["graph"] if any(path.removesuffix(".exe") == f"$(B)/{PROJECT}/answer" for path in node["outputs"]))
                self.assertIn("$(S)/build/long-path.manifest", node["inputs"])

    def test_compiler_features(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macros in (
            "NO_COW()\nNO_NEED_CHECK()\nNO_COMPILER_WARNINGS()",
            "ENABLE(UBSAN)\nNO_SANITIZE()",
            "CXXFLAGS(GLOBAL -DE2E=42)\nCFLAGS(GLOBAL -DE2E_GLOBAL=1)",
            "NO_WSHADOW()\nNO_OPTIMIZE()",
        ):
            with self.subTest(macros=macros):
                self.write("ya.make", f"LIBRARY()\n{macros}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_module_reuse_and_tag_selection(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/left {PROJECT}/right)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        for name in ("left", "right"):
            self.write(name + "/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/shared)\nSRCS({name}.cpp)\nEND()\n")
            self.write(name + "/" + name + ".cpp", f"int {name}() {{ return 42; }}\n")
        self.write("shared/ya.make", "LIBRARY()\nSRCS(shared.cpp)\nEND()\n")
        self.write("shared/shared.cpp", "int shared() { return 42; }\n")
        self.node(self.graph(), "shared/shared.cpp.o")
