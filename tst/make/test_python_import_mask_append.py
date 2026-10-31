from tst.make.lib import MakeTest


class PythonImportMaskAppendTest(MakeTest):
    def test_recursive_scalar_append_cannot_expand_a_multiword_import_mask(self):
        self.write("ya.make", "PY3_LIBRARY()\nSET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE prefix.$NO_CHECK_IMPORTS_FOR_VALUE)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.assertNotEqual(self.make("-DNO_CHECK_IMPORTS_FOR_VALUE=first.* second.*").returncode, 0)
