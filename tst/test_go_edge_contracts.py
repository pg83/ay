import unittest

import lib
from test_go_package import go_world, go_module, go_tool_args, between
from test_go_tests import go_proto_world


class GoEdgeContractsTest(unittest.TestCase):
    def test_default_common_google_apis_are_linked(self):
        files = go_proto_world()
        apis = (
            'api/annotations api/configchange api/distribution api/expr/v1alpha1 api/expr/v1beta1 '
            'api/httpbody api/label api/metric api/monitoredres api/serviceconfig '
            'iam/admin/v1 iam/credentials/v1 iam/v1 iam/v1/logging logging/type logging/v2 '
            'rpc/code rpc/errdetails rpc/status type/calendarperiod type/color type/date '
            'type/date_range type/datetime type/date_time_range type/dayofweek type/decimal '
            'type/expr type/fraction type/interval type/latlng type/localized_text type/money '
            'type/month type/phone_number type/postaladdress type/quaternion type/timeofday'
        ).split()
        peers = ['vendor/google.golang.org/genproto/googleapis/' + api for api in apis] + [
            'vendor/cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb',
            'vendor/cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb',
            'vendor/cloud.google.com/go/longrunning/autogen/longrunningpb',
        ]
        for peer in peers:
            go_module(files, peer)
        files['contrib/libs/googleapis-common-protos/ya.make'] = 'PROTO_LIBRARY()\nEND()\n'
        files['app/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nUSE_COMMON_GOOGLE_APIS()\nSRCS(api.proto)\nEND()\n'
        files['app/api.proto'] = 'syntax = "proto3";\n'
        graph = lib.make(files, 'app')
        app = lib.node_by_output(graph, '$(B)/app/app.a')
        for peer in peers:
            output = f'$(B)/{peer}/{peer.rsplit("/", 1)[-1]}.a'
            self.assertIn(output, app['inputs'])
            self.assertIn(lib.node_by_output(graph, output)['uid'], app['deps'])

    def test_go_proto_ignores_cpp_and_python_generators(self):
        files = go_proto_world()
        files['api/ya.make'] = ('PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSET(PROTO_NAMESPACE api)\n'
            'SRCS(api.proto)\nGENERATE_ENUM_SERIALIZATION(unused.h)\nYMAPS_SPROTO()\nPY_SRCS(unused.py)\nEND()\n')
        files['api/api.proto'] = 'syntax = "proto3";\nmessage Api {}\n'
        graph = lib.make(files, 'api')
        node = lib.node_by_output(graph, '$(B)/api/api.pb.go')
        self.assertIn('$(S)/api/api.proto', node['inputs'])
        self.assertFalse(any('unused' in p for n in graph['graph'] for p in n['inputs'] + n['outputs']))

    def test_native_gateway_options_retain_go_peer_dependencies(self):
        files = go_world()
        for version, macro, option in (
            ('v1', 'GO_GRPC_GATEWAY_SWAGGER_SRCS', 'protoc-gen-swagger/options'),
            ('v2', 'GO_GRPC_GATEWAY_V2_OPENAPI_SRCS', 'v2/protoc-gen-openapiv2/options'),
        ):
            peer = 'vendor/github.com/grpc-ecosystem/grpc-gateway/' + option
            go_module(files, peer)
            files[f'{version}/ya.make'] = f'PROGRAM()\n{lib.BARE_MODULE}{macro}(unused.proto)\nSRCS(main.cpp)\nEND()\n'
            files[f'{version}/main.cpp'] = ''
        graph = lib.make(files, 'v1', 'v2')
        for version in ('v1', 'v2'):
            lib.node_by_output(graph, f'$(B)/{version}/main.cpp.o')
        for option in ('protoc-gen-swagger/options', 'v2/protoc-gen-openapiv2/options'):
            lib.node_by_output(graph, f'$(B)/vendor/github.com/grpc-ecosystem/grpc-gateway/{option}/options.a')
        self.assertFalse(any('.swagger.json' in p for n in graph['graph'] for p in n['outputs']))

    def test_native_global_archive_reaches_go_linker(self):
        files = go_world()
        go_module(files, 'app', module='GO_PROGRAM', macros='PEERDIR(native)\n')
        files['native/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}SRCS(GLOBAL global.cpp)\nEND()\n'
        files['native/global.cpp'] = 'int global;\n'
        graph = lib.make(files, 'app')
        app = lib.node_by_output(graph, '$(B)/app/app')
        linker = between(go_tool_args(app), '++extldflags', '++peers')
        expected = 'native/libnative.global.a'
        self.assertIn(expected, linker)
        self.assertIn('$(B)/' + expected, app['inputs'])
        self.assertLess(linker.index('-Wl,--whole-archive'), linker.index(expected))
        self.assertLess(linker.index(expected), linker.index('-Wl,--no-whole-archive'))

    def test_missing_stdlib_version_is_diagnosed_without_panic(self):
        files = go_world()
        files['build/conf/go.conf'] = '# no configured standard library\n'
        go_module(files, 'app', module='GO_PROGRAM')
        result = lib.make_process(files, 'app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('__unsupported_go_std_library_version_', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
