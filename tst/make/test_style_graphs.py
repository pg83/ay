import json

from tst.make.lib import MakeTest


class StyleGraphsTest(MakeTest):
    def test_yql_style(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(value.yql /query)\nSTYLE_YQL(DIRS_RECURSE queries)\nEND()\n")
        self.write("value.yql", "SELECT 42;\n")
        self.write("queries/nested/answer.yql", "SELECT 42;\n")
        graph = self.graph("-t")
        self.assertTrue(any("yqlfmt" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_recursive_yaml_and_json_style(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(value.json /value)\nSTYLE_JSON(DIRS_RECURSE configs)\nCOLLECT_YAML_LINT_SRCS(DIRS_RECURSE configs EXCLUDE **/ignored.yaml)\nSTYLE_YAML_WITH_CONFIG()\nEND()\n")
        self.write("value.json", '{"answer":42}\n')
        self.write("configs/nested/answer.yaml", "answer: 42\n")
        self.write("configs/nested/ignored.yaml", "answer: 42\n")
        self.write("configs/nested/answer.json", '{"answer":42}\n')
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn("yamlfmt", commands)
        self.assertIn("clang_format_json", commands)
