import unittest

from tst import lib
from tst.make import test_codegen_antlr
from tst.make import test_go_tests
from tst.make import test_proto_frameworks
from tst.make import test_proto_py_variant
from tst.make import test_proto_targets
from tst.make import test_python_suites
from tst.make.test_java_tests import test_nodes


class GeneratorSelectionContractsTest(unittest.TestCase):
    def test_antlr_output_includes_deduplicate_equivalent_paths(self):
        files = test_codegen_antlr.antlr_files(
            'RUN_ANTLR(Calc.g IN Calc.g OUT x.h OUTPUT_INCLUDES mod/extra.h ${ARCADIA_ROOT}/mod/extra.h)\nSRCS(use.cpp)\n',
            {'mod/Calc.g': '', 'mod/use.cpp': '#include "x.h"\n'})
        graph = lib.make(files, 'mod')
        compiled = lib.node_by_output(graph, '$(B)/mod/use.cpp.o')
        self.assertEqual(compiled['inputs'].count('$(S)/mod/extra.h'), 1)
        self.assertIn(lib.node_by_output(graph, '$(B)/mod/x.h')['uid'], compiled['deps'])

    def test_rooted_go_proto_source_keeps_its_original_path(self):
        files = test_go_tests.go_proto_world()
        files.update({'api/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSRCS(${ARCADIA_ROOT}/api/message.proto)\nEND()\n',
                      'api/message.proto': 'syntax = "proto3";\n'})
        graph = lib.make(files, 'api')
        generated = lib.node_by_output(graph, '$(B)/api/message.pb.go')
        self.assertIn('$(S)/api/message.proto', generated['inputs'])
        self.assertIn(generated['uid'], lib.node_by_output(graph, '$(B)/api/api.a')['deps'])

    def test_yson_generator_resolves_imports_from_test_import_path(self):
        files = test_proto_frameworks.go_world()
        for peer in test_proto_frameworks.YSON_PEERS:
            test_proto_frameworks.go_module(files, peer)
        lib.tool_program(files, 'contrib/tools/protoc', 'protoc')
        lib.tool_program(files, test_proto_frameworks.YSON_TOOL, 'proto-yson-gen')
        files.update({'lib/ya.make': 'GO_LIBRARY()\nSET(GO_TEST_IMPORT_PATH fixtures)\nYT_ORM_PROTO_YSON(model/a.proto)\nEND()\n',
                      'model/a.proto': 'syntax = "proto3";\nimport "shared.proto";\n',
                      'fixtures/shared.proto': 'syntax = "proto3";\n',
                      'contrib/libs/protobuf/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\n'})
        generated = lib.node_by_output(lib.make(files, 'lib'), '$(B)/lib/a.yson.go')
        self.assertIn('$(S)/fixtures/shared.proto', generated['inputs'])

    def test_proto_docs_ignores_recursion_only_peer(self):
        files = test_proto_targets.target_files()
        files.update({'p/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(DOCS_PROTO)\nPEERDIR(group)\nSRCS(a.proto)\nEND()\n',
                      'group/ya.make': 'RECURSE(child)\n'})
        lib.tool_program(files, 'vendor/github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc', 'protoc-gen-doc')
        graph = lib.make(files, 'p')
        lib.node_by_output(graph, '$(B)/p/a.md')
        self.assertFalse(any('/group/' in p for n in graph['graph'] for p in n['outputs']))

    def test_python_proto_does_not_run_native_gazetteer_generator(self):
        files = test_proto_py_variant.py_proto_files()
        files.update({'p/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nSRCS(message.proto ignored.gztproto)\nEND()\n',
                      'p/message.proto': 'syntax = "proto3";\n', 'p/ignored.gztproto': '',
                      'q/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nEND()\n'})
        graph = lib.make(files, 'py')
        lib.node_by_output(graph, '$(B)/p/message__intpy3___pb2.py')
        lib.node_by_output(graph, '$(B)/p/ignored__intpy3___pb2.py')
        self.assertFalse(any('$(B)/dict/gazetteer/converter/converter' in c['cmd_args']
                             for n in graph['graph'] for c in n['cmds']))

    def test_resource_validation_is_verbose_in_distributed_mode(self):
        files = {'app/ya.make': 'EXECTEST()\nRUN(tool)\nSET(VALIDATE_DATA yes)\nDATA(sbr://12)\nEND()\n'}
        checks = test_nodes(lib.make(files, 'app', '-t', '--dist'))
        check = checks['app/validate_data_sbr']
        self.assertEqual(check['inputs'], ['$(S)/app', '12'])
        self.assertIn('check_resource --verbose', check['cmds'][-1]['cmd_args'])

    def test_sanitized_python_binary_does_not_run_import_checks(self):
        graph = lib.make(test_python_suites.fixture(macro='PY3_PROGRAM_BIN'), 'lib', '-t', '-DSANITIZER_TYPE=address')
        lib.node_by_output(graph, '$(B)/lib/lib')
        self.assertFalse(any('/import_test/' in p for n in graph['graph'] for p in n['outputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
