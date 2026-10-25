"""Shared ordinary SourceFile edges, proved against native ymake incl_fixer.cpp.

DONT_RESOLVE prevents new resolution; it does not hide already established
parent-file edges. Native final export observes these edges in either root order.
"""
import base64
import collections
import itertools
import json
import unittest

from tst import lib
from tst.make.test_exec_make import Workspace


EXPLICIT = ['$(S)/grove/entries.gzt', '$(S)/grove/article.gztproto']
TRANSITIVE = {
    '$(S)/grove/base.proto',
    *(f'$(S)/vendor/{v}/common/schema.proto' for v in ('oak', 'elm', 'ash')),
}


def fixture():
    files = {
        'grove/entries.gzt': 'import "grove/article.gztproto";\n',
        'grove/article.gztproto': 'import "grove/base.proto";\n',
        'grove/base.proto': 'import "common/schema.proto";\n',
        'build/sysincl/misc.yml': (
            '- includes:\n    - common/schema.proto:\n'
            + ''.join(f'      - vendor/{v}/common/schema.proto\n' for v in ('oak', 'elm', 'ash'))
        ),
    }
    for v in ('oak', 'elm', 'ash'):
        files[f'vendor/{v}/common/schema.proto'] = 'syntax = "proto2";\n'
    lib.tool_program(files, 'tools/reader', 'reader')
    for name, inputs, enabled, include, opaque in (
        ('muted', 'grove/entries.gzt grove/article.gztproto', False, '', False),
        ('entries_only', 'grove/entries.gzt', False, '', False),
        ('schema_owner', 'grove/article.gztproto', True, '', False),
        ('base_owner', 'grove/base.proto', True, '', False),
        ('entries_owner', 'grove/entries.gzt', True, '', False),
        ('opaque', 'grove/article.gztproto', False, '', True),
    ):
        inputs = ' '.join('${ARCADIA_ROOT}/'+path for path in inputs.split())
        files[name+'/ya.make'] = (
            'UNION()\n' + ('SET(DONT_RESOLVE_INCLUDES no)\n' if enabled else '')
            + (f'ADDINCL(FOR proto {include})\n' if include else '')
            + f'RUN_PROGRAM(tools/reader -o ${{BINDIR}}/{name}.bin '
            + ('IN_NOPARSE ' if opaque else 'IN ') + inputs + f' OUT {name}.bin)\nEND()\n'
        )
    return files


def graph(files, roots):
    return lib.make(files, roots[-1], *roots[:-1])


def output(g, module):
    return lib.node_by_output(g, f'$(B)/{module}/{module}.bin')


class SharedSourceEdgesTest(unittest.TestCase):
    def test_disabled_standalone_and_unbridged_parent_edges(self):
        files = fixture()
        for roots in (('muted',), ('muted', 'base_owner'), ('base_owner', 'muted'),
                      ('entries_only', 'schema_owner'), ('schema_owner', 'entries_only')):
            with self.subTest(roots=roots):
                g = graph(files, roots)
                module = 'muted' if 'muted' in roots else 'entries_only'
                expected = EXPLICIT if module == 'muted' else EXPLICIT[:1]
                self.assertEqual(output(g, module)['inputs'], ['$(B)/tools/reader/reader', *expected])

    def test_shared_schema_owner_and_all_three_root_orders(self):
        files = fixture()
        roots = [('muted', 'schema_owner'), ('schema_owner', 'muted')]
        roots += list(itertools.permutations(('muted', 'schema_owner', 'base_owner')))
        for order in roots:
            with self.subTest(roots=order):
                g = graph(files, order)
                consumer = output(g, 'muted')
                self.assertEqual(consumer['inputs'][:3], ['$(B)/tools/reader/reader', *EXPLICIT])
                self.assertEqual(set(consumer['inputs'][3:]), TRANSITIVE)
                self.assertEqual(len(consumer['inputs']), 7)
                tool = lib.node_by_output(g, '$(B)/tools/reader/reader')
                self.assertIn(tool['uid'], consumer['deps'])
                self.assertEqual(consumer['cmds'][0]['cmd_args'], [
                    '$(B)/tools/reader/reader', '-o', '$(B)/muted/muted.bin',
                ])

    def test_flattened_configure_copy_and_python_inputs_keep_their_parsed_views(self):
        for command in (
            'CONFIGURE_FILE(${ARCADIA_ROOT}/grove/article.gztproto parsed.bin)',
            'COPY_FILE_WITH_CONTEXT(${ARCADIA_ROOT}/grove/article.gztproto parsed.bin)',
            'RUN_PYTHON3(${ARCADIA_ROOT}/grove/emit.py IN ${ARCADIA_ROOT}/grove/article.gztproto OUT parsed.bin)',
        ):
            for order in (('operators', 'schema_owner'), ('schema_owner', 'operators')):
                with self.subTest(command=command, roots=order):
                    files = fixture()
                    files['grove/emit.py'] = 'pass\n'
                    files['operators/ya.make'] = (
                        'UNION()\n' + command + '\n'
                        'COPY_FILE(${ARCADIA_ROOT}/grove/article.gztproto text.bin)\n'
                        'RUN_PROGRAM(tools/reader IN parsed.bin text.bin OUT used.bin)\nEND()\n'
                    )
                    g = graph(files, order)
                    parsed = lib.node_by_output(g, '$(B)/operators/parsed.bin')
                    opaque = lib.node_by_output(g, '$(B)/operators/text.bin')
                    self.assertTrue(TRANSITIVE <= set(parsed['inputs']))
                    self.assertIn(EXPLICIT[1], parsed['inputs'])
                    self.assertFalse(TRANSITIVE & set(opaque['inputs']))
                    self.assertFalse(any(p.endswith('.pb.h') for p in parsed['inputs']))
                    used = lib.node_by_output(g, '$(B)/operators/used.bin')
                    self.assertIn(parsed['uid'], used['deps'])
                    self.assertIn(opaque['uid'], used['deps'])

    def test_only_an_established_parent_edge_bridges_the_gzt_input(self):
        for order in (('entries_only', 'entries_owner'), ('entries_owner', 'entries_only')):
            with self.subTest(roots=order):
                consumer = output(graph(fixture(), order), 'entries_only')
                self.assertEqual(set(consumer['inputs']), {
                    '$(B)/tools/reader/reader', *EXPLICIT, *TRANSITIVE,
                })

    def test_text_remains_opaque_and_mixed_inputs_keep_their_modes(self):
        for order in (('opaque', 'schema_owner'), ('schema_owner', 'opaque')):
            with self.subTest(roots=order):
                files = fixture()
                self.assertEqual(output(graph(files, order), 'opaque')['inputs'], [
                    '$(B)/tools/reader/reader', EXPLICIT[1],
                ])
                files['opaque/ya.make'] = files['opaque/ya.make'].replace(
                    'IN_NOPARSE', 'IN ${ARCADIA_ROOT}/grove/entries.gzt IN_NOPARSE',
                )
                self.assertEqual(output(graph(files, order), 'opaque')['inputs'], [
                    '$(B)/tools/reader/reader', *EXPLICIT,
                ])

    def check_resource_source_identity(self, dont_parse):
        files = fixture()
        flags = 'DONT_PARSE DONT_COMPRESS' if dont_parse else 'DONT_COMPRESS'
        files['payload/ya.make'] = (
            f'LIBRARY()\n{lib.BARE_MODULE}'
            f'RESOURCE({flags} ${{ARCADIA_ROOT}}/grove/article.gztproto /payload)\nEND()\n'
        )
        files['library/cpp/resource/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}END()\n'
        lib.tool_program(files, 'tools/rescompiler', 'rescompiler')
        lib.tool_program(files, 'tools/rescompressor', 'rescompressor')
        for order in (('payload', 'schema_owner'), ('schema_owner', 'payload')):
            with self.subTest(roots=order):
                g = graph(files, order)
                packed = [n for n in g['graph'] if EXPLICIT[1] in n['inputs']
                          and any(o.startswith('$(B)/payload/') for o in n['outputs'])]
                self.assertEqual(len(packed), 1)
                node = packed[0]
                expected = ['$(B)/tools/rescompiler/rescompiler', '$(B)/tools/rescompressor/rescompressor',
                            '$(S)/build/scripts/objcopy.py', EXPLICIT[1]]
                if dont_parse:
                    self.assertFalse(TRANSITIVE & set(node['inputs']))
                else:
                    expected += sorted(TRANSITIVE)
                self.assertEqual(collections.Counter(node['inputs']), collections.Counter(expected))
                args = node['cmds'][0]['cmd_args']
                self.assertEqual(args[args.index('--inputs') + 1:args.index('--keys')], [EXPLICIT[1]])
                self.assertEqual(base64.b64decode(args[args.index('--keys') + 1]), b'/payload')
                tools = [lib.node_by_output(g, path) for path in expected[:2]]
                self.assertEqual(collections.Counter(node['deps']),
                                 collections.Counter(tool['uid'] for tool in tools))
                archive = lib.node_by_output(g, '$(B)/payload/libpayload.global.a')
                self.assertIn(node['outputs'][0], archive['inputs'])
                self.assertIn(node['uid'], archive['deps'])
                self.assertIn(archive['uid'], g['result'])
                ids = {producer['uid'] for producer in g['graph']}
                self.assertTrue(g['result'])
                self.assertLessEqual(set(g['result']), ids)
                self.assertTrue(all(set(producer['deps']) <= ids for producer in g['graph']))

    def test_literal_resource_does_not_inherit_shared_file_edges(self):
        self.check_resource_source_identity(dont_parse=True)

    def test_parsed_resource_inherits_shared_file_edges(self):
        self.check_resource_source_identity(dont_parse=False)

    def test_shared_native_source_cycle_is_finalized_once(self):
        files = fixture()
        files.update({
            'native/entry.cpp': '#include "native/left.h"\n',
            'native/left.h': '#include "native/right.h"\n',
            'native/right.h': '#include "native/left.h"\n',
            'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(entry.cpp)\nEND()\n',
        })
        files['muted/ya.make'] = files['muted/ya.make'].replace(
            '${ARCADIA_ROOT}/grove/entries.gzt ${ARCADIA_ROOT}/grove/article.gztproto',
            '${ARCADIA_ROOT}/native/left.h',
        )
        for order in (('muted', 'native'), ('native', 'muted')):
            with self.subTest(roots=order):
                consumer = output(graph(files, order), 'muted')
                self.assertEqual(consumer['inputs'], [
                    '$(B)/tools/reader/reader', '$(S)/native/left.h', '$(S)/native/right.h',
                ])

    def test_ragel5_retains_its_parsed_source_view_and_real_tools(self):
        files = {
            'grammar/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(machine.rl5)\nEND()\n',
            'grammar/machine.rl5': '%%{machine sample;}%%\n',
        }
        lib.tool_program(files, 'contrib/tools/ragel5/ragel', 'ragel')
        lib.tool_program(files, 'contrib/tools/ragel5/rlgen-cd', 'rlgen-cd')
        g = graph(files, ('grammar',))
        producer = lib.node_by_output(g, '$(B)/grammar/machine.rl5.cpp')
        self.assertEqual(producer['inputs'], [
            '$(B)/contrib/tools/ragel5/ragel/ragel',
            '$(B)/contrib/tools/ragel5/rlgen-cd/rlgen-cd', '$(S)/grammar/machine.rl5',
        ])
        for tool in producer['inputs'][:2]:
            self.assertIn(lib.node_by_output(g, tool)['uid'], producer['deps'])

    def test_typescript_test_inputs_retain_their_parsed_import_views(self):
        from tst.make.test_nots import NotsTests

        files = NotsTests().project()
        files.update({
            'web/tests/ya.make': (
                'TS_TEST_JEST_FOR(RELATIVE ../)\nTS_TEST_SRCS(../src)\nEND()\n'
            ),
            'web/src/order.test.ts': 'import "./support";\n',
            'web/src/support.ts': 'export const answer = 42;\n',
            'web/jest.config.js': 'module.exports = {};\n',
        })
        g = lib.make(files, 'web/tests', '--target-platform-tests')
        fake = next(n for n in g['graph'] if any(o.endswith('.ts_test.fake') for o in n['outputs']))
        self.assertIn('$(S)/web/src/order.test.ts', fake['inputs'])
        self.assertIn('$(S)/web/src/support.ts', fake['inputs'])

    def test_disabled_compilation_observes_shared_source_file_edges(self):
        files = fixture()
        files['grove/source.cpp'] = '#include "grove/header.h"\n'
        files['grove/header.h'] = '#pragma once\n'
        for name, switch in (('muted_native', 'SET(DONT_RESOLVE_INCLUDES yes)\n'),
                             ('owner_native', '')):
            files[name+'/ya.make'] = (
                f'LIBRARY()\n{lib.BARE_MODULE}{switch}'
                'SRCS(${ARCADIA_ROOT}/grove/source.cpp)\nEND()\n'
            )
        for order in (('muted_native', 'owner_native'), ('owner_native', 'muted_native')):
            with self.subTest(roots=order):
                g = graph(files, order)
                compiled = [n for n in g['graph'] if n.get('kv', {}).get('p') == 'CC'
                            and '$(S)/grove/source.cpp' in n['inputs']]
                self.assertEqual(len(compiled), 2)
                for node in compiled:
                    self.assertEqual(node['inputs'], ['$(S)/grove/source.cpp', '$(S)/grove/header.h'])

    def test_disabled_generated_property_is_completed_only_by_normal_consumer(self):
        files = {
            'gen/ya.make': (
                'LIBRARY()\n'
                'NO_LIBC()\n'
                'NO_RUNTIME()\n'
                'NO_UTIL()\n'
                'SRCS(owner.cpp)\n'
                'RUN_PYTHON3(generate.py ${BINDIR}/shared.cpp OUT_NOAUTO shared.cpp OUTPUT_INCLUDES dep.h)\n'
                'END()\n'
            ),
            'gen/owner.cpp': 'int owner;\n',
            'gen/generate.py': (
                'import pathlib,sys\n'
                'pathlib.Path(sys.argv[1]).write_text(\'#include "dep.h"\\nint generated;\\n\')\n'
            ),
            'gen/dep.h': '#include "leaf.h"\n',
            'gen/leaf.h': '#pragma once\n',
            'muted/ya.make': (
                'LIBRARY()\n'
                'NO_LIBC()\n'
                'NO_RUNTIME()\n'
                'NO_UTIL()\n'
                'SET(DONT_RESOLVE_INCLUDES yes)\n'
                'PEERDIR(gen)\n'
                'ADDINCL(gen)\n'
                'SRCS(${ARCADIA_BUILD_ROOT}/gen/shared.cpp)\n'
                'END()\n'
            ),
            'normal/ya.make': (
                'LIBRARY()\n'
                'NO_LIBC()\n'
                'NO_RUNTIME()\n'
                'NO_UTIL()\n'
                'PEERDIR(gen)\n'
                'ADDINCL(gen)\n'
                'SRCS(${ARCADIA_BUILD_ROOT}/gen/shared.cpp)\n'
                'END()\n'
            ),
        }
        for roots in (('muted',), ('normal',), ('muted', 'normal'), ('normal', 'muted')):
            with self.subTest(roots=roots):
                g = lib.make(files, roots[-1], *roots[:-1],
                             '--target-platform', 'default-linux-x86_64',
                             '--add-modules-to-results')
                producer = lib.node_by_output(g, '$(B)/gen/shared.cpp')
                expected = ['$(B)/gen/shared.cpp']
                if 'normal' in roots:
                    expected += ['$(S)/gen/dep.h', '$(S)/gen/leaf.h']
                for module in roots:
                    compiled = lib.node_by_output(g, f'$(B)/{module}/__/gen/shared.cpp.o')
                    self.assertCountEqual(compiled['inputs'], expected)
                    self.assertEqual(compiled['deps'], [producer['uid']])

    def test_established_build_edge_keeps_real_producer_and_main_output(self):
        files = fixture()
        files.update({
            'grove/bridge.h': '#include "builder/private.h"\n',
            'builder/source.cpp': '#include "grove/bridge.h"\n',
            'builder/seed.txt': 'seed\n',
            'builder/ya.make': (
                f'LIBRARY()\n{lib.BARE_MODULE}'
                'RUN_PROGRAM(tools/reader IN seed.txt OUT_NOAUTO public.h private.h)\n'
                'SRCS(source.cpp)\nEND()\n'
            ),
        })
        files['muted/ya.make'] = files['muted/ya.make'].replace(
            '${ARCADIA_ROOT}/grove/entries.gzt ${ARCADIA_ROOT}/grove/article.gztproto',
            '${ARCADIA_ROOT}/grove/bridge.h',
        )
        for order in (('muted', 'builder'), ('builder', 'muted')):
            with self.subTest(roots=order):
                g = graph(files, order)
                consumer = output(g, 'muted')
                producer = lib.node_by_output(g, '$(B)/builder/private.h')
                self.assertEqual(set(consumer['inputs']), {
                    '$(B)/tools/reader/reader', '$(S)/grove/bridge.h',
                    '$(B)/builder/public.h', '$(B)/builder/private.h',
                })
                self.assertEqual(consumer['deps'].count(producer['uid']), 1)

    def test_possible_target_tool_source_edges_survive_host_binding(self):
        files = fixture()
        files['grove/shared.h'] = '#include "grove/deep.h"\n'
        files['grove/deep.h'] = '#pragma once\n'
        files['tools/reader/main.cpp'] = '#include "grove/shared.h"\nint main() {}\n'
        files['muted/ya.make'] = files['muted/ya.make'].replace(
            '${ARCADIA_ROOT}/grove/entries.gzt ${ARCADIA_ROOT}/grove/article.gztproto',
            '${ARCADIA_ROOT}/grove/shared.h',
        )
        g = graph(files, ('muted',))
        compiled = lib.node_by_output_prefix(g, '$(B)/tools/reader/main.cpp.')
        self.assertIn('$(S)/grove/deep.h', compiled['inputs'])
        self.assertEqual(compiled['platform'], 'default-linux-x86_64')
        self.assertIn('main.cpp.pic.o', compiled['outputs'][0])
        self.assertEqual(set(output(g, 'muted')['inputs']), {
            '$(B)/tools/reader/reader', '$(S)/grove/shared.h', '$(S)/grove/deep.h',
        })

    def test_pic_source_edges_do_not_enter_plain_target_context(self):
        files = fixture()
        files.update({
            'grove/shared.h': '#include "grove/deep.h"\n',
            'grove/deep.h': '#pragma once\n',
            'pic_owner/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(source.cpp)\nEND()\n',
            'pic_owner/source.cpp': '#include "grove/shared.h"\n',
            'pic_linker/ya.make': (
                f'DYNAMIC_LIBRARY(shared)\n{lib.BARE_MODULE}'
                'DYNAMIC_LIBRARY_FROM(pic_owner)\nEXPORTS_SCRIPT(exports)\nEND()\n'
            ),
            'pic_linker/exports': '{};\n',
            'build/platform/local_so/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\n',
        })
        lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
        files['muted/ya.make'] = files['muted/ya.make'].replace(
            '${ARCADIA_ROOT}/grove/entries.gzt ${ARCADIA_ROOT}/grove/article.gztproto',
            '${ARCADIA_ROOT}/grove/shared.h',
        )
        for order in (('muted', 'pic_linker'), ('pic_linker', 'muted')):
            with self.subTest(roots=order):
                g = graph(files, order)
                compiled = lib.node_by_output(g, '$(B)/pic_owner/source.cpp.pic.o')
                self.assertIn('$(S)/grove/deep.h', compiled['inputs'])
                self.assertEqual(output(g, 'muted')['inputs'], [
                    '$(B)/tools/reader/reader', '$(S)/grove/shared.h',
                ])

    def test_final_source_content_changes_consumer_and_dependent_executor_uids(self):
        for order in (('muted', 'schema_owner'), ('schema_owner', 'muted'), ('muted',)):
            with self.subTest(roots=order):
                files = fixture()
                files['muted/ya.make'] = files['muted/ya.make'].replace(
                    'END()', 'RUN_PROGRAM(tools/reader -o ${BINDIR}/after.bin IN muted.bin OUT after.bin)\nEND()',
                )
                ws = Workspace(self, files)
                ws.make('-j1', targets=order)
                before = {path.name for path in ws.uid_files()}
                ws.write('vendor/oak/common/schema.proto', '// changed source content\n')
                ws.make('-j1', targets=order)
                added = {key for path in ws.uid_files() if path.name not in before
                         for key in json.loads(path.read_text())}
                if 'schema_owner' in order:
                    self.assertTrue({'$(B)/muted/muted.bin', '$(B)/muted/after.bin'} <= added, added)
                else:
                    self.assertEqual(added, set())


if __name__ == '__main__':
    unittest.main(verbosity=2)
