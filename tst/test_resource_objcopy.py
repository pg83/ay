import base64
import unittest

import lib


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
    def test_kv_entries_resolve_rootrel_inputs(self):
        files = base_files(
            "RUN_PROGRAM(tools/gen IN in.txt OUT_NOAUTO gen.bin)\n"
            "RESOURCE(\n"
            f"  - 'resfs/src/k={ROOTREL}\"a.txt\"}}'\n"
            f"  - 'resfs/src/g={ROOTREL}\"gen.bin\"}}'\n"
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
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(B)/lib/gen.h",
            "$(S)/build/scripts/objcopy.py",
            "$(S)/lib/in.cfg",
            "$(B)/lib/gen_inc.h",
        ])
        first_args = first["cmds"][0]["cmd_args"]
        self.assertEqual(section(first_args, "--inputs"), ["$(B)/lib/gen.h"])
        self.assertEqual(
            [base64.b64decode(k).decode() for k in section(first_args, "--keys")],
            ["genkey"],
        )
        self.assertEqual(second["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(B)/lib/gen_inc.h",
            "$(S)/build/scripts/objcopy.py",
            "$(S)/lib/in.cfg",
            "$(B)/lib/gen.h",
        ])
        self.assertEqual(
            section(second["cmds"][0]["cmd_args"], "--kvs"),
            ["resfs/src/resfs/file/p/$(B)/lib/gen_inc.h=lib/gen_inc.h"],
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

    def test_build_root_resource_needs_raw_compile(self):
        for body in (
            "RESOURCE(${ARCADIA_BUILD_ROOT}/lib/x.txt key)\n",
            "RESOURCE(conftest.py key)\n",
        ):
            with self.subTest(body=body):
                files = base_files(body)
                files["lib/conftest.py"] = ""
                self.assertIn(
                    "packResources: lib has raw-routed resource items but no RawCompile",
                    make_error(files),
                )


if __name__ == "__main__":
    unittest.main(verbosity=2)
