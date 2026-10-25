import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_python2 import python2_base
from tst.make.test_yql import udf_fixture


MAIN_DIRS = ('cedar/configured', 'cedar/entry', 'yql/essentials/udfs/common/python')


def files(case):
    sources = proto_world()
    sources.update(python2_base())
    sources.update(udf_fixture())
    if case != 'no_config':
        sources['build/internal/conf/internal.conf'] = '@import "${CONF_ROOT}/internal/conf/settings.conf"\n'
        sources['build/internal/conf/settings.conf'] = 'YQL_PYTHON_DIR=cedar/configured\n'
    body = ''
    if case == 'override':
        body += 'SET(YQL_PYTHON_DIR cedar/entry)\n'
    if case == 'empty':
        body += 'SET(YQL_PYTHON_DIR "")\n'
    if case == 'system':
        body += 'SET(USE_ARCADIA_PYTHON no)\n'
    if case != 'no_register':
        body += 'REGISTER_YQL_PYTHON_UDF()\n'
    if case == 'python_sources':
        body += 'PY_SRCS(helper.py)\n'
    kind = 'YQL_PYTHON3_UDF' if case == 'python3' else 'YQL_PYTHON_UDF'
    sources['grove/ya.make'] = (kind + '(cedar)\n' + lib.BARE_MODULE
                              + 'UDF_NO_PROBE()\nDISABLE(PYTHON_SQLITE3)\n'
                              + body + 'SRCS(body.cpp)\nEND()\n')
    sources['grove/body.cpp'] = 'int value;\n'
    sources['grove/helper.py'] = 'value=1\n'
    for peer in ('library/python/runtime', 'library/python/runtime/main'):
        sources[peer + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(entry.c)\nEND()\n'
        sources[peer + '/entry.c'] = 'int entry;\n'
    for directory in MAIN_DIRS:
        peer = directory + '/main'
        sources[peer + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(GLOBAL entry.c)\nEND()\n'
        sources[peer + '/entry.c'] = 'int entry;\n'
    return sources


class YqlPythonMainConfigTest(unittest.TestCase):
    def graph(self, case, *flags):
        graph = lib.make(files(case), 'grove', '--sandboxing', '--target-platform', 'default-linux-x86_64', *flags)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
        return graph

    def check_main(self, graph, directory):
        owner = directory + '/main'
        cc = lib.node_by_output(graph, '$(B)/' + owner + '/entry.c.pic.o')
        self.assertEqual(cc['inputs'], ['$(S)/' + owner + '/entry.c'])
        archive = next(node for node in graph['graph']
                       if node['outputs'][0].startswith('$(B)/' + owner + '/')
                       and node['outputs'][0].endswith('.global.a'))
        self.assertIn(cc['uid'], archive['deps'])
        self.assertIn(cc['outputs'][0], archive['inputs'])
        link = lib.node_by_output(graph, '$(B)/grove/libcedar.so')
        self.assertIn(archive['uid'], link['deps'])
        self.assertIn(archive['outputs'][0], link['inputs'])
        args = next(command['cmd_args'] for command in link['cmds']
                    if '$(S)/build/scripts/link_dyn_lib.py' in command['cmd_args'])
        self.assertIn(archive['outputs'][0][5:], args)
        for other in MAIN_DIRS:
            if other != directory:
                self.assertFalse(any(output.startswith('$(B)/' + other + '/main/')
                                     for node in graph['graph'] for output in node['outputs']))

    def test_internal_configuration_selects_its_python2_entrypoint(self):
        for flags, expected in (([], 'cedar/configured'), (['-DYQL_PYTHON_DIR=cedar/entry'], 'cedar/entry')):
            with self.subTest(flags=flags):
                self.check_main(self.graph('internal', *flags), expected)

    def test_source_override_and_empty_configuration_keep_native_fallback(self):
        for case, directory in (('override', 'cedar/entry'), ('empty', 'yql/essentials/udfs/common/python'),
                                ('no_config', 'yql/essentials/udfs/common/python')):
            with self.subTest(case=case):
                self.check_main(self.graph(case), directory)

    def test_python_sources_preserve_the_entrypoint_and_resource_producers(self):
        graph = self.graph('python_sources')
        self.check_main(graph, 'cedar/configured')
        self.assertTrue(any('$(S)/grove/helper.py' in node['inputs'] for node in graph['graph']))
        self.assertTrue(any(node['kv']['p'] == 'PY' for node in graph['graph']))

    def test_only_registered_arcadia_python2_uses_configured_main(self):
        for case in ('no_register', 'system', 'python3'):
            with self.subTest(case=case):
                graph = self.graph(case)
                for directory in MAIN_DIRS:
                    self.assertFalse(any(output.startswith('$(B)/' + directory + '/main/')
                                         for node in graph['graph'] for output in node['outputs']))
                if case == 'python3':
                    self.assertTrue(any(output.startswith('$(B)/yql/essentials/udfs/common/python/main_py3/')
                                        for node in graph['graph'] for output in node['outputs']))


if __name__ == '__main__':
    unittest.main()
