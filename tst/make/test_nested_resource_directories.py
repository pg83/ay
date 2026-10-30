from tst.make.lib import MakeTest, PROJECT


class NestedResourceDirectoriesTest(MakeTest):
    def test_nested_directory_variables_keep_source_root_resource_globs(self):
        self.write("ya.make", "LIBRARY()\nALL_RESOURCE_FILES(txt PREFIX /data/ ${E2E_DIRECTORY_1})\nEND()\n")
        self.write("data/value.txt", "42\n")
        flags = [f"-DE2E_DIRECTORY_{i}=${{E2E_DIRECTORY_{i + 1}}}" for i in range(1, 8)]
        flags.append(f"-DE2E_DIRECTORY_8=${{ARCADIA_ROOT}}/{PROJECT}/data")
        graph = self.graph(*flags)
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/data/value.txt", inputs)
