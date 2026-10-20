import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
X86_64 = ("--target-platform", "default-linux-x86_64")


def fixture():
    return {
        "peer/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}SRCS(p.cpp)\n"
            "LDFLAGS(-Wl,--peer-flag)\nEXTRALIBS(z -lfoo)\nEND()\n"
        ),
        "peer/p.cpp": "int p;\n",
        "prog/ya.make": (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(peer)\n"
            "LDFLAGS(-Wl,--own-flag)\n"
            "EXPORTS_SCRIPT(prog/prog.exports)\n"
            "SPLIT_DWARF()\nNO_OPTIMIZE()\nNO_COMPILER_WARNINGS()\nEND()\n"
        ),
        "prog/m.cpp": "int main(){return 0;}\n",
        "prog/prog.exports": "{};\n",
        "noexp/ya.make": (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\n"
            "NO_EXPORT_DYNAMIC_SYMBOLS()\nEXPORTS_SCRIPT(noexp/x.exports)\nEND()\n"
        ),
        "noexp/m.cpp": "int main(){return 0;}\n",
        "noexp/x.exports": "{};\n",
        "plain/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nEND()\n",
        "plain/m.cpp": "int main(){return 0;}\n",
    }


def command_with(node, marker):
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith(marker) for arg in cmd["cmd_args"])
    )


def trailer(args):
    return args[args.index("-Wl,--end-group") + 1:]


class LinkFlagsTest(unittest.TestCase):
    def test_resource_globals_in_link_flags_resolve_in_the_linking_program(self):
        # The flag names a resource its own module does not declare; the
        # program renders it with the resources of its closure, as ymake does.
        files = {
            "res/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(TOOLX sbr:1)\nEND()\n",
            "plugin/ya.make": (
                f"LIBRARY()\n{NO_PLATFORM}"
                "LDFLAGS(-L/X=$TOOLX_RESOURCE_GLOBAL/bin/x -L/Y=${TOOLY_RESOURCE_GLOBAL}/y)\nEND()\n"
            ),
            "both/ya.make": f"LIBRARY()\n{NO_PLATFORM}PEERDIR(res plugin)\nEND()\n",
            "with/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(both)\nEND()\n",
            "with/m.cpp": "int main(){return 0;}\n",
            "without/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(plugin)\nEND()\n",
            "without/m.cpp": "int main(){return 0;}\n",
        }
        for target, expected in (
            ("with", ["-L/X=$(B)/resources/TOOLX/bin/x", "-L/Y=${TOOLY_RESOURCE_GLOBAL}/y"]),
            ("without", ["-L/X=$TOOLX_RESOURCE_GLOBAL/bin/x", "-L/Y=${TOOLY_RESOURCE_GLOBAL}/y"]),
        ):
            with self.subTest(target=target):
                graph = lib.make(files, target, *X86_64)
                link = command_with(lib.node_by_output(graph, f"$(B)/{target}/{target}"), "link_exe.py")
                self.assertEqual([arg for arg in link if arg.startswith("-L/")], expected)

    def test_extralibs_merge_one_value_per_module(self):
        # EXTRALIBS is PY_EXTRALIBS: a module keeps one OBJADDE_LIB_GLOBAL
        # value, skipping libs it already contains; the program appends the
        # peers' values unique as a whole.
        files = {
            "plain/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(a b c)\nEND()\n",
            "plain/m.cpp": "int main(){return 0;}\n",
        }
        for name, extralibs in (
            ("a", "EXTRALIBS(-lutil)\nEXTRALIBS(-ldl -lutil)\nEXTRALIBS(crypt c)\n"),
            ("b", "EXTRALIBS(-lutil -ldl)\n"),
            ("c", "EXTRALIBS(-lutil -ldl)\n"),
        ):
            files[f"{name}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(s.cpp)\n{extralibs}END()\n"
            files[f"{name}/s.cpp"] = "int s;\n"
        graph = lib.make(files, "plain", *X86_64)
        link = command_with(lib.node_by_output(graph, "$(B)/plain/plain"), "link_exe.py")
        self.assertEqual(
            [arg for arg in trailer(link) if arg.startswith("-l")],
            ["-ldl", "-lrt", "-lutil", "-ldl", "-lcrypt", "-lutil", "-ldl", "-lpthread", "-lc", "-lm"],
        )

    def test_program_link_flags_exports_and_split_dwarf(self):
        graph = lib.make(fixture(), "prog", "-r", *X86_64)
        node = lib.node_by_output(graph, "$(B)/prog/prog")
        self.assertEqual(node["outputs"], ["$(B)/prog/prog", "$(B)/prog/prog.debug"])
        link = command_with(node, "link_exe.py")
        self.assertEqual(
            trailer(link),
            [
                "-rdynamic",
                "-Wl,--version-script=$(S)/prog/prog.exports",
                "-ldl", "-lrt",
                "-Wl,--no-as-needed",
                "-Wl,--gdb-index",
                "-Wl,--peer-flag", "-Wl,--own-flag",
                "-lz", "-lfoo",
                "-nodefaultlibs", "-lpthread", "-lc",
                "-lm",
                "-Wl,--gc-sections",
                "-Wl,-no-pie",
            ],
        )
        self.assertEqual(node["inputs"][-1], "$(S)/prog/prog.exports")

        split = [cmd["cmd_args"][1:] for cmd in node["cmds"][-3:]]
        self.assertEqual(
            split,
            [
                ["--only-keep-debug", "$(B)/prog/prog", "$(B)/prog/prog.debug"],
                ["--strip-debug", "$(B)/prog/prog"],
                [
                    "--remove-section=.gnu_debuglink",
                    "--add-gnu-debuglink", "$(B)/prog/prog.debug", "$(B)/prog/prog",
                ],
            ],
        )
        self.assertEqual(
            [cmd.get("env") for cmd in node["cmds"][-3:]],
            [{"ARCADIA_ROOT_DISTBUILD": "$(S)"}] * 3,
        )

        vcs_compile = node["cmds"][1]["cmd_args"]
        self.assertIn("$(B)/prog/__vcs_version__.c.o", vcs_compile)
        self.assertIn("-O0", vcs_compile)
        self.assertNotIn("-O3", vcs_compile)
        self.assertIn("-Wno-everything", vcs_compile)
        self.assertNotIn("-Wall", vcs_compile)
        compile_args = lib.node_by_output(graph, "$(B)/prog/m.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-O0", compile_args)
        self.assertNotIn("-O3", compile_args)
        self.assertIn("-Wno-everything", compile_args)

    def test_debug_build_compresses_debug_sections_when_configured(self):
        files = fixture()
        files["build/ymake_conf.py"] = lib.ymake_conf(compress_debug=True)
        graph = lib.make(files, "prog")
        link = command_with(lib.node_by_output(graph, "$(B)/prog/prog"), "link_exe.py")
        self.assertEqual(
            trailer(link)[:4],
            [
                "-rdynamic",
                "-Wl,--version-script=$(S)/prog/prog.exports",
                "-Wl,--compress-debug-sections=zstd",
                "-ldl",
            ],
        )
        graph = lib.make(files, "prog", "-r")
        link = command_with(lib.node_by_output(graph, "$(B)/prog/prog"), "link_exe.py")
        self.assertNotIn("-Wl,--compress-debug-sections=zstd", link)

    def test_autocheck_disables_pie_for_pic_tools(self):
        files = {
            "lib/ya.make": f"LIBRARY()\n{NO_PLATFORM}RUN_PROGRAM(tools/gen OUT a.o)\nEND()\n",
        }
        lib.tool_program(files, "tools/gen", "gen")
        for flags, expected in (
            ((), False),
            (("AUTOCHECK=yes",), True),
            (("PIC_NO_PIE=yes",), True),
            (("AUTOCHECK=yes", "PIC_NO_PIE=no"), False),
            (("AUTOCHECK=yes", "PIE=yes"), False),
        ):
            with self.subTest(flags=flags):
                args = [item for flag in flags for item in ("--host-platform-flag", flag)]
                graph = lib.make(files, "lib", *X86_64, *args)
                link = command_with(lib.node_by_output(graph, "$(B)/tools/gen/gen"), "link_exe.py")
                self.assertEqual("-Wl,-no-pie" in link, expected)

    def test_system_libraries_use_configured_linker_passthrough(self):
        files = fixture()
        files["build/conf/linkers/ld.conf"] = lib.FAKE_LD_CONF.replace("-lpthread -lc", "-Wl,-lpthread,-lc")
        graph = lib.make(files, "plain", *X86_64)
        link = command_with(lib.node_by_output(graph, "$(B)/plain/plain"), "link_exe.py")
        self.assertIn("-Wl,-lpthread,-lc", link)
        self.assertNotIn("-lpthread", link)
        self.assertNotIn("-lc", link)

    def test_no_export_dynamic_symbols_drops_rdynamic_and_version_script(self):
        graph = lib.make(fixture(), "noexp")
        node = lib.node_by_output(graph, "$(B)/noexp/noexp")
        link = command_with(node, "link_exe.py")
        self.assertEqual(trailer(link)[:3], ["-ldl", "-lrt", "-Wl,--no-as-needed"])
        self.assertNotIn("-rdynamic", link)
        self.assertEqual(node["inputs"][-1], "$(S)/noexp/x.exports")

    def test_musl_and_arcadia_libm_change_system_libraries(self):
        files = fixture()
        files["contrib/libs/libm/ya.make"] = (
            f"LIBRARY()\n{NO_PLATFORM}SRCS(m.c)\nEND()\n"
        )
        files["contrib/libs/libm/m.c"] = "int libm;\n"
        graph = lib.make(files, "plain", "--musl", "-DUSE_ARCADIA_LIBM=yes")
        link = command_with(lib.node_by_output(graph, "$(B)/plain/plain"), "link_exe.py")
        self.assertEqual(
            trailer(link),
            ["-rdynamic", "-Wl,--no-as-needed", "-Wl,--gdb-index",
             "-nostdlib", "-Wl,--gc-sections", "-Wl,-no-pie"],
        )


class AutocheckLinkTest(unittest.TestCase):
    def tree(self):
        files = fixture()
        files["contrib/libs/libeatmydata/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(e.c)\nEND()\n"
        files["contrib/libs/libeatmydata/e.c"] = "int e;\n"
        files["build/cow/on/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(c.c)\nEND()\n"
        files["build/cow/on/c.c"] = "int c;\n"
        lib.tool_program(files, "tools/gen", "gen")
        files["gen/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}RUN_PROGRAM(tools/gen OUT g.h)\nSRCS(g.cpp)\nEND()\n"
        files["gen/g.cpp"] = '#include "g.h"\n'
        return files

    def test_autocheck_links_eatmydata(self):
        files = self.tree()
        files["prog/ya.make"] = "PROGRAM()\nNO_LIBC()\nNO_UTIL()\nSRCS(m.cpp)\nEND()\n"
        graph = lib.make(files, "prog", *X86_64, "-DAUTOCHECK=yes")
        link = command_with(lib.node_by_output(graph, "$(B)/prog/prog"), "link_exe.py")
        self.assertIn("contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a", link)
        graph = lib.make(files, "prog", *X86_64)
        link = command_with(lib.node_by_output(graph, "$(B)/prog/prog"), "link_exe.py")
        self.assertNotIn("contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a", link)

    def test_autocheck_links_pic_tools_without_pie(self):
        files = self.tree()
        host = ("--host-platform", "default-linux-x86_64")
        graph = lib.make(files, "gen", *X86_64, *host)
        tool = command_with(lib.node_by_output(graph, "$(B)/tools/gen/gen"), "link_exe.py")
        self.assertNotIn("-Wl,-no-pie", tool)
        graph = lib.make(files, "gen", *X86_64, *host, "--host-platform-flag", "AUTOCHECK=yes")
        tool = command_with(lib.node_by_output(graph, "$(B)/tools/gen/gen"), "link_exe.py")
        self.assertEqual(tool[-1], "-Wl,-no-pie")

    def test_system_libraries_follow_ld_conf(self):
        files = fixture()
        files["build/conf/linkers/ld.conf"] = lib.FAKE_LD_CONF.replace("-lpthread -lc", "-Wl,-lpthread,-lc")
        graph = lib.make(files, "plain", *X86_64)
        link = trailer(command_with(lib.node_by_output(graph, "$(B)/plain/plain"), "link_exe.py"))
        self.assertEqual(link[link.index("-nodefaultlibs"):link.index("-lm")], ["-nodefaultlibs", "-Wl,-lpthread,-lc"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
