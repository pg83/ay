import unittest

import lib


class EmitJoinSourcesTest(unittest.TestCase):
    def test_join_sources_emits_generator_compilers_archive_and_edge(self):
        files = {
            "joinmod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)\n"
                "SRCS(other.cpp)\nEND()\n"
            ),
            "joinmod/src1.cpp": "int one(){return 1;}\n",
            "joinmod/src2.cpp": "int two(){return 2;}\n",
            "joinmod/other.cpp": "int other(){return 3;}\n",
        }
        graph = lib.make(files, "joinmod")
        counts = {}
        for node in graph["graph"]:
            kind = node["kv"]["p"]
            counts[kind] = counts.get(kind, 0) + 1
        self.assertEqual(counts.get("JS"), 1)
        self.assertEqual(counts.get("CC"), 2)
        self.assertEqual(counts.get("AR"), 1)

        joined = lib.node_by_output(graph, "$(B)/joinmod/all_my.cpp")
        compile_node = lib.node_by_output(graph, "$(B)/joinmod/all_my.cpp.o")
        other = lib.node_by_output(graph, "$(B)/joinmod/other.cpp.o")
        archive = lib.node_by_output_prefix(graph, "$(B)/joinmod/lib")
        self.assertIn("$(B)/joinmod/all_my.cpp", compile_node["inputs"])
        self.assertIn(joined["uid"], compile_node["deps"])
        self.assertIn("$(S)/joinmod/other.cpp", other["inputs"])
        self.assertIn("$(B)/joinmod/all_my.cpp.o", archive["inputs"])
        self.assertIn("$(B)/joinmod/other.cpp.o", archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
