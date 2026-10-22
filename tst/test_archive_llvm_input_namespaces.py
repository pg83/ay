import unittest

import lib


MODULE = "cedar/dispatch"
LIBRARY = "LIBRARY()\n" + lib.BARE_MODULE
X86 = ("--target-platform", "default-linux-x86_64")
ROOTS = (
    ("${ARCADIA_ROOT}/" + MODULE, "$(S)"),
    ("${CURDIR}", "$(S)"),
    ("$(S)/" + MODULE, "$(S)"),
    ("${ARCADIA_BUILD_ROOT}/" + MODULE, "$(B)"),
    ("${BINDIR}", "$(B)"),
    ("$(B)/" + MODULE, "$(B)"),
    ("", "$(B)"),
)


def fixture(body, filename="payload.dat", producer_first=True):
    producer = (
        f"RUN_PROGRAM(tools/scribe OUT_NOAUTO stamp.h {filename} "
        "OUTPUT_INCLUDES ${CURDIR}/generated_detail.h)\n"
    )
    files = {
        MODULE + "/ya.make": (
            LIBRARY + "USE_LLVM_BC20()\nADDINCL(${BINDIR})\n"
            + (producer + body if producer_first else body + producer)
            + "SRCS(use.cpp)\nEND()\n"
        ),
        MODULE + "/" + filename: '#include "source_detail.h"\n',
        MODULE + "/source_detail.h": "",
        MODULE + "/generated_detail.h": "",
        MODULE + "/use.cpp": '#include "stamp.h"\n',
        "build/platform/clang/ya.make": lib.resource_lib("CLANG20", 7),
        "build/platform/python/ymake_python3/ya.make": lib.resource_lib("YMAKE_PYTHON3", 8),
        "library/cpp/resource/ya.make": LIBRARY + "END()\n",
        "build/scripts/rodata2asm.py": "",
    }
    for tool in ("tools/scribe", "tools/archiver", "contrib/tools/yasm", "tools/rescompiler", "tools/rescompressor"):
        lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
    return files


def token(prefix, filename):
    return '"' + (prefix + "/" if prefix else "") + filename + '"'


class ArchiveLlvmInputNamespacesTest(unittest.TestCase):
    def assert_namespace(self, graph, consumer, root, filename):
        producer = lib.node_by_output(graph, "$(B)/" + MODULE + "/stamp.h")
        chosen = root + "/" + MODULE + "/" + filename
        other = ("$(S)" if root == "$(B)" else "$(B)") + "/" + MODULE + "/" + filename
        self.assertIn(chosen, consumer["inputs"])
        self.assertNotIn(other, consumer["inputs"])
        generated = root == "$(B)"
        self.assertEqual(producer["uid"] in consumer["deps"], generated)
        self.assertEqual("$(B)/" + MODULE + "/stamp.h" in consumer["inputs"], generated)
        return chosen

    def test_archive_variants_preserve_explicit_input_roots_over_build_shadows(self):
        for macro in ("ARCHIVE", "ARCHIVE_BY_KEYS", "ARCHIVE_ASM"):
            for prefix, root in ROOTS:
                with self.subTest(macro=macro, prefix=prefix):
                    key = "KEYS /payload" if macro == "ARCHIVE_BY_KEYS" else ""
                    body = f"{macro}(NAME parcel {key} {token(prefix, 'payload.dat')})\n"
                    files = fixture(body)
                    if macro != "ARCHIVE_ASM":
                        files[MODULE + "/use.cpp"] += '#include "parcel"\n'
                    graph = lib.make(files, MODULE, *X86)
                    output = "$(B)/" + MODULE + "/parcel" + (".rodata" if macro == "ARCHIVE_ASM" else "")
                    archive = lib.node_by_output(graph, output)
                    chosen = self.assert_namespace(graph, archive, root, "payload.dat")
                    member = chosen if macro == "ARCHIVE_BY_KEYS" else chosen + ":"
                    self.assertIn(member, archive["cmds"][0]["cmd_args"])

    def test_llvm_compile_uses_source_headers_or_registered_output_context(self):
        for prefix, root in ROOTS:
            with self.subTest(prefix=prefix):
                files = fixture(f"LLVM_COMPILE_CXX({token(prefix, 'translation.cpp')} selected.bc)\n"
                                "RESOURCE(${BINDIR}/selected.bc selected)\n", "translation.cpp")
                graph = lib.make(files, MODULE, *X86)
                compiled = lib.node_by_output(graph, "$(B)/" + MODULE + "/selected.bc")
                chosen = self.assert_namespace(graph, compiled, root, "translation.cpp")
                args = compiled["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("-c") + 1], chosen)
                for name, expected in (("source_detail.h", root == "$(S)"), ("generated_detail.h", root == "$(B)")):
                    self.assertEqual("$(S)/" + MODULE + "/" + name in compiled["inputs"], expected)

    def test_llvm_bc_root_relative_name_does_not_rebind_a_source_input(self):
        for prefix, root in ROOTS[:4]:
            with self.subTest(prefix=prefix):
                graph = lib.make(fixture(
                    f"LLVM_BC({token(prefix, 'translation.cpp')} NAME parcel)\n", "translation.cpp",
                ), MODULE, *X86)
                compiled = lib.node_by_output(graph, "$(B)/" + MODULE + "/translation.cpp.bc")
                chosen = self.assert_namespace(graph, compiled, root, "translation.cpp")
                self.assertEqual(compiled["cmds"][0]["cmd_args"][-3:], [chosen, "-o", compiled["outputs"][0]])

    def test_archive_source_input_does_not_create_a_false_scheduler_cycle(self):
        files = fixture(
            "ARCHIVE(NAME seed.inc ${CURDIR}/payload.dat)\n"
            "RUN_PROGRAM(tools/scribe IN ${BINDIR}/seed.inc OUT_NOAUTO payload.dat)\n"
            "COPY_FILE(${BINDIR}/payload.dat delivered.h)\n",
        )
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            "OUT_NOAUTO stamp.h payload.dat", "OUT_NOAUTO stamp.h"
        )
        files[MODULE + "/use.cpp"] += '#include "delivered.h"\n'
        graph = lib.make(files, MODULE, *X86)
        archive = lib.node_by_output(graph, "$(B)/" + MODULE + "/seed.inc")
        producer = lib.node_by_output(graph, "$(B)/" + MODULE + "/payload.dat")
        self.assertIn("$(S)/" + MODULE + "/payload.dat", archive["inputs"])
        self.assertNotIn(producer["uid"], archive["deps"])
        self.assertIn(archive["uid"], producer["deps"])

    def test_assembly_archive_waits_for_a_later_declared_archive_producer(self):
        graph = lib.make(fixture(
            "ARCHIVE_ASM(NAME parcel ${BINDIR}/intermediate.dat)\n"
            "ARCHIVE(NAME intermediate.dat ${CURDIR}/payload.dat)\n",
        ), MODULE, *X86)
        archive = lib.node_by_output(graph, "$(B)/" + MODULE + "/parcel.rodata")
        producer = lib.node_by_output(graph, "$(B)/" + MODULE + "/intermediate.dat")
        self.assertIn(producer["uid"], archive["deps"])
        self.assertIn(producer["outputs"][0], archive["inputs"])

    def test_unrooted_module_prefix_is_a_module_build_candidate(self):
        nested = MODULE + "/payload.dat"
        files = fixture(f'ARCHIVE(NAME parcel "{nested}")\n', nested)
        files[MODULE + "/payload.dat"] = "physical root source\n"
        files[MODULE + "/use.cpp"] += '#include "parcel"\n'
        graph = lib.make(files, MODULE, *X86)
        archive = lib.node_by_output(graph, "$(B)/" + MODULE + "/parcel")
        self.assertIn("$(B)/" + MODULE + "/" + nested, archive["inputs"])
        self.assertNotIn("$(S)/" + nested, archive["inputs"])

    def test_llvm_bc_unrooted_source_uses_its_resolved_root_relative_path(self):
        files = fixture("LLVM_BC(shared/translation.cpp NAME parcel)\n")
        files["shared/translation.cpp"] = "int physical_source;\n"
        graph = lib.make(files, MODULE, *X86)
        compiled = lib.node_by_output(graph, "$(B)/shared/translation.cpp.bc")
        self.assertIn("$(S)/shared/translation.cpp", compiled["inputs"])

    def test_llvm_bc_unrooted_build_candidate_keeps_its_existing_bitcode_name(self):
        graph = lib.make(fixture("LLVM_BC(translation.cpp NAME parcel)\n", "translation.cpp"), MODULE, *X86)
        compiled = lib.node_by_output(graph, "$(B)/translation.cpp.bc")
        self.assert_namespace(graph, compiled, "$(B)", "translation.cpp")


if __name__ == "__main__":
    unittest.main(verbosity=2)
