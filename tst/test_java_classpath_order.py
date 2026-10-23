import unittest

import lib


def fixture(mode):
    header = 'JTEST' if mode not in ('junit5', 'junit6') else mode.upper()
    annotation = {'JTEST': 'junit4', 'JUNIT5': 'junit5', 'JUNIT6': 'junit6'}[header]
    files = lib.java_tree(**{
        'grove/tests/ya.make': '',
        'grove/tests/Test.java': 'class Test {}\n',
        'grove/alpha/ya.make': 'JAVA_LIBRARY_NON_SPLIT(alpha)\nPEERDIR(grove/common)\nEND()\n',
        'grove/beta/ya.make': 'JAVA_LIBRARY_NON_SPLIT(beta)\nPEERDIR(grove/common)\nEND()\n',
        'grove/common/ya.make': 'JAVA_LIBRARY_NON_SPLIT(common)\nEND()\n',
        'build/platform/java/kotlin/ya.make': lib.resource_lib('KOTLIN_COMPILER', 7),
        'build/platform/java/jacoco-agent/ya.make': lib.resource_lib('JACOCO_AGENT', 6),
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/ya.make': 'JAVA_CONTRIB()\nEND()\n',
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10/kotlin.jar': '',
        'contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10/ya.make': 'JAVA_CONTRIB()\nLOCAL_JAR(kotlin.jar)\nEND()\n',
    })
    for peer in ('junit-runner', 'junit5-runner', 'junit6-runner', 'jtest-annotations/junit4', 'jtest-annotations/junit5', 'jtest-annotations/junit6'):
        files['devtools/' + peer + '/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n'
    body = '' if mode == 'ordinary' else 'WITH_KOTLIN()\n'
    peers = 'grove/alpha grove/beta'
    if mode == 'reversed':
        peers = 'grove/beta grove/alpha'
    if mode == 'duplicate':
        peers = 'devtools/jtest-annotations/' + annotation + ' ' + peers
    if mode == 'transitive':
        files['grove/common/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT(common)\nPEERDIR(devtools/jtest-annotations/' + annotation + ')\nEND()\n'
    files['grove/tests/ya.make'] = header + '()\n' + body + 'JAVA_SRCS(Test.java)\nPEERDIR(' + peers + ')\nEND()\n'
    return files, ['grove/tests', '--target-platform-tests']


def jar_order(node):
    for cmd in node['cmds']:
        args = cmd['cmd_args']
        if '--file' in args and args[args.index('--file') + 1].endswith(('bfg.txt', '.test.cpf', 'run-bf.txt')):
            start = args.index('--ya-start-command-file') + 1
            end = args.index('--ya-end-command-file', start)
            return [a.removeprefix('$(B)/') for a in args[start:end] if a.endswith('.jar')]
    raise AssertionError(node['outputs'])


class JavaClasspathOrderTest(unittest.TestCase):
    def test_config_annotation_precedes_dynamic_kotlin_and_source_peers(self):
        for mode in ('ordinary', 'kotlin', 'reversed', 'duplicate', 'transitive', 'junit5', 'junit6'):
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                suffix = {'junit5': 'junit5', 'junit6': 'junit6'}.get(mode, 'junit4')
                annotation = 'devtools/jtest-annotations/' + suffix + '/jtest-annotations-' + suffix + '.jar'
                compilation = lib.node_by_output(graph, '$(B)/grove/tests/grove-tests.jar')
                order = jar_order(compilation)
                expected = [annotation]
                if mode != 'ordinary':
                    expected.append('contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10/kotlin-stdlib-jdk8-2.3.10.jar')
                if mode in ('junit5', 'junit6'):
                    expected.insert(0, 'devtools/' + mode + '-runner/devtools-' + mode + '-runner.jar')
                first, second = ('beta', 'alpha') if mode == 'reversed' else ('alpha', 'beta')
                expected += ['grove/' + first + '/' + first + '.jar', 'grove/common/common.jar', 'grove/' + second + '/' + second + '.jar']
                self.assertEqual(order, expected)
                suite = lib.node_by_output(graph, '$(B)/grove/tests/test-results/grove-tests/meta.json')
                runtime = jar_order(suite)
                self.assertEqual(runtime[:len(order) + 1], ['grove/tests/grove-tests.jar'] + order)
                runtime_jar = lib.node_by_output(graph, '$(B)/grove/tests/grove-tests.test.cp.jar')
                self.assertEqual(jar_order(runtime_jar), runtime)
                self.assertIn('$(S)/grove/tests/Test.java', compilation['inputs'])
                producer = lib.node_by_output(graph, '$(B)/' + annotation)
                self.assertIn(producer['uid'], compilation['deps'])
                self.assertIn(producer['uid'], suite['deps'])
                self.assertIn(suite['uid'], graph['result'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
