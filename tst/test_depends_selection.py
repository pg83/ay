import unittest

import lib
import test_proto_py2_context
import test_java
import test_python2
import test_python_suites
from test_proto_targets import target_files


class DependsSelectionTest(unittest.TestCase):
    def test_filtered_suite_still_configures_its_dependency_results(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nSIZE(LARGE)\nTAG(expensive)\nDEPENDS(dep)\nEND()\n',
            'dep/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(peer)\nSRCS(dep.cpp)\nEND()\nRECURSE(child)\n',
            'dep/dep.cpp': '',
            'dep/child/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}END()\n',
            'peer/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'peer/local.cpp': '',
            'peer/global.cpp': '',
        }
        for flags in (('--test-size', 'small'), ('--test-tag', '-expensive')):
            for add_modules in (False, True):
                for strip_packages in (False, True):
                    with self.subTest(flags=flags, add_modules=add_modules, strip_packages=strip_packages):
                        options = ['--add-modules-to-results'] if add_modules else []
                        if strip_packages:
                            options.append('--strip-packages-from-results')
                        graph = lib.make(files, 'tests', '-t', *flags, *options)
                        dependency = lib.node_by_output(graph, '$(B)/dep/dep')
                        self.assertIn(dependency['uid'], graph['result'])
                        for name in ('libpeer.a', 'libpeer.global.a'):
                            peer = lib.node_by_output(graph, '$(B)/peer/' + name)
                            self.assertEqual(peer['uid'] in graph['result'], add_modules)
                        self.assertFalse(any('/tests/test-results/' in output or output.startswith('$(B)/dep/child/')
                                             for node in graph['graph'] for output in node['outputs']))

    def test_filtered_suite_reports_invalid_dependency_during_configuration(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nSIZE(LARGE)\nTAG(expensive)\nDEPENDS(missing)\nEND()\n',
            'missing/ya.make': '',
        }
        for flags in (('--test-size', 'small'), ('--test-tag', '-expensive')):
            with self.subTest(flags=flags):
                result = lib.make_process(files, 'tests', '-t', *flags)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('has no module declaration', result.stderr)
                self.assertNotIn('panic:', result.stderr)

    def test_each_enabled_proto_context_is_a_dependency(self):
        files = target_files()
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n'
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO GO_PROTO)\nSRCS(a.proto)\nEND()\n'
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for output in ('$(B)/p/p.jar', '$(B)/p/p.a'):
            with self.subTest(output=output):
                self.assertIn(lib.node_by_output(graph, output)['uid'], graph['result'])
                self.assertNotIn(lib.node_by_output(graph, output)['uid'], suite['deps'])
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
        self.assertIn(peer['uid'], suite['deps'])
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
                actual = {output for node in graph['graph'] if node['uid'] in graph['result'] for output in node['outputs'] if output.startswith('$(B)/native/')}
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
            self.assertEqual(lib.node_by_output(graph, '$(B)/pkg/' + name)['uid'] in suite['deps'],
                             name == 'pkg.final.pkg.fake')

    def test_split_java_dependency_exposes_each_created_jar(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY()\nEND()\n',
        })
        lib.tool_program(files, 'contrib/tools/ijar', 'ijar')
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for name in ('java.jar', 'java-interface.jar'):
            self.assertEqual(lib.node_by_output(graph, '$(B)/java/' + name)['uid'] in suite['deps'],
                             name == 'java.jar')

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

    def test_conditional_java_tags_refine_once_in_the_first_submodule(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY()\nIF(MODULE_TAG == "FULL_JAR_COMPILATION")\nONLY_TAGS(FULL_JAR_COMPILATION)\nELSE()\nONLY_TAGS(IJAR)\nENDIF()\nEND()\n',
        })
        lib.tool_program(files, 'contrib/tools/ijar', 'ijar')
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        self.assertIn(lib.node_by_output(graph, '$(B)/java/java.jar')['uid'], suite['deps'])
        self.assertFalse(any('$(B)/java/java-interface.jar' in node['outputs'] for node in graph['graph']))

    def test_python23_refines_tags_in_its_first_python2_context(self):
        files = test_python2.python2_base()
        files.update({
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(native)\nEND()\n',
            'native/ya.make': f'PY23_NATIVE_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nIF(MODULE_TAG == "PY2_NATIVE")\nONLY_TAGS(PY3)\nENDIF()\nSRCS(local.cpp)\nEND()\n',
            'native/local.cpp': '',
        })
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        self.assertIn(lib.node_by_output(graph, '$(B)/native/libpy3cnative.a')['uid'], graph['result'])
        self.assertFalse(any('$(B)/native/libnative.a' in node['outputs'] for node in graph['graph']))

    def test_excluded_java_first_unit_does_not_construct_its_producers(self):
        files = lib.java_tree(**{
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(java)\nEND()\n',
            'java/ya.make': 'JAVA_LIBRARY()\nONLY_TAGS()\nRUN_JAVA_PROGRAM(missing/tool OUT generated.java)\nJAVA_SRCS(generated.java)\nEND()\n',
        })
        graph = lib.make(files, 'tests', '-t')
        self.assertFalse(any(output.startswith('$(B)/java/') for node in graph['graph'] for output in node['outputs']))

    def test_selected_proto_siblings_execute_their_own_source_bodies(self):
        files = target_files()
        files.update({
            'tests/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n',
            'p/ya.make': 'PROTO_LIBRARY()\nIF(MODULE_TAG == "CPP_PROTO")\nONLY_TAGS(JAVA_PROTO GO_PROTO)\nENDIF()\nIF(MODULE_TAG == "JAVA_PROTO")\nSRCS(java.proto)\nELSEIF(MODULE_TAG == "GO_PROTO")\nSRCS(go.proto)\nENDIF()\nEND()\n',
            'p/java.proto': 'syntax = "proto3";\nmessage Java {}\n',
            'p/go.proto': 'syntax = "proto3";\nmessage Go {}\n',
        })
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        java = lib.node_by_output(graph, '$(B)/p/p.jar')
        go = lib.node_by_output(graph, '$(B)/p/p.a')
        self.assertIn(java['uid'], graph['result'])
        self.assertIn(go['uid'], graph['result'])
        self.assertNotIn(java['uid'], suite['deps'])
        self.assertNotIn(go['uid'], suite['deps'])
        self.assertTrue(any('$(S)/p/java.proto' in node['inputs'] for node in graph['graph']))
        self.assertTrue(any('$(B)/p/go.pb.go' in node['outputs'] for node in graph['graph']))
        self.assertFalse(any('$(B)/p/java.pb.go' in node['outputs'] or '$(B)/p/go.jsrc' in node['outputs'] for node in graph['graph']))

    def test_python_program_depends_selects_binary_and_library_contexts(self):
        files = test_python_suites.fixture(macro='PY3_PROGRAM')
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(lib)\nEND()\n'
        graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        binary = lib.node_by_output(graph, '$(B)/lib/lib')
        archive = lib.node_by_output(graph, '$(B)/lib/libpy3lib.global.a')
        self.assertIn(binary['uid'], suite['deps'])
        self.assertIn(archive['uid'], graph['result'])
        self.assertNotIn(archive['uid'], suite['deps'])

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

    def test_proto_depends_builds_selected_python_context_even_without_start_target(self):
        for tag, version, prefix in (('PY_PROTO', 'py2', 'libpy'), ('PY3_PROTO', 'py3', 'libpy3')):
            with self.subTest(tag=tag):
                files = test_proto_py2_context.proto_world()
                files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n'
                files['p/ya.make'] = files['p/ya.make'].replace('GRPC()', f'ONLY_TAGS({tag})\nGRPC()')
                if version == 'py2':
                    root = lib.make(files, 'p')
                    self.assertFalse(any('__intpy2__' in out for n in root['graph'] for out in n['outputs']))
                graph = lib.make(files, 'tests', '-t', '--strip-packages-from-results')
                suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                archive = lib.node_by_output(graph, f'$(B)/p/{prefix}p.global.a')
                self.assertIn(archive['uid'], graph['result'])
                self.assertNotIn(archive['uid'], suite['deps'])
                producer = lib.node_by_output(graph, f'$(B)/p/a__int{version}___pb2.py')
                self.assertEqual(producer['cmds'][0]['cmd_args'][3], version)
                opposite = 'py3' if version == 'py2' else 'py2'
                self.assertFalse(any(out.startswith('$(B)/p/a__int' + opposite + '__')
                                     for n in graph['graph'] for out in n['outputs']))
                self.assertFalse(any('$(B)/p/a.pb.h' in n['outputs'] for n in graph['graph']))
                if version == 'py2':
                    native = lib.node_by_output(graph, '$(B)/p/libpyp.a')
                    self.assertIn(native['uid'], graph['result'])
                    self.assertNotIn(native['uid'], suite['deps'])

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
