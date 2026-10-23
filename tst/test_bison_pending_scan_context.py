import json
import unittest

import lib
from test_codegen_bison_flex import BISON_SKELETONS, bison_files


def fixture(c_mode=False):
    files = bison_files(('BISON_GEN_C()\n' if c_mode else '') + 'SRCS(grammar.y)\n', {'mod/grammar.y': '%%\n'})
    files = {name.replace('mod/', 'grove/grammar/'): text for name, text in files.items()}
    files['grove/consumer/ya.make'] = 'LIBRARY(consumer)\n' + lib.BARE_MODULE + 'PEERDIR(grove/grammar)\nRUN_PROGRAM(grove/emitter -o generated.cpp OUT generated.cpp)\nEND()\n'
    lib.tool_program(files, 'grove/emitter', 'emitter')
    files['grove/emitter/ya.make'] = files['grove/emitter/ya.make'].replace('SRCS(main.cpp)', 'PEERDIR(grove/grammar)\nSRCS(main.cpp)')
    files['contrib/tools/bison/data/skeletons/lalr1.cc'] = '#include "tail.hh"\n'
    files['contrib/tools/bison/data/skeletons/glr.cc'] = '#include "tail.hh"\n'
    files['contrib/tools/bison/data/skeletons/tail.hh'] = '#pragma once\n'
    return files


class BisonPendingScanContextTest(unittest.TestCase):
    def check(self, c_mode):
        result = lib.make_process(fixture(c_mode), 'grove/consumer', '--sandboxing')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('deduper already borrowed', result.stderr)
        graph = json.loads(result.stdout)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        extension = 'c' if c_mode else 'cpp'
        output = '$(B)/grove/grammar/grammar.y.' + extension
        bison = lib.node_by_output(graph, output)
        expected = ['$(B)/contrib/tools/bison/bison', '$(B)/contrib/tools/m4/m4', '$(S)/grove/grammar/grammar.y']
        if not c_mode:
            expected += ['$(S)/build/scripts/preprocess.py']
            expected += ['$(S)/contrib/tools/bison/data/' + name for name in BISON_SKELETONS]
            expected += ['$(S)/contrib/tools/bison/data/skeletons/tail.hh']
        self.assertEqual(bison['inputs'], expected)
        self.assertEqual(len(bison['cmds']), 1 if c_mode else 2)
        compiled = lib.node_by_output(graph, output + '.pic.o')
        self.assertIn(bison['uid'], compiled['deps'])
        self.assertIn('$(B)/grove/grammar/grammar.h', compiled['inputs'])
        lib.node_by_output(graph, '$(B)/grove/emitter/emitter')

    def test_cold_registered_bison_context_scans_skeletons_before_borrowing_scratch(self):
        self.check(False)

    def test_c_grammar_keeps_its_no_skeleton_contract(self):
        self.check(True)


if __name__ == '__main__':
    unittest.main(verbosity=2)
