import unittest

import lib
from test_java import contrib
from test_java_features import tree


VENDOR = "contrib/java/org/willow"


def workspace(scripts):
    return tree(
        f"PEERDIR({VENDOR}/facade/1)\nDEPENDENCY_MANAGEMENT({VENDOR}/ink/8)\n{scripts}",
        module="JAVA_PROGRAM",
        **{
            "checks/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(app)\nEND()\n",
            "app/first.tmpl": "first runner\n",
            "app/second.tmpl": "second runner\n",
            **contrib(f"{VENDOR}/facade/1", f"{VENDOR}/ink/1", jar=101, src=201),
            **contrib(f"{VENDOR}/ink/1", jar=102, src=202),
            **contrib(f"{VENDOR}/ink/8", jar=103, src=203),
        },
    )


class JavaScriptOutputsTest(unittest.TestCase):
    def test_custom_default_runner_keeps_both_commands_and_one_output(self):
        files = workspace("GENERATE_SCRIPT(TEMPLATE first.tmpl OUT run.sh)")
        graph = lib.make(files, "app")
        run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertEqual(run["outputs"].count("$(B)/app/run.sh"), 1)
        commands = [c["cmd_args"] for c in run["cmds"] if "--output" in c["cmd_args"]]
        self.assertEqual(len(commands), 2)
        self.assertIn("GENERATE_DEFAULT_RUNNER=yes", commands[0])
        self.assertIn("$(S)/app/first.tmpl", commands[1])
        self.assertIn("$(S)/app/first.tmpl", run["inputs"])
        self.assertNotIn("$(B)/" + VENDOR + "/ink/1/ink-1.jar", [o for n in graph["graph"] for o in n["outputs"]])

    def test_depends_retains_unconsumed_artifact_result_with_custom_runner(self):
        for options in ([], ["--add-modules-to-results"]):
            with self.subTest(options=options):
                files = workspace("GENERATE_SCRIPT(TEMPLATE first.tmpl OUT run.sh)")
                graph = lib.make(files, "checks", "-t", *options)
                old = lib.node_by_output(graph, "$(B)/" + VENDOR + "/ink/1/ink-1.jar")
                selected = lib.node_by_output(graph, "$(B)/" + VENDOR + "/ink/8/ink-8.jar")
                run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
                self.assertIn(old["uid"], graph["result"])
                self.assertIn(selected["uid"], run["deps"])
                self.assertNotIn(old["uid"], run["deps"])
                self.assertEqual(old["outputs"], ["$(B)/" + VENDOR + "/ink/1/ink-1.jar", "$(B)/" + VENDOR + "/ink/1/ink-1-sources.jar"])
                self.assertEqual(len(old["cmds"]), 2)
                control = lib.make(workspace(""), "checks", "-t", *options)
                before = lib.node_by_output(control, "$(B)/" + VENDOR + "/ink/1/ink-1.jar")
                def payload(node, graph):
                    nodes = {n["uid"]: n for n in graph["graph"]}
                    return {
                        key: [(nodes[uid].get("platform"), nodes[uid]["outputs"]) for uid in value] if key == "deps" else value
                        for key, value in node.items() if key not in ("uid", "self_uid")
                    }
                self.assertEqual(payload(old, graph), payload(before, control))

    def test_skipped_suite_keeps_unconsumed_artifact_as_an_isolated_result(self):
        for options in ([], ["--add-modules-to-results"]):
            with self.subTest(options=options):
                files = workspace("GENERATE_SCRIPT(TEMPLATE first.tmpl OUT run.sh)")
                files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nSIZE(LARGE)\nTAG(ya:fat)\nDEPENDS(app)\nEND()\n"
                graph = lib.make(
                    files, "checks", "--cache-tests", "--dist", "--output-only-tests",
                    "--target-platform-tests", "--test-size", "small", "--report-skipped-suites", *options,
                    env={"YA_STRIP_IDLE_BUILD_RESULTS": "0", "YA_STRIP_PACKAGES_FROM_RESULTS": "1"},
                )
                old = lib.node_by_output(graph, "$(B)/" + VENDOR + "/ink/1/ink-1.jar")
                self.assertIn(old["uid"], graph["result"])
                self.assertFalse(any(old["uid"] in node["deps"] for node in graph["graph"]))

    def test_uberjar_repeated_script_output_preserves_template_order(self):
        files = workspace(
            "UBERJAR()\nGENERATE_SCRIPT(TEMPLATE first.tmpl OUT launch.sh)\n"
            "GENERATE_SCRIPT(TEMPLATE second.tmpl OUT launch.sh)"
        )
        graph = lib.make(files, "app")
        run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertEqual(run["outputs"].count("$(B)/app/launch.sh"), 1)
        commands = [c["cmd_args"] for c in run["cmds"] if "--template" in c["cmd_args"]]
        self.assertEqual([c[c.index("--template") + 1] for c in commands], ["$(S)/app/first.tmpl", "$(S)/app/second.tmpl"])
        self.assertIn("$(S)/app/first.tmpl", run["inputs"])
        self.assertIn("$(S)/app/second.tmpl", run["inputs"])
        self.assertEqual(len(run["outputs"]), len(set(run["outputs"])))


if __name__ == "__main__":
    unittest.main()
