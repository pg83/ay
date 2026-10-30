import json

from tst.make.lib import MakeTest, PROJECT


class StyleOptionBoundariesTest(MakeTest):
    def python(self, macros):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.py)\n" + macros + "\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")

    def test_python_style_keywords_require_matching_linters_and_values(self):
        for macro in ("STYLE_PYTHON(CONFIG_TYPE)", "STYLE_PYTHON(CHECK_FORMAT)",
                      "STYLE_PYTHON(RUN_IN_SOURCE_ROOT)"):
            with self.subTest(macro=macro):
                self.python(macro)
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_disabled_style_checks_do_not_create_linter_suites(self):
        self.write("ya.make", "LIBRARY()\nNO_LINT()\nSRCS(answer.cpp)\nSTYLE_JSON(DIRS configs)\nCOLLECT_YAML_LINT_SRCS(DIRS configs)\nSTYLE_YAML_WITH_CONFIG()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("configs/answer.json", '{"answer":42}\n')
        self.write("configs/answer.yaml", "answer: 42\n")
        result = self.make("-t")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        outputs = [path.replace("$(BUILD_ROOT)", "$(B)") for node in graph["graph"] for path in node["outputs"]]
        for linter in ("yamlfmt", "clang_format_json"):
            self.assertNotIn(f"$(B)/{PROJECT}/test-results/{linter}/meta.json", outputs)

    def test_recursive_style_collection_rejects_nested_modules(self):
        self.write("ya.make", "LIBRARY()\nSTYLE_JSON(DIRS_RECURSE configs)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("configs/child/ya.make", "LIBRARY()\nEND()\n")
        self.write("configs/child/answer.json", '{"answer":42}\n')
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_repeated_style_calls_merge_files_into_one_suite(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nSTYLE_JSON(DIRS first)\nSTYLE_JSON(DIRS second)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for directory in ("first", "second"):
            self.write(directory + "/answer.json", '{"answer":42}\n')
        checked = self.node(self.graph("-t"), "test-results/clang_format_json/meta.json")
        text = json.dumps(checked["cmds"])
        for directory in ("first", "second"):
            self.assertIn(f"/{PROJECT}/{directory}/answer.json", text)

    def test_explicit_style_configs_must_exist(self):
        self.python(f"SET(MODULE_COMMON_CONFIGS_DIR {PROJECT}/configs)\nSTYLE_RUFF(CONFIG_TYPE ruff.toml)")
        self.write("configs/.keep", "")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_linter_config_maps_require_the_requested_linter(self):
        self.python(f"SET(PYTHON_LINTERS_DEFAULT_CONFIGS {PROJECT}/configs.json)\nSTYLE_RUFF()")
        self.write("configs.json", "{}\n")
        self.assertNotEqual(self.make("-t").returncode, 0)
