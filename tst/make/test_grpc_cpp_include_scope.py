import itertools
import unittest

from tst import lib
from tst.make.test_proto_cpp import GRPC_CPP, PROTOC, STYLEGUIDE, WRAPPER, grpc_files, pb_node, proto_files


BARE = 'NO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n'
APPHOST = 'apphost/tools/stub_generator/cpp_plugin'


def fixture(grpc=True, apphost=True, transitive=False):
    files = proto_files()
    grpc_files(files)
    lib.tool_program(files, APPHOST, 'cpp_plugin')
    files['apphost/tools/stub_generator/cpp_includes/ya.make'] = 'LIBRARY()\n' + BARE + 'END()\n'
    for tool, declarations in (
        ('contrib/tools/protoc', 'INDUCED_DEPS(h+cpp rt/pb_common.h)\n'),
        ('contrib/tools/protoc/plugins/cpp_styleguide', 'INDUCED_DEPS(cpp rt/style_cpp.h)\n'),
        ('contrib/tools/protoc/plugins/grpc_cpp', 'INDUCED_DEPS(h rt/grpc_h.h)\n'
         'INDUCED_DEPS(cpp rt/grpc_cpp.h)\nINDUCED_DEPS(h+cpp rt/grpc_common.h)\n'),
        (APPHOST, 'INDUCED_DEPS(h rt/facet_h.h)\n'),
    ):
        files[tool + '/ya.make'] = files[tool + '/ya.make'].replace('END()', declarations + 'END()')
    for name in ('pb_common', 'style_cpp', 'grpc_h', 'grpc_cpp', 'grpc_common', 'facet_h', 'nested'):
        files['rt/' + name + '.h'] = '#pragma once\n' + ('#include "nested.h"\n' if name == 'facet_h' else '')
    for directory, imported, peer in (('cedar/deep', '', ''),
                                      ('cedar/base', 'cedar/deep', 'cedar/deep'),
                                      ('cedar/model', 'cedar/base', 'cedar/base')):
        files[directory + '/entry.proto'] = 'syntax = "proto3";\n' + (
            'import "' + imported + '/entry.proto";\n' if imported else '') + 'message Entry {}\n'
        extras = ''
        if directory == 'cedar/model':
            extras = ('GRPC()\n' if grpc else '') + ('APPHOST()\n' if apphost else '')
            extras += '' if transitive else 'SET(PROTOC_TRANSITIVE_HEADERS no)\n'
        files[directory + '/ya.make'] = ('PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n' + BARE + extras
                                         + ('PEERDIR(' + peer + ')\n' if peer else '') + 'SRCS(entry.proto)\nEND()\n')
    readers = []
    for suffix in ('.pb.h',) + (('.grpc.pb.h',) if grpc else ()) + (('.apphost.h',) if apphost else ()):
        name = suffix.strip('.').replace('.', '_') + '.cpp'
        readers.append(name)
        files['cedar/read/' + name] = '#include "cedar/model/entry' + suffix + '"\n'
    files['cedar/read/ya.make'] = 'LIBRARY()\n' + BARE + 'PEERDIR(cedar/model)\nSRCS(' + ' '.join(readers) + ')\nEND()\n'
    return files


def markers(node):
    return {value for value in node['inputs'] if value.startswith('$(S)/rt/')}


class GrpcCppIncludeScopeTest(unittest.TestCase):
    def verify(self, graph, grpc=True, apphost=True):
        cpp = {'$(S)/rt/pb_common.h', '$(S)/rt/style_cpp.h'}
        header = {'$(S)/rt/pb_common.h'}
        if grpc:
            cpp.update(('$(S)/rt/grpc_cpp.h', '$(S)/rt/grpc_common.h'))
            header.update(('$(S)/rt/grpc_h.h', '$(S)/rt/grpc_common.h'))
        if apphost:
            header.update(('$(S)/rt/facet_h.h', '$(S)/rt/nested.h'))
        producer = pb_node(graph, '$(B)/cedar/model/entry.pb.h')
        for name in ('entry.pb.cc.o',) + (('entry.grpc.pb.cc.o',) if grpc else ()):
            compiled = lib.node_by_output(graph, '$(B)/cedar/model/' + name)
            self.assertEqual(markers(compiled), cpp)
            self.assertIn(producer['uid'], compiled['deps'])
            self.assertIn(producer['outputs'][0], compiled['inputs'])
            self.assertNotIn(WRAPPER, compiled['inputs'])
            self.assertNotIn('$(S)/cedar/model/entry.proto', compiled['inputs'])
        for name in ('pb_h.cpp',) + (('grpc_pb_h.cpp',) if grpc else ()) + (('apphost_h.cpp',) if apphost else ()):
            reader = lib.node_by_output(graph, '$(B)/cedar/read/' + name + '.o')
            if name == 'apphost_h.cpp':
                self.assertTrue({'$(S)/rt/pb_common.h', '$(S)/rt/facet_h.h', '$(S)/rt/nested.h'} <= markers(reader))
                self.assertNotIn('$(S)/rt/style_cpp.h', markers(reader))
                self.assertNotIn('$(S)/rt/grpc_cpp.h', markers(reader))
            else:
                self.assertEqual(markers(reader), header)
            self.assertIn(producer['uid'], reader['deps'])
            for directory in ('cedar/base', 'cedar/deep'):
                imported = pb_node(graph, '$(B)/' + directory + '/entry.pb.h')
                self.assertIn('$(B)/' + directory + '/entry.pb.h', reader['inputs'])
                self.assertIn(imported['uid'], reader['deps'])
        for path in ('cedar/model/entry.proto', 'cedar/base/entry.proto', 'cedar/deep/entry.proto'):
            self.assertIn('$(S)/' + path, producer['inputs'])
        for tool in (PROTOC, STYLEGUIDE) + ((GRPC_CPP,) if grpc else ()) + ((f'$(B)/{APPHOST}/cpp_plugin',) if apphost else ()):
            self.assertIn(tool, producer['inputs'])
            self.assertIn(lib.node_by_output(graph, tool)['uid'], producer['deps'])
        self.assertEqual(producer['cmds'][0]['cmd_args'][producer['cmds'][0]['cmd_args'].index('--outputs') + 1:
                         producer['cmds'][0]['cmd_args'].index('--')], producer['outputs'])
        return producer

    def test_cpp_and_header_obligations_stay_distinct_for_all_macro_combinations(self):
        for grpc, apphost, transitive in itertools.product((False, True), repeat=3):
            with self.subTest(grpc=grpc, apphost=apphost, transitive=transitive):
                self.verify(lib.make(fixture(grpc, apphost, transitive), 'cedar/read', 'cedar/model'), grpc, apphost)

    def test_common_tool_obligations_still_reach_generated_cpp(self):
        files = fixture()
        files[APPHOST + '/ya.make'] = files[APPHOST + '/ya.make'].replace(
            'INDUCED_DEPS(h rt/facet_h.h)', 'INDUCED_DEPS(h+cpp rt/facet_h.h)')
        graph = lib.make(files, 'cedar/read', 'cedar/model')
        for source in ('entry.pb.cc.o', 'entry.grpc.pb.cc.o'):
            node = lib.node_by_output(graph, '$(B)/cedar/model/' + source)
            self.assertIn('$(S)/rt/facet_h.h', node['inputs'])
            self.assertIn('$(S)/rt/nested.h', node['inputs'])
            self.assertIn('$(S)/rt/style_cpp.h', node['inputs'])
            self.assertNotIn('$(S)/rt/grpc_h.h', node['inputs'])

    def test_root_order_and_target_context_keep_same_basename_import_owners(self):
        for roots, platform in ((('cedar/model', 'cedar/read'), 'default-linux-x86_64'),
                                (('cedar/read', 'cedar/model'), 'default-linux-aarch64')):
            self.verify(lib.make(fixture(), *roots, '--target-platform', platform))

    def test_generic_plugin_with_grpc_suffixes_uses_cpp_obligations(self):
        files = fixture()
        files['cedar/model/ya.make'] = files['cedar/model/ya.make'].replace('GRPC()',
            'CPP_PROTO_PLUGIN2(renamed contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.cc .grpc.pb.h DEPS contrib/libs/grpc)')
        self.verify(lib.make(files, 'cedar/read', 'cedar/model'))

    def test_explicit_source_header_and_generated_header_keep_distinct_ownership(self):
        files = fixture()
        files['cedar/model/entry.pb.h'] = '#include "physical.h"\n'
        files['cedar/model/physical.h'] = '#pragma once\n'
        lib.tool_program(files, 'tools/source_reader', 'source_reader')
        files['cedar/read/ya.make'] = files['cedar/read/ya.make'].replace('END()',
            'RUN_PROGRAM(tools/source_reader IN ${ARCADIA_ROOT}/cedar/model/entry.pb.h OUT marker.cpp)\nEND()')
        graph = lib.make(files, 'cedar/read', 'cedar/model')
        self.verify(graph)
        action = lib.node_by_output(graph, '$(B)/cedar/read/marker.cpp')
        self.assertIn('$(S)/cedar/model/entry.pb.h', action['inputs'])
        self.assertIn('$(S)/cedar/model/physical.h', action['inputs'])
        self.assertNotIn('$(B)/cedar/model/entry.pb.h', action['inputs'])
        compiled = lib.node_by_output(graph, '$(B)/cedar/model/entry.grpc.pb.cc.o')
        self.assertNotIn('$(S)/cedar/model/physical.h', compiled['inputs'])


if __name__ == '__main__':
    unittest.main()
