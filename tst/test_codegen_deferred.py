import json
import unittest

import lib


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n"


def project(producer, consumer, peers):
    return {
        "a/ya.make": library(f"{producer}\nSRCS(a.cpp)"),
        "a/a.cpp": "",
        "c/ya.make": library("SRCS(c.cpp)"),
        "c/c.cpp": "",
        "r/ya.make": library(f"PEERDIR({peers})\nSRCS(r.cpp)"),
        "r/r.cpp": consumer,
    }


class DeferredCodegenTest(unittest.TestCase):
    def test_copy_context_survives_sibling_module(self):
        for peers in ("a", "a c"):
            with self.subTest(peers=peers):
                files = project(
                    "ADDINCL(a/inc)\nCOPY_FILE_WITH_CONTEXT(x.h y.h)",
                    "#include <a/y.h>\n", peers,
                )
                files.update({"a/x.h": '#include "z.h"\n', "a/inc/z.h": ""})
                result = lib.make_process(files, "r")
                self.assertEqual(result.returncode, 0, result.stderr)
                graph = json.loads(result.stdout)
                copy = lib.node_by_output(graph, "$(B)/a/y.h")
                self.assertEqual(set(copy["inputs"]), {"$(S)/a/x.h", "$(S)/a/inc/z.h"})
                self.assertNotIn("missing-include", result.stderr)
                consumer = lib.node_by_output(graph, "$(B)/r/r.cpp.o")
                self.assertIn(copy["uid"], consumer["deps"])

    def test_buildinfo_keeps_owner_output_directory(self):
        for peers in ("a", "a c"):
            with self.subTest(peers=peers):
                files = project(
                    "CREATE_BUILDINFO_FOR(info.h)", "#include <a/info.h>\n", peers,
                )
                for script in ("build_info_gen.py", "xargs.py", "yield_line.py"):
                    files[f"build/scripts/{script}"] = ""
                graph = lib.make(files, "r")
                info = lib.node_by_output(graph, "$(B)/a/info.h")
                self.assertEqual(info["outputs"], ["$(B)/a/info.h"])
                for cmd in info["cmds"]:
                    self.assertIn("$(B)/a/__args", cmd["cmd_args"])
                self.assertEqual(info["cmds"][-1]["cmd_args"][-1], "$(B)/a/info.h")
                consumer = lib.node_by_output(graph, "$(B)/r/r.cpp.o")
                self.assertIn(info["uid"], consumer["deps"])
                self.assertFalse(set(info["inputs"]) & set(consumer["inputs"]))

    def test_one_fired_producer_does_not_release_another(self):
        files = project(
            "ADDINCL(a/inc ${ARCADIA_BUILD_ROOT}/a)\nCOPY_FILE_WITH_CONTEXT(x.h y.h)\n"
            "RUN_PROGRAM(tools/gen OUT first.h second.h)",
            "#include <a/y.h>\n", "a c",
        )
        files["r/ya.make"] = files["r/ya.make"].replace("LIBRARY()", "PROGRAM()")
        files.update({
            "a/a.cpp": '#include "first.h"\n#include "second.h"\n',
            "a/x.h": '#include "z.h"\n',
            "a/inc/z.h": "",
        })
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "r")
        copy = lib.node_by_output(graph, "$(B)/a/y.h")
        self.assertIn("$(S)/a/inc/z.h", copy["inputs"])
        early = lib.node_by_output(graph, "$(B)/a/first.h")
        self.assertEqual(early["outputs"], ["$(B)/a/first.h", "$(B)/a/second.h"])
        owner = lib.node_by_output(graph, "$(B)/a/a.cpp.o")
        self.assertIn(early["uid"], owner["deps"])

    def test_pending_emission_keeps_frame_during_tool_generation(self):
        files = project(
            "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(enums.h)",
            "#include <a/enums.h_serialized.h>\n", "a",
        )
        files.update({
            "a/enums.h": "enum E { A };\n",
            "util/generic/serialized_enum.h": "",
            "tools/enum_parser/enum_serialization_runtime/ya.make": library(""),
        })
        lib.tool_program(files, "tools/enum_parser/enum_parser", "enum_parser")
        graph = lib.make(files, "r")
        generated = lib.node_by_output(graph, "$(B)/a/enums.h_serialized.h")
        consumer = lib.node_by_output(graph, "$(B)/r/r.cpp.o")
        tool = lib.node_by_output(graph, "$(B)/tools/enum_parser/enum_parser/enum_parser")
        self.assertEqual(generated["platform"], consumer["platform"])
        self.assertNotEqual(generated["platform"], tool["platform"])
        self.assertIn(generated["uid"], consumer["deps"])
        self.assertEqual(generated["foreign_deps"], {"tool": [tool["uid"]]})


if __name__ == "__main__":
    unittest.main(verbosity=2)
