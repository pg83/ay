from tst.make.lib import MakeTest, PROJECT


class CollectionContextsTest(MakeTest):
    def test_bundled_resource_does_not_export_copy_script_to_link(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nBUNDLE({PROJECT}/tool NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        self.assertIn("$(S)/build/scripts/fs_tools.py", self.node(graph, "bundled")["inputs"])
        self.assertNotIn("$(S)/build/scripts/fs_tools.py", self.node(graph, "answer")["inputs"])

    def proto(self):
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer { int32 value = 1; }\n')

    def test_union_peer_tags(self):
        self.proto()
        self.write("ya.make", f"UNION()\nSET(PEERDIR_TAGS GO_PROTO)\nPEERDIR({PROJECT}/proto)\nEXPLICIT_OUTPUTS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/proto/answer.pb.go)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(f"$(B)/{PROJECT}/proto/answer.pb.go" in node["outputs"] and not any(path.endswith(".pkg.fake") for path in node["outputs"]) for node in graph["graph"]))
        nodes = {node["uid"]: node for node in graph["graph"]}
        pending = list(graph["result"])
        reachable = set()
        while pending:
            uid = pending.pop()
            if uid in reachable:
                continue
            reachable.add(uid)
            pending.extend(nodes[uid].get("deps", []))
        self.assertFalse(any(path.endswith("/answer.pb.cc") for uid in reachable for path in nodes[uid]["outputs"]))

    def test_appended_peer_tags_preserve_defaults(self):
        self.proto()
        self.write("main.cpp", f'#include "{PROJECT}/proto/answer.pb.h"\nint main() {{ return 0; }}\n')
        self.write("use.proto", f'syntax = "proto3";\nimport "{PROJECT}/proto/answer.proto";\nmessage Use {{ e2e.Answer value = 1; }}\n')
        for module, body in (("PROGRAM", "SRCS(main.cpp)"), ("PROTO_LIBRARY", "ONLY_TAGS(CPP_PROTO)\nSRCS(use.proto)"), ("UNION", f"EXPLICIT_OUTPUTS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/proto/answer.pb.h)")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}()\nSET_APPEND(PEERDIR_TAGS CPP_ROS)\nPEERDIR({PROJECT}/proto)\n{body}\nEND()\n")
                self.node(self.graph(), "proto/answer.pb.cc")

    def test_union_explicit_outputs(self):
        self.write("ya.make", f"UNION()\nPEERDIR({PROJECT}/files)\nEXPLICIT_OUTPUTS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/files/answer.txt)\nEND()\n")
        self.write("files/ya.make", "UNION()\nCOPY_FILE(source.txt answer.txt)\nEND()\n")
        self.write("files/source.txt", "42\n")
        graph = self.graph()
        root = self.node(graph, "__ay_e2e__.pkg.fake")
        self.assertIn(f"$(B)/{PROJECT}/files/answer.txt", root["outputs"])

    def test_package_default_peer_tags(self):
        self.write("ya.make", "PACKAGE()\nRUN_PYTHON3(generate.py $PEERDIR_TAGS OUT tags.txt)\nEND()\n")
        self.write("generate.py", "print('tags')\n")
        commands = [command["cmd_args"] for node in self.graph()["graph"] for command in node["cmds"] if any(arg.endswith(f"/{PROJECT}/generate.py") for arg in command["cmd_args"])]
        self.assertTrue(commands)
        for args in commands:
            self.assertIn("PY2", args)
            self.assertIn("GO_PROGRAM", args)

    def test_bundle_python_program_does_not_expand_program_inputs(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/tool NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("tool/ya.make", "PY3_PROGRAM(tool)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "print(42)\n")
        node = self.node(self.graph(), "bundled")
        self.assertEqual([value for value in node["inputs"] if value.startswith("$(B)/")], [f"$(B)/{PROJECT}/tool/tool"])
