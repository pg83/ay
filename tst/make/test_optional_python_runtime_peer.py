from tst.make.lib import MakeTest


class OptionalPythonRuntimePeerTest(MakeTest):
    def test_configured_interpreter_works_without_the_default_runtime_module(self):
        relative = "build/platform/python/ymake_python3/ya.make"
        if self.backend == "ay":
            self.private_directory("build/platform/python/ymake_python3")
        runtime = self.root / relative
        original = runtime.read_bytes()
        runtime.unlink()
        if self.backend == "ya":
            self.addCleanup(runtime.write_bytes, original)
        self.write("ya.make", "UNION(answer)\nNO_YMAKE_PYTHON3()\nSET(NEED_PLATFORM_PEERDIRS no)\nRUN_PYTHON3(generate.py STDOUT result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        graph = self.graph("-DBUILD_PYTHON3_BIN=python3")
        generated = [node for node in graph["graph"] if any(path.endswith("/result.txt") for path in node["outputs"])
                     and not any(path.endswith(".pkg.fake") for path in node["outputs"])]
        self.assertEqual(len(generated), 1)
        self.assertEqual(generated[0]["cmds"][0]["cmd_args"][0], "python3")
