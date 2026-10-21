import unittest

import lib
import test_go_cgo
import test_headers
import test_nots
import test_sdc_diags
from test_ymake_conf import Tree


def library(body):
    return f'LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n'


class SourceSelectionContractsTest(unittest.TestCase):
    def test_cgo_keeps_global_native_sources_out_of_package_sources(self):
        files = test_go_cgo.cgo_world()
        test_go_cgo.zstd_binding(files)
        directory = test_go_cgo.ZSTD
        files[f'{directory}/ya.make'] = files[f'{directory}/ya.make'].replace(
            'END()', 'SRCS(GLOBAL global.c GLOBAL global.S)\nEND()')
        files[f'{directory}/global.c'] = 'int global;\n'
        files[f'{directory}/global.S'] = '.text\n'
        graph = lib.make(files, directory)
        archive = lib.node_by_output(graph, f'$(B)/{directory}/zstd.a')
        args = archive['cmds'][0]['cmd_args']
        for name in ('global.c', 'global.S'):
            self.assertNotIn(f'$(S)/{directory}/{name}', args)
            lib.node_by_output(graph, f'$(B)/{directory}/{name}.o')

    def test_headers_does_not_scan_generated_directories(self):
        files = test_headers.header_files()
        files['producer/ya.make'] = library('PEERDIR(proto)\nSRCS(local.cpp)\nHEADERS(${BINDIR})')
        graph = lib.make(files, 'consumer')
        link = lib.node_by_output(graph, '$(B)/consumer/consumer')
        self.assertNotIn('$(B)/proto/a.pb.h', link['inputs'])

    def test_explicit_fake_module_flags_preserve_linker_plugin_export(self):
        for setting in ('_FAKE_MODULE yes', 'NEED_ADD_FAKE_SRC no'):
            with self.subTest(setting=setting):
                files = {'producer/ya.make': library(f'SRCS(local.cpp)\nSET({setting})\nLD_PLUGIN(plugin.py)'),
                         'producer/local.cpp': '', 'producer/plugin.py': 'def plugin(): pass\n',
                         'consumer/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(producer)\nSRCS(main.cpp)\nEND()\n',
                         'consumer/main.cpp': 'int main() {}\n'}
                graph = lib.make(files, 'consumer')
                link = lib.node_by_output(graph, '$(B)/consumer/consumer')
                plugin = lib.node_by_output(graph, '$(B)/producer/plugin.py.pyplugin')
                self.assertIn(plugin['outputs'][0], link['inputs'])
                self.assertIn(plugin['uid'], link['deps'])

    def test_header_directory_globs_support_single_character_matches_and_exclusions(self):
        files = test_headers.header_files()
        files['producer/ha/public.h'] = files.pop('producer/public.h')
        files['producer/local.cpp'] = ''
        files['consumer/main.cpp'] = 'int main() {}\n'
        for exclude, exported in (('', True), ('EXCLUDE h?/p?blic.h', False)):
            with self.subTest(exclude=exclude):
                files['producer/ya.make'] = library(f'PEERDIR(proto)\nSRCS(local.cpp)\nHEADERS(h? {exclude})')
                graph = lib.make(files, 'consumer')
                link = lib.node_by_output(graph, '$(B)/consumer/consumer')
                self.assertEqual('$(B)/proto/a.pb.h' in link['inputs'], exported)

    def test_diagnostic_group_can_be_generated_from_another_module(self):
        files = test_sdc_diags.files()
        group = test_sdc_diags.ROOT + '/Hardware'
        files[group + '/_manifest_.yaml'] = 'name: hardware\n'
        files['generator/ya.make'] = library(
            f'RUN_PROGRAM(tools/severity OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{test_sdc_diags.SEVERITY})\n'
            f'SDC_DIAGS_SPLIT_GENERATOR_V3({group})')
        output = test_sdc_diags.GENERATED + '/diagnostics/v3/Hardware/_group.h'
        files['app/ya.make'] = library('PEERDIR(generator)\nSRCS(main.cpp)')
        files['app/main.cpp'] = '#include "' + output.removeprefix('$(B)/') + '"\n'
        graph = lib.make(files, 'app')
        generated = lib.node_by_output(graph, output)
        self.assertIn('$(S)/' + group + '/_manifest_.yaml', generated['inputs'])
        self.assertIn(generated['uid'], lib.node_by_output(graph, '$(B)/app/main.cpp.o')['deps'])

    def test_missing_linker_configuration_reports_its_path(self):
        tree = Tree(self, {'app/ya.make': library('SRCS(main.cpp)'), 'app/main.cpp': ''})
        (tree.src / 'build/conf/linkers/ld.conf').unlink()
        result = tree.make('app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('linker conf: build/conf/linkers/ld.conf is missing', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_package_without_source_files_has_no_style_checks(self):
        files = test_nots.NotsTests().project()
        del files['web/index.js']
        graph = lib.make(files, 'web', '-t')
        lib.node_by_output(graph, '$(B)/web/output.tar')
        self.assertFalse(any(n['kv'].get('run_test_node') for n in graph['graph']))

    def test_docs_accepts_general_no_lint_macro(self):
        graph = lib.make({'guide/ya.make': 'DOCS()\nNO_LINT(ktlint)\nSRCS(index.md)\nEND()\n',
                          'guide/index.md': '# Index\n'}, 'guide', '-t')
        lib.node_by_output(graph, '$(B)/guide/guide.tar.gz')
        self.assertFalse(any(n['kv'].get('run_test_node') for n in graph['graph']))

    def test_long_single_component_test_name_is_preserved(self):
        name = 'long_unittest_module_' + 'x' * 40
        files = {f'{name}/ya.make': f'UNITTEST()\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\n',
                 f'{name}/main.cpp': '', 'library/cpp/testing/unittest_main/ya.make': library('')}
        graph = lib.make(files, name, '-t')
        lib.node_by_output(graph, f'$(B)/{name}/{name}')
        lib.node_by_output(graph, f'$(B)/{name}/test-results/unittest/meta.json')


if __name__ == '__main__':
    unittest.main(verbosity=2)
