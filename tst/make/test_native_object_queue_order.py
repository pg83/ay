"""Local archive inputs follow native instruction and auto-source FIFO order."""
import unittest

from tst import lib
from tst.make import test_archive_asm_contract as archives
from tst.make import test_codegen_enum as enums


MODULE = "grove/parcel"
ROOT = "$(B)/" + MODULE + "/"
X86 = ("--target-platform", "default-linux-x86_64")


def world(body):
    files = archives.fixture("")
    files.pop("capsule/ya.make")
    files.update({
        MODULE + "/ya.make": f"LIBRARY(parcel)\n{lib.BARE_MODULE}CFLAGS(-DQUEUE_OWNER)\n" + body + "END()\n",
        MODULE + "/alpha.cpp": "",
        MODULE + "/tail.c": "",
        MODULE + "/native.S": "",
        MODULE + "/label.h": "enum Label { One };\n",
        MODULE + "/other.h": "enum Other { Two };\n",
        MODULE + "/record": "record\n",
        MODULE + "/local.h": "",
    })
    return enums.enum_files(files)


class NativeObjectQueueOrderTest(unittest.TestCase):
    def assert_archive(self, graph, objects):
        archive = lib.node_by_output(graph, ROOT + "libparcel.a")
        expected = [obj if obj.startswith("$(B)/") else ROOT + obj for obj in objects]
        # Full command, including toolchain/archive mode; do not sort local inputs.
        self.assertEqual(archive["cmds"][0]["cmd_args"], [
            "", "$(S)/build/scripts/link_lib.py", "", "LLVM_AR", "gnu", "$(B)", "None", "--", "--",
            ROOT + "libparcel.a", *expected,
        ])
        for output in expected:
            node = lib.node_by_output(graph, output)
            self.assertIn(node["uid"], archive["deps"])
            self.assertIn(output, archive["inputs"])
            self.assertTrue(set(node["outputs"]).issubset(archive["inputs"]))
        self.assertEqual({p for p in archive["inputs"] if p.endswith(".o")}, set(expected))
        return archive

    def test_listed_build_assembly_and_cpp_keep_source_queue_positions(self):
        for pic in (False, True):
            for explicit in (False, True):
                for order in (("embedded.S", "snapshot.cc"), ("snapshot.cc", "embedded.S")):
                    for before in (False, True):
                        with self.subTest(pic=pic, explicit=explicit, order=order, before=before):
                            paths = ["${ARCADIA_BUILD_ROOT}/grove/created/" + src if explicit else "../created/" + src for src in order]
                            generate = (
                                "RUN_PROGRAM(generators/sprout OUT_NOAUTO "
                                "${ARCADIA_BUILD_ROOT}/grove/created/main.txt "
                                "${ARCADIA_BUILD_ROOT}/grove/created/embedded.S "
                                "${ARCADIA_BUILD_ROOT}/grove/created/snapshot.cc "
                                f"OUTPUT_INCLUDES {MODULE}/local.h)\n"
                            )
                            sources = "SRCS(alpha.cpp " + " ".join(paths) + " tail.c native.S)\n"
                            files = world(generate + sources if before else sources + generate)
                            lib.tool_program(files, "generators/sprout", "sprout")
                            graph = lib.make(files, MODULE, *X86, "-DPIC=" + ("yes" if pic else "no"))
                            suffix = ".pic" if pic else ""
                            objects = ["alpha.cpp" + suffix + ".o"] + [
                                "__/created/" + src + ("" if src.endswith(".S") else suffix) + ".o" for src in order
                            ] + ["tail.c" + suffix + ".o", "native.S.o"]
                            self.assert_archive(graph, objects)
                            producer = lib.node_by_output(graph, "$(B)/grove/created/main.txt")
                            tool = lib.node_by_output(graph, "$(B)/generators/sprout/sprout")
                            self.assertEqual(producer["outputs"], [
                                "$(B)/grove/created/main.txt", "$(B)/grove/created/embedded.S", "$(B)/grove/created/snapshot.cc",
                            ])
                            self.assertIn(tool["uid"], producer["deps"])
                            self.assertIn(tool["outputs"][0], producer["inputs"])
                            for src in order:
                                output = "__/created/" + src + ("" if src.endswith(".S") else suffix) + ".o"
                                node = lib.node_by_output(graph, ROOT + output)
                                self.assertIn("$(B)/grove/created/" + src, node["inputs"])
                                self.assertIn(producer["outputs"][0], node["inputs"])
                                self.assertIn(producer["uid"], node["deps"])
                                self.assertIn("$(S)/" + MODULE + "/local.h", node["inputs"])
                                self.assertNotIn("$(S)/grove/created/" + src, node["inputs"])
                                self.assertIn("-DQUEUE_OWNER", node["cmds"][0]["cmd_args"])
                            for src in ("alpha.cpp", "tail.c", "native.S"):
                                node = lib.node_by_output(graph, ROOT + src + ("" if src.endswith(".S") else suffix) + ".o")
                                self.assertEqual(node["inputs"], ["$(S)/" + MODULE + "/" + src])

    def test_auto_sources_keep_macro_declaration_order_in_each_pic_context(self):
        for pic in (False, True):
            for reverse in (False, True):
                with self.subTest(pic=pic, reverse=reverse):
                    calls = ["GENERATE_ENUM_SERIALIZATION_WITH_HEADER(label.h)", "ARCHIVE_ASM(NAME ledger record)"]
                    if reverse:
                        calls.reverse()
                    graph = lib.make(world("SRCS(alpha.cpp)\n" + "\n".join(calls) + "\nSRCS(tail.c native.S)\n"),
                                     MODULE, *X86, "-DPIC=" + ("yes" if pic else "no"))
                    suffix = ".pic" if pic else ""
                    auto = ["label.h_serialized.cpp" + suffix + ".o", "ledger.rodata" + suffix + ".o"]
                    if reverse:
                        auto.reverse()
                    self.assert_archive(graph, ["alpha.cpp" + suffix + ".o", "tail.c" + suffix + ".o", "native.S.o", *auto])
                    enum = lib.node_by_output(graph, ROOT + "label.h_serialized.cpp")
                    self.assertEqual(enum["outputs"], [ROOT + "label.h_serialized.cpp", ROOT + "label.h_serialized.h"])
                    enum_tool = lib.node_by_output(graph, enums.ENUM_PARSER)
                    self.assertIn(enum_tool["uid"], enum["deps"])
                    self.assertIn("$(S)/" + MODULE + "/label.h", enum["inputs"])
                    compiled = lib.node_by_output(graph, ROOT + "label.h_serialized.cpp" + suffix + ".o")
                    self.assertIn(enum["uid"], compiled["deps"])
                    self.assertIn(enum["outputs"][0], compiled["inputs"])
                    packed = lib.node_by_output(graph, ROOT + "ledger.rodata")
                    rodata = lib.node_by_output(graph, ROOT + "ledger.rodata" + suffix + ".o")
                    archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
                    self.assertEqual(packed["inputs"], ["$(S)/" + MODULE + "/record", archiver["outputs"][0]])
                    self.assertIn(archiver["uid"], packed["deps"])
                    self.assertIn(packed["uid"], rodata["deps"])
                    self.assertIn(packed["outputs"][0], rodata["inputs"])

    def test_interleaved_generators_and_src_macro_priorities(self):
        graph = lib.make(world(
            "SRCS(alpha.cpp)\nARCHIVE_ASM(NAME first record)\n"
            "GENERATE_ENUM_SERIALIZATION(label.h)\nSRC(native.S -mavx)\n"
            "ARCHIVE_ASM(NAME second record)\n"
            "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(other.h)\nSRCS(tail.c)\n"
        ), MODULE, *X86)
        self.assert_archive(graph, [
            "native.S.o", "alpha.cpp.o", "tail.c.o", "first.rodata.o",
            "label.h_serialized.cpp.o", "second.rodata.o", "other.h_serialized.cpp.o",
        ])
        node = lib.node_by_output(graph, ROOT + "native.S.o")
        self.assertIn("-mavx", node["cmds"][0]["cmd_args"])
        self.assertIn("-DQUEUE_OWNER", node["cmds"][0]["cmd_args"])
        self.assertEqual(node["inputs"], ["$(S)/" + MODULE + "/native.S"])

    def test_src_build_action_waits_for_later_producer_and_keeps_per_source_flags(self):
        files = world(
            "SRCS(alpha.cpp tail.c)\n"
            "SRC(${ARCADIA_BUILD_ROOT}/grove/created/piece.S -mavx)\n"
            "RUN_PROGRAM(generators/sprout OUT_NOAUTO "
            "${ARCADIA_BUILD_ROOT}/grove/created/main.txt "
            "${ARCADIA_BUILD_ROOT}/grove/created/piece.S)\n"
        )
        lib.tool_program(files, "generators/sprout", "sprout")
        graph = lib.make(files, MODULE, *X86)
        archive = self.assert_archive(graph, ["alpha.cpp.o", "tail.c.o", "$(B)/grove/created/piece.S.o"])
        node = lib.node_by_output(graph, "$(B)/grove/created/piece.S.o")
        producer = lib.node_by_output(graph, "$(B)/grove/created/main.txt")
        self.assertIn("-mavx", node["cmds"][0]["cmd_args"])
        self.assertIn("-DQUEUE_OWNER", node["cmds"][0]["cmd_args"])
        self.assertIn(producer["uid"], node["deps"])
        self.assertIn(node["uid"], archive["deps"])
        self.assertIn(node["outputs"][0], archive["inputs"])
        self.assertEqual(node["inputs"], ["$(B)/grove/created/piece.S", "$(B)/grove/created/main.txt"])

    def test_listed_build_binary_keeps_position_without_reclassifying_auto_sources(self):
        files = world(
            "RUN_PROGRAM(generators/sprout OUT_NOAUTO "
            "${ARCADIA_BUILD_ROOT}/grove/created/main.txt "
            "${ARCADIA_BUILD_ROOT}/grove/parcel/blob.rodata "
            "${ARCADIA_BUILD_ROOT}/grove/created/piece.S)\n"
            "SRCS(alpha.cpp ${ARCADIA_BUILD_ROOT}/grove/parcel/blob.rodata tail.c "
            "${ARCADIA_BUILD_ROOT}/grove/created/piece.S)\n"
        )
        lib.tool_program(files, "generators/sprout", "sprout")
        graph = lib.make(files, MODULE, *X86)
        self.assert_archive(graph, ["alpha.cpp.o", "blob.rodata.o", "tail.c.o", "__/created/piece.S.o"])
        producer = lib.node_by_output(graph, "$(B)/grove/created/main.txt")
        for obj, source in (("blob.rodata.o", "grove/parcel/blob.rodata"), ("__/created/piece.S.o", "grove/created/piece.S")):
            compiled = lib.node_by_output(graph, ROOT + obj)
            self.assertIn("$(B)/" + source, compiled["inputs"])
            self.assertIn(producer["outputs"][0], compiled["inputs"])
            self.assertIn(producer["uid"], compiled["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
