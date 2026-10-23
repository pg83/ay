import unittest

import lib
from test_proto_py2_context import proto_world


BARE = 'NO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n'
MODULE = 'larch/wire'


def library(files, directory, marker, peers='', python=False):
    files[directory + '/ya.make'] = ('PY3_LIBRARY()\nNO_PYTHON_INCLUDES()\n' if python else 'LIBRARY()\n') + BARE \
        + 'ADDINCL(GLOBAL markers/' + marker + ')\nCFLAGS(GLOBAL -DPEER_' + marker.upper() + ')\n' \
        + ('PEERDIR(' + peers + ')\n' if peers else '') + 'END()\n'
    files['markers/' + marker + '/notice.h'] = ''


def fixture(body='', tags='PY3_PROTO', python_library=False):
    files = proto_world()
    library(files, 'contrib/libs/python', 'python')
    library(files, 'contrib/python/protobuf', 'py_runtime', python=True)
    library(files, 'contrib/python/grpcio', 'py_grpc', 'contrib/libs/grpc', True)
    library(files, 'contrib/libs/grpc', 'cpp_grpc', 'contrib/libs/larch-sentinel')
    library(files, 'contrib/libs/larch-sentinel', 'shared')
    library(files, 'contrib/libs/protobuf', 'cpp_runtime')
    library(files, 'contrib/libs/googleapis-common-protos', 'google')
    library(files, 'runtime/user', 'user', python=True)
    library(files, 'runtime/cobalt', 'cobalt')
    library(files, 'runtime/petal', 'petal', python=True)
    library(files, 'runtime/native-user', 'native_user')
    files['runtime/native-user/ya.make'] = files['runtime/native-user/ya.make'].replace('LIBRARY()', 'PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)')
    for tool in ('generators/cobalt', 'generators/petal', 'contrib/tools/protoc/plugins/grpc_python', 'contrib/tools/protoc/plugins/grpc_cpp'):
        lib.tool_program(files, tool, tool.rsplit('/', 1)[-1])
    files['contrib/libs/python/Include/Python.h'] = '#pragma once\n'
    files[MODULE + '/entry.proto'] = 'syntax = "proto3";\nmessage Entry {}\n'
    peer = 'runtime/native-user' if tags == 'CPP_PROTO' else 'runtime/user'
    files[MODULE + '/ya.make'] = ('PY3_LIBRARY()\n' if python_library else 'PROTO_LIBRARY()\nONLY_TAGS(' + tags + ')\n') \
        + BARE + body + 'PEERDIR(' + peer + ')\n' + ('PY_SRCS' if python_library else 'SRCS') + ('(anchor.py entry.proto)\nEND()\n' if python_library else '(entry.proto)\nEND()\n')
    files[MODULE + '/anchor.py'] = ''
    files['larch/consumer/ya.make'] = 'PY3_LIBRARY()\n' + BARE + 'NO_PYTHON_INCLUDES()\nPEERDIR(larch/wire)\nSRCS(anchor.cpp)\nEND()\n'
    files['larch/consumer/anchor.cpp'] = 'int anchor;\n'
    return files


def resource_compile(graph):
    return next(n for n in graph['graph'] if n['kv'].get('p') == 'CC'
                and any(o.startswith('$(B)/larch/wire/') and '_raw.auxcpp' in o for o in n['outputs']))


def order(node, prefix):
    return [a.removeprefix(prefix) for a in node['cmds'][0]['cmd_args'] if a.startswith(prefix)]


class PythonGrpcPeerPhaseTest(unittest.TestCase):
    def verify(self, files, includes, defines, target=MODULE, *args):
        graph = lib.make(files, target, '--target-platform', 'default-linux-x86_64', *args)
        cc = resource_compile(graph)
        self.assertEqual(order(cc, '-I$(S)/markers/'), includes)
        self.assertEqual(order(cc, '-DPEER_'), [name.upper() for name in defines])
        pb = lib.node_by_output(graph, '$(B)/larch/wire/entry__intpy3___pb2.py')
        self.assertIn('$(S)/larch/wire/entry.proto', pb['inputs'])
        self.assertIn('$(B)/contrib/tools/protoc/protoc', pb['inputs'])
        if '_pb2_grpc.py' in ' '.join(pb['outputs']):
            tool = lib.node_by_output(graph, '$(B)/contrib/tools/protoc/plugins/grpc_python/grpc_python')
            self.assertIn(tool['uid'], pb['deps'])
            self.assertIn(tool['outputs'][0], pb['inputs'])
        self.assertTrue(graph['result'])
        return graph

    def test_grpc_runtime_is_consumed_at_python_source_phase(self):
        self.verify(fixture(), ['python', 'user', 'py_runtime'], ['python', 'user', 'py_runtime'])
        self.verify(fixture('GRPC()\n'), ['python', 'user', 'py_runtime', 'py_grpc', 'cpp_grpc', 'shared'],
                    ['python', 'user', 'py_runtime', 'shared', 'cpp_grpc', 'py_grpc'])

    def test_conditional_google_peer_precedes_python_defaults_and_late_grpc(self):
        for noopt in ('', 'NO_OPTIMIZE_PY_PROTOS()\n'):
            self.verify(fixture(noopt + 'GRPC()\nUSE_COMMON_GOOGLE_APIS(api/annotations)\n'),
                        ['google', 'python', 'user', 'py_runtime', 'py_grpc', 'cpp_grpc', 'shared'],
                        ['google', 'python', 'user', 'py_runtime', 'shared', 'cpp_grpc', 'py_grpc'])

    def test_macros_after_sources_keep_configuration_and_callback_phase(self):
        for body, google in (('GRPC()\n', False), ('GRPC()\nUSE_COMMON_GOOGLE_APIS(api/annotations)\n', True)):
            files = fixture(body)
            files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(body, '').replace(
                'SRCS(entry.proto)', 'SRCS(entry.proto)\n' + body)
            prefix = ['google'] if google else []
            self.verify(files, prefix + ['python', 'user', 'py_runtime', 'py_grpc', 'cpp_grpc', 'shared'],
                        prefix + ['python', 'user', 'py_runtime', 'shared', 'cpp_grpc', 'py_grpc'])

    def test_cpp_plugin_declaration_does_not_add_python_peer(self):
        cpp = 'CPP_PROTO_PLUGIN(cobalt generators/cobalt .cobalt.h DEPS runtime/cobalt)\n'
        self.verify(fixture(cpp), ['python', 'user', 'py_runtime'], ['python', 'user', 'py_runtime'])
        self.verify(fixture('GRPC()\n' + cpp + 'PY_PROTO_PLUGIN(petal _petal.py generators/petal DEPS runtime/petal)\n'),
                    ['python', 'user', 'py_runtime', 'py_grpc', 'cpp_grpc', 'shared', 'petal'],
                    ['python', 'user', 'py_runtime', 'shared', 'cpp_grpc', 'py_grpc', 'petal'])

    def test_python_library_does_not_consume_cpp_plugin_peer(self):
        files = fixture('GRPC()\nCPP_PROTO_PLUGIN(cobalt generators/cobalt .cobalt.h DEPS runtime/cobalt)\n',
                        python_library=True)
        graph = lib.make(files, MODULE)
        cc = resource_compile(graph)
        self.assertEqual(set(order(cc, '-I$(S)/markers/')),
                         {'python', 'user', 'py_runtime', 'py_grpc', 'cpp_grpc', 'shared'})
        self.assertNotIn('COBALT', order(cc, '-DPEER_'))
        pb = lib.node_by_output(graph, '$(B)/larch/wire/entry__intpy3___pb2.py')
        self.assertIn('$(B)/contrib/tools/protoc/plugins/grpc_python/grpc_python', pb['inputs'])
        self.assertTrue(graph['result'])

    def test_cpp_plugin_peer_order_stays_in_cpp_variant(self):
        files = fixture('GRPC()\nUSE_COMMON_GOOGLE_APIS(api/annotations)\n'
                        'CPP_PROTO_PLUGIN(cobalt generators/cobalt .cobalt.h DEPS runtime/cobalt)\n', tags='CPP_PROTO')
        graph = lib.make(files, MODULE, '--target-platform', 'default-linux-x86_64')
        for suffix in ('.pb.cc.o', '.grpc.pb.cc.o'):
            cc = lib.node_by_output(graph, '$(B)/larch/wire/entry' + suffix)
            self.assertEqual(order(cc, '-I$(S)/markers/'), ['google', 'cpp_grpc', 'shared', 'cobalt', 'native_user', 'cpp_runtime'])
            self.assertEqual(order(cc, '-DPEER_'), ['GOOGLE', 'SHARED', 'CPP_GRPC', 'COBALT', 'NATIVE_USER', 'CPP_RUNTIME'])


if __name__ == '__main__':
    unittest.main()
