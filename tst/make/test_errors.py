from tst.make.lib import MakeTest


class ConfigurationErrorsTest(MakeTest):
    def test_missing_source_is_an_error(self):
        self.write("ya.make", "LIBRARY()\nSRCS(missing.cpp)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.cpp", result.stderr)
