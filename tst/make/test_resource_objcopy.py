import base64
import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
ROOTREL = "${rootrel;context=TEXT;input=TEXT:"


def base_files(body):
    files = {
        "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
        "lib/ya.make": f"LIBRARY()\n{BARE}{body}END()\n",
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    lib.tool_program(files, "tools/gen", "gen")
    return files


def objcopy_nodes(graph):
    return [
        node for node in graph["graph"]
        if node["kv"]["p"] == "PY"
        and node["outputs"][0].startswith("$(B)/lib/objcopy_")
    ]


def section(args, flag):
    if flag not in args:
        return []
    start = args.index(flag) + 1
    end = start
    while end < len(args) and not args[end].startswith("--"):
        end += 1
    return args[start:end]


def make_error(files):
    try:
        lib.make(files, "lib")
    except AssertionError as error:
        return str(error)
    raise AssertionError("ay make unexpectedly succeeded")


class ResourceObjcopyTest(unittest.TestCase):
    def test_archive_retains_template_resource_inputs(self):
        for filename, expected in (("data.g.in", True), ("data.h.in", True), ("data.txt", False), ("data.h", False)):
            with self.subTest(filename=filename):
                files = base_files(f"SRCS(local.cpp)\nRESOURCE(DONT_PARSE {filename} payload)\n")
                files["lib/local.cpp"] = "int local;\n"
                files[f"lib/{filename}"] = "payload\n"
                graph = lib.make(files, "lib")
                archive = lib.node_by_output(graph, "$(B)/lib/liblib.global.a")
                self.assertEqual(f"$(S)/lib/{filename}" in archive["inputs"], expected)

    def test_kv_entries_resolve_rootrel_inputs(self):
        files = base_files(
            "RUN_PROGRAM(tools/gen IN in.txt OUT_NOAUTO gen.bin)\n"
            "RESOURCE(\n"
            f"  - resfs/src/k=\\{ROOTREL}\"a.txt\"}}\n"
            f"  - resfs/src/g=\\{ROOTREL}\"gen.bin\"}}\n"
            "  - x=${ARCADIA_ROOT}/y\n"
            f"  - 'open={ROOTREL}\"unterminated'\n"
            ")\n"
        )
        files["lib/a.txt"] = "a"
        files["lib/in.txt"] = "in"
        graph = lib.make(files, "lib")
        [node] = objcopy_nodes(graph)
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(section(args, "--inputs"), [])
        self.assertEqual(section(args, "--kvs"), [
            "resfs/src/k=lib/a.txt",
            "resfs/src/g=lib/gen.bin",
            "x=$(S)/y",
            f"open={ROOTREL}\"unterminated",
        ])
        self.assertEqual(node["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(S)/lib/a.txt",
            "$(B)/lib/gen.bin",
            "$(S)/build/scripts/objcopy.py",
        ])
        producer = lib.node_by_output(graph, "$(B)/lib/gen.bin")
        self.assertIn(producer["uid"], node["deps"])

    def test_generated_inputs_carry_their_build_closure(self):
        files = base_files(
            "RUN_PROGRAM(tools/gen IN in.cfg OUT_NOAUTO gen.h gen_inc.h"
            " OUTPUT_INCLUDES ${ARCADIA_BUILD_ROOT}/lib/gen_inc.h lib/src.h)\n"
            "RESOURCE(${BINDIR}/gen.h genkey)\n"
            "RESOURCE_FILES(PREFIX p/ ${BINDIR}/gen_inc.h)\n"
        )
        files["lib/in.cfg"] = "cfg"
        files["lib/src.h"] = ""
        graph = lib.make(files, "lib")
        first, second = objcopy_nodes(graph)
        producer = lib.node_by_output(graph, "$(B)/lib/gen.h")
        self.assertEqual(first["inputs"], [
            '$(B)/tools/rescompiler/rescompiler',
            '$(B)/tools/rescompressor/rescompressor',
            '$(B)/lib/gen.h',
            '$(S)/build/scripts/objcopy.py',
            '$(B)/lib/gen_inc.h',
            '$(S)/lib/src.h',
        ])
        first_args = first["cmds"][0]["cmd_args"]
        self.assertEqual(section(first_args, "--inputs"), ["$(B)/lib/gen.h"])
        self.assertEqual(
            [base64.b64decode(k).decode() for k in section(first_args, "--keys")],
            ["genkey"],
        )
        self.assertEqual(second["inputs"], [
            '$(B)/tools/rescompiler/rescompiler',
            '$(B)/tools/rescompressor/rescompressor',
            '$(B)/lib/gen_inc.h',
            '$(S)/build/scripts/objcopy.py',
            '$(B)/lib/gen.h',
            '$(S)/lib/src.h',
        ])
        self.assertEqual(
            section(second["cmds"][0]["cmd_args"], "--kvs"),
            ["resfs/src/resfs/file/p/$(B)/lib/gen_inc.h=lib/gen_inc.h"],
        )
        self.assertEqual(
            [base64.b64decode(k).decode() for k in section(second["cmds"][0]["cmd_args"], "--keys")],
            ["resfs/file/p/${BINDIR}/gen_inc.h"],
        )
        for node in (first, second):
            self.assertIn(producer["uid"], node["deps"])

    def test_large_batch_splits_into_command_sized_chunks(self):
        names = [f"file_{i:02}.dat" for i in range(24)]
        files = base_files(f"RESOURCE_FILES({' '.join(names)})\n")
        for name in names:
            files[f"lib/{name}"] = name
        graph = lib.make(files, "lib")
        chunks = [
            section(node["cmds"][0]["cmd_args"], "--inputs")
            for node in objcopy_nodes(graph)
        ]
        self.assertEqual(len(chunks), 2)
        self.assertEqual(
            [path for chunk in chunks for path in chunk],
            [f"$(S)/lib/{name}" for name in names],
        )
        self.assertEqual(len(set(n["outputs"][0] for n in objcopy_nodes(graph))), 2)

    def test_raw_resources_compile_on_arm(self):
        for body in (
            "RUN_PROGRAM(tools/gen OUT_NOAUTO x.txt)\nRESOURCE(${ARCADIA_BUILD_ROOT}/lib/x.txt key)\n",
            "RESOURCE(conftest.py key)\n",
        ):
            with self.subTest(body=body):
                files = base_files(body)
                files["lib/conftest.py"] = ""
                graph = lib.make(files, "lib")
                raw = next(n for n in graph["graph"] if n["outputs"][0].endswith("_raw.auxcpp"))
                compiled = next(n for n in graph["graph"] if n["kv"]["p"] == "CC"
                                and raw["outputs"][0] in n["inputs"])
                self.assertIn("-x", compiled["cmds"][0]["cmd_args"])
                self.assertIn("c++", compiled["cmds"][0]["cmd_args"])
                if "conftest" in body:
                    self.assertIn("$(S)/lib/conftest.py", raw["inputs"])

    def test_non_objcopy_resources_use_yasm_on_x86(self):
        files = base_files("RESOURCE(templates/conftest.py.j2 /template)\n")
        files["lib/templates/conftest.py.j2"] = ""
        lib.tool_program(files, "tools/rorescompiler", "rorescompiler")
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "lib", "--target-platform", "default-linux-x86_64")
        compressed = next(n for n in graph["graph"] if n["outputs"][0].endswith(".yasm"))
        self.assertEqual(len(compressed["outputs"]), 2)
        self.assertTrue(compressed["outputs"][1].endswith(".rodata"))
        self.assertIn("$(S)/lib/templates/conftest.py.j2", compressed["inputs"])
        aux = next(n for n in graph["graph"] if n["outputs"][0].endswith(".auxcpp"))
        self.assertEqual(aux["cmds"][0]["cmd_args"][0], "$(B)/tools/rorescompiler/rorescompiler")
        self.assertTrue(aux["cmds"][0]["cmd_args"][-1].endswith("=/template"))
        assembled = next(n for n in graph["graph"] if n["kv"]["p"] == "AS")
        self.assertIn(compressed["uid"], assembled["deps"])
        self.assertIn(compressed["outputs"][0], assembled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
