from tst.make.lib import MakeTest


class MissingConfigurationFilesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def remove_configuration(self, path):
        self.write_tree(path, "")
        (self.root / path).unlink()

    def test_missing_go_configuration_is_rejected(self):
        self.remove_configuration("build/conf/go.conf")
        self.assertNotEqual(self.make().returncode, 0)

    def test_missing_linker_configuration_is_rejected(self):
        self.remove_configuration("build/conf/linkers/ld.conf")
        self.assertNotEqual(self.make().returncode, 0)

    def test_generated_configuration_requires_a_compiler_version(self):
        self.write_tree("build/ymake_conf.py", 'print("E2E_VALUE=42")\n')
        self.assertNotEqual(self.make().returncode, 0)
