from tst.make.lib import MakeTest


class ProtoIncludeArgumentCountsTest(MakeTest):
    def test_proto_include_declarations_reject_multiple_directories(self):
        self.write("ya.make", f"LIBRARY()\nPROTO_ADDINCL({self.project_name}/first {self.project_name}/second)\nSRCS(answer.cpp)\nEND()\n")
        self.write("first/.keep", "")
        self.write("second/.keep", "")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
