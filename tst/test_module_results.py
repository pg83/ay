import unittest

import lib
from test_proto_targets import target_files


class ModuleResultsTest(unittest.TestCase):
    def test_recursed_libraries_expose_local_and_global_archives(self):
        files = {
            'root/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(child)\n',
            'root/child/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}ADDINCL(${{ARCADIA_BUILD_ROOT}}/root/child)\nRUN_PYTHON3(gen.py OUT_NOAUTO generated.h)\nSRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'root/child/local.cpp': 'int local;\n',
            'root/child/global.cpp': '#include "generated.h"\nint global;\n',
            'root/child/gen.py': '',
        }
        for targets in (('root',), ('root/child', 'root'), ('root', 'root/child')):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, '-t', '--sandboxing', '--strip-packages-from-results')
                for name in ('libroot-child.a', 'libroot-child.global.a'):
                    archive = lib.node_by_output(graph, '$(B)/root/child/' + name)
                    self.assertIn(archive['uid'], graph['result'])
                compiler = lib.node_by_output(graph, '$(B)/root/child/global.cpp.o')
                generator = lib.node_by_output(graph, '$(B)/root/child/generated.h')
                self.assertIn(generator['uid'], compiler['deps'])
                self.assertIn('$(B)/root/child/generated.h', compiler['inputs'])

    def test_unrequested_peer_globals_do_not_become_results(self):
        files = {
            'root/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}PEERDIR(peer)\nSRCS(local.cpp)\nEND()\n',
            'root/local.cpp': 'int local;\n',
            'peer/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(GLOBAL global.cpp)\nEND()\n',
            'peer/global.cpp': 'int global;\n',
        }
        graph = lib.make(files, 'root', '--sandboxing', '--strip-packages-from-results', '--add-modules-to-results')
        self.assertIn(lib.node_by_output(graph, '$(B)/root/libroot.a')['uid'], graph['result'])
        self.assertFalse(any(output.startswith('$(B)/peer/') for node in graph['graph'] for output in node['outputs']))

    def test_host_tool_global_archives_are_module_results(self):
        files = {
            'root/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/gen OUT generated.cpp)\nEND()\n',
            'tools/gen/ya.make': f'PROGRAM(gen)\n{lib.BARE_MODULE}PEERDIR(peer)\nSRCS(main.cpp)\nEND()\n',
            'tools/gen/main.cpp': 'int main() {}\n',
            'peer/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'peer/local.cpp': 'int local;\n',
            'peer/global.cpp': 'int global;\n',
        }
        for options in ([], ['--strip-packages-from-results']):
            with self.subTest(options=options):
                graph = lib.make(files, 'root', '--add-modules-to-results', *options)
                for output in ('$(B)/tools/gen/gen', '$(B)/peer/libpeer.a', '$(B)/peer/libpeer.global.a'):
                    node = lib.node_by_output(graph, output)
                    self.assertIn(node['uid'], graph['result'], output)
                tool = lib.node_by_output(graph, '$(B)/tools/gen/gen')
                self.assertEqual(tool['platform'], 'default-linux-x86_64')

    def test_deferred_java_peer_is_a_module_result(self):
        files = lib.java_tree(**{
            'root/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(peer)\nJAVA_SRCS(Root.java)\nEND()\n',
            'root/Root.java': '',
            'peer/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(Peer.java)\nEND()\n',
            'peer/Peer.java': '',
        })
        for options in ([], ['--strip-packages-from-results']):
            with self.subTest(options=options):
                graph = lib.make(files, 'root', '--add-modules-to-results', *options)
                root = lib.node_by_output(graph, '$(B)/root/root.jar')
                peer = lib.node_by_output(graph, '$(B)/peer/peer.jar')
                self.assertIn(peer['uid'], root['deps'])
                self.assertIn(peer['uid'], graph['result'])

    def test_java_recurse_roots_expose_native_global_archives(self):
        files = lib.java_tree(**{
            'root/ya.make': 'JAVA_LIBRARY()\nEND()\nRECURSE(child)\n',
            'root/child/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'root/child/local.cpp': 'int local;\n',
            'root/child/global.cpp': 'int global;\n',
        })
        graph = lib.make(files, 'root', '--sandboxing', '--strip-packages-from-results')
        for name in ('libroot-child.a', 'libroot-child.global.a'):
            archive = lib.node_by_output(graph, '$(B)/root/child/' + name)
            self.assertIn(archive['uid'], graph['result'])

    def test_requested_proto_variants_expose_global_archives(self):
        files = target_files()
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO PY3_PROTO)\nSRCS(a.proto extra.cpp)\nEND()\n'
        files['p/extra.cpp'] = 'int extra;\n'
        graph = lib.make(files, 'p', '--sandboxing', '--strip-packages-from-results')
        for name in ('libpy3p.a', 'libpy3p.global.a'):
            archive = lib.node_by_output(graph, '$(B)/p/' + name)
            self.assertIn(archive['uid'], graph['result'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
