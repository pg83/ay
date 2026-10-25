import unittest

from tst import lib
from tst.make.test_java import contrib, writer_args


WIRE = 'willow/contracts'
TOOL = 'apphost/tools/stub_generator/java_plugin'
RUNTIME = 'apphost/api/service/java/apphost/api'
PLUGIN_ARGS = ['--plugin=protoc-gen-java_plugin=$(B)/' + TOOL + '/java_plugin',
               '--java_plugin_out=$(B)/java_out']


def fixture(mode='apphost'):
    calls = {
        'apphost': 'APPHOST()',
        'java-only': 'APPHOST_JAVA()',
        'direct': 'JAVA_PROTO_PLUGIN(java_plugin ' + TOOL + ' DEPS ' + RUNTIME + ')',
        'plain': '',
        'cpp-only': 'APPHOST_CPP()',
        'arguments': 'APPHOST(ITEM_DISPATCHER Handler ITEM_DISPATCHER_HEADER handler.h)',
        'before': 'JAVA_PROTO_PLUGIN(first orchard/first DEPS orchard/runtime)\nAPPHOST()',
        'after': 'APPHOST()\nJAVA_PROTO_PLUGIN(first orchard/first DEPS orchard/runtime)',
        'contexts': 'INCLUDE(${ARCADIA_ROOT}/shared/plugin.inc)',
        'contexts-reversed': 'INCLUDE(${ARCADIA_ROOT}/shared/plugin.inc)',
        'cpp': 'APPHOST()',
    }[mode]
    files = lib.java_tree(**{
        'reader/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(' + WIRE + ')\nEND()\n',
        WIRE + '/ya.make': 'PROTO_LIBRARY()\n' + calls + '\nSRCS(service.proto)\nPEERDIR(willow/common)\nEND()\n',
        WIRE + '/service.proto': 'syntax = "proto3";\nimport "willow/common/base.proto";\nmessage Service {}\n',
        'willow/common/ya.make': 'PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n',
        'willow/common/base.proto': 'syntax = "proto3";\nmessage Base {}\n',
        RUNTIME + '/ya.make': 'JAVA_LIBRARY_NON_SPLIT(apphost-api)\nJAVA_SRCS(Api.java)\nEND()\n',
        RUNTIME + '/Api.java': '',
        'orchard/runtime/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(First.java)\nEND()\n',
        'orchard/runtime/First.java': '',
        **contrib('contrib/java/com/google/protobuf/protobuf-java/4.33.0', jar=91),
    })
    for path, name in ((TOOL, 'java_plugin'), ('orchard/first', 'first')):
        lib.tool_program(files, path, name)
        files[path + '/ya.make'] = files[path + '/ya.make'].replace('SRCS(main.cpp)', 'IF (ARCH_X86_64)\nSRCS(host.cpp)\nELSE()\nSRCS(target.cpp)\nENDIF()')
        files[path + '/host.cpp'] = ''
        files[path + '/target.cpp'] = ''
    if mode.startswith('contexts'):
        files['shared/plugin.inc'] = 'IF (ACTIVATE_APPHOST)\nAPPHOST_JAVA()\nENDIF()\n'
        files[WIRE + '/ya.make'] = files[WIRE + '/ya.make'].replace('PROTO_LIBRARY()', 'PROTO_LIBRARY()\nSET(ACTIVATE_APPHOST yes)')
        files['willow/plain/ya.make'] = files[WIRE + '/ya.make'].replace('ACTIVATE_APPHOST yes', 'ACTIVATE_APPHOST no')
        files['willow/plain/service.proto'] = files[WIRE + '/service.proto']
        peers = [WIRE, 'willow/plain']
        if mode.endswith('reversed'):
            peers.reverse()
        files['reader/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(' + ' '.join(peers) + ')\nEND()\n'
    if mode == 'cpp':
        from tst.make.test_proto_cpp import proto_files
        files.update(proto_files())
        lib.tool_program(files, 'apphost/tools/stub_generator/cpp_plugin', 'cpp_plugin')
        files['apphost/tools/stub_generator/cpp_includes/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'END()\n'
        files['reader/ya.make'] = 'PROGRAM()\n' + lib.BARE_MODULE + 'PEERDIR(' + WIRE + ')\nSRCS(main.cpp)\nEND()\n'
        files['reader/main.cpp'] = ''
    return files


def proto(graph, directory=WIRE):
    return lib.node_by_output(graph, '$(B)/' + directory + '/service.jsrc')


class JavaApphostPluginTest(unittest.TestCase):
    def graph(self, mode, platform='default-linux-x86_64'):
        graph = lib.make(fixture(mode), 'reader', '--target-platform', platform)
        self.assertTrue(graph['result'])
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(all(dep in ids for node in graph['graph'] for dep in node['deps']))
        return graph

    def check_plugin(self, graph, platform='default-linux-x86_64'):
        pb = proto(graph)
        args = pb['cmds'][0]['cmd_args']
        self.assertEqual(args[-2:], PLUGIN_ARGS)
        tool = lib.node_by_output(graph, '$(B)/' + TOOL + '/java_plugin')
        self.assertEqual(tool['platform'], 'default-linux-x86_64')
        self.assertIn(tool['outputs'][0], pb['inputs'])
        self.assertIn(tool['uid'], pb['deps'])
        self.assertCountEqual(pb['inputs'], ['$(S)/build/scripts/tared_protoc.py', tool['outputs'][0],
                                           '$(S)/' + WIRE + '/service.proto', '$(S)/willow/common/base.proto'])
        self.assertEqual(pb['platform'], platform)
        host = lib.node_by_output(graph, '$(B)/' + TOOL + '/host.cpp.pic.o')
        self.assertIn(host['uid'], tool['deps'])
        self.assertFalse(any('/' + TOOL + '/target.cpp' in out for node in graph['graph'] for out in node['outputs']))
        jar = lib.node_by_output(graph, '$(B)/' + WIRE + '/willow-contracts.jar')
        runtime = lib.node_by_output(graph, '$(B)/' + RUNTIME + '/apphost-api.jar')
        self.assertIn(pb['uid'], jar['deps'])
        self.assertIn(pb['outputs'][0], jar['inputs'])
        self.assertIn(runtime['uid'], jar['deps'])
        self.assertIn(runtime['outputs'][0], jar['inputs'])
        reader = lib.node_by_output(graph, '$(B)/reader/reader.jar')
        self.assertIn(RUNTIME + '/apphost-api.jar', writer_args(reader, 'bfg.txt'))
        return pb

    def test_apphost_and_java_only_wrappers_match_direct_plugin(self):
        direct = self.graph('direct')
        for mode in ('apphost', 'java-only', 'arguments'):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                self.check_plugin(graph)
                self.assertEqual(graph, direct)

    def test_plain_java_and_cpp_only_wrapper_do_not_load_java_tool(self):
        for mode in ('plain', 'cpp-only'):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                self.assertFalse(any(arg.startswith('--plugin=') for arg in proto(graph)['cmds'][0]['cmd_args']))
                self.assertFalse(any(out.startswith('$(B)/' + TOOL + '/') for node in graph['graph'] for out in node['outputs']))

    def test_native_callback_order_is_preserved_among_explicit_plugins(self):
        for mode in ('before', 'after'):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                args = proto(graph)['cmds'][0]['cmd_args']
                plugins = [arg.split('=', 2)[1] for arg in args if arg.startswith('--plugin=')]
                self.assertEqual(plugins, ['protoc-gen-first', 'protoc-gen-java_plugin'] if mode == 'before' else ['protoc-gen-java_plugin', 'protoc-gen-first'])
                self.assertIn('$(B)/orchard/first/first', proto(graph)['inputs'])

    def test_tool_stays_in_host_context_for_a_distinct_target_platform(self):
        self.check_plugin(self.graph('java-only', 'default-linux-aarch64'), 'default-linux-aarch64')

    def test_shared_lexical_include_uses_each_java_environment_in_both_orders(self):
        for mode in ('contexts', 'contexts-reversed'):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                self.check_plugin(graph)
                plain = proto(graph, 'willow/plain')
                self.assertFalse(any(arg.startswith('--plugin=') for arg in plain['cmds'][0]['cmd_args']))
                self.assertNotIn('$(B)/' + TOOL + '/java_plugin', plain['inputs'])

    def test_existing_cpp_apphost_outputs_do_not_load_java_plugin(self):
        graph = self.graph('cpp')
        self.assertTrue(lib.node_by_output(graph, '$(B)/' + WIRE + '/service.apphost.h')['outputs'])
        self.assertFalse(any(out.endswith('.jsrc') or out.startswith('$(B)/' + TOOL + '/') for node in graph['graph'] for out in node['outputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
