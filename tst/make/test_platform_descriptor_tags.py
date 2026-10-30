from tst.make.lib import MakeTest


class PlatformDescriptorTagsTest(MakeTest):
    def descriptor(self, *flags):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        node = self.node(self.graph("-t", *flags), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        return args[args.index("--target-platform-descriptor") + 1].split("-")

    def test_musl_option_adds_the_platform_tag(self):
        self.assertIn("musl", self.descriptor("--musl"))

    def test_disabled_optimization_omits_the_platform_tag(self):
        self.assertNotIn("lto", self.descriptor("--target-platform-flag=USE_LTO=no"))

    def test_sanitizer_uses_the_short_platform_tag(self):
        self.assertIn("asan", self.descriptor("--target-platform-flag=SANITIZER_TYPE=address"))
