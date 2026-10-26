import json

from tst.make.lib import MakeTest, PROJECT


class MapsStyleGraphsTest(MakeTest):
    project_name = "maps__ay_e2e__"

    def test_maps_style_headers_reach_consumer_link(self):
        project = self.project_name
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py binding.pyx)\nPEERDIR({project}/native)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("binding.pyx", f'cdef extern from "{project}/native/api.h":\n    int value()\ndef answer(): return value()\n')
        self.write("native/ya.make", f"LIBRARY()\nPEERDIR({project}/proto)\nSRCS(native.cpp)\nSET(CPP_LINTERS_DEFAULT_CONFIGS {project}/configs.json)\nSTYLE_CPP_MAPS()\nEND()\n")
        self.write("configs.json", json.dumps({"clang_format_18_vanilla": project + "/.clang-format"}))
        self.write(".clang-format", "BasedOnStyle: WebKit\n")
        self.write("native/api.h", f'#include <{project}/proto/answer.pb.h>\nint value();\n')
        self.write("native/native.cpp", "int value() { return 42; }\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nGRPC()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer { int32 value = 1; }\n')
        node = self.node(self.graph("-t"), "answer")
        self.assertIn(f"$(B)/{project}/proto/answer.pb.h", node["inputs"])

    def test_maps_cpp_style(self):
        self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\nSET(CPP_LINTERS_DEFAULT_CONFIGS {self.project_name}/configs.json)\nSTYLE_CPP_MAPS()\nEND()\n")
        self.write("configs.json", json.dumps({"clang_format_18_vanilla": self.project_name + "/.clang-format"}))
        self.write(".clang-format", "BasedOnStyle: WebKit\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph("-t")
        node = self.node(graph, "test-results/clang_format_18_vanilla/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("custom_clang_format_bin=clang-format" in arg for arg in args))


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
