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

    def test_objcopy_large_resource_threshold(self):
        self.write("ya.make", "PY3_LIBRARY()\nSET(YMAKE_OBJCOPY_LARGE_RESOURCE_THR 0)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        graph = self.graph()
        objcopy = [node["cmds"][0]["cmd_args"] for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/objcopy_") for path in node["outputs"])]
        self.assertTrue(objcopy)
        for args in objcopy:
            target = args.index("--target")
            self.assertEqual(args[target + 2:target + 4], ["--large_resource_thr", "0"])
