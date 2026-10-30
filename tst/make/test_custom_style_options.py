import json

from tst.make.lib import MakeTest, PROJECT


class CustomStyleOptionsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.config = (self.root / "build/ymake.core.conf").read_text()

    def package(self, arguments):
        self.write_tree("build/ymake.core.conf", self.config + f"\nmacro E2E_CUSTOM_LINT() {{\n    _ADD_CUSTOM_EXPLICIT_LINTER_CHECK({arguments})\n}}\n")
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_CUSTOM_LINT()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("lint.py", "pass\n")
        self.write("data.txt", "42\n")

    def test_checks_without_files_do_not_create_a_suite(self):
        self.package("NAME e2e_lint WRAPPER_SCRIPT lint.py")
        result = self.make("-t")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any("/e2e_lint/" in path for node in graph["graph"] for path in node["outputs"]))

    def test_checks_require_a_name_and_wrapper_script(self):
        for arguments in ("WRAPPER_SCRIPT lint.py FILES data.txt", "NAME e2e_lint FILES data.txt"):
            with self.subTest(arguments=arguments):
                self.package(arguments)
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_extra_name_tokens_do_not_prevent_custom_lint_graph_generation(self):
        self.package(f"NAME first second WRAPPER_SCRIPT {PROJECT}/lint.py FILES data.txt")
        graph = self.graph("-t")
        checks = [node for node in graph["graph"] if any(path.endswith("/meta.json") for path in node["outputs"]) and f"$(S)/{PROJECT}/data.txt" in node["inputs"]]
        self.assertEqual(len(checks), 1)
        args = [arg for command in checks[0]["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--lint-name") + 1], "first")

    def test_makefiles_cannot_be_custom_lint_inputs(self):
        self.package("NAME e2e_lint WRAPPER_SCRIPT lint.py FILES ya.make")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_extra_parameters_require_key_value_pairs(self):
        self.package("NAME e2e_lint WRAPPER_SCRIPT lint.py FILES data.txt EXTRA_PARAMS unbound")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_generated_paths_are_omitted_from_custom_lint_inputs(self):
        self.package(f"NAME e2e_lint WRAPPER_SCRIPT {PROJECT}/lint.py FILES data.txt $(B)/{PROJECT}/generated.txt FILE_PROCESSING_TIME 0.25 EXTRA_PARAMS mode=strict")
        graph = self.graph("-t")
        checks = [node for node in graph["graph"] if any(path.endswith("/meta.json") for path in node["outputs"]) and f"$(S)/{PROJECT}/data.txt" in node["inputs"]]
        self.assertEqual(len(checks), 1)
        self.assertNotIn(f"$(B)/{PROJECT}/generated.txt", checks[0]["inputs"])

    def test_custom_checks_can_be_disabled(self):
        self.package(f"NAME e2e_lint WRAPPER_SCRIPT {PROJECT}/lint.py FILES data.txt")
        self.write("ya.make", (self.project / "ya.make").read_text().replace("E2E_CUSTOM_LINT()", "NO_LINT()\nE2E_CUSTOM_LINT()"))
        result = self.make("-t")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any("/e2e_lint/" in path for node in graph["graph"] for path in node["outputs"]))

