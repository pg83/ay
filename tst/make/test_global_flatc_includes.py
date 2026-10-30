from tst.make.lib import MakeTest, PROJECT


class GlobalFlatcIncludesTest(MakeTest):
    def test_flatc_include_directories_are_inherited_from_peers(self):
        self.write("ya.make", f"FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nPEERDIR({PROJECT}/policy)\nSRCS(answer.fbs)\nEND()\n")
        self.write("policy/ya.make", f"FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nADDINCL(GLOBAL FOR flatc {PROJECT}/include)\nEND()\n")
        self.write("include/.keep", "")
        self.write("answer.fbs", "namespace e2e; table Answer { value:int; }\nroot_type Answer;\n")
        node = self.node(self.graph(), "answer.fbs.h")
        self.assertIn(f"$(S)/{PROJECT}/answer.fbs", node["inputs"])
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(["-I", f"$(S)/{PROJECT}/include"], [args[i:i + 2] for i in range(len(args) - 1)])
