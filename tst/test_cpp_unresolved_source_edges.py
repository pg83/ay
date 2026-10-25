import collections
import json
import unittest

import lib
from test_resource_producers import proto_files


def fixture(case):
    if case == 'typed-proto':
        files = fixture('build-proto')
        files['petal/ya.make'] = files['petal/ya.make'].replace(
            'OUT seeded.proto)', 'OUT seeded.proto OUTPUT_INCLUDES petal/dep.proto)').replace(
            'SRCS(main.cpp)', 'SRCS(main.cpp dep.proto)')
        files['petal/dep.proto'] = 'syntax = "proto2";\n'
        files['contrib/tools/protoc/ya.make'] = files['contrib/tools/protoc/ya.make'].replace(
            'END()', 'INDUCED_DEPS(h ${ARCADIA_ROOT}/rt/header.h)\n'
            'INDUCED_DEPS(cpp ${ARCADIA_ROOT}/rt/impl.h)\n'
            'INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/rt/common.h)\nEND()')
        files.update({'rt/header.h': '#pragma once\n', 'rt/impl.h': '#pragma once\n',
                      'rt/common.h': '#include "nested.h"\n', 'rt/nested.h': '#pragma once\n'})
        return files
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

    def test_unused_tool_does_not_report_its_unconsumed_source_diagnostics(self):
        files = fixture('source-candidate')
        files['petal/main.cpp'] = 'int main(){return 0;}\n'
        graph = lib.make(files, 'petal', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        compiled = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
        self.assertEqual(compiled['inputs'], ['$(S)/petal/main.cpp'])
        self.assertEqual(compiled['deps'], [])
        self.assertFalse([n for n in graph['graph']
                          if any(o.startswith('$(B)/tools/gen/') for o in n['outputs'])])
        selected = lib.make_process(files, 'tools/gen', 'petal', '--sandboxing',
                                    '--add-modules-to-results',
                                    '--target-platform', 'default-linux-x86_64')
        self.assertEqual(selected.returncode, 1, selected.stderr)
        self.assertIn('missing-include:', selected.stderr)
        self.assertIn('$(S)/shared/mock.hpp', selected.stderr)
        self.assertIn('unresolved include <harness/h.hpp>', selected.stderr)
        self.assertNotIn('panic:', selected.stderr)

    def test_cached_tool_source_uses_real_consumer_bindings_in_root_order(self):
        files = fixture('source-candidate')
        for first, last in [('tools/gen', 'petal'), ('petal', 'tools/gen')]:
            with self.subTest(first=first):
                result = lib.make_process(files, last, first, '--sandboxing',
                                          '--add-modules-to-results',
                                          '--target-platform', 'default-linux-x86_64')
                if first == 'tools/gen':
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn('missing-include:', result.stderr)
                    self.assertIn('$(S)/shared/mock.hpp', result.stderr)
                    self.assertIn('unresolved include <harness/h.hpp>', result.stderr)
                    self.assertNotIn('panic:', result.stderr)
                    continue
                self.assertEqual(result.returncode, 0, result.stderr)
                graph = json.loads(result.stdout)
                for directory in ['petal', 'tools/gen']:
                    compiled = lib.node_by_output(graph, '$(B)/' + directory + '/main.cpp.o')
                    self.assertCountEqual(compiled['inputs'], [
                        '$(S)/' + directory + '/main.cpp', '$(S)/shared/mock.hpp',
                        '$(S)/petal/support/harness/h.hpp', '$(S)/petal/support/harness/tail.hpp'])
                    self.assertEqual(compiled['deps'], [])

    def test_unparsed_generated_includes_report_missing_include_after_tool_cache_reuse(self):
        for cached in [False, True]:
            with self.subTest(cached=cached):
                files = fixture('source-candidate')
                output = 'unused.txt'
                if cached:
                    output = 'consumed.h'
                    files['petal/ya.make'] = files['petal/ya.make'].replace(
                        'SRCS(main.cpp)', 'RUN_PROGRAM(tools/gen OUT_NOAUTO consumed.h)\nSRCS(main.cpp)')
                files['petal/main.cpp'] = files['petal/main.cpp'].replace(
                    'int main()', '#include "' + output + '"\nint main()')
                result = lib.make_process(files, 'petal', '--sandboxing',
                                          '--target-platform', 'default-linux-x86_64')
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('missing-include:', result.stderr)
                self.assertNotIn('panic:', result.stderr)

    def test_linked_tool_peer_preserves_declared_source_bindings(self):
        for used in [False, True]:
            with self.subTest(used=used):
                files = fixture('source-candidate')
                files['tools/gen/main.cpp'] = 'int main(){return 0;}\n'
                files['tools/gen/ya.make'] = files['tools/gen/ya.make'].replace(
                    'SRCS(main.cpp)', 'PEERDIR(toolpeer)\nSRCS(main.cpp)')
                files['toolpeer/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(owner.cpp)\nEND()\n'
                files['toolpeer/owner.cpp'] = '#include "shared/mock.hpp"\nint peer(){return 0;}\n'
                if used:
                    files['petal/main.cpp'] = files['petal/main.cpp'].replace(
                        'int main()', '#include "unused.txt"\nint main()')
                result = lib.make_process(files, 'petal', '--sandboxing',
                                          '--add-modules-to-results',
                                          '--target-platform', 'default-linux-x86_64')
                if used:
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn('missing-include:', result.stderr)
                    self.assertIn('$(S)/petal/main.cpp', result.stderr)
                    self.assertIn('unresolved include \"unused.txt\"', result.stderr)
                    self.assertNotIn('panic:', result.stderr)
                else:
                    self.assertEqual(result.returncode, 0, result.stderr)
                    graph = json.loads(result.stdout)
                    compiled = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
                    self.assertCountEqual(compiled['inputs'], [
                        '$(S)/petal/main.cpp', '$(S)/shared/mock.hpp',
                        '$(S)/petal/support/harness/h.hpp', '$(S)/petal/support/harness/tail.hpp'])
                    self.assertEqual(compiled['deps'], [])

    def test_sibling_source_diagnostics_keep_their_configuration_order(self):
        from test_sandbox_python_task import fixture as sandbox_fixture
        name = 'work/jobs/InspectDelta'
        for pending in [False, True]:
            with self.subTest(pending=pending):
                files = sandbox_fixture()
                body = ('IF(MODULE_TAG == "SB_TASK_BIN")\nCFLAGS(-DBINARY_VARIANT)\n'
                        'ADDINCL(work/jobs/InspectDelta/support)\nELSE()\n'
                        'CFLAGS(-DLIBRARY_VARIANT)\nENDIF()\nSRC(extra.cpp)\n')
                files[name + '/ya.make'] = files[name + '/ya.make'].replace(
                    'PY_SRCS(__init__.py)', 'PY_SRCS(entry.py)').replace('END()', body + 'END()')
                files[name + '/entry.py'] = ''
                files[name + '/extra.cpp'] = ('#include <harness/h.hpp>\n' if pending else '') + 'int extra;\n'
                files[name + '/support/harness/h.hpp'] = '#pragma once\n'
                result = lib.make_process(files, name, '--sandboxing', '--add-modules-to-results',
                                          '--target-platform', 'default-linux-aarch64', '-t')
                self.assertEqual(result.returncode, 1 if pending else 0, result.stderr)
                if pending:
                    self.assertIn('missing-include:', result.stderr)
                    self.assertIn('$(S)/' + name + '/extra.cpp', result.stderr)
                    self.assertIn('unresolved include <harness/h.hpp>', result.stderr)
                    self.assertNotIn('panic:', result.stderr)

    def test_real_generated_source_requires_the_tool_even_after_cache_reuse(self):
        for cached in [False, True]:
            for bad in [False, True]:
                with self.subTest(cached=cached, bad=bad):
                    files = fixture('source-candidate')
                    if not cached:
                        files['petal/ya.make'] = files['petal/ya.make'].replace(
                            'RUN_PROGRAM(tools/gen OUT_NOAUTO unused.txt)\n', '')
                    files['petal/ya.make'] = files['petal/ya.make'].replace(
                        'SRCS(main.cpp)', 'RUN_PROGRAM(tools/gen OUT generated.cpp)\nSRCS(main.cpp)')
                    if not bad:
                        files['tools/gen/main.cpp'] = 'int main(){return 0;}\n'
                    result = lib.make_process(files, 'petal', '--sandboxing',
                                              '--add-modules-to-results',
                                              '--target-platform', 'default-linux-x86_64')
                    self.assertEqual(result.returncode, 1 if bad else 0, result.stderr)
                    if bad:
                        self.assertIn('missing-include:', result.stderr)
                        self.assertIn('$(S)/shared/mock.hpp', result.stderr)
                        self.assertIn('unresolved include <harness/h.hpp>', result.stderr)
                        self.assertNotIn('panic:', result.stderr)
                        continue
                    graph = json.loads(result.stdout)
                    generated = lib.node_by_output(graph, '$(B)/petal/generated.cpp')
                    compiled = lib.node_by_output(graph, '$(B)/petal/generated.cpp.o')
                    tool = lib.node_by_output(graph, '$(B)/tools/gen/gen')
                    self.assertEqual(compiled['inputs'], ['$(B)/petal/generated.cpp'])
                    self.assertIn(generated['uid'], compiled['deps'])
                    self.assertIn(tool['uid'], generated['deps'])
                    self.assertEqual(generated['foreign_deps']['tool'], [tool['uid']])

    def test_foreign_descent_keeps_only_the_selected_pic_source_command(self):
        files = {
            'dll/ya.make': 'DLL(plug)\n' + lib.BARE_MODULE + 'SRCS(unit.cpp)\nEND()\n',
            'dll/unit.cpp': '#include "header.h"\nint exported;\n',
            'dll/header.h': '#include "leaf.h"\n',
            'dll/leaf.h': '#pragma once\n',
        }
        lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
        graph = lib.make(files, 'dll', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        compiled = lib.node_by_output(graph, '$(B)/dll/unit.cpp.pic.o')
        self.assertCountEqual(compiled['inputs'], [
            '$(S)/dll/unit.cpp', '$(S)/dll/header.h', '$(S)/dll/leaf.h'])
        self.assertEqual(compiled['deps'], [])
        self.assertFalse([n for n in graph['graph'] if '$(B)/dll/unit.cpp.o' in n['outputs']])

    def test_unresolved_source_include_uses_later_real_cpp_consumer_context(self):
        self.check('source-parsed-in')

    def test_generated_proto_header_is_linked_after_its_producer_is_configured(self):
        self.check('build-proto')

    def test_header_discovery_does_not_complete_a_pending_generated_include(self):
        for order in ('before.h seed.proto main.cpp', 'seed.proto before.h main.cpp'):
            with self.subTest(order=order):
                files = proto_files('SRCS(' + order + ')\n', {
                    'lib/before.h': '#include <lib/seed.pb.h>\n',
                    'lib/main.cpp': '#include "before.h"\n',
                    'lib/seed.proto': 'syntax = "proto2";\nmessage Seed {}\n',
                })
                graph = lib.make(files, 'lib', '--sandboxing',
                                 '--target-platform', 'default-linux-x86_64')
                compiled = lib.node_by_output(graph, '$(B)/lib/main.cpp.o')
                producer = lib.node_by_output(graph, '$(B)/lib/seed.pb.h')
                self.assertCountEqual(compiled['inputs'], [
                    '$(S)/lib/main.cpp', '$(S)/lib/before.h', '$(B)/lib/seed.pb.h'])
                self.assertIn(producer['uid'], compiled['deps'])

    def test_peer_header_uses_generated_imports_without_a_cpp_include_consumer(self):
        files = proto_files('PEERDIR(deps)\nSRCS(seed.proto public.h ordinary.cpp)\n', {
            'lib/public.h': '#include <lib/seed.pb.h>\n',
            'lib/seed.proto': 'syntax = "proto2";\nimport "deps/leaf.proto";\nmessage Seed {}\n',
            'lib/ordinary.cpp': 'int ordinary;\n',
            'deps/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(leaf.proto)\nEND()\n',
            'deps/leaf.proto': 'syntax = "proto2";\nmessage Leaf {}\n',
            'petal/ya.make': 'PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\n'
                             'PEERDIR(lib)\nSRCS(main.cpp)\nEND()\n',
            'petal/main.cpp': 'int main(){return 0;}\n',
        })
        graph = lib.make(files, 'petal', '--target-platform', 'default-linux-x86_64')
        linked = lib.node_by_output(graph, '$(B)/petal/Petal')
        roots = ('$(B)/lib/', '$(B)/deps/', '$(B)/petal/')
        expected = ['$(B)/lib/seed.pb.h', '$(B)/deps/leaf.pb.h',
                    '$(B)/lib/liblib.a', '$(B)/deps/libdeps.a', '$(B)/petal/main.cpp.o']
        self.assertCountEqual([p for p in linked['inputs'] if p.startswith(roots)], expected)
        expected_deps = [lib.node_by_output(graph, p)['uid'] for p in expected]
        fixture_deps = {n['uid'] for n in graph['graph']
                        if any(p.startswith(roots) for p in n['outputs'])}
        self.assertCountEqual([uid for uid in linked['deps'] if uid in fixture_deps], expected_deps)
        for output, source in [('$(B)/petal/main.cpp.o', '$(S)/petal/main.cpp'),
                               ('$(B)/lib/ordinary.cpp.o', '$(S)/lib/ordinary.cpp')]:
            compiled = lib.node_by_output(graph, output)
            self.assertEqual(compiled['inputs'], [source])
            self.assertEqual(compiled['deps'], [])
        archive = lib.node_by_output(graph, '$(B)/lib/liblib.a')
        objects = ['$(B)/lib/ordinary.cpp.o', '$(B)/lib/seed.pb.cc.o']
        self.assertCountEqual(archive['inputs'], [*objects, '$(S)/build/scripts/link_lib.py'])
        self.assertCountEqual(archive['deps'], [lib.node_by_output(graph, p)['uid'] for p in objects])

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

    def test_generated_proto_header_preserves_imports_and_header_induced_dependencies(self):
        graph = lib.make(fixture('typed-proto'), 'petal', '--sandboxing')
        compiled = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
        self.assertCountEqual(compiled['inputs'], [
            '$(S)/petal/main.cpp', '$(B)/petal/seeded.pb.h', '$(B)/petal/dep.pb.h',
            '$(S)/rt/header.h', '$(S)/rt/common.h', '$(S)/rt/nested.h'])
        for output in ('seeded.pb.h', 'dep.pb.h'):
            self.assertIn(lib.node_by_output(graph, '$(B)/petal/' + output)['uid'], compiled['deps'])
        implementation = lib.node_by_output(graph, '$(B)/petal/seeded.pb.cc.o')
        self.assertIn('$(S)/rt/impl.h', implementation['inputs'])

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
