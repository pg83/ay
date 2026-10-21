import unittest

import lib
import test_python2
import test_python_proto
import test_resource_objcopy


def python2_files(body):
    files = test_python2.python2_base()
    files.update({'lib/ya.make': f'PY2_LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n',
                  'prog/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR(lib)\nEND()\n',
                  'prog/main.cpp': ''})
    return files


class PythonEdgeContractsTest(unittest.TestCase):
    def test_python2_rejects_unsupported_sources(self):
        for source, diagnostic in (('input.proto', '.proto sources'), ('${BINDIR}/generated.py', 'build-root sources')):
            with self.subTest(source=source):
                files = python2_files(f'PY_SRCS({source})')
                files['lib/input.proto'] = ''
                result = lib.make_process(files, 'prog')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(diagnostic + ' of a Python 2 module are not modelled', result.stderr)
                self.assertNotIn('panic:', result.stderr)

    def test_disabling_light_python2_compiler_restores_regular_tool(self):
        files = python2_files('ENABLE(USE_LIGHT_PY2CC)\nDISABLE(USE_LIGHT_PY2CC)\nSTYLE_PYTHON()\nPY_SRCS(main.py)')
        files['lib/main.py'] = ''
        graph = lib.make(files, 'prog')
        bytecode = lib.node_by_output(graph, '$(B)/lib/main.py.yapyc')
        self.assertEqual(bytecode['cmds'][0]['cmd_args'][0], '$(B)/tools/py2cc/py2cc')
        self.assertFalse(any('black' in ' '.join(c['cmd_args']) for n in graph['graph'] for c in n['cmds']))

    def test_python2_include_macro_applies_native_compiler_flags(self):
        graph = lib.make({'lib/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}PYTHON2_ADDINCL()\nSRCS(main.cpp)\nEND()\n',
                          'lib/main.cpp': ''}, 'lib')
        args = lib.node_by_output(graph, '$(B)/lib/main.cpp.o')['cmds'][0]['cmd_args']
        self.assertIn('-DARCADIA_PYTHON_UNICODE_SIZE=4', args)

    def test_python2_multimodule_cython_c_output_uses_variant_suffix(self):
        files = python2_files('PEERDIR(shared)')
        files['shared/ya.make'] = f'PY23_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(CYTHON_C m.pyx)\nEND()\n'
        files['shared/m.pyx'] = ''
        files['contrib/tools/cython_py2/cython.py'] = ''
        for name in test_python2.CYTHON_UTILITY:
            files[f'contrib/tools/cython_py2/Cython/Utility/{name}'] = ''
        for path in test_python2.CYTHON_HEADERS:
            files[path] = ''
        graph = lib.make(files, 'prog')
        cython = lib.node_by_output(graph, '$(B)/shared/m.pyx.py2.py2.c')
        compiled = lib.node_by_output(graph, '$(B)/shared/m.pyx.py2.py2.c.py2.o')
        self.assertNotIn('--cplus', cython['cmds'][0]['cmd_args'])
        self.assertIn(cython['uid'], compiled['deps'])

    def test_proto_relative_source_resolves_through_source_directories(self):
        files = test_python_proto.python_base()
        files['proto/ya.make'] = f'PROTO_LIBRARY()\n{lib.BARE_MODULE}EXCLUDE_TAGS(CPP_PROTO)\nSRCDIR(extra)\nSRCS(./message.proto)\nEND()\n'
        files['extra/message.proto'] = 'syntax = "proto3";\n'
        graph = lib.make(files, 'app')
        proto = next(n for n in graph['graph'] if any(o.endswith('_pb2.py') for o in n['outputs']))
        self.assertIn('$(S)/extra/message.proto', proto['inputs'])
        self.assertTrue(any(o.endswith('_pb2.py') for o in proto['outputs']))

    def test_darwin_yasm_resource_uses_symbol_prefix(self):
        files = test_resource_objcopy.base_files('RESOURCE(conftest.py payload)\n')
        files['lib/conftest.py'] = ''
        lib.tool_program(files, 'tools/rorescompiler', 'rorescompiler')
        lib.tool_program(files, 'contrib/tools/yasm', 'yasm')
        graph = lib.make(files, 'lib', '--target-platform', 'default-darwin-x86_64')
        compress = next(c['cmd_args'] for n in graph['graph'] for c in n['cmds'] if c['cmd_args'][0] == '$(B)/tools/rescompressor/rescompressor')
        self.assertIn('--prefix', compress)


if __name__ == '__main__':
    unittest.main(verbosity=2)
