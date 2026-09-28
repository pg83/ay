import unittest

import lib


FLATC = "$(B)/contrib/libs/flatbuffers/flatc/flatc"
FLATC64 = "$(B)/contrib/libs/flatbuffers64/flatc/flatc"
WRAPPER = "$(S)/build/scripts/cpp_flatc_wrapper.py"
RUNTIME = "$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h"
RUNTIME64 = "$(S)/contrib/libs/flatbuffers64/include/flatbuffers/flatbuffers.h"
EMPTY_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(x.cpp)\nEND()\n"


def flatc_files():
    files = {
        "build/scripts/cpp_flatc_wrapper.py": "print('flatc')\n",
        "contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h": "#pragma once\n",
        "contrib/libs/flatbuffers64/include/flatbuffers/flatbuffers.h": "#pragma once\n",
    }
    for runtime in ("contrib/libs/flatbuffers", "contrib/libs/flatbuffers64"):
        files[f"{runtime}/ya.make"] = EMPTY_LIBRARY
        files[f"{runtime}/x.cpp"] = "int x(){return 0;}\n"
    lib.tool_program(files, "contrib/libs/flatbuffers/flatc", "flatc")
    lib.tool_program(files, "contrib/libs/flatbuffers64/flatc", "flatc")
    return files


class ProtoFlatcTest(unittest.TestCase):
    def test_fbs_and_fbs64_producers_with_flags_and_includes(self):
        files = flatc_files()
        files.update({
            "f/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "FLATC_FLAGS(--scoped-enums --gen-name-strings)\n"
                "SRCS(a.fbs b.fbs c.fbs64 use.cpp)\nEND()\n"
            ),
            "f/a.fbs": (
                '// include "f/line_comment.fbs";\n'
                '/* include "f/block_comment.fbs"; */\n'
                'include "f/b.fbs";\n'
                '  include "f/b.fbs" ;\n'
                "include f/unquoted.fbs;\n"
                'include "";\n'
                'include "f/no_semicolon.fbs"\n'
                'include "f/trailing.fbs" x;\n'
                'include "f/unterminated.fbs\n'
                "includes \"f/keyword_prefix.fbs\";\n"
                "table A {}\n"
            ),
            "f/b.fbs": 'include "f/common.inc";\ntable B {}\n',
            "f/common.inc": 'include "f/leaf.fbs";\n',
            "f/common.inc.h": "#pragma once\n",
            "f/leaf.fbs": "table Leaf {}\n",
            "f/c.fbs64": "table C {}\n",
            "f/use.cpp": (
                '#include "a.fbs.h"\n#include "c.fbs64.h"\nint u(){return 0;}\n'
            ),
        })
        rejected = (
            "line_comment", "block_comment", "unquoted", "no_semicolon",
            "trailing", "unterminated", "keyword_prefix",
        )
        for name in rejected:
            files[f"f/{name}.fbs"] = "table R {}\n"
        graph = lib.make(files, "f")

        fl = lib.node_by_output(graph, "$(B)/f/a.fbs.h")
        self.assertEqual(fl["kv"], {"p": "FL", "pc": "light-green"})
        self.assertEqual(
            fl["outputs"], ["$(B)/f/a.fbs.h", "$(B)/f/a.fbs.cpp", "$(B)/f/a.bfbs"]
        )
        self.assertEqual(fl["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(fl["cmds"][0]["cmd_args"], [
            "", WRAPPER, FLATC, "--no-warnings", "--cpp", "--keep-prefix",
            "--gen-mutable", "--schema", "-b", "--gen-object-api",
            "--filename-suffix", ".fbs", "--scoped-enums", "--gen-name-strings",
            "-I", "$(B)", "-I", "$(S)", "-o", "$(B)/f/a.fbs.h", "$(S)/f/a.fbs",
        ])
        self.assertEqual(fl["inputs"], [
            FLATC, WRAPPER, "$(S)/f/a.fbs", "$(S)/f/b.fbs", "$(S)/f/common.inc",
            "$(S)/f/leaf.fbs",
        ])
        self.assertIn(lib.node_by_output(graph, FLATC)["uid"], fl["deps"])

        fl64 = lib.node_by_output(graph, "$(B)/f/c.fbs64.h")
        self.assertEqual(fl64["kv"], {"p": "FL64", "pc": "light-green"})
        self.assertEqual(fl64["outputs"], [
            "$(B)/f/c.fbs64.h", "$(B)/f/c.fbs64.cpp", "$(B)/f/c.bfbs64",
        ])
        self.assertEqual(fl64["cmds"][0]["cmd_args"], [
            "", WRAPPER, FLATC64, "--no-warnings", "--cpp", "--keep-prefix",
            "--gen-mutable", "--schema", "-b", "--filename-suffix", ".fbs64",
            "--scoped-enums", "--gen-name-strings",
            "-I", "$(S)", "-I", "$(B)", "-o", "$(B)/f/c.fbs64.h",
            "$(S)/f/c.fbs64",
        ])
        self.assertEqual(fl64["inputs"], [FLATC64, WRAPPER, "$(S)/f/c.fbs64"])

        compile_a = lib.node_by_output(graph, "$(B)/f/a.fbs.cpp.o")
        self.assertEqual(compile_a["inputs"][:3], [
            "$(B)/f/a.fbs.cpp", "$(B)/f/a.fbs.h", "$(B)/f/b.fbs.h",
        ])
        self.assertEqual(set(compile_a["inputs"][3:]), {
            "$(S)/f/common.inc.h", WRAPPER, "$(S)/f/a.fbs", "$(S)/f/b.fbs",
            "$(S)/f/common.inc", "$(S)/f/leaf.fbs", RUNTIME,
        })
        use = lib.node_by_output(graph, "$(B)/f/use.cpp.o")
        for path in ("$(B)/f/a.fbs.h", "$(B)/f/c.fbs64.h", RUNTIME, RUNTIME64):
            self.assertIn(path, use["inputs"])
        for name in rejected:
            self.assertNotIn(f"$(S)/f/{name}.fbs", use["inputs"])

        archive = lib.node_by_output(graph, "$(B)/f/libf.a")
        self.assertEqual([i for i in archive["inputs"] if i.endswith(".o")], [
            "$(B)/f/use.cpp.o", "$(B)/f/a.fbs.cpp.o", "$(B)/f/b.fbs.cpp.o",
            "$(B)/f/c.fbs64.cpp.o",
        ])

    def test_generated_schema_without_flags(self):
        files = flatc_files()
        files.update({
            "f/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "COPY_FILE(schema.in ${BINDIR}/gen.fbs)\n"
                "SRCS(${BINDIR}/gen.fbs)\nEND()\n"
            ),
            "f/schema.in": "table G {}\n",
        })
        graph = lib.make(files, "f")
        fl = lib.node_by_output(graph, "$(B)/f/gen.fbs.h")
        self.assertEqual(fl["cmds"][0]["cmd_args"], [
            "", WRAPPER, FLATC, "--no-warnings", "--cpp", "--keep-prefix",
            "--gen-mutable", "--schema", "-b", "--gen-object-api",
            "--filename-suffix", ".fbs",
            "-I", "$(B)", "-I", "$(S)", "-o", "$(B)/f/gen.fbs.h", "$(B)/f/gen.fbs",
        ])
        self.assertEqual(fl["inputs"], [FLATC, WRAPPER, "$(B)/f/gen.fbs"])
        copy = lib.node_by_output(graph, "$(B)/f/gen.fbs")
        self.assertIn(copy["uid"], fl["deps"])
        compile_node = lib.node_by_output(graph, "$(B)/f/gen.fbs.cpp.o")
        self.assertIn("$(B)/f/gen.fbs.h", compile_node["inputs"])
        self.assertNotIn("$(S)/f/gen.fbs", compile_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
