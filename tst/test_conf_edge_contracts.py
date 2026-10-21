import unittest

import lib
from test_ymake_conf import Tree, library, cc_args


class ConfEdgeContractsTest(unittest.TestCase):
    def test_invalid_imports_and_blocks_report_configuration_errors(self):
        for conf, expected in (
            ('}\n', 'unmatched }'),
            ('when ($YES) {\n', 'unfinished block or assignment'),
            ('FLAGS=value ' + chr(92), 'unfinished block or assignment'),
            ('@import "relative.conf"\n', 'unsupported @import'),
            ('when (yes no) {\nSYSINCL=\n}\n', 'unsupported condition'),
        ):
            with self.subTest(conf=conf):
                result = Tree(self, {'app/ya.make': library('SRCS(main.cpp)\n'), 'app/main.cpp': '',
                                    'build/ymake.core.conf': conf}).make('app')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)
                self.assertNotIn('panic:', result.stderr)

    def test_empty_scalar_substitution_does_not_load_a_sysincl_file(self):
        graph = Tree(self, {'app/ya.make': library('SRCS(main.cpp)\n'), 'app/main.cpp': '',
                            'build/ymake.core.conf': 'SYSINCL=$UNDEFINED\n'}).graph('app', '--sandboxing')
        compiled = lib.node_by_output(graph, '$(B)/app/main.cpp.o')
        self.assertEqual(compiled['inputs'], ['$(S)/app/main.cpp'])

    def test_warning_condition_and_unknown_node_builder_keep_the_module(self):
        files = {'app/ya.make': library('NO_BUILD_IF(WARNING SANDBOXING)\nNODE_BUILDER(foo)\nSRCS(main.cpp)\n'),
                 'app/main.cpp': ''}
        result = lib.make_process(files, 'app', '-k')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('$(B)/app/main.cpp.o', result.stdout)
        self.assertIn('NODE_BUILDER', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
