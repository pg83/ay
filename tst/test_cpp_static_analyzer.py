import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def resource(name, number):
    return f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({name} sbr:{number})\nEND()\n"


def files(settings):
    return {
        "lib/ya.make": (
            "LIBRARY()\n" + BARE
            + "PEERDIR(build/platform/clang build/platform/python/ymake_python3)\n"
            + settings
            + "CFLAGS(-DCOMMON=1)\nCXXFLAGS(-DCXX=1)\nCLANG_WARNINGS(-Wno-unused-function)\n"
            "SRC(extra.cpp -DEXTRA=1)\nSRCS(plain.cpp plain.c)\n"
            "RUN_PYTHON3(generate.py OUT generated.cpp)\nEND()\n"
        ),
        "lib/extra.cpp": "",
        "lib/plain.cpp": "",
        "lib/plain.c": "",
        "lib/generate.py": "",
        "cfg/analysis.json": "{}\n",
        "build/scripts/clang_static_analyzer.py": "",
        "build/platform/clang/ya.make": resource("CLANG20", 101),
        "build/platform/python/ymake_python3/ya.make": resource("YMAKE_PYTHON3", 102),
        "build/platform/clang/clang-static-analyzer/ya.make": resource("CLANG_CSA16", 103),
        "plugins/ya.make": (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(SA_ONE sbr:104 SA_TWO sbr:105)\nEND()\n"
        ),
    }


SELECT_CONFIG = "SELECT_CLANG_SA_CONFIG(${ARCADIA_ROOT}/cfg/analysis.json)\n"
PLUGINS = "USE_SA_PLUGINS(FROM plugins NAME SA_ONE SA_TWO)\n"


class CppStaticAnalyzerTest(unittest.TestCase):
    def test_static_analyzer_precedes_cpp_compilation_with_module_resources(self):
        graph = lib.make(files("ENABLE(CLANG_SA_ENABLE)\n" + SELECT_CONFIG + PLUGINS), "lib")
        for name in ("extra.cpp", "plain.cpp", "generated.cpp"):
            with self.subTest(name=name):
                node = lib.node_by_output(graph, "$(B)/lib/" + name + ".o")
                self.assertEqual(len(node["cmds"]), 2)
                analyzer, compiler = node["cmds"]
                source = ("$(B)" if name == "generated.cpp" else "$(S)") + "/lib/" + name
                head = [
                    "$(B)/resources/YMAKE_PYTHON3/bin/python3",
                    "$(S)/build/scripts/clang_static_analyzer.py", "--testing-src", source,
                    "--clang-bin", "$(B)/resources/CLANG_CSA16/bin/clang", "--source-root", "$(S)",
                    "--config-file", "$(S)/cfg/analysis.json", "--plugins-begin", "dummy_param",
                    "$(B)/resources/SA_ONE/plugin.so", "$(B)/resources/SA_TWO/plugin.so", "--plugins-end",
                ]
                self.assertEqual(analyzer["cmd_args"][:len(head)], head)
                self.assertEqual(analyzer["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
                for flag in ("-DCOMMON=1", "-DCXX=1", "-I$(B)", "-I$(S)"):
                    self.assertIn(flag, analyzer["cmd_args"])
                    self.assertIn(flag, compiler["cmd_args"])
                self.assertNotIn("-Wno-unused-function", analyzer["cmd_args"])
                self.assertIn("-Wno-unused-function", compiler["cmd_args"])
                self.assertNotIn("-Wno-builtin-macro-redefined", analyzer["cmd_args"])
                self.assertIn("-Wno-builtin-macro-redefined", compiler["cmd_args"])
                self.assertEqual(analyzer["cmd_args"][-1:] == ["-DEXTRA=1"], name == "extra.cpp")
                for input_ in ("$(S)/build/scripts/clang_static_analyzer.py", "$(S)/cfg/analysis.json"):
                    self.assertIn(input_, node["inputs"])
                for resource_name in ("CLANG_CSA16", "SA_ONE", "SA_TWO"):
                    fetch = lib.node_by_output(graph, "$(B)/resources/" + resource_name)
                    self.assertIn(fetch["uid"], node["deps"])
        plain_c = lib.node_by_output(graph, "$(B)/lib/plain.c.o")
        self.assertEqual(len(plain_c["cmds"]), 1)
        self.assertNotIn("$(S)/cfg/analysis.json", plain_c["inputs"])

    def test_generated_configuration_activates_its_registry_producer(self):
        settings = (
            "ENABLE(CLANG_SA_ENABLE)\n"
            "RUN_PYTHON3(generate_config.py OUT_NOAUTO analysis.json)\n"
            "SELECT_CLANG_SA_CONFIG(${BINDIR}/analysis.json)\n"
        )
        tree = files(settings)
        tree["lib/generate_config.py"] = ""
        graph = lib.make(tree, "lib")
        producers = [n for n in graph["graph"] if "$(B)/lib/analysis.json" in n["outputs"]]
        self.assertEqual(len(producers), 1)
        self.assertEqual(producers[0]["kv"]["p"], "PY")
        self.assertIn("$(S)/lib/generate_config.py", producers[0]["inputs"])
        for name in ("extra.cpp", "plain.cpp", "generated.cpp"):
            with self.subTest(name=name):
                node = lib.node_by_output(graph, "$(B)/lib/" + name + ".o")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--config-file") + 1], "$(B)/lib/analysis.json")
                self.assertIn("$(B)/lib/analysis.json", node["inputs"])
                self.assertIn(producers[0]["uid"], node["deps"])

    def test_empty_analyzer_resource_retains_the_configured_command(self):
        tree = files("ENABLE(CLANG_SA_ENABLE)\n" + SELECT_CONFIG)
        tree["build/platform/clang/clang-static-analyzer/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
        node = lib.node_by_output(lib.make(tree, "lib"), "$(B)/lib/plain.cpp.o")
        self.assertEqual(len(node["cmds"]), 2)
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--clang-bin") + 1], "/bin/clang")

    def test_analyzer_macros_reject_malformed_arguments(self):
        for settings, message in (
            ("SELECT_CLANG_SA_CONFIG()\n", "expects one configuration file"),
            ("SELECT_CLANG_SA_CONFIG(a b)\n", "expects one configuration file"),
            ("USE_SA_PLUGINS(unlabelled)\n", "expects FROM and NAME sections"),
        ):
            with self.subTest(settings=settings):
                result = lib.make_process(files(settings), "lib")
                self.assertEqual(result.returncode, 1)
                self.assertIn(message, result.stderr)

    def test_static_analyzer_requires_enable_and_configuration(self):
        for settings in (
            SELECT_CONFIG,
            "ENABLE(CLANG_SA_ENABLE)\n",
            "ENABLE(CLANG_SA_ENABLE)\n" + SELECT_CONFIG + "DISABLE(CLANG_SA_ENABLE)\n",
        ):
            with self.subTest(settings=settings):
                node = lib.node_by_output(lib.make(files(settings), "lib"), "$(B)/lib/plain.cpp.o")
                self.assertEqual(len(node["cmds"]), 1)
                self.assertNotIn("$(S)/cfg/analysis.json", node["inputs"])

    def test_static_analyzer_without_plugins_and_with_direct_config_binding(self):
        settings = (
            "ENABLE(CLANG_SA_ENABLE)\nSET(_CLANG_SA_CONFIG ${ARCADIA_ROOT}/cfg/analysis.json)\n"
            "USE_SA_PLUGINS()\n"
        )
        node = lib.node_by_output(lib.make(files(settings), "lib"), "$(B)/lib/plain.cpp.o")
        self.assertEqual(len(node["cmds"]), 2)
        args = node["cmds"][0]["cmd_args"]
        start, end = args.index("--plugins-begin"), args.index("--plugins-end")
        self.assertEqual(args[start:end + 1], ["--plugins-begin", "dummy_param", "--plugins-end"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
