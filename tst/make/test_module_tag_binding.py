"""Native module entry and raw resource tags have separate binding phases."""
import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base, kvs, keys, objcopy_nodes
from tst.make.test_python2 import python2_base
from tst.make.test_resource_objcopy import section


MODULE = 'cedar/ledger'
B = '$(B)/' + MODULE + '/'
S = '$(S)/' + MODULE + '/'


def digest(tag, *tokens):
    return hashlib.md5((','.join(sorted([*tokens, '$S/' + MODULE])) + tag).encode()).hexdigest()[:26]


def output(tag, *tokens):
    return B + 'objcopy_' + digest(tag, *tokens) + '.o'


def world(macro='LIBRARY', before='', body=''):
    files = python_base()
    files.update(python2_base())
    lib.tool_program(files, 'tools/engrave', 'engrave')
    files.update({
        MODULE + '/ya.make': before + macro + '(ledger)\n' + lib.BARE_MODULE + body + 'END()\n',
        MODULE + '/paper.txt': 'physical payload\n', MODULE + '/seed.txt': 'genuine generator seed\n',
        MODULE + '/shared.py': 'value=1\n',
    })
    for directory in ('sandbox/bin', 'sandbox/sdk2', 'sandbox/taskbox/worker'):
        files[directory + '/ya.make'] = 'PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nEND()\n'
    return files


def marker_body():
    return ('IF(MODULE_TAG == "")\nRESOURCE(- entry=empty)\nELSE()\nRESOURCE(- entry=bound)\nENDIF()\n'
            'RESOURCE(- literal=1)\nRESOURCE(paper.txt /paper)\n')


class ModuleTagBindingTest(unittest.TestCase):
    def make(self, files, *roots):
        roots = roots or (MODULE,)
        return lib.make(files, roots[-1], *roots[:-1])

    def assert_resources(self, graph, tag, bound):
        packed = objcopy_nodes(graph, MODULE)
        expected = {output(tag, 'literal=1'), output(tag, 'entry=' + ('bound' if bound else 'empty')),
                    output(tag, 'paper.txt', base64.b64encode(b'/paper').decode())}
        actual = {n['outputs'][0] for n in packed if ('--kvs' in n['cmds'][0]['cmd_args'] and kvs(n) in (['literal=1'], ['entry=empty'], ['entry=bound'])) or ('--keys' in n['cmds'][0]['cmd_args'] and keys(n) == ['/paper'])}
        self.assertEqual(actual, expected)
        for n in packed:
            if n['outputs'][0] not in expected:
                continue
            self.assertIn('$(B)/tools/rescompiler/rescompiler', n['inputs'])
            self.assertIn('$(B)/tools/rescompressor/rescompressor', n['inputs'])
            for tool in ('rescompiler', 'rescompressor'):
                self.assertIn(lib.node_by_output(graph, f'$(B)/tools/{tool}/{tool}')['uid'], n['deps'])
        paper = lib.node_by_output(graph, output(tag, 'paper.txt', base64.b64encode(b'/paper').decode()))
        self.assertEqual(section(paper['cmds'][0]['cmd_args'], '--inputs'), [S + 'paper.txt'])
        self.assertIn(S + 'paper.txt', paper['inputs'])

    def test_untagged_cpp_library_and_program_keep_the_empty_native_hash(self):
        for macro in ('LIBRARY', 'PROGRAM'):
            for after in ('', 'SET(MODULE_TAG AFTER_CONTEXT)\n'):
                with self.subTest(macro=macro, after=after):
                    files = world(macro, body=marker_body())
                    files[MODULE + '/ya.make'] += after
                    self.assert_resources(self.make(files), '', False)

    def test_cpp_source_tags_bind_before_or_after_resource_calls(self):
        for macro in ('LIBRARY', 'PROGRAM'):
            for before, inside, final, bound in (
                ('SET(MODULE_TAG PRIOR)\n', '', 'PRIOR', True),
                ('', 'SET(MODULE_TAG INSIDE)\n', 'INSIDE', True),
                ('', '', 'LATER', False),
                ('SET(MODULE_TAG PRIOR)\n', '', '', True),
            ):
                body = inside + marker_body()
                if not inside and final != 'PRIOR':
                    body += 'SET(MODULE_TAG ' + final + ')\n'
                with self.subTest(macro=macro, final=final, inside=inside):
                    self.assert_resources(self.make(world(macro, before, body)), final, bound)

    def test_python_module_initialization_overwrites_an_inherited_source_tag(self):
        for macro, initial in (('PY2_LIBRARY', 'PY2'), ('PY3_LIBRARY', 'PY3'),
                               ('PY2_PROGRAM', 'PY2'), ('PY3_PROGRAM_BIN', 'PY3')):
            for final in (initial, 'LATER', ''):
                body = marker_body() + ('PY_MAIN(catalog.start)\n' if 'PROGRAM' in macro else '')
                if final != initial:
                    body += 'SET(MODULE_TAG ' + final + ')\n'
                with self.subTest(macro=macro, final=final):
                    graph = self.make(world(macro, 'SET(MODULE_TAG PRIOR)\n', body))
                    self.assert_resources(graph, final, True)
                    if 'PROGRAM' in macro:
                        self.assertIn(output(final, 'PY_MAIN=catalog.start:main'),
                                      {n['outputs'][0] for n in objcopy_nodes(graph, MODULE)})

    def test_python_srcs_default_is_scoped_and_keeps_explicit_bindings(self):
        for before, inside, flags, final, bound in (
            ('', '', (), 'PY3', False),
            ('SET(MODULE_TAG EXPLICIT)\n', '', (), 'EXPLICIT', True),
            ('SET(MODULE_TAG)\n', '', (), '', False),
            ('', 'SET(MODULE_TAG)\n', (), '', False),
            ('', '', ('-DMODULE_TAG=',), '', False),
            ('', '', ('-DMODULE_TAG=INHERITED',), 'INHERITED', True),
        ):
            body = ('SET(PYTHON3 yes)\nSET(_PYTHON_VER py3)\n' + inside + marker_body()
                    + 'PY_SRCS(shared.py)\nIF(MODULE_TAG == "PY3")\nRESOURCE(- after=py3)\nENDIF()\n')
            with self.subTest(before=before, inside=inside, flags=flags):
                graph = lib.make(world('LIBRARY', before, body), MODULE, *flags)
                self.assert_resources(graph, final, bound)
                self.assertEqual(output(final, 'after=py3') in {n['outputs'][0] for n in objcopy_nodes(graph, MODULE)}, final == 'PY3')
                self.assertIn(B + 'shared.py.yapyc3', {o for n in graph['graph'] for o in n['outputs']})

    def test_raw_tag_is_not_deeply_substituted_at_resource_hash_time(self):
        body = 'SET(ALIAS $LATER)\nSET(MODULE_TAG $ALIAS)\nSET(LATER RESOLVED)\n' + marker_body()
        self.assert_resources(self.make(world('LIBRARY', body=body)), '$ALIAS', True)

    def test_sandbox_config_tags_keep_language_and_outer_kind_separate(self):
        for macro, variants in (('SANDBOX_PY3_TASK', ('SB_TASK_BIN', 'PY3')),
                                ('SANDBOX_PY23_TASK', ('SB_TASK_BIN', 'PY2', 'PY3'))):
            body = ('NO_PYTHON_INCLUDES()\nSET(FILE ${MODULE_TAG})\n'
                    'INCLUDE(${ARCADIA_ROOT}/shared/context.inc)\n')
            files = world(macro, 'SET(MODULE_TAG PRIOR)\n', body)
            files['shared/context.inc'] = ('PY_SRCS(shared.py)\n'
                'RUN_PROGRAM(tools/engrave --kind=${MODULE_KIND} --tag=${MODULE_TAG} '
                '--version=${_PYTHON_VER} IN seed.txt OUT_NOAUTO ${FILE}.anchor ${FILE}.bin)\n'
                'RESOURCE(${FILE}.bin payload/${FILE})\nRESOURCE(- literal=1)\n')
            files['consumer/ya.make'] = 'PY3_PROGRAM_BIN(consumer)\n' + lib.BARE_MODULE + f'NO_PYTHON_INCLUDES()\nPEERDIR({MODULE})\nEND()\n'
            for roots in ((MODULE, 'consumer'), ('consumer', MODULE)):
                with self.subTest(macro=macro, roots=roots):
                    graph = self.make(files, *roots)
                    for tag in variants:
                        version = 'py2' if macro == 'SANDBOX_PY23_TASK' and tag != 'PY3' else 'py3'
                        producer = lib.node_by_output(graph, B + tag + '.anchor')
                        self.assertEqual(producer['outputs'], [B + tag + '.anchor', B + tag + '.bin'])
                        self.assertEqual(producer['cmds'][0]['cmd_args'], ['$(B)/tools/engrave/engrave',
                            '--kind=' + macro, '--tag=' + tag, '--version=' + version])
                        self.assertIn(S + 'seed.txt', producer['inputs'])
                        self.assertIn(lib.node_by_output(graph, '$(B)/tools/engrave/engrave')['uid'], producer['deps'])
                        packed = lib.node_by_output(graph, output(tag, tag + '.bin', base64.b64encode(('payload/' + tag).encode()).decode()))
                        self.assertIn(producer['uid'], packed['deps'])
                        self.assertTrue({B + tag + '.anchor', B + tag + '.bin'} <= set(packed['inputs']))
                        self.assertIn(output(tag, 'literal=1'), {n['outputs'][0] for n in objcopy_nodes(graph, MODULE)})
                    binary = lib.node_by_output(graph, B + 'ledger')
                    self.assertIn(lib.node_by_output(graph, output('SB_TASK_BIN', 'PY_MAIN=sandbox.taskbox.binary:main'))['uid'], binary['deps'])
                    for version in ('py2', 'py3') if macro == 'SANDBOX_PY23_TASK' else ('py3',):
                        suffix = '.yapyc' if version == 'py2' else '.yapyc3'
                        self.assertEqual(len([n for n in graph['graph'] if B + 'shared.py' + suffix in n['outputs']]), 1)

    def test_final_resource_tag_does_not_rebind_an_earlier_generator_call(self):
        body = ('SET(FILE ${MODULE_TAG})\n'
                'RUN_PROGRAM(tools/engrave --kind=${MODULE_KIND} --tag=${MODULE_TAG} '
                'IN seed.txt OUT_NOAUTO ${FILE}.anchor ${FILE}.bin)\n'
                'RESOURCE(${FILE}.bin /generated)\nSET(MODULE_TAG FINAL)\n')
        graph = self.make(world('PY3_PROGRAM_BIN', body=body))
        producer = lib.node_by_output(graph, B + 'PY3.anchor')
        self.assertEqual(producer['cmds'][0]['cmd_args'], ['$(B)/tools/engrave/engrave', '--kind=PY3_PROGRAM_BIN', '--tag=PY3'])
        packed = lib.node_by_output(graph, output('FINAL', 'PY3.bin', base64.b64encode(b'/generated').decode()))
        self.assertIn(producer['uid'], packed['deps'])
        self.assertTrue({B + 'PY3.anchor', B + 'PY3.bin'} <= set(packed['inputs']))
        self.assertIn(S + 'seed.txt', producer['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
