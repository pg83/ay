from tst.make.lib import MakeTest


class EmptyNativePythonSourcesTest(MakeTest):
    def test_an_empty_python_source_group_requires_a_python_selector(self):
        self.write("ya.make", "PROGRAM(answer)\nPY_SRCS()\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.assertNotEqual(self.make().returncode, 0)
