from tst.make.lib import MakeTest


class RootModuleOutputsTest(MakeTest):
    def test_root_module_compiles_without_a_directory_prefix(self):
        self.write_tree("ya.make", "LIBRARY(root)\nSRCS(__ay_root_e2e__.cpp)\nEND()\n")
        self.write_tree("__ay_root_e2e__.cpp", "int answer() { return 42; }\n")
        self.project_name = "."
        graph = self.graph()
        matches = [node for node in graph["graph"] if "$(B)/__ay_root_e2e__.cpp.o" in node["outputs"]]
        self.assertEqual(len(matches), 1)
        self.assertIn("$(S)/__ay_root_e2e__.cpp", matches[0]["inputs"])

    def test_root_python_module_embeds_sources_without_a_directory_prefix(self):
        self.write_tree("ya.make", "PY3_LIBRARY(root)\nPY_SRCS(__ay_root_e2e__.py)\nEND()\n")
        self.write_tree("__ay_root_e2e__.py", "ANSWER = 42\n")
        self.project_name = "."
        graph = self.graph()
        resources = [node for node in graph["graph"] if "$(S)/__ay_root_e2e__.py" in node["inputs"] and any(out.startswith("$(B)/objcopy_") for out in node["outputs"])]
        self.assertTrue(resources)
