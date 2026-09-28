import unittest

import lib


LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
X86_64 = ("--target-platform", "default-linux-x86_64")


def mn_files():
    files = {
        "mod/ya.make": (
            LIBRARY_HEAD + "BUILD_MN(model.info mymodel)\nSRCS(use.cpp)\nEND()\n"
        ),
        "mod/model.info": "",
        "mod/use.cpp": "",
        "kernel/matrixnet/mn_sse.h": "",
        "build/scripts/rodata2asm.py": "",
    }
    lib.tool_program(files, "tools/archiver", "archiver")
    lib.tool_program(files, "contrib/tools/yasm", "yasm")
    return files


class BuildMnTest(unittest.TestCase):
    def test_build_mn_produces_cpp_and_rodata_objects(self):
        graph = lib.make(mn_files(), "mod", *X86_64)
        archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        mn = lib.node_by_output(graph, "$(B)/mod/mn.mymodel.cpp")
        self.assertEqual(mn["kv"], {"p": "MN", "pc": "yellow"})
        self.assertEqual(mn["outputs"], [
            "$(B)/mod/mn.mymodel.cpp", "$(B)/mod/MN_External_mymodel.rodata",
        ])
        self.assertEqual(mn["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/internal/scripts/build_mn.py", "BuildMnF", "$(S)",
            "$(B)/tools/archiver/archiver", "$(S)/mod/model.info", "mymodel",
            "ranking_suffix=", "$(B)/mod/mn.mymodel.cpp",
        ])
        self.assertEqual(mn["inputs"], [
            "$(B)/tools/archiver/archiver",
            "$(S)/build/internal/scripts/build_mn.py",
            "$(S)/mod/model.info",
        ])
        self.assertEqual(mn["foreign_deps"], {"tool": [archiver["uid"]]})

        compile_node = lib.node_by_output(graph, "$(B)/mod/mn.mymodel.cpp.o")
        self.assertIn("$(S)/kernel/matrixnet/mn_sse.h", compile_node["inputs"])

        rodata = lib.node_by_output(graph, "$(B)/mod/MN_External_mymodel.rodata.o")
        self.assertEqual(rodata["inputs"][:3], [
            "$(B)/contrib/tools/yasm/yasm",
            "$(S)/build/scripts/rodata2asm.py",
            "$(B)/mod/MN_External_mymodel.rodata",
        ])
        self.assertEqual(sorted(rodata["inputs"][3:]), [
            "$(B)/mod/mn.mymodel.cpp",
            "$(S)/build/internal/scripts/build_mn.py",
            "$(S)/kernel/matrixnet/mn_sse.h",
            "$(S)/mod/model.info",
        ])
        self.assertIn(mn["uid"], rodata["deps"])
        library = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertEqual(library["inputs"][:3], [
            "$(B)/mod/use.cpp.o",
            "$(B)/mod/mn.mymodel.cpp.o",
            "$(B)/mod/MN_External_mymodel.rodata.o",
        ])

    def test_rodata_on_aarch64_is_an_unsupported_source(self):
        with self.assertRaisesRegex(
            AssertionError,
            r'unsupported-source: unsupported \.rodata platform aarch64 for '
            r'"MN_External_mymodel\.rodata"; source skipped',
        ):
            lib.make(mn_files(), "mod")

        graph = lib.make(mn_files(), "mod", "--keep-going")
        lib.node_by_output(graph, "$(B)/mod/mn.mymodel.cpp")
        self.assertEqual(
            [node for node in graph["graph"] if node["kv"]["p"] == "RD"], [],
        )


class CheckConfigHTest(unittest.TestCase):
    def test_check_config_h_generates_compiled_source(self):
        graph = lib.make({
            "mod/ya.make": LIBRARY_HEAD + "CHECK_CONFIG_H(conf.h)\nEND()\n",
            "mod/conf.h": '#include "conf2.h"\n',
            "mod/conf2.h": "",
        }, "mod")
        node = lib.node_by_output(graph, "$(B)/mod/conf.config.cpp")
        self.assertEqual(node["kv"], {"p": "CH", "pc": "yellow"})
        self.assertEqual(node["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/check_config_h.py", "mod/conf.h", "$(B)/mod/conf.config.cpp",
        ])
        self.assertEqual(node["inputs"], [
            "$(S)/build/scripts/check_config_h.py", "$(S)/mod/conf.h", "$(S)/mod/conf2.h",
        ])
        compile_node = lib.node_by_output(graph, "$(B)/mod/conf.config.cpp.o")
        self.assertEqual(compile_node["deps"], [node["uid"]])
        self.assertIn("$(S)/mod/conf.h", compile_node["inputs"])


class LuaJitArchiveTest(unittest.TestCase):
    def test_lj_21_archive_compiles_and_archives_scripts(self):
        files = {
            "mod/ya.make": (
                LIBRARY_HEAD
                + "LJ_21_ARCHIVE(NAME Scripts a.lua sub/b.lua)\nSRCS(use.cpp)\nEND()\n"
            ),
            "mod/a.lua": "",
            "mod/sub/b.lua": "",
            "mod/use.cpp": '#include "LuaScripts.inc"\n#include "LuaSources.inc"\n',
        }
        lib.tool_program(files, "tools/archiver", "archiver")
        lib.tool_program(files, "contrib/libs/luajit_21/compiler", "compiler")
        graph = lib.make(files, "mod")

        compiler = lib.node_by_output(graph, "$(B)/contrib/libs/luajit_21/compiler/compiler")
        first = lib.node_by_output(graph, "$(B)/mod/a.raw")
        self.assertEqual(first["kv"], {"p": "LJ", "pc": "light-cyan"})
        self.assertEqual(first["cmds"][0]["cmd_args"], [
            "$(B)/contrib/libs/luajit_21/compiler/compiler", "-b", "-g",
            "$(S)/mod/a.lua", "$(B)/mod/a.raw",
        ])
        self.assertEqual(first["cmds"][0]["cwd"], "$(S)/contrib/libs/luajit_21")
        self.assertEqual(first["inputs"], [
            "$(B)/contrib/libs/luajit_21/compiler/compiler", "$(S)/mod/a.lua",
        ])
        self.assertEqual(first["foreign_deps"], {"tool": [compiler["uid"]]})
        second = lib.node_by_output(graph, "$(B)/mod/sub/b.raw")

        scripts = lib.node_by_output(graph, "$(B)/mod/LuaScripts.inc")
        self.assertEqual(scripts["cmds"][0]["cmd_args"], [
            "$(B)/tools/archiver/archiver", "-q", "-x", "-p",
            "$(B)/mod/a.raw", "$(B)/mod/sub/b.raw",
            "-k", "a.lua:sub/b.lua", "-o", "$(B)/mod/LuaScripts.inc",
        ])
        self.assertIn(first["uid"], scripts["deps"])
        self.assertIn(second["uid"], scripts["deps"])
        sources = lib.node_by_output(graph, "$(B)/mod/LuaSources.inc")
        self.assertEqual(sources["inputs"], [
            "$(S)/mod/a.lua", "$(S)/mod/sub/b.lua", "$(B)/tools/archiver/archiver",
        ])
        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        for path in ("$(B)/mod/LuaScripts.inc", "$(S)/mod/a.lua", "$(S)/mod/sub/b.lua"):
            self.assertIn(path, use["inputs"])


class BuildInfoTest(unittest.TestCase):
    def test_create_buildinfo_for_emits_three_step_generator(self):
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD + "CREATE_BUILDINFO_FOR(buildinfo_data.h)\nSRCS(use.cpp)\nEND()\n"
            ),
            "mod/use.cpp": '#include "buildinfo_data.h"\n',
            "build/scripts/build_info_gen.py": "",
            "build/scripts/xargs.py": "",
            "build/scripts/yield_line.py": "",
        }, "mod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/mod/buildinfo_data.h")
        self.assertEqual(node["kv"], {
            "disable_cache": "yes", "p": "BI", "pc": "yellow", "show_out": "yes",
        })
        self.assertEqual(len(node["cmds"]), 3)
        self.assertEqual(node["cmds"][0]["cmd_args"][1:4], [
            "$(S)/build/scripts/yield_line.py", "--", "$(B)/mod/__args",
        ])
        flags = node["cmds"][1]["cmd_args"]
        self.assertEqual(flags[1:4], [
            "$(S)/build/scripts/yield_line.py", "--", "$(B)/mod/__args",
        ])
        self.assertIn("-msse4.2", flags)
        self.assertEqual(flags[-2:], ["-DCATBOOST_OPENSOURCE=yes", "-nostdinc++"])
        self.assertEqual(node["cmds"][2]["cmd_args"][1:3], [
            "$(S)/build/scripts/xargs.py", "--",
        ])
        self.assertEqual(node["cmds"][2]["cmd_args"][-2:], [
            "$(S)/build/scripts/build_info_gen.py", "$(B)/mod/buildinfo_data.h",
        ])
        self.assertEqual(node["inputs"], [
            "$(S)/build/scripts/yield_line.py",
            "$(S)/build/scripts/xargs.py",
            "$(S)/build/scripts/build_info_gen.py",
        ])
        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn(node["uid"], use["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
