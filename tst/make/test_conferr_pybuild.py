from tst.make.lib import MakeTest


class PybuildConfigureErrorsTest(MakeTest):
    """Configure errors of the Python build plugin (build/plugins/pybuild.py)."""

    def python_library(self, *statements):
        self.write("ya.make", "PY3_LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.py", "VALUE = 42\n")

    def test_no_lint_outside_of_allowed_paths(self):
        self.python_library("NO_LINT()", "PY_SRCS(value.py)")
        self.assertConfigureLikeYa()

    def test_py_srcs_item_above_the_module(self):
        self.python_library("PY_SRCS(../value.py)")
        self.assertConfigureLikeYa()

    def test_py_srcs_item_from_the_root(self):
        self.python_library("PY_SRCS(/value.py)")
        self.assertConfigureLikeYa()

    def test_top_level_main_in_a_python3_program(self):
        self.write("ya.make", "PY3_PROGRAM()\nPY_SRCS(TOP_LEVEL __main__.py)\nEND()\n")
        self.write("__main__.py", "print(42)\n")
        self.assertConfigureLikeYa()

    def test_unrecognized_py_srcs_argument(self):
        self.python_library("PY_SRCS(value.py value.e2e)")
        self.write("value.e2e", "value\n")
        self.assertConfigureLikeYa()

    def test_duplicate_py_srcs(self):
        self.python_library("PY_SRCS(value.py value.py)")
        self.assertConfigureLikeYa()

    def test_duplicate_type_stubs(self):
        self.python_library("PY_SRCS(value.py value.pyi value.pyi)")
        self.write("value.pyi", "VALUE: int\n")
        self.assertConfigureLikeYa()

    def test_test_srcs_with_a_top_level_namespace(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(TOP_LEVEL test_value.py)\nEND()\n")
        self.write("test_value.py", "def test_value():\n    assert True\n")
        self.assertConfigureLikeYa()

    def test_py_srcs_without_a_python_version(self):
        self.write("ya.make", "LIBRARY()\nPY_SRCS(value.py)\nEND()\n")
        self.write("value.py", "VALUE = 42\n")
        self.assertConfigureLikeYa()

    def test_py_main_without_a_python_version(self):
        self.write("ya.make", "LIBRARY()\nPY_MAIN(value)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_py_register_without_a_python_version(self):
        self.write("ya.make", "LIBRARY()\nPY_REGISTER(value)\nEND()\n")
        self.assertConfigureLikeYa()
