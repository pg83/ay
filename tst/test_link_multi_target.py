import collections
import unittest

import lib


def project():
    """Two programs and a library with a generated header, all sharing it."""
    files = {}
    files["shared/ya.make"] = (
        f"LIBRARY()\n{lib.BARE_MODULE}"
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
