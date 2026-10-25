import json

from tst.make.lib import MakeTest, PROJECT


class TestDependenciesTest(MakeTest):
    def test_reject_nonfinal_multimodule_dependencies(self):
        self.write("run.py", "print(42)\n")
        variants = {
            "proto": 'PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n',
            "schema": 'PROTO_SCHEMA()\nONLY_TAGS(CPP_PROTO PY3_PROTO DESC_PROTO)\nSRCS(answer.proto)\nEND()\n',
            "python": 'PY23_LIBRARY()\nPY_SRCS(answer.py)\nEND()\n',
        }
        for name, content in variants.items():
            with self.subTest(name=name):
                self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/{name})\nRUN(python3 ${{ARCADIA_ROOT}}/{PROJECT}/run.py)\nEND()\n")
                self.write(name + "/ya.make", content)
                self.write(name + "/answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
                self.write(name + "/answer.py", "answer = 42\n")
                self.write(name + "/value.txt", "42\n")
                result = self.make("-t")
                self.assertNotEqual(result.returncode, 0, "DEPENDS must reject a multimodule without final targets")

    def test_union_dependencies(self):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/bundle)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/tool/answer)\nEND()\n")
        self.write("bundle/ya.make", f"UNION()\nPEERDIR({PROJECT}/tool {PROJECT}/library)\nFILES(value.txt)\nEND()\n")
        self.write("bundle/value.txt", "42\n")
        self.write("tool/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.write("library/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("library/answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph("-t"), "tool/answer")

    def test_remote_canonical_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    return 42\n")
        resources = [
            {"uri": "https://storage.yandex-team.ru/get-devtools/123456/answer#part", "checksum": "a1b2"},
            {"uri": "https://{canondata_backend}/123456/answer", "checksum": "a1b2"},
            {"uri": "sbr://123456789/answer.txt", "checksum": "a1b2"},
        ]
        self.write("canondata/result.json", json.dumps({"test_answer.test_answer": {"nested": resources * 2, "value": 42}}))
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn("fetch_from_mds.py", commands)
        self.assertIn("123456/answer", commands)

    def test_docker_images(self):
        digest = "a" * 64
        images = [f"docker://registry.example.com/answer@sha256:{digest}=" + "tag" + "x" * length for length in (0, 3, 12, 30, 48, 90)]
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nDOCKER_IMAGE(" + " ".join(images) + ")\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert True\n")
        graph = self.graph("-t")
        nodes = [node for node in graph["graph"] if "fetch_from_docker_repo.py" in json.dumps(node.get("cmds", []))]
        self.assertEqual(len(nodes), len(images))
