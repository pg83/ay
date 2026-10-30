from tst.make.lib import MakeTest


class ConfigureFileWhitespaceTest(MakeTest):
    def configure(self, text):
        self.write("ya.make", "LIBRARY()\nSET(E2E_FLAG yes)\nCONFIGURE_FILE(config.in result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("config.in", text)
        node = self.node(self.graph(), "result.txt")
        return [arg for command in node["cmds"] for arg in command["cmd_args"]]

    def test_repeated_spaces_and_tabs_keep_cmake_definition_values(self):
        args = self.configure("#cmakedefine  E2E_FLAG\n#cmakedefine\t \tE2E_FLAG\n#cmakedefine01  \t E2E_FLAG\n")
        self.assertEqual(args.count("E2E_FLAG=yes"), 1)

    def test_truncated_cmake_definitions_do_not_hide_other_references(self):
        for suffix in ("#cmakedefine  ", "#cmakedefine\t \t", "#cmakedefine01  \t", "#cmakedefine01\t \t!"):
            with self.subTest(suffix=suffix):
                self.assertIn("E2E_FLAG=yes", self.configure("@E2E_FLAG@\n" + suffix))

    def test_incomplete_cmake_definition_keywords_preserve_prior_references(self):
        for suffix in ("#cmakedefine", "#cmakedefine0", "#cmakedefine0X", "#cmakedefineX"):
            with self.subTest(suffix=suffix):
                self.assertIn("E2E_FLAG=yes", self.configure("@E2E_FLAG@\n" + suffix))
