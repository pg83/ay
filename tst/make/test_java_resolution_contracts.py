import unittest

from tst import lib
from tst.make.test_java import contrib, writer_args
from tst.make.test_java_features import tree


class JavaResolutionContractsTest(unittest.TestCase):
    def test_native_libraries_cross_an_interface_jar_when_explicitly_requested(self):
        files = tree('PEERDIR(dep)\nADD_DLLS_TO_JAR()', module='JAVA_LIBRARY_NON_SPLIT', **{
            'dep/ya.make': 'JAVA_LIBRARY()\nPEERDIR(native)\nEND()\n',
            'native/ya.make': f'DLL()\n{lib.BARE_MODULE}SRCS(native.cpp)\nEND()\n',
            'native/native.cpp': ''})
        lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
        graph = lib.make(files, 'app')
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        native = lib.node_by_output(graph, '$(B)/native/libnative.so')
        self.assertIn(native['outputs'][0], jar['inputs'])
        self.assertIn(native['uid'], jar['deps'])

    def test_uberjar_behind_interface_does_not_export_bundled_dependencies(self):
        files = tree('PEERDIR(dep)', module='JAVA_LIBRARY_NON_SPLIT', **{
            'dep/ya.make': 'JAVA_LIBRARY()\nUBERJAR()\nPEERDIR(leaf)\nEND()\n',
            **contrib('leaf', jar=17)})
        graph = lib.make(files, 'app')
        jar = lib.node_by_output(graph, '$(B)/app/app.jar')
        self.assertIn('dep/dep-interface.jar', writer_args(jar, 'bfg.txt'))
        self.assertNotIn('leaf/leaf.jar', writer_args(jar, 'bfg.txt'))

    def test_uberjar_uses_the_full_jar_of_a_split_peer(self):
        files = tree('PEERDIR(dep)\nUBERJAR()', module='JAVA_LIBRARY_NON_SPLIT', **{
            'dep/ya.make': 'JAVA_LIBRARY()\nEND()\n'})
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertEqual(writer_args(jar, 'bfg.txt'), ['dep/dep.jar'])

    def test_excluding_transitive_split_library_removes_its_dependencies(self):
        files = tree('PEERDIR(middle)\nEXCLUDE(dep)', module='JAVA_LIBRARY_NON_SPLIT', **{
            'middle/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(dep)\nEND()\n',
            'dep/ya.make': 'JAVA_LIBRARY()\nPEERDIR(leaf)\nEND()\n', **contrib('leaf', jar=17)})
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        classpath = writer_args(jar, 'bfg.txt')
        self.assertEqual(classpath, ['middle/middle.jar'])

    def test_dependency_rule_cannot_select_an_excluded_java_proto(self):
        files = tree('PEERDIR(contrib/java/x)\nDEPENDENCY_MANAGEMENT(contrib/java/x/2)', module='JAVA_LIBRARY_NON_SPLIT', **{
            **contrib('contrib/java/x/1', jar=1),
            'contrib/java/x/ya.make': 'JAVA_CONTRIB_PROXY()\nPEERDIR(contrib/java/x/1)\nEND()\n',
            'contrib/java/x/2/ya.make': 'PROTO_LIBRARY()\nEXCLUDE_TAGS(JAVA_PROTO)\nEND()\n'})
        result = lib.make_process(files, 'app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('contrib/java/x/2', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
