import json

from tst.make.lib import MakeTest, PROJECT


class StyleGraphsTest(MakeTest):
    def test_yql_style(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(value.yql /query)\nSTYLE_YQL(DIRS_RECURSE queries)\nEND()\n")
        self.write("value.yql", "SELECT 42;\n")
        self.write("queries/nested/answer.yql", "SELECT 42;\n")
        self.write("queries/nested/ignored.json", "{}\n")
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"] if "yqlfmt" in json.dumps(node.get("cmds", []))])
        self.assertIn(f"/{PROJECT}/value.yql", commands)
        self.assertIn(f"/{PROJECT}/queries/nested/answer.yql", commands)
        self.assertNotIn("ignored.json", commands)

    def test_recursive_yaml_and_json_style(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(value.json /value)\nSTYLE_JSON(DIRS_RECURSE configs)\nCOLLECT_YAML_LINT_SRCS(DIRS_RECURSE configs EXCLUDE **/ignored.yaml)\nSTYLE_YAML_WITH_CONFIG()\nEND()\n")
        self.write("value.json", '{"answer":42}\n')
        self.write("configs/nested/answer.yaml", "answer: 42\n")
        self.write("configs/nested/ignored.yaml", "answer: 42\n")
        self.write("configs/nested/t.yaml", "answer: 42\n")
        self.write("configs/nested/answer.json", '{"answer":42}\n')
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn("yamlfmt", commands)
        self.assertIn("clang_format_json", commands)
        yaml_commands = json.dumps([node.get("cmds", []) for node in graph["graph"] if "yamlfmt" in json.dumps(node.get("cmds", []))])
        self.assertIn(f"/{PROJECT}/configs/nested/answer.yaml", yaml_commands)
        self.assertIn(f"/{PROJECT}/configs/nested/t.yaml", yaml_commands)
        self.assertNotIn("ignored.yaml", yaml_commands)
        self.assertNotIn("answer.json", yaml_commands)

    def test_yaml_style_config(self):
        self.write("ya.make", f"LIBRARY()\nRESOURCE(answer.yaml /answer)\nCOLLECT_YAML_LINT_SRCS()\nSET(MODULE_COMMON_CONFIGS_DIR {PROJECT}/config)\nSTYLE_YAML_WITH_CONFIG(CONFIG_TYPE .yamlfmt.yml)\nEND()\n")
        self.write("answer.yaml", "answer: 42\n")
        self.write("config/.yamlfmt.yml", "formatter:\n  type: basic\n")
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"] if "yamlfmt" in json.dumps(node.get("cmds", []))])
        self.assertIn(f"/{PROJECT}/answer.yaml", commands)
        self.assertIn(f"/{PROJECT}/config/.yamlfmt.yml", commands)
