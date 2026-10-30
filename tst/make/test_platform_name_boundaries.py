from tst.make.lib import MakeTest


class PlatformNameBoundariesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_target_platform_names_require_the_default_prefix(self):
        self.assertNotEqual(self.make("--target-platform=invalid").returncode, 0)

    def test_target_platform_names_require_an_architecture_separator(self):
        self.assertNotEqual(self.make("--target-platform=default-linux").returncode, 0)
