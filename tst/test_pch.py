import unittest

import lib
from test_go_package import go_world


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class PrecompiledHeaderTest(unittest.TestCase):
    def test_pch_producer_uses_cxx_flags_and_existing_include_closure(self):
        files = {
            "mod/ya.make": (
                f"LIBRARY()\n{BARE}PEERDIR(build/platform/clang compiler19)\nCFLAGS(-DLOCAL)\nCXXFLAGS(-DCXX)\nCLANG_WARNINGS(-Wcomment -Wunreachable-code)\n"
                "GEN_PCH_FILE(mod/private.h ${ARCADIA_BUILD_ROOT}/mod/private.h.pch)\n"
                "GEN_PCH_FILE(mod/unused.h ${ARCADIA_BUILD_ROOT}/mod/unused.h.pch)\n"
                "SRCS(main.cpp)\nEND()\n"
            ),
            "build/platform/clang/ya.make": lib.resource_lib("CLANG20", 1),
            "compiler19/ya.make": lib.resource_lib("CLANG19", 2),
            "build/ymake_conf.py": lib.ymake_conf(extra="COMPILER_VERSION=19\n"),
            "mod/main.cpp": '#include "public.h"\n',
            "mod/public.h": '#include "private.h"\n#if 0\n#include "mod/private.h.pch"\n#endif\n',
            "mod/private.h": '#include "leaf.h"\n',
            "mod/leaf.h": "#pragma once\n",
            "mod/unused.h": "#pragma once\n",
        }
        lib.tool_program(files, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
        graph = lib.make(files, "mod")
        pch = lib.node_by_output(graph, "$(B)/mod/private.h.pch")
        self.assertEqual(pch["kv"], {"p": "PC", "pc": "red"})
        args = pch["cmds"][0]["cmd_args"]
        self.assertEqual(args[:6], [
            "$(B)/sdg/tier0/tools/make_pch_wrapper/make_pch_wrapper", "$(B)/resources/CLANG20",
            "$(S)/mod/private.h", "$(B)/mod/private.h.pch", "$(B)", "$(S)",
        ])
        dependencies = [node["outputs"][0] for node in graph["graph"] if node["uid"] in pch["deps"]]
        self.assertIn("$(B)/resources/CLANG20", dependencies)
        self.assertNotIn("$(B)/resources/CLANG19", dependencies)
        self.assertIn("-DLOCAL", args)
        self.assertIn("-DCXX", args)
        self.assertIn("-std=c++20", args)
        self.assertNotIn("-c", args)
        self.assertNotIn("-Wcomment", args)
        self.assertNotIn("-Wunreachable-code", args)
        self.assertEqual(sorted(pch["inputs"]), [
            "$(B)/sdg/tier0/tools/make_pch_wrapper/make_pch_wrapper", "$(S)/mod/leaf.h", "$(S)/mod/private.h",
        ])
        cc = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
        self.assertIn(pch["uid"], cc["deps"])
        self.assertIn("$(B)/mod/private.h.pch", cc["inputs"])
        self.assertFalse(any("$(B)/mod/unused.h.pch" in node["outputs"] for node in graph["graph"]))

    def test_listed_headers_export_generated_includes_to_module_consumers(self):
        files = {
            "producer/ya.make": (
                f"LIBRARY()\n{BARE}"
                "GEN_PCH_FILE(producer/private.h ${ARCADIA_BUILD_ROOT}/producer/private.h.pch)\n"
                "SRCS(local.cpp public.h GLOBAL registry.cpp)\nEND()\n"
            ),
            "producer/local.cpp": "int local;\n",
            "producer/registry.cpp": '#include "public.h"\nint registry;\n',
            "producer/public.h": '#if 0\n#include "producer/private.h.pch"\n#endif\n#include "leaf.h"\n',
            "producer/private.h": '#include "leaf.h"\n',
            "producer/leaf.h": "#pragma once\n",
            "middle/ya.make": f"LIBRARY()\n{BARE}PEERDIR(producer)\nSRCS(middle.cpp)\nEND()\n",
            "middle/middle.cpp": "int middle;\n",
            "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR(middle)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "int main() {}\n",
        }
        lib.tool_program(files, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
        for header_listed in (True, False):
            with self.subTest(header_listed=header_listed):
                graph = lib.make(files, "consumer")
                link = lib.node_by_output(graph, "$(B)/consumer/consumer")
                pch = lib.node_by_output(graph, "$(B)/producer/private.h.pch")
                self.assertEqual("$(B)/producer/private.h.pch" in link["inputs"], header_listed)
                self.assertEqual(pch["uid"] in link["deps"], header_listed)
                self.assertNotIn("$(S)/producer/public.h", link["inputs"])
                self.assertNotIn("$(S)/producer/leaf.h", link["inputs"])
                archive = lib.node_by_output(graph, "$(B)/producer/libproducer.a")
                self.assertNotIn("$(B)/producer/private.h.pch", archive["inputs"])
                go_files = go_world()
                go_files.update(files)
                go_files["consumer/ya.make"] = f"GO_LIBRARY()\n{BARE}PEERDIR(middle)\nSRCS(main.go)\nEND()\n"
                go_files["consumer/main.go"] = "package consumer\n"
                go_graph = lib.make(go_files, "consumer")
                go_archive = lib.node_by_output(go_graph, "$(B)/consumer/consumer.a")
                self.assertEqual("$(B)/producer/private.h.pch" in go_archive["inputs"], header_listed)
                files["producer/ya.make"] = files["producer/ya.make"].replace("local.cpp public.h", "local.cpp")

    def test_source_global_inputs_cross_library_peers_only(self):
        files = {
            "producer/ya.make": f"LIBRARY()\n{BARE}SRCS(local.cpp GLOBAL plugin.pyplugin)\nEND()\n",
            "producer/local.cpp": "int local;\n",
            "producer/plugin.pyplugin": "def link(): pass\n",
            "middle/ya.make": f"LIBRARY()\n{BARE}PEERDIR(producer)\nSRCS(middle.cpp)\nEND()\n",
            "middle/middle.cpp": "int middle;\n",
            "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR(middle)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "int main() {}\n",
        }
        graph = lib.make(files, "consumer")
        link = lib.node_by_output(graph, "$(B)/consumer/consumer")
        self.assertIn("$(S)/producer/plugin.pyplugin", link["inputs"])
        self.assertIn("$(S)/producer/plugin.pyplugin", link["cmds"][2]["cmd_args"])
        go_files = go_world()
        go_files.update(files)
        go_files["consumer/ya.make"] = f"GO_PROGRAM()\n{BARE}PEERDIR(middle)\nSRCS(main.go)\nEND()\n"
        go_files["consumer/main.go"] = "package main\nfunc main() {}\n"
        go_link = lib.node_by_output(lib.make(go_files, "consumer"), "$(B)/consumer/consumer")
        self.assertIn("$(S)/producer/plugin.pyplugin", go_link["inputs"])
        go_args = next(cmd["cmd_args"] for cmd in go_link["cmds"] if "$(S)/build/scripts/go_tool.py" in cmd["cmd_args"])
        begin, end = go_args.index("++ld_plugins"), go_args.index("++vcs")
        self.assertEqual(go_args[begin + 1:end], ["$(S)/producer/plugin.pyplugin"])
        for output in ("$(B)/producer/libproducer.a", "$(B)/middle/libmiddle.a"):
            self.assertNotIn("$(S)/producer/plugin.pyplugin", lib.node_by_output(graph, output)["inputs"])

    def test_global_pch_flags_cross_peers_without_eagerly_using_outputs(self):
        files = {
            "producer/ya.make": (
                f"LIBRARY()\n{BARE}"
                "GEN_PCH_FILE(producer/private.h ${ARCADIA_BUILD_ROOT}/producer/private.h.pch)\n"
                "GEN_PCH_FILE(producer/unused.h ${ARCADIA_BUILD_ROOT}/producer/unused.h.pch)\nEND()\n"
            ),
            "producer/private.h": "#pragma once\n",
            "producer/unused.h": "#pragma once\n",
            "middle/ya.make": f"LIBRARY()\n{BARE}PEERDIR(producer)\nEND()\n",
            "consumer/ya.make": f"LIBRARY()\n{BARE}PEERDIR(middle)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "int main;\n",
        }
        lib.tool_program(files, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
        graph = lib.make(files, "consumer", "-DENABLE_UNSHARE_WRAPPER=yes", opensource=False)
        cc = lib.node_by_output(graph, "$(B)/consumer/main.cpp.o")
        self.assertEqual(cc["cmds"][0]["cmd_args"][:9], [
            "$(B)/resources/UNSHARE_WRAPPER/bin/wrap_unshare",
            "--include-pch=$(B)/producer/private.h.pch", "--include-pch=$(B)/producer/unused.h.pch",
            "--source-root", "$(S)", "--build-root", "$(B)", "--wrap_unshare-end",
            "$(B)/resources/YMAKE_PYTHON3/bin/python3",
        ])
        self.assertFalse(any(node["kv"].get("p") == "PC" for node in graph["graph"]))
        self.assertFalse(any(input.endswith(".pch") for input in cc["inputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
