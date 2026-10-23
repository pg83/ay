"""Physical IncludedInputs follow structural BuildFrom edges, not module Include peers."""
import unittest

import lib


B = '$(B)/vellum/'
S = '$(S)/vellum/'


def fixture(bridge=None, direct=False, multiple=True, own=False):
    peers = 'vellum' if bridge is None else 'bridge' + (' vellum' if direct else '')
    files = {
        'vellum/ya.make': ('PACKAGE(vellum)' if own else 'UNION(vellum)') + '\nFILES(pattern.h.in)\n'
            + ('FROM_SANDBOX(321 OUT weight.dat)\n' if multiple else '') + 'END()\n',
        'vellum/pattern.h.in': '#include "detail.h"\n',
        'vellum/detail.h': '#include "leaf.h"\n',
        'vellum/leaf.h': '// physical header\n',
        'satchel/ya.make': f'PACKAGE(satchel)\nPEERDIR({peers})\nEND()\n',
    }
    if bridge is not None:
        files['bridge/ya.make'] = ('UNION(bridge)\nPEERDIR(vellum)\n'
            + (f'RUN_PROGRAM(tools/writer IN ${{ARCADIA_BUILD_ROOT}}/vellum/pattern.h.in '
               f'OUT_NOAUTO {bridge})\n' if bridge else '') + 'END()\n')
    lib.tool_program(files, 'tools/writer', 'writer')
    return files


def node(graph, output, kind):
    return next(n for n in graph['graph'] if output in n['outputs'] and n['kv'].get('p') == kind)


class CollectionSourceInputsTest(unittest.TestCase):
    def check(self, graph, source=True, bridge=None, multiple=True, own=False, copied='pattern.h.in', headers=()):
        package_path = B + 'vellum.final.pkg.fake' if own else '$(B)/satchel/satchel.final.pkg.fake'
        package = node(graph, package_path, 'PK')
        copy = node(graph, B + copied, 'CP')
        union = node(graph, B + 'vellum.pkg.fake', 'UN')
        physical = {S + 'pattern.h.in', *headers}
        self.assertEqual(set(copy['inputs']), physical)
        self.assertEqual(copy['cmds'][0]['cmd_args'][-2:], [S + 'pattern.h.in', B + copied])
        self.assertIn(copy['uid'], union['deps'])
        self.assertIn(copy['uid'], package['deps'])
        self.assertTrue(physical <= set(union['inputs']))
        expected = {'$(S)/build/scripts/touch.py', '$(S)/build/scripts/copy_to_dir.py',
                    '$(S)/build/scripts/process_command_files.py', B + 'vellum.pkg.fake', B + copied}
        if own and multiple:
            expected.add(B + 'weight.dat')
        if not own:
            expected.add('$(B)/satchel/satchel.pkg.fake')
        late = [B + copied] + ([B + 'weight.dat'] if multiple else [])
        if bridge is not None:
            expected.add('$(B)/bridge/bridge.pkg.fake')
            if bridge:
                late.append('$(B)/bridge/' + bridge)
                run = node(graph, '$(B)/bridge/' + bridge, 'PR')
                tool = lib.node_by_output(graph, '$(B)/tools/writer/writer')
                self.assertEqual(run['cmds'][0]['cmd_args'], [tool['outputs'][0]])
                self.assertEqual(set(run['inputs']), {tool['outputs'][0], B + copied, *physical})
                self.assertEqual(set(run['deps']), {tool['uid'], copy['uid']})
                if bridge.endswith('.in'):
                    expected.add('$(B)/bridge/' + bridge)
        if source:
            expected.update(physical)
        self.assertEqual(set(package['inputs']), expected)
        module = 'vellum' if own else 'satchel'
        self.assertEqual(package['outputs'], [package_path, *(late if own else []),
                         *['$(B)/' + module + '/' + p.removeprefix('$(B)/') for p in late]])
        args = package['cmds'][1]['cmd_args']
        self.assertEqual(args[args.index('--build-root') + 2:-1], late)
        self.assertEqual(copy['deps'], [])
        if multiple:
            payload = node(graph, B + 'weight.dat', 'SB')
            self.assertNotIn(copy['uid'], payload['deps'])
            self.assertNotIn(S + 'pattern.h.in', payload['inputs'])
        return package

    def test_leaf_single_and_multiple_outputs_forward_physical_template(self):
        for multiple in (False, True):
            with self.subTest(multiple=multiple):
                self.check(lib.make(fixture(multiple=multiple), 'satchel'), multiple=multiple)

    def test_module_include_is_not_a_physical_include_even_without_consumer(self):
        for bridge in ('', 'result.dat'):
            with self.subTest(bridge=bridge):
                self.check(lib.make(fixture(bridge=bridge), 'satchel'), source=False, bridge=bridge)

    def test_template_buildfrom_forwards_again_in_the_intermediary(self):
        self.check(lib.make(fixture(bridge='result.in'), 'satchel'), bridge='result.in')

    def test_direct_and_transitive_peer_edges_keep_their_distinct_scope(self):
        for peers in ('bridge vellum', 'vellum bridge'):
            files = fixture(bridge='result.dat', direct=True)
            files['satchel/ya.make'] = f'PACKAGE(satchel)\nPEERDIR({peers})\nEND()\n'
            with self.subTest(peers=peers):
                self.check(lib.make(files, 'satchel'), bridge='result.dat')

    def test_root_order_does_not_turn_included_dependency_into_buildfrom(self):
        for bridge in (None, 'result.dat'):
            for roots in (('vellum', 'satchel'), ('satchel', 'vellum')):
                with self.subTest(bridge=bridge, roots=roots):
                    self.check(lib.make(fixture(bridge=bridge), roots[-1], *roots[:-1]), source=bridge is None, bridge=bridge)

    def test_own_package_keeps_the_real_copy_owner(self):
        self.check(lib.make(fixture(own=True), 'vellum'), own=True)

    def test_parsed_header_closure_text_and_noresolve_boundaries(self):
        for context, resolve in ((True, True), (False, True), (True, False)):
            files = fixture()
            body = 'SET(DONT_RESOLVE_INCLUDES no)\n' if resolve else ''
            body += ('COPY_FILE_WITH_CONTEXT' if context else 'COPY_FILE') + '(pattern.h.in copied.h.in)\n'
            files['vellum/ya.make'] = 'UNION(vellum)\n' + body + 'FROM_SANDBOX(321 OUT weight.dat)\nEND()\n'
            with self.subTest(context=context, resolve=resolve):
                self.check(lib.make(files, 'satchel'), copied='copied.h.in',
                           headers=(S + 'detail.h', S + 'leaf.h') if context and resolve else ())

    def test_late_aliases_keep_independent_producers_in_both_root_orders(self):
        files = fixture(bridge='result.dat')
        files['reader/ya.make'] = ('UNION(reader)\nPEERDIR(vellum)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/vellum/weight.dat clone.dat)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/vellum/pattern.h.in clone.in)\nEND()\n')
        for roots in (('reader', 'satchel'), ('satchel', 'reader')):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[-1], *roots[:-1])
                self.check(graph, source=False, bridge='result.dat')
                data = node(graph, '$(B)/reader/clone.dat', 'CP')
                template = node(graph, '$(B)/reader/clone.in', 'CP')
                self.assertEqual(data['inputs'], [B + 'weight.dat'])
                self.assertEqual(data['deps'], [node(graph, B + 'weight.dat', 'SB')['uid']])
                self.assertEqual(set(template['inputs']), {B + 'pattern.h.in', S + 'pattern.h.in'})
                self.assertEqual(template['deps'], [node(graph, B + 'pattern.h.in', 'CP')['uid']])

    def test_shared_source_first_owner_and_text_edges_survive_module_input_forwarding(self):
        for opaque in (False, True):
            for first in ('alpha', 'beta'):
                files = {
                    'shared/pattern.h.in': '#include "choice.h"\n',
                    'left/choice.h': '#include "detail.h"\n', 'left/detail.h': '// left closure\n',
                    'right/choice.h': '// right closure\n',
                    'shared/copy.inc': 'COPY_FILE_WITH_CONTEXT(${ARCADIA_ROOT}/shared/pattern.h.in pattern.h.in)\n',
                    'satchel/ya.make': 'PACKAGE(satchel)\nPEERDIR(alpha beta)\nEND()\n',
                }
                for module, directory in (('alpha', 'left'), ('beta', 'right')):
                    files[module + '/ya.make'] = (f'UNION({module})\nSET(DONT_RESOLVE_INCLUDES no)\n'
                        f'ADDINCL(${{ARCADIA_ROOT}}/{directory})\nINCLUDE(${{ARCADIA_ROOT}}/shared/copy.inc)\nEND()\n')
                if opaque:
                    files['beta/ya.make'] = files['beta/ya.make'].replace('INCLUDE(${ARCADIA_ROOT}/shared/copy.inc)',
                        'COPY_FILE(${ARCADIA_ROOT}/shared/pattern.h.in pattern.h.in)')
                roots = (first, 'satchel', 'beta' if first == 'alpha' else 'alpha')
                with self.subTest(opaque=opaque, roots=roots):
                    graph = lib.make(files, roots[-1], *roots[:-1])
                    parsed = {'$(S)/right/choice.h'} if first == 'beta' and not opaque else {
                        '$(S)/left/choice.h', '$(S)/left/detail.h'}
                    expected = {'$(S)/build/scripts/touch.py', '$(S)/build/scripts/copy_to_dir.py',
                        '$(S)/build/scripts/process_command_files.py', '$(B)/satchel/satchel.pkg.fake',
                        '$(B)/alpha/alpha.pkg.fake', '$(B)/beta/beta.pkg.fake',
                        '$(B)/alpha/pattern.h.in', '$(B)/beta/pattern.h.in', '$(S)/shared/pattern.h.in', *parsed}
                    package = node(graph, '$(B)/satchel/satchel.final.pkg.fake', 'PK')
                    self.assertEqual(set(package['inputs']), expected)
                    for module in ('alpha', 'beta'):
                        copy = node(graph, f'$(B)/{module}/pattern.h.in', 'CP')
                        self.assertEqual(set(copy['inputs']), {'$(S)/shared/pattern.h.in',
                            *(parsed if module == 'alpha' or not opaque else ())})
                        self.assertIn(copy['uid'], package['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
