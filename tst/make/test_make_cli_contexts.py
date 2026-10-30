import json
import os
import subprocess

from tst.make.lib import MakeTest, PROJECT


class MakeCliContextsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_BUILD=${BUILD_TYPE})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_explicit_target_options_keep_additional_targets(self):
        self.write("child/ya.make", "LIBRARY()\nSRCS(child.cpp)\nEND()\n")
        self.write("child/child.cpp", "int child() { return 17; }\n")
        for flag in ("-C", "--target"):
            with self.subTest(flag=flag):
                graph = self.graph(flag, f"{PROJECT}/child")
                self.node(graph, "answer.cpp.o")
                self.node(graph, "child/child.cpp.o")

    def test_debug_options_select_the_debug_build_configuration(self):
        generated = self.node(self.graph("-d"), "answer.cpp.o")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_BUILD=DEBUG", args)

    def invoke_without_targets(self, cwd):
        args = [self.binary, "make", "-G", "-j0", "--sandboxing"]
        if self.backend == "ya":
            args += ["--build-dir", str(self.work / "build"), "-xx", "--no-locked", "--no-prefetch",
                     "--no-ymake-tool-servermode", "--no-ymake-pic-servermode", "--no-ymake-multiconfig"]
        return subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=120,
                              env={**os.environ, "YA_TC": "no", "YA_NO_RESPAWN": "yes"})

    def test_the_current_directory_is_the_default_build_target(self):
        result = self.invoke_without_targets(self.project)
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        output = f"$(B)/{PROJECT}/answer.cpp.o"
        producers = [node for node in graph["graph"] if output in [p.replace("$(BUILD_ROOT)", "$(B)") for p in node["outputs"]]]
        self.assertEqual(len(producers), 1)

    def test_unmarked_source_trees_are_configuration_errors(self):
        directory = self.work / "unmarked"
        directory.mkdir()
        (directory / "ya.make").write_text("LIBRARY()\nEND()\n")
        self.assertNotEqual(self.invoke_without_targets(directory).returncode, 0)
