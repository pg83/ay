"""Native module header language controls implicit linters independently of SBOM."""
import unittest

from tst import lib
from tst.make.test_style_resource_publication import fixture as json_yaml_fixture


def fixture(kind='UNION()', body='', before='', conditional=True, header=False):
    if kind == 'PROGRAM(cedar)':
        body = 'ALLOCATOR(FAKE)\nSRCS(main.cpp)\n' + body
    files = {
        'orchard/ya.make': before + kind + '\n' + lib.BARE_MODULE + body + '\nEND()\n',
        'orchard/linters.make.inc': 'STYLE_CPP(CONFIG_TYPE .clang-format)\n',
        'orchard/.clang-format': 'BasedOnStyle: LLVM\n',
        'orchard/main.cpp': 'int main(){return 0;}\n',
        'build/conf/autoincludes.json': '["orchard"]',
        'build/config/tests/cpp_style/default_configs.json': '{"clang_format":"orchard/.clang-format"}',
        'tools/cpp_style_checker/wrapper.py': '',
        'build/platform/clang/clang-format/ya.make': lib.resource_lib('CLANG_FORMAT', 71),
    }
    if kind != 'PROGRAM(cedar)':
        del files['orchard/main.cpp']
    if conditional:
        files['orchard/linters.make.inc'] = 'IF (MODULE_LANG == CPP)\n' + files['orchard/linters.make.inc'] + 'ENDIF()\n'
    if header:
        files['orchard/cedar.hpp'] = '#pragma once\n'
    return files


def make(files, *flags):
    graph = lib.make(files, 'orchard', '-t', '--sandboxing', *flags)
    ids = {node['uid'] for node in graph['graph']}
    assert set(graph['result']) <= ids
    assert all(set(node['deps']) <= ids for node in graph['graph'])
    return graph


def cpp_suites(graph):
    return [node for node in graph['graph'] if '$(B)/orchard/test-results/clang_format/meta.json' in node['outputs']]


class ModuleLanguageConfigurationTest(unittest.TestCase):
    def assert_cpp_suite(self, graph, expected):
        suites = cpp_suites(graph)
        self.assertEqual(len(suites), int(expected))
        if expected:
            node = suites[0]
            for path in ('$(S)/orchard', '$(S)/orchard/.clang-format', '$(S)/tools/cpp_style_checker/wrapper.py'):
                self.assertIn(path, node['inputs'])
            args = node['cmds'][0]['cmd_args']
            self.assertEqual(args.count('CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT'), 2)

    def test_unset_script_language_does_not_select_cpp_linter(self):
        for kind in ('UNION()', 'CI_GROUP()', 'GEN_LIBRARY()'):
            with self.subTest(kind=kind):
                self.assert_cpp_suite(make(fixture(kind)), False)
        self.assert_cpp_suite(make(fixture(header=True)), False)

    def test_sdc_union_does_not_invent_a_cpp_language(self):
        files = fixture('SDC_UNION(cedar)', body='FILES(payload.txt)')
        files['orchard/payload.txt'] = 'payload\n'
        files['consumer/ya.make'] = 'PROGRAM(reader)\n' + lib.BARE_MODULE + 'ALLOCATOR(FAKE)\nSRCS(main.cpp)\nDEPENDS(orchard)\nEND()\n'
        files['consumer/main.cpp'] = 'int main(){return 0;}\n'
        graph = lib.make(files, 'consumer', 'orchard', '-t', '--sandboxing')
        self.assert_cpp_suite(graph, False)
        self.assertTrue(graph['result'])
        lib.node_by_output(graph, '$(B)/consumer/reader')
        self.assertTrue(any('$(S)/consumer/main.cpp' in node['inputs'] for node in graph['graph']))

    def test_sdc_union_preserves_explicit_language_and_style(self):
        for body, before, flags, conditional in (
            ('SET(MODULE_LANG CPP)', '', (), True),
            ('', 'SET(MODULE_LANG CPP)\n', (), True),
            ('', '', ('-DMODULE_LANG=CPP',), True),
            ('', '', (), False),
        ):
            with self.subTest(body=body, before=before, flags=flags, conditional=conditional):
                self.assert_cpp_suite(make(fixture(
                    'SDC_UNION(cedar)', body=body, before=before, conditional=conditional,
                ), *flags), True)
        self.assert_cpp_suite(make(fixture('SDC_UNION(cedar)'), '-DMODULE_LANG=CEDAR'), False)
        self.assert_cpp_suite(make(fixture('SDC_UNION(cedar)', body='SET(MODULE_LANG)'), '-DMODULE_LANG=CPP'), False)
        self.assert_cpp_suite(make(fixture('SDC_UNION(cedar)', body='SET(MODULE_LANG CPP)\nNO_LINT()')), False)

    def test_ordinary_cpp_headers_assign_their_language(self):
        for kind in ('LIBRARY()', 'PROGRAM(cedar)'):
            with self.subTest(kind=kind):
                self.assert_cpp_suite(make(fixture(kind)), True)
        graph = make(fixture('LIBRARY()', header=True))
        self.assert_cpp_suite(graph, True)
        node = cpp_suites(graph)[0]
        self.assertIn('$(S)/orchard/cedar.hpp', node['inputs'])
        self.assertIn('$(S)/orchard/cedar.hpp', node['cmds'][0]['cmd_args'])

    def test_cli_language_is_inherited_when_native_header_does_not_assign_it(self):
        self.assert_cpp_suite(make(fixture(), '-DMODULE_LANG=CPP'), True)
        self.assert_cpp_suite(make(fixture(), '-DMODULE_LANG=CEDAR'), False)
        self.assert_cpp_suite(make(fixture('LIBRARY()'), '-DMODULE_LANG=CEDAR'), True)

    def test_source_language_bindings_keep_native_header_and_body_order(self):
        self.assert_cpp_suite(make(fixture(body='SET(MODULE_LANG CPP)')), True)
        self.assert_cpp_suite(make(fixture(body='SET(MODULE_LANG)'), '-DMODULE_LANG=CPP'), False)
        self.assert_cpp_suite(make(fixture(before='SET(MODULE_LANG CPP)\n')), True)
        self.assert_cpp_suite(make(fixture('LIBRARY()', body='SET(MODULE_LANG)')), False)

    def test_unconditional_empty_cpp_style_is_preserved(self):
        self.assert_cpp_suite(make(fixture(conditional=False)), True)

    def test_json_yaml_checks_remain_independent_of_cpp_language(self):
        for kind in ('UNION()', 'CI_GROUP()', 'GEN_LIBRARY()'):
            with self.subTest(kind=kind):
                files = json_yaml_fixture(kind=kind, eligible=True)
                files['build/conf/autoincludes.json'] = '["orchard"]'
                files['orchard/linters.make.inc'] = 'IF (MODULE_LANG == CPP)\nSTYLE_CPP()\nENDIF()\n'
                graph = make(files)
                self.assert_cpp_suite(graph, False)
                for name, source in (('clang_format_json', 'cedar.json'), ('yamlfmt', 'cedar.yaml')):
                    suite = lib.node_by_output(graph, '$(B)/orchard/test-results/' + name + '/meta.json')
                    self.assertIn('$(S)/orchard/' + source, suite['inputs'])
                    self.assertIn('$(S)/orchard/' + source, suite['cmds'][0]['cmd_args'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
