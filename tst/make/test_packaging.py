from tst.make.lib import MakeTest, PROJECT


class PackagingTest(MakeTest):
    def peers(self):
        self.write("app/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("app/main.cpp", "int main() { return 0; }\n")
        self.write("files/ya.make", "UNION(data)\nFILES(value.txt)\nEND()\n")
        self.write("files/value.txt", "42\n")

    def test_package_peers(self):
        self.peers()
        self.write("ya.make", f"PACKAGE(answer)\nPEERDIR({PROJECT}/app {PROJECT}/files)\nEND()\n")
        graph = self.graph()
        self.node(graph, "app/answer")
        self.assertTrue(any(f"$(S)/{PROJECT}/files/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_package_takes_module_includes_of_peers(self):
        self.write("ya.make", f"PACKAGE()\nPEERDIR({PROJECT}/plugin {PROJECT}/header {PROJECT}/dll)\nEND()\n")
        self.write("plugin/ya.make", "DLL(plugin)\nSRCS(plugin.cpp)\nLD_PLUGIN(plugin.py)\nEND()\n")
        self.write("plugin/plugin.cpp", 'extern "C" int plugin() { return 42; }\n')
        self.write("plugin/plugin.py", "def plugin():\n    pass\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(a.proto b.proto)\nEND()\n")
        self.write("proto/a.proto", 'syntax = "proto3"; package e2e; message A { int32 value = 1; }\n')
        self.write("proto/b.proto", 'syntax = "proto3"; package e2e; message B { int32 value = 1; }\n')
        self.write("header/ya.make", f"DLL(header)\nSRCS(header.cpp api.h)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("header/header.cpp", 'extern "C" int header() { return 42; }\n')
        self.write("header/api.h", f"#pragma once\n#include <{PROJECT}/proto/a.pb.h>\n")
        self.write("lib/ya.make", f"LIBRARY()\nSRCS(lib.cpp api.h)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("lib/lib.cpp", "int lib() { return 42; }\n")
        self.write("lib/api.h", f"#pragma once\n#include <{PROJECT}/proto/b.pb.h>\n")
        self.write("dll/ya.make", f"DLL(dll)\nSRCS(dll.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("dll/dll.cpp", 'extern "C" int dll() { return 42; }\n')
        node = self.node(self.graph(), f"{PROJECT}.final.pkg.fake")
        self.assertIn(f"$(B)/{PROJECT}/plugin/plugin.py.pyplugin", node["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/proto/a.pb.h", node["inputs"])
        self.assertNotIn(f"$(B)/{PROJECT}/proto/b.pb.h", node["inputs"])

    def test_package_tar(self):
        self.peers()
        self.write("ya.make", f"PACKAGE(answer)\nPEERDIR({PROJECT}/app {PROJECT}/files)\nPACK(tar)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith((".tar", ".tar.gz")) for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_ci_group(self):
        self.peers()
        self.write("ya.make", f"CI_GROUP(answer)\nPEERDIR({PROJECT}/app)\nFILES(value.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        graph = self.graph()
        self.node(graph, "app/answer")
        self.assertTrue(any(path.endswith(".ci.pkg.fake") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_sandbox_archive(self):
        self.write("ya.make", "PACKAGE(data)\nFROM_SANDBOX(123456 OUT_NOAUTO value.txt)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(f"$(B)/{PROJECT}/value.txt" in node["outputs"] for node in graph["graph"]))

    def test_sandbox_file_rename(self):
        self.write("ya.make", "PACKAGE(data)\nFROM_SANDBOX(FILE 123456 RENAME value.txt OUT_NOAUTO value.txt)\nEND()\n")
        self.assertTrue(any(f"$(B)/{PROJECT}/value.txt" in node["outputs"] for node in self.graph()["graph"]))
