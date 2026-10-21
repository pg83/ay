import base64
import unittest

from lib import make, node_by_output, resource_lib, tool_program
import test_nots
from test_java_tests import option_values, run_args


class NotsSuitesTest(unittest.TestCase):
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


if __name__ == '__main__':
    unittest.main()
