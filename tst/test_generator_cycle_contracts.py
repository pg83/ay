import unittest

import lib
from test_source_selection_contracts import library


class GeneratorCycleContractsTest(unittest.TestCase):
    def test_header_cycle_keeps_one_main_output_for_opaque_siblings(self):
        files = {'m/ya.make': library('ADDINCL(${BINDIR})\nRUN_PROGRAM(tool OUT_NOAUTO first.bin second.pyc third.pyc)\nSRCS(main.cpp)'),
                 'm/main.cpp': '#include "x.h"\n',
                 'm/x.h': '#include "y.h"\n#include "second.pyc"\n',
                 'm/y.h': '#include "x.h"\n#include "third.pyc"\n'}
        lib.tool_program(files, 'tool', 'tool')
        graph = lib.make(files, 'm')
        compiled = lib.node_by_output(graph, '$(B)/m/main.cpp.o')
        for name in ('first.bin', 'second.pyc', 'third.pyc'):
            self.assertEqual(compiled['inputs'].count('$(B)/m/' + name), 1)
        producer = lib.node_by_output(graph, '$(B)/m/first.bin')
        self.assertEqual(compiled['deps'].count(producer['uid']), 1)

    def test_generated_header_in_a_cycle_keeps_its_main_output(self):
        files = {'m/ya.make': library('ADDINCL(${BINDIR})\nRUN_PROGRAM(tool OUT_NOAUTO first.bin generated.h OUTPUT_INCLUDES m/x.h)\nSRCS(main.cpp)'),
                 'm/main.cpp': '#include "x.h"\n', 'm/x.h': '#include "generated.h"\n'}
        lib.tool_program(files, 'tool', 'tool')
        graph = lib.make(files, 'm')
        compiled = lib.node_by_output(graph, '$(B)/m/main.cpp.o')
        for name in ('first.bin', 'generated.h'):
            self.assertEqual(compiled['inputs'].count('$(B)/m/' + name), 1)
        self.assertIn('$(S)/m/x.h', compiled['inputs'])
        self.assertIn(lib.node_by_output(graph, '$(B)/m/generated.h')['uid'], compiled['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
