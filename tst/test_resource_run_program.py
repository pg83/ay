import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PR_KV = {"p": "PR", "pc": "yellow", "show_out": "yes"}


def library(body, **sources):
    files = {"lib/ya.make": f"LIBRARY()\n{BARE}ADDINCL(${{ARCADIA_BUILD_ROOT}}/lib)\n{body}END()\n"}
    files.update({f"lib/{name.replace('__', '.')}": text for name, text in sources.items()})
    lib.tool_program(files, "tools/gen", "gen")
    return files


def uid(graph, output):
    return lib.node_by_output(graph, output)["uid"]


def compile_inputs(graph, output):
    inputs = lib.node_by_output(graph, output)["inputs"]
    return [inputs[0], sorted(inputs[1:])]


class RunProgramTest(unittest.TestCase):
    def test_assembly_inputs_use_the_assembler_include_namespace(self):
        files = library(
            "ADDINCL(native_inc FOR asm asm_inc)\n"
            "RUN_PROGRAM(tools/gen IN instructions.asm OUT result.cpp)\n",
            instructions__asm='%include "defs.inc"\n',
        )
        files["native_inc/defs.inc"] = ""
        files["asm_inc/defs.inc"] = ""
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/result.cpp")
        self.assertIn("$(S)/lib/instructions.asm", run["inputs"])
        self.assertIn("$(S)/asm_inc/defs.inc", run["inputs"])
        self.assertNotIn("$(S)/native_inc/defs.inc", run["inputs"])

    def test_archive_of_additional_output_keeps_opaque_main_output(self):
        files = library(
            "RUN_PROGRAM(tools/gen IN input.cpp OUT_NOAUTO main.cpp data.bin)\n"
            "ARCHIVE(NAME data.inc DONTCOMPRESS data.bin data.bin)\n",
            input__cpp='#include "private.h"\n', private__h="",
        )
        lib.tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "lib")
        archive = lib.node_by_output(graph, "$(B)/lib/data.inc")
        self.assertCountEqual(archive["inputs"], [
            "$(B)/lib/data.bin", "$(B)/lib/main.cpp",
            "$(B)/tools/archiver/archiver",
        ])
        producer = lib.node_by_output(graph, "$(B)/lib/data.bin")
        self.assertIn(producer["uid"], archive["deps"])
        self.assertNotIn("$(B)/lib/main.cpp", archive["cmds"][0]["cmd_args"])

    def test_swig_flags_follow_cpp_source_name_for_any_generator(self):
        files = library("RUN_PROGRAM(tools/gen OUT api.swg.cxx api.swg.cpp api.swg.c ordinary.cpp)\n")
        graph = lib.make(files, "lib")
        for name in ("api.swg.cxx", "api.swg.cpp", "api.swg.c", "ordinary.cpp"):
            with self.subTest(name=name):
                args = lib.node_by_output(graph, f"$(B)/lib/{name}.o")["cmds"][0]["cmd_args"]
                self.assertEqual(args.count("-Wno-deprecated-declarations"), int(name.endswith((".swg.cpp", ".swg.cxx"))))

    def test_auxiliary_tools_induce_dependencies_on_generated_sources(self):
        files = library("RUN_PROGRAM(tools/gen TOOL tools/plugin OUT out.cpp)\n")
        lib.tool_program(files, "tools/plugin", "plugin")
        files["tools/plugin/ya.make"] = files["tools/plugin/ya.make"].replace(
            "END()", "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/runtime/runtime.h)\nEND()"
        )
        files["runtime/runtime.h"] = '#include "detail.h"\n'
        files["runtime/detail.h"] = ""
        graph = lib.make(files, "lib")
        compiled = lib.node_by_output(graph, "$(B)/lib/out.cpp.o")
        self.assertIn("$(S)/runtime/runtime.h", compiled["inputs"])
        self.assertIn("$(S)/runtime/detail.h", compiled["inputs"])

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
            '$(B)/tools/gen/gen',
            '$(B)/tools/aux/aux',
            '$(B)/tools/aux2/aux2',
            '$(S)/lib/in.txt',
            '$(S)/lib/abs.txt',
            '$(S)/shared/root.txt',
            '$(S)/lib/np.txt',
            '$(B)/lib/stage.txt',
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
        self.assertEqual(lib.bucketed(node["inputs"], 2), lib.bucketed([
            "$(B)/tools/gen/gen",
            "$(S)/lib/spec.h",
            "$(S)/lib/dep.h",
            "$(S)/lib/dep2.h",
        ], 2))
        compile_node = lib.node_by_output(graph, "$(B)/lib/c.cpp.o")
        self.assertEqual(compile_inputs(graph, "$(B)/lib/c.cpp.o"), [
            "$(B)/lib/c.cpp",
            ["$(S)/lib/dep.h", "$(S)/lib/dep2.h"],
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
        self.assertEqual(stdout_node["inputs"], ['$(B)/tools/gen/gen'])
        self.assertEqual(compile_inputs(graph, "$(B)/lib/so.cpp.o"), [
            "$(B)/lib/so.cpp",
            ["$(B)/lib/gen.h", "$(S)/lib/dep.h", "$(S)/lib/dep2.h"],
        ])

        header = lib.node_by_output(graph, "$(B)/lib/hdr.h")
        self.assertEqual(header["outputs"], ["$(B)/lib/hdr.h", "$(B)/lib/notes.txt"])
        self.assertEqual(lib.bucketed(header["inputs"], 1), ['$(B)/tools/gen/gen'])
        user = lib.node_by_output(graph, "$(B)/lib/user.cpp.o")
        self.assertEqual(compile_inputs(graph, "$(B)/lib/user.cpp.o"), [
            "$(S)/lib/user.cpp",
            ["$(B)/lib/hdr.h", "$(S)/lib/dep.h", "$(S)/lib/dep2.h"],
        ])
        self.assertEqual(user["deps"], [header["uid"]])

    def test_generated_inputs_bring_their_include_closure(self):
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
        self.assertEqual(lib.bucketed(node["inputs"], 4), [
            '$(B)/tools/gen/gen',
            '$(S)/lib/spec.h',
            '$(B)/lib/gen_in.h',
            '$(B)/lib/data.txt',
            '$(S)/lib/dep.h',
            '$(S)/lib/dep2.h',
        ])
        self.assertIn(producer["uid"], node["deps"])
        self.assertEqual(compile_inputs(graph, "$(B)/lib/a.cpp.o"), [
            "$(B)/lib/a.cpp",
            [
                "$(B)/lib/gen_in.h",
                "$(S)/lib/dep.h",
                "$(S)/lib/dep2.h",
            ],
        ])

    def test_induced_deps_of_every_tool_reach_the_outputs(self):
        # Each ${tool:...} of the command contributes its INDUCED_DEPS: the
        # yabs library/cpp/yaff/proto/well_known_types outputs of
        # RUN_PROGRAM(protoc ... TOOL library/cpp/yaff/tools/protoc_plugin)
        # compile with the yaff runtime induced by the plugin.
        files = library(
            "RUN_PROGRAM(tools/gen TOOL tools/plugin OUT a.cpp)\n",
        )
        files["tools/plugin/ya.make"] = (
            f"PROGRAM(plugin)\n{BARE}INDUCED_DEPS(h+cpp ${{ARCADIA_ROOT}}/inc/runtime.h)\nSRCS(main.cpp)\nEND()\n"
        )
        files["tools/plugin/main.cpp"] = "int main(){return 0;}\n"
        files["inc/runtime.h"] = ""
        graph = lib.make(files, "lib")
        self.assertIn("$(S)/inc/runtime.h", lib.node_by_output(graph, "$(B)/lib/a.cpp.o")["inputs"])

    def test_induced_deps_follow_the_output_extension(self):
        # parser_manager.cpp IndDepsRuleByPath picks the induced deps rule of
        # an output by its extension, without a default: basesearch
        # contrib/libs/cel-cpp feeds the protoc-made empty_descriptor_set_embed.pb
        # to cel_cc_embed, whose node lists the tool and the .pb only.
        files = library(
            "RUN_PROGRAM(tools/plugin IN seed.txt OUT_NOAUTO d.pb x.h)\n"
            "RUN_PROGRAM(tools/gen IN ${BINDIR}/d.pb OUT_NOAUTO e.inc)\n"
            "RUN_PROGRAM(tools/gen IN ${BINDIR}/e.inc OUT a.cpp)\n",
            seed__txt="",
            use__cpp='#include "lib/x.h"\n',
        )
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()", "SRCS(use.cpp)\nEND()")
        files["tools/plugin/ya.make"] = (
            f"PROGRAM(plugin)\n{BARE}INDUCED_DEPS(h+cpp ${{ARCADIA_ROOT}}/inc/runtime.h)\nSRCS(main.cpp)\nEND()\n"
        )
        files["tools/plugin/main.cpp"] = "int main(){return 0;}\n"
        files["inc/runtime.h"] = ""
        graph = lib.make(files, "lib")
        self.assertEqual(sorted(set(lib.node_by_output(graph, "$(B)/lib/e.inc")["inputs"])), [
            "$(B)/lib/d.pb", "$(B)/tools/gen/gen",
        ])
        self.assertIn("$(S)/inc/runtime.h", lib.node_by_output(graph, "$(B)/lib/use.cpp.o")["inputs"])

    def test_generated_empty_parser_inputs_bring_their_sources(self):
        files = library(
            "RUN_PROGRAM(tools/gen IN seed.txt OUT_NOAUTO main.bin gen.py)\n"
            "RUN_PROGRAM(tools/gen IN ${BINDIR}/main.bin ${BINDIR}/gen.py rule.txt OUT_NOAUTO out.bin)\n"
            "RESOURCE(${BINDIR}/out.bin out)\n",
            seed__txt="",
            rule__txt="",
        )
        files["library/cpp/resource/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "lib")
        producer = lib.node_by_output(graph, "$(B)/lib/main.bin")
        node = lib.node_by_output(graph, "$(B)/lib/out.bin")
        self.assertEqual(sorted(set(node["inputs"])), [
            '$(B)/lib/gen.py',
            '$(B)/lib/main.bin',
            '$(B)/tools/gen/gen',
            '$(S)/lib/rule.txt',
        ])
        self.assertIn(producer["uid"], node["deps"])

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
        self.assertEqual(lib.bucketed(pair["inputs"], 3), [
            '$(B)/tools/gen/gen',
            '$(S)/lib/x.txt',
            '$(B)/lib/x.pb.h',
            '$(B)/lib/g.h',
            '$(S)/lib/api.proto',
            '$(S)/lib/other.proto',
        ])
        self.assertIn(uid(graph, "$(B)/lib/g.h"), pair["deps"])

        proto_node = lib.node_by_output(graph, "$(B)/lib/p.h")
        self.assertEqual(proto_node["inputs"], ['$(B)/tools/gen/gen', '$(S)/lib/api.proto', '$(S)/lib/other.proto'])
        objcopy = lib.node_by_output_prefix(graph, "$(B)/lib/objcopy_")
        self.assertCountEqual(objcopy["inputs"], [
            '$(B)/tools/rescompiler/rescompiler',
            '$(B)/tools/rescompressor/rescompressor',
            '$(B)/lib/out1.txt',
            '$(B)/lib/p.h',
            '$(S)/build/scripts/objcopy.py',
            '$(B)/lib/g.h',
            '$(S)/lib/other.proto',
            '$(S)/lib/api.proto',
            '$(S)/lib/src_only.h',
            '$(S)/lib/other.pb.h',
            '$(S)/lib/api.pb.h',
        ])

    def test_proto_input_imports_reach_every_output(self):
        # yabs yabs/server/libs/constant/generated: RUN_PROGRAM(... IN
        # sys_const.proto OUT sys_const.h sys_const.cpp); the compile of the
        # secondary sys_const.cpp takes const_options.pb.h, the pb.h of the
        # proto import, with its closure.
        files = library(
            "RUN_PROGRAM(tools/gen IN api.proto OUT x.h x.cpp)\n",
            api__proto='syntax = "proto3";\nimport "lib/other.proto";\n',
            other__proto='syntax = "proto3";\n',
            other__pb__h='#include "lib/dep.h"\n',
            dep__h="",
        )
        graph = lib.make(files, "lib")
        inputs = lib.node_by_output(graph, "$(B)/lib/x.cpp.o")["inputs"]
        for expected in ("$(B)/lib/x.h", "$(S)/lib/other.pb.h", "$(S)/lib/dep.h"):
            self.assertIn(expected, inputs)

    def test_module_dirs_render_in_the_module_that_first_uses_the_output(self):
        files = {
            "gen/ya.make": (
                f"LIBRARY()\n{BARE}"
                "RUN_PROGRAM(\n"
                "  tools/gen --out out.cpp --dir ${BINDIR}/sub --src ${CURDIR}\n"
                "  OUT_NOAUTO out.cpp\n"
                "  ENV DIR=${BINDIR}\n"
                "  CWD ${BINDIR}\n"
                ")\n"
                "END()\n"
            ),
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "PEERDIR(gen)\n"
                "COPY_FILE(AUTO ${ARCADIA_BUILD_ROOT}/gen/out.cpp copy.cpp)\n"
                "END()\n"
            ),
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        node = lib.node_by_output(graph, "$(B)/gen/out.cpp")
        [cmd] = node["cmds"]
        self.assertEqual(cmd["cmd_args"], [
            "$(B)/tools/gen/gen",
            "--out",
            "$(B)/gen/out.cpp",
            "--dir",
            "$(B)/lib/sub",
            "--src",
            "$(S)/lib",
        ])
        self.assertEqual(cmd["cwd"], "$(B)/lib")
        self.assertEqual(cmd["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)", "DIR": "$(B)/lib"})
        self.assertEqual(node["outputs"], ["$(B)/gen/out.cpp"])

    def test_cwd_outside_both_roots_is_rejected(self):
        files = library(
            "RUN_PROGRAM(tools/gen OUT out.h CWD relative/dir)\nSRCS(user.cpp)\n",
            user__cpp='#include "out.h"\n',
        )
        with self.assertRaisesRegex(AssertionError, r'cwdVFS: unexpected cwd "relative/dir"'):
            lib.make(files, "lib")

    def test_root_lazy_module_renders_its_deferred_program(self):
        files = {
            "pkg/ya.make": (
                "UNION(pkg)\n"
                "RUN_PROGRAM(tools/gen --build ${BINDIR} --source ${CURDIR}\n"
                "  OUT_NOAUTO data.txt ENV DIR=${CURDIR} CWD ${BINDIR})\n"
                "END()\n"
            ),
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "pkg")
        producer = lib.only_node_by_kind(graph, "PR")
        self.assertEqual(producer["outputs"], ["$(B)/pkg/data.txt"])
        self.assertEqual(producer["cmds"][0]["cmd_args"], [
            "$(B)/tools/gen/gen", "--build", "$(B)/pkg", "--source", "$(S)/pkg",
        ])
        self.assertEqual(producer["cmds"][0]["cwd"], "$(B)/pkg")
        self.assertEqual(producer["cmds"][0]["env"]["DIR"], "$(S)/pkg")
        module = lib.node_by_output(graph, "$(B)/pkg/pkg.pkg.fake")
        self.assertIn(producer["uid"], module["deps"])
        self.assertIn(module["uid"], graph["result"])

    def test_nested_lazy_modules_restore_the_consumer_frame(self):
        for root_type in ("UNION", "LIBRARY"):
            with self.subTest(root_type=root_type):
                files = {
                    "inner/ya.make": (
                        "UNION(inner)\n"
                        "RUN_PROGRAM(tools/gen ${BINDIR} ${CURDIR}\n"
                        "  OUT_NOAUTO data.txt ENV DIR=${CURDIR} CWD ${BINDIR})\n"
                        "END()\n"
                    ),
                    "outer/ya.make": (
                        f"{root_type}(outer)\n{BARE}"
                        "PEERDIR(inner)\n"
                        "RUN_PROGRAM(tools/gen ${BINDIR} ${CURDIR}\n"
                        "  IN ${ARCADIA_BUILD_ROOT}/inner/inner.pkg.fake\n"
                        "  OUT out.cpp ENV DIR=${CURDIR} CWD ${BINDIR})\n"
                        "END()\n"
                    ),
                }
                lib.tool_program(files, "tools/gen", "gen")
                graph = lib.make(files, "outer")
                producers = {
                    node["outputs"][0]: node for node in graph["graph"]
                    if node.get("kv", {}).get("p") == "PR"
                }
                for module, output in (("inner", "data.txt"), ("outer", "out.cpp")):
                    node = producers[f"$(B)/{module}/{output}"]
                    [cmd] = node["cmds"]
                    self.assertEqual(cmd["cmd_args"], [
                        "$(B)/tools/gen/gen", f"$(B)/{module}", f"$(S)/{module}",
                    ])
                    self.assertEqual(cmd["cwd"], f"$(B)/{module}")
                    self.assertEqual(cmd["env"]["DIR"], f"$(S)/{module}")
                inner = lib.node_by_output(graph, "$(B)/inner/inner.pkg.fake")
                self.assertIn(producers["$(B)/inner/data.txt"]["uid"], inner["deps"])
                self.assertIn(inner["uid"], producers["$(B)/outer/out.cpp"]["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
