from tst.make.lib import MakeTest, PROJECT


class ModuleSourceOptionsTest(MakeTest):
    def test_python_sanitizer_assignments_keep_the_source(self):
        for macro in ("SET(SANITIZER_TYPE address)", "SET_APPEND(SANITIZER_TYPE address)", "ENABLE(SANITIZER_TYPE)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY3_LIBRARY()\n{macro}\nPY_SRCS(answer.py)\nEND()\n")
                self.write("answer.py", "ANSWER = 42\n")
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] for node in self.graph()["graph"]))

    def test_global_python_source_is_packaged(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(GLOBAL answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] for node in self.graph()["graph"]))

    def test_gazetteer_sources_in_an_ordinary_python_library_are_ignored(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.py ignored.gztproto)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.write("ignored.gztproto", "package e2e; message Ignored {}\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] for node in graph["graph"]))
        self.assertFalse(any(path.startswith(f"$(B)/{PROJECT}/ignored") for node in graph["graph"] for path in node["outputs"]))

    def test_top_level_main_is_rejected_in_a_python_three_program(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(TOP_LEVEL __main__.py)\nEND()\n")
        self.write("__main__.py", "print(42)\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("__main__.py", result.stderr)

    def test_go_proto_ignores_python_sources_and_cpp_only_generators(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nPY_SRCS(ignored.py)\nGENERATE_ENUM_SERIALIZATION(answer.pb.h)\nYMAPS_SPROTO()\nSRCS(answer.proto)\nEND()\n")
        self.write("ignored.py", "ANSWER = 42\n")
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }}\n')
        graph = self.graph()
        self.node(graph, "answer.pb.go")
        outputs = [path for node in graph["graph"] for path in node["outputs"]]
        self.assertFalse(any(path.startswith(f"$(B)/{PROJECT}/") and ("serialized" in path or "ignored.py" in path) for path in outputs))

    def test_asmlib_enable_keeps_native_compilation(self):
        self.write("ya.make", "LIBRARY()\nENABLE(USE_ASMLIB)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertIn(f"$(S)/{PROJECT}/answer.cpp", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_global_go_linker_flags_reach_the_program(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nGO_LDFLAGS(-Wl,--as-needed)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        args = [arg for command in self.node(self.graph(), "answer")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Wl,--as-needed", args)

