import json
import unittest

from lib import make, node_by_output, tool_program


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


if __name__ == '__main__':
    unittest.main()
