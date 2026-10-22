import unittest

import lib
from test_resource_from_sandbox import SCRIPTS, sandbox_nodes


BODY = (
    "FROM_SANDBOX(812 PREFIX ${PREFIX}assets "
    "OUT ${PREFIX}assets/main.h OUT_NOAUTO ${PREFIX}assets/extra.h "
    "OUTPUT_INCLUDES shared/leaf.h)\n"
)


def module(prefix, tail=""):
    return (f"LIBRARY()\n{lib.BARE_MODULE}SET(PREFIX {prefix})\n"
            "INCLUDE(${ARCADIA_ROOT}/shared/fetch.inc)\n"
            + tail + "\nSRCS(use.cpp)\nEND()\n")


class SandboxPrefixScopeTest(unittest.TestCase):
    def check_sandbox(self, graph, project, prefix):
        main = f"$(B)/{project}/{prefix}assets/main.h"
        extra = f"$(B)/{project}/{prefix}assets/extra.h"
        node = lib.node_by_output(graph, main)
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--untar-to") + 1], prefix + "assets")
        self.assertEqual(node["outputs"], [main, extra])
        self.assertEqual(node["inputs"], SCRIPTS)
        self.assertEqual(node["cmds"][0]["cwd"], "$(B)/" + project)
        self.assertEqual(node["requirements"]["network"], "full")
        compiled = lib.node_by_output(graph, f"$(B)/{project}/use.cpp.o")
        self.assertIn(main, compiled["inputs"])
        self.assertIn(extra, compiled["inputs"])
        self.assertIn("$(S)/shared/leaf.h", compiled["inputs"])
        self.assertIn(node["uid"], compiled["deps"])
        archive = lib.node_by_output(graph, f"$(B)/{project}/lib{project}.a")
        self.assertIn(compiled["outputs"][0], archive["inputs"])
        self.assertIn(compiled["uid"], archive["deps"])
        return node

    def test_shared_include_captures_each_module_prefix_in_both_root_orders(self):
        files = {"shared/fetch.inc": BODY, "shared/leaf.h": ""}
        for project, prefix in (("copper", "warm/"), ("silver", "cool/")):
            files[f"{project}/ya.make"] = module(prefix, "SET(PREFIX later/)")
            files[f"{project}/use.cpp"] = f'#include <{project}/{prefix}assets/extra.h>\n'
        for roots in (("copper", "silver"), ("silver", "copper")):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                for project, prefix in (("copper", "warm/"), ("silver", "cool/")):
                    self.check_sandbox(graph, project, prefix)

    def test_repeated_include_retains_each_source_binding(self):
        files = {
            "shared/fetch.inc": BODY,
            "shared/leaf.h": "",
            "copper/ya.make": module("one/", "SET(PREFIX two/)\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/fetch.inc)\nSET(PREFIX last/)"),
            "copper/use.cpp": '#include <copper/one/assets/extra.h>\n'
                '#include <copper/two/assets/extra.h>\n',
        }
        graph = lib.make(files, "copper")
        self.assertEqual(len(sandbox_nodes(graph)), 2)
        for prefix in ("one/", "two/"):
            self.check_sandbox(graph, "copper", prefix)


if __name__ == "__main__":
    unittest.main(verbosity=2)
