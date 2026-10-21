import json
import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PLATFORM_FLAGS = ["--target=x86_64-linux-gnu", "--sysroot=$(B)/resources/OS_SDK_ROOT", "-B$(B)/resources/OS_SDK_ROOT/usr/bin"]


def world(files):
    return {
        "build/ymake_conf.py": lib.ymake_conf(extra="C_FLAGS_PLATFORM=" + " ".join(PLATFORM_FLAGS) + "\n"),
        "build/platform/flang/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(FLANG resources.json)\nEND()\n",
        "build/platform/flang/resources.json": json.dumps({"by_platform": {
            "linux-x86_64": {"uri": "sbr:11"},
            "linux-aarch64": {"uri": "sbr:12"},
        }}),
        "contrib/libs/llvm/flang-rt/ya.make": "LIBRARY()\n" + BARE + "SRCS(runtime.cpp)\nEND()\n",
        "contrib/libs/llvm/flang-rt/runtime.cpp": "",
        **files,
    }


class FortranTest(unittest.TestCase):
    def test_fortran_command_and_local_include_closure(self):
        graph = lib.make(world({
            "mod/ya.make": "PROGRAM(calc)\n" + BARE + "CFLAGS(-DSHOULD_NOT_APPEAR)\nSRCS(calc.f)\nEND()\n",
            "mod/calc.f": (
                "      include 'local.inc'\n"
                "! include 'comment.inc'\n"
                "c include 'comment2.inc'\n"
                "      INCLUDE 'uppercase.inc'\n"
                '      include "double.inc"\n'
                "      end\n"
            ),
            "mod/local.inc": "include 'nested.inc'\n",
            "mod/nested.inc": "",
        }), "mod")
        node = lib.node_by_output(graph, "$(B)/mod/calc.f.o")
        self.assertEqual(node["kv"], {"p": "FL", "pc": "light-green"})
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/resources/FLANG/bin/flang-new", *PLATFORM_FLAGS,
            "-c", "$(S)/mod/calc.f", "-o", "$(B)/mod/calc.f.o",
        ])
        self.assertEqual(node["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
        self.assertEqual(set(node["inputs"]), {"$(S)/mod/calc.f", "$(S)/mod/local.inc", "$(S)/mod/nested.inc"})
        linked = lib.node_by_output(graph, "$(B)/mod/calc")
        self.assertIn("$(B)/mod/calc.f.o", linked["inputs"])
        runtime = lib.node_by_output(graph, "$(B)/contrib/libs/llvm/flang-rt/liblibs-llvm-flang-rt.a")
        self.assertIn(runtime["outputs"][0], linked["inputs"])
        fetch = lib.node_by_output(graph, "$(B)/resources/FLANG")
        self.assertIn("sbr:11", fetch["cmds"][0]["cmd_args"])

    def test_copied_fortran_sources_enter_the_source_queue(self):
        graph = lib.make(world({
            "mod/ya.make": "DLL(calc)\n" + BARE + "COPY_FILE(AUTO input.f generated.f)\nEND()\n",
            "mod/input.f": "      end\n",
        }), "mod")
        copied = lib.node_by_output(graph, "$(B)/mod/generated.f")
        compiled = lib.node_by_output(graph, "$(B)/mod/generated.f.o")
        self.assertEqual(compiled["cmds"][0]["cmd_args"][-4:], [
            "-c", "$(B)/mod/generated.f", "-o", "$(B)/mod/generated.f.o",
        ])
        self.assertEqual(compiled["inputs"], ["$(B)/mod/generated.f"])
        self.assertIn(copied["uid"], compiled["deps"])
        linked = lib.node_by_output(graph, "$(B)/mod/libcalc.so")
        self.assertIn("$(B)/mod/generated.f.o", linked["inputs"])

    def test_generated_fortran_sources_keep_their_producer(self):
        for declaration in (
            "RUN_PROGRAM(tools/generate OUT_NOAUTO generated.f)\nSRCS(generated.f)",
            "RUN_PROGRAM(tools/generate OUT generated.f)",
            "RUN_PYTHON3(generate.py OUT generated.f)",
            "FROM_ARCHIVE(sources.tar OUT generated.f)",
        ):
            with self.subTest(declaration=declaration):
                files = world({
                    "mod/ya.make": "LIBRARY()\n" + BARE + declaration + "\nEND()\n",
                    "mod/generate.py": "print('      end')\n",
                    "mod/sources.tar": "",
                })
                lib.tool_program(files, "tools/generate", "generate")
                graph = lib.make(files, "mod")
                produced = lib.node_by_output(graph, "$(B)/mod/generated.f")
                compiled = lib.node_by_output(graph, "$(B)/mod/generated.f.o")
                self.assertEqual(compiled["inputs"], ["$(B)/mod/generated.f"])
                self.assertIn(produced["uid"], compiled["deps"])

    def test_multiple_sources_share_the_compiler_resource(self):
        files = world({
            "mod/ya.make": "LIBRARY()\n" + BARE + "PEERDIR(extra)\nSRCS(first.f second.f)\nEND()\n",
            "mod/first.f": "      include\n      end\n",
            "mod/second.f": "      end\n",
            "extra/ya.make": lib.resource_lib("EXTRA", 27),
        })
        graph = lib.make(files, "mod")
        for name in ("first", "second"):
            compiled = lib.node_by_output(graph, f"$(B)/mod/{name}.f.o")
            self.assertEqual(compiled["cmds"][0]["cmd_args"][0], "$(B)/resources/FLANG/bin/flang-new")
            self.assertEqual(compiled["inputs"], [f"$(S)/mod/{name}.f"])

    def test_missing_compiler_resource_reports_an_error(self):
        files = world({
            "mod/ya.make": "LIBRARY()\n" + BARE + "SRCS(calc.f)\nEND()\n",
            "mod/calc.f": "      end\n",
            "build/platform/flang/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        })
        result = lib.make_process(files, "mod")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Fortran compiler resource missing", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_resource_global_flags_and_host_resource_selection(self):
        files = world({
            "mod/ya.make": "LIBRARY()\n" + BARE + "SRCS(calc.f)\nEND()\n",
            "mod/calc.f": "      end\n",
            "build/ymake_conf.py": lib.ymake_conf(extra="C_FLAGS_PLATFORM=-DPLATFORM_ONLY ${FLANG_RESOURCE_GLOBAL}/extra\n"),
        })
        graph = lib.make(files, "mod", "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64")
        node = lib.node_by_output(graph, "$(B)/mod/calc.f.o")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/resources/FLANG/bin/flang-new", "-DPLATFORM_ONLY", "$(B)/resources/FLANG/extra",
            "-c", "$(S)/mod/calc.f", "-o", "$(B)/mod/calc.f.o",
        ])
        fetch = lib.node_by_output(graph, "$(B)/resources/FLANG")
        self.assertIn("sbr:11", fetch["cmds"][0]["cmd_args"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
