import json
import unittest

from tst import lib
from tst.make import test_nots


def fixture(mode):
    files = {('orchard/ui/' + k[4:] if k.startswith('web/') else k): v
             for k, v in test_nots.NotsTests().project().items()}
    files['orchard/ui/ya.make'] = 'TS_PACKAGE()\nNO_LINT()\nEND()\n'
    files['orchard/ui/package.json'] = '{"name":"orchard"}'
    files['orchard/ui/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
    files['build/scripts/fs_tools.py'] = 'import process_command_files\n'
    files['build/scripts/process_command_files.py'] = ''
    files['orchard/ya.make'] = 'UNION()\nBUNDLE(orchard/ui NAME payload.tar)\nEND()\n'
    if mode == 'workspace':
        files['orchard/ui/package.json'] = json.dumps({'name': 'orchard', 'dependencies': {'shared': 'workspace:../shared'}})
        files['orchard/shared/ya.make'] = 'TS_PACKAGE()\nNO_LINT()\nEND()\n'
        files['orchard/shared/package.json'] = '{"name":"shared"}'
        files['orchard/shared/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
    if mode == 'vite':
        files['orchard/ui/ya.make'] = 'TS_VITE()\nTS_VITE_CONFIG(vite.config.js)\nTS_VITE_OUTPUT(dist)\nNO_LINT()\nEND()\n'
        files['orchard/ui/package.json'] = '{"name":"orchard","files":["dist"]}'
        files['orchard/ui/vite.config.js'] = 'export default {};\n'
    if mode == 'generated':
        files['orchard/ui/package.json'] = '{"name":"orchard","dependencies":{"api":"workspace:../api"}}'
        files['orchard/api/ya.make'] = ('PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\n'
                                      'EXCLUDE_TAGS(CPP_PROTO JAVA_PROTO PY_PROTO PY3_PROTO GO_PROTO DOCS_PROTO DESC_PROTO)\n'
                                      'SRCS(value.proto)\nEND()\n')
        files['orchard/api/value.proto'] = 'syntax = "proto3";\nmessage Value {}\n'
        files['library/typescript/ts-proto-deps/ya.make'] = 'TS_PACKAGE()\nNO_LINT()\nEND()\n'
        files['library/typescript/ts-proto-deps/package.json'] = '{"name":"proto-deps"}'
        files['library/typescript/ts-proto-deps/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
        lib.tool_program(files, 'contrib/tools/protoc', 'protoc')
    if mode == 'selected':
        files['orchard/ya.make'] = 'UNION()\nBUNDLE_OUTPUT(orchard/ui package.json RENAME chosen.json)\nEND()\n'
    return files


class NotsBundleInputsTest(unittest.TestCase):
    def test_bundle_keeps_the_artifact_dependency_without_ts_peer_input_propagation(self):
        for mode in ('physical', 'workspace', 'vite'):
            with self.subTest(mode=mode):
                graph = lib.make(fixture(mode), 'orchard', '--sandboxing')
                bundle = lib.node_by_output(graph, '$(B)/orchard/payload.tar')
                artifact = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
                prepare = lib.node_by_output(graph, '$(B)/orchard/ui/package.json')
                self.assertEqual(bundle['inputs'], [
                    '$(B)/orchard/ui/output.tar', '$(S)/build/scripts/fs_tools.py',
                    '$(S)/build/scripts/process_command_files.py',
                ])
                self.assertEqual(bundle['deps'], [artifact['uid']])
                self.assertIn('$(B)/orchard/ui/package.json', artifact['inputs'])
                self.assertIn(prepare['uid'], artifact['deps'])
                if mode == 'workspace':
                    for output in ('package.json', 'output.tar'):
                        name = '$(B)/orchard/shared/' + output
                        peer = lib.node_by_output(graph, name)
                        self.assertIn(name, artifact['inputs'])
                        self.assertIn(peer['uid'], artifact['deps'])

    def test_generated_workspace_manifest_remains_a_real_ts_prerequisite(self):
        graph = lib.make(fixture('generated'), 'orchard', '--sandboxing')
        bundle = lib.node_by_output(graph, '$(B)/orchard/payload.tar')
        artifact = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
        self.assertEqual([name for name in bundle['inputs'] if name.startswith('$(B)/')], ['$(B)/orchard/ui/output.tar'])
        self.assertEqual(bundle['deps'], [artifact['uid']])
        for output in ('package.json', 'output.tar'):
            name = '$(B)/orchard/api/' + output
            producer = lib.node_by_output(graph, name)
            self.assertIn(name, artifact['inputs'])
            self.assertIn(producer['uid'], artifact['deps'])
        generated = lib.node_by_output(graph, '$(B)/orchard/api/package.json')
        self.assertIn('--ts-proto-auto-package-name', generated['cmds'][0]['cmd_args'])

    def test_selected_bundle_member_has_only_the_archive_as_a_generated_input(self):
        graph = lib.make(fixture('selected'), 'orchard', '--sandboxing')
        selected = lib.node_by_output(graph, '$(B)/orchard/chosen.json')
        artifact = lib.node_by_output(graph, '$(B)/orchard/ui/output.tar')
        self.assertEqual([name for name in selected['inputs'] if name.startswith('$(B)/')], ['$(B)/orchard/ui/output.tar'])
        self.assertEqual(selected['deps'], [artifact['uid']])
        self.assertIn('--name', selected['cmds'][0]['cmd_args'])
        self.assertIn('package.json', selected['cmds'][0]['cmd_args'])


if __name__ == '__main__':
    unittest.main()
