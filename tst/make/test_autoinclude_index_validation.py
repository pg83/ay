from tst.make.lib import MakeTest


class AutoincludeIndexValidationTest(MakeTest):
    def test_autoinclude_indices_require_valid_json(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write_tree("build/conf/autoincludes.json", "{\n")
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("autoincludes.json", result.stderr)

    def test_a_missing_autoinclude_index_is_a_configuration_error(self):
        index = "build/conf/autoincludes.json"
        self.assertTrue((self.root / index).is_file())
        self.write_tree(index, "[]\n")
        (self.root / index).unlink()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("autoincludes.json", result.stderr)
