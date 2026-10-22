import unittest

import lib
from test_java import contrib
from test_proto_cpp import proto_files
from test_proto_desc import desc_files
from test_proto_gztproto import gazetteer_files


def files(tags):
    sources = {**lib.java_tree(), **proto_files(), **desc_files(),
               **gazetteer_files('${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto')}
    sources.update(contrib('contrib/java/com/google/protobuf/protobuf-java/4.33.0', jar=7))
    sources['kernel/gazetteer/proto/ya.make'] = (
        'PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO JAVA_PROTO DESC_PROTO)\n'
        'PROTO_NAMESPACE(kernel/gazetteer/proto)\nSRCS(base.proto)\nEND()\n'
    )
    sources['p/ya.make'] = (
        f'PROTO_LIBRARY(named)\nONLY_TAGS({tags})\nSRCS(shared.gztproto imported.gztproto manual.proto)\n'
        'IF(MODULE_TAG == "JAVA_PROTO")\nSRCS(java.gztproto)\n'
        'ELSEIF(MODULE_TAG == "DESC_PROTO")\nSRCS(desc.gztproto)\n'
        'ELSE()\nSRCS(native.gztproto)\nENDIF()\nEND()\n'
    )
    sources['p/shared.gztproto'] = 'import "p/imported.gztproto";\nmessage Shared {}\n'
    sources['p/imported.gztproto'] = 'import "base.proto";\nmessage Imported {}\n'
    sources['p/manual.proto'] = 'syntax = "proto2";\nmessage Manual {}\n'
    for name in ('java', 'desc', 'native'):
        sources[f'p/{name}.gztproto'] = 'message Context {}\n'
    return sources


class GztprotoContextsTest(unittest.TestCase):
    def test_repeated_gzt_declaration_in_one_context_is_rejected(self):
        for tags in ('CPP_PROTO', 'JAVA_PROTO', 'DESC_PROTO'):
            with self.subTest(tags=tags):
                sources = files(tags)
                sources['p/ya.make'] = f'PROTO_LIBRARY()\nONLY_TAGS({tags})\nSRCS(shared.gztproto shared.gztproto)\nEND()\n'
                result = lib.make_process(sources, 'p')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('duplicate producer', result.stderr)

    def test_unrelated_producer_cannot_replace_a_gzt_converter(self):
        for owner in ('p', 'foreign'):
            with self.subTest(owner=owner):
                sources = files('CPP_PROTO')
                lib.tool_program(sources, 'tools/generate', 'generate')
                command = 'RUN_PROGRAM(tools/generate OUT_NOAUTO ${ARCADIA_BUILD_ROOT}/p/shared.proto)\n'
                if owner == 'p':
                    sources['p/ya.make'] = sources['p/ya.make'].replace('SRCS(shared.gztproto', command + 'SRCS(shared.gztproto')
                else:
                    sources['p/ya.make'] = sources['p/ya.make'].replace('SRCS(shared.gztproto', 'PEERDIR(foreign)\nSRCS(shared.gztproto')
                    sources['foreign/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}{command}END()\n'
                result = lib.make_process(sources, 'p')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('duplicate producer', result.stderr)

    def test_distinct_sources_with_the_same_basename_are_rejected(self):
        for declarations in (
            'ONLY_TAGS(CPP_PROTO)\nSRCS(left/schema.gztproto right/schema.gztproto)\n',
            'ONLY_TAGS(CPP_PROTO JAVA_PROTO)\nIF(MODULE_TAG == "CPP_PROTO")\nSRCDIR(p/left)\n'
            'ELSE()\nSRCDIR(p/right)\nENDIF()\nSRCS(schema.gztproto)\n',
        ):
            with self.subTest(declarations=declarations):
                sources = files('CPP_PROTO')
                sources['p/ya.make'] = f'PROTO_LIBRARY()\n{declarations}END()\n'
                for directory in ('left', 'right'):
                    sources[f'p/{directory}/schema.gztproto'] = 'message Different {}\n'
                result = lib.make_process(sources, 'p')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('duplicate producer', result.stderr)
                self.assertIn('left/schema.gztproto', result.stderr)
                self.assertIn('right/schema.gztproto', result.stderr)

    def test_shared_gzt_output_retains_first_owner_includes_and_runs_each_context(self):
        for tags, owner in (('CPP_PROTO JAVA_PROTO', 'native_peer'), ('JAVA_PROTO', 'java_peer')):
            with self.subTest(tags=tags):
                sources = files(tags)
                sources['p/ya.make'] = (
                    f'PROTO_LIBRARY(named)\nONLY_TAGS({tags})\nSRCS(shared.gztproto)\n'
                    'IF(MODULE_TAG == "CPP_PROTO")\nPEERDIR(native_peer)\n'
                    'ELSE()\nPEERDIR(java_peer)\nENDIF()\nEND()\n'
                )
                sources['p/shared.gztproto'] = 'import "context.proto";\nmessage Shared {}\n'
                for peer in ('native_peer', 'java_peer'):
                    sources[f'{peer}/ya.make'] = (
                        'PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO JAVA_PROTO)\n'
                        f'PROTO_NAMESPACE({peer})\nSRCS(context.proto)\nEND()\n'
                    )
                    sources[f'{peer}/context.proto'] = 'syntax = "proto2";\nmessage Context {}\n'
                graph = lib.make(sources, 'p')
                converter = lib.node_by_output(graph, '$(B)/p/shared.proto')
                self.assertIn(f'-I$(S)/{owner}', converter['cmds'][0]['cmd_args'])
                self.assertIn(f'$(S)/{owner}/context.proto', converter['inputs'])
                if owner == 'native_peer':
                    self.assertNotIn('-I$(S)/java_peer', converter['cmds'][0]['cmd_args'])
                jar = lib.node_by_output(graph, '$(B)/p/named.jar')
                self.assertIn('$(B)/java_peer/java_peer.jar', jar['inputs'])
                self.assertNotIn('$(B)/native_peer/native_peer.jar', jar['inputs'])

    def test_generated_proto_flows_into_each_selected_language(self):
        for tags in ('CPP_PROTO', 'JAVA_PROTO', 'DESC_PROTO', 'CPP_PROTO JAVA_PROTO DESC_PROTO'):
            with self.subTest(tags=tags):
                graph = lib.make(files(tags), 'p')
                outputs = {output for node in graph['graph'] for output in node['outputs']}
                for tag, source, product in (
                    ('CPP_PROTO', 'native', 'native.pb.cc.o'),
                    ('JAVA_PROTO', 'java', 'java.jsrc'),
                    ('DESC_PROTO', 'desc', 'desc.proto.desc'),
                ):
                    self.assertEqual(f'$(B)/p/{source}.proto' in outputs, tag in tags)
                    self.assertEqual(f'$(B)/p/{product}' in outputs, tag in tags)
                    if tag in tags:
                        generated = f'{source}.pb.h' if tag == 'CPP_PROTO' else product
                        product_node = lib.node_by_output(graph, f'$(B)/p/{generated}')
                        self.assertIn('$(S)/kernel/gazetteer/proto/base.proto', product_node['inputs'])
                imported = lib.node_by_output(graph, '$(B)/p/imported.proto')
                shared = lib.node_by_output(graph, '$(B)/p/shared.proto')
                products = []
                if 'CPP_PROTO' in tags:
                    products.append('$(B)/p/shared.pb.h')
                if 'JAVA_PROTO' in tags:
                    products.append('$(B)/p/shared.jsrc')
                    jar = lib.node_by_output(graph, '$(B)/p/named.jar')
                    self.assertIn('$(B)/p/shared.jsrc', jar['inputs'])
                    self.assertIn('$(B)/kernel/gazetteer/proto/kernel-gazetteer-proto.jar', jar['inputs'])
                    args = jar['cmds'][0]['cmd_args']
                    self.assertEqual([arg for arg in args if arg.endswith('.jsrc')], [
                        f'$(B)/p/{name}.jsrc' for name in ('manual', 'shared', 'imported', 'java')
                    ])
                if 'DESC_PROTO' in tags:
                    products.append('$(B)/p/shared.proto.desc')
                    merge = lib.node_by_output(graph, '$(B)/p/named.self.protodesc')
                    descriptor = lib.node_by_output(graph, '$(B)/p/shared.proto.desc')
                    for output in descriptor['outputs']:
                        self.assertIn(output, merge['inputs'])
                    self.assertIn(descriptor['uid'], merge['deps'])
                    self.assertEqual(merge['cmds'][0]['cmd_args'][3:], [
                        f'$(B)/p/{name}.proto.desc' for name in ('manual', 'shared', 'imported', 'desc')
                    ])
                for output in products:
                    node = lib.node_by_output(graph, output)
                    self.assertEqual(node['cmds'][0]['cwd'], '$(B)')
                    for path in ('$(B)/p/shared.proto', '$(B)/p/imported.proto',
                                 '$(S)/kernel/gazetteer/proto/base.proto'):
                        self.assertIn(path, node['inputs'])
                    self.assertIn(shared['uid'], node['deps'])
                    self.assertIn(imported['uid'], node['deps'])
                    self.assertNotIn('$(S)/p/shared.gztproto', node['inputs'])
                self.assertEqual(sum('$(B)/p/shared.proto' in node['outputs'] for node in graph['graph']), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
