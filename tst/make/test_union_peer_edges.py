"""UNION Include peers are construction obligations, distinct from NodeDeps."""
import unittest

from tst import lib


B = '$(B)/mint/'
S = '$(S)/mint/'


def fixture(template='pattern.in', consumer=False, secondary=False):
    files = {
        'mint/ya.make': 'UNION(mint)\nFILES(' + template + ')\nFROM_SANDBOX(321 OUT weight.dat)\nEND()\n',
        'mint/' + template: 'physical payload\n',
        'relay/ya.make': 'UNION(relay)\nPEERDIR(mint)\n'
            + (f'RUN_PROGRAM(tools/writer --read IN ${{ARCADIA_BUILD_ROOT}}/mint/{template} '
               '${ARCADIA_BUILD_ROOT}/mint/weight.dat OUT_NOAUTO result.dat)\n' if consumer else '') + 'END()\n',
        'parcel/ya.make': 'PACKAGE(parcel)\nPEERDIR(relay)\nEND()\n',
    }
    lib.tool_program(files, 'tools/writer', 'writer')
    if secondary:
        files['mint/ya.make'] = ('UNION(mint)\nRUN_PROGRAM(tools/writer IN seed.dat '
            f'OUT_NOAUTO marker.dat {template})\nFROM_SANDBOX(321 OUT weight.dat)\nEND()\n')
        files['mint/seed.dat'] = 'real generator seed\n'
    return files


def node(graph, output, kind=None):
    return next(n for n in graph['graph'] if output in n['outputs']
                and (kind is None or n['kv'].get('p') == kind))


def dependencies(graph, value):
    by_uid = {n['uid']: n for n in graph['graph']}
    return {by_uid[u]['outputs'][0] for u in value['deps']}



class UnionPeerEdgesTest(unittest.TestCase):
    def make(self, files, *roots):
        return lib.make(files, roots[-1], *roots[:-1])

    def check_base(self, graph, template='pattern.in', consumer=False, secondary=False):
        leaf = node(graph, B + 'mint.pkg.fake', 'UN')
        relay = node(graph, '$(B)/relay/relay.pkg.fake', 'UN')
        own = node(graph, '$(B)/parcel/parcel.pkg.fake', 'UN')
        final = node(graph, '$(B)/parcel/parcel.final.pkg.fake', 'PK')
        generator = node(graph, B + template, 'PR' if secondary else 'CP')
        payload = node(graph, B + 'weight.dat', 'SB')
        self.assertEqual(dependencies(graph, leaf), {generator['outputs'][0], payload['outputs'][0]})
        self.assertEqual(dependencies(graph, own), set())
        self.assertEqual(own['inputs'], [])
        self.assertEqual(own['outputs'], ['$(B)/parcel/parcel.pkg.fake'])
        self.assertEqual(own['cmds'][0]['cmd_args'][-2:], ['-c', "open('$(B)/parcel/parcel.pkg.fake','w').close()"])
        expected_relay = set()
        if consumer:
            run = node(graph, '$(B)/relay/result.dat', 'PR')
            tool = node(graph, '$(B)/tools/writer/writer')
            expected_relay.add(run['outputs'][0])
            if template.endswith('.in'):
                expected_relay.add(generator['outputs'][0])
            self.assertEqual(dependencies(graph, run), {generator['outputs'][0], payload['outputs'][0], tool['outputs'][0]})
            self.assertEqual(run['cmds'][0]['cmd_args'], [tool['outputs'][0], '--read'])
            self.assertIn(B + template, run['inputs'])
            self.assertIn(B + 'weight.dat', run['inputs'])
            self.assertEqual(S + template in run['inputs'], template.endswith('.in') and not secondary)
            if secondary:
                self.assertIn(B + 'marker.dat', run['inputs'])
                self.assertEqual(dependencies(graph, generator), {tool['outputs'][0]})
        self.assertEqual(dependencies(graph, relay), expected_relay)
        # PACKAGE_FINAL really uses BuildFrom peers and must keep those UIDs.
        self.assertTrue({leaf['outputs'][0], relay['outputs'][0], own['outputs'][0]} <= dependencies(graph, final))
        self.assertEqual(B + template in final['inputs'], template.endswith('.in'))
        if template.endswith('.in'):
            self.assertIn(generator['outputs'][0], dependencies(graph, final))
        self.assertNotIn(S + template, final['inputs'])
        return relay

    def test_package_union_include_peers_have_no_direct_command_edges(self):
        self.check_base(self.make(fixture(), 'parcel'))

    def test_real_late_inputs_keep_copy_tool_and_included_template_producers(self):
        for template in ('pattern.in', 'pattern.dat'):
            for roots in (('mint', 'parcel'), ('parcel', 'mint')):
                with self.subTest(template=template, roots=roots):
                    self.check_base(self.make(fixture(template=template, consumer=True), *roots),
                                    template=template, consumer=True)

    def test_secondary_template_retains_actual_main_output_and_generator(self):
        self.check_base(self.make(fixture(consumer=True, secondary=True), 'parcel'), consumer=True, secondary=True)

    def test_standalone_include_peer_is_not_a_construction_dependency(self):
        graph = self.make(fixture(), 'relay')
        relay = node(graph, '$(B)/relay/relay.pkg.fake', 'UN')
        self.assertEqual(dependencies(graph, relay), set())
        self.assertEqual(relay['inputs'], [])
        self.assertEqual(relay['outputs'], ['$(B)/relay/relay.pkg.fake'])
        self.assertFalse(any(output.startswith(B) for n in graph['graph'] for output in n['outputs']))
        self.assertIn(relay['uid'], graph['result'])

    def test_transitive_diamond_peers_do_not_become_direct_edges(self):
        files = fixture()
        files['left/ya.make'] = 'UNION(left)\nPEERDIR(mint)\nEND()\n'
        files['right/ya.make'] = 'UNION(right)\nPEERDIR(mint)\nEND()\n'
        files['relay/ya.make'] = 'UNION(relay)\nPEERDIR(left right mint left)\nFILES(local.txt)\nEND()\n'
        files['relay/local.txt'] = 'own payload\n'
        for roots in (('relay', 'mint'), ('mint', 'relay')):
            with self.subTest(roots=roots):
                graph = self.make(files, *roots)
                for module in ('left', 'right'):
                    self.assertFalse(any(f'$(B)/{module}/{module}.pkg.fake' in n['outputs'] for n in graph['graph']))
                relay = node(graph, '$(B)/relay/relay.pkg.fake', 'UN')
                self.assertEqual(dependencies(graph, relay), {'$(B)/relay/local.txt'})
                self.assertEqual(relay['inputs'], ['$(B)/relay/local.txt'])
                self.assertEqual(node(graph, '$(B)/relay/local.txt', 'CP')['inputs'], ['$(S)/relay/local.txt'])
                self.assertEqual(dependencies(graph, node(graph, B + 'mint.pkg.fake', 'UN')), {B + 'pattern.in', B + 'weight.dat'})
                self.assertEqual(node(graph, B + 'pattern.in', 'CP')['inputs'], [S + 'pattern.in'])
                self.assertIn(relay['uid'], graph['result'])

    def test_independent_aliases_and_local_producers_keep_their_own_edges(self):
        files = fixture()
        files['relay/ya.make'] = ('UNION(relay)\nPEERDIR(mint)\nFILES(local.txt)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/mint/weight.dat clone.dat)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/mint/pattern.in clone.in)\n'
            'RUN_PROGRAM(tools/writer IN seed.dat OUT_NOAUTO first.dat second.dat)\n'
            'FROM_SANDBOX(654 OUT asset.dat)\nEND()\n')
        files['relay/local.txt'] = 'own copied source\n'
        files['relay/seed.dat'] = 'own generator source\n'
        for roots in (('mint', 'relay'), ('relay', 'mint')):
            with self.subTest(roots=roots):
                graph = self.make(files, *roots)
                relay = node(graph, '$(B)/relay/relay.pkg.fake', 'UN')
                self.assertEqual(dependencies(graph, relay), {'$(B)/relay/local.txt', '$(B)/relay/clone.dat',
                    '$(B)/relay/clone.in', '$(B)/relay/first.dat', '$(B)/relay/asset.dat', B + 'pattern.in'})
                self.assertEqual(dependencies(graph, node(graph, '$(B)/relay/clone.dat', 'CP')), {B + 'weight.dat'})
                self.assertEqual(dependencies(graph, node(graph, '$(B)/relay/clone.in', 'CP')), {B + 'pattern.in'})
                generator = node(graph, '$(B)/relay/second.dat', 'PR')
                self.assertEqual(generator['outputs'], ['$(B)/relay/first.dat', '$(B)/relay/second.dat'])
                self.assertEqual(dependencies(graph, generator), {'$(B)/tools/writer/writer'})
                self.assertIn('$(S)/relay/seed.dat', generator['inputs'])

    def test_explicit_buildfrom_of_peer_root_remains_a_real_input_edge(self):
        files = fixture()
        files['relay/ya.make'] = ('UNION(relay)\nPEERDIR(mint)\n'
            'RUN_PROGRAM(tools/writer IN ${ARCADIA_BUILD_ROOT}/mint/mint.pkg.fake OUT_NOAUTO result.dat)\nEND()\n')
        graph = self.make(files, 'relay')
        run = node(graph, '$(B)/relay/result.dat', 'PR')
        self.assertEqual(dependencies(graph, run), {B + 'mint.pkg.fake', B + 'pattern.in', '$(B)/tools/writer/writer'})
        self.assertIn(B + 'mint.pkg.fake', run['inputs'])
        self.assertEqual(dependencies(graph, node(graph, '$(B)/relay/relay.pkg.fake', 'UN')), {'$(B)/relay/result.dat'})


if __name__ == '__main__':
    unittest.main(verbosity=2)
