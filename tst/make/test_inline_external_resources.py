import json

from tst.make.lib import MakeTest


class InlineExternalResourcesTest(MakeTest):
    def test_native_modules_can_declare_generator_resources(self):
        self.write("ya.make", "LIBRARY(answer)\nDECLARE_EXTERNAL_RESOURCE(E2E_INLINE sbr:123456789)\nRUN_PYTHON3(generate.py ${E2E_INLINE_RESOURCE_GLOBAL} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        graph = self.graph()
        node = self.node(graph, "generated.cpp")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.startswith("$(") and "E2E_INLINE" in arg for arg in args), args)
        self.assertIn("123456789", json.dumps(graph))
