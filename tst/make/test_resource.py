import base64
import hashlib

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

    def test_sandbox_repeated_output_is_one_output(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO keys.json keys.json)\nRESOURCE(keys.json /keys)\nEND()\n")
        node = self.node(self.graph(), "keys.json")
        self.assertEqual(node["outputs"], [f"$(B)/{PROJECT}/keys.json"])
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--") + 1:args.index("--ya-end-command-file")], ["keys.json", "keys.json"])

    def test_objcopy_large_resource_threshold(self):
        self.write("ya.make", "PY3_LIBRARY()\nSET(YMAKE_OBJCOPY_LARGE_RESOURCE_THR 0)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        graph = self.graph()
        objcopy = [node["cmds"][0]["cmd_args"] for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/objcopy_") for path in node["outputs"])]
        self.assertTrue(objcopy)
        for args in objcopy:
            target = args.index("--target")
            self.assertEqual(args[target + 2:target + 4], ["--large_resource_thr", "0"])

    def test_resources_library_objects_link_into_program(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nRESOURCE(own.txt /e2e/own)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("own.txt", "own\n")
        self.write("lib/ya.make", f"LIBRARY()\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/res)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        self.write("res/ya.make", "RESOURCES_LIBRARY()\nRESOURCE(query.sql /e2e/query)\nEND()\n")
        self.write("res/query.sql", "SELECT 42;\n")
        graph = self.graph()
        producers = [node for node in graph["graph"] if f"$(S)/{PROJECT}/res/query.sql" in node["inputs"]]
        self.assertEqual(len(producers), 1)
        peer_obj = producers[0]["outputs"][0]
        self.assertTrue(peer_obj.startswith(f"$(B)/{PROJECT}/res/objcopy_"), peer_obj)
        own = [node for node in graph["graph"] if f"$(S)/{PROJECT}/own.txt" in node["inputs"]]
        self.assertEqual(len(own), 1)
        own_obj = own[0]["outputs"][0]
        executable = self.node(graph, "answer")
        self.assertIn(peer_obj, executable["inputs"])
        self.assertIn(producers[0]["uid"], executable["deps"])
        args = [arg for command in executable["cmds"] for arg in command["cmd_args"]]
        rel = lambda path: path.removeprefix("$(B)/")
        start = args.index("-Wl,--no-whole-archive")
        self.assertEqual(args[start + 1:start + 3], [rel(peer_obj), rel(own_obj)])
        archive = next(node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/lib/") and path.endswith(".a") for path in node["outputs"]))
        self.assertNotIn(peer_obj, archive["inputs"])

    def test_resources_library_object_name_hashes_module_tag(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/res)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("res/ya.make", "RESOURCES_LIBRARY()\nRESOURCE(query.sql /e2e/query)\nEND()\n")
        self.write("res/query.sql", "SELECT 42;\n")
        graph = self.graph()
        key = base64.b64encode(b"/e2e/query").decode()
        hashed = ",".join(sorted(["query.sql", key, f"$S/{PROJECT}/res"])) + "RESOURCE_LIB"
        name = "objcopy_" + hashlib.md5(hashed.encode()).hexdigest()[:26] + ".o"
        self.node(graph, f"res/{name}")

    def test_resources_library_peers_link_transitively(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/res)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("res/ya.make", "RESOURCES_LIBRARY()\nRESOURCE(query.sql /e2e/query)\nEND()\n")
        self.write("res/query.sql", "SELECT 42;\n")
        executable = self.node(self.graph(), "answer")
        self.assertIn("$(B)/library/cpp/resource/liblibrary-cpp-resource.a", executable["inputs"])
