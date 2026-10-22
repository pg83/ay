import unittest

import lib
import test_nots_contexts
import test_proto_py2_context
from test_peer_suites import PEER_TESTS, checks


class ConfigureDependenciesTest(unittest.TestCase):
    def workspace(self):
        files = test_nots_contexts.NotsContextsTest().workspace()
        files.update(test_proto_py2_context.proto_world())
        files.update({
            'leaf/tests/ya.make': (
                'TS_TEST_JEST_FOR(leaf)\nINCLUDE(dependencies.inc)\n'
                'TS_TEST_SRCS(../src)\nEND()\nRECURSE(nested)\n'
            ),
            'leaf/tests/dependencies.inc': 'DEPENDS(p recipe)\n',
            'leaf/jest.config.js': 'module.exports = {};\n',
            'leaf/src/value.test.ts': 'it("works", () => {});\n',
            'p/ya.make': (
                f'PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(CPP_PROTO PY_PROTO PY3_PROTO DESC_PROTO)\n'
                'SRCS(a.proto)\nEND()\n'
            ),
            'p/a.proto': 'syntax = "proto2";\nmessage A {}\n',
            'recipe/ya.make': 'UNION()\nFROM_SANDBOX(FILE 42 OUT recipe.sh)\nEND()\n',
            'leaf/tests/nested/ya.make': 'EXECTEST()\nDEPENDS(unwanted)\nRUN(true)\nEND()\n',
            'unwanted/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}END()\n',
        })
        return files

    def test_preexisting_workspace_recurses_configure_depends_without_collecting_suites(self):
        graph = lib.make(self.workspace(), 'web', '-t', '-DAUTOCHECK=yes',
                         '--add-modules-to-results', *PEER_TESTS)
        self.assertEqual(set(checks(graph)), {
            'web/ts_lint_lint', 'left/eslint', 'right/eslint', 'leaf/eslint',
        })
        for output in ('p/libp.a', 'p/libpyp.global.a', 'p/libpy3p.global.a',
                       'p/p.self.protodesc', 'recipe/recipe.pkg.fake'):
            with self.subTest(output=output):
                node = lib.node_by_output(graph, '$(B)/' + output)
                self.assertIn(node['uid'], graph['result'])
        for version in ('py2', 'py3'):
            self.assertEqual(sum(f'$(B)/p/a__int{version}___pb2.py' in n['outputs']
                                 for n in graph['graph']), 1)
        self.assertFalse(any(output.startswith('$(B)/unwanted/')
                             for node in graph['graph'] for output in node['outputs']))

    def test_unselected_configured_dependencies_do_not_become_results(self):
        for args in ((), ('-DAUTOCHECK=yes',), (*PEER_TESTS, '-DAUTOCHECK=yes',
                                               '-DTS_DISABLE_HERMETIC_NODE_MODULES=yes')):
            with self.subTest(args=args):
                graph = lib.make(self.workspace(), 'web', '-t', '--strip-packages-from-results', *args)
                roots = {out for node in graph['graph'] if node['uid'] in graph['result']
                         for out in node['outputs']}
                self.assertFalse(any(out.startswith(('$(B)/p/', '$(B)/recipe/')) for out in roots))
                self.assertFalse(any(path.startswith('leaf/tests/') for path in checks(graph)))

    def test_native_configured_peer_recurses_have_effects_but_new_depends_recurses_do_not(self):
        files = {
            'root/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(peer)\nDEPENDS(peer)\nEND()\n',
            'peer/ya.make': (
                f'LIBRARY()\n{lib.BARE_MODULE}END()\n'
                'RECURSE(child)\nRECURSE_ROOT_RELATIVE(configuration)\n'
            ),
            'peer/child/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(built)\nEND()\n',
            'configuration/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(built)\nEND()\n',
            'built/ya.make': (
                f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(last)\nEND()\nRECURSE(unselected)\n'
            ),
            'last/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(built)\nEND()\n',
            'built/unselected/ya.make': 'EXECTEST()\nRUN(true)\nEND()\n',
        }
        graph = lib.make(files, 'root', '-t', '--strip-packages-from-results', *PEER_TESTS)
        for project in ('built', 'last'):
            node = lib.node_by_output(graph, f'$(B)/{project}/{project}')
            self.assertIn(node['uid'], graph['result'])
            self.assertEqual(sum(node['outputs'][0] in n['outputs'] for n in graph['graph']), 1)
        self.assertEqual(checks(graph), {})
        self.assertFalse(any(output.startswith('$(B)/built/unselected/')
                             for node in graph['graph'] for output in node['outputs']))

    def test_explicit_module_depends_are_retained_without_peer_suite_discovery(self):
        files = {
            'root/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(tool)\nEND()\n',
            'tool/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}END()\n',
        }
        graph = lib.make(files, 'root', '-t', '--strip-packages-from-results')
        self.assertIn(lib.node_by_output(graph, '$(B)/tool/tool')['uid'], graph['result'])
        self.assertEqual(checks(graph), {})

    def test_java_configuration_metadata_is_independent_of_suites(self):
        files = lib.java_tree(**{
            'root/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(peer)\nDEPENDS(peer)\nEND()\n',
            'peer/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\nRECURSE(child)\n',
            'peer/child/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nDEPENDS(native)\nEND()\n',
            'native/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}END()\n',
        })
        graph = lib.make(files, 'root', '-t', '--strip-packages-from-results', *PEER_TESTS)
        self.assertIn(lib.node_by_output(graph, '$(B)/native/native')['uid'], graph['result'])
        self.assertEqual(checks(graph), {})

    def test_nonproject_recurses_and_disabled_traversal_are_not_configured(self):
        files = {
            'root/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(peer)\nDEPENDS(peer)\nEND()\n',
            'peer/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(empty absent)\n',
            'peer/empty/file.txt': 'A source directory without a module.\n',
        }
        self.assertEqual(checks(lib.make(files, 'root', '-t', *PEER_TESTS)), {})
        files['peer/ya.make'] += 'RECURSE_FOR_TESTS(tests)\n'
        files['peer/tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(unwanted)\nEND()\n'
        files['unwanted/ya.make'] = f'PROGRAM()\n{lib.BARE_MODULE}END()\n'
        for flags in (('-t', '-DTRAVERSE_RECURSE=no', *PEER_TESTS),
                      ('-DTRAVERSE_DEPENDS=yes',)):
            with self.subTest(flags=flags):
                graph = lib.make(files, 'root', *flags)
                self.assertFalse(any(output.startswith('$(B)/unwanted/')
                                     for node in graph['graph'] for output in node['outputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
