import unittest

import lib
from test_codegen_enum import enum_files


class CppStyleProducerSourcesTest(unittest.TestCase):
    def files(self, body, **extra):
        return {
            "m/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "STYLE_CPP()\n" + body + "\nEND()\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
            **extra,
        }

    def assert_checked(self, graph, files):
        suite = lib.node_by_output(graph, "$(B)/m/test-results/clang_format/meta.json")
        args = suite["cmds"][0]["cmd_args"]
        config = args.index("--config")
        self.assertEqual(args[config + 2:], sorted(files))
        self.assertEqual([file for file in suite["inputs"] if file.endswith((".cpp", ".h"))], sorted(files))
        return suite

    def test_copy_with_context_checks_original_source_and_preserves_generated_compile_input(self):
        files = self.files("COPY_FILE_WITH_CONTEXT(AUTO ../shared/original.cpp copied.cpp)", **{
            "shared/original.cpp": '#include "included.h"\nint value;\n',
            "shared/included.h": "\n",
        })
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/shared/original.cpp"])
        copied = lib.node_by_output(graph, "$(B)/m/copied.cpp")
        compiled = lib.node_by_output(graph, "$(B)/m/copied.cpp.o")
        self.assertIn("$(B)/m/copied.cpp", compiled["inputs"])
        self.assertIn("$(S)/shared/included.h", compiled["inputs"])
        self.assertIn(copied["uid"], compiled["deps"])

    def test_copy_chain_follows_original_cpp_source_through_non_cpp_outputs(self):
        files = self.files("COPY_FILE(../shared/original.cpp stage.txt)\n"
                           "COPY_FILE_WITH_CONTEXT(AUTO stage.txt copied.cpp)", **{
            "shared/original.cpp": "int value;\n",
        })
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/shared/original.cpp"])
        stage = lib.node_by_output(graph, "$(B)/m/stage.txt")
        copied = lib.node_by_output(graph, "$(B)/m/copied.cpp")
        self.assertIn(stage["uid"], copied["deps"])

    def test_enum_serialization_checks_its_source_header(self):
        files = enum_files(self.files("GENERATE_ENUM_SERIALIZATION(../shared/enum.h)", **{
            "shared/enum.h": '#include "included.h"\nenum E {A};\n',
            "shared/included.h": "\n",
        }))
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/shared/enum.h"])
        generated = lib.node_by_output(graph, "$(B)/shared/enum.h_serialized.cpp")
        self.assertIn("$(S)/shared/enum.h", generated["inputs"])

    def test_explicit_build_source_without_producer_metadata_has_no_invented_source_input(self):
        graph = lib.make(self.files("SRCS(${BINDIR}/external.cpp)"), "m", "-t")
        self.assert_checked(graph, [])
        compiled = lib.node_by_output(graph, "$(B)/m/external.cpp.o")
        self.assertIn("$(B)/m/external.cpp", compiled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
