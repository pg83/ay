import unittest

import lib
from test_go_tests import go_tool_args, go_world


def files(module, declaration):
    sources = go_world()
    sources.update({
        "g/ya.make": f"{module}()\n{lib.BARE_MODULE}SRCS(main.go)\nRUN_PYTHON3(gen.py {declaration})\nEND()\n",
        "g/main.go": "package main\nfunc main() {}\n",
        "g/gen.py": "",
    })
    return sources


class GoRunPythonTest(unittest.TestCase):
    def test_python_auto_outputs_are_go_module_sources_and_keep_producers(self):
        for module, output in (("GO_LIBRARY", "g.a"), ("GO_PROGRAM", "g")):
            for declaration in ("OUT generated.go", "STDOUT generated.go", "OUT_NOAUTO generated.go", "STDOUT_NOAUTO generated.go"):
                with self.subTest(module=module, declaration=declaration):
                    graph = lib.make(files(module, declaration), "g")
                    consumer = lib.node_by_output(graph, "$(B)/g/" + output)
                    producer = lib.node_by_output(graph, "$(B)/g/generated.go")
                    args = go_tool_args(consumer)
                    self.assertIn("$(B)/g/generated.go", args)
                    self.assertLess(args.index("$(B)/g/generated.go"), args.index("$(S)/g/main.go"))
                    self.assertIn("$(B)/g/generated.go", consumer["inputs"])
                    self.assertIn(producer["uid"], consumer["deps"])

    def test_header_python_outputs_do_not_become_go_sources(self):
        for declaration in ("OUT unused.h", "STDOUT unused.h", "OUT_NOAUTO unused.h", "STDOUT_NOAUTO unused.h"):
            with self.subTest(declaration=declaration):
                graph = lib.make(files("GO_LIBRARY", declaration), "g")
                consumer = lib.node_by_output(graph, "$(B)/g/g.a")
                self.assertNotIn("$(B)/g/unused.h", go_tool_args(consumer))
                self.assertNotIn("$(B)/g/unused.h", consumer["inputs"])
                self.assertFalse(any("$(B)/g/unused.h" in node["outputs"] for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
