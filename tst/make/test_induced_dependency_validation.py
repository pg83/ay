from tst.make.lib import MakeTest


class InducedDependencyValidationTest(MakeTest):
    def test_malformed_prepared_dependencies_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py OUT generated.cpp INDUCED_DEPS $E2E_DEPS)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        for value in ("$_FMT_INDUCED_DEPS(h", "$_FMT_INDUCED_DEPS()"):
            with self.subTest(value=value):
                self.assertNotEqual(self.make("-DE2E_DEPS=" + value).returncode, 0)

    def test_bare_prepared_dependencies_are_ignored(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py OUT generated.cpp INDUCED_DEPS $E2E_DEPS)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        node = self.node(self.graph("-DE2E_DEPS=unprepared"), "generated.cpp")
        self.assertFalse(any(path.endswith("/unprepared") for path in node["inputs"]))
