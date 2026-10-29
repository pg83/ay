import json

from tst.make.lib import MakeTest, PROJECT


class MapsStyleGraphsTest(MakeTest):
    project_name = "maps__ay_e2e__"

    def test_cpp_linter_merges_proto_languages(self):
        project = self.project_name
        self.write("java/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({project}/schema)\nEND()\n")
        self.write("java/Main.java", "public class Main {}\n")
        self.write("native/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({project}/schema)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nGENERATE_ENUM_SERIALIZATION(answer.pb.h)\nSET(CPP_LINTERS_DEFAULT_CONFIGS {project}/configs.json)\nSTYLE_CPP_MAPS()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nenum Answer { UNKNOWN = 0; VALUE = 42; }\n')
        self.write("configs.json", json.dumps({"clang_format_18_vanilla": project + "/.clang-format"}))
        self.write(".clang-format", "BasedOnStyle: WebKit\n")
        for order in ("java native", "native java"):
            with self.subTest(order=order):
                self.write("ya.make", f"RECURSE({order})\n")
                graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes")
                node = self.node(graph, "schema/test-results/clang_format_18_vanilla/meta.json")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertEqual(any(arg.startswith("JAVA_PROTOC_RESOURCE_GLOBAL::") for arg in args), order.startswith("java"))

    def test_cpp_linter_on_java_proto_uses_java_resources(self):
        project = self.project_name
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({project}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nGENERATE_ENUM_SERIALIZATION(answer.pb.h)\nSET(CPP_LINTERS_DEFAULT_CONFIGS {project}/configs.json)\nSTYLE_CPP_MAPS()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nenum Answer { UNKNOWN = 0; VALUE = 42; }\n')
        self.write("configs.json", json.dumps({"clang_format_18_vanilla": project + "/.clang-format"}))
        self.write(".clang-format", "BasedOnStyle: WebKit\n")
        graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes")
        node = self.node(graph, "schema/test-results/clang_format_18_vanilla/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.startswith("JAVA_PROTOC_RESOURCE_GLOBAL::") for arg in args))

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

    def test_cpp_linter_on_java_proto_lints_headers(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nSET(CPP_LINTERS_DEFAULT_CONFIGS {PROJECT}/configs.json)\nSTYLE_CPP()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nenum Answer { UNKNOWN = 0; VALUE = 42; }\n')
        self.write("schema/answer.h", "#pragma once\n")
        self.write("configs.json", json.dumps({"clang_format": PROJECT + "/.clang-format"}))
        self.write(".clang-format", "BasedOnStyle: WebKit\n")
        graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes")
        node = self.node(graph, "schema/test-results/clang_format/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.endswith(f"/{PROJECT}/schema/answer.h") for arg in args))
