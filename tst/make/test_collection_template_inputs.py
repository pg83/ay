"""A collection's owned root forwards .in producer dependencies, not late aliases."""
import unittest

from tst import lib


B = '$(B)/supply/'
S = '$(S)/supply/'


def fixture(template='layout.config.in', relay=True, multiple=True, own=False):
    files = {
        'supply/ya.make': ('PACKAGE(supply)' if own else 'UNION(supply)') + '\n'
                        + f'FILES({template})\n'
                        + ('FROM_SANDBOX(321 OUT weight.dat)\n' if multiple else '') + 'END()\n',
        'supply/' + template: 'actual copied template\n',
        'crate/ya.make': 'PACKAGE(crate)\nPEERDIR(' + ('relay' if relay else 'supply') + ')\nEND()\n',
    }
    if relay:
        files['relay/ya.make'] = ('UNION(relay)\nPEERDIR(supply)\n'
            f'RUN_PROGRAM(tools/writer --read IN ${{ARCADIA_BUILD_ROOT}}/supply/{template}'
            ' ${ARCADIA_BUILD_ROOT}/supply/weight.dat OUT_NOAUTO result.dat)\nEND()\n')
    lib.tool_program(files, 'tools/writer', 'writer')
    return files


def node(graph, output, kind):
    return next(n for n in graph['graph'] if output in n['outputs'] and n['kv'].get('p') == kind)


class CollectionTemplateInputsTest(unittest.TestCase):
    def check_package(self, graph, template='layout.config.in', relay=True, multiple=True):
        package = node(graph, '$(B)/crate/crate.final.pkg.fake', 'PK')
        copy = node(graph, B + template, 'CP')
        self.assertIn(B + template, package['inputs'])
        self.assertEqual(package['inputs'].count(B + template), 1)
        self.assertIn(copy['uid'], package['deps'])
        late = [B + template] + ([B + 'weight.dat'] if multiple else [])
        if relay:
            late += ['$(B)/relay/result.dat']
        self.assertEqual(package['outputs'], ['$(B)/crate/crate.final.pkg.fake',
                         *['$(B)/crate/' + v.removeprefix('$(B)/') for v in late]])
        args = package['cmds'][1]['cmd_args']
        self.assertEqual(args[args.index('--build-root') + 2:-1], late)
        self.assertEqual(copy['inputs'], [S + template])
        self.assertEqual(copy['cmds'][0]['cmd_args'][-2:], [S + template, B + template])
        return package

    def test_package_adds_the_real_template_producer_through_peer_union_roots(self):
        graph = lib.make(fixture(), 'crate')
        package = self.check_package(graph)
        self.assertEqual(set(package['inputs']), {
            '$(S)/build/scripts/touch.py', '$(S)/build/scripts/copy_to_dir.py',
            '$(S)/build/scripts/process_command_files.py', '$(B)/crate/crate.pkg.fake',
            B + 'supply.pkg.fake', '$(B)/relay/relay.pkg.fake', B + 'layout.config.in'})
        run = node(graph, '$(B)/relay/result.dat', 'PR')
        copy = node(graph, B + 'layout.config.in', 'CP')
        tool = lib.node_by_output(graph, '$(B)/tools/writer/writer')
        self.assertIn(copy['uid'], run['deps'])
        self.assertIn(tool['uid'], run['deps'])
        self.assertIn(S + 'layout.config.in', run['inputs'])
        self.assertEqual(run['cmds'][0]['cmd_args'], [tool['outputs'][0], '--read'])

    def test_single_and_multiple_late_outputs_keep_real_ownership(self):
        for multiple in (False, True):
            with self.subTest(multiple=multiple):
                graph = lib.make(fixture(relay=False, multiple=multiple), 'crate')
                self.check_package(graph, relay=False, multiple=multiple)
                union = node(graph, B + 'supply.pkg.fake', 'UN')
                copy = node(graph, B + 'layout.config.in', 'CP')
                self.assertIn(copy['uid'], union['deps'])
                self.assertEqual(copy['deps'], [])
                if multiple:
                    sandbox = node(graph, B + 'weight.dat', 'SB')
                    self.assertNotIn(B + 'layout.config.in', sandbox['inputs'])
                    self.assertNotIn(copy['uid'], sandbox['deps'])

    def test_ordinary_payload_does_not_gain_a_template_dependency(self):
        files = fixture(template='seed.dat')
        graph = lib.make(files, 'crate')
        package = node(graph, '$(B)/crate/crate.final.pkg.fake', 'PK')
        self.assertNotIn(B + 'seed.dat', package['inputs'])
        self.assertNotIn(S + 'seed.dat', package['inputs'])
        self.assertNotIn(node(graph, B + 'seed.dat', 'CP')['uid'], package['deps'])

    def test_alias_outputs_keep_their_independent_producers_and_input_scopes(self):
        files = fixture()
        files['reader/ya.make'] = ('UNION(reader)\nPEERDIR(supply)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/supply/weight.dat payload.dat)\n'
            'COPY_FILE(${ARCADIA_BUILD_ROOT}/supply/layout.config.in clone.config.in)\nEND()\n')
        for roots in (('reader', 'crate'), ('crate', 'reader')):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                self.check_package(graph)
                sandbox = node(graph, B + 'weight.dat', 'SB')
                original = node(graph, B + 'layout.config.in', 'CP')
                data = node(graph, '$(B)/reader/payload.dat', 'CP')
                clone = node(graph, '$(B)/reader/clone.config.in', 'CP')
                self.assertEqual(data['inputs'], [B + 'weight.dat'])
                self.assertEqual(data['deps'], [sandbox['uid']])
                self.assertEqual(set(clone['inputs']), {B + 'layout.config.in', S + 'layout.config.in'})
                self.assertEqual(clone['deps'], [original['uid']])

    def test_own_package_context_preserves_copy_and_fake_root_producers(self):
        graph = lib.make(fixture(relay=False, own=True), 'supply')
        package = node(graph, B + 'supply.final.pkg.fake', 'PK')
        union = node(graph, B + 'supply.pkg.fake', 'UN')
        copy = node(graph, B + 'layout.config.in', 'CP')
        for parent in (union, package):
            self.assertIn(copy['uid'], parent['deps'])
            self.assertIn(B + 'layout.config.in', parent['inputs'])
            self.assertIn(S + 'layout.config.in', parent['inputs'])
        self.assertIn(union['uid'], package['deps'])
        self.assertEqual(len([n for n in graph['graph'] if n['kv'].get('p') == 'CP'
                              and B + 'layout.config.in' in n['outputs']]), 1)

    def test_secondary_template_keeps_its_real_main_output_and_tool_dependency(self):
        files = fixture(relay=False)
        files['supply/ya.make'] = ('UNION(supply)\n'
            'RUN_PROGRAM(tools/writer IN seed.dat OUT_NOAUTO marker.dat layout.config.in)\nEND()\n')
        files['supply/seed.dat'] = 'generator seed\n'
        graph = lib.make(files, 'crate')
        generator = node(graph, B + 'layout.config.in', 'PR')
        package = node(graph, '$(B)/crate/crate.final.pkg.fake', 'PK')
        self.assertEqual(generator['outputs'], [B + 'marker.dat', B + 'layout.config.in'])
        self.assertIn(generator['uid'], package['deps'])
        self.assertIn(lib.node_by_output(graph, '$(B)/tools/writer/writer')['uid'], generator['deps'])
        for out in generator['outputs']:
            self.assertIn(out, package['inputs'])
        self.assertIn(S + 'seed.dat', generator['inputs'])
        self.assertNotIn(S + 'seed.dat', package['inputs'])

    def test_shared_lexical_contexts_publish_distinct_template_producers(self):
        files = fixture()
        files['shared/files.inc'] = 'FILES(layout.config.in)\nFROM_SANDBOX(321 OUT weight.dat)\n'
        for module in ('supply', 'other'):
            files[module + '/ya.make'] = f'UNION({module})\nINCLUDE(${{ARCADIA_ROOT}}/shared/files.inc)\nEND()\n'
            files[module + '/layout.config.in'] = module
        files['crate/ya.make'] = 'PACKAGE(crate)\nPEERDIR(supply other)\nEND()\n'
        for roots in (('supply', 'crate', 'other'), ('other', 'crate', 'supply')):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                package = node(graph, '$(B)/crate/crate.final.pkg.fake', 'PK')
                for module in ('supply', 'other'):
                    path = f'$(B)/{module}/layout.config.in'
                    copy = node(graph, path, 'CP')
                    self.assertEqual(copy['inputs'], [f'$(S)/{module}/layout.config.in'])
                    self.assertIn(path, package['inputs'])
                    self.assertIn(copy['uid'], package['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
