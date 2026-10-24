"""Python2 plugin-phase source spelling and real BUILD producer ownership."""
import base64
import collections
import hashlib
import unittest

import lib
from test_python2 import python2_base
from test_resource_objcopy import section


MODULE = 'grove/ledger'


def fixture(token='${BINDIR}/generated.py', flags='', producer='copy'):
    files = python2_base()
    command = 'COPY_FILE(seed.py generated.py)\n'
    if producer == 'python':
        command = 'RUN_PYTHON3(make.py IN seed.py OUT_NOAUTO generated.py)\n'
        files[MODULE + '/make.py'] = '# generator source\n'
    files[MODULE + '/seed.py'] = 'value=17\n'
    files[MODULE + '/ya.make'] = (
        'PY2_LIBRARY(ledger)\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\n' + flags + command
        + 'PY_SRCS(' + token + ')\nEND()\n'
    )
    return files


class Python2BuildSourcesTest(unittest.TestCase):
    def check(self, token='${BINDIR}/generated.py', flags='', producer='copy'):
        graph = lib.make(fixture(token, flags, producer), MODULE, '--sandboxing',
                         '--target-platform', 'default-linux-x86_64')
        path, separator, alias = token.partition('=')
        logical = alias if separator else 'grove.ledger.' + path.removesuffix('.py').replace('/', '.')
        generated = '$(B)/grove/ledger/generated.py'
        source = lib.node_by_output(graph, generated)
        self.assertIn('$(S)/grove/ledger/seed.py', source['inputs'])
        self.assertEqual(source['kv']['p'], 'CP' if producer == 'copy' else 'PY')
        suffix = '' if '/' not in path else '.' + base64.b32encode(
            hashlib.md5(b'$S/grove/ledger').digest()).decode().lower()[:4]
        bytecode_path = generated + suffix + '.yapyc'
        with_py = 'PYBUILD_NO_PY)' not in flags
        with_pyc = 'PYBUILD_NO_PYC)' not in flags
        resource_inputs = ([generated] if with_py else []) + ([bytecode_path] if with_pyc else [])
        raw = '${ARCADIA_BUILD_ROOT}' in path
        resources = [node for node in graph['graph']
                     if any(output.endswith('_raw.auxcpp') if raw else '/objcopy_' in output
                            for output in node['outputs'])
                     and all(item in node['inputs'] for item in resource_inputs)]
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        args = resource['cmds'][0]['cmd_args']
        expected_keys = (['/py_modules/' + logical] if with_py else []) + (['/py_code/' + logical] if with_pyc else [])
        expanded_logical = logical.replace('${BINDIR}', '$(B)/grove/ledger').replace(
            '${ARCADIA_BUILD_ROOT}', '$(B)')
        kv = 'resfs/src//py_modules/' + expanded_logical + '=grove/ledger/generated.py'
        if raw:
            expected_args = ['$(B)/tools/rescompiler/rescompiler', resource['outputs'][0]]
            if with_py:
                expected_args += [generated, '-/py_modules/' + expanded_logical, '-', kv]
            if with_pyc:
                expected_args += [bytecode_path, '-/py_code/' + expanded_logical]
            self.assertEqual(args, expected_args)
            resource_tools = ['$(B)/tools/rescompiler/rescompiler']
        else:
            self.assertEqual(section(args, '--inputs'), resource_inputs)
            self.assertEqual([base64.b64decode(key).decode() for key in section(args, '--keys')], expected_keys)
            resource_tools = ['$(B)/tools/rescompiler/rescompiler', '$(B)/tools/rescompressor/rescompressor',
                              '$(S)/build/scripts/objcopy.py']
        self.assertEqual(collections.Counter(resource['inputs']), collections.Counter(resource_inputs + resource_tools))
        by_uid = {node['uid']: node for node in graph['graph']}
        expected_direct = resource_tools if raw else resource_tools[:2]
        if with_py:
            expected_direct.append(generated)
            if not raw:
                self.assertEqual(section(args, '--kvs'), [kv])
        if with_pyc:
            bytecode = lib.node_by_output(graph, bytecode_path)
            src_x = path.replace('${BINDIR}', '$(B)/grove/ledger').replace('${ARCADIA_BUILD_ROOT}/', '')
            self.assertEqual(bytecode['cmds'][0]['cmd_args'], [
                '$(B)/tools/py2cc/py2cc', src_x + '-', generated, bytecode_path,
            ])
            self.assertEqual(collections.Counter(bytecode['inputs']), collections.Counter([
                generated, '$(B)/tools/py2cc/py2cc',
            ]))
            self.assertIn(source['uid'], bytecode['deps'])
            expected_direct.append(bytecode_path)
        else:
            self.assertFalse(any(output.endswith('.yapyc') for node in graph['graph'] for output in node['outputs']))
        self.assertEqual(collections.Counter(output for uid in resource['deps'] for output in by_uid[uid]['outputs']),
                         collections.Counter(expected_direct))
        archive = lib.node_by_output(graph, '$(B)/grove/ledger/libpyledger.global.a')
        member = resource
        if raw:
            member = lib.node_by_output(graph, resource['outputs'][0] + '.o')
            self.assertIn(resource['uid'], member['deps'])
            self.assertIn(resource['outputs'][0], member['inputs'])
        self.assertIn(member['uid'], archive['deps'])
        self.assertIn(member['outputs'][0], archive['inputs'])
        self.assertIn(archive['uid'], graph['result'])
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), set(by_uid))
        self.assertTrue(all(set(node['deps']) <= set(by_uid) for node in graph['graph']))
        return resource

    def test_rooted_copy_matches_native_bytecode_and_raw_resource_contract(self):
        resource = self.check()
        self.assertEqual(resource['outputs'], ['$(B)/grove/ledger/objcopy_dc250580d0ab0ca348a6ec778c.o'])

    def test_bare_and_build_root_spellings_keep_distinct_srcx_and_keys(self):
        for token in ('generated.py', '${ARCADIA_BUILD_ROOT}/grove/ledger/generated.py'):
            with self.subTest(token=token):
                resource = self.check(token)
                if token.startswith('${ARCADIA_BUILD_ROOT}'):
                    self.assertEqual(resource['outputs'], [
                        '$(B)/grove/ledger/2e577ea098d626a56c081b7572_raw.auxcpp',
                    ])

    def test_explicit_module_alias_keeps_the_real_generated_source(self):
        self.check('${BINDIR}/generated.py=vendor.records')

    def test_source_only_and_bytecode_only_keep_actual_consumers(self):
        for flags in ('ENABLE(PYBUILD_NO_PYC)\n', 'ENABLE(PYBUILD_NO_PY)\n'):
            with self.subTest(flags=flags):
                self.check(flags=flags)

    def test_python_generator_uses_its_real_registered_output(self):
        self.check(producer='python')

    def test_source_only_missing_build_producer_still_fails(self):
        files = fixture(flags='ENABLE(PYBUILD_NO_PYC)\n')
        files[MODULE + '/ya.make'] = files[MODULE + '/ya.make'].replace('COPY_FILE(seed.py generated.py)\n', '')
        result = lib.make_process(files, MODULE, '--sandboxing')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cannot find source file', result.stderr)
        self.assertIn('grove/ledger/generated.py', result.stderr)
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
