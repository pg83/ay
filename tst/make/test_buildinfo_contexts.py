from tst.make.lib import MakeTest, PROJECT


class BuildInfoContextsTest(MakeTest):
    def fixture(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/meta)\nEND()\n")
        self.write("answer.cpp", "int main() { return 0; }\n")
        self.write("meta/ya.make", "PY2_LIBRARY()\nCREATE_BUILDINFO_FOR(buildinfo.h)\nSRCS(meta.cpp)\nEND()\n")
        self.write("meta/meta.cpp", f'#include <{PROJECT}/meta/buildinfo.h>\nint meta() {{ return 42; }}\n')
        return self.graph()

    def test_buildinfo_uses_module_compiler_flags(self):
        graph = self.fixture()
        node = self.node(graph, "meta/buildinfo.h")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DUSE_PYTHON2", args)
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", args)

    def test_buildinfo_does_not_export_an_include_directory(self):
        graph = self.fixture()
        node = self.node(graph, "answer.cpp.o")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn(f"-I$(B)/{PROJECT}/meta", args)

    def test_implicit_configure_file_exports_its_output_directory(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/meta)\nEND()\n")
        self.write("answer.cpp", "int main() { return 0; }\n")
        self.write("meta/ya.make", "LIBRARY()\nSRCS(meta.cpp.in)\nEND()\n")
        self.write("meta/meta.cpp.in", "int meta() { return 42; }\n")
        graph = self.graph()
        for output in ("answer.cpp.o", "meta/meta.cpp.o"):
            with self.subTest(output=output):
                node = self.node(graph, output)
                args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn(f"-I$(B)/{PROJECT}/meta", args)
