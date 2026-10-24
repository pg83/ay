"""Style resources are macro side effects, independent of suite file eligibility."""
import unittest
from collections import Counter

import lib
from test_python_srcs import python_base
from test_python2 import python2_base

def fixture(kind='LIBRARY()',eligible=False,control='',implicit=False):
 style='STYLE_JSON()\nSTYLE_YAML()\n'
 body='RUN(true)\n' if kind=='EXECTEST()' else ''
 if kind=='LIBRARY()':body='SRCS(leaf.cpp)\n'
 files={'orchard/ya.make':kind+'\n'+lib.BARE_MODULE+control+'\n'+('' if implicit else style)+body+'END()\n','orchard/leaf.cpp':'int cedar;\n','orchard/readme.txt':'plain text\n',
 'checks/ya.make':'PY3_PROGRAM_BIN(cedar_check)\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\nPY_SRCS(check.py)\nPEERDIR(orchard)\nEND()\n','checks/check.py':'value=1\n',
 'build/config/tests/json_style/default_configs.json':'{"clang_format_json":"lint/json.conf"}',
 'build/config/tests/yaml_style/default_configs.json':'{"yamlfmt":"lint/yaml.conf"}',
 'lint/json.conf':'{}\n','lint/yaml.conf':'{}\n',
 'tools/clang_format_json_linter/wrapper.py':'','tools/yamlfmt_linter/wrapper.py':'',
 'build/platform/clang/clang-format/ya.make':lib.resource_lib('CLANG_FORMAT',41),
 'build/external_resources/yamlfmt/ya.make':lib.resource_lib('YAMLFMT',42)}
 files={**python_base(),**files}
 if eligible:files.update({'orchard/cedar.json':'{}\n','orchard/cedar.yaml':'name: cedar\n'})
 if implicit:files.update({'build/conf/autoincludes.json':'["orchard"]','orchard/linters.make.inc':'USRV_STYLE_JSON_CURRENT_DIRECTORY()\nUSRV_STYLE_YAML_CURRENT_DIRECTORY()\n'})
 return files

def black_fixture(kind='SANDBOX_PY23_TASK(cedar)',selection='',has_files=True):
 files=python_base();files.update(python2_base())
 files['sapling/ya.make']=kind+'\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\n'+selection+'\nSTYLE_PYTHON()\n'+('PY_SRCS(entry.py)\n' if has_files else '')+'END()\n'
 files['sapling/entry.py']='value=1\n'
 files['tools/black_linter/wrapper.py']=''
 files['tools/flake8_linter/__main__.py']=''
 files['build/external_resources/black/ya.make']=lib.resource_lib('BLACK',43)
 files['contrib/libs/python/Include/.keep']='';files['contrib/tools/python/src/Include/.keep']=''
 for peer in ['sandbox/sdk2','sandbox/bin','sandbox/taskbox/worker']:
  files[peer+'/ya.make']='PY23_LIBRARY()\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\nPY_SRCS(side.py)\nEND()\n';files[peer+'/side.py']='value=2\n'
 return files


def resource_keys(node):
    args = node['cmds'][0]['cmd_args']
    return [args[i + 1].split('::', 1)[0] for i, arg in enumerate(args[:-1]) if arg == '--global-resource']


def make(files, *targets):
    graph = lib.make(files, *targets, '-t')
    ids = {node['uid'] for node in graph['graph']}
    assert graph['result']
    assert set(graph['result']) <= ids
    assert all(set(node['deps']) <= ids for node in graph['graph'])
    return graph


class StyleResourcePublicationTest(unittest.TestCase):
    def assert_style_globals(self, graph, expected):
        node = lib.node_by_output(graph, '$(B)/checks/test-results/import_test/meta.json')
        keys = [key for key in resource_keys(node) if key in ('CLANG_FORMAT_RESOURCE_GLOBAL', 'YAMLFMT_RESOURCE_GLOBAL')]
        self.assertEqual(keys, expected)
        self.assertEqual(len(keys), len(set(keys)))

    def test_empty_explicit_implicit_and_repeated_scopes_publish_peers(self):
        for implicit in (False, True):
            files = fixture(implicit=implicit)
            with self.subTest(implicit=implicit):
                graph = make(files, 'checks')
                self.assert_style_globals(graph, ['CLANG_FORMAT_RESOURCE_GLOBAL', 'YAMLFMT_RESOURCE_GLOBAL'])
                self.assertFalse(any('/orchard/test-results/' in out for n in graph['graph'] for out in n['outputs']))
        files = fixture()
        files['orchard/ya.make'] = files['orchard/ya.make'].replace('END()', 'STYLE_YAML()\nSTYLE_JSON()\nEND()')
        self.assert_style_globals(make(files, 'checks'), ['CLANG_FORMAT_RESOURCE_GLOBAL', 'YAMLFMT_RESOURCE_GLOBAL'])

    def test_suppression_prevents_style_peer_publication(self):
        for control in ('NO_LINT()', 'SET(_NO_LINT_VALUE none_internal)', 'SET(TIDY yes)'):
            with self.subTest(control=control):
                self.assert_style_globals(make(fixture(eligible=True, control=control), 'checks'), [])

    def test_yaml_config_publishes_without_files_and_respects_suppression(self):
        for control, expected in (('', ['YAMLFMT_RESOURCE_GLOBAL']), ('NO_LINT()', []),
                                  ('SET(_NO_LINT_VALUE none_internal)', []), ('SET(TIDY yes)', [])):
            with self.subTest(control=control):
                files = fixture(control=control)
                files['orchard/ya.make'] = files['orchard/ya.make'].replace('STYLE_JSON()\nSTYLE_YAML()\n', 'STYLE_YAML_WITH_CONFIG()\n')
                self.assert_style_globals(make(files, 'checks'), expected)

    def test_union_and_ci_group_emit_the_same_style_contract_as_gen_library(self):
        for kind in ('UNION()', 'CI_GROUP()', 'GEN_LIBRARY()'):
            with self.subTest(kind=kind):
                files = fixture(kind=kind, eligible=True)
                graph = make(files, 'orchard')
                for name, filename, key in (('clang_format_json', 'cedar.json', 'CLANG_FORMAT_RESOURCE_GLOBAL'),
                                            ('yamlfmt', 'cedar.yaml', 'YAMLFMT_RESOURCE_GLOBAL')):
                    suite = lib.node_by_output(graph, '$(B)/orchard/test-results/' + name + '/meta.json')
                    source = '$(S)/orchard/' + filename
                    self.assertIn(source, suite['inputs'])
                    self.assertIn(source, suite['cmds'][0]['cmd_args'])
                    self.assertEqual(Counter(resource_keys(suite)), Counter({key: 2}))
        files['orchard/ya.make'] = files['orchard/ya.make'].replace('END()', 'STYLE_JSON()\nSTYLE_YAML()\nEND()')
        graph = make(files, 'orchard')
        for name in ('clang_format_json', 'yamlfmt'):
            self.assertEqual(sum('$(B)/orchard/test-results/' + name + '/meta.json' in node['outputs'] for node in graph['graph']), 1)

    def test_sandbox_python2_binary_publishes_black_but_library_does_not(self):
        for kind in ('SANDBOX_TASK(cedar)', 'SANDBOX_PY23_TASK(cedar)'):
            with self.subTest(kind=kind):
                graph = make(black_fixture(kind, 'ONLY_TAGS(SB_TASK_BIN)'), 'sapling')
                suite = lib.node_by_output(graph, '$(B)/sapling/test-results/import_test/meta.json')
                self.assertEqual(resource_keys(suite).count('BLACK_RESOURCE_GLOBAL'), 1)
                black = lib.node_by_output(graph, '$(B)/sapling/test-results/black/meta.json')
                self.assertIn('$(S)/sapling/entry.py', black['inputs'])
                self.assertEqual(resource_keys(black).count('BLACK_RESOURCE_GLOBAL'), 2)
        graph = make(black_fixture(selection='ONLY_TAGS(PY2)'), 'sapling')
        self.assertFalse(any('$(B)/sapling/test-results/black/meta.json' in node['outputs'] for node in graph['graph']))
        self.assertFalse(any('BLACK_RESOURCE_GLOBAL' in resource_keys(node) for node in graph['graph']))

    def test_black_peer_is_independent_of_python_files_and_can_be_disabled(self):
        graph = make(black_fixture('PY3_PROGRAM_BIN(cedar)', has_files=False), 'sapling')
        suite = lib.node_by_output(graph, '$(B)/sapling/test-results/import_test/meta.json')
        self.assertEqual(resource_keys(suite).count('BLACK_RESOURCE_GLOBAL'), 1)
        self.assertFalse(any('$(B)/sapling/test-results/black/meta.json' in node['outputs'] for node in graph['graph']))
        files = black_fixture(selection='NO_LINT()')
        files = {(name.replace('sapling/', 'junk/stylepub/', 1) if name.startswith('sapling/') else name): value for name, value in files.items()}
        files['junk/linters.make.inc'] = '# fixture linters\n'
        graph = make(files, 'junk/stylepub')
        suite = lib.node_by_output(graph, '$(B)/junk/stylepub/test-results/import_test/meta.json')
        self.assertNotIn('BLACK_RESOURCE_GLOBAL', resource_keys(suite))
        self.assertFalse(any('$(B)/junk/stylepub/test-results/black/meta.json' in node['outputs'] for node in graph['graph']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
