import unittest

import lib
import test_emit_bundle
import test_java_features
import test_nots
import test_python_cython
from test_source_selection_contracts import library


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
