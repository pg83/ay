import unittest

import lib
import test_java
import test_python2
import test_python_suites
from test_proto_targets import target_files


class DependsSelectionTest(unittest.TestCase):
    def test_each_enabled_proto_context_is_a_dependency(self):
        files = target_files()
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n'
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO GO_PROTO)\nSRCS(a.proto)\nEND()\n'
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for output in ('$(B)/p/p.jar', '$(B)/p/p.a'):
            with self.subTest(output=output):
                self.assertIn(lib.node_by_output(graph, output)['uid'], suite['deps'])
        self.assertFalse(any('$(B)/p/a.pb.cc' in node['outputs'] for node in graph['graph']))

    def test_java_dependency_keeps_managed_peer_selection(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(contrib/java/x/lib)\nDEPENDENCY_MANAGEMENT(contrib/java/x/lib/2)\nEND()\n',
            **test_java.proxy('contrib/java/x/lib', 'contrib/java/x/lib/1'),
            **test_java.contrib('contrib/java/x/lib/1', jar=1),
            **test_java.contrib('contrib/java/x/lib/2', jar=2),
        })
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        java = lib.node_by_output(graph, '$(B)/java/java.jar')
        peer = lib.node_by_output(graph, '$(B)/contrib/java/x/lib/2/lib-2.jar')
        self.assertIn(java['uid'], suite['deps'])
        self.assertIn(peer['uid'], java['deps'])
        self.assertNotIn(peer['uid'], suite['deps'])
        self.assertFalse(any('$(B)/contrib/java/x/lib/1/lib-1.jar' in node['outputs'] for node in graph['graph']))

    def test_union_dependency_closes_bundle_peers_without_recursing_libraries(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(outer)\nEND()\n',
            'outer/ya.make': 'UNION()\nPEERDIR(inner)\nEND()\nRECURSE(child)\n',
            'inner/ya.make': 'UNION()\nPEERDIR(native)\nEND()\n',
            'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}PEERDIR(unused)\nSRCS(native.cpp)\nEND()\n',
            'native/native.cpp': '',
            'unused/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(unused.cpp)\nEND()\n',
            'unused/unused.cpp': '',
            'outer/child/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}END()\n',
        }
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        native = lib.node_by_output(graph, '$(B)/native/libnative.a')
        self.assertIn(native['uid'], graph['result'])
        self.assertFalse(any('$(B)/unused/unused.cpp.o' in node['outputs'] for node in graph['graph']))
        self.assertFalse(any(output.startswith('$(B)/outer/child/') for node in graph['graph'] for output in node['outputs']))
        self.assertFalse(any('/inner/test-results/' in output for node in graph['graph'] for output in node['outputs']))

    def test_nonstarting_python2_context_and_tag_selectors(self):
        for selector, expected in (('', ('libnative.a', 'libpy3cnative.a')), ('ONLY_TAGS(PY3)', ('libpy3cnative.a',)), ('ONLY_TAGS(PY2)', ('libnative.a',))):
            with self.subTest(selector=selector):
                files = test_python2.python2_base()
                files.update({
                    'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(native)\nEND()\n',
                    'native/ya.make': f'PY23_NATIVE_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n{selector}\nSRCS(local.cpp)\nEND()\n',
                    'native/local.cpp': '',
                })
                graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
                suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                actual = {output for node in graph['graph'] if node['uid'] in suite['deps'] for output in node['outputs'] if output.startswith('$(B)/native/')}
                self.assertEqual(actual, {'$(B)/native/' + name for name in expected})

    def test_package_dependency_selects_final_and_union_contexts(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(pkg)\nEND()\n',
            'pkg/ya.make': 'PACKAGE()\nFILES(file.txt)\nEND()\n',
            'pkg/file.txt': '',
        }
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for name in ('pkg.pkg.fake', 'pkg.final.pkg.fake'):
            self.assertIn(lib.node_by_output(graph, '$(B)/pkg/' + name)['uid'], suite['deps'])

    def test_split_java_dependency_exposes_each_created_jar(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY()\nEND()\n',
        })
        lib.tool_program(files, 'contrib/tools/ijar', 'ijar')
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for name in ('java.jar', 'java-interface.jar'):
            self.assertIn(lib.node_by_output(graph, '$(B)/java/' + name)['uid'], suite['deps'])

    def test_java_unit_selectors_filter_direct_dependencies(self):
        for selector in ('ONLY_TAGS(FULL_JAR_COMPILATION)', 'EXCLUDE_TAGS(IJAR)'):
            with self.subTest(selector=selector):
                files = lib.java_tree(**{
                    'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
                    'java/ya.make': f'JAVA_LIBRARY()\n{selector}\nEND()\n',
                })
                graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
                suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                self.assertIn(lib.node_by_output(graph, '$(B)/java/java.jar')['uid'], suite['deps'])
                self.assertFalse(any('$(B)/java/java-interface.jar' in node['outputs'] for node in graph['graph']))

    def test_python_program_depends_selects_binary_and_library_contexts(self):
        files = test_python_suites.fixture(macro='PY3_PROGRAM')
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(lib)\nEND()\n'
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        binary = lib.node_by_output(graph, '$(B)/lib/lib')
        archive = lib.node_by_output(graph, '$(B)/lib/libpy3lib.global.a')
        self.assertIn(binary['uid'], suite['deps'])
        self.assertIn(archive['uid'], suite['deps'])

    def test_conditional_dependency_declaration_uses_its_selected_module(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(choice)\nEND()\n',
            'choice/ya.make': f'IF (USE_JAVA)\nJAVA_LIBRARY_NON_SPLIT()\nEND()\nELSE()\nLIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp)\nEND()\nENDIF()\n',
            'choice/local.cpp': '',
        })
        for flag, output in (('yes', 'choice.jar'), ('no', 'libchoice.a')):
            with self.subTest(flag=flag):
                graph = lib.make(files, 'tests', '-t', '-DUSE_JAVA=' + flag)
                suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                self.assertIn(lib.node_by_output(graph, '$(B)/choice/' + output)['uid'], suite['deps'])

    def test_selected_python2_proto_reports_the_unfinished_producer(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n',
            'p/ya.make': f'PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO)\nEND()\n',
        }
        result = lib.make_process(files, 'tests', '-t')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Python 2 variant of PROTO_LIBRARY', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_dependency_without_a_module_is_a_cli_error(self):
        result = lib.make_process({'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(missing)\nEND()\n', 'missing/ya.make': ''}, 'tests', '-t')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('has no module declaration', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_depending_on_a_test_builds_it_without_running_its_suite(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(other)\nEND()\n',
            'other/ya.make': 'EXECTEST()\nRUN(true)\nEND()\nRECURSE(child)\n',
            'other/child/ya.make': 'EXECTEST()\nRUN(true)\nEND()\n',
        }
        graph = lib.make(files, 'tests', '-t')
        self.assertFalse(any(output.startswith('$(B)/other/test-results/') or output.startswith('$(B)/other/child/')
                             for node in graph['graph'] for output in node['outputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
