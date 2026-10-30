from tst.make.lib import MakeTest


class PythonImportMaskReconfigurationTest(MakeTest):
    def test_changing_sanitizer_configuration_preserves_explicit_import_masks(self):
        self.write("main.py", "print(42)\n")
        self.write("ya.make", "PY3_PROGRAM(answer)\nNO_CHECK_IMPORTS(optional.*)\nSET(SANITIZER_TYPE no)\nPY_SRCS(MAIN main.py)\nEND()\n")
        graph = self.graph("-t")
        node = self.node(graph, "test-results/import_test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--skip optional.*", args[args.index("--checker") + 1])

    def test_disabling_sanitizer_restores_default_import_checks(self):
        self.write("main.py", "print(42)\n")
        self.write("ya.make", "PY3_PROGRAM(answer)\nSET(SANITIZER_TYPE address)\nSET(SANITIZER_TYPE no)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.node(self.graph("-t"), "test-results/import_test/meta.json")

    def test_empty_quoted_import_masks_keep_the_import_check_suite(self):
        self.write("main.py", "print(42)\n")
        self.write("ya.make", 'PY3_PROGRAM(answer)\nNO_CHECK_IMPORTS("\\\"\\\"")\nPY_SRCS(MAIN main.py)\nEND()\n')
        self.node(self.graph("-t"), "test-results/import_test/meta.json")
