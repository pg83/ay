import unittest

import lib
from test_link_test_nodes import library


def fixture():
    files = {"library/cpp/testing/gbenchmark/ya.make": library()}
    for name, tags in (("ordinary", ""), ("optin", "ya:not_autocheck"), ("manual", "ya:manual")):
        files[f"{name}/ya.make"] = (
            f"G_BENCHMARK()\n{lib.BARE_MODULE}SRCS(main.cpp)\nTAG({tags})\nEND()\n"
        )
        files[f"{name}/main.cpp"] = "int main() {}\n"
    return files


def suite_paths(graph):
    return sorted(n["kv"]["path"] for n in graph["graph"] if n["kv"].get("run_test_node"))


class TestTagFilterTest(unittest.TestCase):
    def test_default_filter_keeps_requested_binaries_and_skips_opt_in_suites(self):
        for flags in (("-t",), ("--target-platform-tests", "--report-skipped-suites")):
            with self.subTest(flags=flags):
                graph = lib.make(fixture(), "ordinary", "optin", "manual", *flags, "--sandboxing")
                self.assertEqual(suite_paths(graph), ["ordinary/ordinary"])
                for name in ("ordinary", "optin", "manual"):
                    lib.node_by_output(graph, f"$(B)/{name}/{name}")

    def test_all_test_levels_and_autocheck_follow_the_tag_default(self):
        for flags, expected in (
            (("-ttt",), ["optin/optin", "ordinary/ordinary"]),
            (("-ttt", "-DAUTOCHECK=yes"), ["ordinary/ordinary"]),
            (("-t", "--test-tag", "ya:not_autocheck"), ["optin/optin"]),
            (("-t", "--test-tag", "ya:manual"), ["manual/manual"]),
            (("-t", "--test-tag", "ya:anytag"), ["manual/manual", "optin/optin", "ordinary/ordinary"]),
            (("-t", "--test-tag", "ya:notags"), ["ordinary/ordinary"]),
            (("-t", "--test-tag", ",,"), ["ordinary/ordinary"]),
        ):
            with self.subTest(flags=flags):
                graph = lib.make(fixture(), "ordinary", "optin", "manual", *flags, "--sandboxing")
                self.assertEqual(suite_paths(graph), expected)

    def test_include_union_exclusion_and_environment_filters(self):
        files = {}
        for name, tags in (("first", "chosen"), ("second", "other"), ("third", "chosen blocked")):
            files[f"{name}/ya.make"] = f"EXECTEST()\nRUN(check)\nTAG({tags})\nEND()\n"
        graph = lib.make(files, "first", "second", "third", "-t", "--test-tag", "+chosen,other", "--test-tag", "-blocked", "--sandboxing")
        self.assertEqual(suite_paths(graph), ["first/exectest", "second/exectest"])
        graph = lib.make(files, "first", "second", "third", "-t", "--sandboxing", env={"YA_TEST_TAG": "other"})
        self.assertEqual(suite_paths(graph), ["second/exectest"])

    def test_conflicting_filters_are_rejected(self):
        process = lib.make_process(fixture(), "ordinary", "-t", "--test-tag", "chosen,-chosen", "--sandboxing")
        self.assertNotEqual(process.returncode, 0)
        self.assertIn("intersect", process.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
