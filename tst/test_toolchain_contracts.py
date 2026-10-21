import base64
import json
import unittest

import lib
from test_ymake_conf import RECORDER, Tree, cc_args, library


def config_tree(test, config, script=RECORDER):
    return Tree(test, {
        "app/ya.make": library("SRCS(main.cpp)\n"), "app/main.cpp": "",
        "build/ya.conf.json": json.dumps(config), "build/ymake_conf.py": script,
    })


class ToolchainContractsTest(unittest.TestCase):
    def test_optional_toolchain_metadata_and_host_fallback(self):
        config = json.loads(lib.ya_conf_json())
        config["unrelated"] = {"nested": [None, 3, "ignored"]}
        config["toolchain"] = {"not-cxx": {"tools": {}}, **config["toolchain"]}
        compiler = config["toolchain"]["clang20"]
        compiler["name"] = "renamed"
        compiler["platforms"] = [{"host": {"os": "LINUX"}, "default": True}]
        tree = config_tree(self, config)
        graph = tree.graph("app", "--target-platform", "default-linux-x86_64")
        self.assertIn("-m64", cc_args(graph, "$(B)/app/main.cpp.o"))
        argv = tree.recorded("dist-debug")["argv"]
        params = json.loads(base64.b64decode(argv[argv.index("--toolchain-params") + 1]))
        self.assertEqual(params["platform"]["host"]["arch"], "x86_64")
        self.assertEqual(params["platform"]["host"], params["platform"]["target"])
        self.assertEqual(params["platform"]["target"]["visible_name"], "renamed")

    def test_unbundled_toolchain_without_environment_or_bottle(self):
        config = json.loads(lib.ya_conf_json())
        compiler = config["toolchain"]["clang20"]
        compiler.pop("env")
        compiler["tools"]["c++"].pop("bottle")
        compiler["params"]["use_bundle"] = False
        tree = config_tree(self, config)
        args = cc_args(tree.graph("app"), "$(B)/app/main.cpp.o")
        self.assertIn("-c", args)
        argv = tree.recorded("dist-debug")["argv"]
        params = json.loads(base64.b64decode(argv[argv.index("--toolchain-params") + 1]))
        self.assertEqual(params["env"], {})
        self.assertIsNone(params["bottle_name"])
        self.assertEqual(params["tool_var"], "DEFAULT_LINUX_AARCH64")

    def test_toolchain_without_resource_root_and_script_stderr(self):
        config = json.loads(lib.ya_conf_json())
        config["toolchain"]["clang20"]["params"].pop("match_root")
        tree = config_tree(self, config, 'import sys\nsys.stderr.write("toolchain diagnostic\\n")\n' + RECORDER)
        result = tree.make("app")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("toolchain diagnostic", result.stderr)
        lib.node_by_output(json.loads(result.stdout), "$(B)/app/main.cpp.o")

    def test_any_platform_resource_mapping(self):
        config = json.loads(lib.ya_conf_json(formula={"by_platform": {"any": {"uri": "sbr:21"}}}))
        graph = config_tree(self, config).graph("app")
        self.assertIn("-c", cc_args(graph, "$(B)/app/main.cpp.o"))

    def test_invalid_toolchain_description_is_diagnosed(self):
        for config, message in (([], 'want "{"'), ({"toolchain": []}, 'want "{"')):
            with self.subTest(config=config):
                result = config_tree(self, config).make("app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)
        config = json.loads(lib.ya_conf_json())
        config["toolchain"]["clang20"]["platforms"] = [{"target": {"os": "LINUX", "arch": "aarch64"}}]
        result = config_tree(self, config).make("app")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("lacks a host platform", result.stderr)

    def test_invalid_resource_formulas_are_diagnosed(self):
        for formula, message in (
            ("missing/formula.json", "tool chain formula missing/formula.json is missing"),
            ({"by_platform": {}}, "has no 'by_platform' mapping"),
            ({"other": "value"}, "has no 'by_platform' mapping"),
            ({"by_platform": {"unsupported": {"uri": "sbr:1"}}}, "unsupported resource platform"),
        ):
            with self.subTest(formula=formula):
                result = config_tree(self, json.loads(lib.ya_conf_json(formula=formula))).make("app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_non_string_json_keys_report_decoder_errors(self):
        for config in ('{false: []}', '{"toolchain": {1: {}}}', '{"toolchain": {"first": {}, true: {}}}'):
            with self.subTest(config=config):
                result = Tree(self, {"app/ya.make": library(""), "build/ya.conf.json": config}).make("app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("invalid character", result.stderr)
                self.assertNotIn("panic:", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
