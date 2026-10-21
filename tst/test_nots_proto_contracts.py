import unittest

import lib
import test_nots


class NotsProtoContractsTest(unittest.TestCase):
    def test_auto_proto_workspaces_and_grpc_options(self):
        files = test_nots.NotsTests().project()
        files['web/package.json'] = '{"dependencies":{"api":"workspace:../api"}}'
        files['api/ya.make'] = (
            'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\n'
            'TS_PROTO_NON_RECURSIVE()\nSRCS(api.proto)\nPEERDIR(zeta alpha zeta)\n'
            'GRPC()\nTS_PROTO_OPT(outputJsonMethods=false)\n'
            'PROTO_ADDINCL(GLOBAL WITH_GEN imports)\nUSE_COMMON_GOOGLE_APIS()\nEND()\n'
        )
        files['api/api.proto'] = 'syntax = "proto3";\nmessage Api {}\n'
        for name in ('alpha', 'zeta'):
            files[f'{name}/ya.make'] = 'PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\nSRCS(value.proto)\nEND()\n'
            files[f'{name}/package.json'] = '{}'
            files[f'{name}/value.proto'] = 'syntax = "proto3";\nmessage Value {}\n'
        deps = 'library/typescript/ts-proto-deps'
        files[f'{deps}/ya.make'] = 'TS_PACKAGE()\nEND()\n'
        files[f'{deps}/package.json'] = '{}'
        files[f'{deps}/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\npackages: {}\n"
        lib.tool_program(files, 'contrib/tools/protoc', 'protoc')
        for explicit in (False, True):
            with self.subTest(explicit=explicit):
                if explicit:
                    files['api/ya.make'] = files['api/ya.make'].replace('GRPC()', 'GRPC()\nTS_PROTO_OPT(outputServices=none)')
                graph = lib.make(files, 'web', '-DAUTOCHECK=yes')
                prepare = lib.node_by_output(graph, '$(B)/api/package.json')['cmds'][0]['cmd_args']
                index = prepare.index('--ts-proto-workspace-peers')
                self.assertEqual(prepare[index + 1:], ['alpha', 'zeta'])
                api = lib.node_by_output(graph, '$(B)/api/output.tar')
                args = api['cmds'][-1]['cmd_args']
                self.assertIn('outputServices=none' if explicit else 'outputServices=grpc-js', args)
                if explicit:
                    self.assertNotIn('outputServices=grpc-js', args)
                for path in ('$(B)/imports', '$(S)/imports', '$(S)/contrib/libs/googleapis-common-protos'):
                    self.assertIn(path, args)
                for name in ('alpha', 'zeta'):
                    self.assertIn(f'$(B)/{name}/output.tar', api['inputs'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
