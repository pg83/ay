import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PR_KV = {"p": "PR", "pc": "yellow", "show_out": "yes"}


def library(body, **sources):
    files = {"lib/ya.make": f"LIBRARY()\n{BARE}{body}END()\n"}
    files.update({f"lib/{name.replace('__', '.')}": text for name, text in sources.items()})
    lib.tool_program(files, "tools/gen", "gen")
    return files


def uid(graph, output):
    return lib.node_by_output(graph, output)["uid"]


def compile_inputs(graph, output):
    inputs = lib.node_by_output(graph, output)["inputs"]
    return [inputs[0], sorted(inputs[1:])]


class RunProgramTest(unittest.TestCase):
    def test_arguments_inputs_env_and_tools(self):
        files = library(
            "RUN_PROGRAM(tools/pre IN seed.txt STDOUT_NOAUTO stage.txt)\n"
            "RUN_PROGRAM(\n"
            "  tools/gen in.txt --out=out.h ain.txt prefix/in.txt x_in.txt\n"
            "  .in.txt -in.txt k:in.txt tools/aux stage.txt\n"
            "  IN in.txt ${ARCADIA_ROOT}/lib/abs.txt shared/root.txt\n"
            "  IN_NOPARSE np.txt stage.txt\n"
            "  OUT out.h out.h\n"
            "  OUT_NOAUTO na.bin\n"
            "  TOOL tools/aux tools/aux ${ARCADIA_ROOT}/tools/aux2 ${ARCADIA_ROOT}/tools/aux\n"
            "  ENV A=1 B C=x=y\n"
            "  CWD ${ARCADIA_BUILD_ROOT}/lib\n"
            ")\n"
            "SRCS(user.cpp)\n",
            user__cpp='#include "out.h"\n',
            in__txt="", abs__txt="", np__txt="", seed__txt="",
        )
        files["shared/root.txt"] = ""
        for path in ("tools/aux", "tools/aux2", "tools/pre"):
            lib.tool_program(files, path, path.split("/")[-1])
        graph = lib.make(files, "lib")
        node = lib.node_by_output(graph, "$(B)/lib/out.h")
        self.assertEqual(node["kv"], PR_KV)
        self.assertEqual(node["outputs"], ["$(B)/lib/out.h", "$(B)/lib/na.bin"])
        [cmd] = node["cmds"]
        self.assertEqual(cmd["cmd_args"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/in.txt",
            "--out=$(B)/lib/out.h",
            "ain.txt",
            "prefix/in.txt",
            "x_in.txt",
            ".in.txt",
            "-in.txt",
            "k:$(S)/lib/in.txt",
            "$(B)/tools/aux/aux",
            "$(B)/lib/stage.txt",
        ])
        self.assertEqual(cmd["cwd"], "$(B)/lib")
        self.assertEqual(cmd["env"], {
            "ARCADIA_ROOT_DISTBUILD": "$(S)",
            "A": "1",
            "B": "",
            "C": "x=y",
        })
        self.assertNotIn("stdout", cmd)
        self.assertEqual(node["inputs"], [
            "$(B)/tools/gen/gen",
            "$(B)/tools/aux/aux",
            "$(B)/tools/aux2/aux2",
            "$(S)/lib/in.txt",
            "$(S)/lib/abs.txt",
            "$(S)/shared/root.txt",
            "$(S)/lib/np.txt",
            "$(B)/lib/stage.txt",
            "$(S)/lib/seed.txt",
        ])
        tools = {
            uid(graph, "$(B)/tools/gen/gen"),
            uid(graph, "$(B)/tools/aux/aux"),
            uid(graph, "$(B)/tools/aux2/aux2"),
        }
        self.assertEqual(set(node["foreign_deps"]["tool"]), tools)
        self.assertEqual(
            set(node["deps"]),
            tools | {uid(graph, "$(B)/lib/stage.txt")},
        )
        stage = lib.node_by_output(graph, "$(B)/lib/stage.txt")
        self.assertEqual(stage["cmds"][0]["stdout"], "$(B)/lib/stage.txt")

    def test_stdout_source_is_compiled_with_input_closure(self):
        graph = lib.make(library(
            "RUN_PROGRAM(tools/gen IN spec.h STDOUT c.cpp OUTPUT_INCLUDES lib/dep.h)\n",
            spec__h='#include "dep.h"\n',
            dep__h='#include "dep2.h"\n',
            dep2__h="",
        ), "lib")
        node = lib.node_by_output(graph, "$(B)/lib/c.cpp")
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/gen/gen"])
        self.assertEqual(node["cmds"][0]["stdout"], "$(B)/lib/c.cpp")
        self.assertEqual(node["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/spec.h",
            "$(S)/lib/dep.h",
            "$(S)/lib/dep2.h",
        ])
        compile_node = lib.node_by_output(graph, "$(B)/lib/c.cpp.o")
        self.assertEqual(compile_inputs(graph, "$(B)/lib/c.cpp.o"), [
            "$(B)/lib/c.cpp",
            ["$(S)/lib/dep.h", "$(S)/lib/dep2.h", "$(S)/lib/spec.h"],
        ])
        self.assertIn(node["uid"], compile_node["deps"])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][0], "$(B)/lib/c.cpp.o")

    def test_outputs_without_inputs_use_generated_header_closure(self):
        graph = lib.make(library(
            "RUN_PROGRAM(tools/gen OUT gen.h gen.cpp)\n"
            "RUN_PROGRAM(tools/gen STDOUT so.cpp"
            " OUTPUT_INCLUDES lib/dep.h ${ARCADIA_BUILD_ROOT}/lib/gen.h)\n"
            "RUN_PROGRAM(tools/gen OUT hdr.h notes.txt OUTPUT_INCLUDES lib/dep.h)\n"
            "SRCS(user.cpp)\n",
            user__cpp='#include "hdr.h"\n',
            dep__h='#include "dep2.h"\n',
            dep2__h="",
        ), "lib")
        pair = lib.node_by_output(graph, "$(B)/lib/gen.h")
        self.assertEqual(pair["outputs"], ["$(B)/lib/gen.h", "$(B)/lib/gen.cpp"])
        self.assertEqual(pair["inputs"], ["$(B)/tools/gen/gen"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/lib/gen.cpp.o")["inputs"],
            ["$(B)/lib/gen.cpp", "$(B)/lib/gen.h"],
        )

        stdout_node = lib.node_by_output(graph, "$(B)/lib/so.cpp")
        self.assertEqual(stdout_node["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/dep.h",
            "$(S)/lib/dep2.h",
        ])
        self.assertEqual(compile_inputs(graph, "$(B)/lib/so.cpp.o"), [
            "$(B)/lib/so.cpp",
            ["$(B)/lib/gen.h", "$(S)/lib/dep.h", "$(S)/lib/dep2.h"],
        ])

        header = lib.node_by_output(graph, "$(B)/lib/hdr.h")
        self.assertEqual(header["outputs"], ["$(B)/lib/hdr.h", "$(B)/lib/notes.txt"])
        self.assertEqual(header["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/dep.h",
            "$(S)/lib/dep2.h",
        ])
        user = lib.node_by_output(graph, "$(B)/lib/user.cpp.o")
        self.assertEqual(compile_inputs(graph, "$(B)/lib/user.cpp.o"), [
            "$(S)/lib/user.cpp",
            ["$(B)/lib/hdr.h", "$(S)/lib/dep.h", "$(S)/lib/dep2.h"],
        ])
        self.assertEqual(user["deps"], [header["uid"]])

    def test_generated_inputs_bring_their_sources(self):
        graph = lib.make(library(
            "RUN_PROGRAM(tools/gen IN seed.txt OUT_NOAUTO gen_in.h data.txt"
            " OUTPUT_INCLUDES lib/dep.h)\n"
            "RUN_PROGRAM(tools/gen IN gen_in.h spec.h data.txt OUT a.cpp"
            " OUTPUT_INCLUDES ${ARCADIA_BUILD_ROOT}/lib/gen_in.h lib/dep.h)\n",
            seed__txt="",
            spec__h='#include "gen_in.h"\n#include "dep.h"\n',
            dep__h='#include "dep2.h"\n',
            dep2__h="",
        ), "lib")
        producer = lib.node_by_output(graph, "$(B)/lib/gen_in.h")
        node = lib.node_by_output(graph, "$(B)/lib/a.cpp")
        self.assertEqual(node["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/spec.h",
            "$(B)/lib/gen_in.h",
            "$(B)/lib/data.txt",
            "$(S)/lib/seed.txt",
            "$(S)/lib/dep2.h",
            "$(S)/lib/dep.h",
        ])
        self.assertIn(producer["uid"], node["deps"])
        self.assertEqual(compile_inputs(graph, "$(B)/lib/a.cpp.o"), [
            "$(B)/lib/a.cpp",
            [
                "$(B)/lib/gen_in.h",
                "$(S)/lib/dep.h",
                "$(S)/lib/dep2.h",
                "$(S)/lib/seed.txt",
                "$(S)/lib/spec.h",
            ],
        ])

    def test_proto_inputs_and_pb_h_siblings(self):
        files = library(
            "RUN_PROGRAM(tools/gen IN seed.txt OUT_NOAUTO g.h x.pb.h"
            " OUTPUT_INCLUDES lib/api.proto)\n"
            "RUN_PROGRAM(tools/gen IN x.txt x.pb.h OUT_NOAUTO out1.txt out2.txt"
            " OUTPUT_INCLUDES ${ARCADIA_BUILD_ROOT}/lib/g.h lib/src_only.h)\n"
            "RUN_PROGRAM(tools/gen IN api.proto OUT_NOAUTO p.h"
            " OUTPUT_INCLUDES lib/api.pb.h)\n"
            "RESOURCE(${BINDIR}/out1.txt k1 ${BINDIR}/p.h k2)\n",
            seed__txt="",
            x__txt="",
            src_only__h="",
            api__proto='syntax = "proto3";\nimport "lib/other.proto";\n',
            other__proto='syntax = "proto3";\n',
            api__pb__h="",
            other__pb__h="",
        )
        files["library/cpp/resource/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "lib")
        pair = lib.node_by_output(graph, "$(B)/lib/out1.txt")
        self.assertEqual(pair["outputs"], ["$(B)/lib/out1.txt", "$(B)/lib/out2.txt"])
        self.assertEqual(pair["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/x.txt",
            "$(B)/lib/x.pb.h",
            "$(S)/lib/other.proto",
            "$(S)/lib/seed.txt",
            "$(S)/lib/api.proto",
            "$(S)/lib/other.pb.h",
            "$(S)/lib/api.pb.h",
            "$(B)/lib/g.h",
        ])
        self.assertIn(uid(graph, "$(B)/lib/g.h"), pair["deps"])

        proto_node = lib.node_by_output(graph, "$(B)/lib/p.h")
        self.assertEqual(proto_node["inputs"], [
            "$(B)/tools/gen/gen",
            "$(S)/lib/api.proto",
            "$(S)/lib/other.proto",
            "$(S)/lib/api.pb.h",
        ])
        objcopy = lib.node_by_output_prefix(graph, "$(B)/lib/objcopy_")
        self.assertEqual(objcopy["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(B)/lib/out1.txt",
            "$(B)/lib/p.h",
            "$(S)/build/scripts/objcopy.py",
            "$(B)/lib/g.h",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
