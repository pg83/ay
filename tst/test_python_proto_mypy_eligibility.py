import unittest

import lib
from test_proto_py2_context import proto_world
from test_python_proto import MYPY, PROTOC, WRAPPER, args


PLUGIN = '$(B)/tools/parcel_plugin/parcel_plugin'


def fixture(module='PY3_LIBRARY', body='', version='py3', event=False):
    files = proto_world()
    extension = 'ev' if event else 'proto'
    files['maple/ya.make'] = (
        module + '(maple)\n' + lib.BARE_MODULE + 'NO_CHECK_IMPORTS()\n' + body
        + 'PEERDIR(build/platform/python/ymake_python3)\n'
        + ('SRCS' if module == 'PROTO_LIBRARY' else 'PY_SRCS') + f'(packet.{extension})\nEND()\n'
    )
    files[f'maple/packet.{extension}'] = 'syntax = "proto2";\nmessage Packet { optional string text = 1; }\n'
    files['client/ya.make'] = ('PY2_PROGRAM' if version == 'py2' else 'PY3_PROGRAM') + '()\n' + lib.BARE_MODULE + 'NO_CHECK_IMPORTS()\nPEERDIR(maple)\nEND()\n'
    for peer in ('library/python/pytest', 'sandbox/sdk2', 'sandbox/bin', 'sandbox/taskbox/worker'):
        files[peer + '/ya.make'] = 'PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nEND()\n'
    files['build/platform/python/ymake_python3/ya.make'] = 'RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:31)\nEND()\n'
    lib.tool_program(files, 'tools/parcel_plugin', 'parcel_plugin')
    return files


class PythonProtoMypyEligibilityTest(unittest.TestCase):
    def check_producer(self, graph, version, mypy, plugin=False, event=False):
        suffixes = ['_ev_pb2.py' if event else '_pb2.py']
        if plugin:
            suffixes.append('_stub.py')
        if mypy:
            suffixes.append('_pb2.pyi')
        stem = f'$(B)/maple/packet__int{version}__'
        node = lib.node_by_output(graph, stem + suffixes[0])
        self.assertEqual(node['outputs'], [stem + suffix for suffix in suffixes])
        command = args(node)
        self.assertEqual(command[:5], ['$(B)/resources/YMAKE_PYTHON3/bin/python3', WRAPPER, '--py_ver', version, '--suffixes'])
        self.assertEqual(command[5:command.index('--input')], suffixes)
        extension = 'ev' if event else 'proto'
        self.assertEqual(command[command.index('--input'):command.index('--')], ['--input', f'maple/packet.{extension}', '--ns', '/'])
        compiler = '$(B)/contrib/tools/protoc_old/protoc_old' if version == 'py2' else PROTOC
        required = [WRAPPER, compiler, f'$(S)/maple/packet.{extension}']
        if mypy:
            required.append(MYPY)
            self.assertIn(f'--plugin=protoc-gen-mypy={MYPY}', command)
            self.assertIn('--mypy_out=$(B)/', command)
        else:
            self.assertFalse(any(arg.startswith('--mypy_out=') for arg in command))
        if plugin:
            required.append(PLUGIN)
            self.assertIn(f'--plugin=protoc-gen-parcel={PLUGIN}', command)
            self.assertIn('--parcel_out=$(B)/', command)
        self.assertEqual(set(node['inputs']), set(required))
        for tool in [compiler, *([MYPY] if mypy else []), *([PLUGIN] if plugin else [])]:
            self.assertIn(lib.node_by_output(graph, tool)['uid'], node['deps'])
        self.assertIn(lib.node_by_output(graph, '$(B)/resources/YMAKE_PYTHON3')['uid'], node['deps'])
        self.assertEqual(node['kv']['p'], 'PB')
        for suffix in suffixes:
            self.assertEqual(node['kv']['ext_out_name_for_packet__int' + version + '__' + suffix], 'packet' + suffix)
        return node

    def test_tests_and_plain_program_bases_do_not_enable_mypy(self):
        for module, version in [('PY3TEST', 'py3'), ('PY2TEST', 'py2'), ('PY2_PROGRAM', 'py2'), ('PY3_PROGRAM_BIN', 'py3')]:
            with self.subTest(module=module):
                self.check_producer(lib.make(fixture(module, version=version), 'maple'), version, False)

    def test_library_ancestors_and_python3_program_self_library_keep_mypy(self):
        for module, version in [('PY3_LIBRARY', 'py3'), ('PY2_LIBRARY', 'py2'), ('PY23_LIBRARY', 'py3'), ('PY3_PROGRAM', 'py3')]:
            with self.subTest(module=module):
                target = 'client' if version == 'py2' else 'maple'
                self.check_producer(lib.make(fixture(module, version=version), target), version, True)

    def test_proto_library_python_variants_keep_mypy(self):
        for version, tag in [('py3', 'PY3_PROTO'), ('py2', 'PY_PROTO')]:
            with self.subTest(version=version):
                files = fixture('PROTO_LIBRARY', f'ONLY_TAGS({tag})\nNO_OPTIMIZE_PY_PROTOS()\n', version)
                self.check_producer(lib.make(files, 'client' if version == 'py2' else 'maple'), version, True)

    def test_effective_scalar_and_no_mypy_follow_last_source_assignment(self):
        for setting, enabled in [('NO_MYPY()\n', False), ('DISABLE(PY_PROTO_MYPY_ENABLED)\n', False), ('SET(PY_PROTO_MYPY_ENABLED no)\n', False), ('SET(PY_PROTO_MYPY_ENABLED true)\n', False), ('NO_MYPY()\nENABLE(PY_PROTO_MYPY_ENABLED)\n', True), ('DISABLE(PY_PROTO_MYPY_ENABLED)\nSET(PY_PROTO_MYPY_ENABLED yes)\n', True)]:
            with self.subTest(setting=setting):
                self.check_producer(lib.make(fixture(body=setting), 'maple'), 'py3', enabled)
        self.check_producer(lib.make(fixture('PY3TEST', 'ENABLE(PY_PROTO_MYPY_ENABLED)\n'), 'maple'), 'py3', False)

    def test_cli_scalar_is_the_initial_module_setting(self):
        self.check_producer(lib.make(fixture(), 'maple', '-DPY_PROTO_MYPY_ENABLED=no'), 'py3', False)
        self.check_producer(lib.make(fixture(body='ENABLE(PY_PROTO_MYPY_ENABLED)\n'), 'maple', '-DPY_PROTO_MYPY_ENABLED=no'), 'py3', True)

    def test_sandbox_binary_and_library_units_have_distinct_eligibility(self):
        for module, selection, version, enabled in [('SANDBOX_PY23_3_TASK', 'SB_TASK_BIN', 'py3', False), ('SANDBOX_PY23_TASK', 'SB_TASK_BIN', 'py2', False), ('SANDBOX_PY23_3_TASK', 'PY3', 'py3', True), ('SANDBOX_PY23_3_TASK', 'PY2', 'py2', True)]:
            with self.subTest(module=module, selection=selection):
                files = fixture(module, f'ONLY_TAGS({selection})\n', version)
                self.check_producer(lib.make(files, 'client' if selection == 'PY2' else 'maple'), version, enabled)

    def test_custom_plugins_are_independent_of_mypy_eligibility(self):
        body = 'PY_PROTO_PLUGIN(parcel _stub.py tools/parcel_plugin)\n'
        for module, enabled in [('PY3_LIBRARY', True), ('PY3TEST', False)]:
            with self.subTest(module=module):
                self.check_producer(lib.make(fixture(module, body), 'maple'), 'py3', enabled, plugin=True)

    def test_event_producer_keeps_its_separate_tool_and_output_contract(self):
        self.check_producer(lib.make(fixture(event=True), 'maple'), 'py3', False, event=True)


if __name__ == '__main__':
    unittest.main(verbosity=2)
