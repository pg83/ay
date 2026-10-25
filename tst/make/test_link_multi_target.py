import collections
import unittest

from tst import lib


def project():
    """Two programs and a library with a generated header, all sharing it."""
    files = {}
    files["shared/ya.make"] = (
        f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(${{ARCADIA_BUILD_ROOT}}/shared)\n"
        "RUN_PROGRAM(tools/gen OUT shared_gen.h)\n"
        "SRCS(shared.cpp)\nEND()\n"
    )
    files["shared/shared.cpp"] = '#include "shared_gen.h"\nint shared(){return 0;}\n'
    for name in ("one", "two"):
        files[f"{name}/ya.make"] = (
            f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(shared)\n"
            "SRCS(main.cpp)\nEND()\n"
        )
        files[f"{name}/main.cpp"] = '#include "shared/shared_gen.h"\nint main(){return 0;}\n'
    lib.tool_program(files, "tools/gen", "gen")
    return files


def outputs(graph):
    return collections.Counter(o for node in graph["graph"] for o in node["outputs"])


def result_outputs(graph):
    by_uid = {node["uid"]: node for node in graph["graph"]}
    return {o for uid in graph["result"] for o in by_uid[uid]["outputs"]}


class MultiTargetGraphTest(unittest.TestCase):
    def test_targets_share_one_graph_and_emit_shared_modules_once(self):
        graph = lib.make(project(), "two", "one")
        counts = outputs(graph)
        self.assertEqual([], [o for o, n in counts.items() if n > 1])
        self.assertEqual(1, counts["$(B)/shared/shared_gen.h"])
        self.assertEqual(1, counts["$(B)/shared/libshared.a"])
        self.assertTrue({"$(B)/one/one", "$(B)/two/two"} <= result_outputs(graph))

    def test_library_target_before_its_linking_consumer(self):
        # The library alone reaches its own peers at a lower demand than the
        # program that links it; the shared module must not be emitted twice.
        graph = lib.make(project(), "two", "shared")
        counts = outputs(graph)
        self.assertEqual([], [o for o, n in counts.items() if n > 1])
        self.assertIn("$(B)/shared/libshared.a", result_outputs(graph))
        self.assertIn("$(B)/two/two", result_outputs(graph))

    def test_recursed_libraries_wait_for_every_linking_target(self):
        # In test mode the RECURSE closure of a target is generated too; a library there
        # reaches its peers at a lower demand, and a later linking target would emit them
        # a second time (a second producer of the joined source).
        files = {
            "joined/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}JOIN_SRCS(all.cpp a.cpp b.cpp)\nEND()\n",
            "joined/a.cpp": "int a;\n",
            "joined/b.cpp": "int b;\n",
            "one/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\nRECURSE(lib)\n",
            "one/main.cpp": "int main(){return 0;}\n",
            "one/lib/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(joined)\nEND()\n",
            "two/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(joined)\nSRCS(main.cpp)\nEND()\n",
            "two/main.cpp": "int main(){return 0;}\n",
        }
        counts = outputs(lib.make(files, "two", "one", "-t"))
        self.assertEqual(1, counts["$(B)/joined/all.cpp"])
        self.assertEqual(1, counts["$(B)/joined/libjoined.a"])

    def test_library_target_peered_by_an_earlier_library_target(self):
        # liba reaches libb as a peer before libb's own turn as a target; libb is built
        # either way, so the peer takes the target's demand and is emitted once.
        files = {
            "libb/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}JOIN_SRCS(all.cpp a.cpp b.cpp)\nEND()\n",
            "libb/a.cpp": "int a;\n",
            "libb/b.cpp": "int b;\n",
            "liba/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(libb)\nSRCS(x.cpp)\nEND()\n",
            "liba/x.cpp": "int x;\n",
        }
        for order in (("libb", "liba"), ("liba", "libb")):
            graph = lib.make(files, *order)
            counts = outputs(graph)
            self.assertEqual(1, counts["$(B)/libb/all.cpp"])
            self.assertEqual(1, counts["$(B)/libb/liblibb.a"])
            self.assertTrue({"$(B)/liba/libliba.a", "$(B)/libb/liblibb.a"} <= result_outputs(graph))

    def test_target_option_lists_targets(self):
        positional = lib.make(project(), "two", "one", "shared")
        option = lib.make(project(), "two", "-C", "one;shared")
        self.assertEqual(positional, option)

    def test_accepts_autocheck_command_line(self):
        graph = lib.make(
            project(), "one",
            "-xx", "--dist", "--ignore-nodes-exit-code", "--warning-mode", "dirloops;ChkPeers",
            "--cache-tests", "--output-only-tests", "--cache-namespace", "AC",
            "--target-platform-build-type", "relwithdebinfo", "--target-platform-tests",
            "--target-platform-flag", "AUTOCHECK=yes", "--test-size", "small", "--test-size", "medium",
            "--report-skipped-suites", "--tests-retries", "1", "--build", "release",
            "--backup-test-results", "--add-modules-to-results", "--do-not-output-stderrs",
            "--no-src-links", "--add-changed-ok-configures", "--no-yt-store",
        )
        self.assertIn("$(B)/one/one", result_outputs(graph))


if __name__ == "__main__":
    unittest.main()
