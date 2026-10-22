import unittest

import lib
from test_java_tests import run_args, test_nodes


class JavaClasspathIgnoreTest(unittest.TestCase):
    def command(self, body):
        files = lib.java_tree(**{
            "scope/app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(scope/peer)\nCHECK_JAVA_DEPS(strict)\n" + body + "\nEND()\n",
            "scope/peer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_IGNORE_CLASSPATH_CLASH_FOR(peer.Unrelated)\nEND()\n",
        })
        node = test_nodes(lib.make(files, "scope/app", "--target-platform-tests"))["scope/app/scope-app-dependencies"]
        return run_args(node)

    def ignored(self, body):
        args = self.command(body)
        return args[args.index("--ya-end-command-file") + 1:]

    def test_actual_macro_appends_expanded_classes_and_preserves_runner_order(self):
        self.assertEqual(self.ignored("""
SET(PREFIX example)
JAVA_IGNORE_CLASSPATH_CLASH_FOR(zeta.Last ${PREFIX}.Inner)
JAVA_IGNORE_CLASSPATH_CLASH_FOR()
JAVA_IGNORE_CLASSPATH_CLASH_FOR(alpha.First ${PREFIX}.Inner)
"""), ["ignore_class:alpha.First", "ignore_class:example.Inner", "ignore_class:zeta.Last", "--", "--strict"])

    def test_property_assignment_overwrites_prior_macro_and_append_keeps_final_values(self):
        self.assertEqual(self.ignored("""
JAVA_IGNORE_CLASSPATH_CLASH_FOR(discard.Previous)
SET(JAVA_IGNORE_CLASSPATH_CLASH_VALUE assigned.Base)
SET(NEXT assigned.Next)
SET_APPEND(JAVA_IGNORE_CLASSPATH_CLASH_VALUE ${NEXT})
JAVA_IGNORE_CLASSPATH_CLASH_FOR(macro.Final assigned.Base)
"""), ["ignore_class:assigned.Base", "ignore_class:assigned.Next", "ignore_class:macro.Final", "--", "--strict"])

    def test_empty_property_reset_removes_prior_macro_values_and_peer_values_stay_local(self):
        self.assertEqual(self.ignored("""
JAVA_IGNORE_CLASSPATH_CLASH_FOR(discard.Previous)
SET(JAVA_IGNORE_CLASSPATH_CLASH_VALUE)
"""), ["--", "--strict"])

    def test_direct_property_substitution_runs_in_actual_module_context(self):
        self.assertEqual(self.ignored("""
IF (MODULE_TAG == JAVA)
    SET(CLASS actual.Java)
ELSE()
    SET(CLASS wrong.Module)
ENDIF()
SET(JAVA_IGNORE_CLASSPATH_CLASH_VALUE ${CLASS} explicit.Last)
"""), ["ignore_class:actual.Java", "ignore_class:explicit.Last", "--", "--strict"])

    def test_dart_values_skip_quoted_empty_items_and_unescape_nested_class_dollar(self):
        self.assertEqual(self.ignored(r'''SET(JAVA_IGNORE_CLASSPATH_CLASH_VALUE '""' "''" 'example.Outer${"$"}Inner')'''),
                         ["ignore_class:example.Outer$Inner", "--", "--strict"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
