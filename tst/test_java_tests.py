import unittest

import lib
from lib import java_tree, resource_lib


def test_nodes(graph):
    return {n["kv"]["path"]: n for n in graph["graph"] if n["kv"].get("run_test_node")}


def run_args(node):
    args = node["cmds"][-1]["cmd_args"]
    return args[args.index("$(TEST_TOOL_HOST)/test_tool", 1):]


def head_args(node):
    args = node["cmds"][-1]["cmd_args"]
    return args[:args.index("$(TEST_TOOL_HOST)/test_tool", 1)]


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
        self.assertEqual(context["requirements"], {"cpu": 1, "network": "restricted", "ram": 32})
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


if __name__ == "__main__":
    unittest.main()
