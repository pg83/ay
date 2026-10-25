import unittest

from tst import lib


class ExecTestTouchTest(unittest.TestCase):
    def test_exectest_uses_the_bare_unit_touch_command(self):
        graph = lib.make({
            "checks/ya.make": "EXECTEST()\nRUN(echo)\nEND()\n",
            "collection/ya.make": "UNION()\nEND()\n",
        }, "checks", "collection")
        checks = lib.node_by_output(graph, "$(B)/checks/checks.pkg.fake")
        touch = "$(S)/build/scripts/touch.py"
        args = checks["cmds"][0]["cmd_args"]
        self.assertEqual(args[-2:], [touch, checks["outputs"][0]])
        self.assertIn(touch, checks["inputs"])
        collection = lib.node_by_output(graph, "$(B)/collection/collection.pkg.fake")
        self.assertNotIn(touch, collection["inputs"])
        self.assertEqual(collection["cmds"][0]["cmd_args"][1], "-c")


if __name__ == "__main__":
    unittest.main(verbosity=2)
