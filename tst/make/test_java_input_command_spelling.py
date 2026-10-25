import hashlib
import unittest

from tst import lib


CASES = {
    'source-root': ('${ARCADIA_ROOT}/meadow/spec//api.yaml', '$(S)/meadow/spec/api.yaml'),
    'current-root': ('${CURDIR}/spec//api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'build-root': ('${BINDIR}/stage//api.yaml', '$(B)/meadow/gen/stage/api.yaml'),
    'relative': ('spec//api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'rooted-dot': ('${CURDIR}/spec/./api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'rooted-parent': ('${CURDIR}/spec/../api.yaml', '$(S)/meadow/gen/api.yaml'),
    'relative-parent': ('spec/../api.yaml', '$(S)/meadow/gen/api.yaml'),
    'relative-dot': ('./spec//api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'ordinary': ('${CURDIR}/spec/api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'duplicates': ('${CURDIR}/spec//api.yaml ${CURDIR}/spec/api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
    'not-input': ('${CURDIR}/spec//api.yaml', '$(S)/meadow/gen/spec/api.yaml'),
}


def fixture(kind):
    token, _ = CASES[kind]
    before = ''
    if kind == 'source-root':
        before = 'SET(BASE ${ARCADIA_ROOT}/meadow/spec/)\nDEFAULT(SPEC ${BASE}/api.yaml)\n'
        arguments = '${SPEC}'
    else:
        arguments = token
    if kind == 'build-root':
        before += 'COPY_FILE(api.yaml ${BINDIR}/stage/api.yaml)\n'
    inputs = 'spec/api.yaml' if kind == 'not-input' else arguments
    call = 'Main ' + arguments + ' IN ' + inputs + ' OUT_DIR ${BINDIR}/generated CLASSPATH meadow/tool'
    return lib.java_tree(**{
        'build/platform/java/scriptgen/ya.make': lib.resource_lib('SCRIPTGEN', 9),
        'meadow/tool/ya.make': 'JAVA_PROGRAM(tool)\nNO_LINT()\nJAR_MAIN_CLASS(Main)\nJAVA_SRCS(Main.java)\nEND()\n',
        'meadow/tool/Main.java': 'public class Main {}\n',
        'meadow/gen/ya.make': 'JAVA_LIBRARY_NON_SPLIT(bundle)\nNO_LINT()\n' + before + 'RUN_JAVA_PROGRAM(' + call + ')\nEND()\n',
        'meadow/gen/api.yaml': 'kind: parent\n',
        'meadow/gen/spec/api.yaml': 'kind: local\n',
        'meadow/spec/api.yaml': 'kind: root\n',
    })


def command_paths(kind):
    token, canonical = CASES[kind]
    if token.startswith('${'):
        return token.replace('${ARCADIA_ROOT}', '$(S)').replace('${CURDIR}', '$(S)/meadow/gen').replace('${BINDIR}', '$(B)/meadow/gen').split()
    return [canonical]


class JavaInputCommandSpellingTest(unittest.TestCase):
    def test_command_spelling_and_input_identity_remain_distinct(self):
        for kind, (token, canonical) in CASES.items():
            with self.subTest(kind=kind):
                graph = lib.make(fixture(kind), 'meadow/gen')
                inputs = 'spec/api.yaml' if kind == 'not-input' else token
                preimage = 'Main ' + token + ' IN ' + inputs + ' OUT_DIR ${BINDIR}/generated CLASSPATH meadow/tool'
                digest = hashlib.md5(preimage.encode()).hexdigest()
                producer = lib.node_by_output(graph, '$(B)/meadow/gen/generated.' + digest + '.gentar')
                args = producer['cmds'][1]['cmd_args']
                self.assertEqual(args[args.index('Main') + 1:], command_paths(kind))
                self.assertEqual(producer['inputs'].count(canonical), 1)
                self.assertFalse(any('/./' in name or '/../' in name or '//' in name for name in producer['inputs']))
                tool = lib.node_by_output(graph, '$(B)/meadow/tool/tool.run.cp.jar')
                self.assertIn(tool['outputs'][0], producer['inputs'])
                self.assertIn(tool['uid'], producer['deps'])
                jar = lib.node_by_output(graph, '$(B)/meadow/gen/bundle.jar')
                self.assertIn(producer['outputs'][0], jar['inputs'])
                self.assertIn(producer['uid'], jar['deps'])
                self.assertIn(jar['uid'], graph['result'])
                ids = {node['uid'] for node in graph['graph']}
                self.assertTrue(set(graph['result']) <= ids)
                self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
                if kind == 'build-root':
                    copy = lib.node_by_output(graph, canonical)
                    self.assertIn(copy['uid'], producer['deps'])
                    self.assertIn('$(S)/meadow/gen/api.yaml', copy['inputs'])


if __name__ == '__main__':
    unittest.main()
