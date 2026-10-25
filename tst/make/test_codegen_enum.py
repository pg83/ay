import unittest

from tst import lib


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
    def test_cyclic_header_inputs_survive_another_first_scanner_entry(self):
        files = enum_files({
            "m/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                         + "SRCS(first.cpp)\nGENERATE_ENUM_SERIALIZATION_WITH_HEADER(enums.h)\nEND()\n",
            "m/first.cpp": '#include "other.h"\n',
            "m/other.h": '#include "enums.h"\n',
            "m/enums.h": '#include "other.h"\nenum E { A };\n',
        })
        graph = lib.make(files, "m")
        generated = lib.node_by_output(graph, "$(B)/m/enums.h_serialized.cpp")
        self.assertCountEqual(generated["inputs"],
                              [ENUM_PARSER, "$(S)/m/enums.h", "$(S)/m/other.h"])
        compiled = lib.node_by_output(graph, "$(B)/m/enums.h_serialized.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])

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

    def test_declared_source_header_closure_and_variant_output_includes(self):
        files = enum_files({
            "orchard/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "GENERATE_ENUM_SERIALIZATION(fruit.h)\n"
                "END()\n"
            ),
            "orchard/fruit.h": '#include "branch.h"\nenum Fruit {Apple};\n',
            "orchard/branch.h": '#include "leaf.h"\n',
            "orchard/leaf.h": "",
            "util/generic/runtime_leaf.h": "",
        })
        files["util/generic/serialized_enum.h"] = '#include "runtime_leaf.h"\n'
        files["grove/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}"
            "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(${ARCADIA_ROOT}/orchard/fruit.h)\n"
            "SRCS(use.cpp)\nEND()\n")
        files["grove/use.cpp"] = '#include "orchard/fruit.h_serialized.h"\n'
        for target, with_header in (("orchard", False), ("grove", True)):
            with self.subTest(with_header=with_header):
                graph = lib.make(files, target)
                generated = lib.node_by_output(graph, "$(B)/orchard/fruit.h_serialized.cpp")
                headers = {"$(S)/orchard/" + name for name in ("fruit.h", "branch.h", "leaf.h")}
                self.assertEqual(set(generated["inputs"]), {ENUM_PARSER, *headers})
                expected_args = [ENUM_PARSER, "$(S)/orchard/fruit.h", "--include-path",
                                 "orchard/fruit.h", "--output", "$(B)/orchard/fruit.h_serialized.cpp"]
                if with_header:
                    expected_args += ["--header", "$(B)/orchard/fruit.h_serialized.h"]
                self.assertEqual(generated["cmds"][0]["cmd_args"], expected_args)
                self.assertEqual(generated["deps"], [lib.node_by_output(graph, ENUM_PARSER)["uid"]])
                compiled = next(n for n in graph["graph"] if n["kv"]["p"] == "CC"
                                and any("fruit.h_serialized.cpp.o" in o for o in n["outputs"]))
                self.assertTrue(headers <= set(compiled["inputs"]))
                runtime = {"$(S)/util/generic/serialized_enum.h", "$(S)/util/generic/runtime_leaf.h"}
                self.assertEqual(runtime & set(compiled["inputs"]), set() if with_header else runtime)
                if with_header:
                    consumer = lib.node_by_output(graph, "$(B)/grove/use.cpp.o")
                    self.assertTrue(headers <= set(consumer["inputs"]))
                    self.assertIn("$(B)/orchard/fruit.h_serialized.cpp", consumer["inputs"])
                    self.assertIn(generated["uid"], consumer["deps"])
                    self.assertFalse(runtime & set(consumer["inputs"]))

    def test_generated_header_closure_preserves_primary_producer_and_induced_domains(self):
        for with_header in (False, True):
            with self.subTest(with_header=with_header):
                macro = "GENERATE_ENUM_SERIALIZATION" + ("_WITH_HEADER" if with_header else "")
                files = enum_files({
                    "canopy/ya.make": (
                        f"LIBRARY()\n{lib.BARE_MODULE}"
                        "COPY_FILE(template.h opaque.h)\n"
                        "COPY_FILE_WITH_CONTEXT(template.h contextual.h)\n"
                        "RUN_PROGRAM(tools/foliage trunk.cpp crown.h seed.dat "
                        "IN seed.dat OUT trunk.cpp OUT_NOAUTO crown.h)\n"
                        f"{macro}(opaque.h)\n{macro}(contextual.h)\n{macro}(crown.h)\nEND()\n"
                    ),
                    "canopy/template.h": '#include "canopy/branch.h"\n',
                    "canopy/branch.h": '#include "canopy/leaf.h"\n',
                    "canopy/leaf.h": "",
                    "canopy/seed.dat": "seed",
                    "foliage/header.h": '#include "header_leaf.h"\n',
                    "foliage/header_leaf.h": "",
                    "foliage/compile.h": "",
                    "foliage/shared.h": '#include "shared_leaf.h"\n',
                    "foliage/shared_leaf.h": "",
                })
                lib.tool_program(files, "tools/foliage", "foliage")
                files["tools/foliage/ya.make"] = files["tools/foliage/ya.make"].replace(
                    "END()", "INDUCED_DEPS(h ${ARCADIA_ROOT}/foliage/header.h)\n"
                    "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/foliage/compile.h)\n"
                    "INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/foliage/shared.h)\nEND()")
                graph = lib.make(files, "canopy")
                for header, extra in (
                    ("opaque.h", set()),
                    ("contextual.h", {"$(S)/canopy/" + name for name in ("template.h", "branch.h", "leaf.h")}),
                    ("crown.h", {"$(B)/canopy/trunk.cpp", *(
                        "$(S)/foliage/" + name for name in
                        ("header.h", "header_leaf.h", "shared.h", "shared_leaf.h"))}),
                ):
                    generated = lib.node_by_output(graph, f"$(B)/canopy/{header}_serialized.cpp")
                    self.assertEqual(set(generated["inputs"]), {ENUM_PARSER, f"$(B)/canopy/{header}", *extra})
                    producer = lib.node_by_output(graph, f"$(B)/canopy/{header}")
                    self.assertEqual(set(generated["deps"]), {
                        producer["uid"], lib.node_by_output(graph, ENUM_PARSER)["uid"]})
                    self.assertNotIn("$(S)/foliage/compile.h", generated["inputs"])

    def test_base_codegen_header_keeps_primary_output_and_induced_inputs(self):
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
        self.assertEqual(set(node["inputs"]), {
            ENUM_PARSER, "$(B)/m/base_gen.h", "$(B)/m/base_gen.cpp", "$(S)/inc/ind.h",
        })
        producer = lib.node_by_output(graph, "$(B)/m/base_gen.h")
        self.assertIn(producer["uid"], node["deps"])

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
