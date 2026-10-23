import os
import unittest
from unittest.mock import patch

import lib
from test_python2 import python2_base


def fixture(collection='PACKAGE', program='PROGRAM', blocked=False):
    files = python2_base() if program == 'PY2_PROGRAM' else {}
    body = lib.BARE_MODULE + 'ALLOCATOR(FAKE)\n'
    if program == 'PY2_PROGRAM':
        body += 'NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\nNO_CHECK_IMPORTS()\nPY_MAIN(main)\nPY_SRCS(TOP_LEVEL main.py)\n'
    else:
        body += 'SRCS(main.cpp)\n'
    files.update({
        'willow/parcel/ya.make': collection + '(parcel)\nPEERDIR(willow/relay)\nEND()\n',
        'willow/relay/ya.make': 'UNION(relay)\n' + ('SET(PASS_PEERS no)\n' if blocked else '') + 'PEERDIR(willow/tool)\nEND()\n',
        'willow/tool/ya.make': program + '(utility)\n' + body + 'END()\n',
        'willow/tool/main.cpp': 'int main(){return 0;}\n',
        'willow/tool/main.py': 'print(1)\n',
    })
    if program == 'DLL':
        lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
    return files


class PackageUnionProgramClosureTest(unittest.TestCase):
    def graph(self, files, *targets):
        graph = lib.make(files, targets[0] if targets else 'willow/parcel', *targets[1:], '--sandboxing', '--add-modules-to-results')
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        return graph

    def check_collection(self, graph, collection='PACKAGE'):
        program = lib.node_by_output(graph, '$(B)/willow/tool/utility')
        relay = lib.node_by_output(graph, '$(B)/willow/relay/relay.pkg.fake')
        output = '$(B)/willow/parcel/parcel.' + ('final' if collection == 'PACKAGE' else 'ci') + '.pkg.fake'
        consumer = lib.node_by_output(graph, output)
        self.assertEqual(relay['inputs'], [])
        self.assertEqual(relay['deps'], [])
        self.assertEqual(relay['outputs'], ['$(B)/willow/relay/relay.pkg.fake'])
        self.assertIn(program['outputs'][0], consumer['inputs'])
        self.assertIn(program['uid'], consumer['deps'])
        self.assertIn(relay['uid'], consumer['deps'])
        if collection == 'PACKAGE':
            self.assertIn('$(B)/willow/parcel/willow/tool/utility', consumer['outputs'])
            self.assertIn(program['outputs'][0], consumer['cmds'][1]['cmd_args'])
        else:
            self.assertEqual(consumer['outputs'], [output])
        return program

    def test_buildfrom_collectors_close_union_program_peers(self):
        for collection in ('PACKAGE', 'CI_GROUP'):
            with self.subTest(collection=collection):
                graph = self.graph(fixture(collection))
                program = self.check_collection(graph, collection)
                compiled = lib.node_by_output(graph, '$(B)/willow/tool/main.cpp.o')
                self.assertIn(compiled['uid'], program['deps'])

    def test_python_program_keeps_bytecode_and_link_producers(self):
        graph = self.graph(fixture(program='PY2_PROGRAM'))
        program = self.check_collection(graph)
        self.assertIn('$(B)/willow/tool/main.py.yapyc', {out for node in graph['graph'] for out in node['outputs']})
        self.assertTrue(program['deps'])

    def test_standalone_union_does_not_link_program_peer(self):
        graph = self.graph(fixture(), 'willow/relay')
        relay = lib.node_by_output(graph, '$(B)/willow/relay/relay.pkg.fake')
        self.assertEqual(relay['inputs'], [])
        self.assertEqual(relay['deps'], [])
        self.assertFalse(any('$(B)/willow/tool/utility' in node['outputs'] for node in graph['graph']))

    def test_pass_peers_override_stops_transitive_program_collection(self):
        for collection in ('PACKAGE', 'CI_GROUP'):
            with self.subTest(collection=collection):
                graph = self.graph(fixture(collection, blocked=True))
                consumer = next(node for node in graph['graph'] if node['kv']['p'] == ('PK' if collection == 'PACKAGE' else 'CI'))
                self.assertIn('$(B)/willow/relay/relay.pkg.fake', consumer['inputs'])
                self.assertIn('$(B)/willow/tool/utility', consumer['inputs'])
                self.assertNotIn('$(B)/willow/parcel/willow/tool/utility', consumer['outputs'])
                program = lib.node_by_output(graph, '$(B)/willow/tool/utility')
                self.assertIn(program['uid'], graph['result'])
                self.assertIn(program['uid'], consumer['deps'])

    def test_no_add_roots_preserve_build_edges_even_when_copying_is_blocked(self):
        for blocked in (False, True):
            with self.subTest(blocked=blocked), patch.dict(os.environ):
                os.environ.pop('YA_STRIP_PACKAGES_FROM_RESULTS', None)
                graph = lib.make(fixture(blocked=blocked), 'willow/parcel')
                consumer = lib.node_by_output(graph, '$(B)/willow/parcel/parcel.final.pkg.fake')
                program = lib.node_by_output(graph, '$(B)/willow/tool/utility')
                self.assertEqual(graph['result'], [consumer['uid']])
                self.assertIn(program['uid'], consumer['deps'])
                self.assertIn('$(B)/willow/tool/utility', consumer['inputs'])
                self.assertEqual('$(B)/willow/parcel/willow/tool/utility' in consumer['outputs'], not blocked)

    def test_pic_linked_peer_keeps_its_selected_producer(self):
        graph = self.graph(fixture(program='DLL'))
        peer = lib.node_by_output(graph, '$(B)/willow/tool/libutility.so')
        consumer = lib.node_by_output(graph, '$(B)/willow/parcel/parcel.final.pkg.fake')
        compiled = lib.node_by_output(graph, '$(B)/willow/tool/main.cpp.pic.o')
        self.assertIn(compiled['uid'], peer['deps'])
        self.assertIn(peer['uid'], consumer['deps'])
        self.assertIn(peer['outputs'][0], consumer['inputs'])
        self.assertIn('$(B)/willow/parcel/willow/tool/libutility.so', consumer['outputs'])

    def test_blocked_pic_peer_retains_own_global_build_input(self):
        files = fixture(program='DLL', blocked=True)
        files['willow/tool/ya.make'] = files['willow/tool/ya.make'].replace('SRCS(main.cpp)', 'SRCS(main.cpp)\nSRCS(GLOBAL global.cpp)')
        files['willow/tool/global.cpp'] = 'int payload;\n'
        graph = self.graph(files)
        peer = lib.node_by_output(graph, '$(B)/willow/tool/libutility.so')
        global_object = next(node for node in graph['graph'] if node['kv']['p'] == 'CC' and '$(S)/willow/tool/global.cpp' in node['inputs'])
        consumer = lib.node_by_output(graph, '$(B)/willow/parcel/parcel.final.pkg.fake')
        self.assertIn(peer['uid'], consumer['deps'])
        self.assertIn(global_object['uid'], consumer['deps'])
        self.assertIn(global_object['outputs'][0], consumer['inputs'])
        self.assertNotIn('$(B)/willow/parcel/willow/tool/libutility.so', consumer['outputs'])
        self.assertNotIn('$(B)/willow/parcel/willow/tool/global.cpp.o', consumer['outputs'])

    def test_transitive_diamond_keeps_one_program_and_copy(self):
        files = fixture()
        files['willow/relay/ya.make'] = 'UNION(relay)\nPEERDIR(willow/left willow/right)\nEND()\n'
        for branch in ('left', 'right'):
            files['willow/' + branch + '/ya.make'] = 'UNION(' + branch + ')\nPEERDIR(willow/tool)\nEND()\n'
        graph = self.graph(files)
        self.check_collection(graph)
        consumer = lib.node_by_output(graph, '$(B)/willow/parcel/parcel.final.pkg.fake')
        self.assertEqual(consumer['outputs'].count('$(B)/willow/parcel/willow/tool/utility'), 1)
        self.assertEqual(consumer['cmds'][1]['cmd_args'].count('$(B)/willow/tool/utility'), 1)
        self.assertEqual(sum('$(B)/willow/tool/utility' in node['outputs'] for node in graph['graph']), 1)

    def test_explicit_and_transitive_roots_are_order_independent(self):
        for targets in (('willow/relay', 'willow/parcel', 'willow/tool'), ('willow/tool', 'willow/parcel', 'willow/relay')):
            with self.subTest(targets=targets):
                graph = self.graph(fixture(), *targets)
                program = self.check_collection(graph)
                self.assertIn(program['uid'], graph['result'])
                self.assertEqual(sum('$(B)/willow/tool/utility' in node['outputs'] for node in graph['graph']), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
