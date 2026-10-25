"""CPP ALL_SRCS includes declared action inputs without requiring their outputs."""
import unittest

from tst import lib


def fixture(case):
    producer = 'SET(CEDAR_INPUTS ${ARCADIA_ROOT}/schemas/cedar.hpp ${ARCADIA_ROOT}/schemas/cedar.json)\nRUN_PROGRAM(tools/packer IN_NOPARSE ${CEDAR_INPUTS} OUT_NOAUTO data.tar)\n'
    body = producer + 'USRV_BUILD(FROM ${ARCADIA_BUILD_ROOT}/orchard/data.tar tree.cpp)\n'
    if case == 'direct':
        body = producer.replace('OUT_NOAUTO data.tar', 'OUT tree.cpp')
    elif case == 'parsed':
        body = body.replace('IN_NOPARSE', 'IN')
    elif case == 'unused':
        body = producer
    elif case == 'ordinary':
        body = 'SRCS(local.cpp)\n'
    elif case == 'header':
        body = 'SRCS(${ARCADIA_ROOT}/schemas/cedar.hpp)\n'
    files = {
        'orchard/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'STYLE_CPP()\n' + body + 'END()\n',
        'orchard/local.cpp': '#include "schemas/cedar.hpp"\nint cedar;\n',
        'schemas/cedar.hpp': '#pragma once\n#include "incidental.hpp"\n',
        'schemas/incidental.hpp': '#pragma once\n',
        'schemas/cedar.json': '{}\n',
        'build/config/tests/cpp_style/default_configs.json': '{"clang_format":"configs/cedar-format"}',
        'configs/cedar-format': 'BasedOnStyle: LLVM\n',
        'tools/cpp_style_checker/wrapper.py': '',
        'build/platform/clang/clang-format/ya.make': lib.resource_lib('CLANG_FORMAT', 71),
        'taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make': lib.resource_lib('USRV_UNPACK_TAR', 72),
        'taxi/uservices/linters.make.inc': '# fixture linters\n',
    }
    lib.tool_program(files, 'tools/packer', 'packer')
    files['tools/packer/main.cpp'] = '#include "tool.hpp"\nint main(){return 0;}\n'
    files['tools/packer/tool.hpp'] = '#pragma once\n'
    return files


def make(case):
    graph = lib.make(fixture(case), 'orchard', '-t', '--add-modules-to-results', '--sandboxing', env={'YA_STRIP_IDLE_BUILD_RESULTS': '0'})
    ids = {node['uid'] for node in graph['graph']}
    assert graph['result'] and set(graph['result']) <= ids
    assert all(set(node['deps']) <= ids for node in graph['graph'])
    return graph


class CppStyleDeclaredInputsTest(unittest.TestCase):
    def assert_style_files(self, graph, files):
        node = lib.node_by_output(graph, '$(B)/orchard/test-results/clang_format/meta.json')
        args = node['cmds'][0]['cmd_args']
        selected = [arg for arg in args[args.index('run_custom_lint'):] if arg.endswith(('.cpp', '.hpp'))]
        self.assertEqual(selected, files)
        for file in files:
            self.assertIn(file, node['inputs'])
            self.assertEqual([args[i + 1] for i, arg in enumerate(args[:-1]) if arg == '--test-related-path'].count(file), 1)
        self.assertNotIn('$(S)/schemas/incidental.hpp', node['inputs'])
        self.assertNotIn('$(S)/schemas/cedar.json', node['inputs'])
        self.assertNotIn('$(S)/tools/packer/tool.hpp', node['inputs'])
        self.assertEqual(args.count('CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT'), 2)

    def assert_generator_inputs(self, graph, output, parsed=False):
        node = lib.node_by_output(graph, output)
        for source in ('$(S)/schemas/cedar.hpp', '$(S)/schemas/cedar.json'):
            self.assertIn(source, node['inputs'])
        self.assertEqual('$(S)/schemas/incidental.hpp' in node['inputs'], parsed)
        self.assertTrue(any('$(S)/tools/packer/tool.hpp' in n['inputs'] for n in graph['graph'] if n['kv'].get('p') == 'CC'))
        return node

    def test_ordinary_cpp_includes_do_not_become_style_files(self):
        graph = make('ordinary')
        self.assert_style_files(graph, ['$(S)/orchard/local.cpp'])
        cc = lib.node_by_output(graph, '$(B)/orchard/local.cpp.o')
        for header in ('$(S)/schemas/cedar.hpp', '$(S)/schemas/incidental.hpp'):
            self.assertIn(header, cc['inputs'])

    def test_direct_generator_keeps_declared_header_and_tool_roles(self):
        graph = make('direct')
        self.assert_style_files(graph, ['$(S)/schemas/cedar.hpp'])
        self.assert_generator_inputs(graph, '$(B)/orchard/tree.cpp')

    def test_tar_extraction_preserves_declared_source_header_style(self):
        graph = make('tar')
        self.assert_style_files(graph, ['$(S)/schemas/cedar.hpp'])
        producer = self.assert_generator_inputs(graph, '$(B)/orchard/data.tar')
        extract = lib.node_by_output(graph, '$(B)/orchard/tree.cpp')
        cc = lib.node_by_output(graph, '$(B)/orchard/tree.cpp.o')
        self.assertIn(producer['uid'], extract['deps'])
        self.assertIn('$(B)/orchard/data.tar', extract['inputs'])
        self.assertIn(extract['uid'], cc['deps'])
        self.assertIn('$(B)/orchard/tree.cpp', cc['inputs'])
        self.assertNotIn('$(S)/schemas/cedar.hpp', cc['inputs'])

    def test_parsed_input_closure_stays_distinct_from_style_files(self):
        graph = make('parsed')
        self.assert_style_files(graph, ['$(S)/schemas/cedar.hpp'])
        self.assert_generator_inputs(graph, '$(B)/orchard/data.tar', parsed=True)

    def test_unused_output_still_declares_style_input_without_emitting_tool(self):
        graph = make('unused')
        self.assert_style_files(graph, ['$(S)/schemas/cedar.hpp'])
        outputs = {output for node in graph['graph'] for output in node['outputs']}
        self.assertNotIn('$(B)/orchard/data.tar', outputs)
        self.assertNotIn('$(B)/tools/packer/packer', outputs)

    def test_explicit_header_keeps_its_source_identity(self):
        self.assert_style_files(make('header'), ['$(S)/schemas/cedar.hpp'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
