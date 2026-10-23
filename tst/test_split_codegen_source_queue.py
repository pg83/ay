"""SPLIT_CODEGEN actions use the native declaration/readiness source queue."""
import unittest

import lib
from test_codegen_enum import enum_files, ENUM_PARSER

MODULE = 'grove/pieces'
ROOT = '$(B)/' + MODULE + '/'
SPLIT = 'SPLIT_CODEGEN(tools/splitter alpha NGrove OUT_NUM 6 OUTPUT_INCLUDES grove/pieces/local.h)\n'
ENUM = 'GENERATE_ENUM_SERIALIZATION_WITH_HEADER(label.h)\n'
GEN = 'RUN_PROGRAM(tools/writer OUT_NOAUTO receipt.dat alpha.in)\n'
PARTS = [f'alpha.{i}.cpp' for i in range(6)]


def fixture(body, generated=False, name=MODULE):
    files = enum_files({
        name + '/ya.make': f'LIBRARY(pieces)\n{lib.BARE_MODULE}CFLAGS(-DQUEUE_OWNER)\n' + body + 'END()\n',
        name + '/native.cpp': '#include "' + name + '/alpha.h"\n',
        name + '/listed.cpp': 'int listed;\n',
        name + '/global.cpp': 'int global;\n',
        name + '/label.h': '#include "local.h"\nenum Label {One};\n',
        name + '/local.h': '',
    })
    if not generated:
        files[name + '/alpha.in'] = 'physical seed\n'
    lib.tool_program(files, 'tools/splitter', 'splitter')
    lib.tool_program(files, 'tools/writer', 'writer')
    return files


class SplitCodegenSourceQueueTest(unittest.TestCase):
    def assert_archive(self, graph, sources, suffix='.o', global_archive=False, module=MODULE):
        root = '$(B)/' + module + '/'
        output = root + ('libpieces.global.a' if global_archive else 'libpieces.a')
        node = lib.node_by_output(graph, output)
        objects = [root + s + suffix for s in sources]
        self.assertEqual(node['cmds'][0]['cmd_args'], [
            '', '$(S)/build/scripts/link_lib.py', '', 'LLVM_AR', 'gnu', '$(B)', 'None', '--', '--', output, *objects,
        ])
        self.assertEqual(node['inputs'][:len(objects)], objects)
        self.assertEqual({p for p in node['inputs'] if p.endswith('.o')}, set(objects))
        for path in objects:
            producer = lib.node_by_output(graph, path)
            self.assertIn(producer['uid'], node['deps'])
            self.assertIn(path, node['inputs'])
        return node

    def assert_split(self, graph, generated=False, suffix='.o', module=MODULE):
        root = '$(B)/' + module + '/'
        source = '$(S)/' + module + '/'
        split = lib.node_by_output(graph, root + 'alpha.0.cpp')
        tool = lib.node_by_output(graph, '$(B)/tools/splitter/splitter')
        input_path = (root if generated else source) + 'alpha.in'
        self.assertEqual(split['cmds'][0]['cmd_args'], [
            tool['outputs'][0], input_path, root + 'alpha.cpp', root + 'alpha.h', '--cpp-parts', '1', 'NGrove',
        ])
        self.assertEqual(split['outputs'], [root + p for p in PARTS] + [root + 'alpha.cpp', root + 'alpha.h'])
        self.assertIn(tool['uid'], split['deps'])
        self.assertIn(tool['outputs'][0], split['inputs'])
        self.assertIn(input_path, split['inputs'])
        if generated:
            writer = lib.node_by_output(graph, '$(B)/tools/writer/writer')
            produced = lib.node_by_output(graph, root + 'receipt.dat')
            self.assertEqual(produced['outputs'], [root + 'receipt.dat', root + 'alpha.in'])
            self.assertIn(writer['uid'], produced['deps'])
            self.assertIn(writer['outputs'][0], produced['inputs'])
            self.assertIn(produced['uid'], split['deps'])
            self.assertIn(root + 'receipt.dat', split['inputs'])
            self.assertNotIn(source + 'alpha.in', split['inputs'])
        for part in PARTS:
            cc = lib.node_by_output(graph, root + part + suffix)
            self.assertIn(split['uid'], cc['deps'])
            self.assertIn(root + part, cc['inputs'])
            self.assertIn(root + PARTS[0], cc['inputs'])
            self.assertNotIn(input_path, cc['inputs'])
            self.assertIn('-DQUEUE_OWNER', cc['cmds'][0]['cmd_args'])
        consumer = lib.node_by_output(graph, root + 'native.cpp' + suffix)
        for path in (root + 'alpha.h', root + PARTS[0], source + 'local.h'):
            self.assertIn(path, consumer['inputs'])
        self.assertIn(split['uid'], consumer['deps'])
        self.assertIn('-DQUEUE_OWNER', consumer['cmds'][0]['cmd_args'])
        return split

    def test_ready_physical_and_generated_inputs_preserve_action_declaration_order(self):
        for generated in (False, True):
            for before in (False, True):
                for enum_first in (False, True):
                    with self.subTest(generated=generated, before=before, enum_first=enum_first):
                        actions = ENUM + SPLIT if enum_first else SPLIT + ENUM
                        body = (GEN + actions if before else actions + GEN) + 'SRC(native.cpp -DORIGINAL)\nSRCS(listed.cpp)\n'
                        graph = lib.make(fixture(body, generated), MODULE)
                        enum_ahead = enum_first or generated and not before
                        auto = ['label.h_serialized.cpp', *PARTS] if enum_ahead else [*PARTS, 'label.h_serialized.cpp']
                        self.assert_archive(graph, ['native.cpp', 'listed.cpp', *auto])
                        self.assert_split(graph, generated)
                        cc = lib.node_by_output(graph, ROOT + 'native.cpp.o')
                        self.assertIn('-DORIGINAL', cc['cmds'][0]['cmd_args'])
                        enum = lib.node_by_output(graph, ROOT + 'label.h_serialized.cpp')
                        parser = lib.node_by_output(graph, ENUM_PARSER)
                        self.assertIn(parser['uid'], enum['deps'])
                        for path in ('label.h', 'local.h'):
                            self.assertIn('$(S)/' + MODULE + '/' + path, enum['inputs'])
                        if not generated:
                            self.assertFalse(any(ROOT + 'receipt.dat' in n['outputs'] for n in graph['graph']))

    def test_pending_input_chains_retry_in_finite_fifo_waves(self):
        chain = ('RUN_PROGRAM(tools/writer IN ${BINDIR}/seed.dat OUT_NOAUTO receipt.dat alpha.in)\n'
                 'RUN_PROGRAM(tools/writer OUT_NOAUTO seed.dat)\n')
        for before in (False, True):
            with self.subTest(before=before):
                actions = SPLIT + ENUM
                body = (chain + actions if before else actions + chain) + 'SRC(native.cpp -DORIGINAL)\nSRCS(listed.cpp)\n'
                graph = lib.make(fixture(body, True), MODULE)
                self.assert_archive(graph, ['native.cpp', 'listed.cpp', 'label.h_serialized.cpp', *PARTS])
                self.assert_split(graph, True)
                seed = lib.node_by_output(graph, ROOT + 'seed.dat')
                produced = lib.node_by_output(graph, ROOT + 'receipt.dat')
                self.assertIn(seed['uid'], produced['deps'])
                self.assertIn(seed['outputs'][0], produced['inputs'])

    def test_generated_header_enum_and_later_split_share_native_ready_order(self):
        for enum_before in (False, True):
            with self.subTest(enum_before=enum_before):
                enum = 'GENERATE_ENUM_SERIALIZATION_WITH_HEADER(alpha.h)\n'
                second = SPLIT.replace(' alpha ', ' beta ')
                body = (enum + SPLIT if enum_before else SPLIT + enum) + second + 'SRCS(native.cpp listed.cpp)\n'
                files = fixture(body)
                files[MODULE + '/beta.in'] = 'second physical seed\n'
                graph = lib.make(files, MODULE)
                # If enum is pending before alpha, beta's initial action emits
                # its parts before enum's retried action can append its source.
                beta_parts = [p.replace('alpha.', 'beta.') for p in PARTS]
                tail = [*beta_parts, 'alpha.h_serialized.cpp'] if enum_before else ['alpha.h_serialized.cpp', *beta_parts]
                self.assert_archive(graph, ['native.cpp', 'listed.cpp', *PARTS, *tail])
                first = self.assert_split(graph)
                enum_node = lib.node_by_output(graph, ROOT + 'alpha.h_serialized.cpp')
                self.assertIn(first['uid'], enum_node['deps'])
                for path in ('alpha.h', PARTS[0]):
                    self.assertIn(ROOT + path, enum_node['inputs'])
                cc = lib.node_by_output(graph, ROOT + 'alpha.h_serialized.cpp.o')
                self.assertIn(enum_node['uid'], cc['deps'])
                self.assertIn(ROOT + 'alpha.h_serialized.cpp', cc['inputs'])
                self.assertNotIn('$(S)/' + MODULE + '/alpha.in', cc['inputs'])

    def test_ordinary_automatic_producers_follow_the_same_action_queue(self):
        generated = 'RUN_PROGRAM(tools/writer OUT generated.cpp)\n'
        for before in (False, True):
            with self.subTest(before=before):
                body = (generated + SPLIT + ENUM if before else SPLIT + ENUM + generated) + 'SRCS(native.cpp listed.cpp)\n'
                graph = lib.make(fixture(body), MODULE)
                auto = ['generated.cpp', *PARTS, 'label.h_serialized.cpp'] if before else [*PARTS, 'label.h_serialized.cpp', 'generated.cpp']
                self.assert_archive(graph, ['native.cpp', 'listed.cpp', *auto])
                self.assert_split(graph)
                produced = lib.node_by_output(graph, ROOT + 'generated.cpp')
                cc = lib.node_by_output(graph, ROOT + 'generated.cpp.o')
                self.assertIn(produced['uid'], cc['deps'])
                self.assertIn(produced['outputs'][0], cc['inputs'])

    def test_unsplit_noauto_source_is_only_compiled_when_explicitly_declared_global(self):
        for explicit in (False, True):
            with self.subTest(explicit=explicit):
                body = SPLIT + ENUM + ('SRCS(GLOBAL ${BINDIR}/alpha.cpp GLOBAL global.cpp native.cpp)\n' if explicit else 'SRCS(native.cpp listed.cpp)\n')
                graph = lib.make(fixture(body), MODULE)
                self.assert_archive(graph, ['native.cpp', *([] if explicit else ['listed.cpp']), *PARTS, 'label.h_serialized.cpp'])
                self.assert_split(graph)
                if explicit:
                    self.assert_archive(graph, ['alpha.cpp', 'global.cpp'], global_archive=True)
                    split = lib.node_by_output(graph, ROOT + 'alpha.0.cpp')
                    unsplit = lib.node_by_output(graph, ROOT + 'alpha.cpp.o')
                    self.assertIn(split['uid'], unsplit['deps'])
                    self.assertIn(ROOT + PARTS[0], unsplit['inputs'])
                else:
                    self.assertFalse(any(ROOT + 'alpha.cpp.o' in n['outputs'] for n in graph['graph']))
                self.assertFalse(any(p.endswith('.h.o') for n in graph['graph'] for p in n['outputs']))

    def test_global_srcs_preserve_shared_declaration_metadata_for_source_and_build(self):
        for macro in ('SRCS', 'GLOBAL_SRCS'):
            for before in (False, True):
                for reverse in (False, True):
                    with self.subTest(macro=macro, before=before, reverse=reverse):
                        values = ['${BINDIR}/generated.cpp', '${ARCADIA_ROOT}/' + MODULE + '/global.cpp']
                        expected = ['generated.cpp', 'global.cpp']
                        if reverse:
                            values.reverse()
                            expected.reverse()
                        args = values if macro == 'GLOBAL_SRCS' else ['GLOBAL ' + v for v in values]
                        declaration = macro + '(' + ' '.join(args) + ')\nSRCS(listed.cpp)\n'
                        producer = 'RUN_PROGRAM(tools/writer OUT_NOAUTO generated.cpp)\n'
                        files = fixture(producer + declaration if before else declaration + producer)
                        graph = lib.make(files, MODULE)
                        global_ar = self.assert_archive(graph, expected, global_archive=True)
                        local = self.assert_archive(graph, ['listed.cpp'])
                        self.assertNotIn(global_ar['uid'], local['deps'])
                        cc = lib.node_by_output(graph, ROOT + 'generated.cpp.o')
                        produced = lib.node_by_output(graph, ROOT + 'generated.cpp')
                        self.assertIn(produced['uid'], cc['deps'])
                        self.assertIn(produced['outputs'][0], cc['inputs'])
                        writer = lib.node_by_output(graph, '$(B)/tools/writer/writer')
                        self.assertIn(writer['uid'], produced['deps'])
                        self.assertIn(writer['outputs'][0], produced['inputs'])
                        physical = lib.node_by_output(graph, ROOT + 'global.cpp.o')
                        self.assertEqual(physical['inputs'], ['$(S)/' + MODULE + '/global.cpp'])
                        for node in (cc, physical):
                            self.assertIn('-DQUEUE_OWNER', node['cmds'][0]['cmd_args'])

    def test_host_tool_and_target_module_have_independent_source_action_facts(self):
        files = fixture(GEN + ENUM + SPLIT + 'SRCS(native.cpp listed.cpp)\n', True)
        lib.tool_program(files, 'tools/consumer', 'consumer')
        files['tools/consumer/ya.make'] = files['tools/consumer/ya.make'].replace('END()', 'PEERDIR(' + MODULE + ')\nEND()')
        files['caller/ya.make'] = f'LIBRARY(caller)\n{lib.BARE_MODULE}RUN_PROGRAM(tools/consumer OUT call.cpp)\nEND()\n'
        for roots in (('caller', MODULE), (MODULE, 'caller')):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                archives = [n for n in graph['graph'] if ROOT + 'libpieces.a' in n['outputs']]
                self.assertEqual({n['platform'] for n in archives}, {'default-linux-aarch64', 'default-linux-x86_64'})
                for archive in archives:
                    platform = archive['platform']
                    suffix = '.pic.o' if platform == 'default-linux-x86_64' else '.o'
                    sources = ['native.cpp', 'listed.cpp', 'label.h_serialized.cpp', *PARTS]
                    self.assertEqual(archive['cmds'][0]['cmd_args'][10:], [ROOT + s + suffix for s in sources])
                    split = next(n for n in graph['graph'] if ROOT + PARTS[0] in n['outputs'] and n['platform'] == platform)
                    generated = next(n for n in graph['graph'] if ROOT + 'receipt.dat' in n['outputs'] and n['platform'] == platform)
                    self.assertIn(generated['uid'], split['deps'])
                    self.assertIn(ROOT + 'receipt.dat', split['inputs'])
                    for part in PARTS:
                        cc = next(n for n in graph['graph'] if ROOT + part + suffix in n['outputs'] and n['platform'] == platform)
                        self.assertIn(split['uid'], cc['deps'])
                        self.assertIn(cc['uid'], archive['deps'])
                host_ar = next(n for n in archives if n['platform'] == 'default-linux-x86_64')
                tool = lib.node_by_output(graph, '$(B)/tools/consumer/consumer')
                self.assertIn(host_ar['uid'], tool['deps'])
                run = lib.node_by_output(graph, '$(B)/caller/call.cpp')
                self.assertIn(tool['uid'], run['deps'])
                self.assertIn(tool['outputs'][0], run['inputs'])

    def test_pic_context_and_both_module_root_orders_keep_distinct_facts(self):
        for pic in (False, True):
            for roots in ((MODULE, 'other'), ('other', MODULE)):
                with self.subTest(pic=pic, roots=roots):
                    body = GEN + SPLIT + ENUM + 'SRCS(native.cpp listed.cpp)\n'
                    files = fixture(body, True)
                    files['other/ya.make'] = f'LIBRARY(other)\n{lib.BARE_MODULE}SRCS(plain.cpp)\nEND()\n'
                    files['other/plain.cpp'] = 'int other;\n'
                    args = ('--target-platform', 'default-linux-x86_64', '-DPIC=yes') if pic else ()
                    graph = lib.make(files, *roots, *args)
                    suffix = '.pic.o' if pic else '.o'
                    self.assert_archive(graph, ['native.cpp', 'listed.cpp', *PARTS, 'label.h_serialized.cpp'], suffix)
                    self.assert_split(graph, True, suffix)
                    other = lib.node_by_output(graph, '$(B)/other/libother.a')
                    self.assertNotIn(other['uid'], lib.node_by_output(graph, ROOT + 'alpha.0.cpp')['deps'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
