from tst.make.lib import MakeTest, PROJECT


class GenerateModesTest(MakeTest):
    def test_make_zero_jobs(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        result = self.make(dump_graph=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.project / "answer").exists(), "-j0 must not compile the program")

    def test_multiple_targets(self):
        self.write("ya.make", "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("second/ya.make", "PROGRAM(second)\nSRCS(main.cpp)\nEND()\n")
        self.write("second/main.cpp", "int main() { return 0; }\n")
        graph = self.graph(f"{PROJECT}/second")
        self.assertIn(self.node(graph, "second/second")["uid"], graph["result"])
        self.assertIn(self.node(graph, "libanswer.a")["uid"], graph["result"])

    def test_cross_platform_dll(self):
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        for platform, options, output in (
            ("default-linux-x86_64", "", "libanswer.so"),
            ("default-darwin-arm64", "", "libanswer.dylib"),
            ("default-win-x86_64", "", "answer.dll"),
            ("default-win-x86_64", "PREFIX custom", "customanswer.dll"),
        ):
            with self.subTest(platform=platform, options=options):
                self.write("ya.make", f"DLL(answer {options})\nSRCS(answer.cpp)\nEND()\n")
                graph = self.graph("--target-platform=" + platform)
                self.assertIn(self.node(graph, output)["uid"], graph["result"])
