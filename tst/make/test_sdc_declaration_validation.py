from tst.make.lib import MakeTest


class SdcDeclarationValidationTest(MakeTest):
    def rejected(self, declaration):
        self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{declaration}\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_two_argument_resource_metadata_is_ignored(self):
        self.write("ya.make", "PROGRAM(answer)\nUSE_SDC_META()\nSRCS(main.cpp)\nSDC_RESOURCE_INFO(category name)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        self.node(graph, "answer")
        self.assertFalse(any(output.endswith("/name.sdcmeta") for node in graph["graph"] for output in node["outputs"]))

    def test_sandbox_metadata_requires_a_name(self):
        self.rejected("SDC_SB_RESOURCE_INFO(123456789)")

    def test_rosnode_metadata_requires_a_path(self):
        self.rejected("SDC_ROSNODE_INFO()")

    def test_shared_library_metadata_requires_a_url(self):
        self.rejected("SDC_SOLIB_INFO()")
