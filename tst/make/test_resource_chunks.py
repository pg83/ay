from tst.make.lib import MakeTest, PROJECT


class ResourceChunksTest(MakeTest):
    def test_multiple_resource_chunks(self):
        self.write("value.txt", "42\n")
        resources = " ".join(f"value.txt /value/{i:04d}/" + "x" * 90 for i in range(160))
        self.write("ya.make", f"LIBRARY()\nRESOURCE({resources})\nEND()\n")
        for platform in ("default-linux-x86_64", "default-darwin-arm64", "default-win-x86_64"):
            with self.subTest(platform=platform):
                graph = self.graph("--target-platform=" + platform)
                self.assertTrue(any(f"$(S)/{PROJECT}/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_resource_literal_chunks(self):
        resources = " ".join(f"- e2e.{i:04d}=" + "x" * 90 for i in range(160))
        self.write("ya.make", f"LIBRARY()\nRESOURCE({resources})\nEND()\n")
        self.graph("--target-platform=default-darwin-arm64")
