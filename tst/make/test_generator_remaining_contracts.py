import json
import unittest

from tst import lib
from tst.make import test_proto_targets
from tst.make import test_resource_files
from tst.make.test_native_python_swig import fixture as native_swig_fixture


def library(body):
    return f'LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n'


class GeneratorRemainingContractsTest(unittest.TestCase):
    def test_ssqls_source_generates_compiled_cpp_and_header(self):
        files = {'app/ya.make': library('SRCS(query.ssqls)'), 'app/query.ssqls': '<DbObject/>\n'}
        lib.tool_program(files, 'metrika/core/tools/ssqls', 'ssqls')
        graph = lib.make(files, 'app')
        producer = lib.node_by_output(graph, '$(B)/app/query.cpp')
        self.assertEqual(producer['outputs'], ['$(B)/app/query.cpp', '$(B)/app/query.h'])
        compiled = lib.node_by_output(graph, '$(B)/app/query.cpp.o')
        self.assertIn(producer['uid'], compiled['deps'])

    def test_generated_library_collects_resources_from_its_peers(self):
        files = {'app/ya.make': library('PEERDIR(gen)\nSRCS(main.cpp)'),
                 'app/main.cpp': '#include "gen/generated.h"\n',
                 'gen/ya.make': 'GEN_LIBRARY()\nPEERDIR(python)\nRUN_PYTHON3(gen.py OUT_NOAUTO generated.h)\nEND()\n',
                 'gen/gen.py': '', 'python/ya.make': lib.resource_lib('YMAKE_PYTHON3', 42)}
        graph = lib.make(files, 'app')
        generated = lib.node_by_output(graph, '$(B)/gen/generated.h')
        self.assertIn(generated['uid'], lib.node_by_output(graph, '$(B)/app/main.cpp.o')['deps'])
        self.assertEqual(generated['cmds'][0]['cmd_args'][0], '$(B)/resources/YMAKE_PYTHON3/bin/python3')

    def test_docs_ignores_native_peer_and_accepts_general_macros(self):
        files = {'guide/ya.make': 'DOCS()\nPEERDIR(native)\nCFLAGS(-DUNUSED)\nSRCS(index.md)\nEND()\n',
                 'guide/index.md': '# Index\n', 'native/ya.make': library('SRCS(main.cpp)'), 'native/main.cpp': ''}
        graph = lib.make(files, 'guide')
        lib.node_by_output(graph, '$(B)/guide/guide.tar.gz')
        self.assertFalse(any(o.startswith('$(B)/native/') for n in graph['graph'] for o in n['outputs']))

    def test_proto_docs_skips_non_proto_sources_and_non_proto_peers(self):
        files = test_proto_targets.target_files()
        files.update({'p/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(DOCS_PROTO)\nUSE_COMMON_GOOGLE_APIS()\nPEERDIR(missing native)\nSRCS(a.proto ignored.cpp)\nEND()\n',
                      'p/ignored.cpp': '', 'native/ya.make': library(''),
                      'contrib/libs/googleapis-common-protos/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(DOCS_PROTO)\nPROTO_NAMESPACE(GLOBAL common)\nEND()\n'})
        lib.tool_program(files, 'vendor/github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc', 'protoc-gen-doc')
        graph = lib.make(files, 'p')
        doc = lib.node_by_output(graph, '$(B)/p/a.md')
        self.assertIn('-I=$(S)/common', doc['cmds'][0]['cmd_args'])
        self.assertFalse(any(o.endswith('ignored.cpp.o') for n in graph['graph'] for o in n['outputs']))

    def test_swig_source_in_plain_library_emits_native_python_outputs(self):
        graph = lib.make(native_swig_fixture('library-default'), 'bindings/python', '--sandboxing')
        wrapper = '$(B)/bindings/python/__/api_wrap.swg.cpp'
        sidecar = '$(B)/bindings/python/Cedar.py'
        producer = lib.node_by_output(graph, wrapper)
        self.assertEqual(producer['outputs'], [wrapper, sidecar])
        self.assertIn('-python', producer['cmds'][0]['cmd_args'])
        tool = lib.node_by_output(graph, '$(B)/contrib/tools/swig/swig')
        self.assertIn(tool['outputs'][0], producer['inputs'])
        self.assertIn(tool['uid'], producer['deps'])
        self.assertIn('$(S)/bindings/api.swg', producer['inputs'])
        compiled = lib.node_by_output(graph, wrapper + '.o')
        self.assertIn(wrapper, compiled['inputs'])
        self.assertIn(producer['uid'], compiled['deps'])
        for header in ['$(S)/bindings/api.h', '$(S)/bindings/detail.h']:
            self.assertNotIn(header, producer['inputs'])
            self.assertIn(header, compiled['inputs'])
        library = lib.node_by_output(graph, '$(B)/bindings/python/libCedar.a')
        self.assertEqual(library['outputs'], ['$(B)/bindings/python/libCedar.a', sidecar])
        self.assertIn(compiled['uid'], library['deps'])
        self.assertIn(producer['uid'], library['deps'])
        self.assertTrue(graph['result'])
        ids = {node['uid'] for node in graph['graph']}
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))

    def test_union_skips_build_root_files_without_producers(self):
        graph = lib.make({'pkg/ya.make': 'UNION()\nFILES(plain.txt ${ARCADIA_BUILD_ROOT}/pkg/missing.txt)\nEND()\n',
                          'pkg/plain.txt': ''}, 'pkg')
        lib.node_by_output(graph, '$(B)/pkg/plain.txt')
        self.assertFalse(any('missing.txt' in p for n in graph['graph'] for p in n['inputs'] + n['outputs']))

    def test_tools_conf_uses_explicit_formula_from_tool_definition(self):
        files = test_resource_files.base_files('YA_TOOLS_CONF(conf)\n')
        files.update({'conf/ya.conf.json': '{}',
                      'conf/tools/tools/custom.tool.json': json.dumps({'tool': {'type': 'simple', 'definition': {'formula': 'conf/formula.json'}}}),
                      'conf/formula.json': '{}'})
        graph = lib.make(files, 'lib')
        payloads = test_resource_files.payloads(graph)
        self.assertTrue(any(inputs == ['$(S)/conf/formula.json'] for inputs, keys, kvs in payloads))

    def test_python_split_generator_deduplicates_shared_induced_sources(self):
        files = {'app/ya.make': library('RUN_PROGRAM(tool OUT_NOAUTO one.cpp two.cpp)\nRUN_PYTHON3(split.py IN one.cpp two.cpp OUT_NOAUTO shard.cpp shard.h)\nSRCS(shard.cpp)'),
                 'app/split.py': '', 'app/shared.h': ''}
        lib.tool_program(files, 'tool', 'tool')
        files['tool/ya.make'] = files['tool/ya.make'].replace('END()', 'INDUCED_DEPS(cpp ${ARCADIA_ROOT}/app/shared.h)\nEND()')
        graph = lib.make(files, 'app')
        compiled = lib.node_by_output(graph, '$(B)/app/shard.cpp.o')
        self.assertEqual(compiled['inputs'].count('$(S)/app/shared.h'), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
