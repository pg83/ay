from tst.make.lib import MakeTest


class MissingMacroParenthesesTest(MakeTest):
    def test_a_body_macro_followed_by_an_identifier_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS ANSWER\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ANSWER", result.stderr)
