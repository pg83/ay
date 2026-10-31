from tst.make.lib import MakeTest


class CythonBaseSearchPathsTest(MakeTest):
    def test_noncanonical_include_requires_an_explicit_cython_directory(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", 'include "__ay_base_e2e__/./value.pxi"\ndef answer(): return VALUE\n')
        self.write_tree("contrib/libs/linux-headers/__ay_base_e2e__/value.pxi", "DEF VALUE = 42\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("value.pxi", result.stderr)
