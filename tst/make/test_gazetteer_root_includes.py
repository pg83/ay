from tst.make.lib import MakeTest


class GazetteerRootIncludesTest(MakeTest):
    def test_root_proto_include_paths_reach_gazetteer_conversion(self):
        self.write("ya.make", "LIBRARY()\nADDINCL(FOR proto ${ARCADIA_ROOT}/ ${ARCADIA_BUILD_ROOT}/)\nSRCS(answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", "package e2e; message Answer {}\n")
        converted = self.node(self.graph(), "answer.proto")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)")
                for command in converted["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-I$(S)", args)
        self.assertIn("-I$(B)", args)
        self.assertIn(f"$(S)/{self.project_name}/answer.gztproto", converted["inputs"])
