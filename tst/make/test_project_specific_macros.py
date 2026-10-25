import unittest

from tst import lib


BARE = lib.BARE_MODULE


def library(files, path, srcs=""):
    files[f"{path}/ya.make"] = f"LIBRARY()\n{BARE}{srcs}END()\n"


class CompileNlgTest(unittest.TestCase):
    def test_compile_nlg_generates_and_compiles_the_nlg_library(self):
        files = {}
        lib.tool_program(files, "alice/nlg/tools/codegen", "nlg")
        library(files, "alice/nlg/library/nlg_project_runtime", "SRCS(r.cpp)\n")
        files["alice/nlg/library/nlg_project_runtime/r.cpp"] = "int r;\n"
        files["a/nlg/ya.make"] = (
            f"LIBRARY()\n{BARE}COMPILE_NLG(\n    NLG_CONFIG nlg_config.json\n    TRANSLATIONS_JSON translations.json\n"
            "    names.nlg\n    v2/more.nlg\n)\nEND()\n"
        )
        for name in ("nlg_config.json", "translations.json", "names.nlg", "v2/more.nlg"):
            files[f"a/nlg/{name}"] = "\n"
        graph = lib.make(files, "a/nlg")
        b, s = "$(B)/a/nlg", "$(S)/a/nlg"
        run = lib.node_by_output(graph, f"{b}/dummy.h")
        self.assertEqual(run["kv"]["p"], "PR")
        self.assertEqual(run["cmds"][0]["cmd_args"], [
            "$(B)/alice/nlg/tools/codegen/nlg", "compile-cpp", "--import-dir", "$(S)", "--out-dir", "$(B)",
            "--include-prefix", "a/nlg", "--nlg-config-json", f"{s}/nlg_config.json",
            "--translations-json", f"{s}/translations.json", f"{s}/names.nlg", f"{s}/v2/more.nlg",
        ])
        self.assertEqual(run["outputs"][:3], [f"{b}/dummy.h", f"{b}/dummy.cpp", f"{b}/localizable_phrases.pb.txt"])
        self.assertIn(f"{b}/v2/more.nlg.pb.txt", run["outputs"])
        for obj in ("names.nlg.cpp.o", "_/v2/more.nlg.cpp.o", "nlg_renderer_factory.cpp.o", "translations.cpp.o",
                    "dummy.cpp.o", "register.cpp.o"):
            lib.node_by_output(graph, f"{b}/{obj}")
        lib.node_by_output(graph, "$(B)/a/nlg/liba-nlg.global.a")
        archive = lib.node_by_output(graph, "$(B)/a/nlg/liba-nlg.a")
        objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".cpp.o")]
        self.assertEqual(objects, [f"{b}/{name}.cpp.o" for name in (
            "nlg_renderer_factory", "translations", "names.nlg", "_/v2/more.nlg", "dummy",
        )])
        for output in objects:
            compiled = lib.node_by_output(graph, output)
            self.assertIn(run["uid"], compiled["deps"])
            self.assertIn(compiled["uid"], archive["deps"])


class ConfDefinedProtoPluginTest(unittest.TestCase):
    def test_plugin_deps_follow_the_macro_definition_in_conf(self):
        files = {
            "build/internal/conf/project_specific/alice.conf": (
                "macro ALICE_CAPABILITY() {\n"
                "    CPP_PROTO_PLUGIN(alice_capability_cpp yandex_io/tools/capability_gen .cap.h DEPS dep/capability)\n"
                "}\n"
            ),
            "build/internal/conf/project_specific/yabs.conf": (
                'macro YAFF(NAMESPACE="NProtoYaFF", FILES[], EXPERIMENTAL[]) {\n'
                "    CPP_PROTO_PLUGIN2(yaff $YAFF_PLUGIN_PATH .yaff.h .yaff.cpp DEPS dep/yaff EXTRA_OUT_FLAG x)\n"
                "}\n"
            ),
            "a/ya.make": f"PROTO_LIBRARY()\n{BARE}ALICE_CAPABILITY()\nYAFF()\nSRCS(x.proto)\nEND()\n",
            "a/x.proto": 'syntax = "proto3";\n',
            "b/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nSRCS(b.cpp)\nEND()\n",
            "b/b.cpp": "int b;\n",
        }
        for path in ("contrib/libs/protobuf", "dep/capability", "dep/yaff"):
            library(files, path, "SRCS(x.cpp)\n")
            files[f"{path}/x.cpp"] = "int x;\n"
        for path in ("contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide",
                     "yandex_io/tools/capability_gen", "library/cpp/yaff/tools/protoc_plugin"):
            lib.tool_program(files, path, path.rsplit("/", 1)[-1])
        graph = lib.make(files, "b", "-k")
        lib.node_by_output(graph, "$(B)/dep/capability/x.cpp.o")
        lib.node_by_output(graph, "$(B)/dep/yaff/x.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
