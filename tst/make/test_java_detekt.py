import json

from tst.make.lib import MakeTest, PROJECT


class JavaDetektTest(MakeTest):
    def test_detekt_from_linters_include_with_rooted_sources(self):
        self.write_tree("build/conf/autoincludes.json", json.dumps([PROJECT]))
        self.write("linters.make.inc", "STYLE_DETEKT(CONFIG_TYPE detekt-config.yml detekt-baseline.xml)\n")
        self.write("detekt-config.yml", "build:\n  maxIssues: 0\n")
        self.write("detekt-baseline.xml", "<SmellBaseline/>\n")
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(SRCDIR ${{ARCADIA_ROOT}}/{PROJECT}/src **/*)\nCOLLECT_KOTLIN_LINT_SRCS(DIRS_RECURSE ${{ARCADIA_ROOT}}/{PROJECT}/src)\nEND()\n")
        self.write("src/AnswerTest.kt", "class AnswerTest\n")
        self.write("src/nested/OtherTest.kt", "class OtherTest\n")
        self.write("src/Other.java", "class Other {}\n")
        node = self.node(self.graph("-t"), "tests/test-results/detekt/meta.json")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertIn("run_custom_lint", args)
        self.assertEqual(args[args.index("--lint-name") + 1], "detekt")
        self.assertEqual(args[args.index("--wrapper-script") + 1], "tools/detekt_linter/wrapper.py")
        for path in ("src/AnswerTest.kt", "src/nested/OtherTest.kt", "detekt-config.yml", "detekt-baseline.xml"):
            self.assertIn(f"$(S)/{PROJECT}/{path}", args)
            self.assertIn(f"$(S)/{PROJECT}/{path}", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/Other.java", args)
        self.assertTrue(any(arg.startswith("DETEKT_RESOURCE_GLOBAL::") for arg in args))
