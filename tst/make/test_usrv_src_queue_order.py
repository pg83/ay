import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world


def files(case):
    sources = proto_world()
    lib.tool_program(sources, 'tools/carve', 'carve')
    block = ('USRV_ALL_SRCS(RECURSIVE src EXCLUDE **/*skip*)\n' if case.startswith('usrv')
             else 'SRCS(src/a.cpp src/b.cpp)\n')
    custom = 'SRC(a.cpp -DLOCAL_A=1)\nSRC(b.cpp -DLOCAL_B=1)\n'
    body = custom + block if case.endswith('before') else block + custom
    if case == 'usrv_generated':
        body += 'RUN_PROGRAM(tools/carve OUT generated.cpp)\n'
    if case == 'usrv_waiting':
        body += 'SRC(delayed.cpp -DDELAYED=1)\nRUN_PROGRAM(tools/carve OUT_NOAUTO delayed.cpp)\n'
    sources.update({
        'grove/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCDIR(grove/src)\n' + body + 'END()\n',
        'grove/src/a.cpp': 'int a;\n',
        'grove/src/b.cpp': 'int b;\n',
        'grove/src/skip.cpp': 'int skip;\n',
    })
    return sources


class UsrvSrcQueueOrderTest(unittest.TestCase):
    def check(self, case):
        graph = lib.make(files(case), 'grove', '--sandboxing', '--target-platform', 'default-linux-x86_64')
        root = '$(B)/grove/'
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
        members = ['a.cpp.o', 'b.cpp.o', '_/src/a.cpp.o', '_/src/b.cpp.o']
        generated = 'generated.cpp' if case == 'usrv_generated' else 'delayed.cpp' if case == 'usrv_waiting' else None
        if generated:
            members.append(generated + '.o')
        archive = lib.node_by_output(graph, root + 'libgrove.a')
        self.assertEqual(archive['cmds'][0]['cmd_args'], [
            '', '$(S)/build/scripts/link_lib.py', '', 'LLVM_AR', 'gnu', '$(B)', 'None', '--', '--',
            root + 'libgrove.a', *[root + name for name in members],
        ])
        for name in members:
            cc = lib.node_by_output(graph, root + name)
            self.assertIn(cc['uid'], archive['deps'])
            self.assertIn(root + name, archive['inputs'])
        self.assertEqual({name for name in archive['inputs'] if name.endswith('.o')},
                         {root + name for name in members})
        for source, define in (('a.cpp', '-DLOCAL_A=1'), ('b.cpp', '-DLOCAL_B=1')):
            custom = lib.node_by_output(graph, root + source + '.o')
            listed = lib.node_by_output(graph, root + '_/src/' + source + '.o')
            self.assertEqual(custom['inputs'], ['$(S)/grove/src/' + source])
            self.assertEqual(listed['inputs'], custom['inputs'])
            self.assertIn(define, custom['cmds'][0]['cmd_args'])
            self.assertNotIn(define, listed['cmds'][0]['cmd_args'])
        if generated:
            producer = lib.node_by_output(graph, root + generated)
            cc = lib.node_by_output(graph, root + generated + '.o')
            tool = lib.node_by_output(graph, '$(B)/tools/carve/carve')
            self.assertIn(tool['uid'], producer['deps'])
            self.assertIn(tool['outputs'][0], producer['inputs'])
            self.assertIn(producer['uid'], cc['deps'])
            self.assertIn(root + generated, cc['inputs'])
            if case == 'usrv_waiting':
                self.assertIn('-DDELAYED=1', cc['cmds'][0]['cmd_args'])

    def test_glob_sources_use_the_same_queue_as_srcs(self):
        for kind in ('usrv', 'plain'):
            for position in ('before', 'after'):
                with self.subTest(kind=kind, position=position):
                    self.check(kind + '_' + position)

    def test_generated_actions_keep_their_readiness_round(self):
        for case in ('usrv_generated', 'usrv_waiting'):
            with self.subTest(case=case):
                self.check(case)


if __name__ == '__main__':
    unittest.main()
