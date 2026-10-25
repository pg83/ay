import base64
import hashlib
import posixpath
import unittest

from tst import lib
from tst.make.test_python2 import python2_base
from tst.make.test_python_srcs import python_base
from tst.make.test_python_source_alias import resource_kvs


MODULE = 'grove/bench/lib'
SOURCE = 'grove/bench/cases/017/check.py'


def suffix():
    return '.' + base64.b32encode(hashlib.md5(('$S/' + MODULE).encode()).digest()).decode().lower()[:4]


def fixture(mode='parent', version=3):
    files = python_base() if version == 3 else python2_base()
    aliases = {
        'parent': '../cases/017/check.py=public.case ../../../shared/entry.py=public.transport',
        'rooted': '${ARCADIA_ROOT}/' + SOURCE + '=public.case ${ARCADIA_ROOT}/shared/entry.py=public.transport',
        'local': 'plain.py=public.direct ./plain.py=public.relative',
        'ownership': '../cases/017/check.py=public.case',
        'generated': '${ARCADIA_BUILD_ROOT}/' + MODULE + '/pkg/result.py=public.generated',
        'none': 'plain.py=public.direct',
    }[mode]
    files[MODULE + '/ya.make'] = f'PY{version}_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nPY_SRCS(' + aliases + ')\nEND()\n'
    if mode == 'rooted':
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace('PY_SRCS(', 'ENABLE(PYBUILD_NO_PYC)\nPY_SRCS(')
    files[SOURCE] = '# A Python source may also be a C++ include.\n'
    files['shared/entry.py'] = ''
    files[MODULE + '/plain.py'] = ''
    if mode == 'ownership':
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace('END()', 'SRCS(marker.cpp)\nEND()')
        files[MODULE + '/marker.cpp'] = '#include "../cases/017/check.py"\n'
        compiler = 'tools/py3cc' if version == 3 else 'tools/py2cc'
        files[compiler + '/main.cpp'] = '#include "tool_config.py"\n'
        files[compiler + '/tool_config.py'] = '# C++ input owned by the compiler tool.\n'
    if mode == 'generated':
        lib.tool_program(files, 'orchard/carver', 'carver')
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace('END()', 'RUN_PROGRAM(orchard/carver IN seed.txt OUT_NOAUTO manifest.txt pkg/result.py)\nEND()')
        files[MODULE + '/seed.txt'] = 'seed\n'
    return files


def bytecodes(graph, version):
    ext = '.yapyc3' if version == 3 else '.yapyc'
    return [node for node in graph['graph'] if node['outputs'][0].endswith(ext)]


class PythonAliasOutputsTest(unittest.TestCase):
    def graph(self, mode, version=3):
        graph = lib.make(fixture(mode, version), MODULE, '--target-platform', 'default-linux-x86_64')
        self.assertTrue(graph['result'])
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(all(dep in ids for node in graph['graph'] for dep in node['deps']))
        return graph

    def check_source(self, graph, version, source, alias):
        ext = '.yapyc3' if version == 3 else '.yapyc'
        output = '$(B)/' + source + suffix() + ext
        bytecode = lib.node_by_output(graph, output)
        self.assertEqual(bytecode['cmds'][0]['cmd_args'][-3:], [source + '-', '$(S)/' + source, output])
        for path in bytecode['inputs']:
            self.assertNotIn('/../', path)
        compiler_paths = ['tools/py3cc/py3cc', 'tools/py3cc/slow/slow'] if version == 3 else ['tools/py2cc/py2cc']
        self.assertCountEqual(bytecode['inputs'], ['$(S)/' + source] + ['$(B)/' + path for path in compiler_paths])
        for path in compiler_paths:
            tool = lib.node_by_output(graph, '$(B)/' + path)
            self.assertIn(tool['uid'], bytecode['deps'])
        consumers = [node for node in graph['graph'] if bytecode['uid'] in node['deps']]
        self.assertTrue(consumers)
        for node in consumers:
            self.assertIn(output, node['inputs'])
        if version == 3:
            self.assertIn('resfs/src/resfs/file/py/' + alias.replace('.', '/') + '.py.yapyc3=' + source + suffix() + ext, resource_kvs(graph))
        return bytecode

    def test_parent_aliases_use_canonical_physical_outputs(self):
        for version in (2, 3):
            for mode in ('parent',):
                with self.subTest(version=version, mode=mode):
                    graph = self.graph(mode, version)
                    self.check_source(graph, version, SOURCE, 'public.case')
                    self.check_source(graph, version, 'shared/entry.py', 'public.transport')

    def test_rooted_source_aliases_preserve_embedding_when_bytecode_is_disabled(self):
        for version in (2, 3):
            with self.subTest(version=version):
                graph = self.graph('rooted', version)
                self.assertFalse(bytecodes(graph, version))
                values = resource_kvs(graph)
                for source, alias in ((SOURCE, 'public.case'), ('shared/entry.py', 'public.transport')):
                    key = 'resfs/src/resfs/file/py/' + alias.replace('.', '/') + '.py' if version == 3 else 'resfs/src//py_modules/' + alias
                    self.assertIn(key + '=' + source, values)
                    self.assertTrue(any('$(S)/' + source in node['inputs'] for node in graph['graph']))

    def test_lexical_slash_keeps_the_original_hash_suffix_rule(self):
        for version in (2, 3):
            with self.subTest(version=version):
                graph = self.graph('local', version)
                ext = '.yapyc3' if version == 3 else '.yapyc'
                self.assertCountEqual([node['outputs'][0] for node in bytecodes(graph, version)],
                                      ['$(B)/' + MODULE + '/plain.py' + extra + ext for extra in ('', suffix())])

    def test_cpp_include_and_compiler_tool_keep_their_source_ownership(self):
        for version in (2, 3):
            with self.subTest(version=version):
                graph = self.graph('ownership', version)
                self.check_source(graph, version, SOURCE, 'public.case')
                marker = lib.node_by_output(graph, '$(B)/' + MODULE + '/marker.cpp.o')
                self.assertIn('$(S)/' + SOURCE, marker['inputs'])
                compiler = 'tools/py3cc' if version == 3 else 'tools/py2cc'
                tool_cc = lib.node_by_output(graph, '$(B)/' + compiler + '/main.cpp.pic.o')
                self.assertIn('$(S)/' + compiler + '/tool_config.py', tool_cc['inputs'])

    def test_generated_python_keeps_the_real_producer_and_main_cooutput(self):
        graph = self.graph('generated')
        output = '$(B)/' + MODULE + '/pkg/result.py' + suffix() + '.yapyc3'
        compiled = lib.node_by_output(graph, output)
        producer = lib.node_by_output(graph, '$(B)/' + MODULE + '/pkg/result.py')
        self.assertIn(producer['uid'], compiled['deps'])
        self.assertIn('$(B)/' + MODULE + '/manifest.txt', compiled['inputs'])
        self.assertIn('$(B)/' + MODULE + '/pkg/result.py', compiled['inputs'])
        self.assertIn('$(S)/' + MODULE + '/seed.txt', producer['inputs'])
        tool = lib.node_by_output(graph, '$(B)/orchard/carver/carver')
        self.assertIn(tool['uid'], producer['deps'])
        self.assertFalse(any('$(S)/' + MODULE + '/pkg/' in path for node in graph['graph'] for path in node['inputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
