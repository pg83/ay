import unittest

import lib
from test_proto_py2_context import proto_world


def files(kind='LIBRARY', noauto=False, explicit=False):
    sources = proto_world()
    lib.tool_program(sources, 'tools/carve', 'carve')
    statement = ('RUN_PROGRAM(tools/carve IN seed.in '
                 + ('OUT_NOAUTO' if noauto else 'OUT')
                 + ' seed.proto OUTPUT_INCLUDES imports/base.proto)\n')
    if kind == 'PROTO_LIBRARY':
        statement = 'ONLY_TAGS(CPP_PROTO)\nIF(GEN_PROTO)\n' + statement + 'ENDIF()\n'
    if explicit:
        statement += 'SRCS(seed.proto)\n'
    sources.update({
        'cedar/ya.make': kind + '()\n' + lib.BARE_MODULE + statement + 'SRCS(main.cpp)\nEND()\n',
        'cedar/seed.in': 'message Seed {}\n',
        'cedar/main.cpp': 'int value;\n',
        'imports/base.proto': 'syntax = "proto2";\n',
        'imports/base.pb.h': '',
    })
    for peer in ('library/cpp/testing/unittest', 'library/cpp/testing/common',
                 'library/cpp/testing/unittest_main'):
        sources.setdefault(peer + '/ya.make', 'LIBRARY()\n' + lib.BARE_MODULE + 'END()\n')
    return sources


class GeneratedProtoAutoTest(unittest.TestCase):
    def check_generated(self, kind, noauto=False, explicit=False):
        graph = lib.make(files(kind, noauto, explicit), 'cedar', '--sandboxing',
                         '--target-platform', 'default-linux-x86_64')
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
        if noauto and not explicit:
            self.assertFalse(any('$(B)/cedar/seed.proto' in node['outputs']
                                 or '$(B)/cedar/seed.pb.h' in node['outputs']
                                 for node in graph['graph']))
            return
        producer = lib.node_by_output(graph, '$(B)/cedar/seed.proto')
        pb = lib.node_by_output(graph, '$(B)/cedar/seed.pb.h')
        cc = lib.node_by_output(graph, '$(B)/cedar/seed.pb.cc.o')
        tool = lib.node_by_output(graph, '$(B)/tools/carve/carve')
        self.assertIn('$(S)/cedar/seed.in', producer['inputs'])
        self.assertIn(tool['uid'], producer['deps'])
        self.assertIn('$(B)/cedar/seed.proto', pb['inputs'])
        self.assertIn('$(S)/imports/base.proto', pb['inputs'])
        self.assertIn(producer['uid'], pb['deps'])
        self.assertEqual(pb['outputs'][:2], ['$(B)/cedar/seed.pb.h', '$(B)/cedar/seed.pb.cc'])
        self.assertIn('$(S)/build/scripts/cpp_proto_wrapper.py', pb['inputs'])
        self.assertIn('$(B)/contrib/tools/protoc/protoc', pb['inputs'])
        args = pb['cmds'][0]['cmd_args']
        self.assertIn('$(S)/build/scripts/cpp_proto_wrapper.py', args)
        self.assertEqual(args[-1], 'cedar/seed.proto')
        self.assertEqual(pb['cmds'][0]['cwd'], '$(B)')
        self.assertIn(pb['uid'], cc['deps'])
        self.assertIn('$(B)/cedar/seed.pb.cc', cc['inputs'])
        self.assertIn('$(S)/imports/base.pb.h', cc['inputs'])

    def test_ordinary_generated_proto_dispatch(self):
        for kind in ('LIBRARY', 'UNITTEST'):
            with self.subTest(kind=kind):
                self.check_generated(kind)

    def test_selected_cpp_proto_dispatch(self):
        self.check_generated('PROTO_LIBRARY')

    def test_noauto_stays_lazy_and_explicit_srcs_selects_it(self):
        for kind in ('LIBRARY', 'PROTO_LIBRARY'):
            for explicit in (False, True):
                with self.subTest(kind=kind, explicit=explicit):
                    self.check_generated(kind, noauto=True, explicit=explicit)


if __name__ == '__main__':
    unittest.main()
