import json

from tst.make.lib import MakeTest, PROJECT


class PeerRequestsTest(MakeTest):
    def test_explicit_tags_share_a_peer_without_aliasing_languages(self):
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer {}\n')
        for directory, tag, output in (("cpp", "CPP_PROTO", "answer.pb.cc"), ("go", "GO_PROTO", "answer.pb.go")):
            self.write(f"{directory}/ya.make", f"UNION()\nSET(PEERDIR_TAGS {tag})\nPEERDIR({PROJECT}/proto)\nEXPLICIT_OUTPUTS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/proto/{output})\nEND()\n")
        for order in ("cpp go", "go cpp"):
            with self.subTest(order=order):
                self.write("ya.make", f"RECURSE({order})\n")
                graph = self.graph()
                for output in ("answer.pb.cc", "answer.pb.go"):
                    producers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/proto/{output}" in node["outputs"] and not any(path.endswith(".pkg.fake") for path in node["outputs"])]
                    self.assertEqual(len(producers), 1, output)

    def test_shared_proto_variants_in_both_consumer_orders(self):
        self.write("cpp/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("cpp/main.cpp", "int main() { return 0; }\n")
        self.write("py/ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("py/main.py", "pass\n")
        self.write("go/ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("go/main.go", "package main\nfunc main() {}\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}/proto"; message Answer {{}}\n')
        for order in ("cpp py go", "go py cpp"):
            with self.subTest(order=order):
                self.write("ya.make", f"RECURSE({order})\n")
                graph = self.graph()
                self.node(graph, "proto/answer.pb.cc")
                self.node(graph, "proto/answer__intpy3___pb2.py")
                self.node(graph, "proto/answer.pb.go")

    def test_go_library_demands_its_peer(self):
        self.write("ya.make", f"GO_LIBRARY()\nSRCS(answer.go)\nPEERDIR({PROJECT}/go)\nEND()\n")
        self.write("answer.go", f'package answer\nimport _ "a.yandex-team.ru/{PROJECT}/go"\n')
        self.write("go/ya.make", f"GO_LIBRARY()\nSRCS(answer.go)\nPEERDIR({PROJECT}/dep)\nEND()\n")
        self.write("go/answer.go", f'package answer\nimport _ "a.yandex-team.ru/{PROJECT}/dep"\n')
        self.write("dep/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("dep/answer.go", "package dep\n")
        self.node(self.graph(), "dep/dep.a")

    def test_missing_peer_keeps_error_and_keep_going_output(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/missing)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        for flags in ((), ("-k",)):
            with self.subTest(flags=flags):
                result = self.make(*flags)
                self.assertEqual(result.returncode == 0, bool(flags), result.stderr)
                self.assertIn(f"{PROJECT}/missing", result.stderr)
                if flags:
                    graph = json.loads(result.stdout)
                    self.assertTrue(any(path.endswith(f"/{PROJECT}/main.cpp.o") for node in graph["graph"] for path in node["outputs"]))
