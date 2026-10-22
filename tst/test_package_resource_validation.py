import unittest

import lib
from test_peer_suites import PEER_TESTS, checks


class PackageResourceValidationTest(unittest.TestCase):
    def make(self, files, *targets, extra=(), opensource=False):
        return lib.make(files, *targets, "--target-platform-tests", *PEER_TESTS,
                        *extra, opensource=opensource)

    def test_package_peer_validation_survives_stripped_unused_artifacts(self):
        files = {
            "grove/ya.make": "PACKAGE()\nPEERDIR(parcel)\nEND()\n",
            "parcel/ya.make": (
                "UNION()\nFROM_SANDBOX(845 RENAME upstream.bin OUT payload.bin "
                "AUTOUPDATED payload.bin)\nEND()\n"
            ),
        }
        for stripped in (False, True):
            with self.subTest(stripped=stripped):
                extra = ("--output-only-tests", "--strip-packages-from-results",
                         "--add-modules-to-results") if stripped else ()
                graph = self.make(files, "grove", extra=extra)
                self.assertEqual(set(checks(graph)), {"parcel/validate_resource"})
                suite = checks(graph)["parcel/validate_resource"]
                self.assertEqual(suite["inputs"], ["$(S)/parcel", "845"])
                self.assertIn(suite["uid"], graph["result"])
                self.assertEqual(suite["cmds"][-1]["cmd_args"][-3:],
                                 ["--batch", "--batch-name", "parcel"])
                deps = [n["outputs"][0] for n in graph["graph"] if n["uid"] in suite["deps"]]
                self.assertEqual(deps, ["$(B)/common_test.context"])
                if stripped:
                    self.assertFalse(any(n["kv"].get("p") in ("SB", "UN", "PK")
                                         for n in graph["graph"]))
                else:
                    producer = lib.node_by_output(graph, "$(B)/parcel/payload.bin")
                    union = lib.node_by_output(graph, "$(B)/parcel/parcel.pkg.fake")
                    package = lib.node_by_output(graph, "$(B)/grove/grove.final.pkg.fake")
                    self.assertIn(producer["uid"], union["deps"])
                    self.assertIn(producer["outputs"][0], union["inputs"])
                    self.assertIn(union["uid"], package["deps"])
                    self.assertIn(union["outputs"][0], package["inputs"])

    def test_self_peer_and_final_only_peer_each_bind_their_selected_checks(self):
        for declaration in (
            'IF(MODULE_TAG == "PACKAGE_UNION")\nPEERDIR(parcel)\nENDIF()\n',
            'ONLY_TAGS(PACKAGE_FINAL)\nPEERDIR(parcel)\n',
        ):
            with self.subTest(declaration=declaration):
                files = {
                    "grove/ya.make": "PACKAGE()\n" + declaration + "END()\n",
                    "parcel/ya.make": "UNION()\nFROM_SANDBOX(FILE 846 OUT_NOAUTO unused.bin)\nEND()\n",
                }
                graph = self.make(files, "grove", extra=("--output-only-tests",
                                                         "--strip-packages-from-results"))
                self.assertEqual(set(checks(graph)), {"parcel/validate_resource"})
                self.assertEqual(checks(graph)["parcel/validate_resource"]["inputs"],
                                 ["$(S)/parcel", "846"])
                self.assertFalse(any(n["kv"].get("p") == "SB" for n in graph["graph"]))

    def test_nested_shared_packages_emit_one_suite_per_project(self):
        files = {
            "grove/ya.make": "PACKAGE()\nPEERDIR(inner parcel)\nEND()\n",
            "inner/ya.make": "PACKAGE()\nPEERDIR(parcel)\nEND()\n",
            "parcel/ya.make": "UNION()\nFROM_SANDBOX(FILE 847 OUT_NOAUTO unused.bin)\nEND()\n",
        }
        for targets in (("grove", "inner"), ("inner", "grove")):
            with self.subTest(targets=targets):
                graph = self.make(files, *targets, extra=("--output-only-tests",))
                suites = [n for n in graph["graph"] if n["kv"].get("run_test_node")]
                self.assertEqual(len(suites), 1)
                self.assertEqual(suites[0]["inputs"], ["$(S)/parcel", "847"])
                self.assertIn(suites[0]["uid"], graph["result"])

    def test_native_peer_unused_producer_is_independent_of_validation(self):
        files = {
            "grove/ya.make": "PACKAGE()\nPEERDIR(native)\nEND()\n",
            "native/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "FROM_SANDBOX(FILE 848 OUT_NOAUTO unused.bin)\nSRCS(value.cpp)\nEND()\n"
            ),
            "native/value.cpp": "int value;\n",
        }
        graph = self.make(files, "grove")
        self.assertEqual(set(checks(graph)), {"native/validate_resource"})
        suite = checks(graph)["native/validate_resource"]
        self.assertEqual(suite["inputs"], ["$(S)/native", "848"])
        archive = lib.node_by_output(graph, "$(B)/native/libnative.a")
        package = lib.node_by_output(graph, "$(B)/grove/grove.final.pkg.fake")
        self.assertIn(archive["uid"], package["deps"])
        self.assertNotIn(archive["uid"], suite["deps"])
        self.assertFalse(any("$(B)/native/unused.bin" in n["outputs"] for n in graph["graph"]))

    def test_validation_and_analysis_guards_keep_their_existing_semantics(self):
        files = {
            "grove/ya.make": "PACKAGE()\nPEERDIR(parcel)\nEND()\n",
            "parcel/ya.make": "UNION()\nFROM_SANDBOX(FILE 849 OUT_NOAUTO unused.bin)\nEND()\n",
        }
        for extra, opensource in (
            (("-DVALIDATE_DATA=no",), False),
            (("-DCPP_ANALYSIS_MODE=yes",), False),
            ((), True),
        ):
            with self.subTest(extra=extra, opensource=opensource):
                self.assertEqual(checks(self.make(files, "grove", extra=extra,
                                                  opensource=opensource)), {})
        self.assertEqual(checks(lib.make(files, "grove", *PEER_TESTS, opensource=False)), {})
        self.assertEqual(checks(lib.make(files, "grove", "--target-platform-tests",
                                        opensource=False)), {})
        self.assertEqual(checks(lib.make(files, "grove", "--target-platform-tests",
                                        "-DADD_PEERDIRS_GEN_TESTS=no", opensource=False)), {})
        direct = lib.make(files, "parcel", "--target-platform-tests", opensource=False)
        self.assertEqual(set(checks(direct)), {"parcel/validate_resource"})

    def test_shared_lexical_include_retains_project_and_variant_values(self):
        files = {
            "shared/resources.inc": "FROM_SANDBOX(FILE ${RESOURCE_ID} OUT_NOAUTO unused.bin)\n",
            "grove/ya.make": (
                'PACKAGE()\nIF(MODULE_TAG == "PACKAGE_FINAL")\nSET(RESOURCE_ID 851)\n'
                'ELSE()\nSET(RESOURCE_ID 850)\nENDIF()\n'
                'INCLUDE(${ARCADIA_ROOT}/shared/resources.inc)\nPEERDIR(left right)\nEND()\n'
            ),
        }
        for project, resource_id, guard in (("left", "852", ""),
                                            ("right", "853", "SET(VALIDATE_DATA no)\n")):
            files[f"{project}/ya.make"] = (
                f"UNION()\nSET(RESOURCE_ID {resource_id})\n{guard}"
                "INCLUDE(${ARCADIA_ROOT}/shared/resources.inc)\nEND()\n"
            )
        graph = self.make(files, "grove", extra=("--output-only-tests",))
        suites = checks(graph)
        self.assertEqual(set(suites), {"grove/validate_resource", "left/validate_resource"})
        self.assertEqual(suites["grove/validate_resource"]["inputs"], ["$(S)/grove", "850", "851"])
        self.assertEqual(suites["left/validate_resource"]["inputs"], ["$(S)/left", "852"])
        self.assertEqual(sum(n["kv"].get("path") == "grove/validate_resource"
                             for n in graph["graph"]), 1)

    def test_depends_only_package_does_not_become_a_test_root(self):
        files = {
            "runner/ya.make": "EXECTEST()\nDEPENDS(grove)\nRUN(true)\nEND()\n",
            "grove/ya.make": "PACKAGE()\nPEERDIR(parcel)\nEND()\n",
            "parcel/ya.make": "UNION()\nFROM_SANDBOX(FILE 854 OUT payload.bin)\nEND()\n",
        }
        graph = self.make(files, "runner", extra=("--output-only-tests",))
        self.assertEqual(set(checks(graph)), {"runner/exectest"})
        suite = checks(graph)["runner/exectest"]
        package = lib.node_by_output(graph, "$(B)/grove/grove.final.pkg.fake")
        self.assertIn(package["uid"], suite["deps"])
        producer = lib.node_by_output(graph, "$(B)/parcel/payload.bin")
        union = lib.node_by_output(graph, "$(B)/parcel/parcel.pkg.fake")
        self.assertIn(producer["uid"], union["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
