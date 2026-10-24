import collections
import unittest

import lib
from test_python_cython import python_base
from test_python2 import python2_base
def fixture(case):
    files = python_base()
    files.update(python2_base())
    for rel, text in list(files.items()):
        if rel.startswith('contrib/tools/cython/'):
            files[rel.replace('contrib/tools/cython/', 'contrib/tools/cython_py2/')] = text

    source_case = {
        'hidden-src': 'plain-src',
        'pkg-hidden-src': 'plain-src',
        'explicit-run-sdk': 'explicit-sdk',
        'roots-new-old': 'consumer-only',
        'roots-old-new': 'consumer-only',
    }.get(case, case.removeprefix('pkg-'))
    body = {
        'consumer-only': '',
        'plain-src': 'SRCS(shared/bridge.pxd)\n',
        'plain-run': 'RUN_PROGRAM(tools/probe IN shared/bridge.pxd OUT_NOAUTO unused.o)\n',
        'plain-noparse': 'RUN_PROGRAM(tools/probe IN_NOPARSE shared/bridge.pxd OUT_NOAUTO unused.o)\n',
        'old-cython': 'USE_PYTHON2()\nBUILDWITH_CYTHON_CPP(early.pyx)\n',
        'new-cython': 'USE_PYTHON3()\nBUILDWITH_CYTHON_CPP(early.pyx)\n',
        'explicit-sdk': 'ADDINCL(FOR cython contrib/tools/cython_py2/Cython/Includes)\nSRCS(shared/bridge.pxd)\n',
    }[source_case]
    if case == 'explicit-run-sdk':
        body = body.replace('SRCS(shared/bridge.pxd)',
                            'RUN_PROGRAM(tools/probe IN shared/bridge.pxd OUT_NOAUTO unused.o)')
    if case.startswith('pkg-'):
        body = 'ADDINCL(FOR cython grove/typed)\n' + body
    files.update({
        'maple/ya.make': ('LIBRARY(Maple)\n' + lib.BARE_MODULE + 'SET(PYTHON3 yes)\n'
                          'PEERDIR(grove)\nBUILDWITH_CYTHON_CPP(main.pyx)\nEND()\n'),
        'maple/main.pyx': 'from shared.bridge cimport value\n',
        'grove/ya.make': 'LIBRARY(Grove)\n' + lib.BARE_MODULE + body + 'END()\n',
        'grove/early.pyx': 'from shared.bridge cimport value\n',
        'shared/bridge.pxd': 'from libcpp.pair cimport pair\n',
    })
    for tool in ['cython', 'cython_py2']:
        root = 'contrib/tools/' + tool + '/Cython/Includes/'
        files[root + 'libcpp/__init__.pxd'] = ''
        files[root + 'libcpp/pair.pxd'] = 'from .utility cimport pair\n'
        files[root + 'libcpp/utility.pxd'] = ''
    if case.startswith('pkg-'):
        files['shared/bridge.pxd'] = 'from libcpp cimport pair\n'
        files['grove/typed/libcpp/__init__.pxd'] = ''
    if case in ['hidden-src', 'pkg-hidden-src', 'roots-new-old', 'roots-old-new']:
        files['maple/ya.make'] = files['maple/ya.make'].replace('PEERDIR(grove)', 'PEERDIR(proxy)')
        files['proxy/ya.make'] = ('PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\n'
                                  'IF (MODULE_TAG == PY2)\nPEERDIR(grove)\nENDIF()\nEND()\n')
    if case.startswith('roots-'):
        files['maple/ya.make'] = files['maple/ya.make'].replace('PEERDIR(proxy)', 'PEERDIR(birch proxy)')
        files['birch/ya.make'] = ('LIBRARY(Birch)\n' + lib.BARE_MODULE + 'USE_PYTHON2()\n'
                                  'SET(MODULE_TAG)\nPEERDIR(proxy)\nBUILDWITH_CYTHON_CPP(main.pyx)\nEND()\n')
        files['birch/main.pyx'] = 'from shared.bridge cimport value\n'
    lib.tool_program(files, 'tools/probe', 'probe')
    return files


def build_fixture(host_cython=False):
    files = fixture('hidden-src')
    files['shared/bridge.pxd'] = 'from late.types cimport value\n'
    files['maple/ya.make'] = files['maple/ya.make'].replace(
        'PEERDIR(proxy)', 'PEERDIR(proxy canopy)\nADDINCL(FOR cython ${ARCADIA_BUILD_ROOT}/canopy)')
    files['canopy/ya.make'] = ('GEN_LIBRARY()\nRUN_PROGRAM(tools/probe IN seed.txt '
                               'OUT_NOAUTO late/types.pxd OUTPUT_INCLUDES ${ARCADIA_ROOT}/tail.pxd)\nEND()\n')
    files['canopy/seed.txt'] = 'generator seed\n'
    files['tail.pxd'] = ''
    if host_cython:
        files['tools/probe/ya.make'] = files['tools/probe/ya.make'].replace(
            'SRCS(main.cpp)', 'USE_PYTHON3()\nPEERDIR(hostproxy)\n'
                              'BUILDWITH_CYTHON_CPP(body.pyx)\nSRCS(main.cpp)')
        files['tools/probe/body.pyx'] = 'from hostshared.bridge cimport value\n'
        files['hostshared/bridge.pxd'] = 'from libcpp.pair cimport pair\n'
        files['hostproxy/ya.make'] = ('PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\n'
                                     'IF (MODULE_TAG == PY2)\nPEERDIR(hostgrove)\nENDIF()\nEND()\n')
        files['hostgrove/ya.make'] = ('LIBRARY()\n' + lib.BARE_MODULE
                                     + 'SRCS(hostshared/bridge.pxd)\nEND()\n')
    return files


class CythonSdkScopeTest(unittest.TestCase):
    def check_context(self, case, selected):
        graph = lib.make(fixture(case), 'maple', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        generated = lib.node_by_output(graph, '$(B)/maple/main.pyx.cpp')
        self.assertIn('$(S)/contrib/tools/cython/cython.py', generated['inputs'])
        self.assertIn('-I$(S)/contrib/tools/cython/Cython/Includes', generated['cmds'][0]['cmd_args'])
        self.assertIn('$(S)/maple/main.pyx', generated['inputs'])
        self.assertIn('$(S)/shared/bridge.pxd', generated['inputs'])
        expected = []
        if case.startswith('pkg-'):
            expected.append('$(S)/grove/typed/libcpp/__init__.pxd')
        if selected:
            expected += ['$(S)/contrib/tools/' + selected + '/Cython/Includes/libcpp/' + name
                         for name in ['pair.pxd', 'utility.pxd']]
        actual = [p for p in generated['inputs'] if '/Includes/libcpp/' in p or '/grove/typed/' in p]
        self.assertEqual(collections.Counter(actual), collections.Counter(expected))
        compiled = lib.node_by_output(graph, '$(B)/maple/main.pyx.cpp.o')
        self.assertIn(generated['outputs'][0], compiled['inputs'])
        self.assertIn(generated['uid'], compiled['deps'])
        owner = lib.node_by_output(graph, '$(B)/maple/libMaple.a')
        self.assertIn(compiled['uid'], owner['deps'])
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        return graph

    def test_parsed_inputs_use_owned_package_paths_without_an_implicit_sdk(self):
        for case in ['pkg-plain-run', 'pkg-hidden-src']:
            with self.subTest(case=case):
                self.check_context(case, None)

    def test_unresolved_header_source_does_not_capture_later_consumer_context(self):
        self.check_context('hidden-src', 'cython')

    def test_explicit_sdk_and_real_cython_invocations_keep_selected_source_context(self):
        for case, selected in [('explicit-run-sdk', 'cython_py2'), ('old-cython', 'cython_py2'),
                               ('new-cython', 'cython')]:
            with self.subTest(case=case):
                self.check_context(case, selected)

    def test_no_parse_and_no_prior_source_leave_the_consumer_sdk(self):
        for case in ['consumer-only', 'plain-noparse']:
            with self.subTest(case=case):
                self.check_context(case, 'cython')


    def test_late_configured_build_input_binds_the_real_producer_before_tools(self):
        self.check_build_context(False)

    def test_new_host_cython_uses_are_resolved_in_the_existing_tool_wave(self):
        self.check_build_context(True)

    def check_build_context(self, host_cython):
        graph = lib.make(build_fixture(host_cython), 'maple', '--sandboxing', '--add-modules-to-results',
                         '--target-platform', 'default-linux-x86_64')
        cython = lib.node_by_output(graph, '$(B)/maple/main.pyx.cpp')
        producer = lib.node_by_output(graph, '$(B)/canopy/late/types.pxd')
        tool = lib.node_by_output(graph, '$(B)/tools/probe/probe')
        self.assertIn(producer['outputs'][0], cython['inputs'])
        self.assertIn('$(S)/tail.pxd', cython['inputs'])
        self.assertIn(producer['uid'], cython['deps'])
        self.assertIn(tool['uid'], producer['foreign_deps']['tool'])
        self.assertIn('$(S)/canopy/seed.txt', producer['inputs'])
        compiled_tool = lib.node_by_output(graph, '$(B)/tools/probe/main.cpp.pic.o')
        self.assertIn('-fPIC', compiled_tool['cmds'][0]['cmd_args'])
        self.assertIn(compiled_tool['uid'], tool['deps'])
        if host_cython:
            generated = lib.node_by_output(graph, '$(B)/tools/probe/body.pyx.cpp')
            self.assertCountEqual([p for p in generated['inputs'] if '/Includes/libcpp/' in p],
                                  ['$(S)/contrib/tools/cython/Cython/Includes/libcpp/' + name
                                   for name in ['pair.pxd', 'utility.pxd']])
            self.assertIn('$(S)/hostshared/bridge.pxd', generated['inputs'])
            compiled = lib.node_by_output(graph, '$(B)/tools/probe/body.pyx.cpp.pic.o')
            self.assertIn(generated['uid'], compiled['deps'])
            self.assertIn(compiled['uid'], tool['deps'])
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))

    def test_dependency_traversal_keeps_the_first_real_consumer_for_both_root_orders(self):
        for case, targets in [('roots-new-old', ['maple', 'birch']),
                              ('roots-old-new', ['birch', 'maple'])]:
            with self.subTest(case=case):
                graph = lib.make(fixture(case), targets[-1], '--sandboxing', '--add-modules-to-results',
                                 '--target-platform', 'default-linux-x86_64', *targets[:-1])
                for output in ['$(B)/maple/main.pyx.cpp', '$(B)/birch/main.pyx.py2.cpp']:
                    node = lib.node_by_output(graph, output)
                    self.assertEqual(collections.Counter(p for p in node['inputs'] if '/Includes/libcpp/' in p),
                                     collections.Counter('$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/' + name
                                                         for name in ['pair.pxd', 'utility.pxd']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
