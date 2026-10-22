import unittest

import lib


LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class CopyMacroTest(unittest.TestCase):
    def assert_copy(self, graph, source, destination):
        node = lib.node_by_output(graph, destination)
        self.assertEqual(node["kv"], {"p": "CP", "pc": "light-cyan"})
        self.assertEqual(node["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/fs_tools.py", "copy", source, destination,
        ])
        self.assertEqual(node["inputs"], [source])
        return node

    def test_optional_directories_and_default_basename(self):
        graph = lib.make({
            "parcel/ya.make": (
                "UNION()\nCOPY()\nCOPY(ignored/tree/local.dat)\n"
                "COPY(KEEP_DIR_STRUCT kept/nested/local.cfg)\n"
                "COPY(renamed/flat.cfg DESTINATION installed)\n"
                "COPY(remote.bin FROM assets/payload)\nEND()\n"
            ),
            "parcel/local.dat": "local\n",
            "parcel/kept/nested/local.cfg": "kept\n",
            "parcel/flat.cfg": "flat\n",
            "assets/payload/remote.bin": "remote\n",
        }, "parcel")
        for source, destination in (
            ("parcel/local.dat", "parcel/local.dat"),
            ("parcel/kept/nested/local.cfg", "parcel/kept/nested/local.cfg"),
            ("parcel/flat.cfg", "parcel/installed/flat.cfg"),
            ("assets/payload/remote.bin", "parcel/remote.bin"),
        ):
            self.assert_copy(graph, "$(S)/" + source, "$(B)/" + destination)

    def test_directory_options_accept_all_root_spellings_and_positions(self):
        for origin, destination in (
            ("${CURDIR}/incoming", "${BINDIR}/installed"),
            ("${ARCADIA_ROOT}/parcel/incoming", "${ARCADIA_BUILD_ROOT}/distribution"),
            ("parcel/incoming", "installed"),
        ):
            with self.subTest(origin=origin, destination=destination):
                graph = lib.make({
                    "parcel/ya.make": (
                        "UNION()\n"
                        f"COPY(discarded/deep/item.dat FROM {origin} DESTINATION {destination})\n"
                        f"COPY(DESTINATION {destination} FROM {origin} other.dat)\nEND()\n"
                    ),
                    "parcel/incoming/item.dat": "item\n",
                    "parcel/incoming/other.dat": "other\n",
                }, "parcel")
                prefix = "distribution" if destination.startswith("${ARCADIA_BUILD_ROOT}") else "parcel/installed"
                for filename in ("item.dat", "other.dat"):
                    self.assert_copy(graph, "$(S)/parcel/incoming/" + filename, "$(B)/" + prefix + "/" + filename)

    def test_keep_structure_and_result_preserve_python_joined_paths(self):
        files = {
            "parcel/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/parcel)\n"
                "COPY(KEEP_DIR_STRUCT /nested//item.dat flat.dat FROM ${CURDIR}/incoming "
                "DESTINATION installed//stage/../ RESULT COPIED_PARCELS)\n"
                "CONFIGURE_FILE(paths.h.in paths.h)\n"
                "RUN_PROGRAM(tools/parcel_reader --copied ${COPIED_PARCELS} "
                "IN ${COPIED_PARCELS} OUT_NOAUTO report.txt)\nSRCS(use.cpp)\nEND()\n"
            ),
            "parcel/incoming/nested/item.dat": "nested\n",
            "parcel/incoming/flat.dat": "flat\n",
            "parcel/paths.h.in": "// @COPIED_PARCELS@\n",
            "parcel/use.cpp": '#include "paths.h"\n#include "report.txt"\n',
        }
        lib.tool_program(files, "tools/parcel_reader", "parcel_reader")
        graph = lib.make(files, "parcel")
        copies = [self.assert_copy(graph, "$(S)/parcel/incoming/" + relative, "$(B)/parcel/installed/" + relative)
                  for relative in ("nested/item.dat", "flat.dat")]
        report = lib.node_by_output(graph, "$(B)/parcel/report.txt")
        args = report["cmds"][0]["cmd_args"]
        start = args.index("--copied")
        self.assertEqual(args[start + 1:start + 3], [copied["outputs"][0] for copied in copies])
        configured = lib.node_by_output(graph, "$(B)/parcel/paths.h")
        self.assertIn("COPIED_PARCELS=installed//stage/../nested/item.dat installed//stage/../flat.dat",
                      configured["cmds"][0]["cmd_args"])
        for copied in copies:
            self.assertIn(copied["outputs"][0], report["inputs"])
            self.assertIn(copied["uid"], report["deps"])

    def test_empty_result_replaces_an_existing_variable(self):
        files = {
            "parcel/ya.make": (
                "UNION()\nSET(COPIED_PARCELS obsolete)\nCOPY(RESULT COPIED_PARCELS)\n"
                "RUN_PROGRAM(tools/parcel_reader --copied=${COPIED_PARCELS} OUT_NOAUTO report.txt)\nEND()\n"
            ),
        }
        lib.tool_program(files, "tools/parcel_reader", "parcel_reader")
        graph = lib.make(files, "parcel")
        report = lib.node_by_output(graph, "$(B)/parcel/report.txt")
        self.assertIn("--copied=", report["cmds"][0]["cmd_args"])
        self.assertNotIn("obsolete", str(report))
        self.assertFalse(any(node["kv"] == {"p": "CP", "pc": "light-cyan"} for node in graph["graph"]))

    def test_auto_and_context_options_propagate_only_declared_output_context(self):
        graph = lib.make({
            "parcel/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/parcel)\n"
                "COPY(shape.cpp FROM shared/incoming DESTINATION plain AUTO)\n"
                "COPY(shape.cpp FROM shared/incoming DESTINATION contextual WITH_CONTEXT AUTO "
                "OUTPUT_INCLUDES shared/first.h OUTPUT_INCLUDES shared/second.h)\nEND()\n"
            ),
            "shared/incoming/shape.cpp": '#include "detail.h"\n',
            "shared/incoming/detail.h": "",
            "shared/first.h": "",
            "shared/second.h": "",
        }, "parcel")
        for directory in ("plain", "contextual"):
            self.assert_copy(graph, "$(S)/shared/incoming/shape.cpp", f"$(B)/parcel/{directory}/shape.cpp")
        plain = lib.node_by_output(graph, "$(B)/parcel/_/plain/shape.cpp.o")
        contextual = lib.node_by_output(graph, "$(B)/parcel/_/contextual/shape.cpp.o")
        for source in ("shared/incoming/shape.cpp", "shared/incoming/detail.h", "shared/first.h", "shared/second.h"):
            self.assertNotIn("$(S)/" + source, plain["inputs"])
            self.assertIn("$(S)/" + source, contextual["inputs"])

    def test_generated_inputs_keep_their_registered_producers(self):
        graph = lib.make({
            "parcel/ya.make": (
                "UNION()\nCOPY_FILE(seed.dat generated/item.dat)\n"
                "COPY(item.dat FROM ${BINDIR}/generated DESTINATION staged)\n"
                "COPY(KEEP_DIR_STRUCT staged/item.dat FROM ${BINDIR} DESTINATION final)\nEND()\n"
            ),
            "parcel/seed.dat": "seed\n",
        }, "parcel")
        first = self.assert_copy(graph, "$(S)/parcel/seed.dat", "$(B)/parcel/generated/item.dat")
        second = self.assert_copy(graph, "$(B)/parcel/generated/item.dat", "$(B)/parcel/staged/item.dat")
        third = self.assert_copy(graph, "$(B)/parcel/staged/item.dat", "$(B)/parcel/final/staged/item.dat")
        self.assertIn(first["uid"], second["deps"])
        self.assertIn(second["uid"], third["deps"])

    def test_shared_lexical_call_expands_in_each_module_environment(self):
        files = {
            "shared/parcel.inc": "COPY(KEEP_DIR_STRUCT nested/item.dat FROM ${ORIGIN} DESTINATION ${DEST})\n",
        }
        for name in ("amber", "cobalt"):
            files[f"{name}/ya.make"] = (
                "UNION()\n"
                f"SET(ORIGIN ${{CURDIR}}/incoming)\nSET(DEST {name}_installed)\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/parcel.inc)\nEND()\n"
            )
            files[f"{name}/incoming/nested/item.dat"] = name
        for targets in (("amber", "cobalt"), ("cobalt", "amber")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for name in targets:
                    self.assert_copy(graph, f"$(S)/{name}/incoming/nested/item.dat",
                                     f"$(B)/{name}/{name}_installed/nested/item.dat")

    def test_uppercase_source_and_option_values_remain_user_data(self):
        graph = lib.make({
            "parcel/ya.make": (
                "UNION()\nCOPY(UPPER_PAYLOAD FROM UPPER_SOURCE DESTINATION UPPER_DEST "
                "RESULT UPPER_RESULT OUTPUT_INCLUDES UPPER_CONTEXT)\n"
                "COPY_FILE(${BINDIR}/${UPPER_RESULT} repeated.dat)\nEND()\n"
            ),
            "UPPER_SOURCE/UPPER_PAYLOAD": "payload\n",
            "parcel/UPPER_DEST/UPPER_CONTEXT": "context\n",
        }, "parcel")
        copied = self.assert_copy(graph, "$(S)/UPPER_SOURCE/UPPER_PAYLOAD", "$(B)/parcel/UPPER_DEST/UPPER_PAYLOAD")
        repeated = lib.node_by_output(graph, "$(B)/parcel/repeated.dat")
        self.assertEqual(repeated["inputs"], ["$(B)/parcel/UPPER_DEST/UPPER_PAYLOAD", "$(S)/parcel/UPPER_DEST/UPPER_CONTEXT"])
        self.assertIn(copied["uid"], repeated["deps"])

    def test_copy_values_do_not_exempt_other_macros_from_validation(self):
        for value in ("UPPER_PAYLOAD", "UPPER_SOURCE", "UPPER_DEST", "UPPER_RESULT"):
            with self.subTest(value=value):
                result = lib.make_process({
                    "parcel/ya.make": (
                        LIBRARY + "COPY(UPPER_PAYLOAD FROM UPPER_SOURCE DESTINATION UPPER_DEST RESULT UPPER_RESULT)\n"
                        f"ALLOCATOR({value})\nEND()\n"
                    ),
                    "UPPER_SOURCE/UPPER_PAYLOAD": "payload\n",
                }, "parcel")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'macro ALLOCATOR received service-keyword "{value}"', result.stderr)

    def test_keyword_signature_rejects_missing_values_duplicates_and_array_mixing(self):
        cases = (
            ("FROM", "Missing value for scalar keyword 'FROM'."),
            ("DESTINATION", "Missing value for scalar keyword 'DESTINATION'."),
            ("RESULT", "Missing value for scalar keyword 'RESULT'."),
            ("FROM KEEP_DIR_STRUCT", "Missing value for scalar keyword 'FROM'."),
            ("FROM incoming FROM other", "Keyword 'FROM' appears more than once and is not an array."),
            ("UPPER_PAYLOAD FROM UPPER_SOURCE DESTINATION", "Missing value for scalar keyword 'DESTINATION'."),
            ("FROM UPPER_SOURCE FROM OTHER_SOURCE", "Keyword 'FROM' appears more than once and is not an array."),
            ("AUTO AUTO", "Keyword 'AUTO' appears more than once and is not an array."),
            ("OUTPUT_INCLUDES header.h AUTO item.dat", "Mixing positional arguments with named arrays"),
        )
        for arguments, diagnostic in cases:
            with self.subTest(arguments=arguments):
                result = lib.make_process({"parcel/ya.make": f"UNION()\nCOPY({arguments})\nEND()\n"}, "parcel")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(diagnostic, result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
