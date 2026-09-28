import re
import unittest

import lib


BARE_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"
TOOLS = (
    "tools/rescompiler",
    "tools/rescompressor",
    "tools/gen",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/archiver",
)


def py3_library(body):
    files = {
        "library/cpp/resource/ya.make": BARE_LIBRARY,
        "contrib/libs/python/ya.make": BARE_LIBRARY,
        "lib/ya.make": f"PY3_LIBRARY()\n{body}END()\n",
        "lib/in.txt": "in",
    }
    for path in TOOLS:
        lib.tool_program(files, path, path.split("/")[-1])
    return files


def objcopy_inputs(graph):
    return [
        node["inputs"] for node in graph["graph"]
        if node["kv"]["p"] == "PY"
        and node["outputs"][0].startswith("$(B)/lib/objcopy_")
    ]


class ResourceRawTest(unittest.TestCase):
    def test_generated_bytecode_is_packed_through_rescompiler(self):
        graph = lib.make(py3_library(
            "RUN_PROGRAM(tools/gen IN in.txt OUT gen.py)\n"
            "PY_SRCS(${BINDIR}/gen.py)\n"
        ), "lib")
        [aux] = [
            node for node in graph["graph"]
            if node["kv"]["p"] == "PR"
            and node["outputs"][0].endswith("_raw.auxcpp")
        ]
        [output] = aux["outputs"]
        self.assertRegex(output, r"^\$\(B\)/lib/[0-9a-f]{26}_raw\.auxcpp$")
        self.assertEqual(aux["kv"], {"p": "PR", "pc": "yellow", "show_out": "yes"})
        self.assertEqual(aux["cmds"][0]["cmd_args"], [
            "$(B)/tools/rescompiler/rescompiler",
            output,
            "-",
            "resfs/src/resfs/file/py/lib/gen.py.yapyc3=lib/gen.py.yapyc3",
            "$(B)/lib/gen.py.yapyc3",
            "-resfs/file/py/lib/gen.py.yapyc3",
        ])
        self.assertEqual(aux["inputs"], [
            "$(B)/lib/gen.py.yapyc3",
            "$(B)/tools/rescompiler/rescompiler",
        ])
        rescompiler = lib.node_by_output(graph, "$(B)/tools/rescompiler/rescompiler")
        bytecode = lib.node_by_output(graph, "$(B)/lib/gen.py.yapyc3")
        self.assertEqual(
            sorted(aux["deps"]),
            sorted([rescompiler["uid"], bytecode["uid"]]),
        )

        compile_node = lib.node_by_output(graph, output + ".o")
        self.assertEqual(compile_node["kv"]["p"], "CC")
        args = compile_node["cmds"][0]["cmd_args"]
        self.assertEqual(args[-3:], ["-x", "c++", output])
        self.assertIn(aux["uid"], compile_node["deps"])

        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        self.assertIn(output + ".o", archive["inputs"])
        self.assertEqual(objcopy_inputs(graph)[0], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(S)/lib/in.txt",
            "$(B)/lib/gen.py",
            "$(S)/build/scripts/objcopy.py",
        ])

    def test_type_stubs_are_packed_with_objcopy(self):
        files = py3_library("PY_SRCS(types.pyi)\n")
        files["lib/types.pyi"] = "x: int\n"
        graph = lib.make(files, "lib")
        self.assertIn([
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(S)/lib/types.pyi",
            "$(S)/build/scripts/objcopy.py",
        ], objcopy_inputs(graph))
        self.assertFalse(any(
            re.search(r"_raw\.auxcpp$", output)
            for node in graph["graph"]
            for output in node["outputs"]
        ))

    def test_resource_py_sources_and_stubs_are_archived_in_order(self):
        files = py3_library(
            "RUN_PROGRAM(tools/gen IN in.txt OUT_NOAUTO gen.py)\n"
            "PY_SRCS(gen.py types.pyi mod.py)\n"
            "RESOURCE(data.txt key)\n"
        )
        files["lib/types.pyi"] = "x: int\n"
        files["lib/mod.py"] = "y = 1\n"
        files["lib/data.txt"] = "data\n"
        graph = lib.make(files, "lib")
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        objects = [
            path for path in archive["inputs"]
            if path.startswith("$(B)/lib/objcopy_")
        ]
        firsts = [
            [
                arg for arg in lib.node_by_output(graph, path)["cmds"][0]["cmd_args"]
                if arg.startswith("resfs/src/") or arg.startswith("py/namespace/")
                or arg == "$(S)/lib/data.txt"
            ][0]
            for path in objects
        ]
        self.assertEqual(firsts[0], "$(S)/lib/data.txt")
        self.assertTrue(firsts[1].startswith("py/namespace/"))
        self.assertEqual(firsts[2], "resfs/src/resfs/file/py/lib/gen.py=lib/gen.py")
        self.assertEqual(firsts[3], "resfs/src/resfs/file/py/lib/types.pyi=lib/types.pyi")
        sources = lib.node_by_output(graph, objects[2])
        self.assertEqual(sources["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(S)/lib/mod.py",
            "$(B)/lib/gen.py",
            "$(B)/lib/gen.py.yapyc3",
            "$(B)/lib/mod.py.yapyc3",
            "$(S)/build/scripts/objcopy.py",
        ])
        producer = lib.node_by_output(graph, "$(B)/lib/gen.py")
        self.assertIn(producer["uid"], sources["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
