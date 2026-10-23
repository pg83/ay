"""TS tested builds belong to execution dependencies, independently of dummy inputs."""
import base64
import json
import unittest

import lib
import test_nots
from test_java_tests import option_values, run_args


SCRIPTS = ('browse', 'clock', 'publish', 'copy', 'expand', 'share')


def fixture(enabled=True):
    files = {('orchard/ui/' + k[4:] if k.startswith('web/') else k): v
             for k, v in test_nots.NotsTests().project('TS_NEXT()').items()}
    files['orchard/ui/package.json'] = json.dumps({'name': 'orchard', 'dependencies': {'shared': 'workspace:../shared'}})
    files['orchard/shared/ya.make'] = 'TS_PACKAGE()\nEND()\n'
    files['orchard/shared/package.json'] = '{}'
    files['orchard/shared/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
    files['policy/scripts.inc'] = ''.join(f'TS_TEST({name} custom --{name})\n' for name in SCRIPTS)
    files['orchard/checks/ya.make'] = (
        'TS_TEST_FOR(orchard/ui)\nINCLUDE(${ARCADIA_ROOT}/policy/scripts.inc)\n'
        + ('TS_TEST_DEPENDS_ON_BUILD()\n' if enabled else '')
        + 'DATA(arcadia/fixtures/seed.txt)\nENV(CHECK_MODE=orchard)\nEND()\n')
    files['fixtures/seed.txt'] = 'declared data\n'
    return files


def make(files, *roots, tests=True):
    roots = roots or ('orchard/checks',)
    return lib.make(files, roots[-1], *roots[:-1], *(['--target-platform-tests'] if tests else []), '-DAUTOCHECK=yes')


def suite(graph, script, project='orchard/checks', kind='test'):
    return lib.node_by_output(graph, f'$(B)/{project}/test-results/ts_{kind}_{script}/meta.json')


class NotsBuildDependencyPhaseTest(unittest.TestCase):
    def assert_dummy(self, graph, project='orchard/checks'):
        dummy = lib.node_by_output(graph, '$(B)/' + project + '/' + project.replace('/', '-'))
        prepare = lib.node_by_output(graph, '$(B)/orchard/ui/package.json')
        bridge = lib.node_by_output(graph, f'$(B)/{project}/{project.replace("/", "-")}.test_for.prepare_deps.fake')
        peer_outputs = ('$(B)/orchard/shared/package.json', '$(B)/orchard/shared/output.tar')
        peers = [lib.node_by_output(graph, output) for output in peer_outputs]
        self.assertEqual(set(dummy['deps']), {prepare['uid'], bridge['uid'], *(peer['uid'] for peer in peers)})
        self.assertEqual(set(dummy['inputs']), {
            '$(B)/orchard/ui/package.json', bridge['outputs'][0], '$(S)/build/scripts/touch.py', *peer_outputs})
        self.assertEqual(dummy['cmds'][0]['cmd_args'][-1], dummy['outputs'][0])
        return dummy

    def assert_execution(self, graph, script, project='orchard/checks', enabled=True, kind='test'):
        test = suite(graph, script, project, kind)
        build = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
        self.assertEqual(test['deps'].count(build['uid']), int(enabled))
        self.assertEqual(test['platform'], build['platform'])
        for output in ('$(B)/orchard/ui/package.json', '$(B)/orchard/shared/package.json',
                       '$(B)/orchard/shared/output.tar', '$(B)/devtools/frontend_build_platform/nots/builder/builder'):
            self.assertEqual(test['deps'].count(lib.node_by_output(graph, output)['uid']), 1, output)
        self.assertNotIn('$(B)/orchard/ui/output.tar', test['inputs'])
        self.assertNotIn('$(B)/orchard/ui/package.json', test['inputs'])
        self.assertIn('$(S)/orchard/ui/index.js', test['inputs'])
        args = test['cmds'][-1]['cmd_args']
        self.assertEqual(option_values(run_args(test), '--script-name'), [script])
        self.assertEqual(option_values(run_args(test), '--target_path'), ['orchard/ui'])
        recipes = base64.b64decode(option_values(args, '--recipes')[0]).decode()
        self.assertIn('extract-output-tars orchard/ui', recipes)
        if kind == 'test':
            self.assertIn('$(S)/fixtures/seed.txt', test['inputs'])
            self.assertIn('CHECK_MODE=orchard', str(args))
        return test

    def test_six_checks_bind_the_tested_build_only_at_execution(self):
        graph = make(fixture())
        for script in SCRIPTS:
            self.assert_execution(graph, script)
        self.assert_dummy(graph)
        build = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
        self.assertIn('next build', build['cmds'][-1]['cmd_args'])
        self.assertIn('$(S)/orchard/ui/index.js', build['inputs'])
        self.assertIn(lib.node_by_output(graph, '$(B)/orchard/ui/package.json')['uid'], build['deps'])

    def test_disabled_build_and_explicit_peer_boundaries(self):
        files = fixture(False)
        graph = make(files, 'orchard/ui', 'orchard/checks')
        self.assert_dummy(graph)
        for script in SCRIPTS:
            self.assert_execution(graph, script, enabled=False)
        files['orchard/checks/ya.make'] = files['orchard/checks/ya.make'].replace('END()', 'DEPENDS(orchard/ui orchard/ui)\nEND()')
        explicit = make(files)
        self.assert_dummy(explicit)
        for script in SCRIPTS:
            self.assert_execution(explicit, script)

    def test_build_flag_does_not_add_a_dummy_obligation_without_execution(self):
        for tests in (False, True):
            graph = make(fixture(), 'orchard/ui', 'orchard/checks', tests=tests)
            self.assert_dummy(graph)
            self.assertEqual(len([n for n in graph['graph'] if any('/test-results/ts_test_' in o and o.endswith('/meta.json') for o in n['outputs'])]), 6 if tests else 0)

    def test_lint_uses_the_same_custom_build_dependency(self):
        files = fixture()
        files['policy/scripts.inc'] = 'TS_LINT(format custom --format)\n'
        graph = make(files)
        self.assert_execution(graph, 'format', kind='lint')
        self.assert_dummy(graph)

    def test_build_module_checks_do_not_inherit_the_test_for_epilogue(self):
        files = fixture()
        files['orchard/ui/ya.make'] = 'TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_TEST(inline)\nTS_TEST_DEPENDS_ON_BUILD()\nEND()\n'
        graph = make(files, 'orchard/ui')
        test = suite(graph, 'inline', 'orchard/ui')
        build = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
        self.assertNotIn(build['uid'], test['deps'])
        self.assertIn(lib.node_by_output(graph, '$(B)/orchard/ui/package.json')['uid'], test['deps'])

    def test_no_files_or_disabled_execution_does_not_create_an_execution_dependency(self):
        files = fixture()
        del files['orchard/ui/index.js']
        files['orchard/checks/ya.make'] = files['orchard/checks/ya.make'].replace(
            'TS_TEST_FOR(orchard/ui)', 'TS_TEST_FOR(orchard/ui)\nTS_EXCLUDE_FILES_GLOB(ya.make)')
        for tests in (False, True):
            graph = make(files, tests=tests)
            self.assert_dummy(graph)
            outputs = [output for node in graph['graph'] for output in node['outputs']]
            self.assertNotIn('$(B)/orchard/ui/output.tar', outputs)
            self.assertFalse(any('/test-results/ts_test_' in output for output in outputs))

    def test_shared_lexical_include_and_root_order_keep_independent_bindings(self):
        files = fixture()
        files['orchard/other/ya.make'] = files['orchard/checks/ya.make'].replace('TS_TEST_DEPENDS_ON_BUILD()\n', '')
        for roots in (('orchard/ui', 'orchard/checks', 'orchard/other'), ('orchard/other', 'orchard/checks', 'orchard/ui')):
            graph = make(files, *roots)
            for project, enabled in (('orchard/checks', True), ('orchard/other', False)):
                self.assert_dummy(graph, project)
                for script in SCRIPTS:
                    self.assert_execution(graph, script, project, enabled)

    def test_flag_changes_only_execution_edges_when_build_already_selected(self):
        graphs = [make(fixture(enabled), 'orchard/ui', 'orchard/checks') for enabled in (False, True)]
        maps = [{(n.get('platform'), tuple(n['outputs'])): n for n in g['graph']} for g in graphs]
        self.assertEqual(set(maps[0]), set(maps[1]))
        built = lib.node_by_output(graphs[1], '$(B)/orchard/ui/output.tar')
        for key, before in maps[0].items():
            after = maps[1][key]
            if '/meta.json' in before['outputs'][0] and '/ts_test_' in before['outputs'][0]:
                self.assertEqual(set(after['deps']) - set(before['deps']), {built['uid']})
                self.assertEqual(set(before['deps']) - set(after['deps']), set())
                for field in ('inputs', 'cmds', 'requirements', 'kv'):
                    self.assertEqual(before.get(field), after.get(field), (key, field))
            else:
                self.assertEqual(before, after)


if __name__ == '__main__':
    unittest.main(verbosity=2)
