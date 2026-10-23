"""C++ typed source aliases resolve before native relative object naming."""
import unittest

import lib
from test_all_python_forwarding import complete_nodes

MODULE = "grove/deep/nest/unit"


def fixture(token, global_source=True):
    return {
        MODULE + "/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(" + ("GLOBAL " if global_source else "") + token + ")\nEND()\n",
        "sapling/metrics.cpp": '#include "metrics.h"\nint metrics;\n',
        "sapling/metrics.h": "#pragma once\n",
        MODULE + "/local.cpp": "int local;\n",
        MODULE + "/sub/local.cpp": "int nested;\n",
        "grove/deep/sibling.cpp": "int sibling;\n",
    }


class CppSourceAliasOutputTest(unittest.TestCase):
    def paired(self, alias, equivalent, output, global_source=True):
        actual = lib.make(fixture(alias, global_source), MODULE, "--sandboxing")
        expected = lib.make(fixture(equivalent, global_source), MODULE, "--sandboxing")
        self.assertEqual(complete_nodes(actual), complete_nodes(expected))
        self.assertEqual(actual["result"], expected["result"])
        compiled = lib.node_by_output(actual, "$(B)/" + MODULE + "/" + output)
        source = "$(S)/" + ("sapling/metrics.cpp" if "metrics" in alias else "grove/deep/sibling.cpp" if "sibling" in alias else MODULE + ("/sub/local.cpp" if "/sub/" in alias else "/local.cpp"))
        self.assertIn(source, compiled["inputs"])
        if "metrics" in alias:
            self.assertIn("$(S)/sapling/metrics.h", compiled["inputs"])
        archive = lib.node_by_output(actual, "$(B)/" + MODULE + "/libdeep-nest-unit" + (".global.a" if global_source else ".a"))
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(compiled["outputs"][0], archive["inputs"])
        self.assertIn(compiled["outputs"][0], [a for c in archive["cmds"] for a in c["cmd_args"]])

    def test_root_source_aliases_keep_real_source_and_relative_object_identity(self):
        for token in ("$S/sapling/metrics.cpp", "${ARCADIA_ROOT}/sapling/metrics.cpp"):
            for global_source in (False, True):
                with self.subTest(token=token, global_source=global_source):
                    self.paired(token, "../../../../sapling/metrics.cpp", "__/__/__/__/sapling/metrics.cpp.o", global_source)

    def test_module_local_descendant_and_shared_prefix_aliases(self):
        for source, equivalent, output in (
            (MODULE + "/local.cpp", "local.cpp", "local.cpp.o"),
            (MODULE + "/sub/local.cpp", "sub/local.cpp", "_/sub/local.cpp.o"),
            ("grove/deep/sibling.cpp", "../../sibling.cpp", "__/__/sibling.cpp.o"),
        ):
            with self.subTest(source=source):
                self.paired("$S/" + source, equivalent, output)


if __name__ == "__main__":
    unittest.main(verbosity=2)
