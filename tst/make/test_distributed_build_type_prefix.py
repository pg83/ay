from tst.make.lib import MakeTest


class DistributedBuildTypePrefixTest(MakeTest):
    def test_internal_distribution_prefixes_are_rejected_as_user_build_types(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make("--build=dist-debug")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dist-debug", result.stderr)
