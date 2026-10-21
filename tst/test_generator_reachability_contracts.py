import json
import os
import shutil
import subprocess
import sys
import unittest

import lib
import test_go_package
import test_nots
import test_proto_flatc
from test_source_selection_contracts import library
from test_test_runner_contracts import AGENT_ENV, test_args
from test_ymake_conf import Tree


class GeneratorReachabilityContractsTest(unittest.TestCase):
    def test_typescript_preparation_is_reused_when_package_requires_build(self):
        files = test_nots.NotsTests().project()
        files['web/tests/ya.make'] = 'TS_TEST_FOR(web)\nTS_TEST(test)\nEND()\n'
        files['group/ya.make'] = 'CI_GROUP()\nPEERDIR(web)\nEND()\n'
        graph = lib.make(files, 'group', 'web/tests')
        self.assertEqual(sum('$(B)/web/package.json' in n['outputs'] for n in graph['graph']), 1)
        lib.node_by_output(graph, '$(B)/web/output.tar')

    def test_embed_directory_with_repeated_separators_excludes_hidden_files(self):
        files = test_go_package.go_world()
        test_go_package.go_module(files, 'embed/sample', macros='GO_EMBED_DIR(assets//)\n')
        files.update({'embed/sample/assets/visible.txt': 'visible', 'embed/sample/assets/.hidden': 'hidden'})
        node = test_go_package.go_node(lib.make(files, 'embed/sample'), 'embed/sample')
        self.assertIn('$(S)/embed/sample/assets/visible.txt', node['inputs'])
        self.assertNotIn('$(S)/embed/sample/assets/.hidden', node['inputs'])

    def test_archive_metadata_is_not_a_generated_header(self):
        files = {'app/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(peer)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/peer)\nSRCS(main.cpp)\nEND()\n',
                 'app/main.cpp': '#include "libpeer.a"\n',
                 'peer/ya.make': library('SRCS(peer.cpp)'), 'peer/peer.cpp': ''}
        result = lib.make_process(files, 'app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unresolved include "libpeer.a"', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_generated_template_retains_missing_system_include_diagnostic(self):
        files = {'app/ya.make': library('SRCS(main.cpp.in)'),
                 'app/main.cpp.in': '#include <unresolved_system_header.h>\n'}
        result = lib.make_process(files, 'app', '-k')
        self.assertIn('unresolved include <unresolved_system_header.h>', result.stderr)
        self.assertNotIn('panic:', result.stderr)
        graph = json.loads(result.stdout)
        lib.node_by_output(graph, '$(B)/app/main.cpp.o')

    def test_generated_flatbuffer_keeps_its_producer_across_demand_variants(self):
        files = test_proto_flatc.flatc_files()
        lib.tool_program(files, 'tools/gen', 'gen')
        files.update({'model/ya.make': library('PEERDIR(schema)\nSRCS(${ARCADIA_BUILD_ROOT}/schema/generated.fbs)'),
                      'schema/ya.make': 'GEN_LIBRARY()\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.fbs)\nEND()\n',
                      'group/ya.make': 'CI_GROUP()\nPEERDIR(model)\nEND()\n'})
        graph = lib.make(files, 'group', 'model')
        schema = lib.node_by_output(graph, '$(B)/schema/generated.fbs')
        compiled = next(n for n in graph['graph'] if n['kv']['p'] == 'CC' and '$(B)/schema/generated.fbs.cpp' in n['inputs'])
        flatc = lib.node_by_output(graph, '$(B)/schema/generated.fbs.cpp')
        self.assertIn(schema['uid'], flatc['deps'])
        self.assertIn(flatc['uid'], compiled['deps'])

    def test_agent_detection_uses_default_style_outside_an_agent(self):
        tree = Tree(self, {'app/ya.make': 'EXECTEST()\nRUN(check)\nEND()\n'}, ya_conf='auto_detect_agent = true\n')
        env = {k: v for k, v in os.environ.items() if k not in (*AGENT_ENV, *lib.TOOLCHAIN_ENV_VARS, 'YA_AUTO_DETECT_AGENT', 'YA_OUTPUT_STYLE')}
        prefix = []
        if sys.platform.startswith('linux'):
            if shutil.which('unshare') is None:
                self.skipTest('unshare is unavailable')
            prefix = ['unshare', '-Urpf', '--mount-proc']
            probe = subprocess.run([*prefix, 'true'], capture_output=True, env=env, timeout=10)
            if probe.returncode:
                self.skipTest('unprivileged PID namespaces are unavailable')
        result = subprocess.run([*prefix, str(lib.AY), 'make', '-j0', '-G', '-t', '--source-root', str(tree.src),
                                 '--host-platform', 'default-linux-x86_64', '--target-platform', 'default-linux-aarch64', 'app'],
                                env=env, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        _, args = test_args(json.loads(result.stdout))
        self.assertEqual(args[args.index('--output-style') + 1], 'ninja')

    def test_agent_detection_uses_default_style_when_proc_cannot_be_read(self):
        tree = Tree(self, {'app/ya.make': 'EXECTEST()\nRUN(check)\nEND()\n'}, ya_conf='auto_detect_agent = true\n')
        env = {k: v for k, v in os.environ.items() if k not in (*AGENT_ENV, *lib.TOOLCHAIN_ENV_VARS, 'YA_AUTO_DETECT_AGENT', 'YA_OUTPUT_STYLE')}
        env['AY_CHAOS'] = 'readfile-eio=0'
        result = subprocess.run([str(lib.AY), 'make', '-j0', '-G', '-t', '--source-root', str(tree.src),
                                 '--host-platform', 'default-linux-x86_64', '--target-platform', 'default-linux-aarch64', 'app'],
                                env=env, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        _, args = test_args(json.loads(result.stdout))
        self.assertEqual(args[args.index('--output-style') + 1], 'ninja')


if __name__ == '__main__':
    unittest.main(verbosity=2)
