import json
import unittest

from tst.lib import make, make_process, node_by_output, tool_program


class NotsTests(unittest.TestCase):
    def project(self, macro='TS_PACKAGE()'):
        files = {
            'web/ya.make': f'{macro}\nEND()\n',
            'web/package.json': json.dumps({'name': 'web', 'dependencies': {'dep': '1.2.3'}}),
            'web/pnpm-lock.yaml': """lockfileVersion: '9.0'
packages:
  dep@1.2.3:
    resolution: {integrity: sha512-YWJj, tarball: https://npm.yandex-team.ru/dep/-/dep-1.2.3.tgz?rbtorrent=abc}
  '@scope/pkg@2.0.0':
    resolution:
      integrity: sha1-YWJj
      tarball: https://npm.yandex-team.ru/@scope%2fpkg/-/legacy/pkg-2.0.0.tgz
  local@file:../local:
    resolution: {type: directory, directory: ../local}
""",
            'web/index.js': 'module.exports = 42;\n',
            'web/a.yaml': 'owner: ignored\n',
            'web/node_modules/dep/index.js': 'ignored\n',
            'devtools/frontend_build_platform/erm/erm-packages.json': json.dumps({
                'nodejs': {'default': '22.18.0', 'versions': {'24.1.0': {}, '22.18.0': {}, '22.20.0': {}}},
            }),
            'devtools/frontend_build_platform/nots/constants/src/tier0-settings.yaml': 'pnpm:\n  defaults:\n    registry: https://npm.yandex-team.ru\n',
        }
        tool_program(files, 'devtools/frontend_build_platform/nots/builder', 'builder')
        builder = 'devtools/frontend_build_platform/nots/builder/ya.make'
        files[builder] = 'IF(TS_USE_PREBUILT_NOTS_TOOL)\n' + files[builder] + 'ENDIF()\n'
        for name, directory in [
            ('NODEJS_22_18_0', 'build/platform/nodejs/22.18.0'),
            ('PNPM', 'build/external_resources/pnpm'),
            ('SQUASHFS_TOOLS', 'devtools/frontend_build_platform/nots/squashfs'),
        ]:
            files[f'{directory}/ya.make'] = f'RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({name} sbr:1)\nEND()\n'
        return files

    def test_package_prepares_tarballs_and_registers_bundle_dependency(self):
        graph = make(self.project(), 'web', '-DAUTOCHECK=yes', '--target-platform', 'default-linux-x86_64')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        self.assertEqual(prepare['kv'], {'p': 'TS_DEP', 'pc': 'magenta', 'text': 'web'})
        self.assertEqual(prepare['outputs'], [
            '$(B)/web/package.json', '$(B)/web/pnpm-lock.yaml', '$(B)/web/pnpm-workspace.yaml',
            '$(B)/web/node_modules.layer',
            '$(B)/web/__tarballs__/@scope/pkg/-/pkg-2.0.0.tgz',
            '$(B)/web/__tarballs__/dep/-/dep-1.2.3.tgz',
        ])
        self.assertEqual(prepare['resources'], [
            {'uri': 'https://npm.yandex-team.ru/@scope%2fpkg/-/pkg-2.0.0.tgz#integrity=sha1-YWJj'},
            {'uri': 'https://npm.yandex-team.ru/dep/-/dep-1.2.3.tgz#integrity=sha512-YWJj'},
        ])
        bundle = node_by_output(graph, '$(B)/web/output.tar')
        self.assertIn(prepare['uid'], bundle['deps'])
        self.assertEqual(bundle['kv']['p'], 'TS_PKG')
        self.assertIn('$(S)/web/index.js', bundle['inputs'])
        self.assertNotIn('$(S)/web/a.yaml', bundle['inputs'])
        self.assertNotIn('$(S)/web/node_modules/dep/index.js', bundle['inputs'])

    def test_workspace_prepare_uses_peer_outputs_and_deduplicates_v6_tarballs(self):
        files = self.project()
        files['web/package.json'] = json.dumps({'dependencies': {'peer': 'workspace:../peer'}})
        files['peer/ya.make'] = 'TS_PACKAGE()\nEND()\n'
        files['peer/package.json'] = '{"name":"peer"}'
        files['web/pnpm-lock.yaml'] = """lockfileVersion: '6.0'
packages:
  /dep@1.2.3(peer@1.0.0):
    resolution: {integrity: sha512-YWJj}
  /dep@1.2.3(peer@2.0.0):
    resolution: {integrity: sha512-YWJj}
"""
        graph = make(files, 'web', '-DAUTOCHECK=yes')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        peer = node_by_output(graph, '$(B)/peer/output.tar')
        self.assertIn(peer['uid'], prepare['deps'])
        self.assertIn('$(B)/peer/pnpm-workspace.yaml', prepare['inputs'])
        self.assertEqual(prepare['outputs'].count('$(B)/web/__tarballs__/dep/-/dep-1.2.3.tgz'), 1)


    def test_tarball_local_path_decodes_url_but_resource_uri_keeps_escaping(self):
        for tarball, normalized in [
            ('dep%2f-%2fdep-1.2.3.tgz', 'dep%2f-%2fdep-1.2.3.tgz'),
            ('dep/-/legacy/%64ep-1.2.3.tgz', 'dep/-/%64ep-1.2.3.tgz'),
        ]:
            with self.subTest(tarball=tarball):
                files = self.project()
                files['web/pnpm-lock.yaml'] = (
                    "lockfileVersion: '9.0'\npackages:\n  dep@1.2.3:\n"
                    f"    resolution: {{integrity: sha512-YWJj, tarball: https://npm.yandex-team.ru/{tarball}}}\n"
                )
                graph = make(files, 'web')
                prepare = node_by_output(graph, '$(B)/web/package.json')
                self.assertIn('$(B)/web/__tarballs__/dep/-/dep-1.2.3.tgz', prepare['outputs'])
                self.assertEqual(prepare['resources'], [
                    {'uri': f'https://npm.yandex-team.ru/{normalized}#integrity=sha512-YWJj'},
                ])

    def test_modern_test_prepares_target_without_building_it(self):
        files = self.project()
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n'
        graph = make(files, 'web/tests', '-DAUTOCHECK=yes')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        bridge = node_by_output(graph, '$(B)/web/tests/web-tests.test_for.prepare_deps.fake')
        test = node_by_output(graph, '$(B)/web/tests/web-tests')
        self.assertEqual(test['kv']['p'], 'TS_TST')
        self.assertIn(prepare['uid'], bridge['deps'])
        self.assertIn(bridge['uid'], test['deps'])
        self.assertIn('$(B)/web/package.json', bridge['inputs'])
        self.assertIn('$(B)/web/package.json', test['inputs'])
        self.assertIn(bridge['outputs'][0], test['inputs'])
        self.assertNotIn('$(B)/web/output.tar', [out for node in graph['graph'] for out in node['outputs']])

    def test_build_and_test_share_one_preparation(self):
        files = self.project('TS_TSC()\nTS_BUILD_OUTPUTS(build)')
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n'
        graph = make(files, 'web/tests', 'web', '-DAUTOCHECK=yes')
        self.assertEqual(sum('$(B)/web/package.json' in node['outputs'] for node in graph['graph']), 1)
        bundle = node_by_output(graph, '$(B)/web/output.tar')
        self.assertEqual(bundle['cmds'][-1]['cmd_args'][-2:], ['--build-command', 'tsc --project tsconfig.json --incremental false --composite false --pretty'])

    def test_legacy_test_resolves_relative_target(self):
        files = self.project()
        files['web/tests/ya.make'] = 'TS_TEST_VITEST_FOR(RELATIVE ../)\nEND()\n'
        graph = make(files, 'web/tests', '-DAUTOCHECK=yes')
        test = node_by_output(graph, '$(B)/web/tests/web-tests.ts_test.fake')
        bundle = node_by_output(graph, '$(B)/web/output.tar')
        self.assertEqual(test['kv']['p'], 'TS_JST')
        self.assertIn(bundle['uid'], test['deps'])

    def test_legacy_test_inputs_follow_relative_imports(self):
        files = self.project()
        files.update({
            'web/tests/ya.make': 'TS_TEST_VITEST_FOR(RELATIVE ../)\nTS_TEST_SRCS(../src)\nEND()\n',
            'web/vitest.config.ts': "export {default} from './config.js';\n",
            'web/config.ts': "import './data.json';\n",
            'web/data.json': '{}',
            'web/src/unit.test.ts': """import type {Value} from './value';
import * as directory from './directory';
const cjs = require('./common');
const dynamic = import('./dynamic.js');
export {thing as other} from './exported';
import './cycle';
// nots-ignore-import
import './ignored';
import 'external-package';
// import './comment';
/* require('./comment'); */
const literal = "require('./comment')";
const template = `import './comment'`;
object.require('./comment');
const myrequire = './comment';
""",
            'web/src/value.ts': 'export type Value = string;',
            'web/src/value.js': 'wrong extension priority',
            'web/src/directory/index.tsx': 'export default 1;',
            'web/src/common.d.ts': 'declare const cjs: number;',
            'web/src/dynamic.tsx': 'export default 1;',
            'web/src/exported.js': 'export const thing = 1;',
            'web/src/cycle.ts': "import './unit.test';",
            'web/src/ignored.ts': 'ignored',
            'web/src/comment.ts': 'ignored',
            'web/src/__mocks__/mock.js': 'export default 1;',
            'web/src/unused.ts': 'ignored',
        })
        graph = make(files, 'web/tests', '-DAUTOCHECK=yes', '-t')
        test = node_by_output(graph, '$(B)/web/tests/web-tests.ts_test.fake')
        for filename in ['vitest.config.ts', 'config.ts', 'data.json',
                         'src/unit.test.ts', 'src/value.ts', 'src/directory/index.tsx',
                         'src/common.d.ts', 'src/dynamic.tsx', 'src/exported.js',
                         'src/cycle.ts', 'src/__mocks__/mock.js']:
            self.assertEqual(test['inputs'].count(f'$(S)/web/{filename}'), 1, filename)
        for filename in ['src/value.js', 'src/ignored.ts', 'src/comment.ts', 'src/unused.ts']:
            self.assertNotIn(f'$(S)/web/{filename}', test['inputs'])

    def test_sandbox_inputs_contain_workspace_closure_once(self):
        files = self.project()
        for name, peers in [('web', ['left', 'right']), ('left', ['leaf']), ('right', ['leaf']), ('leaf', [])]:
            files[f'{name}/ya.make'] = 'TS_PACKAGE()\nEND()\n'
            files[f'{name}/package.json'] = json.dumps({
                'dependencies': {peer: f'workspace:../{peer}' for peer in peers},
            })
            files[f'{name}/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
        graph = make(files, 'web', '-DAUTOCHECK=yes')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        bundle = node_by_output(graph, '$(B)/web/output.tar')
        for node in [prepare, bundle]:
            self.assertIn('$(B)/devtools/frontend_build_platform/nots/builder/builder', node['inputs'])
            for peer in ['left', 'right', 'leaf']:
                for output in ['package.json', 'output.tar']:
                    filename = f'$(B)/{peer}/{output}'
                    self.assertEqual(node['inputs'].count(filename), 1)
                    self.assertEqual(node['deps'].count(node_by_output(graph, filename)['uid']), 1)
            self.assertEqual(len(node['deps']), len(set(node['deps'])))
        self.assertIn('$(B)/web/package.json', bundle['inputs'])

    def test_legacy_prepare_still_depends_on_direct_workspace_manifest(self):
        files = self.project()
        files['web/package.json'] = '{"dependencies":{"peer":"workspace:../peer"}}'
        files['peer/ya.make'] = 'TS_PACKAGE()\nEND()\n'
        files['peer/package.json'] = '{}'
        graph = make(files, 'web')
        prepare = node_by_output(graph, '$(B)/web/package.json')
        peer = node_by_output(graph, '$(B)/peer/pnpm-workspace.yaml')
        self.assertIn(peer['uid'], prepare['deps'])
        self.assertIn('$(B)/peer/pnpm-workspace.yaml', prepare['inputs'])
        self.assertNotIn('$(B)/peer/output.tar', prepare['inputs'])

    def test_proto_workspace_selects_typescript_build_and_descriptor_peers(self):
        files = self.project()
        files['web/package.json'] = '{"dependencies":{"api":"workspace:../api"}}'
        files['api/ya.make'] = (
            'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\n'
            'SRCS(api.proto)\nPEERDIR(raw)\nTS_PROTO_OPT(outputJsonMethods=false)\n'
            'IF(TS_PROTO)\nTS_PROTO_OPT(useOptionals=all)\nENDIF()\nEND()\n'
        )
        files['api/package.json'] = '{}'
        files['api/tsconfig.json'] = '{}'
        files['api/api.proto'] = 'syntax = "proto3";\nimport "raw/value.proto";\n'
        files['raw/ya.make'] = 'PROTO_LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nNO_LIBC()\nSRCS(value.proto)\nEND()\n'
        files['raw/value.proto'] = 'syntax = "proto3";\nmessage Value {}\n'
        tool_program(files, 'contrib/tools/protoc', 'protoc')
        graph = make(files, 'web', '-DAUTOCHECK=yes')
        api = node_by_output(graph, '$(B)/api/output.tar')
        self.assertEqual(api['kv']['p'], 'TS_PRO')
        args = api['cmds'][-1]['cmd_args']
        self.assertIn('build-ts-proto', args)
        self.assertEqual(args[args.index('--tsconfigs') + 1:args.index('--protoc-bin')], [
            'tsconfig.json', '--ts-proto-opt', 'outputJsonMethods=false', '--ts-proto-opt', 'useOptionals=all',
        ])
        self.assertIn('$(S)/raw/value.proto', api['inputs'])
        self.assertIn('$(B)/raw/raw.self.protodesc', api['inputs'])
        self.assertNotIn('$(S)/api/package.json', api['inputs'])
        bundle = node_by_output(graph, '$(B)/web/output.tar')
        self.assertIn('$(B)/api/output.tar', bundle['inputs'])
        self.assertIn('$(B)/raw/raw.self.protodesc', bundle['inputs'])
        self.assertFalse(any(out.endswith('.pb.cc') for n in graph['graph'] for out in n['outputs']))

        files['api/ya.make'] = files['api/ya.make'].replace('SRCS(api.proto)', 'TS_PROTO_NON_RECURSIVE()\nSRCS(api.proto)')
        graph = make(files, 'web', '-DAUTOCHECK=yes')
        prepare = node_by_output(graph, '$(B)/api/package.json')
        self.assertNotIn('$(B)/raw/raw.self.protodesc', prepare['inputs'])
        self.assertFalse(any('$(B)/raw/package.json' in n['outputs'] for n in graph['graph']))

    def test_proto_without_manifest_uses_auto_dependency_package(self):
        files = self.project()
        files['web/package.json'] = '{"dependencies":{"api":"workspace:../api"}}'
        files['api/ya.make'] = (
            'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\n'
            'SRCS(api.proto)\nTS_PROTO_PACKAGE_NAME(@scope/api)\nEND()\n'
        )
        files['api/api.proto'] = 'syntax = "proto3";\nmessage Api {}\n'
        deps = 'library/typescript/ts-proto-deps'
        files[f'{deps}/ya.make'] = 'TS_PACKAGE()\nEND()\n'
        files[f'{deps}/package.json'] = files['web/package.json'].replace('api', 'dep').replace('workspace:../dep', '1.2.3')
        files[f'{deps}/pnpm-lock.yaml'] = files['web/pnpm-lock.yaml']
        tool_program(files, 'contrib/tools/protoc', 'protoc')
        graph = make(files, 'web', '-DAUTOCHECK=yes')
        prepare = node_by_output(graph, '$(B)/api/package.json')
        self.assertEqual(prepare['cmds'][0]['cmd_args'][-4:], [
            '--ts-proto-auto-deps-path', deps, '--ts-proto-auto-package-name', '@scope/api',
        ])
        self.assertNotIn('$(S)/api/package.json', prepare['inputs'])
        self.assertIn(f'$(B)/{deps}/output.tar', prepare['inputs'])
        self.assertFalse(any(out.endswith('.tgz') for out in prepare['outputs']))
        self.assertEqual(prepare['resources'], node_by_output(graph, f'$(B)/{deps}/package.json')['resources'])
        api = node_by_output(graph, '$(B)/api/output.tar')
        self.assertIn('$(B)/api/node_modules.layer', api['inputs'])
        args = api['cmds'][-1]['cmd_args']
        self.assertEqual(args[args.index('--auto-package-name'):args.index('--protoc-bin')], [
            '--auto-package-name', '@scope/api', '--auto-deps-path', deps,
        ])

    def test_proto_requires_enabled_preparation_variant(self):
        files = self.project()
        files['web/package.json'] = '{"dependencies":{"api":"workspace:../api"}}'
        files['api/ya.make'] = 'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO)\nEND()\n'
        files['api/package.json'] = '{}'
        result = make_process(files, 'web')
        self.assertEqual(result.returncode, 1)
        self.assertIn('TS_PREPARE_DEPS', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main()
