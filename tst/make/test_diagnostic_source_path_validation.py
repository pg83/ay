from tst.make.lib import MakeTest


class DiagnosticSourcePathValidationTest(MakeTest):
    def test_a_diagnostic_generator_rejects_a_directory_as_its_manifest(self):
        self.write("ya.make", "LIBRARY()\nSDC_DIAGS_SPLIT_GENERATOR_V4(.)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_a_parent_relative_manifest_must_still_be_in_the_diagnostic_tree(self):
        self.write("ya.make", "RECURSE(child)\n")
        self.write("value.yaml", "{}\n")
        self.write("child/ya.make", "LIBRARY()\nSDC_DIAGS_SPLIT_GENERATOR_V4(../value.yaml)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
