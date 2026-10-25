import os
import unittest
from unittest.mock import patch

from tst import lib


class SandboxDownloadTest(unittest.TestCase):
    def test_download_modes_and_consumers(self):
        for dist in (False, True):
            for configured, override, enabled in ((None, None, False), (True, None, True),
                                                   (False, 'yes', True), (True, 'no', False)):
                for token in ('', 'test-download-token'):
                    with self.subTest(dist=dist, configured=configured, override=override, token=bool(token)):
                        files = {'mod/ya.make': 'EXECTEST()\nRUN(tool)\nDATA(sbr://12=fixtures sbr://12)\nEND()\n'}
                        if configured is not None:
                            files['ya.conf'] = 'dir_outputs_in_nodes = ' + str(configured).lower() + '\n'
                        env = {'YA_TOKEN': token}
                        if override is not None:
                            env['YA_DIR_OUTPUTS_IN_NODES'] = override
                        with patch.dict(os.environ):
                            os.environ.pop('YA_DIR_OUTPUTS_IN_NODES', None)
                            graph = lib.make(files, 'mod', '--target-platform-tests', *(['--dist'] if dist else []), env=env)
                        downloads = [n for n in graph['graph'] if n['kv'].get('p') == 'DL']
                        self.assertEqual(len(downloads), 1)
                        node = downloads[0]
                        root = '$(B)/sandbox-storage/12/'
                        outputs = [root + 'resource_info.json', root + 'resource', '$(B)/res12.log']
                        args = ['$(TEST_TOOL_HOST)/test_tool', 'download', '--storage-root', '$(B)', '--id', '12',
                                '--log-path', '$(B)/res12.log', '--rename-to', 'resource', '--resource-file',
                                '$(RESOURCE_ROOT)/sbr/12/resource']
                        if enabled:
                            outputs.insert(0, root + 'dir_res.tar')
                            args += ['--dir-output-tared-path', root + 'dir_res.tar']
                        if dist:
                            args += ['--for-dist-build', '--log-level', 'DEBUG', '--timeout', '900']
                        if token:
                            args += ['--token', token]
                        self.assertEqual(node['outputs'], outputs)
                        self.assertEqual(node['cmds'][0]['cmd_args'], args)
                        self.assertEqual(node['cmds'][0]['cwd'], '$(B)')
                        self.assertEqual(node['requirements']['network'], 'full')
                        self.assertEqual(node['inputs'], [])
                        self.assertEqual(node['deps'], [])
                        consumers = [n for n in graph['graph'] if n['kv'].get('run_test_node') and node['uid'] in n['deps']]
                        self.assertEqual(len(consumers), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
