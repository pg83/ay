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
