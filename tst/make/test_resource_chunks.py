from tst.make.lib import MakeTest, PROJECT


class ResourceChunksTest(MakeTest):
    def test_python_resource_chunk_object_order(self):
        files = []
        for index in range(100):
            name = f"folder{index:03d}_" + "x" * 65 + ("/conftest.py" if index % 3 == 0 else "/module.py")
            files.append(name)
            self.write(name, "VALUE = 42\n")
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(\n" + "\n".join(files) + "\n)\nEND()\n")
        graph = self.graph()
        archive = next(node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".global.a") for path in node["outputs"]))
        objects = [arg for command in archive["cmds"] for arg in command["cmd_args"] if arg.endswith(".o") and f"/{PROJECT}/" in arg]
        raw = [index for index, path in enumerate(objects) if path.endswith("_raw.auxcpp.o")]
        copied = [index for index, path in enumerate(objects) if "/objcopy_" in path]
        self.assertGreaterEqual(len(raw), 2)
        self.assertGreaterEqual(len(copied), 2)
        self.assertLess(raw[0], copied[-1])

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
