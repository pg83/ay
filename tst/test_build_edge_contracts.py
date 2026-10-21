import unittest

import lib
import test_perl_sdk
import test_resource_files
import test_yql


def library(body):
    return f'LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n'


class BuildEdgeContractsTest(unittest.TestCase):
    def test_perl_ubuntu18_and_ubuntu20_layouts(self):
        for sdk, version, prefix in (('ubuntu-18', '5.26', 'perl5.26'), ('ubuntu-20', '5.30', 'usr')):
            with self.subTest(sdk=sdk):
                resource = 'SYSTEM_PERL_' + version.replace('.', '')
                files = test_perl_sdk.sdk_tree(f'SET(PERL_SDK {sdk})\nDECLARE_EXTERNAL_RESOURCE({resource} sbr:1)')
                graph = lib.make(files, 'app')
                args = lib.node_by_output(graph, '$(B)/app/a.cpp.o')['cmds'][0]['cmd_args']
                self.assertIn(f'-I$(B)/resources/{resource}/{prefix}/lib/x86_64-linux-gnu/perl/{version}/CORE', args)

    def test_local_perl_library_change_recalculates_link_flags(self):
        files = test_perl_sdk.sdk_tree('DISABLE(USE_PERL_SYSTEM)\nENABLE(USE_LOCAL_TOOLS)\nSET(LOCAL_PERL_LIBS /new/lib)\nSET_APPEND(LOCAL_PERL_LIBS)')
        graph = lib.make(files, 'app', '-DLOCAL_PERL_ARCHLIB=/local/arch', '-DLOCAL_PERL_LIBS=/old/lib')
        node = lib.node_by_output(graph, '$(B)/app/app')
        args = next(c['cmd_args'] for c in node['cmds'] if '$(S)/build/scripts/link_exe.py' in c['cmd_args'])
        self.assertIn('-L/new/lib', args)
        self.assertNotIn('-L/old/lib', args)

    def test_yql_prebuilt_tool_requires_a_resolved_primary_output(self):
        for primary in ('', 'PRIMARY_OUTPUT(${UNKNOWN})'):
            with self.subTest(primary=primary):
                result = lib.make_process({'tool/ya.make': f'YQL_PREBUILT_TOOL()\n{primary}\nEND()\n'}, 'tool')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('requires a resolved PRIMARY_OUTPUT', result.stderr)

    def test_mixed_yql_abi_requests_add_conflict_peer(self):
        files = test_yql.udf_fixture()
        files.update({'app/ya.make': library('YQL_STABLE_ABI_VERSION()\nYQL_CURRENT_ABI_VERSION()\nSRCS(main.cpp)'),
                      'app/main.cpp': '',
                      'yql/essentials/public/udf/abi_conflict/ya.make': library('CFLAGS(GLOBAL -DABI_CONFLICT)')})
        graph = lib.make(files, 'app')
        args = lib.node_by_output(graph, '$(B)/app/main.cpp.o')['cmds'][0]['cmd_args']
        self.assertIn('-DABI_CONFLICT', args)

    def test_tools_conf_requires_the_named_configuration_file(self):
        result = lib.make_process(test_resource_files.base_files('YA_TOOLS_CONF(missing)\n'), 'lib')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('YA_TOOLS_CONF: file missing/ya.conf.json not found', result.stderr)

    def test_unknown_global_source_is_diagnosed(self):
        result = lib.make_process({'app/ya.make': library('SRCS(GLOBAL unknown.data)'), 'app/unknown.data': ''}, 'app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unsupported source extension', result.stderr)

    def test_explicit_fake_module_and_peer_propagation(self):
        for setting in ('SET(FAKE_MODULE yes)', 'SET(NEED_ADD_FAKE_SRC no)', 'SET(PASS_PEERS no)'):
            with self.subTest(setting=setting):
                files = {'app/ya.make': library(setting + '\nSRCS(main.cpp)'), 'app/main.cpp': ''}
                graph = lib.make(files, 'app')
                lib.node_by_output(graph, '$(B)/app/libapp.a')


if __name__ == '__main__':
    unittest.main(verbosity=2)
