import unittest

from tst import lib
from tst.make.test_python_cython import python_base


MODULE = 'grove/module'
SOURCE = 'grove/sources'


def fixture(body, version=3, declaration='', source='stem.pyx'):
    files = python_base()
    files[MODULE + '/ya.make'] = ('PY' + str(version) + '_LIBRARY()\n' + lib.BARE_MODULE
                                 + 'NO_PYTHON_INCLUDES()\n' + declaration + body + '\nEND()\n')
    files[SOURCE + '/' + source] = 'cdef int value\n' if source.endswith('.pyx') else 'value = 1\n'
    for name, value in list(files.items()):
        if name.startswith('contrib/tools/cython/'):
            files[name.replace('contrib/tools/cython/', 'contrib/tools/cython_py2/')] = value
    lib.tool_program(files, 'tools/py2cc', 'py2cc')
    return files


def cython_node(graph):
    nodes = [n for n in graph['graph'] if n['kv'].get('p') == 'CY']
    assert len(nodes) == 1
    return nodes[0]


class CythonInitialPathTest(unittest.TestCase):
    def check(self, files, expected, extras=()):
        graph = lib.make(files, MODULE)
        node = cython_node(graph)
        args = node['cmds'][0]['cmd_args']
        self.assertEqual([a for a in args if a.startswith('set_initial_path=')],
                         ['set_initial_path=' + expected, *extras])
        self.assertTrue(graph['result'])
        consumers = [n for n in graph['graph'] if n['kv'].get('p') == 'CC' and node['outputs'][0] in n['inputs']]
        self.assertTrue(consumers)
        cc = consumers[0]
        self.assertIn(node['uid'], cc['deps'])
        self.assertIn(node['outputs'][0], cc['inputs'])
        return node

    def test_srcdir_resolves_automatic_path_for_both_python_versions(self):
        for version in (2, 3):
            for declaration, tail in (('SRCDIR(' + SOURCE + ')\n', ''),
                                      ('', '\nSRCDIR(' + SOURCE + ')')):
                files = fixture('PY_SRCS(NAMESPACE fern stem.pyx)' + tail, version, declaration)
                node = self.check(files, SOURCE + '/stem.pyx')
                self.assertIn('$(S)/' + SOURCE + '/stem.pyx', node['inputs'])
                self.assertIn('$(S)/' + SOURCE + '/stem.pyx', node['cmds'][0]['cmd_args'])

    def test_automatic_path_uses_native_module_then_srcdir_search_order(self):
        files = fixture('PY_SRCS(stem.pyx)', declaration='SRCDIR(grove/first ' + SOURCE + ')\n')
        files['grove/first/stem.pyx'] = 'cdef int first\n'
        self.check(files, 'grove/first/stem.pyx')
        files[MODULE + '/stem.pyx'] = 'cdef int local\n'
        self.check(files, MODULE + '/stem.pyx')

    def test_local_and_rooted_sources_keep_physical_paths(self):
        for source, expected in (('stem.pyx', MODULE + '/stem.pyx'),
                                 (SOURCE + '/stem.pyx=fern.stem', SOURCE + '/stem.pyx'),
                                 ('${ARCADIA_ROOT}/' + SOURCE + '/stem.pyx=fern.stem', SOURCE + '/stem.pyx')):
            files = fixture('PY_SRCS(' + source + ')')
            if source == 'stem.pyx':
                files[MODULE + '/stem.pyx'] = files.pop(SOURCE + '/stem.pyx')
            self.check(files, expected)

    def test_cythonized_python_and_user_override_keep_directive_order(self):
        for source, mode in (('stem.pyx', ''), ('stem.py', 'CYTHONIZE_PY ')):
            files = fixture('PY_SRCS(' + mode + 'NAMESPACE fern ' + source
                            + ' CYTHON_DIRECTIVE set_initial_path=custom.py)',
                            declaration='SRCDIR(' + SOURCE + ')\n', source=source)
            node = self.check(files, SOURCE + '/' + source, ('set_initial_path=custom.py',))
            args = node['cmds'][0]['cmd_args']
            start = args.index('--source-root')
            self.assertEqual(args[start:start + 6], ['--source-root', '$(S)', '-X',
                                                    'set_initial_path=' + SOURCE + '/' + source, '-X', 'set_initial_path=custom.py'])

    def test_generated_source_keeps_unrooted_logical_path(self):
        files = fixture('PY_SRCS(grown.pyx)', declaration='COPY_FILE(seed.pyx grown.pyx)\n')
        files[MODULE + '/seed.pyx'] = files.pop(SOURCE + '/stem.pyx')
        self.check(files, 'grown.pyx')

    def test_standalone_cython_keeps_only_explicit_options(self):
        files = fixture('BUILDWITH_CYTHON_CPP(stem.pyx -X set_initial_path=explicit.py)',
                        declaration='SRCDIR(' + SOURCE + ')\n')
        node = cython_node(lib.make(files, MODULE))
        self.assertEqual([a for a in node['cmds'][0]['cmd_args'] if a.startswith('set_initial_path=')],
                         ['set_initial_path=explicit.py'])


if __name__ == '__main__':
    unittest.main()
