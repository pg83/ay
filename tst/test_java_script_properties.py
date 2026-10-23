import base64
import unittest

import lib


MODES = ('absent', 'literal', 'unset', 'empty', 'value', 'literal-value', 'multiple', 'scripts')


def fixture(mode):
    values = {
        'absent': '',
        'literal': 'CUSTOM_PROPERTY RUNTIME_FLAGS ""',
        'unset': 'CUSTOM_PROPERTY RUNTIME_FLAGS ${FLAGS} ""',
        'empty': 'CUSTOM_PROPERTY RUNTIME_FLAGS ${FLAGS} ""',
        'value': 'CUSTOM_PROPERTY RUNTIME_FLAGS ${FLAGS} ""',
        'literal-value': 'CUSTOM_PROPERTY RUNTIME_FLAGS stable',
        'multiple': 'CUSTOM_PROPERTY first "" CUSTOM_PROPERTY second two words CUSTOM_PROPERTY first last ""',
        'scripts': 'CUSTOM_PROPERTY RUNTIME_FLAGS ""',
        'invalid': 'CUSTOM_PROPERTY RUNTIME_FLAGS',
    }
    body = ''
    if mode == 'empty':
        body += 'SET(FLAGS)\n'
    if mode == 'value':
        body += 'SET(FLAGS -Xms64m -Dmode=stable)\n'
    body += 'GENERATE_SCRIPT(OUT launch.sh TEMPLATE launch.tmpl ' + values[mode] + ')\n'
    if mode == 'scripts':
        body += 'GENERATE_SCRIPT(OUT next.sh TEMPLATE next.tmpl CUSTOM_PROPERTY another "")\n'
    files = lib.java_tree(**{
        'grove/app/ya.make': 'JAVA_PROGRAM(app)\n' + body +
            'COPY_FILE(${CURDIR}/service.yaml ${BINDIR}/descriptors/META-INF/service.yaml)\n'
            'JAVA_SRCS(RESOURCES SRCDIR ${BINDIR}/descriptors FILES ${BINDIR}/descriptors/META-INF/service.yaml)\n'
            'JAVA_SRCS(Main.java)\nEND()\n',
        'grove/app/Main.java': 'class Main {}\n',
        'grove/app/service.yaml': 'kind: service\n',
        'grove/app/launch.tmpl': '{{RUNTIME_FLAGS}}\n',
        'grove/app/next.tmpl': '{{another}}\n',
        'build/platform/java/scriptgen/ya.make': lib.resource_lib('SCRIPTGEN', 11),
    })
    lib.tool_program(files, 'contrib/tools/ijar', 'ijar')
    return files, ['grove/app']


def custom_properties(node):
    commands = [c['cmd_args'] for c in node['cmds'] if '--template' in c['cmd_args']]
    return [[base64.b64decode(args[i + 1]).decode() for i, a in enumerate(args[:-1]) if a == '-B'] for args in commands]


class JavaScriptPropertiesTest(unittest.TestCase):
    def test_explicit_empty_value_is_distinct_from_absent_property(self):
        expected = {
            'absent': [[]],
            'literal': [['RUNTIME_FLAGS=']],
            'unset': [['RUNTIME_FLAGS=']],
            'empty': [['RUNTIME_FLAGS=']],
            'value': [['RUNTIME_FLAGS=-Xms64m -Dmode=stable ']],
            'literal-value': [['RUNTIME_FLAGS=stable']],
            'multiple': [['first=', 'second=two words', 'first=last ']],
            'scripts': [['RUNTIME_FLAGS='], ['another=']],
        }
        for mode in MODES:
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                run = lib.node_by_output(graph, '$(B)/grove/app/app.run.cp.jar')
                self.assertEqual(custom_properties(run), expected[mode])
                copied = lib.node_by_output(graph, '$(B)/grove/app/descriptors/META-INF/service.yaml')
                self.assertIn(copied['outputs'][0], run['inputs'])
                self.assertIn(copied['uid'], run['deps'])
                self.assertIn('$(S)/grove/app/service.yaml', copied['inputs'])
                self.assertIn('$(S)/grove/app/launch.tmpl', run['inputs'])
                self.assertIn('$(B)/grove/app/launch.sh', run['outputs'])
                self.assertIn(run['uid'], graph['result'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
