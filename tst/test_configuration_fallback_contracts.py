import unittest

import lib
import test_java_features
import test_python_suites
from test_source_selection_contracts import library


class ConfigurationFallbackContractsTest(unittest.TestCase):
    def test_objcopy_defaults_to_llvm_when_vendor_tool_is_unspecified(self):
        files = {'app/ya.make': library('CFLAGS(-DTOOL=${OBJCOPY_TOOL})\nSRCS(main.cpp)'), 'app/main.cpp': ''}
        files['build/ymake_conf.py'] = lib.ymake_conf(extra='LLVM_TOOLS_ROOT=/custom/llvm\n').replace('emit("OBJCOPY_TOOL_VENDOR", tc.get("objcopy"))', '')
        graph = lib.make(files, 'app')
        node = lib.node_by_output(graph, '$(B)/app/main.cpp.o')
        args = node['cmds'][0]['cmd_args']
        self.assertIn('-DTOOL=/custom/llvm/bin/llvm-objcopy', args)

    def test_test_dependency_resolves_separate_conditional_module_declarations(self):
        files = {'app/ya.make': 'EXECTEST()\nRUN(tool)\nDEPENDS(choice)\nEND()\n',
                 'choice/ya.make': 'IF(BUILD_LIB)\n' + library('SRCS(main.cpp)') + 'ENDIF()\n'
                     + f'IF(NOT BUILD_LIB)\nPROGRAM(tool)\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\nENDIF()\n',
                 'choice/main.cpp': ''}
        graph = lib.make(files, 'app', '-t')
        tool = lib.node_by_output(graph, '$(B)/choice/tool')
        suite = lib.node_by_output(graph, '$(B)/app/test-results/exectest/meta.json')
        self.assertIn(tool['uid'], suite['deps'])

    def test_recursive_java_target_ignores_a_recurse_cycle(self):
        files = test_java_features.tree('JAVA_SRCS(src/Main.java)')
        files['app/ya.make'] += 'RECURSE_ROOT_RELATIVE(app missing)\n'
        graph = lib.make(files, 'app')
        self.assertEqual(sum('$(B)/app/app.jar' in n['outputs'] for n in graph['graph']), 1)

    def test_peer_later_reached_by_recurse_is_built_once(self):
        files = {'root/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(shared)\nSRCS(main.cpp)\nEND()\nRECURSE(middle)\n',
                 'root/main.cpp': '',
                 'root/middle/ya.make': library('') + 'RECURSE_ROOT_RELATIVE(shared)\n',
                 'shared/ya.make': library('SRCS(shared.cpp)'), 'shared/shared.cpp': ''}
        graph = lib.make(files, 'root')
        self.assertEqual(sum('$(B)/shared/shared.cpp.o' in n['outputs'] for n in graph['graph']), 1)

    def test_python_module_under_linux_headers_does_not_peer_itself(self):
        files = test_python_suites.fixture()
        directory = 'contrib/libs/linux-headers/python_helper'
        files[directory + '/ya.make'] = files.pop('lib/ya.make')
        files[directory + '/a.py'] = files.pop('lib/a.py')
        files['app/ya.make'] = f'PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR({directory})\nEND()\n'
        graph = lib.make(files, 'app')
        lib.node_by_output(graph, '$(B)/app/app')
        self.assertTrue(any(directory + '/a.py' in p for n in graph['graph'] for p in n['inputs']))

    def test_large_optional_configuration_resolves_selected_flag(self):
        options = ''.join(f'IF(OPTION_{i})\nCFLAGS(-DSELECTED_{i})\nENDIF()\n' for i in range(4096))
        files = {'app/ya.make': library(options + 'SRCS(main.cpp)'), 'app/main.cpp': ''}
        graph = lib.make(files, 'app', '-DOPTION_4095=yes')
        args = lib.node_by_output(graph, '$(B)/app/main.cpp.o')['cmds'][0]['cmd_args']
        self.assertEqual([a for a in args if a.startswith('-DSELECTED_')], ['-DSELECTED_4095'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
