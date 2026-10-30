from tst.make.lib import MakeTest, PROJECT


class ImplibOptionsTest(MakeTest):
    def test_bundled_libraries_are_inputs_of_import_library_generation(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/shared NAME libanswer.so)\nGENERATE_IMPLIB(answer libanswer.so FROM_BUNDLE SONAME libanswer.so)\nEND()\n")
        self.write("shared/ya.make", "DLL(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("shared/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        bundled = self.node(graph, "libanswer.so")
        generated = self.node(graph, "libanswer.so.init.c")
        self.assertIn(f"$(B)/{PROJECT}/libanswer.so", generated["inputs"])
        self.assertIn(bundled["uid"], generated["deps"])

    def test_import_library_options_require_values(self):
        for option in ("SONAME", "DLOPEN_CALLBACK"):
            with self.subTest(option=option):
                self.write("ya.make", f"LIBRARY()\nGENERATE_IMPLIB(answer /usr/lib/libanswer.so {option})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_import_library_options_reject_unknown_arguments(self):
        self.write("ya.make", "LIBRARY()\nGENERATE_IMPLIB(answer /usr/lib/libanswer.so UNKNOWN_OPTION)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
