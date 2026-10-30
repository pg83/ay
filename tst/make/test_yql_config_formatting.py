from tst.make.lib import MakeTest


class YqlConfigFormattingTest(MakeTest):
    def test_macro_declaration_spacing_preserves_the_stable_abi(self):
        path = "build/conf/project_specific/yql_udf.conf"
        source = (self.root / path).read_text()
        declaration = "macro YQL_STABLE_ABI_VERSION("
        self.assertEqual(source.count(declaration), 1)
        self.write_tree(path, source.replace(declaration, "macro YQL_STABLE_ABI_VERSION ("))
        self.write("ya.make", "LIBRARY(answer)\nYQL_LAST_ABI_VERSION()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DUSE_STABLE_UDF_ABI_VERSION", args)
        self.assertNotIn("-DUSE_CURRENT_UDF_ABI_VERSION", args)
