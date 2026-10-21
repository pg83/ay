import unittest

import lib
from test_python_srcs import python_base


LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def cmd(node):
    return node["cmds"][0]


class RunPython3Test(unittest.TestCase):
    def test_auxiliary_tools_use_host_producers_and_induce_native_dependencies(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD + "SET(TOOLS tools/aux tools/aux)\n"
                "RUN_PYTHON3(gen.py --tool tools/aux --embedded=tools/aux TOOL $TOOLS OUT generated.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "auxlib/ya.make": LIBRARY_HEAD + "SRCS(helper.cpp)\nEND()\n",
            "auxlib/helper.cpp": "",
            "runtime/runtime.h": '#include "detail.h"\n',
            "runtime/detail.h": "",
        }
        lib.tool_program(files, "tools/aux", "aux")
        files["tools/aux/ya.make"] = files["tools/aux/ya.make"].replace(
            "END()", "PEERDIR(auxlib)\nINDUCED_DEPS(cpp ${ARCADIA_ROOT}/runtime/runtime.h)\nEND()"
        )
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/generated.cpp")
        tool = lib.node_by_output(graph, "$(B)/tools/aux/aux")
        self.assertEqual(cmd(generated)["cmd_args"][1:], [
            "$(S)/lib/gen.py", "--tool", "$(B)/tools/aux/aux", "--embedded=$(B)/tools/aux/aux",
        ])
        self.assertEqual(generated["inputs"].count("$(B)/tools/aux/aux"), 1)
        self.assertEqual(generated["foreign_deps"], {"tool": [tool["uid"]]})
        self.assertEqual(tool["platform"], "default-linux-x86_64")
        helpers = [n for n in graph["graph"] if "$(B)/auxlib/helper.cpp.pic.o" in n["outputs"]]
        self.assertEqual(len(helpers), 1)
        compiled = lib.node_by_output(graph, "$(B)/lib/generated.cpp.o")
        for header in ("runtime/runtime.h", "runtime/detail.h"):
            self.assertIn("$(S)/" + header, compiled["inputs"])
            self.assertNotIn("$(S)/" + header, generated["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_generated_header_consumers_do_not_take_an_arbitrary_cpp_sibling(self):
        for outputs in ("main.h other.cpp public.h", "other.cpp main.h public.h"):
            with self.subTest(outputs=outputs):
                files = {
                    "lib/ya.make": LIBRARY_HEAD + (
                        f"RUN_PYTHON3(gen.py --header main.h OUT_NOAUTO {outputs} "
                        "OUTPUT_INCLUDES lib/public.h lib/dep.h)\n"
                        "SRCS(other.cpp use.cpp)\nEND()\n"
                    ),
                    "lib/gen.py": "",
                    "lib/use.cpp": '#include "lib/public.h"\n',
                    "lib/dep.h": "",
                }
                graph = lib.make(files, "lib")
                generated = lib.node_by_output(graph, "$(B)/lib/public.h")
                self.assertEqual(generated["outputs"][0], "$(B)/lib/main.h")
                use = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
                self.assertEqual(set(use["inputs"]), {
                    "$(S)/lib/use.cpp", "$(S)/lib/dep.h",
                    "$(B)/lib/main.h", "$(B)/lib/public.h",
                })
                self.assertIn(generated["uid"], use["deps"])
                other = lib.node_by_output(graph, "$(B)/lib/other.cpp.o")
                self.assertIn("$(B)/lib/main.h", other["inputs"])

    def test_additional_cpp_output_keeps_the_discovered_main_header(self):
        for out_order in ("generated.h generated.cpp", "generated.cpp generated.h"):
            with self.subTest(out_order=out_order):
                files = {
                    "lib/ya.make": LIBRARY_HEAD + (
                        f"RUN_PYTHON3(gen.py --header generated.h --cpp generated.cpp OUT {out_order})\n"
                        "END()\n"
                    ),
                    "lib/gen.py": "",
                }
                graph = lib.make(files, "lib")
                generated = lib.node_by_output(graph, "$(B)/lib/generated.cpp")
                self.assertEqual("$(B)/lib/generated.h", generated["outputs"][0])
                compiled = lib.node_by_output(graph, "$(B)/lib/generated.cpp.o")
                self.assertIn("$(B)/lib/generated.h", compiled["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])

    def test_arguments_expand_quoted_configuration_values(self):
        files = {
            "build/ymake_conf.py": lib.ymake_conf(extra='SCRIPT_ARG="two words"\n'),
            "lib/ya.make": LIBRARY_HEAD + (
                "RUN_PYTHON3(gen.py $SCRIPT_ARG OUT first.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib", "--sandboxing")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        self.assertEqual(cmd(run)["cmd_args"][2:], ["two words"])

    def test_object_outputs_become_archive_members(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + "RUN_PYTHON3(gen.py OUT first.o second.o OUT_NOAUTO unused.o)\nEND()\n",
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/first.o")
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][:2], ["$(B)/lib/first.o", "$(B)/lib/second.o"])
        self.assertIn(generated["uid"], archive["deps"])
        self.assertEqual(archive["deps"].count(generated["uid"]), 1)
        self.assertNotIn("$(B)/lib/unused.o", archive["inputs"])

    def test_outputs_follow_command_discovery_order(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + (
                "RUN_PYTHON3(gen.py third.o first.o "
                "OUT first.o second.o third.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        expected = ["$(B)/lib/" + name for name in ("third.o", "first.o", "second.o")]
        self.assertEqual(run["outputs"], expected)
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["cmds"][0]["cmd_args"][-3:], expected)

    def test_compiler_and_resource_globals_in_arguments(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + (
                "DECLARE_EXTERNAL_RESOURCE(SDK sbr:123)\n"
                "RUN_PYTHON3(gen.py $CXX_COMPILER --sdk $SDK_RESOURCE_GLOBAL "
                "OUT first.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        self.assertEqual(cmd(run)["cmd_args"][2:], [
            "$(B)/resources/CLANG20/bin/clang++", "--sdk", "$(B)/resources/SDK",
        ])
        lib.node_by_output(graph, "$(B)/resources/SDK")

    def test_sections_shape_command_env_and_outputs(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(\n"
                "  gen.py ${ARCADIA_ROOT} --flag in.txt out=in.txt mode=fast gen.h -o\n"
                "  IN in.txt\n"
                "  OUT gen.h gen.cpp data.bin\n"
                "  OUT_NOAUTO extra.h\n"
                "  STDOUT log.cpp\n"
                "  ENV A=1 B\n"
                "  CWD ${ARCADIA_BUILD_ROOT}\n"
                "  OUTPUT_INCLUDES lib/dep.h ${ARCADIA_ROOT}/lib/dep2.h\n"
                ")\n"
                "SRCS(use.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/in.txt": "",
            "lib/dep.h": "",
            "lib/dep2.h": "",
            "lib/use.cpp": '#include "lib/gen.h"\n#include "lib/extra.h"\nint u(){return 0;}\n',
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
        self.assertEqual(run["kv"]["p"], "PY")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/gen.h", "$(B)/lib/gen.cpp", "$(B)/lib/data.bin",
            "$(B)/lib/extra.h", "$(B)/lib/log.cpp",
        ])
        self.assertEqual(cmd(run)["cmd_args"][1:], [
            "$(S)/lib/gen.py", "$(S)", "--flag", "$(S)/lib/in.txt",
            "out=$(S)/lib/in.txt", "mode=fast", "$(B)/lib/gen.h", "-o",
        ])
        self.assertEqual(cmd(run)["cwd"], "$(B)")
        self.assertEqual(cmd(run)["stdout"], "$(B)/lib/log.cpp")
        self.assertEqual(cmd(run)["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)", "A": "1", "B": ""})
        self.assertEqual(run["env"], cmd(run)["env"])
        for expected in ('$(S)/lib/gen.py', '$(S)/lib/in.txt',):
            self.assertIn(expected, run["inputs"])
        self.assertNotIn('$(S)/lib/dep.h', run['inputs'])
        self.assertNotIn('$(S)/lib/dep2.h', run['inputs'])

        for output in ("gen.cpp", "log.cpp"):
            compiled = lib.node_by_output(graph, f"$(B)/lib/{output}.o")
            self.assertIn(f"$(B)/lib/{output}", compiled["inputs"])
            self.assertIn("$(S)/lib/dep2.h", compiled["inputs"])
            self.assertIn(run["uid"], compiled["deps"])
        use = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        for expected in ('$(B)/lib/gen.h', '$(B)/lib/extra.h', '$(S)/lib/dep.h',):
            self.assertIn(expected, use["inputs"])
        self.assertNotIn('$(S)/lib/in.txt', use['inputs'])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][:3], [
            "$(B)/lib/use.cpp.o", "$(B)/lib/gen.cpp.o", "$(B)/lib/log.cpp.o",
        ])

    def test_stdout_output_keeps_command_discovery_order(self):
        for args, names in (
            ("build.log generated.cpp", ("build.log", "generated.cpp")),
            ("generated.cpp build.log", ("generated.cpp", "build.log")),
        ):
            with self.subTest(args=args):
                files = {
                    "lib/ya.make": (
                        LIBRARY_HEAD + "RUN_PYTHON3(gen.py " + args
                        + " STDOUT_NOAUTO build.log OUT generated.cpp)\nEND()\n"
                    ),
                    "lib/gen.py": "",
                }
                graph = lib.make(files, "lib", "--sandboxing")
                run = lib.node_by_output(graph, "$(B)/lib/generated.cpp")
                self.assertEqual(run["outputs"], ["$(B)/lib/" + name for name in names])
                self.assertEqual(cmd(run)["cmd_args"][2:], run["outputs"])
                self.assertEqual(cmd(run)["stdout"], "$(B)/lib/build.log")

    def test_generated_python_uses_declared_output_before_stdout(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "RUN_PYTHON3(gen.py STDOUT_NOAUTO build.log OUT_NOAUTO project.py)\n"
                "PY_SRCS(project.py)\nEND()\n"
            ),
            "lib/gen.py": "",
        })
        graph = lib.make(files, "lib", "--sandboxing")
        run = lib.node_by_output(graph, "$(B)/lib/project.py")
        self.assertEqual(run["outputs"], ["$(B)/lib/project.py", "$(B)/lib/build.log"])
        self.assertEqual(cmd(run)["stdout"], "$(B)/lib/build.log")
        bytecode = lib.node_by_output(graph, "$(B)/lib/project.py.yapyc3")
        resource = next(n for n in graph["graph"] if any("/objcopy_" in o for o in n["outputs"]))
        for consumer in (bytecode, resource):
            self.assertIn("$(B)/lib/project.py", consumer["inputs"])
            self.assertNotIn("$(B)/lib/build.log", consumer["inputs"])
            self.assertIn(run["uid"], consumer["deps"])

    def test_generated_script_and_stdout_noauto(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "COPY_FILE(script.txt script.py)\n"
                "RUN_PYTHON3(${BINDIR}/script.py STDOUT_NOAUTO quiet.cpp OUT data.cpp data.bin)\n"
                "END()\n"
            ),
            "lib/script.txt": "",
        }
        graph = lib.make(files, "lib")
        copy = lib.node_by_output(graph, "$(B)/lib/script.py")
        run = lib.node_by_output(graph, "$(B)/lib/data.cpp")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/data.cpp", "$(B)/lib/data.bin", "$(B)/lib/quiet.cpp",
        ])
        self.assertEqual(cmd(run)["cmd_args"][1:], ["$(B)/lib/script.py"])
        self.assertEqual(cmd(run)["stdout"], "$(B)/lib/quiet.cpp")
        self.assertIn("$(B)/lib/script.py", run["inputs"])
        self.assertIn(copy["uid"], run["deps"])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][0], "$(B)/lib/data.cpp.o")
        self.assertNotIn("$(B)/lib/quiet.cpp.o", archive["inputs"])

    def test_split_outputs_include_generator_induced_sources(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "BASE_CODEGEN(tool proto)\n"
                "RUN_PYTHON3(split.py IN ${BINDIR}/proto.cpp OUT_NOAUTO shard0.cpp shard1.cpp shard.h)\n"
                "SRCS(shard0.cpp shard1.cpp)\n"
                "END()\n"
            ),
            "lib/split.py": "",
            "lib/proto.in": "",
            "lib/induced.h": "",
            "tool/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/lib/induced.h)\n"
                "SRCS(main.cpp)\n"
                "END()\n"
            ),
            "tool/main.cpp": "int main(){return 0;}\n",
        }
        graph = lib.make(files, "lib")
        codegen = lib.node_by_output(graph, "$(B)/lib/proto.cpp")
        run = lib.node_by_output(graph, "$(B)/lib/shard0.cpp")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/shard0.cpp", "$(B)/lib/shard1.cpp", "$(B)/lib/shard.h",
        ])
        self.assertIn("$(B)/lib/proto.cpp", run["inputs"])
        self.assertNotIn("$(S)/lib/proto.in", run["inputs"])
        self.assertIn(codegen["uid"], run["deps"])
        first = lib.node_by_output(graph, "$(B)/lib/shard0.cpp.o")
        self.assertEqual(lib.bucketed(first["inputs"], 1), ['$(B)/lib/shard0.cpp', '$(S)/lib/induced.h'])
        second = lib.node_by_output(graph, "$(B)/lib/shard1.cpp.o")
        self.assertEqual(lib.bucketed(second["inputs"], 1), [
            '$(B)/lib/shard1.cpp',
            '$(B)/lib/shard0.cpp',
            '$(S)/lib/induced.h',
        ])

    def test_split_outputs_take_output_includes(self):
        # OUTPUT_INCLUDES reach every output (model/action_graph.cpp
        # addOutputIncludes), shards included: ydb
        # yql/essentials/parser/proto_ast/gen/v1_proto_split_antlr4 compiles
        # SQLv1Antlr4Parser.pb.code0.cc and every includer of pb.main.h with
        # SQLv1Antlr4Parser.pb.h and .pb.cc.
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(split.py OUT_NOAUTO s0.cc s1.cc main.h OUTPUT_INCLUDES lib/dep.h)\n"
                "SRCS(s0.cc s1.cc use.cpp)\nEND()\n"
            ),
            "lib/split.py": "",
            "lib/dep.h": "",
            "lib/use.cpp": '#include "lib/main.h"\n',
        }
        graph = lib.make(files, "lib")
        for output in ("$(B)/lib/s0.cc.o", "$(B)/lib/s1.cc.o", "$(B)/lib/use.cpp.o"):
            self.assertIn("$(S)/lib/dep.h", lib.node_by_output(graph, output)["inputs"], output)

    def test_script_text_naming_own_output_is_not_an_include(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "DECLARE_IN_DIRS(CONF *.toml DIRS conf)\n"
                "RUN_PYTHON3(\n"
                "  gen.py --header gen.h --cpp gen.cpp \\${CONF_FILES}\n"
                "  IN ${CONF_FILES}\n"
                "  OUT gen.h gen.cpp\n"
                "  OUTPUT_INCLUDES lib/dep.h\n"
                ")\n"
                "SRCS(gen.h)\n"
                "END()\n"
            ),
            "lib/gen.py": 'CPP = """\n#include "gen.h"\n"""\n',
            "lib/conf/a.toml": "",
            "lib/conf/b.toml": "",
            "lib/dep.h": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.h")
        self.assertEqual(run["outputs"], ["$(B)/lib/gen.h", "$(B)/lib/gen.cpp"])
        self.assertEqual(cmd(run)["cmd_args"][1:], [
            "$(S)/lib/gen.py", "--header", "$(B)/lib/gen.h", "--cpp", "$(B)/lib/gen.cpp",
            "$(S)/lib/conf/a.toml", "$(S)/lib/conf/b.toml",
        ])
        self.assertEqual(run["deps"], [])
        self.assertEqual(sorted(set(run["inputs"])), [
            '$(S)/lib/conf/a.toml',
            '$(S)/lib/conf/b.toml',
            '$(S)/lib/gen.py',
        ])
        compiled = lib.node_by_output(graph, "$(B)/lib/gen.cpp.o")
        self.assertEqual(compiled["deps"], [run["uid"]])

    def test_inputs_with_empty_parser_extensions_are_not_scanned(self):
        extensions = ("fml", "fml2", "fml3", "pln", "info", "a", "lua", "sh", "bin", "py", "pyi", "pysrc")
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(gen.py IN "
                + " ".join(f"in.{ext}" for ext in extensions)
                + " OUT gen.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/dep.h": "",
        }
        for ext in extensions:
            files[f"lib/in.{ext}"] = '#include "lib/dep.h"\n'
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
        compiled = lib.node_by_output(graph, "$(B)/lib/gen.cpp.o")
        for ext in extensions:
            self.assertIn(f"$(S)/lib/in.{ext}", run["inputs"])
        self.assertNotIn("$(S)/lib/dep.h", run["inputs"])
        self.assertNotIn("$(S)/lib/dep.h", compiled["inputs"])

    def test_js_imports_are_processed_only_in_ts_modules(self):
        # include_processors/ts_processor.cpp ProcessIncludes handles imports
        # only for modules tagged TS; ydb html_log feeds graphlib.js, whose
        # require('./lib') resolves nowhere, to RUN_PYTHON3 in a LIBRARY and the
        # reference lists only the named .js inputs.
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(cat.py IN a.js b.ts STDOUT out.cpp)\n"
                "END()\n"
            ),
            "lib/cat.py": "",
            "lib/a.js": "var x = require('./lib');\nvar y = require('./dep');\n",
            "lib/b.ts": "import { z } from './dep';\n",
            "lib/dep.js": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/out.cpp")
        self.assertEqual(sorted({i for i in run["inputs"] if i.startswith("$(S)/lib/")}), [
            "$(S)/lib/a.js", "$(S)/lib/b.ts", "$(S)/lib/cat.py",
        ])

    def test_in_noparse_files_are_text_inputs(self):
        # build/ymake.core.conf passes IN_NOPARSE as ${hide;input=TEXT:...}:
        # middlesearch yabs/models_services/feature_store/mappers/argus/py
        # lists argus.h, whose include exists nowhere in the slice, alone.
        for macro in ("RUN_PROGRAM(tool", "RUN_PYTHON3(gen.py"):
            with self.subTest(macro=macro):
                files = {
                    "lib/ya.make": (
                        LIBRARY_HEAD
                        + "SET(HDRS lib/a.h lib/b.h)\n"
                        + macro + " IN c.h IN_NOPARSE ${HDRS} IN_DEPS d.h OUT gen.cpp)\n"
                        "END()\n"
                    ),
                    "lib/gen.py": "",
                    "lib/a.h": '#include "nowhere/missing.h"\n',
                    "lib/b.h": '#include "lib/dep.h"\n',
                    "lib/c.h": '#include "lib/dep2.h"\n',
                    "lib/d.h": "",
                    "lib/dep.h": "",
                    "lib/dep2.h": "",
                }
                lib.tool_program(files, "tool", "tool")
                graph = lib.make(files, "lib", "-k")
                run = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
                inputs = {i for i in run["inputs"] if i.startswith("$(S)/lib/")}
                self.assertEqual(inputs - {"$(S)/lib/gen.py"}, {
                    "$(S)/lib/a.h", "$(S)/lib/b.h", "$(S)/lib/c.h", "$(S)/lib/d.h", "$(S)/lib/dep2.h",
                })

    def test_arcadia_root_left_by_expansion_limit_maps_to_source_root(self):
        # Each expansion pass consumes one "O1}" suffix, so the literal
        # ${ARCADIA_ROOT} only appears after the last of the eight passes.
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "SET(O1 \\${)\n"
                "RUN_PYTHON3(gen.py ${O1}O1}O1}O1}O1}O1}O1}O1}ARCADIA_ROOT} OUT x.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/x.cpp")
        self.assertEqual(cmd(run)["cmd_args"][1:], ["$(S)/lib/gen.py", "$(S)"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
