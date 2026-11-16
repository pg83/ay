from tst.make import toolchains
from tst.make.lib import MakeTest


class ToolchainRequiredMetadataTest(MakeTest):
    def check_missing_field(self, field):
        files = toolchains.cxx_files(self)
        removed = 0
        for _, _, chain in toolchains.cxx_entries(files):
            container = chain["tools"]["c++"] if field == "bottle" else chain.get("params", {})
            if field in container:
                del container[field]
                removed += 1
        self.assertGreater(removed, 0)
        toolchains.write(self, files)
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_bundled_cpp_toolchains_require_a_bottle_reference(self):
        self.check_missing_field("bottle")

    def test_bundled_cpp_toolchains_require_a_match_root(self):
        self.check_missing_field("match_root")
