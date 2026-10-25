import json
import unittest

from tst import lib
from tst.make import test_fortran
from tst.make import test_go_package


class ToolchainEdgeContractsTest(unittest.TestCase):
    def test_pch_llvm_root_can_be_selected_by_module_or_command_line(self):
        for source in ('module', 'flag'):
            with self.subTest(source=source):
                files = {'mod/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}'
                         + ('SET(LLVM_TOOLS_ROOT /custom/llvm)\n' if source == 'module' else '')
                         + 'GEN_PCH_FILE(${ARCADIA_ROOT}/mod/p.h ${BINDIR}/p.h.pch)\nSRCS(main.cpp)\nEND()\n',
                         'mod/p.h': '', 'mod/main.cpp': '#include "mod/p.h.pch"\n'}
                lib.tool_program(files, 'sdg/tier0/tools/make_pch_wrapper', 'make_pch_wrapper')
                graph = lib.make(files, 'mod', *(['-DLLVM_TOOLS_ROOT=/custom/llvm'] if source == 'flag' else []), opensource=False)
                pch = lib.node_by_output(graph, '$(B)/mod/p.h.pch')
                self.assertEqual(pch['cmds'][0]['cmd_args'][1], '/custom/llvm')
                self.assertIn('--sysroot=$(B)/resources/OS_SDK_ROOT', pch['cmds'][0]['cmd_args'])

    def test_fortran_uses_the_non_opensource_sdk(self):
        files = test_fortran.world({'mod/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(main.f)\nEND()\n', 'mod/main.f': '      end\n'})
        graph = lib.make(files, 'mod', opensource=False)
        compiled = lib.node_by_output(graph, '$(B)/mod/main.f.o')
        self.assertIn('--sysroot=$(B)/resources/OS_SDK_ROOT', compiled['cmds'][0]['cmd_args'])

    def test_go_sdk_peer_propagates_its_resource(self):
        files = test_go_package.go_world()
        files.update({'app/ya.make': f'GO_LIBRARY()\n{lib.BARE_MODULE}SRCS(main.go)\nEND()\n', 'app/main.go': 'package app\n',
                      'build/platform/linux_sdk/ya.make': lib.resource_lib('OS_SDK_ROOT', 42)})
        graph = lib.make(files, 'app', opensource=False)
        lib.node_by_output(graph, '$(B)/app/app.a')

    def test_toolchain_extra_flags_are_visible_to_module_conditions(self):
        config = json.loads(lib.ya_conf_json())
        config['toolchain']['clang20']['params']['extra_flags'] = {'FROM_TOOLCHAIN': 'yes', 'COUNT': 7, 'SKIP': {'nested': 1}}
        files = {'build/ya.conf.json': json.dumps(config),
                 'app/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}IF(FROM_TOOLCHAIN AND COUNT == 7)\nSRCS(selected.cpp)\nELSE()\nSRCS(wrong.cpp)\nENDIF()\nEND()\n',
                 'app/selected.cpp': '', 'app/wrong.cpp': ''}
        graph = lib.make(files, 'app', '--build', 'dist-release')
        lib.node_by_output(graph, '$(B)/app/selected.cpp.o')
        self.assertFalse(any('$(B)/app/wrong.cpp.o' in n['outputs'] for n in graph['graph']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
