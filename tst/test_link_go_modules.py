import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
X86_64_RELEASE = ("--target-platform", "default-linux-x86_64", "-r")
GO_SUPPORT = [
    "build/external_resources/go_tools",
    "build/external_resources/yolint",
    "contrib/go/_std_1.26/src/runtime",
    "contrib/go/_std_1.26/src/runtime/cgo",
    "library/go/core/buildinfo",
    "contrib/libs/cxxsupp",
]


def toolchain(name, version):
    return f"RESOURCES_LIBRARY()\nTOOLCHAIN({name})\nVERSION({version})\nEND()\n"


def fixture(sbom):
    files = {
        "g/ya.make": "GO_LIBRARY()\nLICENSE(MIT)\nSRCS(a.go b.s CGO_EXPORT)\nEND()\n",
        "g/a.go": "package g\n",
        "g/b.s": "\n",
        "gp/ya.make": "GO_PROGRAM()\nLICENSE(MIT)\nSRCS(main.go)\nPEERDIR(g)\nEND()\n",
        "gp/main.go": "package main\nfunc main(){}\n",
        "build/platform/python/ymake_python3/ya.make": toolchain("python3", "3.12"),
        "build/platform/lld/ya.make": toolchain("lld", "20"),
    }
    for path in GO_SUPPORT:
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}END()\n"
    files["g2/ya.make"] = "GO_LIBRARY()\nSRCS(b.go)\nPEERDIR(build/platform/lld)\nEND()\n"
    files["g2/b.go"] = "package g2\n"
    files["cl/ya.make"] = "LIBRARY()\nLICENSE(MIT)\nSRCS(c.cpp)\nPEERDIR(g g2)\nEND()\n"
    files["cl/c.cpp"] = "int c;\n"
    files["cp/ya.make"] = "PROGRAM()\nLICENSE(MIT)\nSRCS(m.cpp)\nPEERDIR(cl)\nEND()\n"
    files["cp/m.cpp"] = "int main(){return 0;}\n"
    if sbom:
        files["build/internal/conf/sbom.conf"] = "\n"
        files["build/internal/platform/clang_toolchain_info/ya.make"] = toolchain("clang", "20")
    return files


def command_with(node, marker):
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith(marker) for arg in cmd["cmd_args"])
    )


class GoModulesTest(unittest.TestCase):
    def test_go_program_links_go_package_with_assembly(self):
        graph = lib.make(fixture(sbom=False), "gp", *X86_64_RELEASE)
        symabis = lib.node_by_output(graph, "$(B)/g/gen.symabis")
        self.assertEqual(symabis["kv"]["p"], "go")
        self.assertEqual(symabis["inputs"], ["$(S)/g/b.s"])
        self.assertEqual(symabis["cmds"][0]["cmd_args"][-4:], ["-gensymabis", "-o", "$(B)/g/gen.symabis", "$(S)/g/b.s"])
        package = lib.node_by_output(graph, "$(B)/g/g.a")
        self.assertEqual(package["kv"]["p"], "GO")
        self.assertEqual(package["inputs"][:3], ["$(S)/g/a.go", "$(S)/g/b.s", "$(B)/g/gen.symabis"])
        self.assertIn(symabis["uid"], package["deps"])
        program = lib.node_by_output(graph, "$(B)/gp/gp")
        self.assertEqual(program["outputs"], ["$(B)/gp/gp", "$(B)/gp/gp.vet.txt"])
        self.assertIn(package["uid"], program["deps"])
        self.assertIn("$(B)/g/g.a", program["inputs"])
        produced = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertFalse(any("CGO_EXPORT" in out for out in produced))
        self.assertFalse(any(node["kv"]["p"] == "DX" for node in graph["graph"]))

    def test_go_package_sbom_component(self):
        graph = lib.make(fixture(sbom=True), "gp", *X86_64_RELEASE)
        component = lib.node_by_output(graph, "$(B)/g/g.GO.component.sbom")
        self.assertEqual(
            component["cmds"][0]["cmd_args"][2:],
            [
                "--output", "$(B)/g/g.GO.component.sbom",
                "--type", "library",
                "--path", "g",
                "--ver", "unknown",
                "--lang", "GO",
            ],
        )
        program = lib.node_by_output(graph, "$(B)/gp/gp")
        link_sbom = command_with(program, "link_sbom.py")
        self.assertEqual(link_sbom[2:4], ["--lang", "GO"])
        self.assertEqual(link_sbom[-1], "$(B)/g/g.GO.component.sbom")
        self.assertIn("$(B)/build/platform/lld/toolchain.component.sbom", link_sbom)

    def test_cpp_program_over_go_package_lists_lld_component_once(self):
        graph = lib.make(fixture(sbom=True), "cp", *X86_64_RELEASE)
        link_sbom = command_with(lib.node_by_output(graph, "$(B)/cp/cp"), "link_sbom.py")
        lld = "$(B)/build/platform/lld/toolchain.component.sbom"
        self.assertEqual(link_sbom.count(lld), 1)
        self.assertEqual(link_sbom[-2:], ["$(B)/cl/cl.CPP.component.sbom", "$(B)/cp/cp.CPP.component.sbom"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
