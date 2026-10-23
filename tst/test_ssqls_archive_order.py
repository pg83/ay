"""SSQLS automatic output compilation follows native source queue rounds."""
import unittest

import lib
from test_codegen_ssqls import BARE
from test_proto_cpp import proto_files


def fixture(sources):
    files = {
        "grove/ya.make": "LIBRARY()\n" + BARE + sources + "\nEND()\n",
        "grove/tables/zeta.ssqls": "<DbObject/>\n",
        "grove/tables/alpha.ssqls": "<DbObject/>\n",
        "grove/seed.ssqls": "<DbObject/>\n",
        "grove/first.cpp": "int first;\n",
        "grove/second.cpp": "int second;\n",
        "grove/schema.proto": 'syntax = "proto2"; message Schema {}\n',
    }
    files.update(proto_files())
    lib.tool_program(files, "metrika/core/tools/ssqls", "sql_driver")
    return files


def members(graph):
    archive = next(n for n in graph["graph"] if n.get("kv", {}).get("p") == "AR" and "/grove/" in n["outputs"][0])
    return [x.removeprefix("$(B)/grove/") for x in archive["cmds"][0]["cmd_args"] if x.endswith(".o")]


class SsqlsArchiveOrderTest(unittest.TestCase):
    def test_generated_table_members_follow_initial_sources_in_declaration_order(self):
        cases = (
            ("SRCS(tables/zeta.ssqls tables/alpha.ssqls)\nSRCS(first.cpp second.cpp schema.proto)", ["first.cpp.o", "second.cpp.o", "_/tables/zeta.cpp.o", "_/tables/alpha.cpp.o", "schema.pb.cc.o"]),
            ("SRCS(first.cpp schema.proto second.cpp)\nSRCS(tables/alpha.ssqls tables/zeta.ssqls)", ["first.cpp.o", "second.cpp.o", "schema.pb.cc.o", "_/tables/alpha.cpp.o", "_/tables/zeta.cpp.o"]),
            ("SRCS(tables/zeta.ssqls first.cpp schema.proto tables/alpha.ssqls second.cpp)", ["first.cpp.o", "second.cpp.o", "_/tables/zeta.cpp.o", "schema.pb.cc.o", "_/tables/alpha.cpp.o"]),
            ("SRCS(schema.proto tables/zeta.ssqls first.cpp tables/alpha.ssqls second.cpp)", ["first.cpp.o", "second.cpp.o", "schema.pb.cc.o", "_/tables/zeta.cpp.o", "_/tables/alpha.cpp.o"]),
        )
        for source, expected in cases:
            with self.subTest(source=source):
                graph = lib.make(fixture(source), "grove", "--sandboxing")
                self.assertEqual(members(graph), expected)
                for name in ("zeta", "alpha"):
                    producer = lib.node_by_output(graph, "$(B)/grove/tables/" + name + ".cpp")
                    compiler = lib.node_by_output(graph, "$(B)/grove/_/tables/" + name + ".cpp.o")
                    self.assertIn(producer["uid"], compiler["deps"])
                    self.assertIn("$(S)/grove/tables/" + name + ".ssqls", producer["inputs"])

    def test_explicit_generated_table_keeps_copy_input_and_queue_round(self):
        source = "COPY_FILE(seed.ssqls tables/later.ssqls)\nSRCS(${ARCADIA_BUILD_ROOT}/grove/tables/later.ssqls first.cpp schema.proto second.cpp)"
        graph = lib.make(fixture(source), "grove", "--sandboxing")
        self.assertEqual(members(graph), ["first.cpp.o", "second.cpp.o", "_/tables/later.cpp.o", "schema.pb.cc.o"])
        copy = lib.node_by_output(graph, "$(B)/grove/tables/later.ssqls")
        sql = lib.node_by_output(graph, "$(B)/grove/tables/later.cpp")
        self.assertIn("$(S)/grove/seed.ssqls", copy["inputs"])
        self.assertIn(copy["uid"], sql["deps"])
        self.assertIn("$(B)/grove/tables/later.ssqls", sql["inputs"])

    def test_ordinary_and_table_only_order_are_preserved(self):
        for source, expected in (("SRCS(second.cpp first.cpp)", ["second.cpp.o", "first.cpp.o"]),
                                 ("SRCS(tables/zeta.ssqls tables/alpha.ssqls)", ["_/tables/zeta.cpp.o", "_/tables/alpha.cpp.o"])):
            with self.subTest(source=source):
                self.assertEqual(members(lib.make(fixture(source), "grove", "--sandboxing")), expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
