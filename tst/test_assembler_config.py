import json
import unittest

import lib


BARE = 'LIBRARY()\n' + lib.BARE_MODULE
X64 = ('--target-platform', 'default-linux-x86_64')
DEBUG = ['--replace=$(B)=/-B', '--replace=$(S)=/-S', '--replace=$(TOOL_ROOT)=/-T']


def fixture(body):
    files = {
        'copper/ya.make': BARE + body + '\nEND()\n',
        'copper/vector.asm': '',
        'copper/forced.yasm': '',
        'build/external_resources/nasm/ya.make': lib.resource_lib('NASM', 789),
    }
    lib.tool_program(files, 'contrib/tools/yasm', 'yasm')
    return files


def args(graph, output='$(B)/copper/vector.o'):
    return lib.node_by_output(graph, output)['cmds'][0]['cmd_args']


class AssemblerConfigTest(unittest.TestCase):
    def test_module_flags_append_replace_expand_and_precede_source_flags(self):
        for assembler in ('yasm', 'nasm'):
            with self.subTest(assembler=assembler):
                key = assembler.upper() + '_FLAGS'
                files = fixture(
                    ('USE_NASM()\n' if assembler == 'nasm' else '')
                    + f'SET({key} -D FIRST=1)\nSET_APPEND({key} -D SECOND=2)\n'
                    + 'SET(LOCAL_FLAGS -D THIRD=3)\n'
                    + f'SET_APPEND({key} ${{LOCAL_FLAGS}})\n'
                    + 'ADDINCL(FOR asm copper/headers)\nSRC(vector.asm -D SOURCE=4)'
                )
                files['copper/headers/.keep'] = ''
                graph = lib.make(files, 'copper', *X64)
                head = (['$(B)/resources/NASM/bin/nasm', '-f', 'elf64', '-D', '_x86_64_']
                        if assembler == 'nasm' else
                        ['$(B)/contrib/tools/yasm/yasm', '-f', 'elf64', '-D', 'UNIX',
                         *DEBUG, '-D', '_x86_64_', '-D_YASM_', '-g', 'dwarf2'])
                self.assertEqual(args(graph), head + [
                    '-D', 'FIRST=1', '-D', 'SECOND=2', '-D', 'THIRD=3',
                    '-I', '$(B)', '-I', '$(S)', '-I', '$(S)/copper/headers',
                    '-D', 'SOURCE=4', '-o', '$(B)/copper/vector.o', '$(S)/copper/vector.asm',
                ])
                files['copper/ya.make'] = files['copper/ya.make'].replace(
                    'ADDINCL(', f'SET({key} -D REPLACED=5)\nADDINCL(')
                self.assertEqual(args(lib.make(files, 'copper', *X64)), head + [
                    '-D', 'REPLACED=5', '-I', '$(B)', '-I', '$(S)', '-I', '$(S)/copper/headers',
                    '-D', 'SOURCE=4', '-o', '$(B)/copper/vector.o', '$(S)/copper/vector.asm',
                ])

    def test_preincludes_keep_command_spelling_order_duplicates_and_scan_inputs(self):
        files = fixture(
            'SET(YASM_PREINCLUDES_VALUE shared/initial.asm)\n'
            'SET_APPEND(YASM_PREINCLUDES_VALUE ${CURDIR}/next.asm)\n'
            'SET(PRELUDE shared/final.asm)\nASM_PREINCLUDE(${PRELUDE} shared/initial.asm)\n'
            'ASM_PREINCLUDE()\nADDINCL(FOR asm shared/headers)\nSRC(vector.asm -D SOURCE=1)'
        )
        files.update({
            'shared/initial.asm': '%include "nested.asm"\n',
            'shared/nested.asm': '%include "leaf.asm"\n',
            'shared/headers/leaf.asm': '',
            'copper/next.asm': '',
            'shared/final.asm': '',
        })
        result = lib.make_process(files, 'copper', *X64)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('ASM_PREINCLUDE', result.stderr)
        graph = json.loads(result.stdout)
        self.assertEqual(args(graph)[-11:], [
            '-o', '$(B)/copper/vector.o', '-P', 'shared/initial.asm',
            '-P', '$(S)/copper/next.asm', '-P', 'shared/final.asm',
            '-P', 'shared/initial.asm', '$(S)/copper/vector.asm',
        ])
        self.assertCountEqual(lib.node_by_output(graph, '$(B)/copper/vector.o')['inputs'], [
            '$(B)/contrib/tools/yasm/yasm', '$(S)/copper/vector.asm',
            '$(S)/shared/initial.asm', '$(S)/shared/nested.asm', '$(S)/shared/headers/leaf.asm',
            '$(S)/copper/next.asm', '$(S)/shared/final.asm',
        ])

    def test_nasm_preincludes_are_arguments_while_yasm_declares_hidden_inputs(self):
        files = fixture(
            'USE_NASM()\nSET(YASM_FLAGS -D YASM_ONLY=1)\nSET(NASM_FLAGS -D NASM_ONLY=2)\n'
            'ASM_PREINCLUDE(shared/setup.asm)\nSRCS(vector.asm forced.yasm)'
        )
        files.update({'shared/setup.asm': '%include "child.asm"\n', 'shared/child.asm': ''})
        graph = lib.make(files, 'copper', *X64)
        nasm = lib.node_by_output(graph, '$(B)/copper/vector.o')
        yasm = lib.node_by_output(graph, '$(B)/copper/forced.o')
        self.assertEqual(args(graph)[-5:], [
            '-o', '$(B)/copper/vector.o', '-P', 'shared/setup.asm', '$(S)/copper/vector.asm',
        ])
        self.assertIn('NASM_ONLY=2', args(graph))
        self.assertNotIn('YASM_ONLY=1', args(graph))
        self.assertEqual(nasm['inputs'], ['$(S)/copper/vector.asm'])
        self.assertIn('YASM_ONLY=1', yasm['cmds'][0]['cmd_args'])
        self.assertNotIn('NASM_ONLY=2', yasm['cmds'][0]['cmd_args'])
        self.assertCountEqual(yasm['inputs'], ['$(B)/contrib/tools/yasm/yasm',
                                             '$(S)/copper/forced.yasm', '$(S)/shared/setup.asm',
                                             '$(S)/shared/child.asm'])

    def test_initial_script_values_and_shared_lexical_calls_use_each_module_context(self):
        files = fixture('')
        files['build/ymake_conf.py'] = lib.ymake_conf(extra=(
            'YASM_FLAGS=-D CONFIG=1\nNASM_FLAGS=-D CONFIG=2\n'
            'YASM_PREINCLUDES_VALUE=shared/start.asm\n'
        ))
        files['shared/start.asm'] = ''
        files['shared/append.inc'] = (
            'SET_APPEND(YASM_FLAGS -D ${LABEL}=3)\nSET_APPEND(NASM_FLAGS -D ${LABEL}=4)\n'
            'ASM_PREINCLUDE(${CURDIR}/setup.asm)\n'
        )
        for module in ('copper', 'silver'):
            files[f'{module}/ya.make'] = (
                BARE + f'SET(LABEL {module})\n'
                'INCLUDE(${ARCADIA_ROOT}/shared/append.inc)\nSRCS(vector.asm)\nEND()\n'
            )
            files[f'{module}/vector.asm'] = ''
            files[f'{module}/setup.asm'] = ''
        for nasm in (False, True):
            for module in ('copper', 'silver'):
                if nasm:
                    files[f'{module}/ya.make'] = files[f'{module}/ya.make'].replace(
                        'SRCS(', 'USE_NASM()\nSRCS(')
            for targets in (('copper', 'silver'), ('silver', 'copper')):
                with self.subTest(targets=targets, nasm=nasm):
                    graph = lib.make(files, *targets, *X64)
                    for module in targets:
                        node = lib.node_by_output(graph, f'$(B)/{module}/vector.o')
                        command = node['cmds'][0]['cmd_args']
                        index = 5 if nasm else 13
                        self.assertEqual(command[index:index + 4],
                                         ['-D', 'CONFIG=2' if nasm else 'CONFIG=1', '-D',
                                          module + ('=4' if nasm else '=3')])
                        self.assertEqual(command[-5:], ['-P', 'shared/start.asm', '-P',
                                                       f'$(S)/{module}/setup.asm', f'$(S)/{module}/vector.asm'])
                        if nasm:
                            self.assertEqual(node['inputs'], [f'$(S)/{module}/vector.asm'])
                        else:
                            self.assertIn(f'$(S)/{module}/setup.asm', node['inputs'])
                            self.assertNotIn(f'$(S)/{next(m for m in targets if m != module)}/setup.asm', node['inputs'])

    def test_source_preinclude_arrays_precede_module_values_for_both_assemblers(self):
        for nasm in (False, True):
            with self.subTest(nasm=nasm):
                files = fixture(
                    ('USE_NASM()\n' if nasm else '')
                    + 'ASM_PREINCLUDE(shared/module.asm)\n'
                    + 'SRC(vector.asm -D SOURCE=1 PREINCLUDES shared/local.asm '
                    'PREINCLUDES shared/second.asm)'
                )
                for name in ('local', 'second', 'module'):
                    files[f'shared/{name}.asm'] = ''
                graph = lib.make(files, 'copper', *X64)
                self.assertEqual(args(graph)[-11:], [
                    '-D', 'SOURCE=1', '-o', '$(B)/copper/vector.o',
                    '-P', 'shared/local.asm', '-P', 'shared/second.asm',
                    '-P', 'shared/module.asm', '$(S)/copper/vector.asm',
                ])
                node = lib.node_by_output(graph, '$(B)/copper/vector.o')
                for name in ('local', 'second', 'module'):
                    self.assertEqual(f'$(S)/shared/{name}.asm' in node['inputs'], not nasm)

    def test_shared_preinclude_scan_uses_each_modules_assembler_include_roots(self):
        files = fixture('')
        files['shared/setup.asm'] = '%include "choice.asm"\n'
        for module in ('copper', 'silver'):
            files[f'{module}/ya.make'] = (
                BARE + f'ADDINCL(FOR asm {module}/headers)\n'
                'ASM_PREINCLUDE(shared/setup.asm)\nSRCS(vector.asm)\nEND()\n'
            )
            files[f'{module}/vector.asm'] = ''
            files[f'{module}/headers/choice.asm'] = ''
        for targets in (('copper', 'silver'), ('silver', 'copper')):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, *X64)
                for module in targets:
                    node = lib.node_by_output(graph, f'$(B)/{module}/vector.o')
                    self.assertCountEqual(node['inputs'], ['$(B)/contrib/tools/yasm/yasm',
                                                          f'$(S)/{module}/vector.asm',
                                                          '$(S)/shared/setup.asm',
                                                          f'$(S)/{module}/headers/choice.asm'])

    def test_empty_assignment_replaces_preincludes_and_no_resolve_keeps_explicit_input(self):
        files = fixture('ASM_PREINCLUDE(shared/old.asm)\n'
                        'SET(YASM_PREINCLUDES_VALUE)\nASM_PREINCLUDE(${CURDIR}/new.asm)\n'
                        'ENABLE(DONT_RESOLVE_INCLUDES)\nSRCS(vector.asm)')
        files['copper/new.asm'] = '%include "hidden.asm"\n'
        files['copper/hidden.asm'] = ''
        graph = lib.make(files, 'copper', *X64)
        node = lib.node_by_output(graph, '$(B)/copper/vector.o')
        self.assertEqual(args(graph)[-3:], ['-P', '$(S)/copper/new.asm', '$(S)/copper/vector.asm'])
        self.assertCountEqual(node['inputs'], ['$(B)/contrib/tools/yasm/yasm',
                                              '$(S)/copper/vector.asm', '$(S)/copper/new.asm'])

    def test_module_format_overrides_and_windows_i386_condition(self):
        graph = lib.make(fixture(
            'DISABLE(ARCH_X86_64)\nENABLE(ARCH_I386)\nSET(HARDWARE_ARCH 32)\n'
            'SRCS(vector.asm)'), 'copper', '--target-platform', 'default-windows-x86_64')
        self.assertEqual(args(graph)[:5], ['$(B)/contrib/tools/yasm/yasm', '-f', 'win32', '-D', 'WIN32'])
        graph = lib.make(fixture(
            'SET(_YASM_FMT_VALUE custom)\nSET(HARDWARE_ARCH 128)\n'
            'SET(_YASM_PLATFORM_FLAGS_VALUE -D PLATFORM=1)\n'
            'SET(ASM_PREFIX_VALUE --prefix=explicit_)\nSET(_YASM_PREDEFINED_FLAGS_VALUE -g trace)\n'
            'SET(YASM_FLAGS -D MODULE=2)\nSRCS(vector.asm)'), 'copper', *X64)
        self.assertEqual(args(graph)[:16], [
            '$(B)/contrib/tools/yasm/yasm', '-f', 'custom128', '-D', 'PLATFORM=1', *DEBUG,
            '-D', '_x86_64_', '-D_YASM_', '--prefix=explicit_', '-g', 'trace', '-D', 'MODULE=2',
        ])

    def test_registered_generated_preinclude_wins_over_source_shadow(self):
        for spelling in ('setup.asm', '${BINDIR}/setup.asm'):
            with self.subTest(spelling=spelling):
                files = fixture(
                    'COPY(setup.asm FROM ${CURDIR}/incoming)\n'
                    f'ASM_PREINCLUDE({spelling})\nSRCS(vector.asm)'
                )
                files.update({'copper/incoming/setup.asm': '', 'copper/setup.asm': 'source shadow'})
                graph = lib.make(files, 'copper', *X64)
                node = lib.node_by_output(graph, '$(B)/copper/vector.o')
                producer = lib.node_by_output(graph, '$(B)/copper/setup.asm')
                self.assertEqual(args(graph)[-3:], ['-P', 'setup.asm' if spelling == 'setup.asm'
                                                   else '$(B)/copper/setup.asm', '$(S)/copper/vector.asm'])
                self.assertIn('$(B)/copper/setup.asm', node['inputs'])
                self.assertNotIn('$(S)/copper/setup.asm', node['inputs'])
                self.assertIn(producer['uid'], node['deps'])

    def test_parent_relative_preinclude_keeps_command_spelling(self):
        files = fixture('ASM_PREINCLUDE(../shared/setup.asm)\nSRCS(vector.asm)')
        files['shared/setup.asm'] = ''
        graph = lib.make(files, 'copper', *X64)
        self.assertEqual(args(graph)[-3:], ['-P', '../shared/setup.asm', '$(S)/copper/vector.asm'])
        self.assertIn('$(S)/shared/setup.asm', lib.node_by_output(graph, '$(B)/copper/vector.o')['inputs'])

    def test_platform_defaults_prefix_and_explicit_predefined_flags(self):
        for platform, format_, platform_flags, predefined in (
            ('default-linux-x86_64', 'elf64', ['-D', 'UNIX'], ['-g', 'dwarf2']),
            ('default-darwin-x86_64', 'macho64', ['-D', 'DARWIN', '-D', 'UNIX'], []),
            ('default-windows-x86_64', 'win64', ['-D', 'WIN64'], []),
            ('default-ios-arm64', 'macho64', ['-D', 'DARWIN', '-D', 'UNIX'], []),
        ):
            for clear in (False, True):
                with self.subTest(platform=platform, clear=clear):
                    files = fixture('SET(ASM_PREFIX tag_)\n'
                                    + ('SET(_YASM_PREDEFINED_FLAGS_VALUE)\n' if clear else '')
                                    + 'SET(YASM_FLAGS -D LOCAL=7)\nSRCS(vector.asm)')
                    graph = lib.make(files, 'copper', '--target-platform', platform)
                    isa = 'arm64' if 'arm64' in platform else 'x86_64'
                    self.assertEqual(args(graph), [
                        '$(B)/contrib/tools/yasm/yasm', '-f', format_, *platform_flags,
                        *DEBUG, '-D', f'_{isa}_', '-D_YASM_', '--prefix=tag_',
                        *([] if clear else predefined), '-D', 'LOCAL=7', '-I', '$(B)', '-I', '$(S)',
                        '-o', '$(B)/copper/vector.o', '$(S)/copper/vector.asm',
                    ])


if __name__ == '__main__':
    unittest.main(verbosity=2)
