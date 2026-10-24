import base64
import collections
import unittest

import lib
from test_resource_objcopy import section


def fixture(case):
    body = 'RESOURCE(inputs/source.h payload)\n'
    if case in ['text', 'text-source-first', 'text-resource-first']:
        body = 'RESOURCE(DONT_PARSE inputs/source.h payload)\n'
    if case == 'resource-files':
        body = 'RESOURCE_FILES(inputs/source.h)\n'
    src = 'SRCS(main.cpp)\n'
    main = 'int main(){return 0;}\n'
    if case in ['text-source-first', 'text-resource-first']:
        main = '#include "inputs/source.h"\nint main(){return 0;}\n'
    if case == 'text-source-first':
        body = src + body
        src = ''
    if case == 'owner-peer':
        body = 'PEERDIR(oak)\n' + body
    if case in ['generated', 'generated-text']:
        body = ('RUN_PROGRAM(tools/forge IN seed.dat OUT_NOAUTO generated.h '
                'OUTPUT_INCLUDES petal/inputs/source.h)\n'
                'RESOURCE(${BINDIR}/generated.h payload)\n')
    if case == 'generated-text':
        body = body.replace('RESOURCE(', 'RESOURCE(DONT_PARSE ')
    files = {
        'petal/ya.make': ('PROGRAM(Petal)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\n'
                          'ADDINCL(petal/typed)\n' + body + src + 'END()\n'),
        'petal/main.cpp': main,
        'petal/inputs/source.h': '#include "leaf.h"\n#include <choice.h>\n',
        'petal/inputs/leaf.h': '#include "source.h"\n',
        'petal/typed/choice.h': '#include "deeper.h"\n',
        'petal/typed/deeper.h': '#pragma once\n',
        'petal/seed.dat': 'real generator seed\n',
        'oak/ya.make': ('LIBRARY()\n' + lib.BARE_MODULE + 'ADDINCL(oak/typed)\n'
                        'SRCS(owner.cpp)\nEND()\n'),
        'oak/owner.cpp': '#include "petal/inputs/source.h"\n',
        'oak/typed/choice.h': '#include "deeper.h"\n',
        'oak/typed/deeper.h': '#pragma once\n',
        'library/cpp/resource/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'END()\n',
    }
    for name in ['rescompiler', 'rescompressor', 'forge']:
        lib.tool_program(files, 'tools/' + name, name)
    return files


class ResourceHeaderInputsTest(unittest.TestCase):
    def check(self, case, owner=None):
        graph = lib.make(fixture(case), 'petal', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        resource = lib.node_by_output_prefix(graph, '$(B)/petal/objcopy_')
        command = resource['cmds'][0]['cmd_args']
        payload = '$(B)/petal/generated.h' if case in ['generated', 'generated-text'] else '$(S)/petal/inputs/source.h'
        self.assertEqual(section(command, '--inputs'), [payload])
        keys = [base64.b64decode(key).decode() for key in section(command, '--keys')]
        self.assertEqual(keys, ['resfs/file/inputs/source.h' if case == 'resource-files' else 'payload'])
        expected = ['$(B)/tools/rescompiler/rescompiler', '$(B)/tools/rescompressor/rescompressor',
                    '$(S)/build/scripts/objcopy.py', payload]
        if case in ['generated', 'generated-text']:
            expected.append('$(S)/petal/inputs/source.h')
        if owner:
            expected += ['$(S)/petal/inputs/leaf.h', '$(S)/' + owner + '/typed/choice.h',
                         '$(S)/' + owner + '/typed/deeper.h']
        self.assertEqual(collections.Counter(resource['inputs']), collections.Counter(expected))
        executable = lib.node_by_output(graph, '$(B)/petal/Petal')
        self.assertIn(resource['outputs'][0], executable['inputs'])
        self.assertIn(resource['uid'], executable['deps'])
        for name in ['rescompiler', 'rescompressor']:
            tool = lib.node_by_output(graph, '$(B)/tools/' + name + '/' + name)
            self.assertIn(tool['uid'], resource['deps'])
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        return graph, resource

    def test_parsed_source_resource_keeps_its_transitive_physical_headers(self):
        self.check('parsed', 'petal')

    def test_opaque_source_identity_does_not_inherit_the_parsed_cpp_identity(self):
        for case in ['text', 'text-source-first', 'text-resource-first', 'resource-files']:
            with self.subTest(case=case):
                graph, resource = self.check(case)
                if case in ['text-source-first', 'text-resource-first']:
                    compiled = lib.node_by_output(graph, '$(B)/petal/main.cpp.o')
                    self.assertIn('$(S)/petal/typed/deeper.h', compiled['inputs'])
                    self.assertNotIn('$(S)/petal/typed/deeper.h', resource['inputs'])

    def test_first_configured_header_owner_keeps_its_include_search_context(self):
        graph, resource = self.check('owner-peer', 'oak')
        self.assertNotIn('$(S)/petal/typed/choice.h', resource['inputs'])
        compiled = lib.node_by_output(graph, '$(B)/oak/owner.cpp.o')
        self.assertIn('$(S)/oak/typed/deeper.h', compiled['inputs'])

    def test_generated_resource_keeps_registered_producer_and_declared_header_facts(self):
        for case in ['generated', 'generated-text']:
            with self.subTest(case=case):
                self.check_generated(case)

    def test_raw_and_yasm_packers_keep_parsed_and_text_source_edges(self):
        for suffix, flags in (('_raw.auxcpp', 'DONT_COMPRESS '), ('.yasm', '')):
            for opaque in (False, True):
                with self.subTest(suffix=suffix, opaque=opaque):
                    files = fixture('text' if opaque else 'parsed')
                    files['petal/ya.make'] = files['petal/ya.make'].replace(
                        'RESOURCE(', 'RESOURCE(' + flags).replace(' payload)', ' conftest.py/payload)')
                    for tool in ('tools/rorescompiler', 'contrib/tools/yasm'):
                        lib.tool_program(files, tool, tool.rsplit('/', 1)[-1])
                    graph = lib.make(files, 'petal', '--target-platform', 'default-linux-x86_64')
                    resource = next(n for n in graph['graph']
                                    if n['outputs'][0].startswith('$(B)/petal/')
                                    and n['outputs'][0].endswith(suffix))
                    headers = {p for p in resource['inputs'] if p.startswith('$(S)/petal/')}
                    expected = {'$(S)/petal/inputs/source.h'}
                    if not opaque:
                        expected.update({'$(S)/petal/inputs/leaf.h', '$(S)/petal/typed/choice.h',
                                         '$(S)/petal/typed/deeper.h'})
                    self.assertEqual(headers, expected)

    def check_generated(self, case):
        graph, resource = self.check(case, 'petal')
        producer = lib.node_by_output(graph, '$(B)/petal/generated.h')
        self.assertIn(producer['uid'], resource['deps'])
        self.assertIn('$(S)/petal/seed.dat', producer['inputs'])
        tool = lib.node_by_output(graph, '$(B)/tools/forge/forge')
        self.assertIn(tool['uid'], producer['foreign_deps']['tool'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
