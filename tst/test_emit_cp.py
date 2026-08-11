import unittest

import lib


class EmitCopyFileTest(unittest.TestCase):
    def test_included_macro_uses_source_root_input(self):
        graph = lib.make({
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/copy.ya.make.inc)\n"
                "SRCS(use.cpp)\n"
                "END()\n"
            ),
            "mod/use.cpp": '#include "shared/generated.h"\n',
            "shared/copy.ya.make.inc": (
                "COPY_FILE(\n"
                "  TEXT\n"
                "  shared/generated.txt\n"
                "  ${BINDIR}/shared/generated.h\n"
                ")\n"
            ),
            "shared/generated.txt": "generated\n",
        }, "mod")
        copy = lib.node_by_output(graph, "$(B)/mod/shared/generated.h")
        self.assertIn("$(S)/shared/generated.txt", copy["inputs"])
        self.assertNotIn("$(S)/mod/shared/generated.txt", copy["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
