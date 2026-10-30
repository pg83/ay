from tst.make.lib import MakeTest


class DarwinResourceAssemblyTest(MakeTest):
    def test_darwin_resource_assembly_prefixes_symbols(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE_FILES(conftest.py)\nEND()\n")
        self.write("conftest.py", "VALUE = 42\n")
        graph = self.graph("--target-platform=default-darwin-x86_64")
        generated = [node for node in graph["graph"] if any(path.endswith(".yasm") for path in node["outputs"])]
        self.assertTrue(generated)
        for node in generated:
            args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertIn("--prefix", args)
