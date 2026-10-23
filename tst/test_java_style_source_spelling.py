import unittest

import lib
from test_java_tests import option_values


def fixture(mode):
    directories = {
        'plain': 'src', 'trailing': 'src/', 'double': 'src//',
        'dot': 'src/./', 'parent': 'src/../src/',
        'leading-dot': './src/', 'leading-double': './/src/',
        'rooted': '${CURDIR}/src/', 'source-rooted': '${ARCADIA_ROOT}/grove/styles/src/',
        'literal-dots': 'version..name/', 'missing': 'absent/',
    }
    directory = directories[mode]
    files = lib.java_tree(**{
        'grove/styles/ya.make': 'JAVA_LIBRARY(styles)\nWITH_KOTLIN()\nJAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ' + directory + ' **/*)\nEND()\n',
        'grove/styles/src/Item.java': 'class Item {}\n',
        'grove/styles/src/Item.kt': 'class KItem\n',
        'grove/styles/version..name/Item.java': 'class Other {}\n',
        'build/platform/java/kotlin/ya.make': lib.resource_lib('KOTLIN_COMPILER', 7),
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/ya.make': 'JAVA_CONTRIB()\nEND()\n',
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10/kotlin.jar': '',
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10/ya.make': 'JAVA_CONTRIB()\nLOCAL_JAR(kotlin.jar)\nEND()\n',
    })
    lib.tool_program(files, 'contrib/tools/ijar', 'ijar')
    return files, ['grove/styles', '--target-platform-tests']


class JavaStyleSourceSpellingTest(unittest.TestCase):
    def test_unreconstructed_relative_directory_spelling_uses_module_fallback(self):
        for mode in ('plain', 'trailing', 'double', 'dot', 'parent', 'leading-dot', 'leading-double', 'rooted', 'source-rooted', 'literal-dots', 'missing'):
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                directory = '$(S)/grove/styles'
                if mode not in ('trailing', 'leading-dot', 'leading-double', 'missing'):
                    directory += '/version..name' if mode == 'literal-dots' else '/src'
                for suite in ('java_style', 'ktlint'):
                    node = lib.node_by_output(graph, '$(B)/grove/styles/test-results/' + suite + '/meta.json')
                    related = ['$(S)/build/rules/jstyle/migrations.yaml', directory] if suite == 'java_style' else list(dict.fromkeys([
                        '$(S)/grove/styles', '$(S)/.editorconfig', '$(S)/build/platform/java/ktlint/.editorconfig', directory,
                    ]))
                    self.assertEqual(option_values(node['cmds'][-1]['cmd_args'], '--test-related-path'), related)
                    source_list = 'lint-java.srclst' if suite == 'java_style' else 'all-kt-sources.txt'
                    producer = lib.node_by_output(graph, '$(B)/grove/styles/' + source_list)
                    self.assertIn(producer['uid'], node['deps'])
                    self.assertIn('$(B)/grove/styles/' + source_list, node['inputs'])
                    self.assertEqual(node['inputs'], sorted(set(['$(S)/grove/styles', '$(B)/grove/styles/' + source_list, *related])))
                    self.assertIn(node['uid'], graph['result'])
                if mode != 'missing':
                    jar = lib.node_by_output(graph, '$(B)/grove/styles/styles.jar')
                    source = 'version..name' if mode == 'literal-dots' else 'src'
                    self.assertIn('$(S)/grove/styles/' + source + '/Item.java', jar['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
