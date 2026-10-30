from tst.make.lib import MakeTest


class DeepPythonImportMasksTest(MakeTest):
    def test_excessively_deep_configuration_references_in_import_masks_are_rejected(self):
        core = self.root / "build/ymake.core.conf"
        chain = "".join(f"E2E_IMPORT_MASK_{index}=$E2E_IMPORT_MASK_{index + 1}\n" for index in range(64))
        chain += "E2E_IMPORT_MASK_64=optional.*\nNO_CHECK_IMPORTS_FOR_VALUE=$E2E_IMPORT_MASK_0\n"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\n" + chain)
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.assertNotEqual(self.make("-t").returncode, 0)
