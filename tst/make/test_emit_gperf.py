import unittest

from tst import lib


class EmitGperfTest(unittest.TestCase):
    def test_gperf_rejects_a_tool_without_linkable_output(self):
        files = {
            "gpmod/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(tags.gperf)\nEND()\n",
            "gpmod/tags.gperf": "",
            "contrib/tools/gperf/ya.make": (
                "LIBRARY()\nVERSION(1.0)\n" + lib.BARE_MODULE + "END()\n"
            ),
        }
        result = lib.make_process(files, "gpmod", "--sandboxing")
        self.assertEqual(result.returncode, 1)
        self.assertIn("tool contrib/tools/gperf has no linkable output", result.stderr)

    def test_gperf_generates_compiles_and_enters_archive(self):
        files = {
            "gpmod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(tags.gperf)\nEND()\n"
            ),
            "gpmod/tags.gperf": '%{\n#include "tag.h"\n%}\n%%\n',
            "gpmod/tag.h": "#pragma once\n",
        }
        lib.tool_program(files, "contrib/tools/gperf", "gperf")
        graph = lib.make(files, "gpmod")
        generated = lib.node_by_output(graph, "$(B)/gpmod/tags.gperf.cpp")
        self.assertEqual(generated["kv"], {"p": "GP", "pc": "yellow"})
        gperf = "$(B)/contrib/tools/gperf/gperf"
        self.assertEqual(generated["cmds"][0]["cmd_args"], [
            gperf,
            "-CtTLANSI-C",
            "-Dk*",
            "-c",
            "-Nin_tags_set",
            "$(S)/gpmod/tags.gperf",
        ])
        self.assertEqual(
            generated["cmds"][0]["stdout"],
            "$(B)/gpmod/tags.gperf.cpp",
        )
        # The includes of the .gperf are induced deps of its output: the yabs
        # reference lists only gperf and the .gperf for library/cpp/html
        # tag.gperf.cpp and tags.gperf.cpp, and their compiles take the
        # includes but never the .gperf.
        self.assertEqual(sorted(generated["inputs"]), [gperf, "$(S)/gpmod/tags.gperf"])
        tool = lib.node_by_output(graph, gperf)
        self.assertIn(tool["uid"], generated["deps"])

        compile_node = lib.node_by_output(graph, "$(B)/gpmod/tags.gperf.cpp.o")
        self.assertIn("$(B)/gpmod/tags.gperf.cpp", compile_node["inputs"])
        self.assertIn("$(S)/gpmod/tag.h", compile_node["inputs"])
        self.assertNotIn("$(S)/gpmod/tags.gperf", compile_node["inputs"])
        self.assertIn(generated["uid"], compile_node["deps"])
        archive = lib.node_by_output_prefix(graph, "$(B)/gpmod/lib")
        self.assertIn("$(B)/gpmod/tags.gperf.cpp.o", archive["inputs"])

    def test_ordinary_cpp_emits_no_gperf_node(self):
        files = {
            "plain/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(a.cpp)\nEND()\n"
            ),
            "plain/a.cpp": "int a(){return 0;}\n",
        }
        graph = lib.make(files, "plain")
        self.assertFalse(any(
            node["kv"]["p"] == "GP" for node in graph["graph"]
        ))
        compile_node = lib.node_by_output(graph, "$(B)/plain/a.cpp.o")
        self.assertEqual(compile_node["kv"]["p"], "CC")
        archive = lib.node_by_output_prefix(graph, "$(B)/plain/lib")
        self.assertIn("$(B)/plain/a.cpp.o", archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
