import unittest

from tst import lib
from tst.make.test_yql import udf_fixture


class YqlResultsTest(unittest.TestCase):
    def fixture(self, body='ARCHIVE_ASM(NAME Payload data.bin)\n', tags=''):
        files = udf_fixture()
        files.update({
            'udf/ya.make': f'YQL_UDF(named)\n{lib.BARE_MODULE}UDF_NO_PROBE()\n{tags}SRCS(body.cpp)\n{body}END()\n',
            'udf/body.cpp': 'int body;\n',
            'udf/data.bin': 'payload\n',
            'root/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE_ROOT_RELATIVE(udf)\n',
            'consumer/ya.make': f'CI_GROUP()\n{lib.BARE_MODULE}PEERDIR(udf)\nEND()\n',
            'tests/ya.make': 'EXECTEST()\nRUN(tool)\nDEPENDS(udf tool)\nEND()\n',
        })
        lib.tool_program(files, 'contrib/tools/yasm', 'yasm')
        lib.tool_program(files, 'tool', 'tool')
        return files

    def outputs(self, graph):
        results = set(graph['result'])
        return {output for node in graph['graph'] if node['uid'] in results for output in node['outputs']}

    def test_requested_udf_retains_each_static_start_result(self):
        expected = {'$(B)/udf/' + name for name in (
            'libnamed.so', 'libnamed.a', 'libnamed.global.a',
            'yql-abi-current-named.a', 'yql-abi-current-named.global.a',
        )}
        for targets in (('udf',), ('root',), ('consumer', 'udf'), ('udf', 'consumer')):
            with self.subTest(targets=targets):
                graph = lib.make(self.fixture(), *targets, '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
                self.assertEqual({p for p in self.outputs(graph) if p.startswith('$(B)/udf/')}, expected)

    def test_global_only_static_variants_have_no_fabricated_local_result(self):
        graph = lib.make(self.fixture(body=''), 'udf', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
        self.assertEqual(self.outputs(graph), {'$(B)/udf/' + name for name in (
            'libnamed.so', 'libnamed.global.a', 'yql-abi-current-named.global.a',
        )})

    def test_java_recurse_registers_each_udf_start_variant(self):
        files = lib.java_tree(**self.fixture())
        files['java/ya.make'] = 'JAVA_LIBRARY()\nEND()\nRECURSE_ROOT_RELATIVE(udf)\n'
        graph = lib.make(files, 'java', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
        self.assertEqual({p for p in self.outputs(graph) if p.startswith('$(B)/udf/')}, {
            '$(B)/udf/' + name for name in ('libnamed.so', 'libnamed.a', 'libnamed.global.a',
                'yql-abi-current-named.a', 'yql-abi-current-named.global.a')
        })

    def test_udf_tag_selection_controls_start_results(self):
        cases = (
            ('ONLY_TAGS(YQL_UDF_SHARED)', ('libnamed.so',)),
            ('ONLY_TAGS(YQL_UDF_STATIC)', ('libnamed.a', 'libnamed.global.a')),
            ('ONLY_TAGS(YQL_UDF_STATIC_CURRENT)', ('yql-abi-current-named.a', 'yql-abi-current-named.global.a')),
            ('EXCLUDE_TAGS(YQL_UDF_STATIC YQL_UDF_STATIC_CURRENT)', ('libnamed.so',)),
        )
        for tags, expected in cases:
            with self.subTest(tags=tags):
                graph = lib.make(self.fixture(tags=tags + '\n'), 'udf', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
                self.assertEqual(self.outputs(graph), {'$(B)/udf/' + name for name in expected})

    def test_depends_retains_each_selected_udf_context_as_a_result(self):
        graph = lib.make(self.fixture(), 'tests', '-t', '--strip-packages-from-results', '--target-platform', 'default-linux-x86_64')
        self.assertEqual({p for p in self.outputs(graph) if p.startswith('$(B)/udf/')}, {
            '$(B)/udf/' + name for name in ('libnamed.so', 'libnamed.a', 'libnamed.global.a',
                'yql-abi-current-named.a', 'yql-abi-current-named.global.a')
        })
        suite = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
        for name in ('libnamed.so', 'libnamed.a', 'libnamed.global.a',
                     'yql-abi-current-named.a', 'yql-abi-current-named.global.a'):
            self.assertEqual(lib.node_by_output(graph, '$(B)/udf/' + name)['uid'] in suite['deps'],
                             name == 'libnamed.so')


if __name__ == '__main__':
    unittest.main(verbosity=2)
