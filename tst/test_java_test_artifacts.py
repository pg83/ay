import unittest

import lib


SONAR = 'contrib/java/org/sonarsource/scanner/cli/sonar-scanner-cli/2.8'
MODES = ('jtest', 'jtest-for', 'junit5', 'junit6', 'library', 'library-explicit', 'two-owners', 'build-only', 'renamed', 'configured')


def fixture(mode):
    header = {
        'jtest-for': 'JTEST_FOR(grove/library)', 'junit5': 'JUNIT5()', 'junit6': 'JUNIT6()',
        'library': 'JAVA_LIBRARY_NON_SPLIT()', 'library-explicit': 'JAVA_LIBRARY_NON_SPLIT()',
    }.get(mode, 'JTEST()')
    body = 'DEPENDS(' + SONAR + ')\n' if mode == 'library-explicit' else ''
    files = lib.java_tree(**{
        'grove/checks/ya.make': header + '\n' + body + 'JAVA_SRCS(Test.java)\nEND()\n',
        'grove/checks/Test.java': 'class Test {}\n',
        'grove/library/ya.make': 'JAVA_LIBRARY_NON_SPLIT(library)\nEND()\n',
        'build/platform/java/jacoco-agent/ya.make': lib.resource_lib('JACOCO_AGENT', 6),
        SONAR + '/ya.make': 'JAVA_CONTRIB()\nLOCAL_JAR(binary.jar)\nLOCAL_SOURCES_JAR(sources.jar)\nEND()\n',
        SONAR + '/binary.jar': 'binary fixture\n',
        SONAR + '/sources.jar': 'sources fixture\n',
    })
    for peer in ('junit-runner', 'junit5-runner', 'junit6-runner', 'jtest-annotations/junit4', 'jtest-annotations/junit5', 'jtest-annotations/junit6'):
        files['devtools/' + peer + '/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n'
    target = 'grove/checks'
    additional = []
    if mode == 'two-owners':
        files['grove/other/ya.make'] = 'JTEST()\nJAVA_SRCS(Other.java)\nEND()\n'
        files['grove/other/Other.java'] = 'class Other {}\n'
        additional = ['grove/other']
    if mode == 'configured':
        files['grove/checks/ya.make'] = files['grove/checks/ya.make'].replace('JTEST()\n', 'JTEST()\nONLY_TAGS(JTEST_JAR_COMPILATION)\n')
        files['grove/root/ya.make'] = 'PROGRAM()\n' + lib.BARE_MODULE + 'DEPENDS(grove/checks)\nEND()\n'
        target = 'grove/root'
        additional = ['-t', '--target-platform-flag', 'ADD_PEERDIRS_GEN_TESTS=yes']
    if mode == 'renamed':
        files[SONAR + '/ya.make'] = files[SONAR + '/ya.make'].replace('JAVA_CONTRIB()', 'JAVA_CONTRIB(scanner_fixture PREFIX picked_)')
    return files, [target, *additional, *([] if mode == 'build-only' else ['--target-platform-tests'])]


class JavaTestArtifactsTest(unittest.TestCase):
    def test_jtest_compilation_declares_one_unconsumed_artifact_result(self):
        for mode in MODES:
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                artifact_nodes = [n for n in graph['graph'] if any(o.startswith('$(B)/' + SONAR + '/') for o in n['outputs'])]
                if mode in ('library', 'junit5', 'junit6', 'build-only'):
                    self.assertEqual(artifact_nodes, [])
                    continue
                self.assertEqual(len(artifact_nodes), 1)
                artifact = artifact_nodes[0]
                name = 'picked_scanner_fixture' if mode == 'renamed' else 'sonar-scanner-cli-2.8'
                source_name = 'scanner_fixture' if mode == 'renamed' else name
                self.assertEqual(artifact['outputs'], ['$(B)/' + SONAR + '/' + name + '.jar', '$(B)/' + SONAR + '/' + source_name + '-sources.jar'])
                self.assertEqual(set(artifact['inputs']), {'$(S)/build/scripts/fs_tools.py', '$(S)/build/scripts/process_command_files.py', *['$(S)/' + SONAR + '/' + n for n in ('binary.jar', 'sources.jar')]})
                self.assertEqual([c['cmd_args'][-2:] for c in artifact['cmds']], [
                    ['$(S)/' + SONAR + '/binary.jar', artifact['outputs'][0]],
                    ['$(S)/' + SONAR + '/sources.jar', artifact['outputs'][1]],
                ])
                self.assertIn(artifact['uid'], graph['result'])
                self.assertFalse(any(artifact['uid'] in n['deps'] for n in graph['graph']))
                self.assertFalse(any(any(o in n['inputs'] for o in artifact['outputs']) for n in graph['graph']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
