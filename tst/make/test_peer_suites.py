import unittest

from tst import lib
from tst.make.test_go_tests import AUTOCHECK, go_module, go_world, test_paths
from tst.make.test_java_tests import style_tree, test_nodes
from tst.make.test_python2 import python2_base
from tst.make.test_python_srcs import python_base as python3_base


PEER_TESTS = ("--target-platform-flag", "ADD_PEERDIRS_GEN_TESTS=yes")
BARE = lib.BARE_MODULE


def checks(graph):
    return {node["kv"]["path"]: node for node in graph["graph"] if node["kv"].get("run_test_node")}


def native_world():
    files = {"build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n"}
    for project, peers in (("root", "left right"), ("left", "leaf"), ("right", "leaf"), ("leaf", "")):
        files[f"{project}/ya.make"] = (
            f"LIBRARY()\n{BARE}STYLE_CPP()\nPEERDIR({peers})\nSRCS(code.cpp)\nEND()\n"
        )
        files[f"{project}/code.cpp"] = "int value;\n"
    return files


class PeerSuitesTest(unittest.TestCase):
    def test_peer_traversal_is_opt_in_and_shared_peers_are_tested_once(self):
        for flags, expected in (((), {"root/clang_format"}), (PEER_TESTS, {
            "root/clang_format", "left/clang_format", "right/clang_format", "leaf/clang_format",
        }), (("-DADD_PEERDIRS_GEN_TESTS=no",), {"root/clang_format"})):
            with self.subTest(flags=flags):
                graph = lib.make(native_world(), "root", "-t", *flags)
                self.assertEqual(set(checks(graph)), expected)
                runs = [n for n in graph["graph"] if n["kv"].get("run_test_node")]
                self.assertEqual(len(runs), len(expected))
                self.assertTrue({n["uid"] for n in runs}.issubset(graph["result"]))
        self.assertEqual(checks(lib.make(native_world(), "root", *PEER_TESTS)), {})

    def test_tools_depends_and_peer_recurses_do_not_become_test_roots(self):
        files = native_world()
        lib.tool_program(files, "tool", "tool")
        files["tool/ya.make"] = files["tool/ya.make"].replace("END()", "STYLE_CPP()\nPEERDIR(host)\nEND()")
        for project in ("host", "dep", "leaf/ut"):
            files[f"{project}/ya.make"] = f"LIBRARY()\n{BARE}STYLE_CPP()\nEND()\n"
        files["leaf/ya.make"] += "RECURSE_FOR_TESTS(ut)\n"
        files["root/ya.make"] = files["root/ya.make"].replace("END()", "RUN_PROGRAM(tool OUT generated.cpp)\nEND()")
        files["tests/ya.make"] = "EXECTEST()\nRUN(dep)\nDEPENDS(dep)\nEND()\n"
        graph = lib.make(files, "root", "tests", "-t", *PEER_TESTS,
                         "--host-platform-flag", "ADD_PEERDIRS_GEN_TESTS=yes")
        self.assertEqual(set(checks(graph)), {
            "root/clang_format", "left/clang_format", "right/clang_format", "leaf/clang_format", "tests/exectest",
        })

    def test_join_sources_are_linted_in_metadata_only_peers(self):
        files = native_world()
        files["leaf/ya.make"] = (
            f"LIBRARY()\n{BARE}STYLE_CPP()\nSRCDIR(shared)\n"
            "COPY_FILE(code.cpp generated.cpp)\nJOIN_SRCS(joined.cpp code.cpp other.cpp generated.cpp)\nEND()\n"
        )
        files["shared/other.cpp"] = "int other;\n"
        graph = lib.make(files, "root", "-t", *PEER_TESTS)
        run = checks(graph)["leaf/clang_format"]
        self.assertIn("$(S)/leaf/code.cpp", run["inputs"])
        self.assertIn("$(S)/shared/other.cpp", run["inputs"])
        self.assertFalse(any(path.endswith(("generated.cpp", "joined.cpp")) for path in run["inputs"]))

    def test_target_python2_peer_keeps_only_its_selected_language(self):
        files = python2_base()
        files.update({
            "root/ya.make": f"LIBRARY()\n{BARE}USE_PYTHON2()\nPEERDIR(py)\nEND()\n",
            "py/ya.make": f"PY23_LIBRARY()\n{BARE}NO_PYTHON_INCLUDES()\nPY_SRCS(module.py)\nEND()\n",
            "py/module.py": "",
        })
        graph = lib.make(files, "root", "-t", *PEER_TESTS)
        self.assertIn("py/py2_flake8", checks(graph))
        self.assertNotIn("py/flake8", checks(graph))
        run = checks(graph)["py/py2_flake8"]
        self.assertIn("$(S)/py/module.py", run["inputs"])
        self.assertIn(run["uid"], graph["result"])

    def test_transitive_variants_merge_validation_before_emission(self):
        files = python3_base()
        files.update(python2_base())
        files.update({
            "py/ya.make": (
                f"PY23_LIBRARY()\n{BARE}NO_PYTHON_INCLUDES()\nPY_SRCS(module.py)\n"
                "SET(VALIDATE_DATA yes)\nIF(PYTHON2)\n"
                "FROM_SANDBOX(FILE 20 OUT_NOAUTO resource.bin)\nELSE()\n"
                "FROM_SANDBOX(FILE 10 OUT_NOAUTO resource.bin)\nENDIF()\nEND()\n"
            ),
            "py/module.py": "",
            "root2/ya.make": f"PROGRAM()\n{BARE}USE_PYTHON2()\nPEERDIR(py)\nEND()\n",
            "root3/ya.make": f"PROGRAM()\n{BARE}PEERDIR(py)\nEND()\n",
        })
        for targets in (("root2", "root3"), ("root3", "root2")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, "-t", *PEER_TESTS)
                runs = [node for node in graph["graph"]
                        if node["kv"].get("path") == "py/validate_resource"]
                self.assertEqual(len(runs), 1)
                self.assertEqual(runs[0]["inputs"], ["$(S)/py", "10", "20"])
                self.assertIn("py/flake8", checks(graph))
                self.assertIn("py/py2_flake8", checks(graph))

    def test_script_peers_are_traversed_and_validation_remains_per_module(self):
        files = native_world()
        files["parent/ya.make"] = "UNION()\nPEERDIR(root)\nEND()\n"
        files["outer/ya.make"] = "GEN_LIBRARY()\nPEERDIR(parent)\nEND()\n"
        graph = lib.make(files, "outer", "-t", *PEER_TESTS)
        self.assertEqual(set(checks(graph)), {
            "root/clang_format", "left/clang_format", "right/clang_format", "leaf/clang_format",
        })

    def test_go_peers_keep_their_own_vet_producer(self):
        files = go_world()
        go_module(files, "library/go/leaf")
        go_module(files, "library/go/root", module="GO_PROGRAM", macros="PEERDIR(library/go/leaf)\n")
        graph = lib.make(files, "library/go/root", *AUTOCHECK, *PEER_TESTS)
        paths = test_paths(graph)
        for project in ("library/go/root", "library/go/leaf", "library/go/core/buildinfo"):
            self.assertIn(project + "/gofmt", paths)
            self.assertIn(project + "/govet", paths)
            suffix = "" if project == "library/go/root" else ".a"
            package = lib.node_by_output(graph, f"$(B)/{project}/{project.rsplit('/', 1)[-1]}{suffix}")
            vet = checks(graph)[project + "/govet"]
            self.assertIn(package["uid"], vet["deps"])
        self.assertFalse(any(path.startswith("contrib/") for path in paths))

    def test_docs_find_target_font_validation_without_testing_host_tool(self):
        files = {
            "guide/ya.make": "DOCS()\nUSE_PLANTUML()\nEND()\n",
            "contrib/java/openjdk-fontconfig/ya.make": (
                "UNION()\nSET(VALIDATE_DATA yes)\nFROM_SANDBOX(789 OUT font.ttf)\nEND()\n"
            ),
        }
        graph = lib.make(files, "guide", "-t", *PEER_TESTS)
        self.assertEqual(set(checks(graph)), {"contrib/java/openjdk-fontconfig/validate_resource"})
        self.assertEqual(checks(graph)["contrib/java/openjdk-fontconfig/validate_resource"]["inputs"], [
            "$(S)/contrib/java/openjdk-fontconfig", "789",
        ])

    def test_java_peers_include_style_suites(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(peer)\nEND()\n",
            "peer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(Main.java)\nEND()\n",
            "peer/Main.java": "class Main {}\n",
        })
        graph = lib.make(files, "app", "-t", *PEER_TESTS)
        self.assertIn("peer/java_style", test_nodes(graph))

    def test_java_discovery_uses_selected_managed_and_native_peers(self):
        from tst.make.test_java import proxy

        base = "contrib/java/example/lib"
        files = style_tree(**{
            "app/ya.make": (
                f"JAVA_LIBRARY_NON_SPLIT()\nPEERDIR({base} native)\n"
                f"DEPENDENCY_MANAGEMENT({base}/2)\nEND()\n"
            ),
            **proxy(base, base + "/1"),
            "native/ya.make": "UNION()\nSET(VALIDATE_DATA yes)\nFROM_SANDBOX(FILE 3 OUT_NOAUTO data)\nEND()\n",
        })
        for version in ("1", "2"):
            files[f"{base}/{version}/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(Main.java)\nEND()\n"
            files[f"{base}/{version}/Main.java"] = "class Main {}\n"
        graph = lib.make(files, "app", "-t", *PEER_TESTS)
        paths = set(checks(graph))
        self.assertIn(base + "/2/java_style", paths)
        self.assertNotIn(base + "/1/java_style", paths)
        self.assertIn("native/validate_resource", paths)


if __name__ == "__main__":
    unittest.main(verbosity=2)
