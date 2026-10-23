import unittest
from collections import Counter

import lib
from test_codegen_llvm_bc import llvm_bc_files


MODULE = 'grove/ledger'
PREFIX = '$(B)/' + MODULE + '/'


def fixture(body):
    files = llvm_bc_files('', {})
    del files['mod/ya.make']
    files[MODULE + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'USE_LLVM_BC20()\n' + body + 'END()\n'
    files['library/cpp/resource/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'END()\n'
    for name in ('anchor.cpp', 'first.cpp', 'second.cpp'):
        files[MODULE + '/' + name] = '#include "local.h"\nint value;\n'
    files[MODULE + '/local.h'] = '#pragma once\n'
    files[MODULE + '/data.txt'] = 'payload\n'
    lib.tool_program(files, 'tools/writer', 'writer')
    return files


def resource_for(graph, input_path):
    return next(n for n in graph['graph'] if n['outputs'][0].startswith(PREFIX + 'objcopy_')
                and input_path in n['inputs'])


class LlvmResourceSourceOrderTest(unittest.TestCase):
    def check(self, body, expected):
        graph = lib.make(fixture(body), MODULE, '--target-platform', 'default-linux-x86_64')
        archive = lib.node_by_output(graph, PREFIX + 'libgrove-ledger.global.a')
        members = []
        for name in expected:
            node = (lib.node_by_output(graph, PREFIX + name) if name.endswith('.o')
                    else resource_for(graph, ('$(S)/' + MODULE + '/data.txt') if name == 'payload'
                                      else PREFIX + name + '_optimized.bc'))
            members.append(node['outputs'][0])
            self.assertIn(node['uid'], archive['deps'])
            self.assertIn(node['outputs'][0], archive['inputs'])
        self.assertEqual([a for a in archive['cmds'][0]['cmd_args'] if a.endswith('.o')], members)
        self.assertEqual(Counter(i for i in archive['inputs'] if i.endswith('.o')), Counter(members))
        for name in ('delta', 'cedar', 'grown'):
            optimized = next((n for n in graph['graph'] if PREFIX + name + '_optimized.bc' in n['outputs']), None)
            if optimized is None:
                continue
            merged = lib.node_by_output(graph, PREFIX + name + '_merged.bc')
            packed = resource_for(graph, optimized['outputs'][0])
            self.assertIn(merged['uid'], optimized['deps'])
            self.assertIn(merged['outputs'][0], optimized['inputs'])
            self.assertIn(optimized['uid'], packed['deps'])
            self.assertIn('$(S)/build/scripts/llvm_opt_wrapper.py', optimized['inputs'])
            for path in merged['inputs']:
                if path.endswith('.bc'):
                    compiled = lib.node_by_output(graph, path)
                    self.assertIn(compiled['uid'], merged['deps'])
                    self.assertIn('$(S)/build/scripts/clang_wrapper.py', compiled['inputs'])
                    if name != 'grown':
                        self.assertIn('$(S)/' + MODULE + '/local.h', compiled['inputs'])
            for tool in ('rescompiler', 'rescompressor'):
                producer = lib.node_by_output(graph, '$(B)/tools/' + tool + '/' + tool)
                self.assertIn(producer['uid'], packed['deps'])
                self.assertIn(producer['outputs'][0], packed['inputs'])
        self.assertTrue(graph['result'])
        return graph

    def test_llvm_resources_keep_default_macro_priority_before_cpp(self):
        for first, second in (('delta', 'cedar'), ('cedar', 'delta')):
            body = 'SRCS(GLOBAL anchor.cpp)\nLLVM_BC(first.cpp NAME ' + first + ')\nLLVM_BC(second.cpp NAME ' + second + ')\n'
            self.check(body, [first, second, 'anchor.cpp.o'])

    def test_llvm_and_explicit_resource_calls_keep_declaration_order(self):
        self.check('LLVM_BC(first.cpp NAME delta)\nRESOURCE(data.txt payload)\n'
                   'SRCS(GLOBAL anchor.cpp)\nLLVM_BC(second.cpp NAME cedar)\n',
                   ['delta', 'payload', 'cedar', 'anchor.cpp.o'])

    def test_llvm_generated_input_observes_native_pending_round(self):
        producer = 'RUN_PROGRAM(tools/writer OUT_NOAUTO grown.cpp receipt.txt)\n'
        source = 'SRCS(GLOBAL anchor.cpp)\nLLVM_BC(grown.cpp NAME grown)\n'
        for body, expected in ((producer + source, ['grown', 'anchor.cpp.o']),
                               (source + producer, ['anchor.cpp.o', 'grown'])):
            graph = self.check(body, expected)
            produced = lib.node_by_output(graph, PREFIX + 'grown.cpp')
            compiled = lib.node_by_output(graph, '$(B)/grown.cpp.bc')
            self.assertEqual(produced['outputs'], [PREFIX + 'grown.cpp', PREFIX + 'receipt.txt'])
            self.assertIn(produced['uid'], compiled['deps'])
            self.assertEqual([i for i in compiled['inputs'] if i.startswith('$(B)/')], [PREFIX + 'grown.cpp'])

    def test_multiple_bitcode_inputs_preserve_every_producer(self):
        graph = self.check('SRCS(GLOBAL anchor.cpp)\nLLVM_BC(first.cpp second.cpp NAME delta SYMBOLS first second)\n',
                           ['delta', 'anchor.cpp.o'])
        merged = lib.node_by_output(graph, PREFIX + 'delta_merged.bc')
        self.assertEqual([a for a in merged['cmds'][0]['cmd_args'] if a.endswith('.bc')],
                         [PREFIX + 'first.cpp.bc', PREFIX + 'second.cpp.bc', PREFIX + 'delta_merged.bc'])

    def test_physical_resource_without_llvm_keeps_its_existing_phase(self):
        self.check('SRCS(GLOBAL anchor.cpp)\nRESOURCE(data.txt payload)\n', ['payload', 'anchor.cpp.o'])


if __name__ == '__main__':
    unittest.main()
