import unittest

from tst import lib


BODIES = {
    'explicit': 'SRCS(zebra.md amber.md)\n',
    'reverse': 'SRCS(amber.md zebra.md)\n',
    'overlap': 'SRCS(zebra.md *.md zebra.md)\n',
    'split': 'SRCS(zebra.md)\nSRCS(amber.md zebra.md)\n',
    'recursive': 'SRCS(**/*.md)\n',
    'excluded': 'SRCS(zebra.md **/*.md EXCLUDE a/** amber.md)\n',
    'srcdir': 'SRCS(SRCDIR sections zebra.md *.md)\n',
    'empty': 'SRCS(absent*.md)\n',
    'default': '',
}


def fixture(mode):
    files = {'orchard/manual/ya.make': 'DOCS_LIBRARY()\n' + BODIES[mode] + 'END()\n'}
    for name in ('zebra.md', 'amber.md', 'a/entry.md', 'b/entry.md', 'a/deep/page.md',
                 'sections/zebra.md', 'sections/amber.md'):
        files['orchard/manual/' + name] = '# ' + name + '\n'
    return files, 'orchard/manual', []


def source_args(node):
    args = node['cmds'][0]['cmd_args']
    return args[args.index('--srcs') + 1:args.index('--include-srcs')]


EXPECTED = {
    'explicit': ['zebra.md', 'amber.md'],
    'reverse': ['amber.md', 'zebra.md'],
    'overlap': ['zebra.md', 'amber.md'],
    'split': ['zebra.md', 'amber.md', 'zebra.md'],
    'recursive': ['a/entry.md', 'b/entry.md', 'sections/amber.md', 'sections/zebra.md',
                  'a/deep/page.md', 'amber.md', 'zebra.md'],
    'excluded': ['zebra.md', 'b/entry.md', 'sections/amber.md', 'sections/zebra.md'],
    'srcdir': ['sections/zebra.md', 'sections/amber.md'],
    'empty': [],
    'default': [],
}


class DocsSourceOrderTest(unittest.TestCase):
    def test_pattern_discovery_order_and_per_call_identity_follow_native(self):
        for mode, expected in EXPECTED.items():
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, '--sandboxing', *flags)
                node = lib.node_by_output(graph, '$(B)/orchard/manual/orchard-manual.docslib')
                self.assertEqual(source_args(node), ['$(S)/orchard/manual/' + name for name in expected])
                for name in set(expected):
                    self.assertIn('$(S)/orchard/manual/' + name, node['inputs'])
                self.assertIn(node['uid'], graph['result'])



if __name__ == '__main__':
    unittest.main(verbosity=2)
