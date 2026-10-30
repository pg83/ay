from tst.make.lib import MakeTest, PROJECT


class PythonFlatbufferGroupsTest(MakeTest):
    def fixture(self, declarations, name="answer.fbs"):
        self.write("ya.make", f"PY3_LIBRARY()\n{declarations}\nEND()\n")
        self.write(name, "namespace e2e; table Answer { value:int; }\n")

    def converters(self, graph):
        return [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".fbs.pysrc") for path in node["outputs"])]

    def test_one_schema_can_use_distinct_python_module_names(self):
        self.fixture("PY_SRCS(answer.fbs=second.answer answer.fbs=first.answer)")
        generated = self.converters(self.graph())
        self.assertEqual(len(generated), 1)
        self.assertIn(f"$(S)/{PROJECT}/answer.fbs", generated[0]["inputs"])

    def test_quoted_schema_filenames_keep_source_dependencies(self):
        self.fixture('PY_SRCS("answer\'s.fbs")', "answer's.fbs")
        generated = self.converters(self.graph())
        self.assertEqual(len(generated), 1)
        self.assertIn(f"$(S)/{PROJECT}/answer's.fbs", generated[0]["inputs"])

    def test_repeated_schema_groups_are_rejected(self):
        self.fixture("PY_SRCS(answer.fbs)\nPY_SRCS(answer.fbs)")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(".fbs.pysrc", result.stderr)
        self.assertIn("already added", result.stderr)
