from tst.make.lib import MakeTest


class PythonGeneratorInterpretersTest(MakeTest):
    def test_resource_free_collections_can_use_a_configured_python_interpreter(self):
        self.write_tree("build/platform/python/ymake_python3/ya.make", "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\nEND()\n")
        self.write("ya.make", "UNION(answer)\nNO_YMAKE_PYTHON3()\nSET(NEED_PLATFORM_PEERDIRS no)\nRUN_PYTHON3(generate.py STDOUT result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        graph = self.graph("-DBUILD_PYTHON3_BIN=python3")
        generators = [node for node in graph["graph"]
                      if any(path.endswith("/result.txt") for path in node["outputs"])
                      and not any(path.endswith(".pkg.fake") for path in node["outputs"])]
        self.assertEqual(len(generators), 1)
        generated = generators[0]
        self.assertEqual(generated["cmds"][0]["cmd_args"][0], "python3")
