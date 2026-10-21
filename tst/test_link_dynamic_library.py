import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
COMPRESS_DEBUG_CONF = lib.ymake_conf(compress_debug=True)


def library(body=""):
    return f"LIBRARY()\n{NO_PLATFORM}{body}END()\n"


def make_raw(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-dyn-") as directory:
        root = Path(directory)
        lib.seed_root(root)
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
    def test_nested_dll_prefix_places_vcs_files_beside_link_output(self):
        for prefix, directory in (("lib", ""), ("auto/binding/lib", "auto/binding/"), ("plugins/", "plugins/")):
            with self.subTest(prefix=prefix):
                files = base_files()
                files["binding/ya.make"] = (
                    f"DLL(Binding PREFIX {prefix})\n{NO_PLATFORM}SRCS(binding.cpp)\nEND()\n"
                )
                files["binding/binding.cpp"] = "int binding;\n"
                graph = lib.make(files, "binding")
                output = f"$(B)/binding/{prefix}Binding.so"
                linked = lib.node_by_output(graph, output)
                source = f"$(B)/binding/{directory}__vcs_version__.c"
                obj = source + ".pic.o"
                vcs = command_with(linked, "vcs_info.py")["cmd_args"]
                self.assertIn(source, vcs)
                compile_args = next(c["cmd_args"] for c in linked["cmds"] if "-c" in c["cmd_args"])
                self.assertIn(source, compile_args)
                self.assertEqual(compile_args[compile_args.index("-o") + 1], obj)
                link = command_with(linked, "link_dyn_lib.py")["cmd_args"]
                self.assertIn(obj, link)
                self.assertIn("$(B)/binding/binding.cpp.pic.o", link)
                self.assertEqual(linked["outputs"], [output])

    def test_dynamic_library_copy_step_observes_so_outputs(self):
        for statement, copied in (
            ("", True),
            ("SET(SO_OUTPUTS no)", False),
            ("DISABLE(SO_OUTPUTS)", False),
            ("SET(SO_OUTPUTS yes)", True),
            ("ENABLE(SO_OUTPUTS)", True),
            ("WITH_DYNAMIC_LIBS()", True),
        ):
            with self.subTest(statement=statement):
                files = base_files()
                files["dyn/ya.make"] = (
                    f"DYNAMIC_LIBRARY(foo)\n{NO_PLATFORM}{statement}\n"
                    "EXPORTS_SCRIPT(foo.exports)\nDYNAMIC_LIBRARY_FROM(lib2)\nEND()\n"
                )
                files["dyn/foo.exports"] = "{};\n"
                files["build/scripts/fs_tools.py"] = ""
                graph = lib.make(files, "dyn")
                dynamic = lib.node_by_output(graph, "$(B)/dyn/libfoo.so")
                self.assertEqual(copied, any("link_or_copy_to_dir" in c["cmd_args"] for c in dynamic["cmds"]))
                self.assertEqual(copied, "$(S)/build/scripts/fs_tools.py" in dynamic["inputs"])
                if copied:
                    copy = command_with(dynamic, "fs_tools.py")["cmd_args"]
                    self.assertEqual(copy[-3:], ["link_or_copy_to_dir", "--no-check", "$(B)/dyn"])
                    link = command_with(dynamic, "link_dyn_lib.py")["cmd_args"]
                    self.assertIn("-Wl,-rpath,/opt/lib", link)
                    self.assertNotIn("-Wl,-rpath,$ORIGIN", link)

    def test_internal_dynamic_binary_uses_pic_without_implicit_copy_command(self):
        files = base_files()
        files["dyn/ya.make"] = (
            f"DYNAMIC_LIBRARY(foo)\n{NO_PLATFORM}"
            "EXPORTS_SCRIPT(foo.exports)\nDYNAMIC_LIBRARY_FROM(lib2)\nEND()\n"
        )
        files["dyn/foo.exports"] = "{};\n"
        files["build/scripts/fs_tools.py"] = ""
        graph = lib.make(files, "dyn", opensource=False)
        dynamic = lib.node_by_output(graph, "$(B)/dyn/libfoo.so")
        link = command_with(dynamic, "link_dyn_lib.py")["cmd_args"]
        self.assertIn("-fPIC", link)
        compile_vcs = next(c["cmd_args"] for c in dynamic["cmds"] if "-c" in c["cmd_args"])
        self.assertIn("-fPIC", compile_vcs)
        peer = lib.node_by_output(graph, "$(B)/lib2/b.cpp.pic.o")
        self.assertIn("-fPIC", peer["cmds"][0]["cmd_args"])
        self.assertFalse(any("link_or_copy_to_dir" in c["cmd_args"] for c in dynamic["cmds"]))
        self.assertNotIn("$(S)/build/scripts/fs_tools.py", dynamic["inputs"])

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
        self.assertNotIn("-Wl,-no-pie", args)
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
        self.assertEqual(
            command_with(dyn, "fs_tools.py")["cmd_args"][-3:],
            ["link_or_copy_to_dir", "--no-check", "$(B)/dyn"],
        )

        program = lib.node_by_output(graph, "$(B)/p/prog")
        self.assertEqual(program["outputs"], ["$(B)/p/prog", "$(B)/p/libfoo.so"])
        self.assertIn(dyn["uid"], program["deps"])
        program_link = " ".join(command_with(program, "link_exe.py")["cmd_args"])
        self.assertIn("--start-plugins --end-plugins", program_link)
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
        self.assertIn("-fPIC", target_args)
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
        }
        for declaration, message in cases.items():
            with self.subTest(message=message):
                files = base_files()
                files["dyn/ya.make"] = declaration
                files["dyn/t.cpp"] = "int t;\n"
                result = make_raw(files, "dyn")
                self.assertEqual(result.returncode, 1)
                self.assertIn(message, result.stderr)

    def test_autocheck_common_link_peers_precede_explicit_dll_peers(self):
        files = base_files()
        files["dlltool/ya.make"] = (
            "DLL_TOOL(tool)\nNO_RUNTIME()\nEXPORTS_SCRIPT(t.exports)\n"
            "SRCS(t.cpp)\nPEERDIR(lib2)\nEND()\n"
        )
        files["dlltool/t.cpp"] = ""
        files["dlltool/t.exports"] = ""
        files["contrib/libs/libeatmydata/ya.make"] = library("SRCS(x.cpp)\n")
        files["contrib/libs/libeatmydata/x.cpp"] = ""
        graph = lib.make(files, "dlltool", "-DAUTOCHECK=yes")
        node = lib.node_by_output(graph, "$(B)/dlltool/libtool.so")
        args = command_with(node, "link_dyn_lib.py")["cmd_args"]
        group = args[args.index("-Wl,--start-group") + 1:args.index("-Wl,--end-group")]
        self.assertEqual(group, [
            "contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a",
            "build/cow/on/libbuild-cow-on.a", "lib2/liblib2.a",
        ])

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
        self.assertIn("$(B)/dlltool/t.cpp.o", node["inputs"])
        self.assertIn("$(S)/dlltool/t.exports", node["inputs"])
        self.assertEqual(
            [graph["graph"][ref]["outputs"][0] for ref in graph["result"]],
            ["$(B)/dlltool/mytool.so"],
        )

    def test_dll_links_pic_shared_object(self):
        files = base_files()
        files["dll/ya.make"] = f"DLL(d)\n{NO_PLATFORM}STRIP()\nSRCS(d.cpp)\nPEERDIR(lib2)\nEND()\n"
        files["dll/d.cpp"] = "int d;\n"
        graph = lib.make(files, "dll")
        node = lib.node_by_output(graph, "$(B)/dll/libd.so")
        self.assertEqual([graph["graph"][ref]["outputs"][0] for ref in graph["result"]], ["$(B)/dll/libd.so"])
        text = " ".join(command_with(node, "link_dyn_lib.py")["cmd_args"])
        self.assertIn("$(B)/dll/d.cpp.pic.o -o $(B)/dll/libd.so -shared -Wl,-soname,libd.so", text)
        self.assertNotIn("--version-script", text)
        self.assertIn("-Wl,--strip-all -Wl,--gc-sections", text)
        self.assertIn("-fPIC", lib.node_by_output(graph, "$(B)/lib2/b.cpp.pic.o")["cmds"][0]["cmd_args"])

    def test_so_program_builds_archive_and_recurse_finds_dll_tool(self):
        files = base_files()
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
            library("SRCS(top.cpp)\n") + "RECURSE(../so ../dlltool)\n"
        )
        files["top/top.cpp"] = "int top;\n"
        graph = lib.make(files, "top")
        self.assertEqual(
            [graph["graph"][ref]["outputs"][0] for ref in graph["result"]],
            ["$(B)/top/libtop.a", "$(B)/dlltool/libtool.so"],
        )

        for target, archive in (("so", "$(B)/so/libs.a"),):
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

    def test_peer_resource_globals_select_dynamic_library_toolchain(self):
        files = base_files()
        files["res/ya.make"] = (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:20 LLD_ROOT sbr:21)\n"
            "END()\n"
        )
        files["lib/ya.make"] = library("SRCS(a.cpp)\nPEERDIR(res)\n")
        files["lib2/ya.make"] = library("SRCS(b.cpp)\nPEERDIR(res)\n")
        files["dyn/ya.make"] = (
            f"DYNAMIC_LIBRARY(foo)\n{NO_PLATFORM}EXPORTS_SCRIPT(foo.exports)\n"
            "DYNAMIC_LIBRARY_FROM(lib lib2)\nEND()\n"
        )
        files["dyn/foo.exports"] = "{};\n"
        graph = lib.make(files, "dyn")
        dyn = lib.node_by_output(graph, "$(B)/dyn/libfoo.so")
        clang = lib.node_by_output(graph, "$(B)/resources/CLANG20")
        lld = lib.node_by_output(graph, "$(B)/resources/LLD_ROOT")
        self.assertIn(clang["uid"], dyn["deps"])
        self.assertIn(lld["uid"], dyn["deps"])
        self.assertEqual(dyn["cmds"][1]["cmd_args"][0], "$(B)/resources/CLANG20/bin/clang")
        link = command_with(dyn, "link_dyn_lib.py")["cmd_args"]
        self.assertIn("$(B)/resources/CLANG20/bin/llvm-objcopy", link)
        self.assertIn("$(B)/resources/CLANG20/bin/clang++", link)
        self.assertIn("--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld", link)

    def test_platform_lld_peer_selects_the_linker_without_library_peers(self):
        # DYNAMIC_LIBRARY is a linking module: build/platform/lld is one of its
        # implicit peers, so LLD_ROOT reaches --ld-path even when no
        # DYNAMIC_LIBRARY_FROM library peers it (devtools_ya_bin reference:
        # contrib/libs/libiconv/dynamic links with $(LLD_ROOT)/bin/ld.lld).
        files = base_files()
        files["build/platform/lld/ya.make"] = (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(LLD_ROOT sbr:21)\nEND()\n"
        )
        files["dyn/ya.make"] = (
            "DYNAMIC_LIBRARY(foo)\nNO_RUNTIME()\nEXPORTS_SCRIPT(foo.exports)\n"
            "DYNAMIC_LIBRARY_FROM(lib)\nEND()\n"
        )
        files["dyn/foo.exports"] = "{};\n"
        graph = lib.make(files, "dyn")
        link = command_with(lib.node_by_output(graph, "$(B)/dyn/libfoo.so"), "link_dyn_lib.py")["cmd_args"]
        self.assertIn("--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld", link)


if __name__ == "__main__":
    unittest.main(verbosity=2)
