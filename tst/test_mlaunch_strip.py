import unittest

import lib
from test_link_ld_flags import command_with
from test_mlaunch import files


CASES = {
    "default": ("", "", True),
    "header_overrides": ("DISABLE(STRIP)\n", "", True),
    "body_disables": ("", "DISABLE(STRIP)\n", True),
    "body_reenables": ("", "DISABLE(STRIP)\nENABLE(STRIP)\n", True),
    "no_strip": ("", "ENABLE(NO_STRIP)\n", False),
    "clear_no_strip": ("ENABLE(NO_STRIP)\n", "DISABLE(NO_STRIP)\n", True),
    "platform_empty": ("SET(SDC_PLATFORM)\n", "", True),
    "platform_no": ("SET(SDC_PLATFORM no)\n", "", False),
    "platform_yes": ("SET(SDC_PLATFORM yes)\n", "", False),
    "platform_body": ("", "SET(SDC_PLATFORM yes)\n", False),
    "platform_explicit_strip": ("SET(SDC_PLATFORM yes)\n", "STRIP()\n", True),
    "included_disable": ("", "INCLUDE(disable.inc)\n", True),
}


def fixture(cases=CASES):
    sources = files()
    roots = []
    for name, (before, body, _) in cases.items():
        root = "sprout/" + name
        roots.append(root)
        sources[root + "/ya.make"] = (
            before + "MLAUNCH_PIPELINE(seed)\n" + lib.BARE_MODULE
            + "ALLOCATOR(FAKE)\nONLY_TAGS(PIPELINE)\n" + body + "END()\n"
        )
        sources[root + "/disable.inc"] = "DISABLE(STRIP)\n"
    return sources, roots


def stripped(graph, root):
    return "-Wl,--strip-all" in command_with(lib.node_by_output(graph, "$(B)/" + root + "/seed"), "link_exe.py")


class MlaunchStripTest(unittest.TestCase):
    def graph(self, cases, *flags):
        sources, roots = fixture(cases)
        graph = lib.make(sources, roots[-1], *roots[:-1], *flags,
                         "--target-platform", "default-linux-x86_64", "--sandboxing")
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        for root in roots:
            node = lib.node_by_output(graph, "$(B)/" + root + "/seed")
            self.assertIn("$(B)/" + root + "/seed.sdcmeta", node["outputs"])
        return graph

    def test_pipeline_header_and_body_scalars(self):
        graph = self.graph(CASES, "-r")
        for name, (_, _, expected) in CASES.items():
            with self.subTest(case=name):
                self.assertEqual(stripped(graph, "sprout/" + name), expected)

    def test_delayed_helper_uses_final_scalars(self):
        cases = {name: CASES[name] for name in ("default", "body_disables", "clear_no_strip")}
        for flags, enabled in ((("-r", "-DSTRIP=no"), {"default", "body_disables", "clear_no_strip"}),
                               (("-r", "-DNO_STRIP=yes"), {"clear_no_strip"}),
                               (("-d",), {"default", "body_disables", "clear_no_strip"})):
            graph = self.graph(cases, *flags)
            for name in cases:
                with self.subTest(flags=flags, case=name):
                    self.assertEqual(stripped(graph, "sprout/" + name), name in enabled)

    def test_go_sibling_keeps_ordinary_go_strip_mode(self):
        sources = files()
        sources["sprout/sibling/ya.make"] = "MLAUNCH_PIPELINE(seed)\n" + lib.BARE_MODULE + "ONLY_TAGS(GOLAUNCH)\nEND()\n"
        graph = lib.make(sources, "sprout/sibling", "-r", "--sandboxing")
        node = lib.node_by_output(graph, "$(B)/sprout/sibling/seed.golaunch")
        self.assertNotIn("$(B)/sprout/sibling/seed", node["outputs"])
        self.assertTrue(all("-Wl,--strip-all" not in c["cmd_args"] for c in node["cmds"]))


if __name__ == "__main__":
    unittest.main()
