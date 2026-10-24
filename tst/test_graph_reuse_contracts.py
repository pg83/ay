import json
import unittest

import lib
import test_headers
import test_go_package
import test_java
import test_java_features
import test_nots
import test_userver_proto
from test_source_selection_contracts import library


class RemainingGraphContractsTest(unittest.TestCase):
    def test_typescript_root_and_package_reuse_the_same_outputs(self):
        files = test_nots.NotsTests().project()
        files['group/ya.make'] = 'CI_GROUP()\nPEERDIR(web)\nEND()\n'
        graph = lib.make(files, 'group', 'web')
        bundle = lib.node_by_output(graph, '$(B)/web/output.tar')
        package = lib.node_by_output(graph, '$(B)/group/group.ci.pkg.fake')
        self.assertIn(bundle['uid'], package['deps'])
        for output in ('$(B)/web/package.json', '$(B)/web/output.tar'):
            self.assertEqual(sum(output in n['outputs'] for n in graph['graph']), 1)

    def test_typescript_packages_share_registry_configuration(self):
        files = test_nots.NotsTests().project()
        for directory in ('web', 'other'):
            files[directory + '/ya.make'] = 'TS_PACKAGE()\nEND()\n'
            files[directory + '/package.json'] = json.dumps({'dependencies': {'dep': '1.2.3'}})
            files[directory + '/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages:\n  dep@1.2.3:\n    resolution: {integrity: sha512-YWJj}\n"
        graph = lib.make(files, 'web', 'other')
        for directory in ('web', 'other'):
            prep = lib.node_by_output(graph, f'$(B)/{directory}/package.json')
            self.assertEqual(prep['resources'], [{'uri': 'https://npm.yandex-team.ru/dep/-/dep-1.2.3.tgz#integrity=sha512-YWJj'}])

    def test_many_unbound_build_constraints_do_not_discard_module(self):
        names = ' '.join(f'OPTIONAL_CONSTRAINT_{i}' for i in range(4096))
        files = {'app/ya.make': library(f'NO_BUILD_IF({names})\nSRCS(main.cpp)'), 'app/main.cpp': ''}
        lib.node_by_output(lib.make(files, 'app'), '$(B)/app/main.cpp.o')

    def test_unknown_macro_keeps_its_name_without_intern_shuffle(self):
        files = {'app/ya.make': library('UNRECOGNIZED_USER_MACRO(argument)\nSRCS(main.cpp)'), 'app/main.cpp': ''}
        result = lib.make_process(files, 'app', env={'AY_CHAOS': 'intern-shuffle=0'})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('UNRECOGNIZED_USER_MACRO', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_root_exclusion_does_not_exclude_all_headers(self):
        files = test_headers.header_files()
        files['producer/ya.make'] = library('PEERDIR(proto)\nSRCS(local.cpp)\nHEADERS(. EXCLUDE ${ARCADIA_ROOT})')
        graph = lib.make(files, 'consumer')
        link = lib.node_by_output(graph, '$(B)/consumer/consumer')
        proto = lib.node_by_output(graph, '$(B)/proto/a.pb.h')
        self.assertIn(proto['uid'], link['deps'])
        self.assertIn('$(B)/proto/a.pb.h', link['inputs'])

    def test_java_dependency_management_preserves_specific_rule(self):
        files = test_java_features.tree('PEERDIR(contrib/java/x)\nDEPENDENCY_MANAGEMENT(contrib/java/x/2 contrib/java/x)',
            module='JAVA_LIBRARY_NON_SPLIT', **{
                **test_java.contrib('contrib/java/x/1', jar=1),
                **test_java.contrib('contrib/java/x/2', jar=2),
                'contrib/java/x/ya.make': 'JAVA_CONTRIB_PROXY()\nPEERDIR(contrib/java/x/1)\nEND()\n'})
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertEqual(test_java.writer_args(jar, 'bfg.txt'), ['contrib/java/x/2/x-2.jar'])

    def test_java_glob_does_not_read_an_unresolved_source_root(self):
        files = test_java_features.tree('JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR "$(UNKNOWN_ROOT)" **/*.java)')
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertFalse(any('UNKNOWN_ROOT' in p for p in jar['inputs']))

    def test_external_build_source_has_no_invented_producer(self):
        files = {'app/ya.make': library('SRCS(${BINDIR}/external.cpp)')}
        graph = lib.make(files, 'app')
        compiled = lib.node_by_output(graph, '$(B)/app/external.cpp.o')
        self.assertIn('$(B)/app/external.cpp', compiled['inputs'])
        self.assertFalse(any('$(B)/app/external.cpp' in n['outputs'] for n in graph['graph']))

    def test_global_linker_source_preserves_previously_scanned_dependencies(self):
        files = {'app/ya.make': library('SRCS(main.cpp GLOBAL options.ld)'),
                 'app/main.cpp': '#include "options.ld"\n',
                 'app/options.ld': '#include "nested.h"\n', 'app/nested.h': ''}
        graph = lib.make(files, 'app')
        compiled = lib.node_by_output(graph, '$(B)/app/main.cpp.o')
        self.assertIn('$(S)/app/nested.h', compiled['inputs'])

    def test_go_library_peer_keeps_its_go_dependencies(self):
        files = test_go_package.go_world()
        test_go_package.go_module(files, 'go_parent', macros='PEERDIR(go_leaf)\n')
        test_go_package.go_module(files, 'go_leaf')
        test_go_package.go_module(files, 'app', macros='PEERDIR(go_parent)\n')
        graph = lib.make(files, 'app')
        app = lib.node_by_output(graph, '$(B)/app/app.a')
        parent = lib.node_by_output(graph, '$(B)/go_parent/go_parent.a')
        leaf = lib.node_by_output(graph, '$(B)/go_leaf/go_leaf.a')
        for consumer, peer in ((app, parent), (parent, leaf)):
            self.assertIn(peer['outputs'][0], consumer['inputs'])
            self.assertIn(peer['uid'], consumer['deps'])
        self.assertIn(app['uid'], graph['result'])

    def test_userver_uses_a_generated_proto(self):
        files = test_userver_proto.userver_files()
        lib.tool_program(files, 'tools/gen', 'gen')
        files['model/ya.make'] = 'PROTO_LIBRARY()\nPROTO_NAMESPACE(model)\nRUN_PROGRAM(tools/gen OUT_NOAUTO a.proto)\nSRCS(a.proto)\nEND()\n'
        del files['model/a.proto']
        files['framework/ya.make'] = 'LIBRARY()\nPROTO_NAMESPACE(model)\nPEERDIR(model)\nUSRV_GEN_GRPC_SERVICE_V2(FROM model model/a.proto)\nEND()\n'
        graph = lib.make(files, 'framework')
        producer = lib.node_by_output(graph, '$(B)/model/a.proto')
        generated = lib.node_by_output(graph, '$(B)/model/a_service.usrv.pb.cpp')
        self.assertIn('$(B)/model/a.proto', generated['inputs'])
        self.assertIn(producer['uid'], generated['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
