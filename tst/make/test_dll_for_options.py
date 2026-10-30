from tst.make.lib import MakeTest, PROJECT


class DllForOptionsTest(MakeTest):
    def test_rooted_source_directory_and_exports_reach_the_library(self):
        self.write("ya.make", f"DLL_FOR(${{ARCADIA_ROOT}}/{PROJECT}/lib answer EXPORTS symbols.exports)\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("symbols.exports", "C answer\n")
        graph = self.graph()
        library = self.node(graph, "libanswer.so")
        self.assertIn(library["uid"], graph["result"])
        self.assertTrue(any(f"$(S)/{PROJECT}/symbols.exports" in node["inputs"] for node in graph["graph"]))

    def test_missing_and_build_root_directories_are_rejected(self):
        for args in ("", f"${{ARCADIA_BUILD_ROOT}}/{PROJECT}/lib answer", PROJECT):
            with self.subTest(args=args):
                self.write("ya.make", f"DLL_FOR({args})\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_recursive_library_inclusion_is_a_configuration_error(self):
        self.write("ya.make", f"DLL_FOR({PROJECT}/first answer)\n")
        self.write("first/ya.make", f"DLL_FOR({PROJECT}/second first)\n")
        self.write("second/ya.make", f"DLL_FOR({PROJECT}/first second)\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_empty_exports_option_does_not_prevent_library_generation(self):
        self.write("ya.make", f"DLL_FOR({PROJECT}/lib answer EXPORTS)\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.node(self.graph(), "libanswer.so")
