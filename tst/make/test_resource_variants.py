from tst.make.lib import MakeTest, PROJECT


class ResourceVariantsTest(MakeTest):
    def test_resource_conftest(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(conftest.py /conftest.py - /inline=answer)\nEND()\n")
        self.write("conftest.py", "ANSWER = 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/conftest.py" in node["inputs"] for node in graph["graph"]))

    def test_resource_uncompressed(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(DONT_COMPRESS conftest.py /conftest.py)\nEND()\n")
        self.write("conftest.py", "ANSWER = 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/conftest.py" in node["inputs"] for node in graph["graph"]))

    def test_resource_file_extension(self):
        self.write("ya.make", "LIBRARY()\nALL_RESOURCE_FILES(txt PREFIX /data/ data data/deep)\nEND()\n")
        self.write("data/one.txt", "1\n")
        self.write("data/deep/two.txt", "2\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/data/deep/two.txt", inputs)

    def test_resource_generated_header(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(value.h generated.h)\nRESOURCE(generated.h /generated.h)\nEND()\n")
        self.write("value.h", '#include "dependency.h"\n')
        self.write("dependency.h", "#define VALUE 42\n")
        graph = self.graph()
        producer = self.node(graph, "generated.h")
        consumers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.h" in node["inputs"]]
        self.assertTrue(consumers)
        self.assertTrue(any(producer["uid"] in node.get("deps", []) for node in consumers))
