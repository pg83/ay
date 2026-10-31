from tst.make.lib import MakeTest, PROJECT


class SandboxModuleOutputCollisionsTest(MakeTest):
    def test_sandbox_output_cannot_replace_a_python_variant_archive(self):
        self.write("ya.make", f"PY23_LIBRARY()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO libpy3{PROJECT}.a)\nSRCS(GLOBAL extra.ld)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("extra.ld", "")
        self.write("answer.py", "ANSWER = 42\n")
        self.assertNotEqual(self.make().returncode, 0)
