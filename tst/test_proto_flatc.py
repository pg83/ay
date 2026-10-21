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
        self.assertEqual(lib.bucketed(fl["inputs"], 3), lib.bucketed([
            FLATC, WRAPPER, "$(S)/f/a.fbs", "$(S)/f/b.fbs", "$(S)/f/common.inc",
            "$(S)/f/leaf.fbs",
        ], 3))
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
            RUNTIME,
            '$(S)/f/common.inc.h',
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

    def test_proto2fbs_preserves_nested_output_and_chains_cpp_generation(self):
        files = flatc_files()
        files.update({
            "build/scripts/fs_tools.py": 'import process_command_files\n',
            "build/scripts/process_command_files.py": '',
            "f/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PROTO2FBS(nested/schema.proto)\nEND()\n"
            ),
            "f/nested/schema.proto": 'syntax = "proto3";\nmessage S {}\n',
        })
        graph = lib.make(files, "f")
        output = "$(B)/f/nested/schema.fbs"
        converted = lib.node_by_output(graph, output)
        self.assertEqual(converted["kv"], {"p": "FBS", "pc": "yellow"})
        self.assertEqual(converted["outputs"], [output])
        self.assertEqual(converted["cmds"][0]["cwd"], "$(B)/f")
        self.assertEqual(converted["cmds"][0]["cmd_args"], [
            FLATC, "-I", ".", "-I", "$(S)", "--proto", "$(S)/f/nested/schema.proto",
        ])
        self.assertNotIn("cwd", converted["cmds"][1])
        self.assertEqual(converted["cmds"][1]["cmd_args"], [
            "", "$(S)/build/scripts/fs_tools.py", "rename", "$(B)/f/schema.fbs", output,
        ])
        self.assertEqual(set(converted["inputs"]), {
            FLATC, "$(S)/f/nested/schema.proto", "$(S)/build/scripts/fs_tools.py",
            "$(S)/build/scripts/process_command_files.py",
        })
        generated = lib.node_by_output(graph, output + ".h")
        self.assertIn(output, generated["cmds"][0]["cmd_args"])
        self.assertIn(converted["uid"], generated["deps"])
        self.assertNotIn("$(S)/f/nested/schema.proto", generated["inputs"])
        compiled = next(node for node in graph["graph"] if node["kv"].get("p") == "CC" and node["inputs"][0] == output + ".cpp")
        self.assertIn(output + ".h", compiled["inputs"])
        archive = lib.node_by_output(graph, "$(B)/f/libf.a")
        self.assertIn(compiled["outputs"][0], archive["inputs"])

    def test_proto2fbs_generated_input_is_scheduled_before_conversion(self):
        files = flatc_files()
        lib.tool_program(files, "tools/proto", "proto")
        files.update({
            "build/scripts/fs_tools.py": '',
            "f/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PROTO2FBS(${BINDIR}/nested/generated.proto)\n"
                "RUN_PROGRAM(tools/proto IN template.txt OUT_NOAUTO ${BINDIR}/nested/generated.proto)\nEND()\n"
            ),
            "f/template.txt": 'message Generated {}\n',
        })
        graph = lib.make(files, "f")
        input_proto = lib.node_by_output(graph, "$(B)/f/nested/generated.proto")
        converted = lib.node_by_output(graph, "$(B)/f/nested/generated.fbs")
        self.assertIn(input_proto["uid"], converted["deps"])
        self.assertEqual(converted["cmds"][0]["cmd_args"][-1], "$(B)/f/nested/generated.proto")
        generated = lib.node_by_output(graph, "$(B)/f/nested/generated.fbs.h")
        self.assertIn(converted["uid"], generated["deps"])

    def test_proto2fbs_keeps_imports_and_induced_generated_headers(self):
        files = flatc_files()
        lib.tool_program(files, "tools/header", "header")
        files.update({
            "build/scripts/fs_tools.py": 'import process_command_files\n',
            "build/scripts/process_command_files.py": '',
            "f/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "RUN_PROGRAM(tools/header OUT_NOAUTO ${BINDIR}/dep.pb.h)\n"
                "PROTO2FBS(nested/schema.proto)\nEND()\n"
            ),
            "f/nested/schema.proto": 'syntax = "proto3";\nimport "f/dep.proto";\nmessage S {}\n',
            "f/dep.proto": 'syntax = "proto3";\nmessage D {}\n',
        })
        graph = lib.make(files, "f")
        converted = lib.node_by_output(graph, "$(B)/f/nested/schema.fbs")
        self.assertIn("$(S)/f/dep.proto", converted["inputs"])
        generated = lib.node_by_output(graph, "$(B)/f/nested/schema.fbs.h")
        self.assertNotIn("$(S)/f/dep.proto", generated["inputs"])
        compiled = next(node for node in graph["graph"] if node["kv"].get("p") == "CC" and node["inputs"][0] == "$(B)/f/nested/schema.fbs.cpp")
        self.assertIn("$(B)/f/dep.pb.h", compiled["inputs"])
        self.assertIn(lib.node_by_output(graph, "$(B)/f/dep.pb.h")["uid"], compiled["deps"])

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
        self.assertEqual(fl["inputs"], [
            FLATC,
            WRAPPER,
            '$(B)/f/gen.fbs',
            '$(S)/f/schema.in',
        ])
        copy = lib.node_by_output(graph, "$(B)/f/gen.fbs")
        self.assertIn(copy["uid"], fl["deps"])
        compile_node = lib.node_by_output(graph, "$(B)/f/gen.fbs.cpp.o")
        self.assertIn("$(B)/f/gen.fbs.h", compile_node["inputs"])
        self.assertNotIn("$(S)/f/gen.fbs", compile_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
