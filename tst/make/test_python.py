from tst.make.lib import MakeTest, PROJECT


class PythonTest(MakeTest):
    def test_all_python_sources_filters_and_explicit_directories(self):
        self.write("ya.make", "PY3_LIBRARY()\nALL_PY_SRCS(TOP_LEVEL NO_TEST_FILES RECURSIVE src other src)\nEND()\n")
        included = ("src/answer.py", "src/nested/helper.py", "other/extra.py")
        excluded = ("local.py", "src/test_answer.py", "src/answer_test.py", "src/nested/test_helper.py")
        for source in included + excluded:
            self.write(source, "VALUE = 42\n")
        self.write("src/readme.txt", "not a Python module\n")
        graph = self.graph()
        inputs = [path for node in graph["graph"] for path in node["inputs"]]
        for source in included:
            self.assertIn(f"$(S)/{PROJECT}/{source}", inputs)
        for source in excluded:
            self.assertNotIn(f"$(S)/{PROJECT}/{source}", inputs)

    def test_recursive_python_sources_reject_nested_modules(self):
        self.write("ya.make", "PY3_LIBRARY()\nALL_PY_SRCS(RECURSIVE)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        self.write("nested/ya.make", "PY3_LIBRARY()\nPY_SRCS(hidden.py)\nEND()\n")
        self.write("nested/hidden.py", "VALUE = 17\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("nested/ya.make", result.stderr)

    def test_all_python_sources_requires_namespace_value(self):
        self.write("ya.make", "PY3_LIBRARY()\nALL_PY_SRCS(NAMESPACE)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_recursive_python_sources(self):
        self.write("ya.make", "PY3_LIBRARY()\nALL_PY_SRCS(RECURSIVE NAMESPACE e2e)\nEND()\n")
        self.write("module.py", "VALUE = 42\n")
        self.write("package/__init__.py", "\n")
        self.write("package/helper.py", "VALUE = 137\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        for source in ("module.py", "package/__init__.py", "package/helper.py"):
            self.assertIn(f"$(S)/{PROJECT}/{source}", inputs)

    def test_python_sources_with_unset_variable(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(${E2E_UNSET_SRCS} module.py)\nEND()\n")
        self.write("module.py", "VALUE = 42\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/module.py", inputs)

    def test_stub_resources_precede_generated_proto_resources(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(\n    answer.py\n    answer.pyi\n    answer.proto\n)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        self.write("answer.pyi", "VALUE: int\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        graph = self.graph()
        stub = next(node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.pyi" in node["inputs"] and any("/objcopy_" in path for path in node["outputs"]))
        archive = next(node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".global.a") for path in node["outputs"]))
        objects = [arg.rsplit("/", 1)[-1] for command in archive["cmds"] for arg in command["cmd_args"] if arg.endswith(".o") and f"/{PROJECT}/" in arg]
        raw = [index for index, name in enumerate(objects) if name.endswith("_raw.auxcpp.o")]
        self.assertTrue(raw, objects)
        self.assertLess(objects.index(stub["outputs"][0].rsplit("/", 1)[-1]), raw[0], objects)
