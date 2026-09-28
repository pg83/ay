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
    def test_variants_and_header_locations(self):
        graph = lib.make(enum_files({
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "GENERATE_ENUM_SERIALIZATION(enums.h)\n"
                "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(sub/enums2.h)\n"
                "GENERATE_ENUM_SERIALIZATION(${ARCADIA_ROOT}/other/enums3.h)\n"
                "COPY_FILE(tpl.h gen.h)\n"
                "GENERATE_ENUM_SERIALIZATION(gen.h)\n"
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
        self.assertEqual(plain_compile["inputs"], [
            "$(B)/mod/enums.h_serialized.cpp",
            "$(S)/mod/enums.h",
            "$(S)/util/generic/serialized_enum.h",
        ])

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

        srcdir = lib.node_by_output(graph, "$(B)/mod/enums4.h_serialized.cpp")
        self.assertEqual(srcdir["cmds"][0]["cmd_args"][1:4], [
            "$(S)/srcd/enums4.h", "--include-path", "srcd/enums4.h",
        ])

    def test_proto_library_cpp_variant_serializes_and_python_variant_skips(self):
        files = enum_files({
            "pl/ya.make": "PROTO_LIBRARY()\nGENERATE_ENUM_SERIALIZATION(enums.h)\nEND()\n",
            "pl/enums.h": "",
            "py/ya.make": "PY3_LIBRARY()\nPEERDIR(pl)\nEND()\n",
            "contrib/libs/protobuf/ya.make": EMPTY_LIBRARY,
            "contrib/libs/python/ya.make": EMPTY_LIBRARY,
        })
        cpp = lib.make(files, "pl")
        node = lib.node_by_output(cpp, "$(B)/pl/enums.h_serialized.cpp")
        self.assertEqual(node["inputs"], [ENUM_PARSER, "$(S)/pl/enums.h"])
        lib.node_by_output(cpp, "$(B)/pl/enums.h_serialized.cpp.o")

        python = lib.make(files, "py")
        self.assertEqual(
            [n for n in python["graph"] if n["kv"]["p"] == "EN"], [],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
