"""Header fields and compatibility tags belong to the owner closed by END."""
import unittest

import lib
from test_python2_native_headers import HEADERS, SCHEMA, INCLUDE, UNICODE, make, outputs, world
from test_python_proto import args


def fixture(macro, inside, after, included_end=False):
    files = world(macro, policy=inside)
    files['renamed/owned/Python.h'] = '#pragma once\n'
    files['renamed/directory/Python.h'] = '#pragma once\n'
    if included_end:
        files['shared/close.inc'] = 'END()\nSET(ARCADIA_PYTHON_UNICODE_SIZE 8)\n'
        files[HEADERS + '/ya.make'] = files[HEADERS + '/ya.make'].replace(
            'END()', 'INCLUDE(${ARCADIA_ROOT}/shared/close.inc)')
    files[HEADERS + '/ya.make'] += after + '\n'
    return files


def complete(test, graph):
    ids = {n['uid'] for n in graph['graph']}
    test.assertTrue(set(graph['result']).issubset(ids))
    for node in graph['graph']:
        test.assertTrue(set(node['deps']).issubset(ids), node['outputs'])


class PythonHeaderOwnerEndTest(unittest.TestCase):
    def graph(self, files, roots=('renamed/consumer',)):
        graph = make(files, list(roots))
        complete(self, graph)
        return graph

    def test_unicode_and_end_from_include_bind_the_finished_owner(self):
        for included in (False, True):
            with self.subTest(included=included):
                files = fixture('PYTHON2_ADDINCL()', 'SET(ARCADIA_PYTHON_UNICODE_SIZE 7)',
                                'SET(ARCADIA_PYTHON_UNICODE_SIZE 9)', included)
                graph = self.graph(files)
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.assertEqual(args(own).count('-DARCADIA_PYTHON_UNICODE_SIZE=7'), 1)
                self.assertNotIn('-DARCADIA_PYTHON_UNICODE_SIZE=9', args(own))
                producer = lib.node_by_output(graph, f'$(B)/{HEADERS}/native_module.reg.cpp')
                compiled = lib.node_by_output(graph, producer['outputs'][0] + '.o')
                self.assertIn(producer['uid'], compiled['deps'])
                self.assertIn(producer['outputs'][0], compiled['inputs'])
                self.assertEqual(args(compiled).count('-DARCADIA_PYTHON_UNICODE_SIZE=7'), 1)
                self.assertIn('$(S)/contrib/libs/python/Include/Python.h', own['inputs'])
                control = fixture('PYTHON2_ADDINCL()', 'SET(ARCADIA_PYTHON_UNICODE_SIZE 7)', '', included)
                if included:
                    control['shared/close.inc'] = 'END()\n'
                self.assertEqual(graph, self.graph(control))

    def test_directory_settings_do_not_switch_the_finished_header_backend(self):
        for inside, after, flag, header in (
            ('SET(USE_ARCADIA_PYTHON yes)', 'SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON 2.7)',
             UNICODE, 'contrib/libs/python/Include/Python.h'),
            ('SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON 2.7)',
             'SET(USE_ARCADIA_PYTHON yes)\nSET(USE_SYSTEM_PYTHON)', '-DSYSTEM_HEADERS', 'supplied/Python.h'),
        ):
            with self.subTest(inside=inside):
                graph = self.graph(fixture('PYTHON2_ADDINCL()', inside, after))
                own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
                self.assertEqual(args(own).count(flag), 1)
                self.assertIn('$(S)/' + header, own['inputs'])
                self.assertEqual(graph, self.graph(fixture('PYTHON2_ADDINCL()', inside, '')))

    def test_python3_header_root_and_bare_alias_are_bound_in_owner_namespace(self):
        for inside, after in (
            ('SET(PY3_BASE_INCLUDE_DIR renamed/owned)', 'SET(PY3_BASE_INCLUDE_DIR renamed/directory)'),
            ('SET(HEADER_DIR renamed/owned)\nSET(PY3_BASE_INCLUDE_DIR $HEADER_DIR)', 'SET(HEADER_DIR renamed/directory)'),
        ):
            with self.subTest(inside=inside):
                graph = self.graph(fixture('PYTHON3_ADDINCL()', inside, after))
                for path in (f'$(B)/{HEADERS}/native.cpp.o', '$(B)/renamed/consumer/reader.cpp.o'):
                    node = lib.node_by_output(graph, path)
                    self.assertIn('$(S)/renamed/owned/Python.h', node['inputs'])
                    self.assertNotIn('$(S)/renamed/directory/Python.h', node['inputs'])
                    self.assertEqual(args(node).count('-I$(S)/renamed/owned'), 1)
                self.assertEqual(graph, self.graph(fixture('PYTHON3_ADDINCL()', inside, '')))

    def test_finished_peer_policy_retains_cpp_or_real_python_producer_obligations(self):
        for owned, directory, version in (
            ('CPP_PROTO_FROM_SCHEMA', 'PY3_PROTO_FROM_SCHEMA', None),
            ('PY_PROTO_FROM_SCHEMA', 'CPP_PROTO_FROM_SCHEMA', 2),
        ):
            with self.subTest(owned=owned):
                inside = f'SET(PEERDIR_TAGS {owned} __EMPTY__)'
                after = f'SET(PEERDIR_TAGS {directory} __EMPTY__)'
                graph = self.graph(fixture('PYTHON2_ADDINCL()', inside, after))
                if version:
                    producer = lib.node_by_output(graph, f'$(B)/{SCHEMA}/record__intpy2___pb2.py')
                    self.assertIn(f'$(S)/{SCHEMA}/record.proto', producer['inputs'])
                    self.assertIn('$(S)/renamed/common/base.proto', producer['inputs'])
                    self.assertIn('$(B)/contrib/tools/protoc_old/protoc_old', args(producer))
                    bytecode = next(n for n in graph['graph'] if any('record__intpy2___pb2.py.' in out and out.endswith('.yapyc') for out in n['outputs']))
                    self.assertIn(producer['uid'], bytecode['deps'])
                    resource = next(n for n in graph['graph'] if any('resfs/src//py_modules/' in a and 'record__intpy2___pb2.py' in a for a in args(n)))
                    self.assertIn(producer['uid'], resource['deps'])
                else:
                    lib.node_by_output(graph, f'$(B)/{SCHEMA}/record.pb.h')
                    self.assertFalse(any('__intpy' in out for out in outputs(graph)))
                self.assertFalse(any('__intpy3__' in out for out in outputs(graph)))
                self.assertEqual(graph, self.graph(fixture('PYTHON2_ADDINCL()', inside, '')))

    def test_shared_include_and_root_orders_do_not_share_finished_header_environment(self):
        files = world(settings='SET(OWNED_SIZE 7)')
        files['shared/headers.inc'] += 'SET(ARCADIA_PYTHON_UNICODE_SIZE ${OWNED_SIZE})\n'
        files[HEADERS + '/ya.make'] += 'SET(ARCADIA_PYTHON_UNICODE_SIZE 9)\n'
        files['renamed/other/ya.make'] = files[HEADERS + '/ya.make'].replace('OWNED_SIZE 7', 'OWNED_SIZE 11').replace('UNICODE_SIZE 9', 'UNICODE_SIZE 13')
        files['renamed/other/native.cpp'] = '#include <Python.h>\nint other;\n'
        for roots in ((HEADERS, 'renamed/other'), ('renamed/other', HEADERS)):
            graph = self.graph(files, roots)
            for path, size in ((HEADERS, 7), ('renamed/other', 11)):
                node = lib.node_by_output(graph, f'$(B)/{path}/native.cpp.o')
                self.assertEqual(args(node).count(f'-DARCADIA_PYTHON_UNICODE_SIZE={size}'), 1)
                self.assertIn('$(S)/contrib/libs/python/Include/Python.h', node['inputs'])
                self.assertEqual(args(node).count(INCLUDE), 1)

    def test_implicit_owner_end_uses_eof_header_values(self):
        files = world(policy='SET(ARCADIA_PYTHON_UNICODE_SIZE 7)')
        files[HEADERS + '/ya.make'] = files[HEADERS + '/ya.make'].replace('END()\n', '')
        graph = lib.make(files, 'renamed/consumer', '-k')
        complete(self, graph)
        own = lib.node_by_output(graph, f'$(B)/{HEADERS}/native.cpp.o')
        self.assertEqual(args(own).count('-DARCADIA_PYTHON_UNICODE_SIZE=7'), 1)
        self.assertIn('$(S)/contrib/libs/python/Include/Python.h', own['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
