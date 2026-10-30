from tst.make.lib import MakeTest


class DualPythonHeaderMacrosTest(MakeTest):
    def test_python2_header_macro_uses_python3_in_the_native_python3_variant(self):
        self.write("ya.make", "PY23_NATIVE_LIBRARY(answer)\nPYTHON2_ADDINCL()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <Python.h>\nint python_version() { return PY_MAJOR_VERSION; }\n")
        node = self.node(self.graph(), "answer.cpp.py3.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DUSE_PYTHON3", args)
        self.assertNotIn("-DUSE_PYTHON2", args)
