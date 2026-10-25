import collections
import unittest

from tst import lib
from tst.make.test_go_tests import go_module, go_proto_world, go_suite_resources
from tst.make.test_proto_cpp import proto_files
from tst.make.test_python_proto import python_base


def fixture():
    files = go_proto_world()
    files.update(proto_files())
    files.update(python_base())
    files.pop('app/ya.make')
    files.pop('app/main.py')
    lib.tool_program(files, 'contrib/tools/protoc/bin', 'protoc')
    files.update({
        'contrib/libs/python/Include/.keep': '',
        'contrib/tools/python/src/Include/.keep': '',
        'wire/parcel/ya.make': ('PROTO_LIBRARY()\n' + lib.BARE_MODULE
                              + 'NO_PYTHON_INCLUDES()\nINCLUDE_TAGS(GO_PROTO)\nSRCS(parcel.proto)\nEND()\n'),
        'wire/parcel/parcel.proto': 'syntax = "proto3";\nmessage Parcel {}\n',
        'grove/generator/ya.make': ('PY3_PROGRAM(generator)\n' + lib.BARE_MODULE
                                  + 'NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\n'
                                  'PY_SRCS(MAIN main.py)\nPEERDIR(wire/parcel)\nEND()\n'),
        'grove/generator/main.py': 'print("generated")\n',
        'grove/native/ya.make': ('UNITTEST(consumer)\n' + lib.BARE_MODULE
                               + 'ALLOCATOR(FAKE)\nADDINCL(${ARCADIA_BUILD_ROOT}/grove/native)\n'
                               'RUN_PROGRAM(grove/generator OUT generated.h)\nSRCS(main.cpp)\nEND()\n'),
        'grove/native/main.cpp': '#include "generated.h"\nint consumer;\n',
        'library/cpp/testing/unittest_main/ya.make': ('LIBRARY()\n' + lib.BARE_MODULE
                                                    + 'SRCS(main.cpp)\nEND()\n'),
        'library/cpp/testing/unittest_main/main.cpp': 'int main(){return 0;}\n',
        'grove/data/ya.make': ('PY3_PROGRAM(data)\n' + lib.BARE_MODULE
                             + 'NO_PYTHON_INCLUDES()\nPY_SRCS(MAIN main.py)\nPEERDIR(wire/parcel)\nEND()\n'),
        'grove/data/main.py': 'print("data")\n',
    })
    go_module(files, 'grove/goapp', macros='PEERDIR(wire/parcel)\n')
    return files


class GovetForeignContextTest(unittest.TestCase):
    def make(self, roots):
        graph = lib.make(fixture(), *roots, '--sandboxing', '-DAUTOCHECK=yes',
                         '--target-platform', 'default-linux-x86_64', '--target-platform-tests',
                         '--target-platform-flag', 'ADD_PEERDIRS_GEN_TESTS=yes',
                         '--target-platform-build-type', 'relwithdebinfo', '--host-build-type', 'release',
                         '--host-platform-flag', 'IS_CROSS_TOOLS=yes', '--add-modules-to-results')
        index = {node['uid']: node for node in graph['graph']}
        self.assertTrue(set(graph['result']) <= index.keys())
        self.assertTrue(all(set(node['deps']) <= index.keys() for node in graph['graph']))
        suite = next(node for node in graph['graph'] if node['kv'].get('path') == 'wire/parcel/govet')
        self.assertEqual(len(suite['deps']), len(set(suite['deps'])))
        resources = go_suite_resources(self, graph, suite, fixture())
        outputs = collections.Counter(index[ref]["outputs"][0] for ref in suite["deps"])
        runtime_outputs = [index[ref]["outputs"][0] for ref in resources]
        return graph, outputs, runtime_outputs

    def test_required_host_tool_keeps_its_proto_modules_out_of_target_project_query(self):
        for roots in (('grove/goapp', 'grove/native'), ('grove/native', 'grove/goapp')):
            with self.subTest(roots=roots):
                graph, outputs, runtime_outputs = self.make(roots)
                self.assertEqual(outputs, collections.Counter([
                    '$(B)/common_test.context', '$(B)/wire/parcel/parcel.a', *runtime_outputs]))
                # These real host producers still belong to the selected generator.
                generator = lib.node_by_output(graph, '$(B)/grove/generator/generator')
                proto = lib.node_by_output(graph, '$(B)/wire/parcel/libpy3wire-parcel.global.a')
                self.assertIn(proto['uid'], generator['deps'])
                lib.node_by_output(graph, '$(B)/grove/native/generated.h')

    def test_explicit_target_python_data_keeps_global_project_modules(self):
        for roots in (('grove/goapp', 'grove/native', 'grove/data'),
                      ('grove/data', 'grove/native', 'grove/goapp')):
            with self.subTest(roots=roots):
                graph, outputs, runtime_outputs = self.make(roots)
                self.assertEqual(outputs, collections.Counter([
                    '$(B)/common_test.context', '$(B)/wire/parcel/parcel.a',
                    '$(B)/wire/parcel/libwire-parcel.a',
                    '$(B)/wire/parcel/libpy3wire-parcel.global.a', *runtime_outputs]))
                binary = lib.node_by_output(graph, '$(B)/grove/data/data')
                own = lib.node_by_output(graph, '$(B)/grove/data/libpy3data.global.a')
                suite = next(node for node in graph['graph'] if node['kv'].get('path') == 'grove/data/import_test')
                self.assertTrue({binary['uid'], own['uid']} <= set(suite['deps']))
                self.assertIn(own['uid'], binary['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
