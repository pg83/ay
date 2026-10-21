import unittest

import lib
from test_java import contrib, writer_args
from test_java_features import tree, command
from test_java_tests import junit_tree, test_nodes, run_args, option_values
from test_ymake_conf import Tree


class JavaEdgeContractsTest(unittest.TestCase):
    def test_long_unsplittable_test_module_names_keep_the_last_component(self):
        for directory in ('x' * 60, 'parent/' + 'x' * 60):
            with self.subTest(directory=directory):
                files = junit_tree('JUNIT5()\nEND()\n', **{f'{directory}/ya.make': 'JUNIT5()\nEND()\n'})
                graph = lib.make(files, directory)
                lib.node_by_output(graph, f'$(B)/{directory}/{"x" * 60}.test.cp.jar')

    def test_peerdir_markers_are_not_treated_as_dependencies(self):
        files = tree('PEERDIR(ADDINCL GLOBAL dep)\nJAVA_SRCS(src/Main.java)', **contrib('dep', jar=1))
        graph = lib.make(files, 'app')
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        self.assertEqual(writer_args(jar, 'bfg.txt'), ['dep/dep.jar'])

    def test_jstyle_server_and_nonrecursive_kotlin_collection(self):
        files = tree('WITH_KOTLIN()\nJAVA_SRCS(SRCDIR src *.java *.kt)\n'
                     'JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ${ARCADIA_BUILD_ROOT}/app/generated *.java)\n'
                     'COLLECT_KOTLIN_LINT_SRCS(DIRS src)\nSTYLE_DETEKT()', **{'app/src/nested/Skip.kt': 'class Skip\n', 'build/platform/java/detekt/ya.make': lib.resource_lib('DETEKT', 9)})
        graph = Tree(self, files, ya_conf='use_jstyle_server = true\n[flags]\nOPENSOURCE = "yes"\n').graph('app', '-t')
        nodes = test_nodes(graph)
        self.assertIn('--use-jstyle-server', nodes['app/java_style']['cmds'][-1]['cmd_args'])
        self.assertNotIn('$(B)/app/generated', option_values(nodes['app/java_style']['cmds'][-1]['cmd_args'], '--test-related-path'))
        sources = option_values(nodes['app/detekt']['cmds'][-1]['cmd_args'], '--test-related-path')
        self.assertIn('$(S)/app/src/Main.kt', sources)
        self.assertNotIn('$(S)/app/src/nested/Skip.kt', sources)

    def test_program_classpath_checks_follow_uberjar_and_dist_settings(self):
        for uberjar in (False, True):
            with self.subTest(uberjar=uberjar):
                files = tree('CHECK_JAVA_DEPS(yes)\nJAVA_SRCS(src/Main.java)\n' + ('UBERJAR()' if uberjar else ''), module='JAVA_PROGRAM')
                graph = lib.make(files, 'app', '-t', '--dist')
                checks = [node for node in test_nodes(graph).values()
                          if 'run_classpath_clash' in run_args(node)]
                self.assertEqual(len(checks), 1)
                self.assertIn('--verbose', run_args(checks[0]))
                self.assertIn('app/app.jar', run_args(checks[0]))

    def test_incomplete_raw_variable_is_preserved_in_javac_options(self):
        files = tree("JAVAC_FLAGS('-Dunfinished=${BROKEN')\nJAVA_SRCS(src/Main.java)")
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertIn('-Dunfinished=${BROKEN', command(jar, 'run_javac.py'))


if __name__ == '__main__':
    unittest.main(verbosity=2)
