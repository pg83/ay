from tst.make.lib import MakeTest


class PythonImportMaskSelfReferencesTest(MakeTest):
    def test_self_referencing_import_mask_configuration_is_rejected(self):
        config = "build/ymake.core.conf"
        original = (self.root / config).read_text()
        self.write_tree(config, original + "\nE2E_IMPORT_MASK=$E2E_IMPORT_MASK\n")
        self.write("ya.make", "PY3_PROGRAM(answer)\nNO_CHECK_IMPORTS($E2E_IMPORT_MASK)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.assertNotEqual(self.make().returncode, 0)
