import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from tst import lib
from tst.dev.test_dump import raw_node


class DumpEdgeContractsTest(unittest.TestCase):
    def normalize(self, graph):
        with tempfile.TemporaryDirectory(prefix='ay-dump-edge-') as directory:
            path = Path(directory) / 'graph.json'
            path.write_text(graph if isinstance(graph, str) else json.dumps(graph))
            return subprocess.run([str(lib.NORMALIZE), '--in', str(path),
                                   '--ref-graph'],
                                  capture_output=True, text=True, timeout=10)

    def test_non_string_key_after_graph_is_diagnosed(self):
        result = self.normalize('{"graph": [], 1: []}')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('expected object key', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_unknown_trailing_fields_do_not_hide_result_roots(self):
        node = raw_node('root', 'XX', '$(B)/app/result')
        result = self.normalize({'graph': [node], 'extra': {'nested': [1, 2]}, 'result': ['root']})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)['outputs'], ['$(B)/app/result'])

    def test_other_enum_commands_keep_all_declared_inputs(self):
        for commands in ([], [{'cmd_args': ['first']}, {'cmd_args': ['second']}],
                         [{'cmd_args': ['other', 'header', '--include-path', 'header', '--output', 'out']}],
                         [{'cmd_args': ['$(B)/tools/enum_parser/enum_parser/enum_parser', 'header', '--bad', 'header', '--output', 'out']}]):
            with self.subTest(commands=commands):
                enum = raw_node('enum', 'EN', '$(B)/app/enum.cpp', inputs=['$(S)/app/a.h', '$(S)/app/b.h'], cmds=commands)
                root = raw_node('root', 'LD', '$(B)/app/app', deps=['enum'])
                result = self.normalize({'graph': [root, enum], 'result': ['root']})
                self.assertEqual(result.returncode, 0, result.stderr)
                normalized = next(n for n in map(json.loads, result.stdout.splitlines()) if n['kv']['p'] == 'EN')
                self.assertEqual(normalized['inputs'], enum['inputs'])

    def test_enum_preserves_input_producers_with_empty_primary_outputs(self):
        tool = '$(B)/tools/enum_parser/enum_parser/enum_parser'
        header = '$(S)/app/a.h'
        enum = raw_node('enum', 'EN', '$(B)/app/enum.cpp',
                        inputs=[tool, header, '$(B)/app/removed.h', '$(B)/app/command.h'], deps=['tool', 'removed', 'empty'],
                        cmds=[{'cmd_args': [tool, header, '--include-path', '$(B)/app/command.h', '--output', '$(B)/app/enum.cpp']}])
        nodes = [raw_node('root', 'LD', '$(B)/app/app', deps=['enum']), enum,
                 raw_node('tool', 'LD', tool), raw_node('removed', 'CC', ['', '$(B)/app/removed.h']),
                 raw_node('empty', 'CC', ['', '$(B)/app/command.h'])]
        result = self.normalize({'graph': nodes, 'result': ['root']})
        self.assertEqual(result.returncode, 0, result.stderr)
        normalized = list(map(json.loads, result.stdout.splitlines()))
        enum = next(n for n in normalized if n['kv']['p'] == 'EN')
        command = next(n for n in normalized if '$(B)/app/command.h' in n['outputs'])
        self.assertIn(command['uid'], enum['deps'])
        removed = next(n for n in normalized if '$(B)/app/removed.h' in n['outputs'])
        self.assertIn(removed['uid'], enum['deps'])
        self.assertEqual(enum['inputs'], sorted(nodes[1]['inputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
