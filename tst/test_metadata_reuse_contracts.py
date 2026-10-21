import unittest

import lib
import test_java
import test_link_sbom
import test_native_swig
import test_proto_py_variant
import test_python_cython
from test_source_selection_contracts import library


class MetadataReuseContractsTest(unittest.TestCase):
    def test_sbom_component_is_reused_after_root_becomes_package_dependency(self):
        files = test_link_sbom.fixture()
        files['group/ya.make'] = 'CI_GROUP()\nPEERDIR(lib)\nEND()\n'
        graph = lib.make(files, 'group', 'lib', *test_link_sbom.X86_64)
        self.assertEqual(sum('$(B)/lib/lib.CPP.component.sbom' in n['outputs'] for n in graph['graph']), 1)
        lib.node_by_output(graph, '$(B)/group/group.ci.pkg.fake')

    def test_sbom_output_collision_is_reported(self):
        files = test_link_sbom.fixture()
        lib.tool_program(files, 'tools/gen', 'gen')
        files['app/ya.make'] = library('LICENSE(MIT)\nRUN_PROGRAM(tools/gen OUT_NOAUTO app.CPP.component.sbom)\nSRCS(main.cpp)')
        files['app/main.cpp'] = ''
        result = lib.make_process(files, 'app', *test_link_sbom.X86_64)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('duplicate producer', result.stderr)
        self.assertIn('app.CPP.component.sbom', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_python_only_proto_has_no_cpp_sbom_component(self):
        files = {**test_link_sbom.fixture(), **test_proto_py_variant.py_proto_files()}
        files['p/ya.make'] = 'PROTO_LIBRARY()\nEXCLUDE_TAGS(CPP_PROTO)\nONLY_TAGS(PY3_PROTO)\nLICENSE(MIT)\nSRCS(message.proto)\nEND()\n'
        files['p/message.proto'] = 'syntax = "proto3";\n'
        files['q/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nEND()\n'
        graph = lib.make(files, 'py', *test_link_sbom.X86_64)
        lib.node_by_output(graph, '$(B)/p/message__intpy3___pb2.py')
        self.assertFalse(any('$(B)/p/p.CPP.component.sbom' in n['outputs'] for n in graph['graph']))

    def test_java_build_input_directory_is_not_scanned_as_source(self):
        files = test_java.JavaProgramTest().tree()
        files['gen/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT()\nRUN_JAVA_PROGRAM(Main IN_DIR ${BINDIR} OUT_DIR out CLASSPATH app)\nEND()\n'
        graph = lib.make(files, 'gen')
        generated = next(n for n in graph['graph'] if n['kv']['p'] == 'RJ')
        self.assertNotIn('$(S)/gen/in.txt', generated['inputs'])
        self.assertIn('$(B)/app/app.run.cp.jar', generated['inputs'])

    def test_swig_header_cycle_uses_the_native_header_parser(self):
        files = test_native_swig.perl_tree('LIBRARY(Binding)')
        files['bindings/api.swg'] = '%include "api.h"\n'
        files['bindings/api.h'] = '#include "api.swg"\n#include "tail.h"\n'
        files['bindings/tail.h'] = ''
        graph = lib.make(files, 'bindings/perl')
        generated = lib.node_by_output(graph, '$(B)/bindings/perl/__/api_wrap.swg.cpp')
        for name in ('api.swg', 'api.h', 'tail.h'):
            self.assertIn('$(S)/bindings/' + name, generated['inputs'])
        self.assertEqual(len(generated['inputs']), len(set(generated['inputs'])))

    def test_generated_cython_include_resolves_declared_dependency(self):
        files = test_python_cython.python_base()
        lib.tool_program(files, 'tools/gen', 'gen')
        files['lib/ya.make'] = 'PY3_LIBRARY()\nPEERDIR(gen)\nPY_SRCS(main.pyx)\nEND()\n'
        files['gen/ya.make'] = 'GEN_LIBRARY()\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.pxi OUTPUT_INCLUDES gen/extra.pxd)\nEND()\n'
        files['lib/main.pyx'] = 'include "gen/generated.pxi"\n'
        files['gen/extra.pxd'] = 'cdef int value\n'
        graph = lib.make(files, 'app')
        generated = lib.node_by_output(graph, '$(B)/gen/generated.pxi')
        cython = next(n for n in graph['graph'] if n['kv']['p'] == 'CY')
        self.assertIn(generated['uid'], cython['deps'])
        self.assertIn('$(S)/gen/extra.pxd', cython['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
