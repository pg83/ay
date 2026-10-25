import json

from tst.make.lib import MakeTest, PROJECT


class CommandOptionsTest(MakeTest):
    def test_output_and_platform_options(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        variants = (
            ("--build=debug", "--stat", "--verbose"),
            ("--build=release", "--host-build-type=debug", "--host-platform-flag=E2E=yes", "--target-platform-flag=E2E=yes"),
            ("--target-platform=default-linux-x86_64", "--target-platform-build-type=release", "--host-platform=default-linux-x86_64"),
            ("--output=" + str(self.work / "output"), "--build-dir=" + str(self.work / "build")),
            ("--musl",),
            ("--source-root=" + str(self.root), "-DE2E=yes", "--keep-going", "--no-src-links", "--no-yt-store"),
        )
        for flags in variants:
            with self.subTest(flags=flags):
                self.node(self.graph(*flags), "answer")

    def test_package_result_options(self):
        self.write("ya.make", f"PACKAGE(answer)\nPEERDIR({PROJECT}/tool)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        result = self.make("--add-modules-to-results", "--strip-packages-from-results")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["result"], [])

    def test_invalid_options(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for flags in (("--jobs=bad",), ("--tests-retries=bad",), ("--unknown-e2e-option",)):
            with self.subTest(flags=flags):
                self.assertNotEqual(self.make(*flags).returncode, 0)
