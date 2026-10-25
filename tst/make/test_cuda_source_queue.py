"""The CUDA plugin declares host SRC explicitly after its noauto actions."""
import unittest

from tst import lib
from tst.make import test_codegen_cuda as cuda_fixtures


def fixture(body):
    files = cuda_fixtures.CudaTest().split_fixture()
    files['cobalt/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}SET(CUDA_ARCHITECTURES sm_80:sm_90a)\n{body}END()\n'
    files['cobalt/sub/alpha.cu'] = '#include "kernel.h"\n'
    files['cobalt/sub/kernel.h'] = ''
    files['cobalt/beta.cu'] = ''
    files['cobalt/plain.cpp'] = ''
    files['cobalt/other.cpp'] = ''
    return files


def check_edges(test, graph, stem, platform=None):
    def node(out):
        return next(n for n in graph['graph'] if out in n['outputs'] and (platform is None or n['platform'] == platform))
    base = '$(B)/cobalt/' + stem
    cc = node(base + '.cudafe1.cpp.o') if platform is None else next(
        n for n in graph['graph'] if n['platform'] == platform and base + '.cudafe1.cpp' in n['inputs'] and n['kv']['p'] == 'CC')
    host_input = next(v for v in cc['inputs'] if v.endswith('.cudafe1.cpp'))
    host = node(host_input)
    base = host_input[:-len('.cudafe1.cpp')]
    fat = node(base + '.fatbin.c')
    device = node(base + '.90a.cubin')
    test.assertEqual(host['outputs'], [base + '.ii', base + '.cudafe1.cpp'])
    test.assertIn(host['uid'], cc['deps'])
    test.assertIn(fat['uid'], cc['deps'])
    test.assertIn(device['uid'], cc['deps'])
    for suffix in ('.ii', '.cudafe1.cpp', '.fatbin.c', '.90a.cudafe1.stub.c', '.90a.cubin'):
        test.assertIn(base + suffix, cc['inputs'])
    test.assertIn('-D__NV_LEGACY_LAUNCH', cc['cmds'][0]['cmd_args'])
    test.assertIn(device['uid'], host['deps'])
    for tool in ('mtime0', 'custom_pid'):
        output = '$(B)/tools/' + tool + '/' + tool
        test.assertIn(lib.node_by_output(graph, output)['uid'], host['deps'])
    if stem == 'sub/alpha':
        test.assertIn('$(S)/cobalt/sub/kernel.h', device['inputs'])


class CudaSourceQueueTest(unittest.TestCase):
    def check_order(self, graph, expected):
        ar = lib.node_by_output(graph, '$(B)/cobalt/libcobalt.a')
        outputs = ['$(B)/cobalt/' + name for name in expected]
        self.assertEqual(ar['cmds'][0]['cmd_args'][10:], outputs)
        self.assertEqual({p for p in ar['inputs'] if p.endswith('.o')}, set(outputs))
        for out in outputs:
            self.assertIn(lib.node_by_output(graph, out)['uid'], ar['deps'])

    def test_plugin_host_src_obeys_priority_and_declared_instruction_order(self):
        cases = [
            ('SRCS(plain.cpp)\nCUDA_SRCS(sub/alpha.cu beta.cu)\nSRCS(other.cpp)\n',
             ['sub/alpha.cudafe1.cpp.o', 'beta.cudafe1.cpp.o', 'plain.cpp.o', 'other.cpp.o']),
            ('CUDA_SRCS(sub/alpha.cu beta.cu)\nSRCS(plain.cpp other.cpp)\n',
             ['sub/alpha.cudafe1.cpp.o', 'beta.cudafe1.cpp.o', 'plain.cpp.o', 'other.cpp.o']),
            ('SRC(plain.cpp)\nCUDA_SRCS(sub/alpha.cu beta.cu)\nSRCS(other.cpp)\n',
             ['plain.cpp.o', 'sub/alpha.cudafe1.cpp.o', 'beta.cudafe1.cpp.o', 'other.cpp.o']),
            ('CUDA_SRCS(sub/alpha.cu)\nSRC(plain.cpp)\nCUDA_SRCS(beta.cu)\nSRCS(other.cpp)\n',
             ['sub/alpha.cudafe1.cpp.o', 'plain.cpp.o', 'beta.cudafe1.cpp.o', 'other.cpp.o']),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                graph = lib.make(fixture(body), 'cobalt')
                self.check_order(graph, expected)
                check_edges(self, graph, 'sub/alpha')
                check_edges(self, graph, 'beta')

    def test_direct_cu_retains_srcs_initial_source_order(self):
        graph = lib.make(fixture('SRCS(plain.cpp sub/alpha.cu other.cpp)\n'), 'cobalt')
        self.check_order(graph, ['plain.cpp.o', '_/sub/alpha.cu.o', 'other.cpp.o'])
        cuda = lib.node_by_output(graph, '$(B)/cobalt/_/sub/alpha.cu.o')
        self.assertIn('$(S)/cobalt/sub/alpha.cu', cuda['inputs'])
        self.assertIn('$(S)/cobalt/sub/kernel.h', cuda['inputs'])

    def test_relative_plugin_source_preserves_host_action_and_edges(self):
        for name in ('sub/alpha.cu', './sub/alpha.cu'):
            with self.subTest(name=name):
                graph = lib.make(fixture(f'SRCS(plain.cpp)\nCUDA_SRCS({name})\n'), 'cobalt')
                self.check_order(graph, ['sub/alpha.cudafe1.cpp.o', 'plain.cpp.o'])
                check_edges(self, graph, 'sub/alpha')

    def test_generated_cuda_primary_input_is_ready_only_after_its_producer(self):
        for before in (False, True):
            gen = 'RUN_PROGRAM(tools/mould OUT_NOAUTO made.cu)\n'
            cuda = 'CUDA_SRCS(${BINDIR}/made.cu)\n'
            files = fixture((gen + cuda if before else cuda + gen) + 'SRCS(plain.cpp other.cpp)\n')
            lib.tool_program(files, 'tools/mould', 'mould')
            with self.subTest(before=before):
                graph = lib.make(files, 'cobalt')
                ordinary = ['plain.cpp.o', 'other.cpp.o']
                host = ['made.cudafe1.cpp.o']
                self.check_order(graph, host + ordinary if before else ordinary + host)
                generated = lib.node_by_output(graph, '$(B)/cobalt/made.cu')
                device = lib.node_by_output(graph, '$(B)/cobalt/made.80.cubin')
                self.assertIn(generated['uid'], device['deps'])
                self.assertIn('$(B)/cobalt/made.cu', device['inputs'])
                check_edges(self, graph, 'made')

    def test_global_source_slots_do_not_make_plugin_src_global(self):
        files = fixture('SRCS(GLOBAL plain.cpp other.cpp)\nCUDA_SRCS(sub/alpha.cu)\n')
        graph = lib.make(files, 'cobalt')
        self.check_order(graph, ['sub/alpha.cudafe1.cpp.o', 'other.cpp.o'])
        global_ar = lib.node_by_output(graph, '$(B)/cobalt/libcobalt.global.a')
        cc = lib.node_by_output(graph, '$(B)/cobalt/plain.cpp.o')
        self.assertEqual(global_ar['cmds'][0]['cmd_args'][10:], ['$(B)/cobalt/plain.cpp.o'])
        self.assertIn(cc['uid'], global_ar['deps'])
        check_edges(self, graph, 'sub/alpha')

    def test_host_target_queues_and_graph_root_orders_are_independent(self):
        files = fixture('CUDA_SRCS(sub/alpha.cu)\nSRCS(plain.cpp)\n')
        lib.tool_program(files, 'tools/consumer', 'consumer')
        files['tools/consumer/ya.make'] = files['tools/consumer/ya.make'].replace('END()', 'PEERDIR(cobalt)\nEND()')
        files['caller/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/consumer OUT body.cpp)\nEND()\n'
        for roots in (('caller', 'cobalt'), ('cobalt', 'caller')):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                archives = [n for n in graph['graph'] if '$(B)/cobalt/libcobalt.a' in n['outputs']]
                self.assertEqual({n['platform'] for n in archives}, {'default-linux-x86_64', 'default-linux-aarch64'})
                for ar in archives:
                    suffix = '.pic.o' if ar['platform'] == 'default-linux-x86_64' else '.o'
                    outputs = ['$(B)/cobalt/' + p + suffix for p in ('sub/alpha.cudafe1.cpp', 'plain.cpp')]
                    self.assertEqual(ar['cmds'][0]['cmd_args'][10:], outputs)
                    for out in outputs:
                        cc = next(n for n in graph['graph'] if out in n['outputs'] and n['platform'] == ar['platform'])
                        self.assertIn(cc['uid'], ar['deps'])
                    check_edges(self, graph, 'sub/alpha', ar['platform'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
