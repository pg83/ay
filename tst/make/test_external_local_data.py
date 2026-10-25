import unittest

from tst import lib
from tst.make.test_test_macro_values import option_values


def fixture(mode):
    if mode == 'python':
        from tst.make.test_no_doctests import fixture as python_fixture
        files, target, flags = python_fixture('ordinary')
        files[target + '/ya.make'] = files[target + '/ya.make'].replace('END()', 'DATA(ext:payload.json)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nEND()')
        files[target + '/payload.json'] = '{}\n'
        return files, target, flags
    body = 'DATA(ext:payload.json)\n'
    files = {'orchard/check/payload.json': '{}\n'}
    if mode == 'duplicate':
        body = 'DATA(ext:payload.json ext:payload.json ext:./payload.json)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\n'
    if mode == 'wins':
        files['orchard/check/payload.json.external'] = 'invalid placeholder is not read when physical data exists\n'
    if mode == 'relative':
        body = 'DATA(ext:../shared/seed.json)\n'
        files['orchard/shared/seed.json'] = files.pop('orchard/check/payload.json')
    if mode == 'ordinary':
        body = 'DATA(arcadia/fixtures/input)\n'
        files['fixtures/input'] = 'data\n'
    if mode == 'mixed':
        body = 'DATA(sbr://42 ext:payload.json arcadia/fixtures/input)\n'
        files['fixtures/input'] = 'data\n'
    files['orchard/check/ya.make'] = 'EXECTEST()\nRUN(true)\n' + body + 'END()\n'
    return files, 'orchard/check', []


class ExternalLocalDataTest(unittest.TestCase):
    def test_physical_external_files_keep_their_copy_producer_and_runtime_flags(self):
        for mode in ('physical', 'duplicate', 'wins', 'relative', 'mixed'):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, '-t', '--sandboxing', *flags)
                rel = 'orchard/shared/seed.json' if mode == 'relative' else 'orchard/check/payload.json'
                output = '$(B)/external_local/' + rel
                copy = lib.node_by_output(graph, output)
                self.assertEqual(copy['inputs'], ['$(S)/' + rel])
                self.assertEqual(copy['outputs'], [output])
                self.assertEqual(copy['deps'], [])
                self.assertEqual(copy['cmds'][0]['cmd_args'], [
                    '$(YMAKE_PYTHON3)/bin/python3', '$(S)/build/scripts/fs_tools.py', 'copy', '$(S)/' + rel, output])
                self.assertFalse(copy['cache'])
                self.assertEqual(copy['kv'], {'p': 'CP', 'pc': 'light-blue'})
                self.assertNotIn(copy['uid'], graph['result'])
                self.assertEqual(sum(output in node['outputs'] for node in graph['graph']), 1)
                runs = [node for node in graph['graph'] if node.get('kv', {}).get('run_test_node')]
                self.assertEqual(len(runs), 2 if mode == 'duplicate' else 1)
                for run in runs:
                    self.assertEqual(option_values(run, '--external-local-file'), [rel] * (2 if mode == 'duplicate' else 1))
                    self.assertIn(copy['uid'], run['deps'])
                    self.assertNotIn(output, run['inputs'])
                    if mode == 'mixed':
                        self.assertEqual(option_values(run, '--sandbox-resource'), ['42'])
                        download = lib.node_by_output(graph, '$(B)/sandbox-storage/42/resource')
                        self.assertIn(download['uid'], run['deps'])

    def test_python_chunks_keep_one_copy_without_changing_source_producers_or_style_suites(self):
        files, target, flags = fixture('python')
        graph = lib.make(files, target, '-t', '--sandboxing', *flags)
        output = '$(B)/external_local/' + target + '/payload.json'
        copy = lib.node_by_output(graph, output)
        self.assertEqual(sum(output in node['outputs'] for node in graph['graph']), 1)
        for index in range(2):
            run = lib.node_by_output(graph, f'$(B)/{target}/test-results/py3test/chunk{index}/meta.json')
            self.assertEqual(option_values(run, '--external-local-file'), [target + '/payload.json'])
            self.assertEqual(run['deps'].count(copy['uid']), 1)
        for suite in ('import_test', 'flake8'):
            run = lib.node_by_output(graph, f'$(B)/{target}/test-results/{suite}/meta.json')
            self.assertEqual(option_values(run, '--external-local-file'), [])
            self.assertNotIn(copy['uid'], run['deps'])
        lib.node_by_output(graph, f'$(B)/{target}/a.py.yapyc3')

    def test_plain_arcadia_data_does_not_create_an_external_copy(self):
        files, target, flags = fixture('ordinary')
        graph = lib.make(files, target, '-t', '--sandboxing', *flags)
        self.assertFalse(any('/external_local/' in output for node in graph['graph'] for output in node['outputs']))
        run = lib.node_by_output(graph, '$(B)/orchard/check/test-results/exectest/meta.json')
        self.assertEqual(option_values(run, '--external-local-file'), [])
        self.assertIn('$(S)/fixtures/input', run['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
