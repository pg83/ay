"""Native unit.get identity, GetSubst metadata and RESOURCE command phases."""
import base64
import hashlib
import unittest

import lib
import test_sandbox_python_task as sandbox
from test_python_srcs import python_base, objcopy_nodes, kvs


def pathid(raw):
    return base64.b32encode(hashlib.md5(raw.encode()).digest()).decode().lower().rstrip('=')


def program(files, name, body='', prelude=''):
    files[name + '/ya.make'] = (prelude + 'PY3_PROGRAM_BIN(runner)\n' + lib.BARE_MODULE
                               + 'NO_PYTHON_INCLUDES()\nPY_SRCS(entry.py)\n' + body + 'END()\n')
    files[name + '/entry.py'] = 'def main(): pass\n'


class PythonImportMaskExpressionTest(unittest.TestCase):
    def assert_resource(self, graph, name, raw, arguments, binary='runner', tag='PY3'):
        key = 'py/no_check_imports/' + pathid(raw)
        nodes = [n for n in objcopy_nodes(graph, name) if any(a.startswith(key + '=') for a in kvs(n))]
        self.assertEqual(len(nodes), 1)
        node = nodes[0]
        self.assertEqual(kvs(node), [key + '=' + arguments[0], *arguments[1:]])
        if tag is not None:
            kv = key + '="' + raw + '"'
            identity = ','.join(sorted([kv, '$S/' + name])) + tag
            digest = hashlib.md5(identity.encode()).hexdigest()[:26]
            self.assertEqual(node['outputs'], [f'$(B)/{name}/objcopy_{digest}.o'])
        linked = lib.node_by_output(graph, f'$(B)/{name}/{binary}')
        self.assertIn(node['uid'], linked['deps'])
        self.assertIn(node['outputs'][0], linked['inputs'])
        tools = [lib.node_by_output(graph, f'$(B)/tools/{p}/{p}') for p in ('rescompiler', 'rescompressor')]
        for tool in tools:
            self.assertIn(tool['uid'], node['deps'])
            self.assertIn(tool['outputs'][0], node['inputs'])
        bytecode = lib.node_by_output(graph, f'$(B)/{name}/entry.py.yapyc3')
        self.assertEqual(bytecode['inputs'][-1], f'$(S)/{name}/entry.py')
        packed = [n for n in objcopy_nodes(graph, name) if bytecode['outputs'][0] in n['inputs']
                  and n['outputs'][0] in linked['inputs']]
        self.assertEqual(len(packed), 1)
        packed = packed[0]
        self.assertIn(bytecode['uid'], packed['deps'])
        self.assertIn(packed['uid'], linked['deps'])
        return node

    def assert_skips(self, graph, name, masks):
        node = lib.node_by_output(graph, f'$(B)/{name}/test-results/import_test/meta.json')
        checker = node['cmds'][0]['cmd_args']
        actual = checker[checker.index('--checker') + 1]
        expected = 'run_pyimports --markup' + ''.join(' --skip ' + m for m in masks)
        self.assertEqual(actual.split(' --trace-dir=')[0], expected)

    def test_default_configured_empty_and_sanitizer_values(self):
        for config, flags, expected in (
            ('', [], None),
            ('NO_CHECK_IMPORTS_FOR_VALUE=fern.* birch.*\n', [], ('fern.* birch.*', ['fern.* birch.*'])),
            ('NO_CHECK_IMPORTS_FOR_VALUE=\n', [], None),
            ('NO_CHECK_IMPORTS_FOR_VALUE=fern.*\n', ['-DSANITIZER_TYPE=address'], None),
            ('SELECTED=fern.* birch.*\nNO_CHECK_IMPORTS_FOR_VALUE=$SELECTED\n', [], ('$SELECTED', ['fern.*birch.*'])),
            (r'NO_CHECK_IMPORTS_FOR_VALUE=${"a\\b\"c"}' + '\n', [], (r'${"a\\b\"c"}', ['a\\b"c'])),
            ("NO_CHECK_IMPORTS_FOR_VALUE=\"\" ''\n", [], ("\"\" ''", [" ''"])),
            ('NO_CHECK_IMPORTS_FOR_VALUE="fern.* birch.*"\n', [], ('"fern.* birch.*"', ['fern.*', 'birch.*'])),
        ):
            with self.subTest(config=config, flags=flags):
                files = python_base()
                files['build/ymake_conf.py'] = lib.ymake_conf(extra=config)
                program(files, 'fern/configured')
                graph = lib.make(files, 'fern/configured', *flags)
                if expected:
                    self.assert_resource(graph, 'fern/configured', *expected)
                else:
                    self.assertFalse(any(v.startswith('py/no_check_imports/') for n in objcopy_nodes(graph, 'fern/configured') for v in kvs(n)))

    def test_source_macro_replaces_captures_and_late_bare_references(self):
        for body, raw, final, skips in (
            ('NO_CHECK_IMPORTS($LATE)\nSET(LATE)\n', '$LATE', '', ['*']),
            ('SET(UNRELATED first.*)\nSET_APPEND(UNRELATED second.*)\nNO_CHECK_IMPORTS($UNRELATED)\n', 'first.* second.*', 'first.* second.*', ['first.*', 'second.*']),
            ('NO_CHECK_IMPORTS(first.*)\nNO_CHECK_IMPORTS(second.*)\n', 'second.*', 'second.*', ['second.*']),
            ('NO_CHECK_IMPORTS()\nNO_CHECK_IMPORTS(custom.*)\n', 'custom.*', 'custom.*', ['custom.*']),
            ('SET(NO_CHECK_IMPORTS_FOR_VALUE source.* leaf.*)\n', 'source.* leaf.*', 'source.* leaf.*', ['source.*', 'leaf.*']),
            ('NO_CHECK_IMPORTS($LATE ${ABSENT})\nSET(LATE fern.* birch.*)\n', '$LATE', 'fern.*birch.*', ['fern.*', 'birch.*']),
            ('SET(EARLY before.*)\nNO_CHECK_IMPORTS($EARLY ${EARLY})\nSET(EARLY after.*)\n', 'before.* before.*', 'before.* before.*', ['before.*', 'before.*']),
            ('SET(NO_CHECK_IMPORTS_FOR_VALUE ${"$"}CASH)\n', '${"$"}CASH', '$CASH', ['$CASH']),
            ('NO_CHECK_IMPORTS(${"$"}CASH)\n', '${"$"}CASH', '$CASH', ['$CASH']),
            ('NO_CHECK_IMPORTS("fern.* birch.*")\n', 'fern.* birch.*', 'fern.* birch.*', ['fern.*', 'birch.*']),
            ('NO_CHECK_IMPORTS(pre.$LATE)\nSET(LATE one.* two.*)\n', 'pre.$LATE', 'pre.one.*two.*', ['pre.one.*', 'two.*']),
        ):
            with self.subTest(body=body):
                files = python_base()
                program(files, 'fern/source', body)
                graph = lib.make(files, 'fern/source', '-t')
                self.assert_resource(graph, 'fern/source', raw, [final])
                self.assert_skips(graph, 'fern/source', skips)

    def test_append_keeps_inherited_binding_and_raw_hash(self):
        for prelude, body, raw, final in (
            ('', 'SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE)\n', '$NO_CHECK_IMPORTS_FOR_VALUE ', 'None '),
            ('', 'SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE $LATE)\nSET(LATE fern.* birch.*)\n', '$NO_CHECK_IMPORTS_FOR_VALUE $LATE', 'Nonefern.*birch.*fern.*birch.* fern.*birch.*'),
            ('', 'SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE fern.*)\n', '$NO_CHECK_IMPORTS_FOR_VALUE fern.*', 'Nonefern.*fern.* fern.*'),
            ('', 'SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE fern.*)\nSET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE birch.*)\n', '$NO_CHECK_IMPORTS_FOR_VALUE fern.* birch.*', 'Nonefern.*birch.*fern.*birch.* fern.* birch.*'),
            ('SET(NO_CHECK_IMPORTS_FOR_VALUE prelude.*)\n', '', '$NO_CHECK_IMPORTS_FOR_VALUE prelude.*', 'Noneprelude.*prelude.* prelude.*'),
            ('SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE prelude.*)\n', '', '$NO_CHECK_IMPORTS_FOR_VALUE prelude.*', 'Noneprelude.*prelude.* prelude.*'),
            ('', 'NO_CHECK_IMPORTS(source.*)\nSET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE leaf.*)\n', 'source.* leaf.*', 'source.* leaf.*'),
        ):
            with self.subTest(prelude=prelude, body=body):
                files = python_base()
                program(files, 'fern/appended', body, prelude)
                graph = lib.make(files, 'fern/appended')
                self.assert_resource(graph, 'fern/appended', raw, [final])

    def test_inherited_configuration_references_remain_late_after_appending(self):
        for reference in ('$SELECTED', '${SELECTED}'):
            with self.subTest(reference=reference):
                files = python_base()
                files['build/ymake_conf.py'] = lib.ymake_conf(extra='SELECTED=before.*\nNO_CHECK_IMPORTS_FOR_VALUE=' + reference + '\n')
                program(files, 'fern/inherited', 'SET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE fern.*)\nSET(SELECTED after.*)\n')
                graph = lib.make(files, 'fern/inherited', '-t')
                self.assert_resource(graph, 'fern/inherited', '$NO_CHECK_IMPORTS_FOR_VALUE fern.*', ['after.*fern.*fern.* fern.*'])
                self.assert_skips(graph, 'fern/inherited', ['after.*', 'fern.*'])

    def test_shared_lexical_include_has_independent_bindings_in_both_root_orders(self):
        files = python_base()
        files['shared/mask.inc'] = 'NO_CHECK_IMPORTS($LATE ${CAPTURED})\nSET(LATE fern.* birch.*)\n'
        for name, captured in (('first', 'one.*'), ('second', 'two.*')):
            program(files, 'fern/' + name, 'SET(CAPTURED ' + captured + ')\nINCLUDE(${ARCADIA_ROOT}/shared/mask.inc)\n')
        plans = []
        for order in (('first', 'second'), ('second', 'first')):
            graph = lib.make(files, *['fern/' + p for p in order], '-t')
            plan = []
            for name, captured in (('first', 'one.*'), ('second', 'two.*')):
                node = self.assert_resource(graph, 'fern/' + name, '$LATE ' + captured, ['fern.*birch.* ' + captured])
                self.assert_skips(graph, 'fern/' + name, ['fern.*', 'birch.*', captured])
                plan.append((node['cmds'], node['inputs'], node['outputs']))
            plans.append(plan)
        self.assertEqual(*plans)

    def test_sandbox_config_appends_after_program_reset_without_mutating_library(self):
        raw = '$NO_CHECK_IMPORTS_FOR_VALUE api.* kernel.* library.* sky*'
        for flags, inherited in (([], 'None'), (['-DNO_CHECK_IMPORTS_FOR_VALUE=custom.*'], 'custom.*'),
                                 (['-DSANITIZER_TYPE=address'], 'None'), (['-DNO_CHECK_IMPORTS_FOR_VALUE='], '')):
            with self.subTest(flags=flags):
                files = sandbox.fixture()
                name = 'work/jobs/InspectDelta'
                files[name + '/ya.make'] = files[name + '/ya.make'].replace('PY_SRCS(__init__.py)', 'PY_SRCS(entry.py)')
                files[name + '/entry.py'] = ''
                graph = lib.make(files, name, '-t', *flags)
                final = inherited + 'api.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*'
                # T105 separately owns the inherited effective resource tag.
                node = self.assert_resource(graph, name, raw, [final], 'InspectDelta', tag=None)
                library = lib.node_by_output(graph, '$(B)/' + name + '/libpy3work-jobs-InspectDelta.global.a')
                self.assertNotIn(node['uid'], library['deps'])
                self.assert_skips(graph, name, ([inherited] if inherited else []) + ['api.*', 'kernel.*', 'library.*', 'sky*'])

    def test_source_sanitizer_assignments_recalculate_only_the_import_mask_condition(self):
        for body, present in (
            ('SET(SANITIZER_TYPE address)\n', False),
            ('SET(SANITIZER_TYPE address)\nSET(SANITIZER_TYPE no)\n', True),
            ('ENABLE(SANITIZER_TYPE)\n', False),
            ('ENABLE(SANITIZER_TYPE)\nDISABLE(SANITIZER_TYPE)\n', True),
            ('SET_APPEND(SANITIZER_TYPE address)\n', False),
        ):
            with self.subTest(body=body):
                files = sandbox.fixture()
                name = 'work/jobs/InspectDelta'
                files[name + '/ya.make'] = files[name + '/ya.make'].replace('END()', body + 'END()')
                graph = lib.make(files, name)
                values = [v for n in objcopy_nodes(graph, name) for v in kvs(n) if v.startswith('py/no_check_imports/')]
                self.assertEqual(len(values), int(present))
                if present:
                    self.assertEqual(values[0].split('=', 1)[1], 'Noneapi.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*')
        for body, raw, expected in (
            ('NO_CHECK_IMPORTS(custom.*)\nSET(SANITIZER_TYPE address)\n', None, None),
            ('SET(SANITIZER_TYPE address)\nNO_CHECK_IMPORTS(custom.*)\n', 'custom.*', ['custom.*']),
            ('SET(SANITIZER_TYPE address)\nSET(SANITIZER_TYPE no)\n', None, None),
        ):
            with self.subTest(body=body):
                files = python_base()
                program(files, 'fern/sanitized', body)
                graph = lib.make(files, 'fern/sanitized')
                if raw:
                    self.assert_resource(graph, 'fern/sanitized', raw, expected)
                else:
                    self.assertFalse(any(v.startswith('py/no_check_imports/') for n in objcopy_nodes(graph, 'fern/sanitized') for v in kvs(n)))

    def test_runtime_roots_and_resource_owners_bind_only_in_the_final_command(self):
        files = python_base()
        program(files, 'fern/runtime', r'NO_CHECK_IMPORTS($ARCADIA_ROOT ${ARCADIA_BUILD_ROOT} \$ABSENT)' + '\n')
        graph = lib.make(files, 'fern/runtime')
        raw = '$ARCADIA_ROOT ${ARCADIA_BUILD_ROOT} $ABSENT'
        generated = next(n for n in graph['graph'] if any(a.startswith('py/no_check_imports/') for c in n['cmds'] for a in c['cmd_args']))
        key = 'py/no_check_imports/' + pathid(raw)
        self.assertEqual(generated['cmds'][0]['cmd_args'][-2:], ['-', key + '=$(S) $(B) '])
        tool = lib.node_by_output(graph, '$(B)/tools/rescompiler/rescompiler')
        self.assertIn(tool['uid'], generated['deps'])
        self.assertIn(tool['outputs'][0], generated['inputs'])
        compiled = next(n for n in graph['graph'] if generated['outputs'][0] in n['inputs'])
        self.assertIn(generated['uid'], compiled['deps'])
        self.assertTrue(compiled['outputs'][0].endswith('_raw.auxcpp.o'))
        linked = lib.node_by_output(graph, '$(B)/fern/runtime/runner')
        self.assertIn(compiled['uid'], linked['deps'])
        self.assertIn(compiled['outputs'][0], linked['inputs'])
        for own in (True, False):
            with self.subTest(own=own):
                files = python_base()
                declaration = 'DECLARE_EXTERNAL_RESOURCE(MINT sbr:731)\n'
                program(files, 'fern/runtime', 'NO_CHECK_IMPORTS($MINT_RESOURCE_GLOBAL/mask)\n' +
                        (declaration if own else 'PEERDIR(fern/assets)\n'))
                files['fern/assets/ya.make'] = 'RESOURCES_LIBRARY()\n' + declaration + 'END()\n'
                graph = lib.make(files, 'fern/runtime')
                self.assert_resource(graph, 'fern/runtime', '$MINT_RESOURCE_GLOBAL/mask', ['$(B)/resources/MINT/mask'])
                resource = lib.node_by_output(graph, '$(B)/resources/MINT')
                self.assertIn('sbr:731', resource['cmds'][0]['cmd_args'])

    def test_native_rejects_source_recursion_and_invalid_final_bindings(self):
        for extra, body, error in (
            ('NO_CHECK_IMPORTS_FOR_VALUE=$ALIAS\nALIAS=$ALIAS\n', '', 'self-contradictory'),
            ('NO_CHECK_IMPORTS_FOR_VALUE=fern.* birch.*\n', 'SET(NO_CHECK_IMPORTS_FOR_VALUE pre.$NO_CHECK_IMPORTS_FOR_VALUE)\n', 'unexpected multiargument substitution'),
            ('NO_CHECK_IMPORTS_FOR_VALUE=$VAR0\n' + ''.join('VAR%d=$VAR%d\n' % (i, i+1) for i in range(60)) + 'VAR60=leaf.*\n', '', 'configuration command expansion is too deep'),
        ):
            with self.subTest(extra=extra, body=body):
                files = python_base()
                files['build/ymake_conf.py'] = lib.ymake_conf(extra=extra)
                program(files, 'fern/rejected', body)
                result = lib.make_process(files, 'fern/rejected')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
        files = sandbox.fixture()
        name = 'work/jobs/InspectDelta'
        files[name + '/ya.make'] = files[name + '/ya.make'].replace('END()', 'NO_CHECK_IMPORTS($NO_CHECK_IMPORTS_FOR_VALUE)\nEND()')
        result = lib.make_process(files, name)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Macro subst exceeded the limit 128', result.stderr)

    def test_empty_and_none_source_values_disable_only_their_defined_phase(self):
        for body, checked in (('NO_CHECK_IMPORTS()\n', False), ('NO_CHECK_IMPORTS(None)\n', True),
                              ('SET(NO_CHECK_IMPORTS_FOR_VALUE)\n', False)):
            with self.subTest(body=body):
                files = python_base()
                program(files, 'fern/empty', body)
                graph = lib.make(files, 'fern/empty', '-t')
                self.assertFalse(any(v.startswith('py/no_check_imports/') for n in objcopy_nodes(graph, 'fern/empty') for v in kvs(n)))
                checks = [n for n in graph['graph'] if '$(B)/fern/empty/test-results/import_test/meta.json' in n['outputs']]
                self.assertEqual(len(checks), int(checked))


if __name__ == '__main__':
    unittest.main(verbosity=2)
