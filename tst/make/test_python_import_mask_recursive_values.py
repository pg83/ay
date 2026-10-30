from tst.make.lib import MakeTest


class PythonImportMaskRecursiveValuesTest(MakeTest):
    def fixture(self, mask):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nNO_CHECK_IMPORTS({mask})\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")

    def test_scalar_self_reference_cannot_substitute_a_multiword_import_mask(self):
        self.fixture("prefix.$NO_CHECK_IMPORTS_FOR_VALUE")
        self.assertNotEqual(self.make("-t", "-DNO_CHECK_IMPORTS_FOR_VALUE=first.* second.*").returncode, 0)
