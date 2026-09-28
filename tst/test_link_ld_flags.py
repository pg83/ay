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
        files["build/ymake_conf.py"] = "debug_info_flags.append('-gz=zstd')\n"
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
