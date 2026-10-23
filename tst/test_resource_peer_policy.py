"""Private owner globals and native PASS_PEERS resource export boundaries."""
import unittest

import lib
from test_java_tests import test_nodes

BARE = lib.BARE_MODULE
YMAKE = {"YMAKE_PYTHON3"}
COMPILER = {"CLANG", "CLANG20"}
PAINT = COMPILER | YMAKE | {"LEAF", "PAINT"}
TESTS = ("--target-platform-tests", "--test-size", "small")


def globals_of(node):
    args = node["cmds"][0]["cmd_args"]
    return {args[i + 1].split("_RESOURCE_GLOBAL::")[0] for i, arg in enumerate(args[:-1]) if arg == "--global-resource"}


def fixture(kind="PROGRAM", policy="", after="", end=True):
    files = {
        "build/platform/python/ymake_python3/ya.make": lib.resource_lib("YMAKE_PYTHON3", 31),
        "build/platform/clang/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG sbr:32 CLANG20 sbr:33)\nEND()\n",
        "build/platform/lld/ya.make": lib.resource_lib("LLD_ROOT", 34),
        "leaf/ya.make": f"LIBRARY()\n{BARE}DECLARE_EXTERNAL_RESOURCE(LEAF sbr:35)\nSRCS(leaf.cpp)\nEND()\n",
        "leaf/leaf.cpp": "int leaf;\n",
        "child/unit.cpp": "int main(){return 0;}\n",
        "parent/ya.make": "UNION()\nSET(VALIDATE_DATA yes)\nPEERDIR(child)\nFROM_SANDBOX(FILE 72 OUT_NOAUTO hidden.bin)\nEND()\n",
    }
    body = f"{kind}()\n{BARE}SET(VALIDATE_DATA yes)\n"
    if kind == "UNION":
        files["paint/ya.make"] = f"LIBRARY()\n{BARE}DECLARE_EXTERNAL_RESOURCE(PAINT sbr:36)\nPEERDIR(leaf)\nSRCS(unit.cpp)\nEND()\n"
        files["paint/unit.cpp"] = "int paint;\n"
        body += "PEERDIR(paint)\n"
    else:
        body += "PEERDIR(leaf)\nDECLARE_EXTERNAL_RESOURCE(PAINT sbr:36)\nSRCS(unit.cpp)\nLDFLAGS(-L${PAINT_RESOURCE_GLOBAL}/lib)\n"
    body += policy + "FROM_SANDBOX(FILE 71 OUT_NOAUTO unused.bin)\n" + ("END()\n" if end else "") + after
    files["child/ya.make"] = body
    return files


class ResourcePeerPolicyTest(unittest.TestCase):
    def graph(self, files, *roots, flags=()):
        graph = lib.make(files, "parent", *roots, *TESTS, *flags)
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        return graph

    def check(self, kind, policy, exports, after="", flags=(), end=True):
        graph = self.graph(fixture(kind, policy, after, end), "child", flags=flags)
        checks = test_nodes(graph)
        private = PAINT | ({"LLD_ROOT"} if kind in ("PROGRAM", "DLL") else set())
        self.assertEqual(globals_of(checks["child/validate_resource"]), private)
        self.assertEqual(globals_of(checks["parent/validate_resource"]), private if exports else YMAKE)
        self.assertEqual(checks["child/validate_resource"]["inputs"], ["$(S)/child", "71"])
        self.assertEqual(checks["parent/validate_resource"]["inputs"], ["$(S)/parent", "72"])
        produced = any("$(B)/child/unused.bin" in n["outputs"] for n in graph["graph"])
        self.assertEqual(produced, kind == "UNION")
        for name in private:
            self.assertIsNotNone(lib.node_by_output(graph, "$(B)/resources/" + name))
        return graph

    def test_default_library_program_and_bundle_exports(self):
        for kind in ("LIBRARY", "PROGRAM", "DLL", "UNION"):
            with self.subTest(kind=kind):
                self.check(kind, "", kind not in ("PROGRAM", "DLL"))

    def test_selected_static_yql_units_keep_library_defaults_and_overrides(self):
        from test_yql import udf_fixture
        for kind in ("YQL_UDF", "YQL_UDF_CONTRIB", "YQL_UDF_YDB"):
            for value in (None, "yes", "no"):
                with self.subTest(kind=kind, value=value):
                    policy = "" if value is None else f"SET(PASS_PEERS {value})\n"
                    files = udf_fixture()
                    files.update(fixture(kind, "UDF_NO_PROBE()\n" + policy))
                    files["parent/ya.make"] = files["parent/ya.make"].replace("UNION()", "PROGRAM()\n" + BARE)
                    graph = self.graph(files, "child")
                    checks = test_nodes(graph)
                    self.assertIn("PAINT", globals_of(checks["child/validate_resource"]))
                    exported = globals_of(checks["parent/validate_resource"])
                    self.assertEqual("PAINT" in exported, value != "no")
                    self.assertEqual("LEAF" in exported, value != "no")
                    self.assertIsNotNone(lib.node_by_output(graph, "$(B)/child/libchild.global.a"))
                    self.assertIsNotNone(lib.node_by_output(graph, "$(B)/resources/PAINT"))

    def test_explicit_yes_and_no_override_each_class(self):
        for kind in ("LIBRARY", "PROGRAM", "DLL", "UNION"):
            for value in ("yes", "no"):
                with self.subTest(kind=kind, value=value):
                    self.check(kind, f"SET(PASS_PEERS {value})\n", value == "yes")

    def test_native_istrue_is_an_exact_case_insensitive_positive_set(self):
        for value in ("TRUE", "t", "Y", "On", "1", "DA", "", "false", "0", "arbitrary", " yes "):
            with self.subTest(value=value):
                self.check("LIBRARY", f'SET(PASS_PEERS "{value}")\n', value in ("TRUE", "t", "Y", "On", "1", "DA"))

    def test_bare_alias_stays_raw_while_braced_alias_expands_at_assignment(self):
        for alias, exports in (("$CHOICE", False), ("${CHOICE}", True)):
            with self.subTest(alias=alias):
                self.check("PROGRAM", f"SET(CHOICE yes)\nSET(PASS_PEERS {alias})\n", exports)

    def test_after_end_assignments_cannot_rebind_the_finished_owner(self):
        for kind in ("LIBRARY", "PROGRAM"):
            for value in ("yes", "no"):
                with self.subTest(kind=kind, value=value):
                    after = "SET(PASS_PEERS " + ("no" if value == "yes" else "yes") + ")\n"
                    self.check(kind, f"SET(PASS_PEERS {value})\n", value == "yes", after=after)

    def test_inherited_config_and_preamble_are_not_owner_overrides(self):
        for kind in ("LIBRARY", "PROGRAM"):
            for value in ("yes", "no"):
                with self.subTest(kind=kind, value=value):
                    files = fixture(kind)
                    files["child/ya.make"] = f"SET(PASS_PEERS {value})\n" + files["child/ya.make"]
                    graph = self.graph(files, flags=("-DPASS_PEERS=" + value,))
                    self.assertEqual(globals_of(test_nodes(graph)["parent/validate_resource"]),
                                     YMAKE if kind == "PROGRAM" else PAINT)

    def test_defaults_and_source_mutators_bind_only_the_owner(self):
        for body, exports in (("DEFAULT(PASS_PEERS yes)\n", True),
                              ("DEFAULT(PASS_PEERS no)\nDEFAULT(PASS_PEERS yes)\n", False),
                              ("DISABLE(PASS_PEERS)\n", False),
                              ("ENABLE(PASS_PEERS)\n", True),
                              ("SET_APPEND(PASS_PEERS yes)\n", True)):
            with self.subTest(body=body):
                self.check("PROGRAM", body, exports)
        self.check("LIBRARY", "DEFAULT(PASS_PEERS no)\n", True, flags=("-DPASS_PEERS=yes",))

    def test_implicit_eof_owner_end_retains_the_captured_policy(self):
        for value in ("yes", "no"):
            with self.subTest(value=value):
                self.check("PROGRAM", f"SET(PASS_PEERS {value})\n", value == "yes", end=False)

    def test_transitive_diamond_and_both_root_orders(self):
        for roots in (("left", "parent"), ("parent", "left")):
            with self.subTest(roots=roots):
                files = fixture("LIBRARY", "SET(PASS_PEERS no)\n")
                files["left/ya.make"] = "UNION()\nPEERDIR(child)\nEND()\n"
                files["right/ya.make"] = "UNION()\nPEERDIR(child leaf)\nEND()\n"
                files["parent/ya.make"] = files["parent/ya.make"].replace("PEERDIR(child)", "PEERDIR(left right)")
                graph = self.graph(files, *roots)
                self.assertEqual(globals_of(test_nodes(graph)["parent/validate_resource"]), YMAKE | COMPILER | {"LEAF"})

    def test_run_program_keeps_private_toolchain_and_real_producer_closure(self):
        files = fixture()
        files["parent/ya.make"] = files["parent/ya.make"].replace("END()", "RUN_PROGRAM(child OUT produced.txt)\nEND()")
        graph = self.graph(files)
        checks = test_nodes(graph)
        self.assertEqual(globals_of(checks["parent/validate_resource"]), YMAKE)
        tool = lib.node_by_output(graph, "$(B)/child/child")
        command = next(c["cmd_args"] for c in tool["cmds"] if any(a.endswith("link_exe.py") for a in c["cmd_args"]))
        self.assertIn("$(B)/resources/CLANG20/bin/clang++", command)
        self.assertIn("-L$(B)/resources/PAINT/lib", command)
        produced = lib.node_by_output(graph, "$(B)/parent/produced.txt")
        self.assertIn(tool["uid"], produced["deps"])
        self.assertIn(tool["outputs"][0], produced["inputs"])
        union = lib.node_by_output(graph, "$(B)/parent/parent.pkg.fake")
        self.assertIn(produced["uid"], union["deps"])
        self.assertIn(produced["outputs"][0], union["inputs"])

    def test_private_python_owner_keeps_import_and_style_resources(self):
        from test_python_suites import fixture as python_fixture
        py = python_fixture(macro="PY3_PROGRAM_BIN")
        files = {(k.replace("lib/", "child/", 1) if k.startswith("lib/") else k): v for k, v in py.items()}
        files.update(fixture("PY3_PROGRAM_BIN", "SET(PASS_PEERS no)\n"))
        files["child/ya.make"] = files["child/ya.make"].replace("END()", "PY_SRCS(a.py)\nEND()")
        graph = self.graph(files, "child")
        checks = test_nodes(graph)
        self.assertEqual(globals_of(checks["parent/validate_resource"]), YMAKE)
        self.assertTrue({"PAINT", "LEAF", "CLANG20", "LLD_ROOT"} <= globals_of(checks["child/import_test"]))
        self.assertEqual(globals_of(checks["child/flake8"]), {"FLAKE8_PY3"})
        binary = lib.node_by_output(graph, "$(B)/child/child")
        self.assertIn(binary["uid"], checks["child/import_test"]["deps"])
        self.assertIn("$(B)/resources/PAINT/lib", " ".join(a for c in binary["cmds"] for a in c["cmd_args"]))

    def test_java_foreign_resource_consumer_obeys_public_policy(self):
        for kind in ("PROGRAM", "LIBRARY"):
            for value in (None, "yes", "no"):
                with self.subTest(kind=kind, value=value):
                    policy = "" if value is None else f"SET(PASS_PEERS {value})\n"
                    files = lib.java_tree()
                    files.update(fixture(kind, policy))
                    files["parent/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(child)\nJAVA_SRCS(Main.java)\nEND()\n"
                    files["parent/Main.java"] = "class Main {}\n"
                    graph = self.graph(files, "child")
                    checks = test_nodes(graph)
                    exported = globals_of(checks["parent/java_style"])
                    expected = value == "yes" or value is None and kind == "LIBRARY"
                    self.assertEqual("PAINT" in exported, expected)
                    self.assertEqual("LEAF" in exported, expected)
                    self.assertTrue({"JDK17", "JDK26", "YMAKE_PYTHON3"} <= exported)
                    self.assertTrue({"PAINT", "LEAF"} <= globals_of(checks["child/validate_resource"]))
                    self.assertIsNotNone(lib.node_by_output(graph, "$(B)/resources/PAINT"))

    def test_java_packaging_consumes_selected_foreign_owner_globals(self):
        for kind in ("LIBRARY", "PROGRAM", "DLL"):
            for value in (None, "yes", "no"):
                for consume in (False, True):
                    with self.subTest(kind=kind, value=value, consume=consume):
                        policy = "" if value is None else f"SET(PASS_PEERS {value})\n"
                        files = lib.java_tree()
                        files.update(fixture(kind, policy))
                        packing = "ADD_DLLS_TO_JAR()\n" if consume else ""
                        files["parent/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\n" + packing + "PEERDIR(child)\nJAVA_SRCS(Main.java)\nEND()\n"
                        files["parent/Main.java"] = "class Main {}\n"
                        graph = self.graph(files, "child")
                        checks = test_nodes(graph)
                        observed = globals_of(checks["parent/java_style"])
                        public = value == "yes" or value is None and kind == "LIBRARY"
                        self.assertEqual("PAINT" in observed, consume or public)
                        self.assertEqual("LEAF" in observed, consume or public)
                        self.assertIn("PAINT", globals_of(checks["child/validate_resource"]))
                        producer = lib.node_by_output(graph, "$(B)/resources/PAINT")
                        self.assertIsNotNone(producer)
                        if kind == "DLL":
                            linked = lib.node_by_output(graph, "$(B)/child/libchild.so")
                            self.assertIn("-L$(B)/resources/PAINT/lib", " ".join(a for c in linked["cmds"] for a in c["cmd_args"]))
                            jar = lib.node_by_output(graph, "$(B)/parent/parent.jar")
                            linked_ids = {n["uid"] for n in graph["graph"] if "$(B)/child/libchild.so" in n["outputs"]}
                            self.assertEqual(bool(linked_ids.intersection(jar["deps"])), consume)
                            self.assertEqual("$(B)/child/libchild.so" in jar["inputs"], consume)

    def test_import_consumer_uses_own_globals_without_private_library_globals(self):
        from test_python_srcs import python_base
        files = python_base()
        files.update(fixture("LIBRARY", "SET(PASS_PEERS no)\n"))
        files["parent/ya.make"] = files["parent/ya.make"].replace("UNION()", "PY3_PROGRAM_BIN()\n" + BARE).replace(
            "END()", "DECLARE_EXTERNAL_RESOURCE(CALLER sbr:37)\nEND()")
        graph = self.graph(files, "child")
        checks = test_nodes(graph)
        own = globals_of(checks["parent/import_test"])
        self.assertIn("CALLER", own)
        self.assertTrue({"LEAF", "PAINT"}.isdisjoint(own))
        self.assertTrue({"LEAF", "PAINT"} <= globals_of(checks["child/validate_resource"]))
        self.assertIn(lib.node_by_output(graph, "$(B)/parent/parent")["uid"], checks["parent/import_test"]["deps"])


if __name__ == "__main__":
    unittest.main()
