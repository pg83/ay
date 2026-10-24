"""Lazy source producers retain the selected module context's ownership."""
import unittest

import lib
import json

def fixture():
 files = {
  'orchard/ya.make':'PROGRAM(cedar)\n'+lib.BARE_MODULE+'ALLOCATOR(FAKE)\nADDINCL(orchard)\nUSRV_BUILD(FROM ${ARCADIA_ROOT}/archive.tar DEPS_FILE deps.json oak/main.cpp OUT_NOAUTO data.json)\nRESOURCE(data.json cedar/data)\nEND()\n',
  'orchard/deps.json':json.dumps({'oak/main.cpp':['dep.h'],'data.json':[]}),
  'orchard/dep.h':'int cedar;\n','archive.tar':'tiny graph-only source archive\n',
  'grove/ya.make':'UNION()\nPEERDIR(orchard)\nEND()\n',
  'bundle/ya.make':'UNION()\nBUNDLE(orchard NAME cedar)\nEND()\n',
  'collector/ya.make':'CI_GROUP()\nPEERDIR(grove)\nEND()\n',
  'taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make':lib.resource_lib('USRV_UNPACK_TAR',1),
 }
 for tool in ['tools/rescompiler','tools/rescompressor']:lib.tool_program(files,tool,tool.split('/')[-1])
 files['library/cpp/resource/ya.make']='LIBRARY()\n'+lib.BARE_MODULE+'SRCS(resource.cpp)\nEND()\n'
 files['library/cpp/resource/resource.cpp']='int fixture_resource;\n'
 files['taxi/linters.make.inc']='# fixture linters\n'
 files['taxi/uservices/linters.make.inc']='# fixture linters\n'
 files['taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/linters.make.inc']='# fixture linters\n'
 return files


class LazyProducerContextsTest(unittest.TestCase):
    def graph(self, *targets):
        graph = lib.make(fixture(), *targets)
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        source = lib.node_by_output(graph, '$(B)/orchard/oak/main.cpp')
        payload = lib.node_by_output(graph, '$(B)/orchard/data.json')
        for producer in (source, payload):
            self.assertEqual(producer['kv']['p'], 'UT')
            self.assertEqual(producer['inputs'], ['$(S)/archive.tar'])
            self.assertEqual(producer['cmds'][0]['cmd_args'][-4:], [
                '$(S)/archive.tar', '--target', '$(B)/orchard', producer['outputs'][0]])
        compilers = [n for n in graph['graph'] if '$(B)/orchard/_/oak/main.cpp.o' in n['outputs']]
        self.assertTrue(compilers)
        for compiled in compilers:
            self.assertIn('$(B)/orchard/oak/main.cpp', compiled['inputs'])
            self.assertIn('$(S)/orchard/dep.h', compiled['inputs'])
            self.assertIn(source['uid'], compiled['deps'])
        compiled = lib.node_by_output(graph, '$(B)/orchard/_/oak/main.cpp.o')
        resource = next(n for n in graph['graph'] if n['kv']['p'] == 'PY'
                        and '$(B)/orchard/data.json' in n['inputs'])
        self.assertIn(payload['uid'], resource['deps'])
        binary = lib.node_by_output(graph, '$(B)/orchard/cedar')
        self.assertIn(compiled['uid'], binary['deps'])
        self.assertIn(resource['uid'], binary['deps'])
        return graph, binary

    def test_root_and_bundle_contexts_keep_real_producers_in_both_orders(self):
        for targets in (('bundle', 'orchard'), ('orchard', 'bundle')):
            with self.subTest(targets=targets):
                graph, _ = self.graph(*targets)
                bundled = lib.node_by_output(graph, '$(B)/bundle/cedar')
                nodes = {node['uid']: node for node in graph['graph']}
                binaries = [nodes[uid] for uid in bundled['deps']
                            if '$(B)/orchard/cedar' in nodes[uid]['outputs']]
                self.assertEqual(len(binaries), 1)
                binary = binaries[0]
                self.assertIn(binary['outputs'][0], bundled['inputs'])
                self.assertTrue(any('$(B)/orchard/_/oak/main.cpp.o' in nodes[uid]['outputs']
                                    for uid in binary['deps']))
                self.assertTrue(any(nodes[uid]['kv']['p'] == 'PY' and '$(B)/orchard/data.json' in nodes[uid]['inputs']
                                    for uid in binary['deps']))

    def test_collection_consumes_the_program_after_a_producer_only_peer(self):
        graph, binary = self.graph('collector')
        collection = lib.node_by_output(graph, '$(B)/collector/collector.ci.pkg.fake')
        self.assertIn(binary['uid'], collection['deps'])
        self.assertIn(binary['outputs'][0], collection['inputs'])

    def test_direct_and_union_roots_preserve_the_same_source_inputs(self):
        for targets in (('orchard',), ('grove', 'orchard'), ('orchard', 'grove')):
            with self.subTest(targets=targets):
                self.graph(*targets)

    def test_different_archives_cannot_produce_one_output_in_the_same_context(self):
        files = fixture()
        files['orchard/ya.make'] = files['orchard/ya.make'].replace(
            'END()', 'USRV_BUILD(FROM ${ARCADIA_ROOT}/second.tar DEPS_FILE deps.json oak/main.cpp)\nEND()')
        files['second.tar'] = 'different graph-only archive\n'
        result = lib.make_process(files, 'orchard')
        self.assertEqual(result.returncode, 1)
        self.assertIn('duplicate producer for "$(B)/orchard/oak/main.cpp"', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
