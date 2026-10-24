"""CPP style inputs belong to the module declaring their source edges."""
import unittest

import lib


def fixture(case):
    shared = '${ARCADIA_ROOT}/schemas/shared.hpp'
    local = 'RUN_PROGRAM(tools/packer IN_NOPARSE ' + shared + ' OUT_NOAUTO own-unused.tar)\n'
    foreign = 'PEERDIR(grove/pack)\nUSRV_BUILD(FROM ${ARCADIA_BUILD_ROOT}/grove/pack/payload.tar tree.cpp)\n'
    body = local if case == 'local-unused' else foreign + (local if case == 'foreign-plus-local-declaration' else '')
    files = {
        'orchard/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'STYLE_CPP()\n' + body + 'END()\n',
        'grove/pack/ya.make': 'GEN_LIBRARY()\nRUN_PROGRAM(tools/packer IN_NOPARSE ' + shared + ' OUT_NOAUTO payload.tar)\nEND()\n',
        'schemas/shared.hpp': '#pragma once\n',
        'build/config/tests/cpp_style/default_configs.json': '{"clang_format":"configs/cedar-format"}',
        'configs/cedar-format': 'BasedOnStyle: LLVM\n',
        'tools/cpp_style_checker/wrapper.py': '',
        'build/platform/clang/clang-format/ya.make': lib.resource_lib('CLANG_FORMAT', 71),
        'taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make': lib.resource_lib('USRV_UNPACK_TAR', 72),
        'taxi/uservices/linters.make.inc': '# native fixture no project lints\n',
    }
    lib.tool_program(files, 'tools/packer', 'packer')
    return files


class CppStyleOwnedSourcesTest(unittest.TestCase):
    def make(self, case, headers):
        graph = lib.make(fixture(case), 'orchard', '-t', '--add-modules-to-results', '--sandboxing',
                         env={'YA_STRIP_IDLE_BUILD_RESULTS': '0'})
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        suite = lib.node_by_output(graph, '$(B)/orchard/test-results/clang_format/meta.json')
        args = suite['cmds'][0]['cmd_args']
        selected = [arg for arg in args[args.index('run_custom_lint'):] if arg.endswith('.hpp')]
        expected = ['$(S)/schemas/shared.hpp'] if headers else []
        self.assertEqual(selected, expected)
        self.assertEqual([source for source in suite['inputs'] if source.endswith('.hpp')], expected)
        related = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == '--test-related-path']
        self.assertEqual([source for source in related if source.endswith('.hpp')], expected)
        for source in ['$(S)/configs/cedar-format', '$(S)/orchard', '$(S)/tools/cpp_style_checker/wrapper.py']:
            self.assertIn(source, suite['inputs'])
        outputs = {output for node in graph['graph'] for output in node['outputs']}
        self.assertNotIn('$(B)/orchard/own-unused.tar', outputs)
        return graph

    def assert_foreign_producer_chain(self, graph):
        producer = lib.node_by_output(graph, '$(B)/grove/pack/payload.tar')
        self.assertIn('$(S)/schemas/shared.hpp', producer['inputs'])
        tool = lib.node_by_output(graph, '$(B)/tools/packer/packer')
        self.assertIn(tool['uid'], producer['deps'])
        extract = lib.node_by_output(graph, '$(B)/orchard/tree.cpp')
        self.assertIn('$(B)/grove/pack/payload.tar', extract['inputs'])
        self.assertIn(producer['uid'], extract['deps'])
        cc = lib.node_by_output(graph, '$(B)/orchard/tree.cpp.o')
        self.assertIn('$(B)/orchard/tree.cpp', cc['inputs'])
        self.assertIn(extract['uid'], cc['deps'])
        self.assertNotIn('$(S)/schemas/shared.hpp', cc['inputs'])
        archive = next(node for node in graph['graph'] if node['kv'].get('p') == 'AR' and '$(B)/orchard/tree.cpp.o' in node['inputs'])
        self.assertIn(cc['uid'], archive['deps'])

    def test_unused_local_action_keeps_its_declared_header(self):
        graph = self.make('local-unused', True)
        self.assertNotIn('$(B)/tools/packer/packer', {out for node in graph['graph'] for out in node['outputs']})

    def test_consumed_foreign_tar_keeps_header_only_in_its_producer(self):
        self.assert_foreign_producer_chain(self.make('foreign-tar-only', False))

    def test_local_declaration_of_same_foreign_header_keeps_style_obligation(self):
        self.assert_foreign_producer_chain(self.make('foreign-plus-local-declaration', True))


if __name__ == '__main__':
    unittest.main(verbosity=2)
