import unittest

import lib
from test_python_suites import fixture, values
import test_yql_results


def dependencies(graph, suite):
    refs = set(suite['deps'])
    return {node['outputs'][0] for node in graph['graph'] if node['uid'] in refs}


class RuntimeDependsTest(unittest.TestCase):
    def test_native_primary_outputs_exclude_globals_and_fake_modules(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(native global_only)\nEND()\n',
            'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'native/local.cpp': '', 'native/global.cpp': '',
            'global_only/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(GLOBAL global.cpp)\nEND()\n',
            'global_only/global.cpp': '',
        }
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        actual = {out for out in dependencies(graph, suite) if out.startswith(('$(B)/native/', '$(B)/global_only/'))}
        self.assertEqual(actual, {'$(B)/native/libnative.a'})
        for output in ('$(B)/native/libnative.a', '$(B)/native/libnative.global.a', '$(B)/global_only/libglobal_only.global.a'):
            self.assertIn(lib.node_by_output(graph, output)['uid'], graph['result'])
        self.assertEqual(suite['env']['PATH'], '$(B)/:$(B)/native')

    def test_union_closes_include_peers_but_package_uses_only_final_context(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(outer pkg)\nEND()\n',
            'outer/ya.make': 'UNION()\nPEERDIR(inner)\nEND()\n',
            'inner/ya.make': 'UNION()\nPEERDIR(native)\nEND()\n',
            'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'native/local.cpp': '', 'native/global.cpp': '',
            'pkg/ya.make': 'PACKAGE()\nFILES(file.txt)\nPEERDIR(native)\nEND()\n',
            'pkg/file.txt': '',
        }
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        actual = {out for out in dependencies(graph, suite) if out.startswith(('$(B)/outer/', '$(B)/inner/', '$(B)/native/', '$(B)/pkg/'))}
        self.assertEqual(actual, {'$(B)/outer/outer.pkg.fake', '$(B)/inner/inner.pkg.fake',
                                 '$(B)/native/libnative.a', '$(B)/pkg/pkg.final.pkg.fake'})
        lib.node_by_output(graph, '$(B)/pkg/pkg.pkg.fake')
        self.assertIn(lib.node_by_output(graph, '$(B)/native/libnative.global.a')['uid'], graph['result'])

    def test_multimodule_binary_rewrites_path_and_keeps_configuration_results(self):
        files = fixture(macro='PY3_PROGRAM')
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(lib)\nEND()\n'
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        self.assertIn(lib.node_by_output(graph, '$(B)/lib/lib')['uid'], suite['deps'])
        archive = lib.node_by_output(graph, '$(B)/lib/libpy3lib.global.a')
        self.assertNotIn(archive['uid'], suite['deps'])
        self.assertIn(archive['uid'], graph['result'])
        self.assertEqual(suite['env']['PATH'], '$(B)/:$(B)/lib')

    def test_udf_runtime_has_only_shared_final_target_and_dynamic_dep_root(self):
        files = test_yql_results.YqlResultsTest().fixture()
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        actual = {out for out in dependencies(graph, suite) if out.startswith('$(B)/udf/')}
        self.assertEqual(actual, {'$(B)/udf/libnamed.so'})
        self.assertIn('$(B)/udf', values(suite, '--dep-root'))

    def test_python_runtime_unrolls_bundle_for_shared_object_directories(self):
        files = test_yql_results.YqlResultsTest().fixture()
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(pkg)\nEND()\n'
        files['pkg/ya.make'] = 'PACKAGE()\nPEERDIR(shared)\nEND()\n'
        files['shared/ya.make'] = f'DLL()\n{lib.BARE_MODULE}SRCS(local.cpp)\nEND()\n'
        files['shared/local.cpp'] = ''
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        self.assertIn('$(B)/shared', values(suite, '--dep-root'))
        self.assertNotIn(lib.node_by_output(graph, '$(B)/shared/libshared.so')['uid'], suite['deps'])
        self.assertEqual(suite['env']['PATH'], '$(B)/:$(B)/pkg')

    def test_java_bundles_follow_managed_peers_and_native_include_edges(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(shared)\nEND()\n',
            'shared/ya.make': f'DLL()\n{lib.BARE_MODULE}SRCS(local.cpp)\nEND()\n',
            'shared/local.cpp': '',
        })
        for consume in ('', 'ADD_DLLS_TO_JAR()'):
            with self.subTest(consume=consume):
                files['java/ya.make'] = f'JAVA_LIBRARY_NON_SPLIT()\n{consume}\nPEERDIR(shared)\nEND()\n'
                graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
                suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                for output in ('$(B)/java/java.jar', '$(B)/shared/libshared.so'):
                    self.assertIn(lib.node_by_output(graph, output)['uid'], suite['deps'])
                self.assertIn('$(B)/shared', values(suite, '--dep-root'))



if __name__ == '__main__':
    unittest.main(verbosity=2)
