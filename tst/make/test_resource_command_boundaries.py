from tst.make.lib import MakeTest, PROJECT


class ResourceCommandBoundariesTest(MakeTest):
    def test_resource_file_prefix_expands_late_directory_macros(self):
        self.write("value.txt", "42\n")
        for variable, root in (("CURDIR", "$(S)"), ("BINDIR", "$(B)")):
            expression = "${" + variable + "}/"
            for prefix in (expression, "$ENV{E2E_RESOURCE_PREFIX}"):
                with self.subTest(variable=variable, prefix=prefix):
                    self.write("ya.make", f"LIBRARY()\nRESOURCE_FILES(PREFIX {prefix} value.txt)\nEND()\n")
                    graph = self.graph(env={"E2E_RESOURCE_PREFIX": expression})
                    args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)")
                            for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
                    self.assertIn(f"resfs/src/resfs/file/{root}/{PROJECT}/value.txt={PROJECT}/value.txt", args)

    def test_uncompressed_inline_resource_preserves_a_quoted_value(self):
        self.write("ya.make", 'LIBRARY()\nRESOURCE(DONT_COMPRESS - "conftest.py=\'forty two\'")\nEND()\n')
        graph = self.graph()
        args = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("conftest.py=" in arg and "forty two" in arg for arg in args), args)
