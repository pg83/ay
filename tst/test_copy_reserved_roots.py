"""COPY path variables bind as native typed prefixes, not string fragments."""
import unittest

import lib
from test_proto_py2_context import proto_world


def fixture(separator='', bindings=False):
    body = 'RUN_PROGRAM(tools/grow marker=${ARCADIA_BUILD_ROOT}maple/grown.h OUT_NOAUTO grown.h extra.h)\n'
    copies = [
        ('${ARCADIA_BUILD_ROOT}' + separator + 'maple/grown.h', 'built.h'),
        ('${ARCADIA_ROOT}' + separator + 'maple/raw.h', 'rooted.h'),
        ('${CURDIR}' + separator + 'raw.h', 'local.h'),
        ('${BINDIR}' + separator + 'grown.h', 'local_build.h'),
    ]
    for source, destination in copies:
        body += 'COPY_FILE(' + source + ' ' + destination + ')\n'
    body += ('COPY_FILE_WITH_CONTEXT(${CURDIR}' + separator + 'raw.h context.h OUTPUT_INCLUDES '
             '${ARCADIA_BUILD_ROOT}' + separator + 'maple/extra.h ${ARCADIA_ROOT}' + separator + 'maple/extra_source.h)\n')
    headers = [destination for _, destination in copies] + ['context.h']
    if bindings:
        body += (
            'SET(BASE ${ARCADIA_BUILD_ROOT}maple/)\nSET(FILE ${BASE}grown.h)\nCOPY_FILE(${FILE} bound.h)\n'
            'SET(ARCADIA_BUILD_ROOT_SUFFIX raw.h)\nCOPY_FILE(${ARCADIA_BUILD_ROOT_SUFFIX} suffix.h)\n'
            'SET(ODD_ROOT raw.h)\nCOPY_FILE(${ODD_ROOT} custom.h)\n'
            'COPY_FILE(${MISSING_ROOT}raw.h unknown.h)\n'
            'SET(FRAG raw.h)\nCOPY_FILE(prefix${FRAG} interior.h)\n'
            'COPY_FILE(${BINDIR}extra.h secondary.h)\n'
        )
        headers += ['bound.h', 'suffix.h', 'custom.h', 'unknown.h', 'interior.h', 'secondary.h']
    body += ('COPY_FILE(${ARCADIA_BUILD_ROOT}' + separator + 'maple/grown.h noauto.dat)\n'
             'RESOURCE(noauto.dat copied-data)\nSRCS(main.cpp ' + ' '.join(headers) + ')\n')
    files = proto_world()
    files.update({
        'maple/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + body + 'END()\n',
        'maple/main.cpp': ''.join('#include "maple/' + name + '"\n' for name in headers) + 'int main(){return 0;}\n',
        'maple/raw.h': '#include "deep.h"\n',
        'maple/deep.h': '',
        'maple/extra_source.h': '',
        'build/scripts/fs_tools.py': 'import process_command_files\n',
        'build/scripts/process_command_files.py': '',
        'maple/prefixraw.h': '',
    })
    for tool in ('tools/grow', 'contrib/tools/yasm', 'tools/rorescompiler'):
        lib.tool_program(files, tool, tool.split('/')[-1])
    return files


class CopyReservedRootsTest(unittest.TestCase):
    def graph(self, files, *flags):
        graph = lib.make(files, 'maple', '--target-platform', 'default-linux-x86_64', '--sandboxing', *flags)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
        return graph

    def check_copy(self, graph, name, source, generated=False, secondary=False, context=False):
        node = lib.node_by_output(graph, '$(B)/maple/' + name)
        self.assertEqual(node['cmds'][0]['cmd_args'][1:], [
            '$(S)/build/scripts/fs_tools.py', 'copy', source, '$(B)/maple/' + name,
        ])
        self.assertIn(source, node['inputs'])
        self.assertIn('$(S)/build/scripts/fs_tools.py', node['inputs'])
        if generated:
            producer = lib.node_by_output(graph, '$(B)/maple/grown.h')
            self.assertIn(producer['uid'], node['deps'])
        if secondary:
            self.assertIn('$(B)/maple/grown.h', node['inputs'])
        if context:
            self.assertIn('$(S)/maple/deep.h', node['inputs'])
        return node

    def check_roots(self, graph):
        for name in ('built.h', 'local_build.h', 'noauto.dat'):
            self.check_copy(graph, name, '$(B)/maple/grown.h', generated=True)
        for name in ('rooted.h', 'local.h'):
            self.check_copy(graph, name, '$(S)/maple/raw.h')
        self.check_copy(graph, 'context.h', '$(S)/maple/raw.h', context=True)
        compiled = lib.node_by_output(graph, '$(B)/maple/main.cpp.o')
        for path in ('$(B)/maple/extra.h', '$(S)/maple/extra_source.h', '$(S)/maple/deep.h'):
            self.assertIn(path, compiled['inputs'])
        producer = lib.node_by_output(graph, '$(B)/maple/grown.h')
        self.assertEqual(producer['cmds'][0]['cmd_args'][1:], ['marker=$(B)maple/grown.h'])

    def test_reserved_roots_with_and_without_separator_keep_real_producers(self):
        for separator in ('/', ''):
            with self.subTest(separator=separator):
                self.check_roots(self.graph(fixture(separator)))

    def test_scalar_bindings_and_non_root_names_keep_their_own_meaning(self):
        graph = self.graph(fixture(bindings=True))
        self.check_roots(graph)
        self.check_copy(graph, 'bound.h', '$(B)/maple/grown.h', generated=True)
        for name in ('suffix.h', 'custom.h', 'unknown.h'):
            self.check_copy(graph, name, '$(S)/maple/raw.h')
        self.check_copy(graph, 'interior.h', '$(S)/maple/prefixraw.h')
        self.check_copy(graph, 'secondary.h', '$(B)/maple/extra.h', generated=True, secondary=True)

    def test_cli_reference_chains_still_reach_the_physical_source(self):
        files = fixture('/')
        files['maple/ya.make'] = files['maple/ya.make'].replace('SRCS(main.cpp ', 'COPY_FILE(${R1} chain.h)\nSRCS(main.cpp chain.h ')
        files['maple/main.cpp'] += '#include "maple/chain.h"\n'
        flags = ['-DR' + str(i) + '=${R' + str(i + 1) + '}' for i in range(1, 8)]
        flags += ['-DR8=${ARCADIA_ROOT}maple/raw.h']
        self.check_copy(self.graph(files, *flags), 'chain.h', '$(S)/maple/raw.h')

    def test_resource_globals_keep_runtime_binding_but_are_not_source_paths(self):
        files = fixture('/')
        files['maple/ya.make'] = files['maple/ya.make'].replace(
            'RUN_PROGRAM(tools/grow ', 'PEERDIR(resource)\nRUN_PROGRAM(tools/grow resource=${CEDAR_RESOURCE_GLOBAL} ',
        )
        files['resource/ya.make'] = lib.resource_lib('CEDAR', 71)
        graph = self.graph(files)
        producer = lib.node_by_output(graph, '$(B)/maple/grown.h')
        self.assertIn('resource=$(B)/resources/CEDAR', producer['cmds'][0]['cmd_args'])
        fetched = lib.node_by_output(graph, '$(B)/resources/CEDAR')
        self.assertIn('sbr:71', fetched['cmds'][0]['cmd_args'])
        bound_files = fixture('/')
        bound_files['maple/ya.make'] = bound_files['maple/ya.make'].replace(
            'SRCS(main.cpp ', 'SET(CEDAR_RESOURCE_GLOBAL ${ARCADIA_ROOT}maple/raw.h)\n'
            + 'COPY_FILE_WITH_CONTEXT(${CEDAR_RESOURCE_GLOBAL} bound_resource.h)\nSRCS(main.cpp bound_resource.h ',
        )
        bound_files['maple/main.cpp'] += '#include "maple/bound_resource.h"\n'
        self.check_copy(self.graph(bound_files), 'bound_resource.h', '$(S)/maple/raw.h', context=True)
        for declaration in ('', 'DECLARE_EXTERNAL_RESOURCE(CEDAR sbr:71)\n'):
            with self.subTest(declaration=declaration):
                files = fixture('/')
                files['maple/ya.make'] = files['maple/ya.make'].replace(
                    'SRCS(main.cpp ', 'PEERDIR(resource)\n' + declaration
                    + 'COPY_FILE_WITH_CONTEXT(${CEDAR_RESOURCE_GLOBAL}/raw.h fetched.h)\nSRCS(main.cpp fetched.h ',
                )
                files['maple/main.cpp'] += '#include "maple/fetched.h"\n'
                files['resource/ya.make'] = 'RESOURCES_LIBRARY()\nEND()\n'
                result = lib.make_process(files, 'maple', '--target-platform', 'default-linux-x86_64')
                self.assertEqual(result.returncode, 1)
                self.assertIn('CEDAR_RESOURCE_GLOBAL', result.stderr)


if __name__ == '__main__':
    unittest.main()
