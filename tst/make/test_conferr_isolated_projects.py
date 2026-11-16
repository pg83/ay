from tst.make.lib import MakeTest, PROJECT


class IsolatedProjectsConfigureErrorsTest(MakeTest):
    """Configure errors of Autocheck isolated projects (devtools/ymake/isolated_projects.cpp). A source compiled
    from an isolated project and a tool from one are reported along ymake's object and command nodes; ay does
    not model those paths yet."""

    def isolate(self, *projects):
        path = "build/rules/autocheck.isolated_projects"
        self.write_tree(path, (self.root / path).read_text() + "".join(project + "\n" for project in projects))

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def isolated_library(self):
        self.write("isolated/ya.make", "LIBRARY()\nSRCS(isolated.cpp)\nEND()\n")
        self.write("isolated/isolated.cpp", "int isolated() { return 42; }\n")
        self.write("isolated/isolated.h", "#pragma once\n")

    def test_peerdir_into_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.library(f"PEERDIR({PROJECT}/isolated)")
        self.isolated_library()
        self.assertConfigureLikeYa()

    def test_include_from_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.library()
        self.write("value.cpp", f'#include <{PROJECT}/isolated/isolated.h>\nint value() {{ return 42; }}\n')
        self.isolated_library()
        self.assertConfigureLikeYa()

    def test_depends_on_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDEPENDS({PROJECT}/isolated)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.write("isolated/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("isolated/main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def test_peerdir_inside_an_isolated_project(self):
        self.isolate(PROJECT)
        self.library(f"PEERDIR({PROJECT}/isolated)")
        self.isolated_library()
        self.assertConfigureLikeYa()

    def test_srcdir_in_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.library(f"SRCDIR({PROJECT}/isolated)")
        self.isolated_library()
        self.assertConfigureLikeYa()

    def test_addincl_of_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.library(f"ADDINCL({PROJECT}/isolated)")
        self.isolated_library()
        self.assertConfigureLikeYa()

    def test_data_from_an_isolated_project(self):
        self.isolate(f"{PROJECT}/isolated")
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDATA(arcadia/{PROJECT}/isolated)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.isolated_library()
        self.assertConfigureLikeYa()
