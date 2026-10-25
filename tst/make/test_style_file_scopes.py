import unittest

from tst import lib


MODULE = "orchard"
BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
LINTERS = {"JSON": "clang_format_json", "YAML": "yamlfmt"}


def fixture(macros):
    files = {
        f"{MODULE}/ya.make": "LIBRARY()\n" + BARE + macros + "\nEND()\n",
        "build/config/tests/json_style/default_configs.json": '{"clang_format_json": "lint/json.conf"}',
        "build/config/tests/yaml_style/default_configs.json": '{"yamlfmt": "lint/yaml.conf"}',
        "lint/json.conf": "", "lint/yaml.conf": "",
        "tools/clang_format_json_linter/wrapper.py": "", "tools/yamlfmt_linter/wrapper.py": "",
        "build/platform/clang/clang-format/ya.make": lib.resource_lib("CLANG_FORMAT", 41),
        "build/external_resources/yamlfmt/ya.make": lib.resource_lib("YAMLFMT", 42),
    }
    for name in ("root", "config/direct", "config/nested/ignored", "data/child", "data/deep/leaf", "canondata/ignored"):
        for extension in ("json", "yaml", "yml"):
            files[f"{MODULE}/{name}.{extension}"] = "{}"
    for name in ("a.yaml", "t.yaml", "data/a.yaml", "data/deep/t.yaml", "data/readme.txt", "empty/readme.txt"):
        files[f"{MODULE}/{name}"] = ""
    return files


def selected_files(graph, kind):
    name = LINTERS[kind]
    output = f"$(B)/{MODULE}/test-results/{name}/meta.json"
    nodes = [node for node in graph["graph"] if output in node["outputs"]]
    if len(nodes) != 1:
        raise AssertionError(f"expected one {name} suite, got {len(nodes)}")
    args = nodes[0]["cmds"][0]["cmd_args"]
    start = args.index("run_custom_lint")
    extensions = (".json",) if kind == "JSON" else (".yaml", ".yml")
    prefix = f"$(S)/{MODULE}/"
    selected = [arg[len(prefix):] for arg in args[start:] if arg.startswith(prefix) and arg.endswith(extensions)]
    inputs = {arg[len(prefix):] for arg in nodes[0]["inputs"] if arg.startswith(prefix) and arg.endswith(extensions)}
    if set(selected) != inputs:
        raise AssertionError(f"command files {selected} differ from inputs {inputs}")
    if len(selected) != len(set(selected)):
        raise AssertionError(f"duplicate lint files {selected}")
    return selected


class StyleFileScopesTest(unittest.TestCase):
    def test_single_scope_defaults_explicit_and_recursive(self):
        # custom_lint.conf keeps the literal '.' when DIRS is empty, even
        # when DIRS_RECURSE is supplied. YAML excludes a.yaml and t.yaml.
        for kind in LINTERS:
            extensions = ("json",) if kind == "JSON" else ("yaml", "yml")
            for scope, names in (
                ("", ["root"]),
                ("DIRS config", ["config/direct"]),
                ("DIRS_RECURSE data", ["root", "data/child", "data/deep/leaf"]),
                ("DIRS config DIRS_RECURSE data", ["config/direct", "data/child", "data/deep/leaf"]),
            ):
                with self.subTest(kind=kind, scope=scope):
                    graph = lib.make(fixture(f"STYLE_{kind}({scope})"), MODULE, "-t")
                    expected = sorted(f"{name}.{ext}" for name in names for ext in extensions)
                    self.assertEqual(selected_files(graph, kind), expected)

    def test_repeated_scopes_merge_files_and_keep_the_first_config(self):
        macros = "\n".join([
            "STYLE_JSON(DIRS config)", "STYLE_YAML(DIRS config)",
            "SET(JSON_LINTER_DEFAULT_CONFIGS lint/alternate.json)",
            "SET(YAML_LINTER_DEFAULT_CONFIGS lint/alternate.json)",
            "STYLE_JSON(DIRS_RECURSE data)", "STYLE_YAML(DIRS_RECURSE data)",
            "STYLE_JSON(DIRS config)", "STYLE_YAML(DIRS config)",
        ])
        files = fixture(macros)
        files["lint/alternate.json"] = '{"clang_format_json": "lint/alternate.conf", "yamlfmt": "lint/alternate.conf"}'
        files["lint/alternate.conf"] = ""
        graph = lib.make(files, MODULE, "-t")
        for kind, name in LINTERS.items():
            with self.subTest(kind=kind):
                extensions = ("json",) if kind == "JSON" else ("yaml", "yml")
                expected = sorted(f"{file}.{ext}" for file in ("root", "config/direct", "data/child", "data/deep/leaf") for ext in extensions)
                self.assertEqual(selected_files(graph, kind), expected)
                node = lib.node_by_output(graph, f"$(B)/{MODULE}/test-results/{name}/meta.json")
                config = "json" if kind == "JSON" else "yaml"
                self.assertIn(f"$(S)/lint/{config}.conf", node["inputs"])
                self.assertNotIn("$(S)/lint/alternate.conf", node["inputs"])

    def test_autoincluded_current_directory_preserves_explicit_scopes(self):
        files = fixture("STYLE_JSON(DIRS_RECURSE data)\nSTYLE_YAML(DIRS_RECURSE data)")
        files["build/conf/autoincludes.json"] = f'["{MODULE}"]'
        files[f"{MODULE}/linters.make.inc"] = "USRV_STYLE_JSON_CURRENT_DIRECTORY()\nUSRV_STYLE_YAML_CURRENT_DIRECTORY()\n"
        graph = lib.make(files, MODULE, "-t")
        for kind in LINTERS:
            with self.subTest(kind=kind):
                extensions = ("json",) if kind == "JSON" else ("yaml", "yml")
                expected = sorted(f"{file}.{ext}" for file in ("root", "data/child", "data/deep/leaf") for ext in extensions)
                self.assertEqual(selected_files(graph, kind), expected)

    def test_empty_scope_emits_no_suite(self):
        graph = lib.make(fixture("STYLE_JSON(DIRS empty)\nSTYLE_YAML(DIRS empty)"), MODULE, "-t")
        self.assertFalse(any(node["kv"].get("run_test_node") for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main()
