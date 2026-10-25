from tst.make.lib import MakeTest, PROJECT


class ResourceTest(MakeTest):
    def test_resource_and_resource_files_keep_source_inputs(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(message.txt /e2e/message)\nRESOURCE_FILES(PREFIX /e2e/ data.json)\nEND()\n")
        self.write("message.txt", "hello\n")
        self.write("data.json", '{"answer": 42}\n')
        graph = self.graph()
        for source in ("message.txt", "data.json"):
            with self.subTest(source=source):
                producers = [node for node in graph["graph"] if f"$(S)/{PROJECT}/{source}" in node["inputs"]]
                self.assertTrue(producers, f"no command consumes {source}")
                self.assertTrue(any(node["outputs"] for node in producers))
