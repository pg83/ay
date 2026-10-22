import unittest

import lib


MODULE = "orchard/bundle"
LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class CopyOutputRootsTest(unittest.TestCase):
    def assert_copy(self, graph, source, output):
        node = lib.node_by_output(graph, output)
        self.assertEqual(node["kv"], {"p": "CP", "pc": "light-cyan"})
        self.assertEqual(node["cmds"][0]["cmd_args"][-2:], [source, output])
        self.assertEqual(node["inputs"], [source])
        return node

    def test_all_copy_forms_resolve_output_roots_before_registration(self):
        # FormatBuildPath converts known roots to Build, joins unresolved
        # names to the build directory, then reconstructs dot segments.
        destinations = (
            ("staged/./old/../", MODULE + "/staged"),
            ("../handover", "orchard/handover"),
            (MODULE + "/staged", MODULE + "/" + MODULE + "/staged"),
            ("${ARCADIA_ROOT}/exports/./old/..", "exports"),
            ("${ARCADIA_ROOT}/" + MODULE, MODULE),
            ("${CURDIR}/staged/../delivered", MODULE + "/delivered"),
            ("${ARCADIA_BUILD_ROOT}/exports/./old/..", "exports"),
            ("${BINDIR}/staged/../delivered", MODULE + "/delivered"),
            ("$(S)/exports/./old/..", "exports"),
            ("$(B)/exports/./old/..", "exports"),
        )
        for macro in ("COPY", "COPY_FILE", "COPY_FILE_WITH_CONTEXT"):
            for destination, expected in destinations:
                with self.subTest(macro=macro, destination=destination):
                    if macro == "COPY":
                        call = f'COPY(payload.dat FROM incoming DESTINATION "{destination}")'
                    else:
                        call = f'{macro}(incoming/payload.dat "{destination}/payload.dat")'
                    graph = lib.make({
                        MODULE + "/ya.make": "UNION()\n" + call + "\nEND()\n",
                        MODULE + "/incoming/payload.dat": "payload\n",
                    }, MODULE)
                    self.assert_copy(graph, "$(S)/" + MODULE + "/incoming/payload.dat",
                                     "$(B)/" + expected + "/payload.dat")

    def test_source_inputs_keep_their_namespace_and_build_inputs_keep_producers(self):
        graph = lib.make({
            MODULE + "/ya.make": (
                "UNION()\n"
                "COPY_FILE(seed.dat ${CURDIR}/generated/item.dat)\n"
                "COPY(item.dat FROM ${BINDIR}/generated DESTINATION ${ARCADIA_ROOT}/exports)\n"
                'COPY_FILE(${ARCADIA_BUILD_ROOT}/exports/item.dat "$(S)/exports/final.dat")\n'
                "COPY_FILE(${ARCADIA_ROOT}/exports/item.dat ${CURDIR}/source.dat)\n"
                "COPY_FILE(${CURDIR}/generated/item.dat ${ARCADIA_ROOT}/exports/local.dat)\n"
                'COPY_FILE("$(S)/exports/item.dat" ${BINDIR}/internal.dat)\nEND()\n'
            ),
            MODULE + "/seed.dat": "seed\n",
            MODULE + "/generated/item.dat": "physical source\n",
            "exports/item.dat": "physical source\n",
        }, MODULE)
        first = self.assert_copy(graph, "$(S)/" + MODULE + "/seed.dat",
                                 "$(B)/" + MODULE + "/generated/item.dat")
        second = self.assert_copy(graph, first["outputs"][0], "$(B)/exports/item.dat")
        third = self.assert_copy(graph, second["outputs"][0], "$(B)/exports/final.dat")
        self.assertIn(first["uid"], second["deps"])
        self.assertIn(second["uid"], third["deps"])
        for source, output in (
            ("exports/item.dat", MODULE + "/source.dat"),
            (MODULE + "/generated/item.dat", "exports/local.dat"),
            ("exports/item.dat", MODULE + "/internal.dat"),
        ):
            copied = self.assert_copy(graph, "$(S)/" + source, "$(B)/" + output)
            self.assertNotIn(first["uid"], copied["deps"])
            self.assertNotIn(second["uid"], copied["deps"])

    def test_auto_noauto_and_context_survive_source_root_output_conversion(self):
        graph = lib.make({
            MODULE + "/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/" + MODULE + ")\n"
                "COPY_FILE(AUTO incoming/entry.cpp ${ARCADIA_ROOT}/" + MODULE + "/plain/entry.cpp)\n"
                "COPY_FILE_WITH_CONTEXT(AUTO incoming/entry.cpp ${CURDIR}/contextual/entry.cpp)\n"
                'COPY(entry.cpp FROM ${ARCADIA_ROOT}/' + MODULE + '/incoming DESTINATION "$(S)/' + MODULE + '/macro_context" WITH_CONTEXT AUTO)\n'
                "COPY(entry.cpp FROM incoming DESTINATION ${CURDIR}/macro_plain AUTO)\n"
                "COPY_FILE(incoming/entry.cpp ${CURDIR}/retained/entry.cpp)\n"
                "COPY_FILE(${BINDIR}/retained/entry.cpp materialized.h)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            MODULE + "/incoming/entry.cpp": '#include "detail.h"\n',
            MODULE + "/incoming/detail.h": "",
            MODULE + "/use.cpp": '#include "materialized.h"\n',
        }, MODULE)
        for directory, context in (("plain", False), ("contextual", True),
                                   ("macro_context", True), ("macro_plain", False)):
            copied = lib.node_by_output(graph, f"$(B)/{MODULE}/{directory}/entry.cpp")
            compiled = lib.node_by_output(graph, f"$(B)/{MODULE}/_/{directory}/entry.cpp.o")
            self.assertIn(copied["uid"], compiled["deps"])
            for name in ("entry.cpp", "detail.h"):
                self.assertEqual(f"$(S)/{MODULE}/incoming/{name}" in compiled["inputs"], context)
        retained = lib.node_by_output(graph, "$(B)/" + MODULE + "/retained/entry.cpp")
        materialized = lib.node_by_output(graph, "$(B)/" + MODULE + "/materialized.h")
        self.assertIn(retained["uid"], materialized["deps"])
        self.assertFalse(any(node["kv"].get("p") == "CC" and retained["outputs"][0] in node["inputs"]
                             for node in graph["graph"]))

    def test_result_keeps_lexical_path_while_registered_output_is_build_rooted(self):
        destination = "${ARCADIA_ROOT}/" + MODULE + "/staged/../delivered"
        graph = lib.make({
            MODULE + "/ya.make": (
                LIBRARY + "ADDINCL(${ARCADIA_BUILD_ROOT}/" + MODULE + ")\n"
                f"COPY(KEEP_DIR_STRUCT nested/item.dat FROM incoming DESTINATION {destination} RESULT TRANSFERRED)\n"
                "CONFIGURE_FILE(paths.h.in paths.h)\nSRCS(use.cpp)\nEND()\n"
            ),
            MODULE + "/incoming/nested/item.dat": "item\n",
            MODULE + "/paths.h.in": "// @TRANSFERRED@\n",
            MODULE + "/use.cpp": '#include "paths.h"\n#include "delivered/nested/item.dat"\n',
        }, MODULE)
        self.assert_copy(graph, "$(S)/" + MODULE + "/incoming/nested/item.dat",
                         "$(B)/" + MODULE + "/delivered/nested/item.dat")
        configured = lib.node_by_output(graph, "$(B)/" + MODULE + "/paths.h")
        self.assertIn("TRANSFERRED=$(S)/" + MODULE + "/staged/../delivered/nested/item.dat",
                      configured["cmds"][0]["cmd_args"])

    def test_cached_lexical_copy_expands_roots_in_each_module(self):
        files = {
            "shared/transfer.inc": (
                "COPY(KEEP_DIR_STRUCT nested/item.dat FROM ${CURDIR}/incoming DESTINATION ${CURDIR}/installed)\n"
                "COPY_FILE(${BINDIR}/installed/nested/item.dat ${ARCADIA_ROOT}/${MODDIR}/final.dat)\n"
            ),
        }
        for name in ("copper", "indigo"):
            files[f"grove/{name}/ya.make"] = (
                "UNION()\nINCLUDE(${ARCADIA_ROOT}/shared/transfer.inc)\nEND()\n"
            )
            files[f"grove/{name}/incoming/nested/item.dat"] = name
        for targets in (("grove/copper", "grove/indigo"), ("grove/indigo", "grove/copper")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for module in targets:
                    first = self.assert_copy(graph, f"$(S)/{module}/incoming/nested/item.dat",
                                             f"$(B)/{module}/installed/nested/item.dat")
                    second = self.assert_copy(graph, first["outputs"][0], f"$(B)/{module}/final.dat")
                    self.assertIn(first["uid"], second["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
