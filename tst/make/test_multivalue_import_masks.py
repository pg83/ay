from tst.make.lib import MakeTest, xfail_on_ay


class MultivalueImportMasksTest(MakeTest):
    @xfail_on_ay("ymake configures TOOL modules in the target configuration too; a global NO_CHECK_IMPORTS_FOR_VALUE gives each a resource whose rescompiler TOOL loops back through its peers")
    def test_recursive_multivalue_import_mask_is_rejected(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nNO_CHECK_IMPORTS($NO_CHECK_IMPORTS_FOR_VALUE extra.*)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.assertNotEqual(self.make("-DNO_CHECK_IMPORTS_FOR_VALUE=first.* second.*").returncode, 0)
