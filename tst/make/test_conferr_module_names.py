from tst.make.lib import MakeTest


class ModuleNameConfigureErrorsTest(MakeTest):
    """Configure errors of module names and source paths (devtools/ymake/module_loader.cpp, module_resolver.cpp)."""

    def test_module_name_with_a_slash(self):
        self.write("ya.make", "PROGRAM(e2e/answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def test_source_outside_of_the_build_tree(self):
        self.write("ya.make", "LIBRARY()\nSRCS(/e2e/absolute.cpp)\nEND()\n")
        self.assertConfigureLikeYa()
