import unittest

from tst import lib
from tst.make import test_project_specific_macros
from tst.make import test_userver_proto


class GeneratorEdgeContractsTest(unittest.TestCase):
    def test_nlg_empty_cpp_and_incomplete_template_fragments(self):
        files = {}
        lib.tool_program(files, 'alice/nlg/tools/codegen', 'nlg')
        test_project_specific_macros.library(files, 'alice/nlg/library/nlg_project_runtime')
        files['a/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}SET(COMPILE_NLG_EMPTY_CPP yes)\nCOMPILE_NLG(a.nlg b.nlg)\nEND()\n'
        files['a/a.nlg'] = '{{ text }}\n{% import "a/b.nlg" %}\n# unfinished comment'
        files['a/b.nlg'] = '{% unfinished command'
        graph = lib.make(files, 'a')
        run = lib.node_by_output(graph, '$(B)/a/dummy.h')
        self.assertIn('--empty-cpp', run['cmds'][0]['cmd_args'])
        self.assertIn('$(S)/a/a.nlg', run['inputs'])
        self.assertIn('$(S)/a/b.nlg', run['inputs'])
        compiled = lib.node_by_output(graph, '$(B)/a/a.nlg.cpp.o')
        self.assertIn('$(B)/a/b.nlg.h', compiled['inputs'])

    def test_empty_userver_generator_does_not_require_tools(self):
        graph = lib.make({'a/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}USRV_GEN_GRPC_SERVICE_V2()\nSRCS(main.cpp)\nEND()\n', 'a/main.cpp': ''}, 'a')
        lib.node_by_output(graph, '$(B)/a/liba.a')
        self.assertFalse(any(n['kv']['p'] == 'UG' for n in graph['graph']))

    def test_userver_origin_selection_ignores_unrelated_peers(self):
        files = test_userver_proto.userver_files()
        files['other/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}END()\n'
        files['framework/ya.make'] = (
            'LIBRARY()\nPEERDIR(other model)\nPROTO_NAMESPACE(model)\n'
            'USRV_GEN_PROTO_STRUCTS(PROTODESC desc ORIGINS model)\nEND()\n')
        graph = lib.make(files, 'framework')
        generated = lib.node_by_output(graph, '$(B)/model/a.structs.usrv.pb.hpp')
        self.assertIn('$(S)/model/a.proto', generated['inputs'])
        self.assertFalse(any('other' in out and 'usrv.pb' in out for n in graph['graph'] for out in n['outputs']))

    def usrv_files(self):
        files = {
            'a/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/gen OUT_NOAUTO generated.tar)\n'
                'USRV_BUILD(FROM ${BINDIR}/generated.tar DEPS_FILE deps.json OUT_NOAUTO generated.h schema.proto)\n'
                'SRCS(main.cpp)\nEND()\n',
            'a/main.cpp': '#include "a/generated.h"\n',
            'a/deps.json': '{"generated.h": ["dep.h"], "schema.proto": []}',
            'a/dep.h': '',
            'taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make': lib.resource_lib('USRV_UNPACK_TAR', 1),
        }
        lib.tool_program(files, 'tools/gen', 'gen')
        return files

    def test_archive_outputs_are_only_compiled_when_requested(self):
        graph = lib.make(self.usrv_files(), 'a')
        header = lib.node_by_output(graph, '$(B)/a/generated.h')
        compiled = lib.node_by_output(graph, '$(B)/a/main.cpp.o')
        self.assertIn(header['uid'], compiled['deps'])
        self.assertIn('$(S)/a/dep.h', compiled['inputs'])
        self.assertFalse(any(out.endswith('.pb.cc') for n in graph['graph'] for out in n['outputs']))

    def test_archive_outputs_require_declared_dependencies(self):
        files = self.usrv_files()
        files['a/deps.json'] = '{}'
        result = lib.make_process(files, 'a')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('dependencies for generated.h missing in deps.json', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
