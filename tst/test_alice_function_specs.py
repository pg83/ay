import unittest

import lib
from test_python_srcs import python_base


MODULE = 'grove/catalog'
B = '$(B)/' + MODULE + '/'
GENERATOR = 'alice/functions/json_spec_generator/generator'
REGISTRY = 'alice/functions/proto/proto_registry'
OUTPUTS = ['llm_function_specs.json', 'llm_function_specs_all_versions.json',
           'llm_function_specs_namespaces.json', 'llm_function_specs_en-GB.json',
           'llm_function_specs_en-GB_all_versions.json', 'llm_function_specs_namespaces_en-GB.json']


def fixture(mode='default'):
    files = python_base()
    lib.tool_program(files, GENERATOR, 'generator')
    lib.tool_program(files, REGISTRY, 'proto_registry')
    invocation = {'default': 'ALICE_GENERATE_FUNCTION_SPECS()',
                  'omit': 'ALICE_GENERATE_FUNCTION_SPECS(DONT_ADD_TO_RESOURCE)',
                  'omit-used': 'ALICE_GENERATE_FUNCTION_SPECS(DONT_ADD_TO_RESOURCE ignored)',
                  'later-flag': 'ALICE_GENERATE_FUNCTION_SPECS(ignored DONT_ADD_TO_RESOURCE)',
                  'before': 'ALICE_GENERATE_FUNCTION_SPECS()',
                  'after': 'ALICE_GENERATE_FUNCTION_SPECS()',
                  'python-tool': 'ALICE_GENERATE_FUNCTION_SPECS()',
                  'registry': 'ALICE_GENERATE_FUNCTION_SPECS()',
                  'expanded': '',
                  'expanded-omit': '',
                  'none': ''}[mode]
    if mode in ('expanded', 'expanded-omit'):
        invocation = 'BUNDLE(' + REGISTRY + ' NAME functions_proto_registry.desc)\n'
        invocation += 'RUN_PROGRAM(' + GENERATOR + ' functions_proto_registry.desc ${BINDIR}/llm_function_specs IN functions_proto_registry.desc '
        invocation += ' '.join('OUT_NOAUTO ${BINDIR}/' + output for output in OUTPUTS) + ')\n'
        if mode == 'expanded':
            invocation += ''.join('RESOURCE(${BINDIR}/' + output + ' ' + output + ')\n' for output in OUTPUTS)
    if mode == 'registry':
        from test_proto_desc import desc_files
        files.update(desc_files())
        files[REGISTRY + '/ya.make'] = 'PROTO_REGISTRY(renamed_registry)\nEND()\n'
    before = 'RESOURCE(left.dat left)\nSRCS(mark.cpp)\n' if mode in ('before', 'after') else ''
    after = 'RESOURCE(right.dat right)\n'
    if mode == 'after':
        before, after = after, before
    if mode == 'omit-used':
        after += 'RESOURCE(${BINDIR}/llm_function_specs_namespaces_en-GB.json manual)\n'
    files[MODULE + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + before + invocation + '\n' + after + 'END()\n'
    for name in ('left.dat', 'right.dat', 'mark.cpp'):
        files[MODULE + '/' + name] = ''
    files['reader/ya.make'] = 'PROGRAM()\n' + lib.BARE_MODULE + 'PEERDIR(' + MODULE + ')\nSRCS(main.cpp)\nEND()\n'
    files['reader/main.cpp'] = ''
    if mode == 'python-tool':
        files[GENERATOR + '/ya.make'] = 'PY3_PROGRAM()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nPY_SRCS(MAIN entry.py)\nEND()\n'
        files[GENERATOR + '/entry.py'] = ''
    return files


def object_members(node):
    return [arg for cmd in node['cmds'] for arg in cmd['cmd_args'] if arg.endswith('.o')]


class AliceFunctionSpecsTest(unittest.TestCase):
    def graph(self, mode):
        graph = lib.make(fixture(mode), 'reader', '--target-platform', 'default-linux-x86_64')
        self.assertTrue(graph['result'])
        uids = {node['uid'] for node in graph['graph']}
        self.assertTrue(all(dep in uids for node in graph['graph'] for dep in node['deps']))
        return graph

    def check_producers(self, graph, expected_resources):
        bundle = lib.node_by_output(graph, B + 'functions_proto_registry.desc')
        registry_output = next(path for path in bundle['inputs'] if path.startswith('$(B)/' + REGISTRY + '/'))
        registry = lib.node_by_output(graph, registry_output)
        self.assertIn(registry['uid'], bundle['deps'])
        self.assertIn(registry['outputs'][0], bundle['inputs'])
        run = lib.node_by_output(graph, B + OUTPUTS[0])
        self.assertEqual(run['outputs'], [B + name for name in OUTPUTS])
        self.assertEqual(run['cmds'][0]['cmd_args'], ['$(B)/' + GENERATOR + '/generator',
                                                   B + 'functions_proto_registry.desc', B + 'llm_function_specs'])
        self.assertEqual(run['inputs'], ['$(B)/' + GENERATOR + '/generator', B + 'functions_proto_registry.desc'])
        self.assertIn(bundle['uid'], run['deps'])
        tool = lib.node_by_output(graph, '$(B)/' + GENERATOR + '/generator')
        self.assertIn(tool['uid'], run['deps'])
        resources = [node for node in graph['graph'] if any('/objcopy_' in output for output in node['outputs']) and any(B + name in node['inputs'] for name in OUTPUTS)]
        self.assertEqual(len(resources), expected_resources)
        archive = lib.node_by_output(graph, B + 'libgrove-catalog.global.a')
        for resource in resources:
            self.assertIn(run['uid'], resource['deps'])
            self.assertIn(resource['outputs'][0], archive['inputs'])
            self.assertIn(resource['uid'], archive['deps'])
        link = lib.node_by_output(graph, '$(B)/reader/reader')
        self.assertIn(archive['uid'], link['deps'])
        return archive

    def test_default_and_extra_arguments_keep_six_separate_resources(self):
        for mode in ('default', 'later-flag', 'python-tool', 'registry'):
            with self.subTest(mode=mode):
                self.check_producers(self.graph(mode), 6)

    def test_first_argument_can_disable_resource_callbacks(self):
        graph = self.graph('omit')
        self.assertFalse(any(B + OUTPUTS[0] in node['outputs'] for node in graph['graph']))
        self.assertEqual(len(object_members(lib.node_by_output(graph, B + 'libgrove-catalog.global.a'))), 1)

    def test_disabled_macro_still_declares_all_outputs_for_explicit_consumers(self):
        self.check_producers(self.graph('omit-used'), 1)

    def test_callback_order_retains_surrounding_resources_and_source_phase(self):
        for mode in ('before', 'after'):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                archive = self.check_producers(graph, 6)
                members = object_members(archive)
                def origin(output):
                    node = lib.node_by_output(graph, output)
                    args = node['cmds'][0]['cmd_args']
                    return args[args.index('--inputs') + 1].rsplit('/', 1)[-1]
                self.assertEqual([origin(member) for member in members],
                                 (['left.dat'] + OUTPUTS + ['right.dat']) if mode == 'before' else (['right.dat'] + OUTPUTS + ['left.dat']))
                ordinary = lib.node_by_output(graph, B + 'libgrove-catalog.a')
                self.assertEqual(object_members(ordinary), [B + 'mark.cpp.o'])

    def test_expansion_matches_explicit_native_callbacks(self):
        macro = self.graph('default')
        expanded = self.graph('expanded')
        self.assertEqual(macro, expanded)

    def test_dormant_bundle_uses_the_existing_bundle_path(self):
        self.assertEqual(self.graph('omit'), self.graph('expanded-omit'))

    def test_absent_macro_preserves_ordinary_resource(self):
        graph = self.graph('none')
        self.assertFalse(any(B + OUTPUTS[0] in node['outputs'] for node in graph['graph']))
        self.assertEqual(len(object_members(lib.node_by_output(graph, B + 'libgrove-catalog.global.a'))), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
