import unittest

import lib
from lib import java_tree, resource_lib
from test_go_tests import AUTOCHECK, go_module, go_world


def test_nodes(graph):
    return {n["kv"]["path"]: n for n in graph["graph"] if n["kv"].get("run_test_node")}


def head_args(node):
    args = node["cmds"][-1]["cmd_args"]
    return args[:args.index("--tar") + 2]


def run_args(node):
    return node["cmds"][-1]["cmd_args"][len(head_args(node)):]


def option_values(args, option):
    return [args[i + 1] for i, a in enumerate(args) if a == option]


def global_names(args):
    return [g.split("::")[0] for g in option_values(args, "--global-resource")]


def style_tree(**extra):
    return java_tree(**{
        "build/platform/java/kotlin/ya.make": resource_lib("KOTLIN_COMPILER", 7),
        "build/platform/java/detekt/ya.make": resource_lib("DETEKT", 8),
        "contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/ya.make": "JAVA_CONTRIB()\nEND()\n",
        "app/src/Main.java": "class Main {}\n",
        "app/src/Util.kt": "object Util\n",
        **extra,
    })


class JavaStyleSuitesTest(unittest.TestCase):
    def test_java_style_and_ktlint_suites(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\nLINT(strict)\nENV(TZ=UTC)\n"
                           "ENV(PATTERN=%d %m)\nJAVA_SRCS(SRCDIR src **/*)\nJAVA_SRCS(**/*.txt)\nEND()\n",
        })
        graph = lib.make(files, "app", "--target-platform-tests")
        tests = test_nodes(graph)
        style = tests["app/java_style"]
        head = style["cmds"][-1]["cmd_args"]
        self.assertEqual(head[:2], ["$(TEST_TOOL_HOST)/test_tool", "run_test"])
        self.assertEqual(option_values(head, "--test-type"), ["java.style"])
        # JavaStyleTestSuite.env appends to a non-empty ENV list on each of the two reads.
        self.assertEqual(option_values(head, "--env"), [
            "TZ=UTC", "PATTERN=%d %m",
            "YA_CORE_TMP_PATH=$(RESOURCE_ROOT)/tests/jstyle_server",
            "YA_CORE_TMP_PATH=$(RESOURCE_ROOT)/tests/jstyle_server",
        ])
        self.assertEqual(option_values(head, "--test-related-path"), [
            "$(S)/build/rules/jstyle/migrations.yaml", "$(S)/app", "$(S)/app/src",
        ])
        run = run_args(style)
        self.assertEqual(run[1], "run_javastyle")
        self.assertEqual(option_values(run, "--java"), ["$(B)/resources/JDK26/bin/java"])
        self.assertEqual(option_values(run, "--config-xml"), ["/yandex_checks_strict.xml"])
        self.assertEqual(run[-1], "$(B)/app/lint-java.srclst::$(S)")
        self.assertEqual(style["requirements"], {"cpu": 4, "network": "restricted", "ram": 16, "ram_disk": 0})

        ktlint = tests["app/ktlint"]
        self.assertEqual(option_values(ktlint["cmds"][-1]["cmd_args"], "--test-related-path"), [
            "$(S)/app", "$(S)/.editorconfig", "$(S)/app/src", "$(S)/build/platform/java/ktlint/.editorconfig",
        ])
        self.assertEqual(run_args(ktlint)[1], "run_ktlint_test")

        context = lib.node_by_output(graph, "$(B)/common_test.context")
        # build_opts DefaultNodeRequirementsOptions: no ya.conf default_node_requirements
        self.assertEqual(context["requirements"], {"network": "restricted"})
        for node in (style, ktlint):
            self.assertIn(context["uid"], node["deps"])
            self.assertIn(node["uid"], graph["result"])

    def test_empty_env_gets_one_jstyle_entry(self):
        files = style_tree(**{"app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n"})
        style = test_nodes(lib.make(files, "app", "--target-platform-tests"))["app/java_style"]
        self.assertEqual(option_values(style["cmds"][-1]["cmd_args"], "--env"), [
            "YA_CORE_TMP_PATH=$(RESOURCE_ROOT)/tests/jstyle_server",
        ])


class JavaCustomLintTest(unittest.TestCase):
    def tree(self):
        return style_tree(**{
            "build/conf/autoincludes.json": '["proj"]\n',
            "proj/linters.make.inc": "STYLE_DETEKT(CONFIG_TYPE detekt.yml baseline.xml)\n",
            "proj/detekt.yml": "\n",
            "proj/baseline.xml": "\n",
            "proj/app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\nJAVA_SRCS(SRCDIR src **/*)\n"
                                "COLLECT_KOTLIN_LINT_SRCS(DIRS_RECURSE ../shared)\nEND()\n",
            "proj/app/src/A.kt": "object A\n",
            "proj/shared/deep/B.kt": "object B\n",
            "proj/shared/ya.make": "\n",
            "proj/lib/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
            "proj/lib/src/C.java": "class C {}\n",
        })

    def test_detekt_suite_from_linters_make(self):
        graph = lib.make(self.tree(), "proj/app", "--target-platform-tests")
        detekt = test_nodes(graph)["proj/app/detekt"]
        head = head_args(detekt)
        self.assertEqual(option_values(head, "--test-type"), ["detekt"])
        self.assertEqual(global_names(head), ["DETEKT_RESOURCE_GLOBAL"])
        self.assertEqual(option_values(head, "--test-related-path"), [
            "$(S)/proj/shared/deep/B.kt", "$(S)/proj/detekt.yml", "$(S)/proj/baseline.xml",
            "$(S)/tools/detekt_linter/wrapper.py",
        ])
        self.assertEqual(run_args(detekt)[1:], [
            "run_custom_lint", "--source-root", "$(S)", "--build-root", "$(B)",
            "--project-path", "$(S)/proj/app",
            "--trace-path", "$(B)/proj/app/test-results/detekt/ytest.report.trace",
            "--out-path", "$(B)/proj/app/test-results/detekt/testing_out_stuff",
            "--lint-name", "detekt", "--wrapper-script", "tools/detekt_linter/wrapper.py",
            "--depends", "proj/app",
            "--config", "$(S)/proj/detekt.yml", "--config", "$(S)/proj/baseline.xml",
            "--global-resource", "DETEKT_RESOURCE_GLOBAL::$(B)/resources/DETEKT",
            "$(S)/proj/shared/deep/B.kt",
        ])
        self.assertIn("$(S)/proj/app", detekt["inputs"])
        style = test_nodes(graph)["proj/app/java_style"]
        self.assertIn("DETEKT_RESOURCE_GLOBAL", global_names(style["cmds"][-1]["cmd_args"]))

    def test_no_lint_files_no_suite_but_resource_peer(self):
        tests = test_nodes(lib.make(self.tree(), "proj/lib", "--target-platform-tests"))
        self.assertNotIn("proj/lib/detekt", tests)
        self.assertIn("DETEKT_RESOURCE_GLOBAL", global_names(tests["proj/lib/java_style"]["cmds"][-1]["cmd_args"]))


class JavaTestGlobalsTest(unittest.TestCase):
    def test_static_peers_do_not_declare_linker_resources(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(native)\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
            "native/ya.make": "LIBRARY()\nSRCS(native.cpp)\nEND()\n",
            "native/native.cpp": "",
            "build/platform/lld/ya.make": resource_lib("LLD_ROOT", 10),
        })
        style = test_nodes(lib.make(files, "app", "--target-platform-tests"))["app/java_style"]
        self.assertNotIn("LLD_ROOT_RESOURCE_GLOBAL", global_names(style["cmds"][-1]["cmd_args"]))

    def tree(self, header):
        bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        return style_tree(**{
            "app/ya.make": f"{header}\nPEERDIR(app/jni)\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
            "app/jni/ya.make": f"DLL(app_jni)\n{bare}PEERDIR(res)\nSRCS(j.cpp)\nEND()\n",
            "app/jni/j.cpp": "int j;\n",
            "res/ya.make": resource_lib("NATIVE_TOOL", 9),
        })

    def globals(self, header):
        style = test_nodes(lib.make(self.tree(header), "app", "--target-platform-tests"))["app/java_style"]
        return global_names(style["cmds"][-1]["cmd_args"])

    def test_shared_library_globals_reach_consuming_modules_only(self):
        # A DLL does not pass its peers' global vars on (global_vars_collector.cpp);
        # a module consuming non-manageable peers takes them from its peer closure.
        self.assertIn("NATIVE_TOOL_RESOURCE_GLOBAL", self.globals("JAVA_LIBRARY_NON_SPLIT()\nADD_DLLS_TO_JAR()"))
        self.assertNotIn("NATIVE_TOOL_RESOURCE_GLOBAL", self.globals("JAVA_LIBRARY_NON_SPLIT()"))


def junit_tree(test_body, **extra):
    bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
    return java_tree(**{
        "devtools/junit5-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "devtools/jtest-annotations/junit5/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "build/platform/java/jacoco-agent/ya.make": resource_lib("JACOCO_AGENT", 6),
        "build/platform/java/jdk/jdk21/ya.make": resource_lib("JDK21", 7),
        "lib/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(lib/jni)\nEND()\n",
        "lib/jni/ya.make": f"DLL(lib_jni)\n{bare}SRCS(j.cpp)\nEND()\n",
        "lib/jni/j.cpp": "int j;\n",
        "base/ut/ya.make": "JUNIT5()\nPEERDIR(lib)\nEND()\n",
        "proj/sub/ut/ya.make": test_body,
        "proj/sub/ut/data.txt": "x\n",
        **extra,
    })


class JavaJunitSuiteTest(unittest.TestCase):
    def test_data_checks_extract_resource_id_prefixes(self):
        files = junit_tree("JUNIT5()\nSET(VALIDATE_DATA yes)\nSIZE(LARGE)\nDATA(sbr:12=label sbr:/34suffix sbr://56/path sbr:///78)\nEND()\n")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests", "--test-size", "small")
        check = test_nodes(graph)["proj/sub/ut/validate_data_sbr"]
        self.assertEqual(check["inputs"], ["$(S)/proj/sub/ut", "12", "34", "56"])

    def test_data_checks_run_even_when_test_size_is_filtered(self):
        files = junit_tree("JUNIT5()\nSET(VALIDATE_DATA yes)\nSIZE(LARGE)\nDATA(sbr://123=fixtures arcadia/proj/sub/ut/data.txt)\nENV(KEY=value)\nEND()\n")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests", "--test-size", "small", "--tests-retries", "3", env={"YA_TOKEN": "test-token"})
        tests = test_nodes(graph)
        self.assertNotIn("proj/sub/ut/sub-ut", tests)
        check = tests["proj/sub/ut/validate_data_sbr"]
        self.assertEqual(check["inputs"], ["$(S)/proj/sub/ut", "123"])
        self.assertEqual(run_args(check)[1:], [
            "run_check", "--source-root", "$(S)", "--checker", "check_resource --token test-token",
            "--check-name", "validate_data_sbr", "--trace-path", "$(B)/proj/sub/ut/test-results/validate_data_sbr/ytest.report.trace",
            "--out-path", "$(B)/proj/sub/ut/test-results/validate_data_sbr/testing_out_stuff",
            "--log-path", "$(B)/proj/sub/ut/test-results/validate_data_sbr/testing_out_stuff/run_check.log",
            "123", "--batch", "--batch-name", "ut",
        ])
        self.assertEqual(option_values(head_args(check), "--env"), ["KEY=value"])
        self.assertEqual(check["requirements"]["network"], "full")

    def test_junit_node(self):
        files = junit_tree("JUNIT5()\nJDK_VERSION(21)\nPEERDIR(lib base/ut)\nENV(A=1)\nJVM_ARGS(-Dd=${CURDIR})\n"
                           "SYSTEM_PROPERTIES(k v)\nDATA(arcadia/proj/sub/ut/data.txt sbr://1)\nEND()\n")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
        node = test_nodes(graph)["proj/sub/ut/sub-ut"]
        work = "$(B)/proj/sub/ut/test-results/sub-ut"
        head = head_args(node)
        self.assertEqual(option_values(head, "--test-type"), ["java"])
        self.assertEqual(option_values(head, "--test-related-path"), [
            "$(S)/build/scripts/run_junit.py", "$(S)/build/scripts/unpacking_jtest_runner.py",
            "$(S)/proj/sub/ut", "$(S)/proj/sub/ut/data.txt",
        ])
        self.assertEqual(option_values(head, "--smooth-shutdown-signals"), ["SIGUSR1"])
        self.assertIn("--supports-canonization", head)
        writer = node["cmds"][1]["cmd_args"]
        classpath = writer[writer.index("--ya-start-command-file") + 1:writer.index("--ya-end-command-file")]
        self.assertEqual(classpath[0], "proj/sub/ut/proj-sub-ut.jar")
        self.assertIn("base/ut/base-ut.jar", classpath)
        self.assertNotIn("base/ut/base-ut.test.cp.jar", classpath)
        run = run_args(node)
        jdk = "$(B)/resources/JDK21"
        self.assertEqual(run[:14], [
            "$(YMAKE_PYTHON3)/bin/python3", "$(S)/build/scripts/run_junit.py",
            "$(YMAKE_PYTHON3)/bin/python3", "$(S)/build/scripts/unpacking_jtest_runner.py",
            "--classpath-option-type", "manifest", "--trace-file", work + "/ytest.report.trace",
            "--jar-binary", jdk + "/bin/jar", "--tests-jar-path", "$(B)/proj/sub/ut/proj-sub-ut.jar", jdk + "/bin/java",
            "-Djava.io.tmpdir=" + work + "/tests_tmp_dir",
        ])
        self.assertIn("-Djava.library.path=$(B)/lib/jni", run)
        self.assertIn("-Dd=$(S)/proj/sub/ut", run)
        self.assertEqual(run[run.index("-ea") + 2], "-DJAVA=" + jdk + "/bin/java")
        # base64 of [{"key":"k","type":"inline","value":"v"}]
        self.assertEqual(option_values(run, "--properties"), ["W3sia2V5IjoiayIsInR5cGUiOiJpbmxpbmUiLCJ2YWx1ZSI6InYifV0="])
        deps = [n["outputs"][0] for n in graph["graph"] if n["uid"] in node["deps"]]
        self.assertEqual(sorted(deps), ["$(B)/common_test.context", "$(B)/proj/sub/ut/proj-sub-ut.jar",
                                        "$(B)/sandbox-storage/1/resource_info.json"])
        self.assertIn(node["uid"], graph["result"])

    def test_forked_suite_chunks_and_accumulator(self):
        files = junit_tree("JUNIT5()\nSIZE(MEDIUM)\nFORK_TESTS()\nSPLIT_FACTOR(3)\nEXPERIMENTAL_FORK()\n"
                           "REQUIREMENTS(ram:16 ram_disk:4)\nEND()\n")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
        chunks = sorted((n for n in graph["graph"] if n["kv"].get("path") == "proj/sub/ut/sub-ut"),
                        key=lambda n: n["kv"]["split_index"])
        self.assertEqual([n["kv"]["split_index"] for n in chunks], [0, 1, 2])
        self.assertEqual({n["kv"]["p"] for n in chunks}, {"TM"})
        self.assertEqual(chunks[1]["requirements"], {"cpu": 4, "network": "restricted", "ram": 16, "ram_disk": 4})
        head = head_args(chunks[1])
        self.assertIn("--requires-ram-disk", head)
        self.assertEqual(option_values(head, "--split-index"), ["1"])
        self.assertEqual(option_values(head, "--timeout"), ["600"])
        run = run_args(chunks[1])
        self.assertEqual(run[run.index("--modulo"):run.index("--modulo") + 5], ["--modulo", "3", "--modulo-index", "1", "--experimental-fork"])
        work = "$(B)/proj/sub/ut/test-results/sub-ut"
        acc = lib.node_by_output(graph, work + "/results_accumulator.log")
        self.assertEqual(acc["kv"], {"p": "TA", "pc": "light-cyan", "show_out": True})
        self.assertNotIn("platform", acc)
        self.assertEqual(option_values(acc["cmds"][0]["cmd_args"], "--output"), [work + f"/chunk{i}" for i in range(3)])
        self.assertEqual(acc["outputs"][-1], work + "/results_accumulator.log")
        self.assertEqual(sorted(acc["deps"]), sorted(n["uid"] for n in chunks))
        self.assertIn(acc["uid"], graph["result"])
        for n in chunks:
            self.assertNotIn(n["uid"], graph["result"])

    def test_classpath_clash_check(self):
        files = junit_tree("JUNIT5()\nPEERDIR(lib)\nCHECK_JAVA_DEPS(strict)\nIGNORE_CLASSPATH_CLASH(b.C a.B)\nEND()\n")
        node = test_nodes(lib.make(files, "proj/sub/ut", "--target-platform-tests"))["proj/sub/ut/sub-ut-dependencies"]
        self.assertEqual(option_values(head_args(node), "--test-type"), ["classpath.clash"])
        run = run_args(node)
        start = run.index("--ya-start-command-file")
        end = run.index("--ya-end-command-file")
        self.assertEqual(run[start - 1], "$(B)")
        self.assertEqual(run[start + 1:start + 3], ["proj/sub/ut/proj-sub-ut.jar", "proj/sub/ut/proj-sub-ut.jar"])
        self.assertEqual(run[end + 1:], ["ignore_class:a.B", "ignore_class:b.C", "--", "--strict"])


class JavaFullPathNameTest(unittest.TestCase):
    def test_long_test_module_name_is_shortened(self):
        deep = "aaaaaaaaaa/bbbbbbbbbbbb/cccccccccccccccccc/dddddddddddddddd/src/test"
        files = junit_tree("JUNIT5()\nEND()\n", **{f"{deep}/ya.make": "JUNIT5()\nEND()\n"})
        graph = lib.make(files, deep)
        # dir and name fit in 98 chars: leading dirs are dropped (ShortenPath)
        lib.node_by_output(graph, f"$(B)/{deep}/dddddddddddddddd-src-test.test.cp.jar")


class SharedTestContextTest(unittest.TestCase):
    def test_java_and_go_suites_depend_on_one_context_node(self):
        files = style_tree(**go_world(), **{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nLINT(base)\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
        })
        go_module(files, "library/go/greet")
        graph = lib.make(files, "app", "library/go/greet", *AUTOCHECK)
        contexts = [n for n in graph["graph"] if "$(B)/common_test.context" in n.get("outputs", [])]
        self.assertEqual(len(contexts), 1)
        # TestFramer.inject_test_context_generator_node: ya.conf default_node_requirements + restricted network
        self.assertEqual(contexts[0]["requirements"], {"cpu": 1, "network": "restricted", "ram": 32})
        tests = test_nodes(graph)
        self.assertIn("app/java_style", tests)
        self.assertIn("library/go/greet/gofmt", tests)
        for node in tests.values():
            self.assertIn(contexts[0]["uid"], node["deps"])


if __name__ == "__main__":
    unittest.main()
