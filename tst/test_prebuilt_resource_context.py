import json
import unittest

import lib
from resource_dependencies import fetched_uri


BARE = 'NO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\nDISABLE(NEED_PLATFORM_PEERDIRS COMMON_LINK_SETTINGS)\n'


def formula(host, target):
    return json.dumps({'by_platform': {'linux': {'uri': host}, 'linux-aarch64': {'uri': target}}})


def files(equal=False):
    return {
        'forge/ya.make': 'SET_RESOURCE_URI_FROM_JSON(URI formula.json)\nPREBUILT_PROGRAM(payload)\nDECLARE_EXTERNAL_RESOURCE(GEN ${URI})\nPRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/payload)\nEND()\n',
        'forge/formula.json': formula('sbr:11', 'sbr:11' if equal else 'sbr:12'),
        'sink/ya.make': 'LIBRARY(sink)\n' + BARE + 'RUN_PROGRAM(forge OUT generated.cpp)\nEND()\n',
        'build/scripts/fs_tools.py': '',
    }


def fetch(graph, name, uri):
    found = [n for n in graph['graph'] if n['outputs'] == [f'$(B)/resources/{name}'] and fetched_uri({'graph': [n]}, name) == uri]
    if len(found) != 1:
        raise AssertionError((name, uri, found))
    return found[0]


def contextual(graph, output, platform):
    found = [n for n in graph['graph'] if output in n['outputs'] and n['platform'] == platform]
    if len(found) != 1:
        raise AssertionError((output, platform, found))
    return found[0]


class PrebuiltResourceContextTest(unittest.TestCase):
    def check_context(self, graph, platform, uri, python):
        copy = contextual(graph, '$(B)/forge/payload', platform)
        self.assertIn(fetch(graph, 'GEN', uri)['uid'], copy['deps'])
        self.assertEqual(copy['inputs'], ['$(S)/build/scripts/fs_tools.py'])
        return copy

    def test_required_host_owns_its_resource(self):
        graph = lib.make(files(), 'sink')
        self.check_context(graph, 'default-linux-x86_64', 'sbr:11', 'sbr:31')
        run = lib.node_by_output(graph, '$(B)/sink/generated.cpp')
        self.assertIn(contextual(graph, '$(B)/forge/payload', 'default-linux-x86_64')['uid'], run['deps'])
        self.assertFalse(any(n['outputs'] == ['$(B)/resources/GEN'] and n['cmds'][0]['cmd_args'][4] == 'sbr:12' for n in graph['graph']))

    def test_genuine_target_root_owns_target_resource(self):
        self.check_context(lib.make(files(), 'forge'), 'default-linux-aarch64', 'sbr:12', 'sbr:32')

    def test_both_contexts_and_root_orders_keep_own_fetch_dependencies(self):
        for roots in [('sink', 'forge'), ('forge', 'sink')]:
            with self.subTest(roots=roots):
                graph = lib.make(files(), roots[-1], *roots[:-1])
                self.check_context(graph, 'default-linux-x86_64', 'sbr:11', 'sbr:31')
                self.check_context(graph, 'default-linux-aarch64', 'sbr:12', 'sbr:32')

    def test_equal_resource_descriptors_share_fetch_in_both_orders(self):
        for roots in [('sink', 'forge'), ('forge', 'sink')]:
            with self.subTest(roots=roots):
                graph = lib.make(files(True), roots[-1], *roots[:-1])
                self.check_context(graph, 'default-linux-x86_64', 'sbr:11', 'sbr:31')
                self.check_context(graph, 'default-linux-aarch64', 'sbr:11', 'sbr:32')
                self.assertEqual(len([n for n in graph['graph'] if n['outputs'] == ['$(B)/resources/GEN']]), 1)

    def test_native_declaration_phases_select_last_plain_then_bundle(self):
        for declaration, expected in [('DECLARE_EXTERNAL_RESOURCE(GEN sbr:44)', 'sbr:99'), ('DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(GEN formula.json)', 'sbr:11')]:
            with self.subTest(declaration=declaration):
                fixture = files()
                fixture['forge/ya.make'] = 'PREBUILT_PROGRAM(payload)\n' + declaration + '\nDECLARE_EXTERNAL_RESOURCE(GEN sbr:99)\nPRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/payload)\nEND()\n'
                self.check_context(lib.make(fixture, 'forge'), 'default-linux-aarch64', expected, 'sbr:32')

    def test_primary_resource_keeps_its_own_fetch_before_unrelated_declaration(self):
        fixture = files()
        fixture['forge/ya.make'] = fixture['forge/ya.make'].replace('PRIMARY_OUTPUT(', 'DECLARE_EXTERNAL_RESOURCE(OTHER sbr:77)\nPRIMARY_OUTPUT(')
        copy = self.check_context(lib.make(fixture, 'forge'), 'default-linux-aarch64', 'sbr:12', None)
        self.assertEqual(len(copy['deps']), 1)

    def test_generic_compiler_consumers_keep_context_resource_refs(self):
        fixture = files()
        fixture.update({
            'forge/ya.make': 'PROGRAM(payload)\n' + BARE + 'ALLOCATOR(FAKE)\nPEERDIR(build/platform/clang)\nSRCS(main.cpp)\nEND()\n',
            'forge/main.cpp': 'int main() {}\n',
            'sink/ya.make': 'LIBRARY(sink)\n' + BARE + 'PEERDIR(build/platform/clang)\nRUN_PROGRAM(forge OUT generated.cpp)\nEND()\n',
            'build/platform/clang/ya.make': 'SET_RESOURCE_URI_FROM_JSON(URI formula.json)\nRESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 ${URI})\nEND()\n',
            'build/platform/clang/formula.json': formula('sbr:21', 'sbr:22'),
        })
        for unit in ('forge', 'sink'):
            fixture[unit + '/ya.make'] = fixture[unit + '/ya.make'].replace('PEERDIR(build/platform/clang)\n', 'PEERDIR(build/platform/clang)\nSET(C_COMPILER ${CLANG20_RESOURCE_GLOBAL}/bin/clang)\nSET(CXX_COMPILER ${CLANG20_RESOURCE_GLOBAL}/bin/clang++)\n')
        graph = lib.make(fixture, 'sink')
        for output, platform, compiler, python in [
            ('$(B)/sink/generated.cpp.o', 'default-linux-aarch64', 'sbr:22', 'sbr:32'),
            ('$(B)/forge/main.cpp.pic.o', 'default-linux-x86_64', 'sbr:21', 'sbr:31'),
        ]:
            node = contextual(graph, output, platform)
            self.assertIn(fetch(graph, 'CLANG20', compiler)['uid'], node['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
