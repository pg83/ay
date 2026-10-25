"""Command order and file inputs checked against native ymake controls."""
import unittest

from tst import lib


class CommandConstructionQueueTest(unittest.TestCase):
    def test_direct_blocked_and_automatic_commands_keep_native_fifo_order(self):
        producer = "RUN_PYTHON3(gen.py OUT_NOAUTO generated.cpp)"
        generated = "SRC(${BINDIR}/generated.cpp)"
        plain = "SRC(plain.cpp)"
        cases = [
            ([producer, generated, plain], ["generated.cpp.o", "plain.cpp.o"]),
            ([generated, producer, plain], ["plain.cpp.o", "generated.cpp.o"]),
            ([generated, plain, producer], ["plain.cpp.o", "generated.cpp.o"]),
            (["RUN_PYTHON3(gen.py OUT generated.cpp)", "SRCS(plain.cpp)"],
             ["plain.cpp.o", "generated.cpp.o"]),
            (["JOIN_SRCS(joined.cpp a.cpp b.cpp)", "SRCS(plain.cpp)"],
             ["plain.cpp.o", "joined.cpp.o"]),
            ([producer, "SRCS(${BINDIR}/generated.cpp plain.cpp)"],
             ["generated.cpp.o", "plain.cpp.o"]),
            (["RUN_PYTHON3(gen.py IN ${BINDIR}/seed.dat OUT_NOAUTO generated.cpp)",
              "RUN_PYTHON3(gen.py OUT_NOAUTO seed.dat)",
              "SRCS(${BINDIR}/generated.cpp plain.cpp)"],
             ["generated.cpp.o", "plain.cpp.o"]),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                files = {
                    "probe/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "NO_LINT()\n"
                                     + "\n".join(body) + "\nEND()\n",
                    "probe/gen.py": "",
                    "probe/plain.cpp": "int plain;\n",
                    "probe/a.cpp": "int first;\n",
                    "probe/b.cpp": "int second;\n",
                }
                graph = lib.make(files, "probe")
                archive = lib.node_by_output(graph, "$(B)/probe/libprobe.a")
                members = [arg for cmd in archive["cmds"] for arg in cmd["cmd_args"]
                           if arg.endswith(".o")]
                self.assertEqual(members, ["$(B)/probe/" + name for name in expected])
                for output in members:
                    node = lib.node_by_output(graph, output)
                    self.assertIn(output, archive["inputs"])
                    self.assertIn(node["uid"], archive["deps"])
                generated_name = "joined.cpp" if "joined.cpp.o" in expected else "generated.cpp"
                source = lib.node_by_output(graph, "$(B)/probe/" + generated_name)
                compile_node = lib.node_by_output(graph, "$(B)/probe/" + generated_name + ".o")
                self.assertIn(source["uid"], compile_node["deps"])

    def test_proto_conversion_appends_each_generated_source_to_the_queue(self):
        from tst.make.test_proto_flatc import flatc_files

        files = flatc_files()
        files.update({
            "probe/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                             + "PROTO2FBS(created.proto)\nSRCS(simple.fbs direct.cpp)\nEND()\n",
            "probe/created.proto": 'syntax = "proto3";\nmessage Created {}\n',
            "probe/simple.fbs": "table Simple {}\n",
            "probe/direct.cpp": "int direct;\n",
        })
        graph = lib.make(files, "probe")
        archive = lib.node_by_output(graph, "$(B)/probe/libprobe.a")
        members = [arg for cmd in archive["cmds"] for arg in cmd["cmd_args"]
                   if arg.endswith(".o")]
        self.assertEqual(members, ["$(B)/probe/" + name for name in
                                  ("direct.cpp.o", "simple.fbs.cpp.o", "created.fbs.cpp.o")])
        converted = lib.node_by_output(graph, "$(B)/probe/created.fbs")
        flatc = lib.node_by_output(graph, "$(B)/probe/created.fbs.h")
        compiled = lib.node_by_output(graph, "$(B)/probe/created.fbs.cpp.o")
        self.assertIn("$(S)/probe/created.proto", converted["inputs"])
        self.assertIn(converted["uid"], flatc["deps"])
        self.assertIn(flatc["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], archive["deps"])

    def test_header_declared_after_its_consumer_keeps_template_inputs(self):
        files = {
            "probe/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                             + "SRCS(consumer.cpp late.h.in)\nEND()\n",
            "probe/consumer.cpp": '#include "late.h"\nint consume;\n',
            "probe/late.h.in": '#include "marker.h"\n',
            "probe/marker.h": "// marker\n",
        }
        graph = lib.make(files, "probe")
        consumer = lib.node_by_output(graph, "$(B)/probe/consumer.cpp.o")
        for path in ("$(B)/probe/late.h", "$(S)/probe/consumer.cpp",
                     "$(S)/probe/late.h.in", "$(S)/probe/marker.h"):
            self.assertIn(path, consumer["inputs"])
        header = lib.node_by_output(graph, "$(B)/probe/late.h")
        self.assertIn(header["uid"], consumer["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
