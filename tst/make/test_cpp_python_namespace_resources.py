"""The native PY_SRCS plugin emits resources in a valid C++ module context."""
import hashlib
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base
from tst.make.test_python2 import python2_base

MODULE = 'grove/ledger'
B = '$(B)/' + MODULE + '/'
S = '$(S)/' + MODULE + '/'


class CppPythonNamespaceResourcesTest(unittest.TestCase):
    def test_namespace_resources_follow_scoped_default_and_explicit_empty_tags(self):
        for setting, tag in (('', 'PY3'), ('SET(MODULE_TAG CUSTOM)\n', 'CUSTOM'), ('SET(MODULE_TAG)\n', '')):
            with self.subTest(setting=setting):
                files = python_base()
                files[MODULE + '/ya.make'] = ('LIBRARY(ledger)\n' + lib.BARE_MODULE
                    + 'SET(PYTHON3 yes)\nSET(_PYTHON_VER py3)\n' + setting + 'PY_SRCS(note.py)\nEND()\n')
                files[MODULE + '/note.py'] = 'value=1\n'
                graph = lib.make(files, MODULE)
                key = 'py/namespace/' + hashlib.md5(b'grove.ledger.note').hexdigest() + '/' + MODULE
                token = key + '="grove.ledger."'
                digest = hashlib.md5((','.join(sorted(['$S/' + MODULE, token])) + tag).encode()).hexdigest()[:26]
                namespace = lib.node_by_output(graph, B + 'objcopy_' + digest + '.o')
                self.assertEqual(namespace['cmds'][0]['cmd_args'][-2:], ['--kvs', key + '=grove.ledger.'])
                data = next(n for n in graph['graph'] if n['outputs'][0].startswith(B + 'objcopy_') and S + 'note.py' in n['inputs'])
                bytecode = lib.node_by_output(graph, B + 'note.py.yapyc3')
                self.assertIn(bytecode['uid'], data['deps'])
                self.assertIn(B + 'note.py.yapyc3', data['inputs'])
                archive = lib.node_by_output(graph, B + 'libledger.global.a')
                for node in (namespace, data):
                    self.assertIn(node['outputs'][0], archive['inputs'])
                    self.assertIn(node['uid'], archive['deps'])
                    for tool in ('rescompiler', 'rescompressor'):
                        producer = lib.node_by_output(graph, '$(B)/tools/' + tool + '/' + tool)
                        self.assertIn(producer['outputs'][0], node['inputs'])
                        self.assertIn(producer['uid'], node['deps'])

    def test_python2_keeps_file_and_bytecode_resources_without_namespace_objects(self):
        for header, body in (('PY2_LIBRARY', ''), ('LIBRARY', 'USE_PYTHON2()\n')):
            with self.subTest(header=header):
                files = python_base()
                files.update(python2_base())
                files[MODULE + '/ya.make'] = header + '(ledger)\n' + lib.BARE_MODULE + body + 'PY_SRCS(note.py)\nEND()\n'
                files[MODULE + '/note.py'] = 'value=1\n'
                graph = lib.make(files, MODULE)
                resources = [n for n in graph['graph'] if n['outputs'][0].startswith(B + 'objcopy_')]
                self.assertTrue(resources)
                self.assertFalse(any(a.startswith('py/namespace/') for n in resources for a in n['cmds'][0]['cmd_args']))
                bytecode = lib.node_by_output(graph, B + 'note.py.yapyc')
                data = next(n for n in resources if S + 'note.py' in n['inputs'])
                self.assertIn(B + 'note.py.yapyc', data['inputs'])
                self.assertIn(bytecode['uid'], data['deps'])

    def test_an_ordinary_cpp_module_does_not_invent_python_resources(self):
        for macro in ('LIBRARY', 'PROGRAM'):
            with self.subTest(macro=macro):
                files = {MODULE + '/ya.make': macro + '(ledger)\n' + lib.BARE_MODULE + 'SRCS(plain.cpp)\nEND()\n',
                         MODULE + '/plain.cpp': ''}
                graph = lib.make(files, MODULE)
                self.assertFalse(any(o.startswith(B + 'objcopy_') for n in graph['graph'] for o in n['outputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
