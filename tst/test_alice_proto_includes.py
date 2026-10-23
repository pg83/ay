import base64
import json
import unittest

import lib
from test_proto_cpp import proto_files


DATA = 'orchard/registry'
B = '$(B)/' + DATA + '/'
GENERATOR = 'alice/functions/function_proto_converters/generator'


def fixture(mode='glob'):
    files = proto_files()
    lib.tool_program(files, GENERATOR, 'generator')
    files['alice/functions/proto/ya.make'] = 'PROTO_LIBRARY()\n' + lib.BARE_MODULE + 'ONLY_TAGS(CPP_PROTO)\nPEERDIR(alice/wires)\nEND()\n'
    files['alice/wires/ya.make'] = 'PROTO_LIBRARY()\n' + lib.BARE_MODULE + 'ONLY_TAGS(CPP_PROTO)\nSRCS(alpha.proto nested/beta.proto)\nEND()\n'
    files['alice/wires/alpha.proto'] = '''syntax = "proto3";
// message Ghost { and an unmatched brace {
/* message Hidden { } */
package orchard.api;
message Outer {
  message Inner { }
}
message Tail {}
'''
    files['alice/wires/nested/beta.proto'] = 'syntax = "proto3";\nmessage Solo {}\n'
    invocation = {
        'glob': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'relative': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ../../alice/wires)',
        'direct': 'ALICE_GENERATE_FUNCTION_PROTO_INCLUDES(${ARCADIA_ROOT}/alice/wires/nested/beta.proto ignored.txt ${ARCADIA_ROOT}/alice/wires/alpha.proto)',
        'empty': 'ALICE_GENERATE_PROTO_INCLUDES(DIR nowhere)',
        'none': '',
        'default': 'ALICE_GENERATE_PROTO_INCLUDES()',
        'separators': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'unicode': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'python-tool': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'mixed-before': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'mixed-after': 'ALICE_GENERATE_PROTO_INCLUDES(DIR ${ARCADIA_ROOT}/alice/wires)',
        'build': 'ALICE_GENERATE_FUNCTION_PROTO_INCLUDES(${ARCADIA_BUILD_ROOT}/alice/wires/alpha.proto)',
    }[mode]
    if mode == 'default':
        files[DATA + '/local.proto'] = 'syntax = \"proto3\";\n'
        files['h'] = '#pragma once\n'
    if mode == 'separators':
        special = 'line\u2028row\u2029end.proto'
        files['alice/wires/' + special] = 'syntax = \"proto3\";\npackage literal\\u2028.literal\\u2029;\nmessage Boundary {}\n'
        files['alice/wires/ya.make'] = files['alice/wires/ya.make'].replace('SRCS(', 'SRCS(\"' + special.replace('\\', '\\\\') + '\" ')
    if mode == 'unicode':
        files['alice/wires/alpha.proto'] = 'syntax = \"proto3\";\npackage café.api;\noption note = \"αmessage Ghost {}\";\nmessage Éclair {}\n'
    files[DATA + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'PEERDIR(alice/functions/proto)\n' + invocation + '\nEND()\n'
    if mode.startswith('mixed-'):
        files[DATA + '/mark.cpp'] = ''
        before, after = ('SRCS(mark.cpp)\n', '') if mode == 'mixed-before' else ('', 'SRCS(mark.cpp)\n')
        files[DATA + '/ya.make'] = files[DATA + '/ya.make'].replace(invocation, before + invocation + '\n' + after)
    files['reader/ya.make'] = 'PROGRAM()\n' + lib.BARE_MODULE + 'PEERDIR(' + DATA + ')\nSRCS(main.cpp)\nEND()\n'
    files['reader/main.cpp'] = '#include <' + DATA + '/proto_includes.h>\n'
    if mode == 'python-tool':
        from test_python_srcs import python_base
        files.update(python_base())
        files[GENERATOR + '/ya.make'] = 'PY3_PROGRAM()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nPY_SRCS(MAIN entry.py)\nEND()\n'
        files[GENERATOR + '/entry.py'] = ''
    return files


class AliceProtoIncludesTest(unittest.TestCase):
    def check_graph(self, graph, expected):
        self.assertTrue(graph['result'])
        generated = lib.node_by_output(graph, B + 'proto_includes.h')
        self.assertEqual(generated['outputs'], [B + 'proto_includes.h', B + 'proto_includes.cpp'])
        args = generated['cmds'][0]['cmd_args']
        self.assertEqual(args[:2], ['$(B)/' + GENERATOR + '/generator', B + 'proto_includes.h'])
        self.assertEqual(base64.b64decode(args[2]).decode(), json.dumps(expected, ensure_ascii=False, sort_keys=True))
        self.assertEqual(generated['inputs'], ['$(B)/' + GENERATOR + '/generator'])
        cc = lib.node_by_output(graph, B + 'proto_includes.cpp.o')
        caller = lib.node_by_output(graph, '$(B)/reader/main.cpp.o')
        archive = lib.node_by_output(graph, B + 'liborchard-registry.a')
        link = lib.node_by_output(graph, '$(B)/reader/reader')
        for compiled in (cc, caller):
            self.assertIn(B + 'proto_includes.h', compiled['inputs'])
            self.assertIn(generated['uid'], compiled['deps'])
            for header in expected:
                pb = lib.node_by_output(graph, '$(B)/' + header)
                self.assertIn('$(B)/' + header, compiled['inputs'])
                self.assertIn(pb['uid'], compiled['deps'])
        self.assertIn(cc['outputs'][0], archive['inputs'])
        self.assertIn(cc['uid'], archive['deps'])
        self.assertIn(archive['outputs'][0], link['inputs'])
        self.assertIn(archive['uid'], link['deps'])
        self.assertIn(archive['outputs'][0].removeprefix('$(B)/'), [a for c in link['cmds'] for a in c['cmd_args']])
        uids = {n['uid'] for n in graph['graph']}
        self.assertTrue(all(dep in uids for n in graph['graph'] for dep in n['deps']))

    def test_directory_glob_parses_top_level_messages_and_preserves_producers(self):
        for mode in ('glob', 'relative', 'direct', 'python-tool'):
            with self.subTest(mode=mode):
                graph = lib.make(fixture(mode), 'reader', '--target-platform', 'default-linux-x86_64')
                self.check_graph(graph, {'alice/wires/alpha.pb.h': ['orchard::api::Outer', 'orchard::api::Tail'],
                                         'alice/wires/nested/beta.pb.h': ['Solo']})

    def test_plugin_declared_source_precedes_ordinary_source_calls(self):
        for mode in ('mixed-before', 'mixed-after'):
            with self.subTest(mode=mode):
                graph = lib.make(fixture(mode), 'reader', '--target-platform', 'default-linux-x86_64')
                archive = lib.node_by_output(graph, B + 'liborchard-registry.a')
                members = [a for a in archive['cmds'][0]['cmd_args'] if a.endswith('.o')]
                self.assertEqual(members, [B + 'proto_includes.cpp.o', B + 'mark.cpp.o'])

    def test_empty_glob_still_generates_and_compiles_registration_source(self):
        graph = lib.make(fixture('empty'), 'reader', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph, {})

    def test_default_directory_and_empty_message_list_follow_the_plugin(self):
        graph = lib.make(fixture('default'), 'reader', '--target-platform', 'default-linux-x86_64')
        generated = lib.node_by_output(graph, B + 'proto_includes.h')
        self.assertEqual(base64.b64decode(generated['cmds'][0]['cmd_args'][2]).decode(), '{"h": []}')
        compiled = lib.node_by_output(graph, '$(B)/reader/main.cpp.o')
        self.assertIn('$(S)/h', compiled['inputs'])
        self.assertIn(generated['uid'], compiled['deps'])

    def test_request_preserves_unicode_without_ascii_escaping(self):
        graph = lib.make(fixture('unicode'), 'reader', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph, {'alice/wires/alpha.pb.h': ['café::api::Éclair'],
                                'alice/wires/nested/beta.pb.h': ['Solo']})

    def test_request_keeps_line_separators_and_literal_backslash_u_distinct(self):
        graph = lib.make(fixture('separators'), 'reader', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph, {'alice/wires/alpha.pb.h': ['orchard::api::Outer', 'orchard::api::Tail'],
                                'alice/wires/nested/beta.pb.h': ['Solo'],
                                'alice/wires/line\u2028row\u2029end.pb.h': ['literal\\u2028::literal\\u2029::Boundary']})

    def test_configure_time_parser_does_not_read_a_build_output_as_source(self):
        result = lib.make_process(fixture('build'), 'reader', '--target-platform', 'default-linux-x86_64')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('needs a source proto file', result.stderr)

    def test_an_ordinary_empty_library_remains_empty(self):
        files = fixture('none')
        files['reader/main.cpp'] = ''
        graph = lib.make(files, 'reader', '--target-platform', 'default-linux-x86_64')
        self.assertFalse(any(B + 'proto_includes.h' in n['outputs'] for n in graph['graph']))
        self.assertFalse(any(B + 'liborchard-registry.a' in n['outputs'] for n in graph['graph']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
