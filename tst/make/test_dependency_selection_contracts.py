import unittest

from tst import lib
from tst.make import test_java
from tst.make import test_java_features
from tst.make import test_nots
from tst.make.test_source_selection_contracts import library


class DependencySelectionContractsTest(unittest.TestCase):
    def test_jni_wrapper_uses_explicit_system_jdk_includes(self):
        files = test_java.swig_jni_tree()
        files['jni/ya.make'] = files['jni/ya.make'].replace('SRCS(', 'SET(USE_SYSTEM_JDK yes)\nADDINCL(sdk)\nSRCS(')
        files['jni/api.h'] = '#include <jni.h>\n'
        files['sdk/jni.h'] = ''
        graph = lib.make(files, 'jni')
        compiled = lib.node_by_output(graph, '$(B)/jni/wrap_wrap.swg.cpp.pic.o')
        self.assertIn('$(S)/sdk/jni.h', compiled['inputs'])
        self.assertNotIn('$(S)/contrib/libs/jdk/include/jni.h', compiled['inputs'])

    def test_jni_library_without_swig_sources_builds_plain_native_code(self):
        files = test_java.swig_jni_tree()
        files['jni/ya.make'] = f'DLL_JAVA(native)\n{lib.BARE_MODULE}SRCS(impl.cpp)\nEND()\n'
        graph = lib.make(files, 'jni')
        lib.node_by_output(graph, '$(B)/jni/libnative.so')
        self.assertFalse(any(n['kv']['p'] == 'SW' for n in graph['graph']))

    def test_child_dependency_management_is_preserved_without_parent_rule(self):
        root = 'contrib/java/x'
        files = test_java_features.tree('PEERDIR(middle)', module='JAVA_LIBRARY_NON_SPLIT', **{
            'middle/ya.make': f'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR({root})\nDEPENDENCY_MANAGEMENT({root}/2)\nEND()\n',
            root + '/ya.make': f'JAVA_CONTRIB_PROXY()\nPEERDIR({root}/1)\nEND()\n',
            **test_java.contrib(root + '/1', jar=1), **test_java.contrib(root + '/2', jar=2)})
        graph = lib.make(files, 'app')
        app = lib.node_by_output(graph, '$(B)/app/app.jar')
        classpath = test_java.writer_args(app, 'bfg.txt')
        self.assertTrue(any('/x/2/' in p for p in classpath), classpath)
        self.assertFalse(any('/x/1/' in p for p in classpath), classpath)

    def test_parent_management_unifies_distinct_transitive_versions(self):
        root = 'contrib/java/x'
        files = test_java_features.tree(f'PEERDIR(left right)\nDEPENDENCY_MANAGEMENT({root}/3)', module='JAVA_LIBRARY_NON_SPLIT')
        for version in (1, 2, 3):
            files.update(test_java.contrib(f'{root}/{version}', jar=version))
        for side, version in (('left', 1), ('right', 2)):
            files[side + '/ya.make'] = f'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR({root}/{version})\nEND()\n'
        graph = lib.make(files, 'app')
        app = lib.node_by_output(graph, '$(B)/app/app.jar')
        classpath = test_java.writer_args(app, 'bfg.txt')
        selected = [p for p in classpath if '/x/' in p]
        self.assertEqual(len(selected), 1, classpath)
        self.assertIn('/x/3/', selected[0])

    def test_repeated_recursed_test_target_emits_one_suite(self):
        files = {'first/ya.make': library('') + 'RECURSE_ROOT_RELATIVE(shared)\n',
                 'second/ya.make': library('') + 'RECURSE_ROOT_RELATIVE(shared)\n',
                 'shared/ya.make': 'EXECTEST()\nRUN(check)\nEND()\n'}
        graph = lib.make(files, 'first', 'second', '-t')
        suites = [n for n in graph['graph'] if '$(B)/shared/test-results/exectest/meta.json' in n['outputs']]
        self.assertEqual(len(suites), 1)

    def test_typescript_target_shared_with_native_parent_has_one_producer(self):
        files = test_nots.NotsTests().project()
        files['app/ya.make'] = library('PEERDIR(web)\nSRCS(main.cpp)')
        files['app/main.cpp'] = ''
        graph = lib.make(files, 'app', 'web')
        for output in ('package.json', 'output.tar'):
            self.assertEqual(sum(f'$(B)/web/{output}' in n['outputs'] for n in graph['graph']), 1)
        lib.node_by_output(graph, '$(B)/app/libapp.a')


if __name__ == '__main__':
    unittest.main(verbosity=2)
