import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptSharedToolContextTest(MakeTest):
    def test_codegen_tools_share_a_typescript_package_dependency(self):
        self.write("ya.make", f"LIBRARY(answer)\nRUN_PROGRAM({PROJECT}/first OUT first.cpp)\nRUN_PROGRAM({PROJECT}/second OUT second.cpp)\nEND()\n")
        for name, macros in (("first", ""), ("second", f"PEERDIR({PROJECT}/first)")):
            self.write(f"{name}/ya.make", f"TS_PACKAGE()\n{macros}\nEND()\n")
            self.write(f"{name}/package.json", json.dumps({"name": "@e2e/" + name, "version": "1.0.0", "engines": {"node": "22.13.0"}}))
            self.write(f"{name}/pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        graph = self.graph()
        for name in ("first", "second"):
            with self.subTest(tool=name):
                generated = self.node(graph, name + ".cpp")
                compiled = self.node(graph, name + ".cpp.o")
                self.assertIn(generated["uid"], compiled["deps"])
                tools = {node["uid"] for node in graph["graph"] if f"$(B)/{PROJECT}/{name}/output.tar" in node["outputs"]}
                self.assertTrue(tools.intersection(generated["deps"]))
