from tst.make.lib import MakeTest


class NativePythonSourceVersionsTest(MakeTest):
    def test_native_program_python_sources_require_an_explicit_python_version(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nPY_SRCS(helper.py)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("helper.py", "VALUE = 42\n")
        self.assertNotEqual(self.make().returncode, 0)
