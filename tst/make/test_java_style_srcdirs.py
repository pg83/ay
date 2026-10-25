import unittest

from tst import lib
from tst.make.test_java_tests import option_values, style_tree, test_nodes
from tst.make.test_ymake_conf import Tree


MIGRATIONS = "$(S)/build/rules/jstyle/migrations.yaml"
EDITOR = "$(S)/build/platform/java/ktlint/.editorconfig"


class JavaStyleSourceDirectoriesTest(unittest.TestCase):
    def assert_directories(self, graph, directories, ktlint_directories=None):
        suites = test_nodes(graph)
        if ktlint_directories is None:
            ktlint_directories = directories
        expected = {
            "java_style": [MIGRATIONS, *sorted(set(directories))],
            "ktlint": list(dict.fromkeys(["$(S)/app", "$(S)/.editorconfig", *sorted({EDITOR, *ktlint_directories})])),
        }
        for name, related in expected.items():
            with self.subTest(suite=name):
                node = suites["app/" + name]
                args = node["cmds"][-1]["cmd_args"]
                self.assertEqual(option_values(args, "--test-related-path"), related)
                srclist = "lint-java.srclst" if name == "java_style" else "all-kt-sources.txt"
                self.assertEqual(node["inputs"], sorted({"$(S)/app", "$(B)/app/" + srclist, *related}))

    def test_default_layout_resolves_existing_empty_directories_and_missing_directories(self):
        for present in ((), ("src/main/java",), ("src/main/resources",), ("src/main/java", "src/main/resources")):
            with self.subTest(present=present):
                files = style_tree(**{
                    "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\nDEFAULT_JAVA_SRCS_LAYOUT()\n"
                        "JAVA_SRCS(SRCDIR src/main/kotlin **/*.kt)\nEND()\n",
                    "app/src/main/kotlin/Only.kt": "class Only\n",
                })
                tree = Tree(self, files)
                for directory in present:
                    (tree.src / "app" / directory).mkdir(parents=True)
                graph = tree.graph("app", "--target-platform-tests", "--sandboxing")
                expected = ["$(S)/app/" + directory for directory in (*present, "src/main/kotlin")]
                if len(present) < 2:
                    expected.append("$(S)/app")
                self.assert_directories(graph, expected)

    def test_contents_globs_resource_and_source_modifiers_do_not_filter_existing_directories(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\n"
                "JAVA_SRCS(RESOURCES SRCDIR assets **/* EXCLUDE assets/**)\n"
                "JAVA_SRCS(SRCDIR unmatched **/*.java)\n"
                "JAVA_SRCS(SRCDIR explicit PACKAGE_PREFIX example.api FILES explicit/Item.java)\n"
                "JAVA_SRCS(SRCDIR kotlin **/*.kt)\nEND()\n",
            "app/assets/config.txt": "resource\n",
            "app/unmatched/README.txt": "not a Java source\n",
            "app/explicit/Item.java": "class Item {}\n",
            "app/kotlin/Item.kt": "class Item\n",
        })
        graph = lib.make(files, "app", "--target-platform-tests")
        self.assert_directories(graph, ["$(S)/app/" + d for d in ("assets", "unmatched", "explicit", "kotlin")])

    def test_rooted_source_paths_keep_their_spelling_even_when_absent(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR absent **/*.java)\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ${CURDIR}/rooted-absent **/*.java)\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ${ARCADIA_ROOT}/shared-absent **/*.kt)\n"
                "JAVA_SRCS(SRCDIR . FILES Main.java)\nEND()\n",
            "app/Main.java": "class Main {}\n",
        })
        graph = lib.make(files, "app", "--target-platform-tests")
        self.assert_directories(graph, ["$(S)/app", "$(S)/app/rooted-absent", "$(S)/shared-absent"])

    def test_relative_resolution_uses_module_source_directories_then_the_source_root(self):
        for local in (False, True):
            with self.subTest(local=local):
                files = style_tree(**{
                    "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\nSRCDIR(support)\n"
                        "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR shared **/*.java)\n"
                        "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR secondary **/*.kt)\nEND()\n",
                    "shared/Root.java": "class Root {}\n",
                    "support/secondary/Support.kt": "class Support\n",
                })
                if local:
                    files["app/shared/Local.java"] = "class Local {}\n"
                graph = lib.make(files, "app", "--target-platform-tests")
                shared = "$(S)/app/shared" if local else "$(S)/shared"
                self.assert_directories(graph, [shared, "$(S)/support/secondary"])

    def test_generated_source_paths_are_excluded_from_check_data_without_losing_producers(self):
        files = style_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\n"
                "FROM_SANDBOX(FILE 123 OUT Generated.java)\n"
                "FROM_SANDBOX(FILE 456 OUT ${ARCADIA_BUILD_ROOT}/RootGenerated.java)\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ${BINDIR}/generated **/*.java)\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR Generated.java *.java)\n"
                "JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR RootGenerated.java *.java)\n"
                "JAVA_SRCS(SRCDIR src **/*.java **/*.kt)\nEND()\n",
        })
        graph = lib.make(files, "app", "--target-platform-tests")
        self.assert_directories(graph, ["$(S)/app/src"])
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        for output in ("$(B)/app/Generated.java", "$(B)/RootGenerated.java"):
            producer = lib.node_by_output(graph, output)
            self.assertIn(output, jar["inputs"])
            self.assertIn(producer["uid"], jar["deps"])

    def test_ktlint_opt_out_excludes_source_directories_without_affecting_java_style(self):
        for declaration, flags in (("WITH_YA_1931()\n", ()), ("", ("-D_WITH_YA_1931=yes",))):
            with self.subTest(declaration=declaration, flags=flags):
                files = style_tree(**{
                    "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nWITH_KOTLIN()\n" + declaration
                        + "JAVA_SRCS(SRCDIR src **/*.java **/*.kt)\nEND()\n",
                })
                graph = lib.make(files, "app", "--target-platform-tests", *flags)
                self.assert_directories(graph, ["$(S)/app/src"], ktlint_directories=[])


if __name__ == "__main__":
    unittest.main(verbosity=2)
