import unittest

import lib
import test_proto_py_variant
import test_python2
from test_python_proto import args


def proto_world():
    files = test_proto_py_variant.py_proto_files()
    files.update(test_python2.python2_base())
    lib.tool_program(files, 'contrib/tools/protoc_old', 'protoc_old')
    lib.tool_program(files, 'contrib/tools/protoc/plugins/grpc_python', 'grpc_python')
    lib.tool_program(files, 'contrib/tools/protoc/plugins/grpc_cpp', 'grpc_cpp')
    files.update({
        'py/ya.make': f'PY2_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(p)\nEND()\n',
        'p/ya.make': (
            f'PROTO_LIBRARY()\n{lib.BARE_MODULE}GRPC()\nPY_NAMESPACE(api.messages)\n'
            'PROTO_NAMESPACE(p)\nPEERDIR(q)\nSRCS(a.proto)\n'
            'IF (PY_PROTO AND PYTHON2 AND MODULE_LANG == "PY2")\nSRCS(native.cpp)\nENDIF()\nEND()\n'
        ),
        'p/a.proto': 'syntax = "proto2";\nimport "q/b.proto";\nmessage A {}\n',
        'p/native.cpp': '#include <Python.h>\nint value;\n',
        'q/ya.make': f'PROTO_LIBRARY()\n{lib.BARE_MODULE}SRCS(b.proto)\nEND()\n',
        'q/b.proto': 'syntax = "proto2";\nmessage B {}\n',
        'contrib/python/grpcio/ya.make': test_proto_py_variant.EMPTY_LIBRARY,
        'contrib/python/grpcio/x.cpp': '',
        'contrib/libs/grpc/ya.make': test_proto_py_variant.EMPTY_LIBRARY,
        'contrib/libs/grpc/x.cpp': '',
        'contrib/libs/python/Include/Python.h': '',
        'contrib/python/protobuf/ya.make': (
            f'PY23_LIBRARY(protobuf)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n'
            'RESOURCE(DONT_COMPRESS - runtime=protobuf)\nEND()\n'
        ),
    })
    return files


class ProtoPython2ContextTest(unittest.TestCase):
    def test_python2_peer_selects_old_protoc_and_python2_resources(self):
        graph = lib.make(proto_world(), 'py')
        producer = lib.node_by_output(graph, '$(B)/p/a__intpy2___pb2.py')
        command = args(producer)
        self.assertEqual(producer['outputs'], [
            '$(B)/p/a__intpy2___pb2.py', '$(B)/p/a__intpy2___pb2_grpc.py',
            '$(B)/p/a__intpy2___pb2.pyi',
        ])
        self.assertEqual(command[command.index('--py_ver') + 1], 'py2')
        self.assertIn('$(B)/contrib/tools/protoc_old/protoc_old', command)
        self.assertNotIn('$(B)/contrib/tools/protoc/protoc', command)
        self.assertIn('$(S)/q/b.proto', producer['inputs'])
        self.assertEqual(producer['kv']['ext_out_name_for_a__intpy2___pb2.py'], 'a_pb2.py')
        bytecode = next(n for n in graph['graph']
                        if any(out.startswith('$(B)/p/a__intpy2___pb2.py.') and out.endswith('.yapyc')
                               for out in n['outputs']))
        self.assertEqual(args(bytecode)[0], '$(B)/tools/py2cc/py2cc')
        self.assertIn(producer['uid'], bytecode['deps'])
        resources = next(n for n in graph['graph'] if
                         'resfs/src//py_modules/api.messages.a_pb2=p/a__intpy2___pb2.py' in args(n))
        self.assertIn('-/py_modules/api.messages.a_pb2', args(resources))
        self.assertIn('-/py_code/api.messages.a_pb2', args(resources))
        resource_object = lib.node_by_output(graph, resources['outputs'][0] + '.py2.o')
        self.assertIn(resources['uid'], resource_object['deps'])
        native = lib.node_by_output(graph, '$(B)/p/native.cpp.py2.o')
        self.assertIn('-DARCADIA_PYTHON_UNICODE_SIZE=4', args(native))
        self.assertNotIn('-DUSE_PYTHON3', args(native))
        self.assertIn('$(S)/contrib/libs/python/Include/Python.h', native['inputs'])
        link = lib.node_by_output(graph, '$(B)/py/py')
        self.assertIn('$(B)/p/libp.a', link['inputs'])
        self.assertIn('$(B)/contrib/python/protobuf/libpyprotobuf.global.a', link['inputs'])
        self.assertIn(resource_object['uid'], lib.node_by_output(graph, '$(B)/p/libpyp.global.a')['deps'])
        outputs = {out for node in graph['graph'] for out in node['outputs']}
        self.assertIn('$(B)/q/b__intpy2___pb2.py', outputs)
        self.assertFalse(any('__intpy3__' in out for out in outputs))

    def test_python2_and_python3_producers_coexist_and_share_native_sibling(self):
        files = proto_world()
        files['py3/ya.make'] = f'PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(p)\nEND()\n'
        for targets in (('py', 'py3'), ('py3', 'py')):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for version, compiler in (('py2', 'protoc_old/protoc_old'), ('py3', 'protoc/protoc')):
                    producer = lib.node_by_output(graph, f'$(B)/p/a__int{version}___pb2.py')
                    self.assertEqual(args(producer)[args(producer).index('--py_ver') + 1], version)
                    self.assertIn(f'$(B)/contrib/tools/{compiler}', args(producer))
                self.assertEqual(sum('$(B)/p/a.pb.h' in n['outputs'] for n in graph['graph']), 1)

    def test_python2_gazetteer_uses_registered_generated_proto(self):
        files = proto_world()
        files['p/ya.make'] = f'PROTO_LIBRARY()\n{lib.BARE_MODULE}SRCS(g.gztproto)\nEND()\n'
        files['p/g.gztproto'] = 'message G {}\n'
        graph = lib.make(files, 'py')
        converter = lib.node_by_output(graph, '$(B)/p/g.proto')
        producer = lib.node_by_output(graph, '$(B)/p/g__intpy2___pb2.py')
        self.assertIn(converter['uid'], producer['deps'])
        self.assertIn('$(B)/p/g.proto', producer['inputs'])
        self.assertNotIn('$(S)/p/g.proto', producer['inputs'])
        self.assertEqual(producer['cmds'][0]['cwd'], '$(B)')
        self.assertEqual(sum('$(B)/p/g.proto' in n['outputs'] for n in graph['graph']), 1)

    def test_python2_schema_and_shared_source_keep_context_metadata(self):
        files = proto_world()
        files['py/ya.make'] = files['py/ya.make'].replace('PEERDIR(p)', 'PEERDIR(p q)')
        files['shared/x.proto'] = 'syntax = "proto2";\nmessage X {}\n'
        for directory, macro in (('p', 'PROTO_SCHEMA'), ('q', 'PROTO_LIBRARY')):
            cpp_tag = 'CPP_PROTO_FROM_SCHEMA' if macro == 'PROTO_SCHEMA' else 'CPP_PROTO'
            files[f'{directory}/ya.make'] = (
                f'{macro}()\n{lib.BARE_MODULE}EXCLUDE_TAGS({cpp_tag})\n'
                f'PY_NAMESPACE({directory}.messages)\nSRCDIR(shared)\nGRPC()\nNO_MYPY()\nSRCS(x.proto)\nEND()\n'
            )
        graph = lib.make(files, 'py')
        producer = lib.node_by_output(graph, '$(B)/shared/x__intpy2___pb2.py')
        self.assertEqual(producer['outputs'], [
            '$(B)/shared/x__intpy2___pb2.py', '$(B)/shared/x__intpy2___pb2_grpc.py',
        ])
        self.assertEqual(sum('$(B)/shared/x__intpy2___pb2.py' in n['outputs'] for n in graph['graph']), 1)
        for directory in ('p', 'q'):
            self.assertTrue(any(
                f'resfs/src//py_modules/{directory}.messages.x_pb2=shared/x__intpy2___pb2.py' in args(n)
                for n in graph['graph']
            ))
        self.assertFalse(any(out.endswith('.pb.h') for n in graph['graph'] for out in n['outputs']))

    def test_no_optimization_omits_native_self_context_in_both_versions(self):
        for version in ('py2', 'py3'):
            with self.subTest(version=version):
                files = proto_world()
                files['p/ya.make'] = (
                    f'PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\n'
                    'NO_MYPY()\nSRCS(a.proto)\nEND()\n'
                )
                files['p/a.proto'] = 'syntax = "proto2";\nmessage A {}\n'
                files['py/ya.make'] = files['py/ya.make'].replace('PY2_PROGRAM', version.upper() + '_PROGRAM')
                graph = lib.make(files, 'py')
                lib.node_by_output(graph, f'$(B)/p/a__int{version}___pb2.py')
                self.assertFalse(any('$(B)/p/a.pb.h' in n['outputs'] for n in graph['graph']))

    def test_python2_proto_does_not_compile_extracted_gazetteer_source(self):
        files = proto_world()
        files['p/ya.make'] = (
            f'PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO)\nSRCS(a.proto)\n'
            'FROM_ARCHIVE(input.tar OUT generated.gztproto)\nEND()\n'
        )
        files['p/a.proto'] = 'syntax = "proto2";\nmessage A {}\n'
        files['p/input.tar'] = ''
        graph = lib.make(files, 'py')
        lib.node_by_output(graph, '$(B)/p/a__intpy2___pb2.py')
        self.assertFalse(any('$(B)/dict/gazetteer/converter/converter' in args(n) for n in graph['graph']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
