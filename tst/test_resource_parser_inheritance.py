import base64
import collections
import unittest

import lib
from test_resource_header_inputs import fixture as header_fixture
from test_resource_objcopy import section


def fixture(known_header=False):
    files = header_fixture('parsed')
    if known_header:
        files['petal/inputs/fragment.asset'] = files['petal/inputs/source.h']
        files['petal/inputs/source.h'] = '#include "fragment.asset"\n'
    else:
        files['petal/inputs/source.asset'] = files.pop('petal/inputs/source.h')
        for path in ('petal/ya.make', 'petal/inputs/leaf.h', 'oak/owner.cpp'):
            files[path] = files[path].replace('source.h', 'source.asset')
    return files


class ResourceParserInheritanceTest(unittest.TestCase):
    def check(self, roots=('petal',), *, known_header=False, owner=None):
        graph = lib.make(fixture(known_header), roots[-1], '--sandboxing',
                         '--target-platform', 'default-linux-x86_64',
                         '--add-modules-to-results', *roots[:-1])
        resource = lib.node_by_output_prefix(graph, '$(B)/petal/objcopy_')
        payload = '$(S)/petal/inputs/source.' + ('h' if known_header else 'asset')
        expected = ['$(B)/tools/rescompiler/rescompiler',
                    '$(B)/tools/rescompressor/rescompressor',
                    '$(S)/build/scripts/objcopy.py', payload]
        if owner:
            expected += ['$(S)/petal/inputs/leaf.h',
                         '$(S)/' + owner + '/typed/choice.h',
                         '$(S)/' + owner + '/typed/deeper.h']
        if known_header:
            expected.append('$(S)/petal/inputs/fragment.asset')
        self.assertEqual(collections.Counter(resource['inputs']), collections.Counter(expected))
        command = resource['cmds'][0]['cmd_args']
        self.assertEqual(section(command, '--inputs'), [payload])
        self.assertEqual([base64.b64decode(k).decode() for k in section(command, '--keys')], ['payload'])
        binary = lib.node_by_output(graph, '$(B)/petal/Petal')
        self.assertIn(resource['outputs'][0], binary['inputs'])
        self.assertIn(resource['uid'], binary['deps'])
        for tool in ('rescompiler', 'rescompressor'):
            producer = lib.node_by_output(graph, '$(B)/tools/' + tool + '/' + tool)
            self.assertIn(producer['uid'], resource['deps'])
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        return graph

    def test_unknown_resource_uses_no_implicit_cpp_parser(self):
        self.check()

    def test_cpp_include_selects_the_parser_before_resource(self):
        graph = self.check(('oak', 'petal'), owner='oak')
        compiled = lib.node_by_output(graph, '$(B)/oak/owner.cpp.o')
        self.assertEqual(collections.Counter(compiled['inputs']), collections.Counter([
            '$(S)/oak/owner.cpp', '$(S)/petal/inputs/source.asset',
            '$(S)/petal/inputs/leaf.h', '$(S)/oak/typed/choice.h',
            '$(S)/oak/typed/deeper.h']))

    def test_resource_keeps_its_first_parser_when_later_included_from_cpp(self):
        graph = self.check(('petal', 'oak'))
        compiled = lib.node_by_output(graph, '$(B)/oak/owner.cpp.o')
        self.assertEqual(collections.Counter(compiled['inputs']), collections.Counter([
            '$(S)/oak/owner.cpp', '$(S)/petal/inputs/source.asset']))

    def test_registered_header_passes_its_parser_to_unknown_include(self):
        self.check(known_header=True, owner='petal')


if __name__ == '__main__':
    unittest.main(verbosity=2)
