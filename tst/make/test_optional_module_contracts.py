import unittest

from tst import lib
from tst.make import test_emit_bundle
from tst.make import test_java_features
from tst.make import test_nots
from tst.make import test_python_cython
from tst.make.test_source_selection_contracts import library


class OptionalModuleContractsTest(unittest.TestCase):
    def test_bundle_of_resource_only_module_remains_unresolved(self):
        files = test_emit_bundle.base_files()
        files['dep/ya.make'] = 'RESOURCES_LIBRARY()\nEND()\n'
        files['app/ya.make'] = library('BUNDLE(dep NAME payload)\nRESOURCE(payload key)')
        graph = lib.make(files, 'app')
        bundle = lib.node_by_output(graph, '$(B)/app/payload')
        self.assertEqual(bundle['inputs'], [])
        self.assertEqual(bundle.get('deps', []), [])
        self.assertEqual(bundle['cmds'][0]['cmd_args'][-2:], ['rename', '$(B)/app/payload'])

    def test_root_module_uses_generated_source(self):
        files = {'ya.make': library('RUN_PROGRAM(tool OUT_NOAUTO generated.cpp)\nSRCS(generated.cpp)')}
        lib.tool_program(files, 'tool', 'tool')
        graph = lib.make(files, '.')
        generated = lib.node_by_output(graph, '$(B)/generated.cpp')
        compiled = [n for n in graph['graph'] if n['kv']['p'] == 'CC' and '$(B)/generated.cpp' in n['inputs']]
        self.assertEqual(len(compiled), 1)
        self.assertIn(generated['uid'], compiled[0]['deps'])
        self.assertEqual(compiled[0]['outputs'], ['$(B)/generated.cpp.o'])
        archive = lib.node_by_output(graph, '$(B)/lib.a')
        self.assertIn(compiled[0]['uid'], archive['deps'])
        self.assertIn('$(B)/generated.cpp.o', archive['inputs'])
        self.assertIn(archive['uid'], graph['result'])

    def test_root_source_paths_stay_canonical_with_an_empty_srcdir(self):
        cases = [
            ('.', {'ya.make': library('SRCS(root.cpp)'), 'root.cpp': 'int root;\n'}, 'root.cpp.o', 'lib.a'),
            ('a', {'a/ya.make': library('SRCDIR(.)\nSRCS(root.cpp)'), 'root.cpp': 'int root;\n'}, 'a/__/root.cpp.o', 'a/liba.a'),
        ]
        for target, files, output, archive_output in cases:
            with self.subTest(target=target):
                graph = lib.make(files, target)
                compiled = [n for n in graph['graph'] if n['kv']['p'] == 'CC']
                self.assertEqual(len(compiled), 1)
                self.assertEqual(compiled[0]['inputs'], ['$(S)/root.cpp'])
                self.assertEqual(compiled[0]['outputs'], ['$(B)/' + output])
                archive = lib.node_by_output(graph, '$(B)/' + archive_output)
                self.assertIn(compiled[0]['uid'], archive['deps'])
                self.assertIn(compiled[0]['outputs'][0], archive['inputs'])
                self.assertIn(archive['uid'], graph['result'])

    def test_root_library_output_names_and_source_directories(self):
        for name, body, source, output in (
            ('rootlib', 'SRCS(root.cpp)', 'root.cpp', 'root.cpp.o'),
            ('', 'SRCS(nested/child.cpp)', 'nested/child.cpp', '_/nested/child.cpp.o'),
            ('', 'SRCDIR(shared)\nSRCS(root.cpp)', 'shared/root.cpp', '_/shared/root.cpp.o'),
            ('', 'SRC(nested/child.cpp)', 'nested/child.cpp', 'nested/child.cpp.o'),
            ('', 'SRC(./nested/child.cpp)', 'nested/child.cpp', 'nested/child.cpp.o'),
        ):
            with self.subTest(name=name, source=source):
                files = {'ya.make': library(body).replace('LIBRARY()', 'LIBRARY(' + name + ')'), source: 'int root;\n'}
                graph = lib.make(files, '.', '-DPIC=no')
                compiled = lib.node_by_output(graph, '$(B)/' + output)
                self.assertEqual(compiled['inputs'], ['$(S)/' + source])
                args = compiled['cmds'][0]['cmd_args']
                self.assertEqual(args[args.index('-o') + 1], compiled['outputs'][0])
                archive = lib.node_by_output(graph, '$(B)/lib' + name + '.a')
                self.assertIn(compiled['uid'], archive['deps'])
                self.assertIn(compiled['outputs'][0], archive['inputs'])
                self.assertIn(compiled['outputs'][0], archive['cmds'][0]['cmd_args'])
                self.assertEqual(graph['result'], [archive['uid']])

    def test_root_global_and_local_archives_keep_distinct_object_dependencies(self):
        files = {
            'ya.make': library('SRCS(GLOBAL global.cpp)\nSRCS(local.cpp)'),
            'global.cpp': 'int global;\n', 'local.cpp': 'int local;\n',
        }
        graph = lib.make(files, '.', '-DPIC=no')
        archives = []
        for source, archive_output in (('global.cpp', 'lib.global.a'), ('local.cpp', 'lib.a')):
            compiled = lib.node_by_output(graph, '$(B)/' + source + '.o')
            self.assertEqual(compiled['inputs'], ['$(S)/' + source])
            archive = lib.node_by_output(graph, '$(B)/' + archive_output)
            self.assertEqual(archive['inputs'], [compiled['outputs'][0], '$(S)/build/scripts/link_lib.py'])
            self.assertEqual(archive['deps'], [compiled['uid']])
            self.assertIn(compiled['outputs'][0], archive['cmds'][0]['cmd_args'])
            archives.append(archive['uid'])
        self.assertCountEqual(graph['result'], archives)

    def test_plain_library_accepts_golaunch_metadata(self):
        graph = lib.make({'app/ya.make': library('GOLAUNCH_OVERRIDE(component Launch)\nGENERATE_GOLAUNCH_CALLBACKS(callbacks)\nSRCS(main.cpp)'),
                          'app/main.cpp': ''}, 'app')
        lib.node_by_output(graph, '$(B)/app/libapp.a')
        self.assertFalse(any(p.endswith('launch.go') for n in graph['graph'] for p in n['outputs']))

    def test_empty_typescript_lint_selection_has_no_suite(self):
        files = test_nots.NotsTests().project('TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_LINT(lint)\nTS_EXCLUDE_FILES_GLOB(**/*)')
        graph = lib.make(files, 'web', '-t')
        lib.node_by_output(graph, '$(B)/web/output.tar')
        self.assertFalse(any('/ts_lint_' in p for n in graph['graph'] for p in n['outputs']))

    def test_proto_schema_exposes_typescript_variant(self):
        files = test_nots.NotsTests().project()
        files['web/package.json'] = '{"dependencies":{"api":"workspace:../api"}}'
        files['api/ya.make'] = 'PROTO_SCHEMA()\nINCLUDE_TAGS(TS_PROTO_FROM_SCHEMA TS_PREPARE_DEPS)\nSRCS(api.proto)\nEND()\n'
        files['api/api.proto'] = 'syntax = "proto3";\n'
        deps = 'library/typescript/ts-proto-deps'
        files[deps + '/ya.make'] = 'TS_PACKAGE()\nEND()\n'
        files[deps + '/package.json'] = '{}'
        files[deps + '/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
        lib.tool_program(files, 'contrib/tools/protoc', 'protoc')
        graph = lib.make(files, 'web')
        generated = lib.node_by_output(graph, '$(B)/api/output.tar')
        self.assertIn('$(S)/api/api.proto', generated['inputs'])

    def test_java_source_root_handles_repeated_separators(self):
        files = test_java_features.tree('JAVA_SRCS(SRCDIR ${ARCADIA_ROOT}/app/src //Main.java)')
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertIn('$(S)/app/src/Main.java', jar['inputs'])
        self.assertNotIn('$(S)/app/src/Skip.java', jar['inputs'])

    def test_java_unknown_source_root_does_not_become_a_source_input(self):
        files = test_java_features.tree('JAVA_SRCS(src/Main.java "$(UNKNOWN)/absent.java")')
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertIn('$(S)/app/src/Main.java', jar['inputs'])
        self.assertFalse(any('absent.java' in p for p in jar['inputs']))

    def test_missing_cython_include_reports_the_origin(self):
        files = test_python_cython.python_base()
        files['lib/ya.make'] = f'PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(module.pyx)\nEND()\n'
        files['lib/module.pyx'] = 'include "missing.pxi"\n'
        result = lib.make_process(files, 'app')
        self.assertNotEqual(result.returncode, 0, result.stderr)
        self.assertIn('missing.pxi', result.stderr)
        self.assertIn('lib/module.pyx', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
