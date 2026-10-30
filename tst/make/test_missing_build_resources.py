from tst.make.lib import MakeTest, PROJECT


class MissingBuildResourcesTest(MakeTest):
    def test_flake8_suites_can_be_generated_without_a_resource_declaration(self):
        self.write_tree("build/external_resources/flake8_py3/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        suite = self.node(self.graph("-t"), "test-results/flake8/meta.json")
        args = [arg for command in suite["cmds"] for arg in command["cmd_args"]]
        self.assertFalse(any(arg.startswith("FLAKE8_PY3_RESOURCE_GLOBAL::") for arg in args))

    def test_fortran_graphs_can_be_generated_without_compiler_resource_declarations(self):
        self.write_tree("build/platform/flang/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
        self.write("ya.make", "LIBRARY()\nSRCS(answer.f)\nEND()\n")
        self.write("answer.f", "      INTEGER FUNCTION ANSWER()\n      ANSWER = 42\n      END\n")
        compiled = self.node(self.graph(), "answer.f.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.f", compiled["inputs"])
