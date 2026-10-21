import unittest

import lib


EMPTY_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"
ENUM_PARSER = "$(B)/tools/enum_parser/enum_parser/enum_parser"


def enum_files(files):
    files.update({
        "util/generic/serialized_enum.h": "",
        "tools/enum_parser/enum_serialization_runtime/ya.make": EMPTY_LIBRARY,
    })
    lib.tool_program(files, "tools/enum_parser/enum_parser", "enum_parser")
    return files


class EnumSerializationTest(unittest.TestCase):
    def test_alice_noexcept_serialization_emits_only_a_header_and_induces_its_inputs(self):
        tool = "yandex_io/tools/enum_serializer_generator"
        runtime = tool + "/runtime/enum_runtime.h"
        files = {
            "m/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION(types.h)\nSRCS(use.cpp)\nEND()\n"
            ),
            "m/types.h": '#include "nested.h"\nenum E {A};\n',
            "m/nested.h": "",
            "m/use.cpp": '#include "m/types.h_serialized.h"\n',
            runtime: '#include "detail.h"\n',
            tool + "/runtime/detail.h": "",
        }
        lib.tool_program(files, tool, "enum_serializer_generator")
        files[tool + "/ya.make"] = files[tool + "/ya.make"].replace(
            "END()", f"INDUCED_DEPS(h ${{ARCADIA_ROOT}}/{runtime})\nEND()")
        graph = lib.make(files, "m")
        output = "$(B)/m/types.h_serialized.h"
        generated = lib.node_by_output(graph, output)
        tool_output = f"$(B)/{tool}/enum_serializer_generator"
        self.assertEqual(generated["outputs"], [output])
        self.assertEqual(generated["kv"], {"p": "EN", "pc": "yellow"})
        self.assertEqual(generated["cmds"][0]["cmd_args"], [
            tool_output, "--input_path", "$(S)/m/types.h",
            "--include_path", "m/types.h", "--output_path", output,
        ])
        self.assertEqual(generated["inputs"], [tool_output, "$(S)/m/types.h", "$(S)/m/nested.h"])
        compiled = lib.node_by_output(graph, "$(B)/m/use.cpp.o")
        for header in ("m/types.h", "m/nested.h", runtime, tool + "/runtime/detail.h"):
            self.assertIn("$(S)/" + header, compiled["inputs"])
        self.assertIn(output, compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertFalse(any(
            "_serialized.cpp" in output
            for node in graph["graph"] for output in node["outputs"]
        ))

    def test_alice_noexcept_serialization_uses_a_generated_header_producer(self):
        files = {
            "m/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "COPY_FILE_WITH_CONTEXT(template.h ${ARCADIA_BUILD_ROOT}/shared/types.h)\n"
                "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION(shared/types.h)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            "m/template.h": '#include "m/nested.h"\n',
            "m/nested.h": "",
            "m/use.cpp": '#include "m/shared/types.h_serialized.h"\n',
        }
        lib.tool_program(files, "yandex_io/tools/enum_serializer_generator", "enum_serializer_generator")
        graph = lib.make(files, "m")
        copied = lib.node_by_output(graph, "$(B)/shared/types.h")
        generated = lib.node_by_output(graph, "$(B)/m/shared/types.h_serialized.h")
        self.assertIn("$(B)/shared/types.h", generated["inputs"])
        for header in ("$(S)/m/template.h", "$(S)/m/nested.h"):
            self.assertIn(header, generated["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
        compiled = lib.node_by_output(graph, "$(B)/m/use.cpp.o")
        self.assertIn("$(S)/m/template.h", compiled["inputs"])
        self.assertIn("$(S)/m/nested.h", compiled["inputs"])

    def test_root_relative_generated_header_resolves_from_registry(self):
        graph = lib.make(enum_files({
            "p/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}COPY_FILE(template.h ${{ARCADIA_BUILD_ROOT}}/shared/enums.h)\n"
                "GENERATE_ENUM_SERIALIZATION(shared/enums.h)\nEND()\n"
            ),
            "p/template.h": "enum E {A};\n",
        }), "p")
        source = lib.node_by_output(graph, "$(B)/shared/enums.h")
        generated = lib.node_by_output(graph, "$(B)/p/shared/enums.h_serialized.cpp")
        self.assertIn("$(B)/shared/enums.h", generated["inputs"])
        self.assertIn(source["uid"], generated["deps"])
        self.assertEqual(generated["cmds"][0]["cmd_args"][1], "$(B)/shared/enums.h")

    def test_parent_header_output_is_normalized_before_registration(self):
        graph = lib.make(enum_files({
            "p/sub/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(../enums.h)\nEND()\n"
            ),
            "p/enums.h": "enum E {A};\n",
        }), "p/sub")
        generated = lib.node_by_output(graph, "$(B)/p/enums.h_serialized.cpp")
        self.assertEqual(generated["outputs"], [
            "$(B)/p/enums.h_serialized.cpp", "$(B)/p/enums.h_serialized.h",
        ])
        compiled = lib.node_by_output(graph, "$(B)/p/sub/__/enums.h_serialized.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertFalse(any("/../" in a for c in generated["cmds"] for a in c["cmd_args"]))

    def test_variants_and_header_locations(self):
        graph = lib.make(enum_files({
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "GENERATE_ENUM_SERIALIZATION(enums.h)\n"
                "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(sub/enums2.h)\n"
                "GENERATE_ENUM_SERIALIZATION(${ARCADIA_ROOT}/other/enums3.h)\n"
                "COPY_FILE(tpl.h gen.h)\n"
                "GENERATE_ENUM_SERIALIZATION(gen.h)\n"
                "COPY_FILE(tpl.h gen2.h)\n"
                "GENERATE_ENUM_SERIALIZATION(mod/gen2.h)\n"
                "GENERATE_ENUM_SERIALIZATION(mod/rooted.h)\n"
                "SRCDIR(srcd)\n"
                "GENERATE_ENUM_SERIALIZATION(enums4.h)\n"
                "END()\n"
            ),
            "mod/enums.h": "enum E {A};\n",
            "mod/sub/enums2.h": "",
            "mod/tpl.h": "",
            "other/enums3.h": "",
            "mod/rooted.h": "",
            "srcd/enums4.h": "",
        }), "mod")
        parser = lib.node_by_output(graph, ENUM_PARSER)

        plain = lib.node_by_output(graph, "$(B)/mod/enums.h_serialized.cpp")
        self.assertEqual(plain["kv"], {"p": "EN", "pc": "yellow"})
        self.assertEqual(plain["cmds"][0]["cmd_args"], [
            ENUM_PARSER, "$(S)/mod/enums.h",
            "--include-path", "mod/enums.h",
            "--output", "$(B)/mod/enums.h_serialized.cpp",
        ])
        self.assertEqual(plain["inputs"], [ENUM_PARSER, "$(S)/mod/enums.h"])
        self.assertEqual(plain["foreign_deps"], {"tool": [parser["uid"]]})
        plain_compile = lib.node_by_output(graph, "$(B)/mod/enums.h_serialized.cpp.o")
        self.assertEqual(lib.bucketed(plain_compile["inputs"], 1), lib.bucketed([
            "$(B)/mod/enums.h_serialized.cpp",
            "$(S)/mod/enums.h",
            "$(S)/util/generic/serialized_enum.h",
        ], 1))

        with_header = lib.node_by_output(graph, "$(B)/mod/sub/enums2.h_serialized.h")
        self.assertEqual(with_header["outputs"], [
            "$(B)/mod/sub/enums2.h_serialized.cpp",
            "$(B)/mod/sub/enums2.h_serialized.h",
        ])
        self.assertEqual(with_header["cmds"][0]["cmd_args"][-4:], [
            "--output", "$(B)/mod/sub/enums2.h_serialized.cpp",
            "--header", "$(B)/mod/sub/enums2.h_serialized.h",
        ])
        lib.node_by_output(graph, "$(B)/mod/_/sub/enums2.h_serialized.cpp.o")

        rooted = lib.node_by_output(graph, "$(B)/other/enums3.h_serialized.cpp")
        self.assertEqual(rooted["cmds"][0]["cmd_args"][1:4], [
            "$(S)/other/enums3.h", "--include-path", "other/enums3.h",
        ])
        lib.node_by_output(graph, "$(B)/mod/__/other/enums3.h_serialized.cpp.o")

        copy = lib.node_by_output(graph, "$(B)/mod/gen.h")
        generated = lib.node_by_output(graph, "$(B)/mod/gen.h_serialized.cpp")
        self.assertEqual(generated["cmds"][0]["cmd_args"][1:4], [
            "$(B)/mod/gen.h", "--include-path", "mod/gen.h",
        ])
        self.assertEqual(generated["inputs"], [ENUM_PARSER, "$(B)/mod/gen.h"])
        self.assertIn(copy["uid"], generated["deps"])

        module_rooted = lib.node_by_output(graph, "$(B)/mod/mod/rooted.h_serialized.cpp")
        self.assertEqual(module_rooted["cmds"][0]["cmd_args"][1:4], [
            "$(S)/mod/rooted.h", "--include-path", "mod/rooted.h",
        ])
        lib.node_by_output(graph, "$(B)/mod/_/mod/rooted.h_serialized.cpp.o")

        module_generated = lib.node_by_output(graph, "$(B)/mod/mod/gen2.h_serialized.cpp")
        self.assertEqual(module_generated["inputs"], [ENUM_PARSER, "$(B)/mod/gen2.h"])
        self.assertIn(lib.node_by_output(graph, "$(B)/mod/gen2.h")["uid"], module_generated["deps"])

        srcdir = lib.node_by_output(graph, "$(B)/mod/enums4.h_serialized.cpp")
        self.assertEqual(srcdir["cmds"][0]["cmd_args"][1:4], [
            "$(S)/srcd/enums4.h", "--include-path", "srcd/enums4.h",
        ])

    def test_generated_header_is_the_only_source_input(self):
        # Every EN node in the devtools_ymake_bin, arc_server_bin and
        # devtools_ya_bin_2 references lists the enum_parser and the header
        # alone: neither the induced includes of a generated .pb.h nor, for
        # middlesearch calcer_codegen.h, the BASE_CODEGEN main output.
        files = enum_files({
            "m/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "BASE_CODEGEN(tool base_gen)\n"
                "GENERATE_ENUM_SERIALIZATION(${BINDIR}/base_gen.h)\n"
                "END()\n"
            ),
            "m/base_gen.in": "",
            "inc/ind.h": "",
        })
        lib.tool_program(files, "tool", "base_gen")
        files["tool/ya.make"] = files["tool/ya.make"].replace(
            "END()", "INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/inc/ind.h)\nEND()")
        graph = lib.make(files, "m")
        node = lib.node_by_output(graph, "$(B)/m/base_gen.h_serialized.cpp")
        self.assertEqual(node["inputs"], [ENUM_PARSER, "$(B)/m/base_gen.h"])

    def test_proto_library_cpp_variant_serializes_and_python_variant_skips(self):
        files = enum_files({
            "pl/ya.make": "PROTO_LIBRARY()\nGENERATE_ENUM_SERIALIZATION(enums.h)\nEND()\n",
            "pl/enums.h": "",
            "py/ya.make": "PY3_LIBRARY()\nPEERDIR(pl)\nEND()\n",
            "contrib/libs/protobuf/ya.make": EMPTY_LIBRARY,
            "contrib/libs/python/ya.make": EMPTY_LIBRARY,
        })
        cpp_files = dict(files)
        cpp_files["pl/ya.make"] = files["pl/ya.make"].replace("PROTO_LIBRARY()", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)")
        cpp = lib.make(cpp_files, "pl")
        node = lib.node_by_output(cpp, "$(B)/pl/enums.h_serialized.cpp")
        self.assertEqual(node["inputs"], [ENUM_PARSER, "$(S)/pl/enums.h"])
        lib.node_by_output(cpp, "$(B)/pl/enums.h_serialized.cpp.o")

        python = lib.make(files, "py")
        self.assertEqual(
            [n for n in python["graph"] if n["kv"]["p"] == "EN"], [],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
