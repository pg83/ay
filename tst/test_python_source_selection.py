import json
import unittest

import lib
from test_python_srcs import python_base, objcopy_nodes, kvs
from test_python_suites import fixture as suite_files


MODULE = 'grove/module'
FIRST = 'grove/first'
SECOND = 'grove/second'


def fixture(version=3, local=True, dirs=(FIRST, SECOND), late=False):
    files = python_base()
    files.update(suite_files())
    configs = json.loads(files['build/config/tests/py_style/default_configs.json'])
    configs['py2_flake8'] = configs['flake8']
    files['build/config/tests/py_style/default_configs.json'] = json.dumps(configs)
    files['build/external_resources/flake8_py2/ya.make'] = ('RESOURCES_LIBRARY()\n'
        'DECLARE_EXTERNAL_RESOURCE(FLAKE8_PY2 sbr:123)\nEND()\n')
    lib.tool_program(files, 'tools/py2cc', 'py2cc')
    files[MODULE + '/anchor.cpp'] = 'int anchor;\n'
    files[FIRST + '/__init__.py'] = 'first = 1\n'
    files[SECOND + '/__init__.py'] = 'second = 1\n'
    files[SECOND + '/leaf.py'] = 'leaf = 1\n'
    if local:
        files[MODULE + '/__init__.py'] = 'local = 1\n'
    declaration = 'SRCDIR(' + ' '.join(dirs) + ')\n'
    body = 'PY_SRCS(NAMESPACE fern __init__.py leaf.py)\n'
    files[MODULE + '/ya.make'] = ('PY' + str(version) + '_LIBRARY()\n' + lib.BARE_MODULE
                                 + 'NO_PYTHON_INCLUDES()\nSRCS(anchor.cpp)\n'
                                 + (body + declaration if late else declaration + body) + 'END()\n')
    return files


class PythonSourceSelectionTest(unittest.TestCase):
    def check(self, files, expected, version=3):
        graph = lib.make(files, MODULE, '-t')
        extension = '.yapyc' if version == 2 else '.yapyc3'
        compiled = lib.node_by_output(graph, '$(B)/' + MODULE + '/__init__.py' + extension)
        source = '$(S)/' + expected
        self.assertIn(source, compiled['inputs'])
        self.assertEqual(compiled['cmds'][0]['cmd_args'][-3:],
                         [expected + '-', source, compiled['outputs'][0]])
        objects = objcopy_nodes(graph, MODULE)
        self.assertTrue(any(source in n['inputs'] for n in objects))
        packed = [n for n in objects if compiled['outputs'][0] in n['inputs']]
        self.assertTrue(packed)
        self.assertTrue(all(compiled['uid'] in n['deps'] for n in packed))
        styles = [n for n in graph['graph'] if '/test-results/flake8/' in n['outputs'][0]
                  or '/test-results/py2_flake8/' in n['outputs'][0]]
        self.assertEqual(len(styles), 1)
        self.assertIn(source, styles[0]['inputs'])
        self.assertIn(source, styles[0]['cmds'][0]['cmd_args'])
        self.assertTrue(graph['result'])
        return graph

    def test_module_source_wins_over_srcdir_for_both_python_versions(self):
        for version in (2, 3):
            self.check(fixture(version), MODULE + '/__init__.py', version)

    def test_srcdirs_keep_declaration_order_when_module_source_is_absent(self):
        for dirs, expected in (((FIRST, SECOND), FIRST), ((SECOND, FIRST), SECOND)):
            self.check(fixture(local=False, dirs=dirs), expected + '/__init__.py')

    def test_late_srcdir_uses_the_same_module_source_fact(self):
        graph = self.check(fixture(late=True), MODULE + '/__init__.py')
        namespaces = [kv for node in objcopy_nodes(graph, MODULE) for kv in kvs(node)
                      if kv.startswith('py/namespace/')]
        self.assertTrue(any('/' + MODULE + '=fern.' in x for x in namespaces))
        self.assertTrue(any('/' + SECOND + '=fern.' in x for x in namespaces))

    def test_selection_is_generic_for_non_init_python_files(self):
        files = fixture()
        files[MODULE + '/leaf.py'] = 'own_leaf = 1\n'
        graph = self.check(files, MODULE + '/__init__.py')
        node = lib.node_by_output(graph, '$(B)/' + MODULE + '/leaf.py.yapyc3')
        self.assertIn('$(S)/' + MODULE + '/leaf.py', node['inputs'])
        self.assertNotIn('$(S)/' + SECOND + '/leaf.py', node['inputs'])

    def test_root_relative_source_keeps_existing_binding(self):
        files = fixture(local=False)
        del files[FIRST + '/__init__.py']
        del files[SECOND + '/__init__.py']
        files['__init__.py'] = 'root = 1\n'
        self.check(files, '__init__.py')

    def test_generated_python_source_keeps_registry_producer(self):
        files = fixture()
        files[MODULE + '/template.py'] = 'generated = 1\n'
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace(
            'PY_SRCS(', 'COPY_FILE(template.py grown.py)\nPY_SRCS(grown.py)\nPY_SRCS(', 1)
        graph = self.check(files, MODULE + '/__init__.py')
        copied = lib.node_by_output(graph, '$(B)/' + MODULE + '/grown.py')
        bytecode = lib.node_by_output(graph, '$(B)/' + MODULE + '/grown.py.yapyc3')
        self.assertIn(copied['uid'], bytecode['deps'])
        self.assertIn(copied['outputs'][0], bytecode['inputs'])


if __name__ == '__main__':
    unittest.main()
