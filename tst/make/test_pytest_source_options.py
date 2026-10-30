from tst.make.lib import MakeTest, PROJECT


class PytestSourceOptionsTest(MakeTest):
    def source_inputs(self):
        return {path for node in self.graph("-t")["graph"] for path in node["inputs"]
                if path.startswith(f"$(S)/{PROJECT}/")}

    def test_namespace_and_test_filename_filters_with_duplicate_directories(self):
        self.write("ya.make", "PY3TEST()\nALL_PYTEST_SRCS(TOP_LEVEL NAMESPACE e2e ONLY_TEST_FILES tests tests)\nEND()\n")
        self.write("tests/test_answer.py", "def test_answer(): assert True\n")
        self.write("tests/answer_test.py", "def test_other(): assert True\n")
        self.write("tests/helper.py", "answer = 42\n")
        inputs = self.source_inputs()
        for name in ("test_answer.py", "answer_test.py"):
            self.assertIn(f"$(S)/{PROJECT}/tests/{name}", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/tests/helper.py", inputs)

    def test_default_source_directory_includes_helpers(self):
        self.write("ya.make", "PY3TEST()\nALL_PYTEST_SRCS()\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.write("helper.py", "answer = 42\n")
        self.write("nested/ignored.py", "answer = 43\n")
        inputs = self.source_inputs()
        self.assertIn(f"$(S)/{PROJECT}/helper.py", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/nested/ignored.py", inputs)

    def test_namespace_requires_a_value(self):
        self.write("ya.make", "PY3TEST()\nALL_PYTEST_SRCS(NAMESPACE)\nEND()\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_recursive_test_sources_reject_nested_modules(self):
        self.write("ya.make", "PY3TEST()\nALL_PYTEST_SRCS(RECURSIVE)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.write("nested/ya.make", "PY3_LIBRARY()\nPY_SRCS(helper.py)\nEND()\n")
        self.write("nested/helper.py", "answer = 42\n")
        self.assertNotEqual(self.make("-t").returncode, 0)
