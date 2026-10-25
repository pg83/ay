"""File discovery and selection have different dependency traversal rules.

Native ymake visits unused command inputs during configuration, but its include
fixer does not follow the EDT_Search path of an unused command. Both orders
were verified with native ymake using a fresh configuration (-xx).
"""

import unittest

from tst import lib


def fixture():
    files = {
        "muted/ya.make": (
            "UNION()\nRUN_PROGRAM(tools/reader -o ${BINDIR}/muted.bin "
            "IN ${ARCADIA_ROOT}/grove/entries.gzt "
            "${ARCADIA_ROOT}/grove/article.gztproto OUT muted.bin)\nEND()\n"
        ),
        "unused_owner/ya.make": (
            "LIBRARY()\n" + lib.BARE_MODULE + "SET(DONT_RESOLVE_INCLUDES no)\n"
            "RUN_PROGRAM(tools/reader -o ${BINDIR}/unused.bin "
            "IN ${ARCADIA_ROOT}/grove/article.gztproto OUT_NOAUTO unused.bin)\nEND()\n"
        ),
        "grove/entries.gzt": 'import "grove/article.gztproto";\n',
        "grove/article.gztproto": 'import "grove/base.proto";\n',
        "grove/base.proto": 'import "common/schema.proto";\n',
        "build/sysincl/misc.yml": (
            "- includes:\n    - common/schema.proto:\n"
            "      - vendor/oak/common/schema.proto\n"
            "      - vendor/elm/common/schema.proto\n"
            "      - vendor/ash/common/schema.proto\n"
        ),
    }
    for tree in ("oak", "elm", "ash"):
        files[f"vendor/{tree}/common/schema.proto"] = ""
    lib.tool_program(files, "tools/reader", "reader")
    return files


class LateFileDiscoveryTest(unittest.TestCase):
    def check_order(self, targets, bound):
        graph = lib.make(fixture(), targets[-1], *targets[:-1],
                         "--target-platform", "default-linux-x86_64")
        command = lib.node_by_output(graph, "$(B)/muted/muted.bin")
        inputs = {
            "$(B)/tools/reader/reader", "$(S)/grove/entries.gzt",
            "$(S)/grove/article.gztproto",
        }
        if bound:
            inputs.add("$(S)/grove/base.proto")
            inputs.update(f"$(S)/vendor/{tree}/common/schema.proto"
                          for tree in ("oak", "elm", "ash"))
        self.assertEqual(set(command["inputs"]), inputs)
        self.assertFalse(any("$(B)/unused_owner/unused.bin" in node["outputs"]
                             for node in graph["graph"]))

    def test_unused_owner_first_establishes_bindings_without_selecting_its_command(self):
        self.check_order(("unused_owner", "muted"), bound=True)

    def test_unused_owner_after_muted_does_not_resolve_the_deferred_file(self):
        self.check_order(("muted", "unused_owner"), bound=False)


if __name__ == "__main__":
    unittest.main(verbosity=2)
