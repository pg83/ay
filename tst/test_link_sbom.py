import unittest

import lib


X86_64 = ("--target-platform", "default-linux-x86_64")


def library(body=""):
    return f"LIBRARY()\n{body}END()\n"


def toolchain(name, version):
    return f"RESOURCES_LIBRARY()\nTOOLCHAIN({name})\nVERSION({version})\nEND()\n"


def fixture():
    files = {
        "build/internal/conf/sbom.conf": "\n",
        "build/internal/platform/clang_toolchain_info/ya.make": toolchain("clang", "20"),
        "build/platform/python/ymake_python3/ya.make": toolchain("python3", "3.12"),
        "build/platform/lld/ya.make": toolchain("lld", "20"),
        "build/cow/on/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(cow.cpp)\nEND()\n"
        ),
        "build/cow/on/cow.cpp": "int cow;\n",
        "contrib/libs/cxxsupp/ya.make": library(
            "LICENSE(MIT)\nVERSION(1)\nSRCS(c.cpp)\n"
        ),
        "contrib/libs/cxxsupp/c.cpp": "int c;\n",
        "contrib/libs/cxxsupp/libcxx/ya.make": library(
            "NO_RUNTIME()\nNO_UTIL()\nLICENSE(Apache-2.0)\nVERSION(18)\nSRCS(x.cpp)\n"
        ),
        "contrib/libs/cxxsupp/libcxx/x.cpp": "int x;\n",
        "library/cpp/malloc/jemalloc/ya.make": library(
            "NO_RUNTIME()\nLICENSE(BSD)\nSRCS(j.cpp)\n"
        ),
        "library/cpp/malloc/jemalloc/j.cpp": "int j;\n",
        "lib/ya.make": library("LICENSE(Apache-2.0)\nVERSION(2 1)\nSRCS(a.cpp)\n"),
        "lib/a.cpp": "int a;\n",
        "prog/ya.make": (
            "PROGRAM()\nALLOCATOR(J)\nLICENSE(MIT)\nSRCS(m.cpp)\nPEERDIR(lib)\nEND()\n"
        ),
        "prog/m.cpp": "int main(){return 0;}\n",
        "dlltool/ya.make": (
            "DLL_TOOL(t)\nLICENSE(MIT)\nEXPORTS_SCRIPT(t.exports)\n"
            "SRCS(t.cpp)\nPEERDIR(lib)\nEND()\n"
        ),
        "dlltool/t.cpp": "int t;\n",
        "dlltool/t.exports": "{};\n",
    }
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


def component(path):
    return f"$(B)/{path}"


LINKED_COMPONENTS = [
    component("build/platform/python/ymake_python3/toolchain.component.sbom"),
    component("build/internal/platform/clang_toolchain_info/toolchain.component.sbom"),
    component("contrib/libs/cxxsupp/libcxx/libs-cxxsupp-libcxx.CPP.component.sbom"),
    component("contrib/libs/cxxsupp/contrib-libs-cxxsupp.CPP.component.sbom"),
    component("build/platform/lld/toolchain.component.sbom"),
]


def command_with(node, script):
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith(script) for arg in cmd["cmd_args"])
    )


def sbom_nodes(graph):
    return [node for node in graph["graph"] if node["kv"]["p"] == "DX"]


class SbomTest(unittest.TestCase):
    def test_release_program_embeds_ordered_components(self):
        graph = lib.make(fixture(), "prog", *X86_64, "-r")

        library_component = lib.node_by_output(graph, component("lib/lib.CPP.component.sbom"))
        self.assertEqual(library_component["kv"], {"p": "DX", "pc": "yellow"})
        self.assertEqual(
            library_component["cmds"][0]["cmd_args"],
            [
                "$(B)/resources/YMAKE_PYTHON3/bin/python3",
                "$(S)/build/internal/scripts/gen_sbom.py",
                "--output", component("lib/lib.CPP.component.sbom"),
                "--type", "library",
                "--path", "lib",
                "--ver", "2.1",
                "--lang", "CPP",
            ],
        )
        self.assertEqual(library_component["inputs"], ["$(S)/build/internal/scripts/gen_sbom.py"])
        allocator = lib.node_by_output(
            graph, component("library/cpp/malloc/jemalloc/cpp-malloc-jemalloc.CPP.component.sbom")
        )
        self.assertIn("unknown", allocator["cmds"][0]["cmd_args"])
        lld = lib.node_by_output(graph, component("build/platform/lld/toolchain.component.sbom"))
        self.assertEqual(
            lld["cmds"][0]["cmd_args"][2:],
            [
                "--output", component("build/platform/lld/toolchain.component.sbom"),
                "--type", "toolchain",
                "--toolchain-name", "lld",
                "--ver", "20",
            ],
        )

        program = lib.node_by_output(graph, "$(B)/prog/prog")
        link_sbom = command_with(program, "link_sbom.py")
        self.assertEqual(
            link_sbom[:10],
            [
                "",
                "$(S)/build/internal/scripts/link_sbom.py",
                "--lang", "CPP",
                "--mod-path", "prog",
                "--output", "$(B)/prog/__sbomdata.json",
                "--vcs-info", "$(B)/vcs.json",
            ],
        )
        self.assertEqual(
            link_sbom[10:],
            LINKED_COMPONENTS
            + [
                component("library/cpp/malloc/jemalloc/cpp-malloc-jemalloc.CPP.component.sbom"),
                component("lib/lib.CPP.component.sbom"),
                component("prog/prog.CPP.component.sbom"),
            ],
        )
        self.assertEqual(
            program["cmds"][-1]["cmd_args"][1:],
            ["--add-section", ".rosbomdata=$(B)/prog/__sbomdata.json", "$(B)/prog/prog"],
        )
        self.assertEqual(program["inputs"][-1], "$(S)/build/internal/scripts/link_sbom.py")
        self.assertIn(component("prog/prog.CPP.component.sbom"), program["inputs"])
        link = " ".join(command_with(program, "link_exe.py"))
        self.assertIn(
            "-Wl,--start-group contrib/libs/cxxsupp/libcxx/liblibs-cxxsupp-libcxx.a "
            "build/cow/on/libbuild-cow-on.a library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a "
            "contrib/libs/cxxsupp/libcontrib-libs-cxxsupp.a lib/liblib.a -Wl,--end-group",
            link,
        )

    def test_debug_program_emits_components_without_embedding(self):
        graph = lib.make(fixture(), "prog", *X86_64)
        self.assertIn(
            component("prog/prog.CPP.component.sbom"),
            {out for node in sbom_nodes(graph) for out in node["outputs"]},
        )
        program = lib.node_by_output(graph, "$(B)/prog/prog")
        scripts = [arg for cmd in program["cmds"] for arg in cmd["cmd_args"]]
        self.assertNotIn("$(S)/build/internal/scripts/link_sbom.py", scripts)
        self.assertEqual(len(program["cmds"]), 4)

    def test_non_x86_target_adds_cxxsupp_peer_without_components(self):
        graph = lib.make(fixture(), "prog", "-r")
        self.assertEqual(sbom_nodes(graph), [])
        link = " ".join(command_with(lib.node_by_output(graph, "$(B)/prog/prog"), "link_exe.py"))
        self.assertIn("contrib/libs/cxxsupp/libcontrib-libs-cxxsupp.a lib/liblib.a", link)

    def test_release_dll_tool_embeds_components(self):
        graph = lib.make(fixture(), "dlltool", *X86_64, "-r")
        node = lib.node_by_output(graph, "$(B)/dlltool/libt.so")
        scripts = [cmd["cmd_args"][1] for cmd in node["cmds"]]
        self.assertEqual(
            scripts,
            [
                "$(S)/build/scripts/vcs_info.py",
                node["cmds"][1]["cmd_args"][1],
                "$(S)/build/internal/scripts/link_sbom.py",
                "$(S)/build/scripts/link_dyn_lib.py",
                "--add-section",
            ],
        )
        self.assertEqual(
            command_with(node, "link_sbom.py")[10:],
            LINKED_COMPONENTS
            + [
                component("lib/lib.CPP.component.sbom"),
                component("dlltool/libt.CPP.component.sbom"),
            ],
        )
        self.assertEqual(node["cmds"][2]["cwd"], "$(B)")
        self.assertEqual(
            node["inputs"][-2:],
            [
                component("dlltool/libt.CPP.component.sbom"),
                "$(S)/build/internal/scripts/link_sbom.py",
            ],
        )

    def test_py3_program_component_precedes_lld_and_unversioned_toolchain(self):
        files = fixture()
        python_peers = [
            "contrib/libs/python",
            "library/python/runtime_py3/main",
            "library/python/import_tracing/constructor",
            "library/python/testing/import_test",
            "contrib/tools/python3/Modules/_sqlite",
        ]
        for path in python_peers:
            name = path.replace("/", "_")
            files[f"{path}/ya.make"] = (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                f"LICENSE(PSF)\nSRCS({name}.cpp)\nEND()\n"
            )
            files[f"{path}/{name}.cpp"] = f"int {name};\n"
        files["build/platform/nover/ya.make"] = (
            "RESOURCES_LIBRARY()\nTOOLCHAIN(nover)\nEND()\n"
        )
        files["lib/ya.make"] = library(
            "LICENSE(Apache-2.0)\nVERSION(2 1)\nSRCS(a.cpp)\nPEERDIR(build/platform/nover)\n"
        )
        files["py3/ya.make"] = "PY3_PROGRAM()\nLICENSE(MIT)\nPEERDIR(lib)\nEND()\n"
        graph = lib.make(files, "py3", *X86_64, "-r")

        nover = lib.node_by_output(graph, component("build/platform/nover/toolchain.component.sbom"))
        self.assertEqual(
            nover["cmds"][0]["cmd_args"][-4:],
            ["--toolchain-name", "nover", "--ver", "unknown"],
        )
        own = component("py3/py3.PY3.component.sbom")
        self.assertIn("--lang", lib.node_by_output(graph, own)["cmds"][0]["cmd_args"])
        self.assertEqual(lib.node_by_output(graph, own)["cmds"][0]["cmd_args"][-1], "PY3")
        link_sbom = command_with(lib.node_by_output(graph, "$(B)/py3/py3"), "link_sbom.py")
        self.assertEqual(link_sbom[2:4], ["--lang", "PY3"])
        components = link_sbom[10:]
        self.assertEqual(
            components[components.index(own) - 2:components.index(own) + 2],
            [
                component("build/platform/nover/toolchain.component.sbom"),
                component("lib/lib.CPP.component.sbom"),
                own,
                component("build/platform/lld/toolchain.component.sbom"),
            ],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
