import unittest

from tst import lib
from tst.make.test_codegen_enum import enum_files


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
        self.assertEqual([file for file in suite["inputs"] if file.endswith((".c", ".cpp", ".h"))], sorted(files))
        return suite

    def test_cpp_and_json_checks_keep_separate_source_lists(self):
        files = self.files("STYLE_JSON()\nSRCS(local.cpp)", **{
            "m/local.cpp": "int local;\n",
            "m/settings.json": "{}\n",
            "build/config/tests/json_style/default_configs.json":
                '{"clang_format_json":"configs/json.clang-format"}',
            "configs/json.clang-format": "BasedOnStyle: LLVM\n",
        })
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/m/local.cpp"])
        json_suite = lib.node_by_output(graph, "$(B)/m/test-results/clang_format_json/meta.json")
        self.assertIn("$(S)/m/settings.json", json_suite["inputs"])
        self.assertNotIn("$(S)/m/local.cpp", json_suite["inputs"])

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

    def test_run_program_checks_declared_inputs_without_tool_sources_or_incidental_headers(self):
        files = self.files("COPY_FILE(${ARCADIA_ROOT}/shared/template.cpp stage.cpp)\n"
                           "RUN_PROGRAM(tools/generator IN ${BINDIR}/stage.cpp ${ARCADIA_ROOT}/shared/template.cpp "
                           "${ARCADIA_ROOT}/shared/schema.h ${ARCADIA_ROOT}/shared/template.cpp "
                           "${ARCADIA_ROOT}/shared/notes.txt OUT generated.cpp)", **{
            "shared/template.cpp": '#include "incidental.h"\nint value;\n',
            "shared/schema.h": "\n",
            "shared/incidental.h": "\n",
            "shared/notes.txt": "\n",
        })
        lib.tool_program(files, "tools/generator", "generator")
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/shared/schema.h", "$(S)/shared/template.cpp"])
        generated = lib.node_by_output(graph, "$(B)/m/generated.cpp")
        copied = lib.node_by_output(graph, "$(B)/m/stage.cpp")
        self.assertIn(copied["uid"], generated["deps"])
        self.assertIn("$(B)/m/stage.cpp", generated["inputs"])
        for source in ("template.cpp", "schema.h", "incidental.h", "notes.txt"):
            self.assertIn("$(S)/shared/" + source, generated["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/m/generated.cpp.o")
        self.assertIn("$(B)/m/generated.cpp", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_decimal_hash_checks_all_explicit_inputs(self):
        files = self.files("DECIMAL_MD5_LOWER_32_BITS(hash.cpp FUNCNAME hash "
                           "../shared/first.cpp ../shared/second.h ../shared/notes.txt)", **{
            "shared/first.cpp": "int first;\n",
            "shared/second.h": "\n",
            "shared/notes.txt": "\n",
        })
        graph = lib.make(files, "m", "-t")
        self.assert_checked(graph, ["$(S)/shared/first.cpp", "$(S)/shared/second.h"])
        generated = lib.node_by_output(graph, "$(B)/m/hash.cpp")
        compiled = lib.node_by_output(graph, "$(B)/m/hash.cpp.o")
        for source in ("first.cpp", "second.h", "notes.txt"):
            self.assertIn("$(S)/shared/" + source, generated["inputs"])
            self.assertNotIn("$(S)/shared/" + source, compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_cython_checks_embedded_text_inputs_without_induced_compiler_headers(self):
        from tst.make.test_python_cython import CYTHON_HEADERS, CYTHON_UTILITY, python_base
        files = python_base()
        files.update(self.files("BUILDWITH_CYTHON_CPP(module.pyx)", **{"m/module.pyx": "pass\n"}))
        graph = lib.make(files, "m", "-t")
        expected = ["$(S)/contrib/tools/cython/Cython/Utility/" + name
                    for name in CYTHON_UTILITY if name.endswith((".c", ".cpp", ".h"))]
        suite = self.assert_checked(graph, expected)
        for header in CYTHON_HEADERS:
            self.assertNotIn("$(S)/" + header, suite["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
