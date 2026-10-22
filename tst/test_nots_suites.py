import base64
import json
import unittest

from lib import make, node_by_output, resource_lib, tool_program
import test_nots
import test_proto_py2_context
import test_proto_targets
from test_java_tests import option_values, run_args
from test_proto_desc import desc_files


class NotsSuitesTest(unittest.TestCase):
    def workspace(self, macro):
        files = test_nots.NotsTests().project(macro)
        for name, peers in (('web', ['left', 'right']), ('left', ['leaf']), ('right', ['leaf']), ('leaf', [])):
            if name != 'web':
                files[f'{name}/ya.make'] = 'TS_PACKAGE()\nEND()\n'
            files[f'{name}/package.json'] = json.dumps({
                'dependencies': {peer: f'workspace:../{peer}' for peer in peers},
            })
            files[f'{name}/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
        return files

    def assert_workspace_dependencies(self, graph, test):
        builder = node_by_output(graph, '$(B)/devtools/frontend_build_platform/nots/builder/builder')
        self.assertIn(builder['uid'], test['deps'])
        self.assertIn(node_by_output(graph, '$(B)/web/package.json')['uid'], test['deps'])
        for peer in ('left', 'right', 'leaf'):
            for output in ('package.json', 'output.tar'):
                producer = node_by_output(graph, f'$(B)/{peer}/{output}')
                self.assertEqual(test['deps'].count(producer['uid']), 1, producer['outputs'])

    def test_script_suites_depend_on_preparation_and_workspace_packages(self):
        files = self.workspace('TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_LINT(lint)')
        files['web/ya.make'] += '\nRECURSE_FOR_TESTS(tests)\n'
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n'
        graph = make(files, 'web', '--target-platform-tests', '--output-only-tests', '-DAUTOCHECK=yes')
        own_build = node_by_output(graph, '$(B)/web/output.tar')['uid']
        for project, name in (('web', 'ts_lint_lint'), ('web/tests', 'ts_test_test')):
            with self.subTest(name=name):
                test = node_by_output(graph, f'$(B)/{project}/test-results/{name}/meta.json')
                self.assert_workspace_dependencies(graph, test)
                self.assertNotIn(own_build, test['deps'])
                self.assertIn('$(S)/web/index.js', test['inputs'])
                self.assertNotIn('$(B)/web/package.json', test['inputs'])

    def test_legacy_suite_depends_on_test_module_and_workspace_packages(self):
        files = self.workspace('TS_PACKAGE()')
        files.update({
            'web/tests/ya.make': 'TS_TEST_VITEST_FOR(web)\nTS_TEST_SRCS(../src)\nEND()\n',
            'web/vitest.config.ts': 'export default {};\n',
            'web/src/unit.test.ts': "import './value';\n",
            'web/src/value.ts': 'export default 1;\n',
        })
        graph = make(files, 'web/tests', '--target-platform-tests', '--output-only-tests', '-DAUTOCHECK=yes')
        test = node_by_output(graph, '$(B)/web/tests/test-results/vitest/meta.json')
        self.assert_workspace_dependencies(graph, test)
        module = node_by_output(graph, '$(B)/web/tests/web-tests.ts_test.fake')
        self.assertIn(module['uid'], test['deps'])
        self.assertIn('$(S)/web/src/value.ts', module['inputs'])
        self.assertIn(node_by_output(graph, '$(B)/web/output.tar')['uid'], module['deps'])
        self.assertEqual(test['inputs'], ['$(S)/web/tests', 'web/package.json'])

    def test_legacy_styles_depend_on_workspace_packages(self):
        files = self.workspace('TS_VITE()\nTS_TYPECHECK()')
        files['web/tsconfig.json'] = '{}'
        graph = make(files, 'web', '--target-platform-tests', '--output-only-tests', '-DAUTOCHECK=yes')
        for name in ('eslint', 'tsc_typecheck'):
            with self.subTest(name=name):
                test = node_by_output(graph, f'$(B)/web/test-results/{name}/meta.json')
                self.assert_workspace_dependencies(graph, test)
                self.assertIn('$(B)/web/package.json', test['inputs'])

    def test_schema_workspace_dependency_keeps_default_descriptions(self):
        files = test_nots.NotsTests().project('TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_LINT(lint)')
        files.update(test_proto_targets.target_files())
        files.update(test_proto_py2_context.proto_world())
        files.update(desc_files())
        files.update({
            'web/package.json': '{"dependencies":{"api":"workspace:../api"}}',
            'web/tests/ya.make': 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n',
            'web/ya.make': files['web/ya.make'] + '\nRECURSE_FOR_TESTS(tests)\n',
            'api/ya.make': 'PROTO_SCHEMA()\nINCLUDE_TAGS(TS_PROTO_FROM_SCHEMA TS_PREPARE_DEPS)\nSRCS(api.proto)\nPEERDIR(raw)\nEND()\n',
            'api/package.json': '{}',
            'api/api.proto': 'syntax = "proto3";\nimport "raw/value.proto";\nmessage Api {}\n',
            'raw/ya.make': 'PROTO_LIBRARY()\nSRCS(value.proto)\nEND()\n',
            'raw/value.proto': 'syntax = "proto3";\nmessage Value {}\n',
        })
        graph = make(files, 'web', '--target-platform-tests', '--output-only-tests', '-DAUTOCHECK=yes')
        descriptions = node_by_output(graph, '$(B)/api/api.protodesc')
        raw = node_by_output(graph, '$(B)/raw/raw.self.protodesc')
        for project, name in (('web', 'ts_lint_lint'), ('web/tests', 'ts_test_test')):
            with self.subTest(name=name):
                test = node_by_output(graph, f'$(B)/{project}/test-results/{name}/meta.json')
                self.assertIn(descriptions['uid'], test['deps'])
                self.assertNotIn(raw['uid'], test['deps'])
                self.assertIn(node_by_output(graph, '$(B)/api/output.tar')['uid'], test['deps'])
        self.assertIn(raw['outputs'][0], descriptions['inputs'])

    def test_descriptor_resource_globals_reach_typescript_test_suites(self):
        files = test_nots.NotsTests().project('TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_LINT(lint)')
        files.update({
            'web/package.json': '{"dependencies":{"api":"workspace:../api"}}',
            'api/ya.make': 'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\nPEERDIR(raw)\nEND()\n',
            'api/package.json': '{}',
            'raw/ya.make': 'PROTO_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSTYLE_PYTHON()\nSTYLE_CPP()\nWITH_KOTLIN_GRPC()\nEND()\n',
            'build/external_resources/black/ya.make': resource_lib('BLACK', 2),
            'build/platform/clang/clang-format/ya.make': resource_lib('CLANG_FORMAT', 3),
            'build/platform/java/kotlin_grpc/ya.make': resource_lib('KOTLIN_GRPC', 4),
            'contrib/libs/protobuf/builtin_proto/protos_from_protoc/ya.make': 'PROTO_LIBRARY()\nEND()\n',
        })
        tool_program(files, 'contrib/tools/protoc', 'protoc')
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n'
        graph = make(files, 'web', '--target-platform-tests')
        check = node_by_output(graph, '$(B)/web/test-results/ts_lint_lint/meta.json')
        for output in ('$(B)/web/package.json', '$(B)/api/package.json', '$(B)/api/output.tar'):
            self.assertIn(node_by_output(graph, output)['uid'], check['deps'])
        descriptor = node_by_output(graph, '$(B)/raw/raw.self.protodesc')
        self.assertNotIn(descriptor['uid'], check['deps'])
        globals = option_values(check['cmds'][-1]['cmd_args'], '--global-resource')
        for name in ('BLACK', 'CLANG_FORMAT', 'KOTLIN_GRPC'):
            self.assertIn(f'{name}_RESOURCE_GLOBAL::$(B)/resources/{name}', globals)
        files['raw/ya.make'] = files['raw/ya.make'].replace('STYLE_PYTHON()\n', '')
        files['api/ya.make'] = files['api/ya.make'].replace('PEERDIR(raw)', 'STYLE_PYTHON()\nPEERDIR(raw)')
        graph = make(files, 'web/tests', '--target-platform-tests')
        check = node_by_output(graph, '$(B)/web/tests/test-results/ts_test_test/meta.json')
        globals = option_values(check['cmds'][-1]['cmd_args'], '--global-resource')
        for name in ('BLACK', 'CLANG_FORMAT', 'KOTLIN_GRPC'):
            self.assertIn(f'{name}_RESOURCE_GLOBAL::$(B)/resources/{name}', globals)

    def test_script_checks_use_source_inputs_and_node_modules_recipe(self):
        files = test_nots.NotsTests().project('TS_LIBRARY()\nTS_BUILD_OUTPUTS(build)\nTS_BUILD_SCRIPT(build)\nTS_LINT(lint:ci)')
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test TIMEOUT_MEDIUM)\nEND()\n'
        files['web/ya.make'] += '\nRECURSE_FOR_TESTS(tests)\n'
        graph = make(files, 'web', '--target-platform-tests', '-DAUTOCHECK=yes', '--target-platform', 'default-linux-x86_64', '--tests-retries', '3')
        check = node_by_output(graph, '$(B)/web/test-results/ts_lint_lint_ci/meta.json')
        self.assertIn('--no-clean-environment', check['cmds'][-1]['cmd_args'])
        self.assertIn('$(S)/web/index.js', check['inputs'])
        self.assertNotIn('$(B)/web/package.json', check['inputs'])
        self.assertEqual(check['requirements'], {'cpu': 4, 'ram': 16, 'ram_disk': 4, 'network': 'restricted'})
        self.assertEqual(option_values(run_args(check), '--script-name'), ['lint:ci'])
        recipes = base64.b64decode(option_values(check['cmds'][-1]['cmd_args'], '--recipes')[0]).decode()
        self.assertIn('extract-node-modules web node_modules.layer --squashfs-tools-path $SQUASHFS_TOOLS_RESOURCE_GLOBAL', recipes)
        test = node_by_output(graph, '$(B)/web/tests/test-results/ts_test_test/meta.json')
        self.assertEqual(option_values(test['cmds'][-1]['cmd_args'], '--test-size'), ['medium'])

    def test_legacy_jest_uses_target_config_and_raw_manifest_input(self):
        files = test_nots.NotsTests().project()
        files.update({
            'web/tests/ya.make': 'TS_TEST_JEST_FOR(RELATIVE ../)\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../jest.config.js)\nEND()\n',
            'web/src/a.test.ts': 'it("works", () => {});',
            'web/jest.config.js': 'module.exports = {};',
        })
        graph = make(files, 'web/tests', '--target-platform-tests', '-DAUTOCHECK=yes')
        test = node_by_output(graph, '$(B)/web/tests/test-results/jest/meta.json')
        self.assertEqual(test['inputs'], ['$(S)/web/tests', 'web/package.json'])
        self.assertEqual(option_values(run_args(test), '--config'), ['jest.config.js'])
        self.assertIn('--supports-canonization', test['cmds'][-1]['cmd_args'])

    def test_legacy_styles_use_prepared_manifest_and_filter_extensions(self):
        files = test_nots.NotsTests().project('TS_VITE()\nTS_ESLINT_CONFIG(.eslintrc.cjs)\nTS_TYPECHECK()\nTS_STYLELINT(.stylelintrc)\nTS_BIOME(biome.json)')
        files.update({'web/a.ts': '', 'web/a.css': '', 'web/tsconfig.json': '{}',
                      'web/.eslintrc.cjs': '{}', 'web/.stylelintrc': '{}', 'web/biome.json': '{}'})
        graph = make(files, 'web', '--target-platform-tests', '-DAUTOCHECK=yes', '--target-platform', 'default-linux-x86_64')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        for name in ('eslint', 'tsc_typecheck', 'ts_stylelint', 'ts_biome'):
            with self.subTest(name=name):
                test = node_by_output(graph, f'$(B)/web/test-results/{name}/meta.json')
                self.assertIn(prepare['uid'], test['deps'])
                self.assertIn('$(B)/web/package.json', test['inputs'])
                self.assertEqual(option_values(test['cmds'][-1]['cmd_args'], '--test-run-cwd'), ['$(B)/web'])
        style = node_by_output(graph, '$(B)/web/test-results/ts_stylelint/meta.json')
        self.assertIn('a.css', run_args(style))
        self.assertNotIn('a.ts', run_args(style))

    def test_legacy_test_config_and_data_directories(self):
        files = test_nots.NotsTests().project()
        files.update({
            'web/tests/ya.make': 'TS_TEST_JEST_FOR(web)\nTS_TEST_SRCS(../src)\n'
                                 'TS_CONFIG(../tsconfig.test.json)\nTS_TEST_DATA(data/*.json)\nEND()\n',
            'web/src/a.test.ts': 'it("works", () => {});',
            'web/tsconfig.test.json': '{}',
            'web/tests/data/a.json': '{}',
            'web/tests/data/b.json': '{}',
        })
        graph = make(files, 'web/tests', '--target-platform-tests', '-DAUTOCHECK=yes')
        test = node_by_output(graph, '$(B)/web/tests/test-results/jest/meta.json')
        args = run_args(test)
        self.assertEqual(option_values(args, '--ts-config-path'), ['tsconfig.test.json'])
        self.assertEqual(option_values(args, '--test-data-dirs'), ['web/tests/data'])
        self.assertEqual(args.count('web/tests/data'), 1)

    def test_optional_typecheck_and_empty_style_inputs(self):
        files = test_nots.NotsTests().project('TS_VITE()\nTS_TYPECHECK(tsconfig.check.json)\nTS_STYLELINT(style.json)')
        files.update({'web/tsconfig.check.json': '{}', 'web/a.ts': ''})
        graph = make(files, 'web', '--target-platform-tests', '-DAUTOCHECK=yes')
        test = node_by_output(graph, '$(B)/web/test-results/tsc_typecheck/meta.json')
        self.assertIn('tsconfig.check.json', run_args(test))
        self.assertFalse(any('ts_stylelint/meta.json' in out for n in graph['graph'] for out in n['outputs']))
        files['web/ya.make'] = files['web/ya.make'].replace('END()', 'NO_TS_TYPECHECK()\nEND()')
        graph = make(files, 'web', '--target-platform-tests', '-DAUTOCHECK=yes')
        self.assertFalse(any('tsc_typecheck/meta.json' in out for n in graph['graph'] for out in n['outputs']))


if __name__ == '__main__':
    unittest.main()
