from tst.make.lib import MakeTest, PROJECT


class GoGenerationInputsTest(MakeTest):
    def test_indented_standard_library_version_assignment_is_valid(self):
        config = (self.root / "build/conf/go.conf").read_text()
        self.assertIn("\nGOSTD_VERSION=", config)
        self.write_tree("build/conf/go.conf", config.replace("\nGOSTD_VERSION=", "\n    GOSTD_VERSION="))
        self.write("ya.make", "GO_PROGRAM(answer)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", 'package main\nimport "fmt"\nfunc main() { fmt.Println(42) }\n')
        graph = self.graph()
        self.assertIn(self.node(graph, "answer")["uid"], graph["result"])

    def test_flatbuffers_namespace_maps_are_inherited_once_through_a_diamond(self):
        self.write("ya.make", f"GO_LIBRARY()\nPEERDIR({PROJECT}/left {PROJECT}/right)\nSRCS(answer.fbs)\nEND()\n")
        self.write("answer.fbs", 'include "base/base.fbs";\nnamespace e2e; table Answer { value: shared.Base; } root_type Answer;\n')
        self.write("base/ya.make", f"GO_LIBRARY()\nFBS_NAMESPACE(shared a.yandex-team.ru/{PROJECT}/base)\nSRCS(base.fbs)\nEND()\n")
        self.write("base/base.fbs", "namespace shared; table Base { value: int; }\n")
        for name in ("left", "right"):
            self.write(name + "/ya.make", f"GO_LIBRARY()\nPEERDIR({PROJECT}/base)\nSRCS(value.go)\nEND()\n")
            self.write(name + "/value.go", f"package {name}\nconst Answer = 42\n")
        generated = self.node(self.graph(), "answer.fbs.gosrc")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args.count(f"shared=a.yandex-team.ru/{PROJECT}/base"), 1)

    def test_explicit_generated_go_source_keeps_its_producer(self):
        self.write("ya.make", "GO_LIBRARY()\nCOPY_FILE(template.go generated.go)\nSRCS(${BINDIR}/generated.go)\nEND()\n")
        self.write("template.go", "package answer\nfunc Answer() int { return 42 }\n")
        graph = self.graph()
        generated = self.node(graph, "generated.go")
        archive = self.node(graph, f"{PROJECT}.a")
        self.assertIn(f"$(B)/{PROJECT}/generated.go", archive["inputs"])
        self.assertIn(generated["uid"], archive["deps"])

    def test_missing_go_source_is_a_configuration_error(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(missing.go)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_global_native_source_is_linked_into_the_go_program(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nSRCS(main.go GLOBAL native.cpp)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("native.cpp", 'extern "C" int native_answer() { return 42; }\n')
        graph = self.graph()
        native = [node for node in graph["graph"] if f"$(S)/{PROJECT}/native.cpp" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
        self.assertEqual(len(native), 1)
        binary = self.node(graph, "answer")
        self.assertIn(native[0]["uid"], binary["deps"])
        self.assertTrue(set(native[0]["outputs"]) & set(binary["inputs"]))

    def test_shared_library_exports_a_c_header(self):
        self.write("ya.make", "GO_DLL(answer)\nCGO_SRCS(answer.go)\nEND()\n")
        self.write("answer.go", '''package main
import "C"
//export Answer
func Answer() C.int { return 42 }
func main() {}
''')
        graph = self.graph()
        nodes = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".so") for path in node["outputs"])]
        self.assertEqual(len(nodes), 1)
        node = nodes[0]
        library = next(path for path in node["outputs"] if path.endswith(".so"))
        self.assertIn(library[:-3] + ".h", node["outputs"])
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("dll", args)
        self.assertTrue(any(arg.endswith("/_cgo_export.h") for arg in args))

    def test_generator_go_inputs_keep_imported_go_dependencies(self):
        self.write("ya.make", f"GO_LIBRARY()\nRUN_PY3_PROGRAM({PROJECT}/generator ${{ARCADIA_ROOT}}/{PROJECT}/data.go ${{BINDIR}}/generated.go IN data.go OUT generated.go)\nEND()\n")
        self.write("data.go", f'package generated\nimport "a.yandex-team.ru/{PROJECT}/dependency"\nfunc Answer() int {{ return dependency.Answer() }}\n')
        self.write("dependency/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("dependency/answer.go", "package dependency\nfunc Answer() int { return 42 }\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "import pathlib, sys\npathlib.Path(sys.argv[2]).write_text(pathlib.Path(sys.argv[1]).read_text())\n")
        graph = self.graph()
        generated = self.node(graph, "generated.go")
        self.assertIn(f"$(S)/{PROJECT}/data.go", generated["inputs"])
        archive = self.node(graph, f"{PROJECT}.a")
        self.assertIn(f"$(B)/{PROJECT}/dependency/dependency.a", archive["inputs"])
        self.assertIn(generated["uid"], archive["deps"])

    def test_missing_go_import_is_a_configuration_error(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", f'package answer\nimport _ "a.yandex-team.ru/{PROJECT}/missing"\n')
        self.assertNotEqual(self.make().returncode, 0)
