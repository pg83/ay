"""A copied generated source can end at a physical PB leaf in queue traversal."""
import unittest

from tst import lib
from tst.make.test_proto_cpp import proto_files


B = "$(B)/quarry/"
S = "$(S)/quarry/"


def fixture(generated=False, after=False):
    files = proto_files()
    lib.tool_program(files, "contrib/tools/ragel6", "ragel6")
    lib.tool_program(files, "tools/carver", "carver")
    schema = "grown" if generated else "seed"
    # Ragel accepts host C++ without machine directives. Feed the copied
    # generated C++ through it, preserving a real PB -> CP -> R6 -> CC chain.
    copy = f"COPY_FILE(AUTO {schema}.pb.cc reflected.cpp.rl6)\n"
    sources = f"SRCS({schema}.proto anchor.cpp)\n"
    body = "RUN_PROGRAM(tools/carver OUT_NOAUTO grown.proto)\n" if generated else ""
    body += sources + copy if after else copy + sources
    files.update({
        "quarry/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + body + "END()\n",
        "quarry/seed.proto": 'syntax = "proto3";\n',
        "quarry/anchor.cpp": "int anchor;\n",
    })
    return files


class SourceQueueFallbackTest(unittest.TestCase):
    def check_pipeline(self, generated, after):
        graph = lib.make(fixture(generated, after), "quarry", "--target-platform", "default-linux-x86_64")
        schema = "grown" if generated else "seed"
        pb = lib.node_by_output(graph, B + schema + ".pb.cc")
        copy = lib.node_by_output(graph, B + "reflected.cpp.rl6")
        ragel = lib.node_by_output(graph, B + "reflected.cpp")
        compiled = lib.node_by_output(graph, B + "reflected.cpp.o")
        archive = lib.node_by_output(graph, B + "libquarry.a")
        expected = [B + "anchor.cpp.o", B + schema + ".pb.cc.o", B + "reflected.cpp.o"]
        self.assertEqual(archive["cmds"][0]["cmd_args"][10:], expected)
        self.assertCountEqual([p for p in archive["inputs"] if p.endswith(".o")], expected)
        for output in expected:
            self.assertIn(lib.node_by_output(graph, output)["uid"], archive["deps"])
        self.assertEqual(pb["kv"]["p"], "PB")
        self.assertCountEqual(pb["outputs"], [B + schema + ".pb.cc", B + schema + ".pb.h"])
        self.assertIn((B if generated else S) + schema + ".proto", pb["inputs"])
        self.assertEqual(copy["cmds"][0]["cmd_args"][-2:], [B + schema + ".pb.cc", B + "reflected.cpp.rl6"])
        for producer, consumer, path in ((pb, copy, B + schema + ".pb.cc"),
                                         (copy, ragel, B + "reflected.cpp.rl6"),
                                         (ragel, compiled, B + "reflected.cpp")):
            self.assertIn(path, consumer["inputs"])
            self.assertIn(producer["uid"], consumer["deps"])
        self.assertEqual(ragel["kv"]["p"], "R6")
        self.assertEqual(ragel["cmds"][0]["cmd_args"][-3:], ["-o", B + "reflected.cpp", B + "reflected.cpp.rl6"])
        for tool, consumer in (("contrib/tools/protoc/protoc", pb), ("contrib/tools/ragel6/ragel6", ragel)):
            node = lib.node_by_output(graph, "$(B)/" + tool)
            self.assertIn(node["outputs"][0], consumer["inputs"])
            self.assertIn(node["uid"], consumer["deps"])
        if generated:
            source = lib.node_by_output(graph, B + "grown.proto")
            self.assertIn(source["uid"], pb["deps"])
            self.assertNotIn(S + "grown.proto", pb["inputs"])
        else:
            # A physical schema is the terminal leaf: PB has no local BUILD
            # input producer for the source queue fallback to follow.
            local_build_inputs = [p for p in pb["inputs"] if p.startswith(B)]
            self.assertEqual(local_build_inputs, [])

    def test_physical_schema_terminates_generated_source_traversal(self):
        for after in (False, True):
            with self.subTest(copy_after_sources=after):
                self.check_pipeline(False, after)

    def test_generated_schema_keeps_its_owned_input_producer(self):
        for after in (False, True):
            with self.subTest(copy_after_sources=after):
                self.check_pipeline(True, after)


if __name__ == "__main__":
    unittest.main(verbosity=2)
