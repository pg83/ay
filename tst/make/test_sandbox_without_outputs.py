from tst.make.lib import MakeTest


class SandboxWithoutOutputsTest(MakeTest):
    def test_sandbox_download_requires_at_least_one_output(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(123456789)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
