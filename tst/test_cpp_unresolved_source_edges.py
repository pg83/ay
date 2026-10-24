import collections
import unittest

import lib
from test_resource_producers import proto_files


def fixture(case):
    if case in ['unresolved-source', 'unresolved-build', 'noresolve']:
        name = 'petal/absent.pb.h' if case == 'unresolved-build' else 'absent.hpp'
        body = 'SET(DONT_RESOLVE_INCLUDES yes)\n' if case == 'noresolve' else ''
        return {'petal/ya.make': 'PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\n' + body + 'SRCS(main.cpp)\nEND()\n', 'petal/main.cpp': '#include <' + name + '>\nint main(){return 0;}\n'}
    if case == 'host-build-proto':
        files = fixture('build-proto')
        for key in list(files):
            if key.startswith('petal/'):
                files['tools/forge/' + key[6:]] = files.pop(key).replace('Petal', 'forge').replace('petal/seeded.pb.h', 'tools/forge/seeded.pb.h')
        files['petal/ya.make'] = 'PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\nRUN_PROGRAM(tools/forge OUT generated.cpp)\nSRCS(main.cpp)\nEND()\n'
        files['petal/main.cpp'] = 'int main(){return 0;}\n'
        return files
    if case == 'build-proto':
        files = proto_files('', {})
        files.pop('lib/ya.make')
        files['petal/ya.make'] = 'PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\nRUN_PROGRAM(tools/gen IN seed.in OUT seeded.proto)\nSRCS(main.cpp)\nEND()\n'
        files['petal/main.cpp'] = '#include <petal/seeded.pb.h>\nint main(){return 0;}\n'
        files['petal/seed.in'] = 'actual seed\n'
        return files
    files = {'petal/ya.make': 'PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\nPEERDIR(oak)\nADDINCL(petal/support)\nSRCS(main.cpp)\nEND()\n', 'petal/main.cpp': '#include "shared/mock.hpp"\nint main(){return 0;}\n', 'oak/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(owner.cpp)\nEND()\n', 'oak/owner.cpp': '#include "shared/mock.hpp"\n', 'shared/mock.hpp': '#include <harness/h.hpp>\n', 'petal/support/harness/h.hpp': '#include "tail.hpp"\n', 'petal/support/harness/tail.hpp': '#pragma once\n'}
    if case == 'source-candidate':
        files['petal/ya.make'] = files['petal/ya.make'].replace('SRCS(main.cpp)', 'RUN_PROGRAM(tools/gen OUT_NOAUTO unused.txt)\nSRCS(main.cpp)')
        files['oak/owner.cpp'] = 'int owner(){return 0;}\n'
        lib.tool_program(files, 'tools/gen', 'gen')
        files['tools/gen/main.cpp'] = '#include "shared/mock.hpp"\nint main(){return 0;}\n'
    if case == 'source-parsed-in':
        files['oak/ya.make'] = files['oak/ya.make'].replace('SRCS(owner.cpp)', 'RUN_PROGRAM(tools/gen IN ${ARCADIA_ROOT}/shared/mock.hpp OUT_NOAUTO unused.txt)\nSRCS(owner.cpp)')
        files['oak/owner.cpp'] = 'int owner(){return 0;}\n'
        lib.tool_program(files, 'tools/gen', 'gen')
    return files

class CppUnresolvedSourceEdgesTest(unittest.TestCase):
    def check(self, case):
        graph = lib.make(fixture(case), 'petal', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        node = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
        expected = ['$(S)/petal/main.cpp']
        if case == 'build-proto':
            expected += ['$(B)/petal/seeded.pb.h']
            producer = lib.node_by_output(graph, '$(B)/petal/seeded.pb.h')
            seed = lib.node_by_output(graph, '$(B)/petal/seeded.proto')
            self.assertIn(producer['uid'], node['deps'])
            self.assertIn(seed['uid'], producer['deps'])
            self.assertIn('$(S)/petal/seed.in', seed['inputs'])
        else:
            expected += ['$(S)/shared/mock.hpp', '$(S)/petal/support/harness/h.hpp',
                         '$(S)/petal/support/harness/tail.hpp']
        self.assertEqual(collections.Counter(node['inputs']), collections.Counter(expected))
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        return graph

    def test_unused_tool_source_does_not_prevent_real_consumer_resolution(self):
        self.check('source-candidate')

    def test_unresolved_source_include_uses_later_real_cpp_consumer_context(self):
        self.check('source-parsed-in')

    def test_generated_proto_header_is_linked_after_its_producer_is_configured(self):
        self.check('build-proto')

    def test_selected_host_cpp_repair_keeps_the_configured_pb_producer(self):
        graph = lib.make(fixture('host-build-proto'), 'petal', '--sandboxing',
                         '--add-modules-to-results', '--target-platform', 'default-linux-x86_64')
        compiled = lib.node_by_output(graph, '$(B)/tools/forge/main.cpp.pic.o')
        producer = lib.node_by_output(graph, '$(B)/tools/forge/seeded.pb.h')
        self.assertEqual(collections.Counter(compiled['inputs']), collections.Counter([
            '$(S)/tools/forge/main.cpp', '$(B)/tools/forge/seeded.pb.h']))
        self.assertIn(producer['uid'], compiled['deps'])
        tool = lib.node_by_output(graph, '$(B)/tools/forge/forge')
        consumer = lib.node_by_output(graph, '$(B)/petal/generated.cpp')
        self.assertIn(tool['uid'], consumer['foreign_deps']['tool'])
        ids = {n['uid'] for n in graph['graph']}
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))

    def test_unresolved_source_and_unconfigured_build_header_still_fail(self):
        for case, include in [('unresolved-source', 'absent.hpp'),
                              ('unresolved-build', 'petal/absent.pb.h')]:
            with self.subTest(case=case):
                result = lib.make_process(fixture(case), 'petal', '--sandboxing')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('missing-include:', result.stderr)
                self.assertIn('unresolved include <' + include + '>', result.stderr)
                self.assertIn('$(S)/petal/main.cpp', result.stderr)

    def test_dont_resolve_includes_keeps_its_native_source_boundary(self):
        graph = lib.make(fixture('noresolve'), 'petal', '--sandboxing')
        compiled = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
        self.assertEqual(compiled['inputs'], ['$(S)/petal/main.cpp'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
