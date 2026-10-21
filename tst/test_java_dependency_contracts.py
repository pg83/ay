import unittest

import lib
from test_java import contrib, proxy, writer_args
from test_java_features import tree
from test_java_tests import junit_tree, option_values, run_args, test_nodes


class JavaDependencyContractsTest(unittest.TestCase):
    def test_ktlint_resolves_all_modules_in_its_project(self):
        for module, other_target in (("JAVA_PROGRAM", "app.run.cp.jar"), ("JAVA_LIBRARY", None)):
            with self.subTest(module=module):
                files = tree("WITH_KOTLIN()\nJAVA_SRCS(SRCDIR src **/*.kt)\nFILES(config.yaml)",
                             module=module, **{"app/config.yaml": "key: value\n"})
                graph = lib.make(files, "app", "--target-platform-tests", "--output-only-tests")
                lint = test_nodes(graph)["app/ktlint"]
                outputs = ["$(B)/app/app.jar"] + (["$(B)/app/" + other_target] if other_target else [])
                for output in outputs:
                    self.assertIn(lib.node_by_output(graph, output)["uid"], lint["deps"])
                if module == "JAVA_PROGRAM":
                    run = lib.node_by_output(graph, "$(B)/app/" + other_target)
                    config = lib.node_by_output(graph, "$(B)/app/config.yaml")
                    self.assertIn(config["uid"], run["deps"])
                    self.assertEqual(sum("$(B)/app/config.yaml" in n["outputs"] for n in graph["graph"]), 1)
                else:
                    self.assertFalse(any("$(B)/app/app-interface.jar" in n["outputs"] for n in graph["graph"]))

    def test_ktlint_includes_an_interface_jar_used_by_a_consumer(self):
        files = tree("WITH_KOTLIN()\nJAVA_SRCS(SRCDIR src **/*.kt)", **{
            "consumer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(app)\nEND()\n",
        })
        graph = lib.make(files, "app", "consumer", "--target-platform-tests")
        lint = test_nodes(graph)["app/ktlint"]
        self.assertIn(lib.node_by_output(graph, "$(B)/app/app-interface.jar")["uid"], lint["deps"])

    def test_junit_native_dependencies_stop_at_consuming_peers(self):
        files = junit_tree("JUNIT5()\nPEERDIR(lib)\nEND()\n")
        files["lib/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\nADD_DLLS_TO_JAR()\nPEERDIR(lib/jni)\nEND()\n"
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
        run = test_nodes(graph)["proj/sub/ut/sub-ut"]
        native = lib.node_by_output(graph, "$(B)/lib/jni/liblib_jni.so")
        jar = lib.node_by_output(graph, "$(B)/lib/lib.jar")
        self.assertIn(native["uid"], jar["deps"])
        self.assertIn(jar["uid"], run["deps"])
        self.assertNotIn(native["uid"], run["deps"])

    def test_classpath_check_depends_on_each_managed_jar_producer(self):
        files = tree("PEERDIR(api)\nCHECK_JAVA_DEPS(yes)", module="JAVA_LIBRARY_NON_SPLIT", **{
            **contrib("api", "runtime", jar=1), **contrib("runtime", jar=2),
        })
        graph = lib.make(files, "app", "--target-platform-tests", "--output-only-tests")
        check = next(n for n in test_nodes(graph).values()
                     if "--checker" in run_args(n) and "run_classpath_clash" in run_args(n))
        for output in ("$(B)/app/app.jar", "$(B)/api/api.jar", "$(B)/runtime/runtime.jar"):
            self.assertIn(lib.node_by_output(graph, output)["uid"], check["deps"])

    def test_classpath_metadata_without_a_producer_stays_unresolved(self):
        files = tree("")
        files["app/ya.make"] = (
            "JAVA_LIBRARY_NON_SPLIT(named PREFIX pre-)\nCHECK_JAVA_DEPS(yes)\nEND()\n"
        )
        graph = lib.make(files, "app", "--target-platform-tests")
        check = next(n for n in test_nodes(graph).values() if "run_classpath_clash" in run_args(n))
        self.assertIn("app/named.jar", run_args(check))
        self.assertNotIn(lib.node_by_output(graph, "$(B)/app/pre-named.jar")["uid"], check["deps"])
        self.assertEqual(check["deps"], [lib.node_by_output(graph, "$(B)/common_test.context")["uid"]])

    def test_cyclic_peers_keep_each_classpath_entry_once(self):
        files = tree("PEERDIR(a)", **{
            **contrib("a", "b", jar=1), **contrib("b", "a", jar=2),
        })
        graph = lib.make(files, "app")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertEqual(writer_args(jar, "bfg.txt"), ["a/a.jar", "b/b.jar"])
        for output in ("$(B)/a/a.jar", "$(B)/b/b.jar"):
            self.assertIn(lib.node_by_output(graph, output)["uid"], jar["deps"])
            self.assertEqual(sum(output in n["outputs"] for n in graph["graph"]), 1)

    def test_exclusions_propagate_but_preserve_direct_dependencies(self):
        root, middle, leaf = (f"contrib/java/example/{p}/1" for p in ("root", "middle", "leaf"))
        for direct_leaf, exclusions in ((False, middle), (True, middle), (True, middle + " " + leaf)):
            with self.subTest(direct_leaf=direct_leaf, exclusions=exclusions):
                files = tree(f"PEERDIR({root} {leaf if direct_leaf else ''})\nEXCLUDE({exclusions})", module="JAVA_LIBRARY_NON_SPLIT", **{
                    **contrib(root, middle, jar=1), **contrib(middle, leaf, jar=2), **contrib(leaf, jar=3),
                })
                jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
                self.assertEqual(writer_args(jar, "bfg.txt"),
                                 [root + "/root-1.jar"] + ([leaf + "/leaf-1.jar"] if direct_leaf else []))

    def test_missing_managed_versions_are_diagnosed(self):
        root = "contrib/java/example/api"
        for rule, expected in (("", "Dependency with default version"),
                               (root + "/absent", "dependency to missing directory")):
            with self.subTest(rule=rule):
                files = tree(f"PEERDIR({root})\nDEPENDENCY_MANAGEMENT({rule})", module="JAVA_LIBRARY_NON_SPLIT", **{
                    **proxy(root, root + "/1"), **contrib(root + "/1", jar=1),
                })
                result = lib.make_process(files, "app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertNotIn("panic:", result.stderr)

    def test_runtime_exclusion_does_not_affect_the_compile_classpath(self):
        root, leaf = "contrib/java/example/api/1", "contrib/java/example/leaf/1"
        files = tree(f"PEERDIR({root})\nJAVA_RUNTIME_EXCLUDE({leaf})\nJAVA_TEST_EXCLUDE({root})\nDEFAULT_JAVA_SRCS_LAYOUT()",
                     module="JAVA_PROGRAM", **{**contrib(root, leaf, jar=1), **contrib(leaf, jar=2)})
        graph = lib.make(files, "app")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertIn(leaf + "/leaf-1.jar", writer_args(jar, "bfg.txt"))
        run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertIn("app/api-1.jar", writer_args(run, "run-bf.txt"))
        self.assertNotIn("app/leaf-1.jar", writer_args(run, "run-bf.txt"))

    def test_junit_metadata_and_explicit_test_jar_peer(self):
        files = junit_tree("JUNIT5()\nJUNIT_TESTS_JAR(lib)\nJAVA_TEST_PEERDIR(testdata)\n"
                           "TAG(ya:manual)\nDATA_FILES(fixtures/input.txt)\nDEPENDS(tool)\n"
                           "TEST_JAVA_CLASSPATH_CMD_TYPE(COMMAND_FILE)\nEND()\n", **{
            **contrib("testdata", jar=7), "fixtures/input.txt": "data\n",
        })
        lib.tool_program(files, "tool", "tool")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
        run = test_nodes(graph)["proj/sub/ut/sub-ut"]
        self.assertEqual(option_values(run_args(run), "--classpath-option-type"), ["command_file"])
        self.assertIn("$(S)/fixtures/input.txt", run["inputs"])
        self.assertIn(lib.node_by_output(graph, "$(B)/tool/tool")["uid"], run["deps"])
        args = run["cmds"][-1]["cmd_args"]
        self.assertIn("ya:manual", args)
        classpath = writer_args(run, ".test.cpf")
        self.assertIn("testdata/testdata.jar", classpath)

    def test_old_kotlin_linter_and_selective_disable(self):
        for macro, enabled in (("USE_KTLINT_OLD()", True), ("NO_LINT(ktlint)", False)):
            with self.subTest(macro=macro):
                files = tree(f"WITH_KOTLIN()\nJAVA_SRCS(SRCDIR src **/*.java **/*.kt)\n{macro}", module="JAVA_LIBRARY_NON_SPLIT")
                nodes = test_nodes(lib.make(files, "app", "--target-platform-tests"))
                self.assertIn("app/java_style", nodes)
                self.assertEqual("app/ktlint" in nodes, enabled)
                if enabled:
                    self.assertEqual(option_values(run_args(nodes["app/ktlint"]), "--binary"), ["$(KTLINT_OLD)/run.bat"])

    def test_disable_sources_jar_and_data_validation(self):
        files = tree("ENABLE(SOURCES_JAR)\nDISABLE(SOURCES_JAR)\nDISABLE_DATA_VALIDATION()\n"
                     "DATA(sbr://42)\nJAVA_SRCS(src/Main.java)", module="JAVA_LIBRARY_NON_SPLIT")
        graph = lib.make(files, "app", "--target-platform-tests", opensource=False)
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertNotIn("$(B)/app/app-sources.jar", jar["outputs"])
        self.assertFalse(any("validate_data_sbr" in p for p in test_nodes(graph)))


if __name__ == "__main__":
    unittest.main(verbosity=2)
