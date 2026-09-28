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

    def test_generated_output_includes_stay_out_of_the_copy_inputs(self):
        files = {
            "lib/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "RUN_PROGRAM(tools/gen OUT gen.h)\n"
                "COPY_FILE(src.h dst.h OUTPUT_INCLUDES gen.h)\n"
                "SRCS(a.cpp)\nEND()\n"
            ),
            "lib/src.h": '#include "gen.h"\n',
            "lib/a.cpp": '#include "dst.h"\nint a(){return 0;}\n',
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        self.assertEqual(lib.node_by_output(graph, "$(B)/lib/dst.h")["inputs"], ["$(S)/lib/src.h"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/lib/a.cpp.o")["inputs"],
            ["$(S)/lib/a.cpp", "$(B)/lib/dst.h", "$(B)/lib/gen.h"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
