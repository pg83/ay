import unittest

from tst import lib
from tst.make.test_proto_flatc import flatc_files


def library(body="", name=""):
    return f"LIBRARY({name})\n{lib.BARE_MODULE}{body}END()\n"


def peer_fixture(path, setup=""):
    return {
        "client/probe/ya.make": (
            f"PROGRAM(path_probe)\n{lib.BARE_MODULE}{setup}"
            f"PEERDIR(ADDINCL {path} vendor/numbers vendor/numbers)\n"
            "SRCS(entry.cpp)\nEND()\n"
        ),
        "client/probe/entry.cpp": '#include "value.h"\n',
        "vendor/numbers/ya.make": library(
            "SRCS(value.cpp)\nCFLAGS(GLOBAL -DNUMBER_PEER)\n", "numbers"
        ),
        "vendor/numbers/value.cpp": "int value;\n",
        "vendor/numbers/value.h": "#pragma once\n",
    }


class RootedPeerdirTest(unittest.TestCase):
    def test_rooted_and_expanded_paths_have_the_same_native_peer_effects(self):
        canonical = lib.make(peer_fixture("vendor/numbers"), "client/probe")
        for path, setup in (
            ("${ARCADIA_ROOT}/vendor/numbers", ""),
            ("$ARCADIA_ROOT/vendor/numbers", ""),
            ('"$(S)/vendor/numbers"', ""),
            ("${NUMBER_PEER}", "SET(BASE ${ARCADIA_ROOT}/vendor)\nSET(NUMBER_PEER ${BASE}/numbers)\n"),
            ("${NUMBER_PEERS}", "SET(NUMBER_PEERS ${ARCADIA_ROOT}/vendor/numbers vendor/numbers)\n"),
            ("///vendor//numbers///", ""),
            ("./vendor/unused/../numbers/.", ""),
            ("${ARCADIA_ROOT}////vendor//unused/../numbers///", ""),
        ):
            with self.subTest(path=path):
                graph = lib.make(peer_fixture(path, setup), "client/probe")
                self.assertEqual(graph, canonical)
                cc = lib.node_by_output(graph, "$(B)/client/probe/entry.cpp.o")
                self.assertIn("$(S)/vendor/numbers/value.h", cc["inputs"])
                args = cc["cmds"][0]["cmd_args"]
                self.assertEqual(args.count("-DNUMBER_PEER"), 1)
                self.assertEqual(args.count("-I$(S)/vendor/numbers"), 1)
                archive = lib.node_by_output(graph, "$(B)/vendor/numbers/libnumbers.a")
                program = lib.node_by_output(graph, "$(B)/client/probe/path_probe")
                self.assertIn(archive["uid"], program["deps"])
                self.assertEqual(program["inputs"].count("$(B)/vendor/numbers/libnumbers.a"), 1)

    def test_build_root_is_not_mapped_to_a_source_module(self):
        result = lib.make_process(peer_fixture("${ARCADIA_BUILD_ROOT}/vendor/numbers"), "client/probe")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("$(B)/vendor/numbers", result.stderr)

    def test_cached_include_resolves_curdir_in_each_module(self):
        files = {"shared/peers.inc": "PEERDIR(ADDINCL ${CURDIR}/engine)\n"}
        for directory, flag in (("cedar", "CEDAR"), ("birch", "BIRCH")):
            files[f"{directory}/ya.make"] = library(
                "INCLUDE(${ARCADIA_ROOT}/shared/peers.inc)\nSRCS(use.cpp)\n", directory
            )
            files[f"{directory}/use.cpp"] = '#include "local.h"\n'
            files[f"{directory}/engine/ya.make"] = library(
                f"CFLAGS(GLOBAL -D{flag}_ENGINE)\nSRCS(engine.cpp)\n", flag.lower()
            )
            files[f"{directory}/engine/engine.cpp"] = "int engine;\n"
            files[f"{directory}/engine/local.h"] = "#pragma once\n"
        for targets in (("cedar", "birch"), ("birch", "cedar")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for directory, other in (("cedar", "birch"), ("birch", "cedar")):
                    cc = lib.node_by_output(graph, f"$(B)/{directory}/use.cpp.o")
                    self.assertIn(f"$(S)/{directory}/engine/local.h", cc["inputs"])
                    self.assertNotIn(f"$(S)/{other}/engine/local.h", cc["inputs"])
                    self.assertIn(f"-D{directory.upper()}_ENGINE", cc["cmds"][0]["cmd_args"])
                    self.assertNotIn(f"-D{other.upper()}_ENGINE", cc["cmds"][0]["cmd_args"])

    def test_rooted_peer_publishes_generated_source_and_header_dependencies(self):
        files = {
            "renderer/ya.make": (
                f"PROGRAM(renderer)\n{lib.BARE_MODULE}"
                "PEERDIR(${ARCADIA_ROOT}/emitters/palette)\n"
                "SRCS(view.cpp)\nEND()\n"
            ),
            "renderer/view.cpp": '#include "emitters/palette/shade.h"\n',
            "emitters/palette/ya.make": library(
                "BASE_CODEGEN(tools/palette_writer shade)\n", "palette"
            ),
            "emitters/palette/shade.in": "",
        }
        lib.tool_program(files, "tools/palette_writer", "palette_writer")
        graph = lib.make(files, "renderer")
        generated = lib.node_by_output(graph, "$(B)/emitters/palette/shade.h")
        self.assertIn("$(B)/emitters/palette/shade.cpp", generated["outputs"])
        consumer = lib.node_by_output(graph, "$(B)/renderer/view.cpp.o")
        self.assertIn(generated["uid"], consumer["deps"])
        self.assertIn("$(B)/emitters/palette/shade.h", consumer["inputs"])
        self.assertIn("$(B)/emitters/palette/shade.cpp", consumer["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/palette_writer/palette_writer")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertEqual(generated["cmds"][0]["cmd_args"][0], "$(B)/tools/palette_writer/palette_writer")

    def test_cached_include_keeps_cpp_and_java_peers_in_their_language_contexts(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "shared/language_peers.inc": "PEERDIR(${ARCADIA_ROOT}//adapters/./${ADAPTER}/)\n",
            "schemas/shapes/ya.make": (
                f"FBS_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(CPP_FBS JAVA_FBS)\n"
                "IF(CPP_FBS)\nSET(ADAPTER native)\nELSE()\nSET(ADAPTER jvm)\nENDIF()\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/language_peers.inc)\n"
                "SRCS(shape.fbs native.cpp)\nEND()\n"
            ),
            "schemas/shapes/shape.fbs": "table Shape {}\n",
            "schemas/shapes/native.cpp": "int native;\n",
            "adapters/native/ya.make": library("CFLAGS(GLOBAL -DNATIVE_ADAPTER)\n", "native_adapter"),
            "adapters/jvm/ya.make": "JAR_LIBRARY(jvm_adapter)\nEND()\n",
            "contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make": "JAVA_CONTRIB()\nEND()\n",
        })
        graph = lib.make(files, "schemas/shapes")
        cc = lib.node_by_output(graph, "$(B)/schemas/shapes/native.cpp.o")
        self.assertIn("-DNATIVE_ADAPTER", cc["cmds"][0]["cmd_args"])
        jvm = lib.node_by_output(graph, "$(B)/adapters/jvm/jvm_adapter.jar")
        jar = lib.node_by_output(graph, "$(B)/schemas/shapes/schemas-shapes.jar")
        self.assertIn(jvm["uid"], jar["deps"])
        producer = lib.node_by_output(graph, "$(B)/schemas/shapes/shape.fbs.jsrc")
        self.assertIn(producer["uid"], jar["deps"])
        generated_cc = lib.node_by_output(graph, "$(B)/schemas/shapes/shape.fbs.cpp.o")
        header = lib.node_by_output(graph, "$(B)/schemas/shapes/shape.fbs.h")
        self.assertIn(header["uid"], generated_cc["deps"])
        self.assertIn("-DNATIVE_ADAPTER", generated_cc["cmds"][0]["cmd_args"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
