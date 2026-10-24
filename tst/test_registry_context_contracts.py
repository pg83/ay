"""Supported module variants share producers without changing their owner."""
import unittest

import lib
import test_link_sbom
import test_nots
import test_proto_flatc
import test_sandbox_legacy_task
import test_yql


X86_64 = ('--target-platform', 'default-linux-x86_64')


def nots_tool_workspace():
    files = test_nots.NotsTests().project()
    files['web/package.json'] = '{"name":"web"}'
    files['web/pnpm-lock.yaml'] = "lockfileVersion: '9.0'\nimporters:\n  .: {}\npackages: {}\n"
    files['parent/ya.make'] = 'TS_PACKAGE()\nEND()\n'
    files['parent/package.json'] = '{"name":"parent","dependencies":{"web":"workspace:../web"}}'
    files['parent/pnpm-lock.yaml'] = (
        "lockfileVersion: '9.0'\nimporters:\n  .:\n    dependencies:\n"
        "      web:\n        specifier: workspace:../web\n        version: link:../web\npackages: {}\n"
    )
    files['devtools/frontend_build_platform/nots/constants/src/tier0-settings.yaml'] = (
        'pnpm:\n  supportedCustomSettings: {}\n  defaults:\n    registry: https://npm.yandex-team.ru\n'
    )
    files['parcel/ya.make'] = (
        'PROGRAM(Parcel)\n' + lib.BARE_MODULE
        + 'ALLOCATOR(FAKE)\nADDINCL(${ARCADIA_BUILD_ROOT}/parcel)\n'
        'RUN_PYTHON3(consume.py ${ARCADIA_BUILD_ROOT}/web/output.tar '
        '${ARCADIA_BUILD_ROOT}/parent/output.tar ${BINDIR}/stamp.h '
        'TOOL web parent OUT stamp.h)\nSRCS(main.cpp)\nEND()\n'
    )
    files['parcel/main.cpp'] = '#include "stamp.h"\nint main() {}\n'
    files['parcel/consume.py'] = (
        'import pathlib, sys, tarfile\n'
        'names = [name for path in sys.argv[1:3] for name in tarfile.open(path).getnames()]\n'
        'pathlib.Path(sys.argv[3]).write_text("#pragma once\\n// " + " ".join(names))\n'
    )
    return files


class RegistryContextContractsTest(unittest.TestCase):
    def assert_graph(self, graph):
        ids = {node['uid'] for node in graph['graph']}
        self.assertEqual(len(ids), len(graph['graph']))
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
            for refs in node.get('foreign_deps', {}).values():
                self.assertLessEqual(set(refs), ids)

    def test_generated_flatbuffer_preserves_the_canonical_cpp_producer(self):
        files = lib.java_tree(**test_proto_flatc.flatc_files())
        lib.tool_program(files, 'tools/carve', 'carve')
        files.update({
            'grove/ya.make': (
                'FBS_LIBRARY()\n' + lib.BARE_MODULE
                + 'ONLY_TAGS(CPP_FBS JAVA_FBS)\n'
                'RUN_PROGRAM(tools/carve IN seed.txt OUT_NOAUTO model.fbs)\n'
                'SRCS(${ARCADIA_BUILD_ROOT}/grove/model.fbs)\nEND()\n'
            ),
            'grove/seed.txt': 'table Model {}\n',
            'contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make':
                'JAVA_CONTRIB()\nEND()\n',
        })
        graph = lib.make(files, 'grove', '--sandboxing', *X86_64)
        self.assert_graph(graph)
        schema = lib.node_by_output(graph, '$(B)/grove/model.fbs')
        header = lib.node_by_output(graph, '$(B)/grove/model.fbs.h')
        self.assertEqual(sum('$(B)/grove/model.fbs.h' in n['outputs'] for n in graph['graph']), 1)
        self.assertEqual(header['outputs'], [
            '$(B)/grove/model.fbs.h', '$(B)/grove/model.fbs.cpp', '$(B)/grove/model.bfbs'])
        self.assertIn(schema['uid'], header['deps'])
        self.assertIn('$(B)/grove/model.fbs', header['inputs'])
        self.assertIn('$(S)/grove/seed.txt', schema['inputs'])
        compiled = lib.node_by_output(graph, '$(B)/grove/model.fbs.cpp.o')
        self.assertIn(header['uid'], compiled['deps'])
        java = lib.node_by_output(graph, '$(B)/grove/model.fbs.jsrc')
        self.assertIn(schema['uid'], java['deps'])
        self.assertIn(java['uid'], lib.node_by_output(graph, '$(B)/grove/grove.jar')['deps'])

    def test_sandbox_binary_and_library_share_the_declared_rodata_archive(self):
        files = test_sandbox_legacy_task.fixture()
        task = test_sandbox_legacy_task.TASK
        files[task + '/ya.make'] = files[task + '/ya.make'].replace(
            'END()', 'ARCHIVE_ASM(NAME CedarLedger payload.bin payload.bin)\nEND()')
        files[task + '/payload.bin'] = 'ledger\n'
        lib.tool_program(files, 'tools/archiver', 'archiver')
        lib.tool_program(files, 'contrib/tools/yasm', 'yasm')
        files['build/scripts/rodata2asm.py'] = ''
        graph = lib.make(files, task, '--sandboxing', '--strip-packages-from-results', *X86_64)
        self.assert_graph(graph)
        output = '$(B)/' + task + '/CedarLedger.rodata'
        producers = [node for node in graph['graph'] if output in node['outputs']]
        self.assertEqual(len(producers), 1)
        archive = producers[0]
        tool = lib.node_by_output(graph, '$(B)/tools/archiver/archiver')
        member = '$(S)/' + task + '/payload.bin'
        self.assertEqual(archive['cmds'][0]['cmd_args'], [
            tool['outputs'][0], '-q', member + ':', member + ':', '-o', output])
        self.assertCountEqual(archive['inputs'], [member, tool['outputs'][0]])
        self.assertIn(tool['uid'], archive['deps'])
        for tag in ('SB_TASK_BIN', 'PY2'):
            root = test_sandbox_legacy_task.task_node(graph, tag)
            self.assertIn(root['uid'], graph['result'])
        rodata = [node for node in graph['graph'] if output + '.o' in node['outputs']]
        self.assertTrue(rodata)
        self.assertTrue(all(archive['uid'] in node['deps'] for node in rodata))
        for path in ('cedar', 'libpycedar.a'):
            consumer = lib.node_by_output(graph, '$(B)/' + task + '/' + path)
            self.assertTrue(any(node['uid'] in consumer['deps'] for node in rodata))

    def test_sandbox_schema_contexts_keep_the_first_header_producer(self):
        files = test_sandbox_legacy_task.fixture()
        files.update(test_proto_flatc.flatc_files())
        task = test_sandbox_legacy_task.TASK
        files[task + '/ya.make'] = files[task + '/ya.make'].replace(
            'END()', 'RUN_PROGRAM(tools/marker IN seed.txt OUT_NOAUTO model.fbs)\n'
            'SRCS(${ARCADIA_BUILD_ROOT}/' + task + '/model.fbs)\nEND()')
        files[task + '/seed.txt'] = 'table Model {}\n'
        graph = lib.make(files, task, '--sandboxing', *X86_64)
        self.assert_graph(graph)
        base = '$(B)/' + task + '/model.fbs'
        header = lib.node_by_output(graph, base + '.h')
        self.assertEqual(sum(base + '.h' in node['outputs'] for node in graph['graph']), 1)
        schema = lib.node_by_output(graph, base)
        self.assertIn(schema['uid'], header['deps'])
        self.assertIn(base, header['inputs'])
        compiled = [node for node in graph['graph'] if base + '.cpp.o' in node['outputs']]
        self.assertEqual(len(compiled), 2)
        for context in ('BINARY', 'LIBRARY'):
            node = next(node for node in compiled if
                        '-DSANDBOX_' + context + '_CONTEXT' in node['cmds'][0]['cmd_args'])
            self.assertIn(header['uid'], node['deps'])
            self.assertIn(base + '.h', node['inputs'])

    def test_udf_abi_variants_share_their_own_component(self):
        files = test_link_sbom.fixture()
        files.update(test_yql.udf_fixture())
        files.update({
            'udf/ya.make': ('YQL_UDF(Mint)\n' + lib.BARE_MODULE
                            + 'LICENSE(MIT)\nVERSION(2 7)\nUDF_NO_PROBE()\nSRCS(body.cpp)\nEND()\n'),
            'udf/body.cpp': 'int body;\n',
        })
        for project, abi in (('stable', ''), ('current', 'YQL_CURRENT_ABI_VERSION()\n')):
            files[project + '/ya.make'] = ('PROGRAM()\n' + lib.BARE_MODULE
                                          + 'ALLOCATOR(FAKE)\n' + abi + 'PEERDIR(udf)\nEND()\n')
        graph = lib.make(files, 'udf', 'stable', 'current', '--sandboxing', *X86_64)
        self.assert_graph(graph)
        output = '$(B)/udf/Mint.CPP.component.sbom'
        components = [node for node in graph['graph'] if output in node['outputs']]
        self.assertEqual(len(components), 1)
        component = components[0]
        args = component['cmds'][0]['cmd_args']
        self.assertEqual(args[args.index('--ver') + 1], '2.7')
        self.assertEqual(args[args.index('--path') + 1], 'udf')
        self.assertEqual(component['inputs'], ['$(S)/build/internal/scripts/gen_sbom.py'])
        for name in ('libMint.global.a', 'yql-abi-current-Mint.global.a'):
            archive = lib.node_by_output(graph, '$(B)/udf/' + name)
            self.assertIn(archive['uid'], graph['result'])
        for project in ('stable', 'current'):
            consumer = lib.node_by_output(graph, '$(B)/' + project + '/' + project)
            self.assertIn(component['uid'], consumer['deps'])
            self.assertIn(output, consumer['inputs'])

    def test_python_tools_share_typescript_workspace_preparation(self):
        graph = lib.make(nots_tool_workspace(), 'parcel', '--sandboxing', *X86_64)
        self.assert_graph(graph)
        output = lib.node_by_output(graph, '$(B)/parcel/stamp.h')
        self.assertIn('$(S)/parcel/consume.py', output['inputs'])
        compiled = lib.node_by_output(graph, '$(B)/parcel/main.cpp.o')
        self.assertIn(output['uid'], compiled['deps'])
        self.assertIn(output['outputs'][0], compiled['inputs'])
        for project in ('web', 'parent'):
            package = lib.node_by_output(graph, '$(B)/' + project + '/output.tar')
            self.assertIn(package['uid'], output['deps'])
            self.assertIn(package['outputs'][0], output['inputs'])
            preparations = [node for node in graph['graph']
                            if '$(B)/' + project + '/package.json' in node['outputs']]
            self.assertEqual(len(preparations), 1)
            self.assertIn(preparations[0]['uid'], package['deps'])
        web = lib.node_by_output(graph, '$(B)/web/output.tar')
        parent = lib.node_by_output(graph, '$(B)/parent/output.tar')
        self.assertIn(web['uid'], parent['deps'])


if __name__ == '__main__':
    unittest.main()
