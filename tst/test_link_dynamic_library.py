import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
COMPRESS_DEBUG_CONF = "debug_info_flags.append('-gz=zstd')\n"


def library(body=""):
    return f"LIBRARY()\n{NO_PLATFORM}{body}END()\n"


def make_raw(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-dyn-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


def base_files():
    files = {
        "lib/ya.make": library(
            "SRCS(a.cpp)\n"
            "ADDINCL(GLOBAL lib/include)\n"
            "CFLAGS(GLOBAL -DLIB_C)\n"
            "CXXFLAGS(GLOBAL -DLIB_CXX)\n"
            "CONLYFLAGS(GLOBAL -DLIB_CONLY)\n"
            "LD_PLUGIN(plug.py)\n"
            "SET_APPEND(RPATH_GLOBAL -Wl,-rpath,/opt/lib)\n"
        ),
        "lib/a.cpp": "int a(){return 1;}\n",
        "lib/plug.py": "PLUGIN = 1\n",
        "lib/include/h.h": "\n",
        "lib2/ya.make": library(
            "SRCS(b.cpp)\n"
            "CFLAGS(GLOBAL -DLIB_C)\n"
            "SET_APPEND(RPATH_GLOBAL -Wl,-rpath,/opt/lib)\n"
            "LD_PLUGIN(plug2.py)\n"
        ),
        "lib2/b.cpp": "int b(){return 2;}\n",
        "lib2/plug2.py": "PLUGIN = 2\n",
        "build/platform/local_so/ya.make": library(
            "SET_APPEND(RPATH_GLOBAL -Wl,-rpath,$ORIGIN)\n"
        ),
        "build/cow/on/ya.make": library("SRCS(cow.cpp)\n"),
        "build/cow/on/cow.cpp": "int cow;\n",
    }
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


def command_with(node, script):
    return next(
        cmd
        for cmd in node["cmds"]
        if any(arg.endswith(script) for arg in cmd["cmd_args"])
    )


class DynamicLibraryTest(unittest.TestCase):
    def test_dynamic_library_links_whole_peers_and_feeds_programs(self):
        files = base_files()
        files["dyn/ya.make"] = (
            "DYNAMIC_LIBRARY(foo)\n"
            "EXPORTS_SCRIPT(foo.exports)\n"
            "DYNAMIC_LIBRARY_FROM(lib lib2 lib build/platform/local_so)\n"
            "CFLAGS(GLOBAL -DDYN)\n"
            "END()\n"
        )
        files["dyn/foo.exports"] = "{ global: a; local: *; };\n"
        files["p/ya.make"] = (
            f"PROGRAM(prog)\n{NO_PLATFORM}SRCS(m.cpp x.c)\nPEERDIR(dyn)\nEND()\n"
        )
        files["p/m.cpp"] = "int main(){return 0;}\n"
        files["p/x.c"] = "int x;\n"
        graph = lib.make(files, "p")

        dyn = lib.node_by_output(graph, "$(B)/dyn/libfoo.so")
        self.assertEqual(dyn["kv"], {"p": "LD", "pc": "light-blue", "show_out": "yes"})
        link = command_with(dyn, "link_dyn_lib.py")
        args = link["cmd_args"]
        self.assertEqual(link["cwd"], "$(B)")
        self.assertEqual(
            args[args.index("--start-plugins"):args.index("--end-plugins") + 1],
            [
                "--start-plugins",
                "$(B)/lib/plug.py.pyplugin",
                "$(B)/lib2/plug2.py.pyplugin",
                "--end-plugins",
            ],
        )
        text = " ".join(args)
        self.assertIn(
            "--whole-archive-peers lib --whole-archive-peers lib2 "
            "--whole-archive-peers lib --whole-archive-peers build/platform/local_so",
            text,
        )
        self.assertIn("--fix-elf $(B)/tools/fix_elf/fix_elf", text)
        self.assertIn("-o $(B)/dyn/libfoo.so -shared -Wl,-soname,libfoo.so", text)
        self.assertIn(
            "-Wl,--start-group build/cow/on/libbuild-cow-on.a lib/liblib.a "
            "lib2/liblib2.a -Wl,--end-group",
            text,
        )
        self.assertIn("-Wl,--version-script=$(S)/dyn/foo.exports", text)
        self.assertEqual(args[-1], "-Wl,-no-pie")
        self.assertEqual(
            dyn["inputs"],
            [
                "$(B)/build/cow/on/libbuild-cow-on.a",
                "$(B)/lib/liblib.a",
                "$(B)/lib2/liblib2.a",
                "$(B)/lib/plug.py.pyplugin",
                "$(B)/lib2/plug2.py.pyplugin",
                "$(B)/tools/fix_elf/fix_elf",
                "$(S)/build/scripts/c_templates/svn_interface.c",
                "$(S)/build/scripts/c_templates/svnversion.h",
                "$(S)/dyn/foo.exports",
            ],
        )
        copy = command_with(dyn, "fs_tools.py")
        self.assertEqual(
            copy["cmd_args"][-3:],
            ["link_or_copy_to_dir", "--no-check", "$(B)/dyn"],
        )

        program = lib.node_by_output(graph, "$(B)/p/prog")
        self.assertEqual(program["outputs"], ["$(B)/p/prog", "$(B)/p/libfoo.so"])
        self.assertIn(dyn["uid"], program["deps"])
        program_link = " ".join(command_with(program, "link_exe.py")["cmd_args"])
        self.assertIn(
            "--start-plugins $(B)/lib/plug.py.pyplugin "
            "$(B)/lib2/plug2.py.pyplugin --end-plugins",
            program_link,
        )
        self.assertIn(
            "-Wl,--start-group build/cow/on/libbuild-cow-on.a dyn/libfoo.so "
            "-Wl,--end-group",
            program_link,
        )
        self.assertIn(
            "-Wl,--no-as-needed -Wl,-rpath,/opt/lib -Wl,-rpath,$ORIGIN "
            "-Wl,--gdb-index -Wl,-rpath,/opt/lib -Wl,-rpath,$ORIGIN",
            program_link,
        )
        self.assertEqual(
            command_with(program, "fs_tools.py")["cmd_args"][-4:],
            ["link_or_copy_to_dir", "--no-check", "$(B)/dyn/libfoo.so", "$(B)/p"],
        )

        c_source = lib.node_by_output(graph, "$(B)/p/x.c.o")
        c_args = c_source["cmds"][0]["cmd_args"]
        self.assertIn("-DDYN", c_args)
        self.assertIn("-DLIB_C", c_args)
        self.assertIn("-DLIB_CONLY", c_args)
        self.assertNotIn("-DLIB_CXX", c_args)
        self.assertNotIn("-std=c++20", c_args)
        self.assertIn("-I$(S)/lib/include", c_args)
        cxx_args = lib.node_by_output(graph, "$(B)/p/m.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DLIB_CXX", cxx_args)
        self.assertNotIn("-DLIB_CONLY", cxx_args)

    def test_host_dynamic_library_is_pic_and_target_compresses_debug(self):
        files = base_files()
        files["build/ymake_conf.py"] = COMPRESS_DEBUG_CONF
        files["dyn/ya.make"] = (
            f"DYNAMIC_LIBRARY(bar)\n{NO_PLATFORM}"
            "EXPORTS_SCRIPT(bar.exports)\nDYNAMIC_LIBRARY_FROM(lib2)\nEND()\n"
        )
        files["dyn/bar.exports"] = "{};\n"
        lib.tool_program(files, "tools/archiver", "archiver")
        files["tools/archiver/ya.make"] = files["tools/archiver/ya.make"].replace(
            "SRCS(main.cpp)\n", "SRCS(main.cpp)\nPEERDIR(dyn)\n"
        )
        files["target/ya.make"] = (
            f"DYNAMIC_LIBRARY(baz)\n{NO_PLATFORM}"
            "EXPORTS_SCRIPT(baz.exports)\nDYNAMIC_LIBRARY_FROM(user)\nEND()\n"
        )
        files["target/baz.exports"] = "{};\n"
        files["user/ya.make"] = library(
            "SRCS(use.cpp)\nARCHIVE(NAME data.inc payload.lst)\n"
        )
        files["user/use.cpp"] = '#include "data.inc"\n'
        files["user/payload.lst"] = "row\n"
        graph = lib.make(files, "target")

        host = lib.node_by_output(graph, "$(B)/dyn/libbar.so")
        host_args = command_with(host, "link_dyn_lib.py")["cmd_args"]
        self.assertIn("--target=x86_64-linux-gnu", host_args)
        self.assertEqual(host_args.count("-fPIC"), 2)
        self.assertNotIn("-Wl,--compress-debug-sections=zstd", host_args)
        self.assertNotIn("build/cow/on/libbuild-cow-on.a", host_args)
        self.assertIn(
            "$(B)/dyn/__vcs_version__.c.pic.o",
            command_with(host, "link_dyn_lib.py")["cmd_args"],
        )

        target = lib.node_by_output(graph, "$(B)/target/libbaz.so")
        target_args = command_with(target, "link_dyn_lib.py")["cmd_args"]
        self.assertIn("--target=aarch64-linux-gnu", target_args)
        self.assertNotIn("-fPIC", target_args)
        self.assertIn("-Wl,--compress-debug-sections=zstd", target_args)
        archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        self.assertIn(host["uid"], archiver["deps"])
        self.assertIn(
            "dyn/libbar.so",
            command_with(archiver, "link_exe.py")["cmd_args"],
        )

    def test_dynamic_library_declaration_errors(self):
        cases = {
            "DYNAMIC_LIBRARY()\nEXPORTS_SCRIPT(x.exports)\nDYNAMIC_LIBRARY_FROM(lib)\nEND()\n":
                "gen: dyn DYNAMIC_LIBRARY requires a basename argument",
            "DYNAMIC_LIBRARY(x)\nEXPORTS_SCRIPT(x.exports)\nEND()\n":
                "gen: dyn DYNAMIC_LIBRARY requires DYNAMIC_LIBRARY_FROM(...)",
            "DYNAMIC_LIBRARY(x)\nDYNAMIC_LIBRARY_FROM(lib)\nEND()\n":
                "gen: dyn DYNAMIC_LIBRARY requires EXPORTS_SCRIPT(...)",
            "DLL_TOOL(x)\nSRCS(t.cpp)\nEND()\n":
                "gen: dyn DLL requires EXPORTS_SCRIPT(...)",
        }
        for declaration, message in cases.items():
            with self.subTest(message=message):
                files = base_files()
                files["dyn/ya.make"] = declaration
                files["dyn/t.cpp"] = "int t;\n"
                result = make_raw(files, "dyn")
                self.assertEqual(result.returncode, 1)
                self.assertIn(message, result.stderr)

    def test_dll_tool_links_shared_object_with_prefix(self):
        files = base_files()
        files["dlltool/ya.make"] = (
            "DLL_TOOL(tool PREFIX my)\nEXPORTS_SCRIPT(t.exports)\n"
            "SRCS(t.cpp)\nPEERDIR(lib2)\nEND()\n"
        )
        files["dlltool/t.cpp"] = "int t;\n"
        files["dlltool/t.exports"] = "{};\n"
        graph = lib.make(files, "dlltool")
        node = lib.node_by_output(graph, "$(B)/dlltool/mytool.so")
        self.assertEqual(node["kv"]["p"], "LD")
        self.assertEqual([cmd["cmd_args"][1] for cmd in node["cmds"][::2]], [
            "$(S)/build/scripts/vcs_info.py",
            "$(S)/build/scripts/link_dyn_lib.py",
        ])
        text = " ".join(command_with(node, "link_dyn_lib.py")["cmd_args"])
        self.assertIn(
            "-Wl,--no-whole-archive $(B)/dlltool/__vcs_version__.c.o "
            "$(B)/dlltool/t.cpp.o -o $(B)/dlltool/mytool.so -shared "
            "-Wl,-soname,mytool.so",
            text,
        )
        self.assertIn(
            "-Wl,--start-group build/cow/on/libbuild-cow-on.a lib2/liblib2.a "
            "-Wl,--end-group",
            text,
        )
        self.assertIn("-Wl,--version-script=$(S)/dlltool/t.exports", text)
        self.assertEqual(node["inputs"][-1], "$(B)/dlltool/t.cpp.o")
        self.assertIn("$(S)/dlltool/t.exports", node["inputs"])
        self.assertEqual(
            [graph["graph"][ref]["outputs"][0] for ref in graph["result"]],
            ["$(B)/dlltool/mytool.so"],
        )

    def test_dll_and_so_program_build_archives_and_recurse_finds_dll_tool(self):
        files = base_files()
        files["dll/ya.make"] = (
            f"DLL(d 1 2)\n{NO_PLATFORM}SRCS(d.cpp)\nPEERDIR(lib2)\nEND()\n"
        )
        files["dll/d.cpp"] = "int d;\n"
        files["so/ya.make"] = (
            f"SO_PROGRAM(s)\n{NO_PLATFORM}SRCS(s.cpp)\nPEERDIR(lib2)\nEND()\n"
        )
        files["so/s.cpp"] = "int s;\n"
        files["dlltool/ya.make"] = (
            "DLL_TOOL(tool)\nEXPORTS_SCRIPT(t.exports)\nSRCS(t.cpp)\nEND()\n"
        )
        files["dlltool/t.cpp"] = "int t;\n"
        files["dlltool/t.exports"] = "{};\n"
        files["top/ya.make"] = (
            library("SRCS(top.cpp)\n") + "RECURSE(../dll ../so ../dlltool)\n"
        )
        files["top/top.cpp"] = "int top;\n"
        graph = lib.make(files, "top")
        self.assertEqual(
            [graph["graph"][ref]["outputs"][0] for ref in graph["result"]],
            ["$(B)/top/libtop.a", "$(B)/dlltool/libtool.so"],
        )

        for target, archive in (("dll", "$(B)/dll/libd.a"), ("so", "$(B)/so/libs.a")):
            with self.subTest(target=target):
                graph = lib.make(files, target)
                node = lib.node_by_output(graph, archive)
                self.assertEqual(node["kv"]["p"], "AR")
                self.assertEqual(
                    [graph["graph"][ref]["outputs"][0] for ref in graph["result"]],
                    [archive],
                )
                self.assertIn(
                    "$(B)/lib2/liblib2.a",
                    {out for n in graph["graph"] for out in n["outputs"]},
                )


if __name__ == "__main__":
    unittest.main(verbosity=2)
