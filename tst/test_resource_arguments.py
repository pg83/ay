"""RESOURCE arguments are expanded and paired in their module context."""
import base64
import unittest

import lib
from test_resource_objcopy import base_files, objcopy_nodes, section
from test_python_srcs import python_base
from test_python2 import python2_base
from test_go_package import go_module, go_world


def keys(node):
    return [base64.b64decode(value).decode() for value in section(node['cmds'][0]['cmd_args'], '--keys')]


class ResourceArgumentsTest(unittest.TestCase):
    def test_included_pair_list_is_expanded_before_resource_arity_is_checked(self):
        fixed = ''.join(f' payload/schema_{i}.json /schemas/{i}\n' for i in range(6))
        extra = ''.join(f' payload/zone_{i}.yaml /zones/{i}\n' for i in range(8))
        files = base_files('INCLUDE(pairs.inc)\nRESOURCE(\n' + fixed + ' ${ZONE_RESOURCES}\n)\n')
        files['lib/pairs.inc'] = 'SET(ZONE_RESOURCES\n' + extra + ')\n'
        for i in range(6):
            files[f'payload/schema_{i}.json'] = '{}'
        for i in range(8):
            files[f'payload/zone_{i}.yaml'] = 'zone: renamed\n'
        graph = lib.make(files, 'lib')
        [packed] = objcopy_nodes(graph)
        self.assertEqual(keys(packed), [f'/schemas/{i}' for i in range(6)] + [f'/zones/{i}' for i in range(8)])
        self.assertEqual(section(packed['cmds'][0]['cmd_args'], '--inputs'),
                         [f'$(S)/payload/schema_{i}.json' for i in range(6)] +
                         [f'$(S)/payload/zone_{i}.yaml' for i in range(8)])
        self.assertIn(packed['uid'], lib.node_by_output(graph, '$(B)/lib/liblib.global.a')['deps'])

    def test_shared_include_has_different_effective_pair_counts_in_python_contexts(self):
        files = python_base()
        files.update(python2_base())
        files.update({
            'old/ya.make': f'PY2_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(shared)\nEND()\n',
            'new/ya.make': f'PY3_PROGRAM_BIN()\n{lib.BARE_MODULE}PEERDIR(shared)\nEND()\n',
            'shared/ya.make': (
                f'PY23_LIBRARY()\n{lib.BARE_MODULE}'
                'IF(MODULE_TAG == "PY2")\nSET(RESOURCES assets/old.txt /old)\n'
                'ELSE()\nSET(RESOURCES assets/new.txt /new - version=3)\nENDIF()\n'
                'INCLUDE(${ARCADIA_ROOT}/policy/resources.inc)\nEND()\n'
            ),
            'policy/resources.inc': 'RESOURCE(${RESOURCES})\n',
            'assets/old.txt': 'old', 'assets/new.txt': 'new',
        })
        for targets in (('old', 'new'), ('new', 'old')):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                packed = [n for n in graph['graph'] if n['outputs'][0].startswith('$(B)/shared/objcopy_')]
                self.assertEqual(len(packed), 2)
                for version, label in ((2, 'old'), (3, 'new')):
                    node = next(n for n in packed if keys(n) == ['/' + label])
                    self.assertEqual(section(node['cmds'][0]['cmd_args'], '--inputs'), ['$(S)/assets/' + label + '.txt'])
                    self.assertEqual(section(node['cmds'][0]['cmd_args'], '--kvs'), ['version=3'] if version == 3 else [])
                    archive = lib.node_by_output(graph, '$(B)/shared/' + ('libpyshared' if version == 2 else 'libpy3shared') + '.global.a')
                    self.assertIn(node['uid'], archive['deps'])

    def test_expanded_flags_keep_producer_input_and_select_compression(self):
        for flags in ('DONT_PARSE DONT_COMPRESS', 'DONT_COMPRESS DONT_PARSE',
                      'DONT_PARSE', 'DONT_COMPRESS', ''):
            with self.subTest(flags=flags):
                files = base_files(
                    'SET(FLAGS ' + flags + ')\n'
                    'RESOURCE(${FLAGS} ${ARCADIA_BUILD_ROOT}/lib/payload.h /payload)\n'
                    'RUN_PROGRAM(tools/gen OUT_NOAUTO main.bin payload.h '
                    'OUTPUT_INCLUDES ${BINDIR}/detail.h)\n'
                    'RUN_PROGRAM(tools/gen OUT_NOAUTO detail.h)\n'
                )
                lib.tool_program(files, 'tools/rorescompiler', 'rorescompiler')
                lib.tool_program(files, 'contrib/tools/yasm', 'yasm')
                graph = lib.make(files, 'lib', '--target-platform', 'default-linux-x86_64')
                producer = lib.node_by_output(graph, '$(B)/lib/main.bin')
                packed = next(n for n in graph['graph'] if '$(B)/lib/payload.h' in n['inputs']
                              and any(o.endswith(('_raw.auxcpp', '.yasm')) for o in n['outputs']))
                self.assertIn(producer['uid'], packed['deps'])
                self.assertIn('$(B)/lib/main.bin', packed['inputs'])
                self.assertEqual(packed['outputs'][0].endswith('_raw.auxcpp'), 'DONT_COMPRESS' in flags)

    def test_empty_and_quoted_literals_stay_arguments_while_expanded_lists_split_on_spaces(self):
        files = base_files(
            'RESOURCE(- "" "a path.txt" "a key" - "literal\\value" ${UNSET} ${EXTRA})\n'
        )
        files['lib/a path.txt'] = 'file'
        graph = lib.make(files, 'lib', '-DEXTRA=  -   expanded=value  ')
        [packed] = objcopy_nodes(graph)
        self.assertEqual(keys(packed), ['a key'])
        self.assertEqual(section(packed['cmds'][0]['cmd_args'], '--inputs'), ['$(S)/lib/a path.txt'])
        self.assertEqual(section(packed['cmds'][0]['cmd_args'], '--kvs'), ['', 'literalvalue', 'expanded=value'])
        for body in ('RESOURCE()\n', 'RESOURCE(${EMPTY})\n', 'RESOURCE(DONT_PARSE DONT_COMPRESS)\n'):
            with self.subTest(body=body):
                self.assertEqual(objcopy_nodes(lib.make(base_files(body), 'lib')), [])

    def test_dereference_is_one_pass_and_retains_escaped_and_unsupported_references(self):
        files = base_files(
            'RESOURCE(- alias=${ALIAS} - escaped=\\${VALUE} - bare=$VALUE '
            '- other=$OTHER{key} - broken=${} - open=${unfinished '
            '- nested=${outer${VALUE}} - trailing=$VALUE\\ '
            '- underscore=$MACRO_NAME{key} - env=$ENV{RESOURCE_ARGUMENT_VALUE})\n'
        )
        graph = lib.make(files, 'lib', '-DALIAS=${VALUE}', '-DVALUE=one',
                         env={'RESOURCE_ARGUMENT_VALUE': 'environment'})
        [packed] = objcopy_nodes(graph)
        self.assertEqual(section(packed['cmds'][0]['cmd_args'], '--kvs'), [
            'alias=${VALUE}', 'escaped=${VALUE}', 'bare=$VALUE', 'other=$OTHER{key}',
            'broken=${}', 'open=${unfinished', 'nested=${outerone}', 'trailing=$VALUE\\',
            'underscore=$MACRO_NAME{key}', 'env=environment',
        ])

    def test_only_effective_malformed_pairs_fail_and_flags_follow_first_two_positions(self):
        for body, flags, count in (
            ('RESOURCE(${PAIRS})', ['-DPAIRS=a b c'], 3),
            ('RESOURCE(${PAIRS} key)', ['-DPAIRS=a b'], 3),
            ('RESOURCE(${PAIRS})', ['-DPAIRS=- "quoted value"'], 3),
            ('RESOURCE(${PAIRS})', ['-DPAIRS=-\tvalue'], 1),
            ('RESOURCE(DONT_PARSE DONT_PARSE file key)', [], 3),
            ('RESOURCE(DONT_COMPRESS DONT_COMPRESS file key)', [], 3),
        ):
            with self.subTest(body=body, flags=flags):
                result = lib.make_process(base_files(body + '\n'), 'lib', *flags)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'must be even (got {count})', result.stderr)
        files = base_files('IF(NEVER)\nRESOURCE(odd)\nENDIF()\nRESOURCE(ignored DONT_PARSE key)\n')
        files['lib/DONT_PARSE'] = 'resource file'
        [packed] = objcopy_nodes(lib.make(files, 'lib'))
        self.assertEqual(section(packed['cmds'][0]['cmd_args'], '--inputs'), ['$(S)/lib/DONT_PARSE'])
        self.assertEqual(keys(packed), ['key'])

    def test_go_resource_pairs_use_the_same_effective_argument_list(self):
        files = go_world()
        go_module(files, 'library/go/core/resource')
        go_module(files, 'library/go/core/resource/cc', module='GO_PROGRAM')
        go_module(files, 'packet', macros='SET(PAIRS payload/data.txt /data)\nRESOURCE(${PAIRS})\n')
        files['payload/data.txt'] = 'data'
        graph = lib.make(files, 'packet')
        packed = next(n for n in graph['graph'] if n['outputs'][0].endswith('.res.go'))
        self.assertIn('$(S)/payload/data.txt', packed['inputs'])
        self.assertEqual(packed['cmds'][0]['cmd_args'][-2:], ['$(S)/payload/data.txt', 'notafile/data'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
