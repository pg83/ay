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

    def test_disabling_flake8_keeps_explicit_ruff_checks(self):
        self.python("SET(DISABLE_FLAKE8 yes)\nSTYLE_RUFF()")
        graph = self.graph("-t")
        self.node(graph, "test-results/ruff/meta.json")
        self.assertFalse(any(path.endswith("/test-results/flake8/meta.json")
                             for node in graph["graph"] for path in node["outputs"]))

    def test_analysis_mode_omits_python_style_checks(self):
        self.python("SET(CPP_ANALYSIS_MODE yes)\nSTYLE_RUFF()")
        result = self.make("-t")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any(path.endswith(("/test-results/flake8/meta.json", "/test-results/ruff/meta.json"))
                             for node in graph["graph"] for path in node["outputs"]))

    def test_repeated_cpp_style_calls_create_one_suite(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nSTYLE_CPP()\nSTYLE_CPP()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph("-t")
        suites = [node for node in graph["graph"] if any(
            path.endswith("/test-results/clang_format/meta.json") for path in node["outputs"])]
        self.assertEqual(len(suites), 1)
        self.assertIn(f"/{PROJECT}/answer.cpp", json.dumps(suites[0]["cmds"]))

    def test_cpp_style_rejects_generator_modules_without_native_sources(self):
        self.write("ya.make", "GEN_LIBRARY(answer)\nRUN_PYTHON3(generate.py STDOUT_NOAUTO generated.txt)\nSTYLE_CPP()\nEND()\n")
        self.write("generate.py", "print('answer = 42')\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_python_linter_source_lists_require_valid_json(self):
        for seed in ("invalid", "null"):
            with self.subTest(seed=seed):
                self.write("ya.make", f"PY3_LIBRARY()\nSET(PY_LINTER_FILES {seed})\nPY_SRCS(answer.py)\nEND()\n")
                self.write("answer.py", "ANSWER = 42\n")
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_python_no_lint_rejects_unknown_names_and_unapproved_paths(self):
        for arguments in ("unknown_linter", ""):
            with self.subTest(arguments=arguments):
                self.python(f"NO_LINT({arguments})")
                self.assertNotEqual(self.make("-t").returncode, 0)
