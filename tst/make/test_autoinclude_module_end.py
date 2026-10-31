import json

from tst.make.lib import MakeTest


class AutoincludeModuleEndTest(MakeTest):
    def test_linter_autoincludes_cannot_end_the_enclosing_module(self):
        self.write_tree("build/conf/autoincludes.json", json.dumps([self.project_name]))
        self.write("linters.make.inc", "END()\n")
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
