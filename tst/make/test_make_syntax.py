from tst.make.lib import MakeTest


class MakeSyntaxTest(MakeTest):
    def test_bare_carriage_return_line_endings_are_rejected(self):
        self.write("ya.make", "# header\rLIBRARY()\r# sources\rSRCS(answer.cpp)\rEND()\r")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_non_call_tokens_cannot_start_statements(self):
        for token in ('"text"', '42', './word', '(', ')', '==', '\x00'):
            with self.subTest(token=token):
                self.write("ya.make", "LIBRARY()\n" + token + "\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_unterminated_quoted_segments_are_rejected(self):
        for token in ('./word"unfinished', '42word\\ value"unfinished', '$(E2E)\\ value"unfinished'):
            with self.subTest(token=token):
                self.write("ya.make", "LIBRARY()\n" + token + "\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_quoted_segments_with_escapes_do_not_form_macro_names(self):
        self.write("ya.make", 'LIBRARY()\n./word"escaped\\\"suffix"()\nEND()\n')
        self.assertNotEqual(self.make().returncode, 0)
