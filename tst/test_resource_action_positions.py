"""Resource actions retain the positions of their complete source declarations."""
import unittest

import lib
from test_python_proto import python_base


MODULE = 'schema'
PREFIX = '$(B)/schema/'


def fixture(kind, count=2, reverse=False):
    files = python_base()
    files['app/ya.make'] = files.pop('app/ya.make').replace('PEERDIR(proto)', 'PEERDIR(schema)')
    files['schema/ordinary.py'] = ''
    files['schema/note.dat'] = 'note'
    if kind == 'proto':
        series = [f'wire{i:02}.proto' for i in range(count)]
        calls = ['SRCS(' + ' '.join(series) + ')', 'SRCS(extra.proto)']
        if reverse:
            calls.reverse()
        body = '\n'.join(calls)
        for name in [*series, 'extra.proto']:
            files['schema/' + name] = 'syntax = "proto3";\n'
        files['schema/ya.make'] = 'PROTO_LIBRARY()\n' + body + '\nEXCLUDE_TAGS(CPP_PROTO)\nEND()\n'
    else:
        if kind == 'bundle':
            lib.tool_program(files, 'tools/writer', 'writer')
            bundle = 'BUNDLE(tools/writer NAME brought.txt)'
            resources = 'RESOURCE(brought.txt bundle)\nRESOURCE(note.dat note)'
            body = resources + '\n' + bundle if reverse else bundle + '\n' + resources
        else:
            for tool in ('tools/rorescompiler', 'contrib/tools/yasm'):
                lib.tool_program(files, tool, tool.rsplit('/', 1)[-1])
            ordinary = [f'piece{i:02}.txt key{i:02}' for i in range(80)]
            special = [f'conftest.py.part{i:02} special{i:02}' for i in range(count)]
            for row in [*ordinary, *special]:
                files['schema/' + row.split()[0]] = 'data'
            body = 'RESOURCE(' + ' '.join(ordinary + special) + ')'
        files['schema/ya.make'] = 'PY3_LIBRARY()\nPY_SRCS(ordinary.py)\n' + body + '\nEND()\n'
    return files


def archive_members(graph):
    archive = lib.node_by_output(graph, PREFIX + 'libpy3schema.global.a')
    outputs = [a for a in archive['cmds'][0]['cmd_args'] if a.endswith('.o')]
    return archive, outputs


def labels(graph):
    _, outputs = archive_members(graph)
    result = []
    for output in outputs:
        node = lib.node_by_output(graph, output)
        words = node['cmds'][0]['cmd_args']
        if '.auxcpp.' in output and '_raw.auxcpp' not in output:
            result.append('aux')
        elif any(i.endswith('.yasm') for i in node['inputs']):
            result.append('asm')
        elif '_raw.auxcpp' in output:
            packed = lib.node_by_output(graph, next(i for i in node['inputs'] if i.endswith('.auxcpp')))
            result.append('extra' if any('extra__int' in i for i in packed['inputs']) else 'series')
        elif PREFIX + 'brought.txt' in node['inputs']:
            result.append('bundle')
        elif '$(S)/schema/note.dat' in node['inputs']:
            result.append('note')
        elif any('py/namespace/' in a for a in words):
            result.append('namespace')
        elif '$(S)/schema/ordinary.py' in node['inputs']:
            result.append('python')
        else:
            result.append('resource')
    return result


class ResourceActionPositionsTest(unittest.TestCase):
    def check_graph(self, graph):
        archive, outputs = archive_members(graph)
        self.assertTrue(graph['result'])
        self.assertTrue(outputs)
        for output in outputs:
            producer = lib.node_by_output(graph, output)
            self.assertIn(output, archive['inputs'])
            self.assertIn(producer['uid'], archive['deps'])
        uids = {n['uid'] for n in graph['graph']}
        for node in graph['graph']:
            self.assertTrue(set(node['deps']) <= uids)

    def test_multi_proto_call_precedes_following_single_proto_call(self):
        for count in (2, 30):
            with self.subTest(count=count):
                graph = lib.make(fixture('proto', count), 'app', '--target-platform', 'default-linux-x86_64')
                self.check_graph(graph)
                order = labels(graph)
                self.assertEqual(order[-1], 'extra')
                self.assertTrue(all(x == 'series' for x in order[:-1]))
                self.assertGreater(len(order), 1)

    def test_single_proto_call_keeps_its_position_before_multi_proto_call(self):
        graph = lib.make(fixture('proto', 2, reverse=True), 'app', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph)
        self.assertEqual(labels(graph), ['extra', 'series'])

    def test_bundle_ready_before_resource_preserves_resource_order(self):
        graph = lib.make(fixture('bundle'), 'schema', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph)
        self.assertEqual(labels(graph), ['bundle', 'note', 'namespace', 'python'])
        bundle = lib.node_by_output(graph, PREFIX + 'brought.txt')
        target = lib.node_by_output(graph, '$(B)/tools/writer/writer')
        self.assertIn(target['uid'], bundle['deps'])
        packed = lib.node_by_output(graph, archive_members(graph)[1][0])
        self.assertIn(bundle['uid'], packed['deps'])

    def test_bundle_declared_after_resource_keeps_pending_action(self):
        graph = lib.make(fixture('bundle', reverse=True), 'schema', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph)
        self.assertEqual(labels(graph), ['note', 'namespace', 'python', 'bundle'])

    def test_compressed_auxiliary_follows_objcopy_chunks_before_python(self):
        for count in (1, 80):
            with self.subTest(count=count):
                graph = lib.make(fixture('compressed', count), 'schema', '--target-platform', 'default-linux-x86_64')
                self.check_graph(graph)
                expected = ['resource'] * 3
                expected += ['namespace', 'python', 'aux', 'asm'] if count > 1 else ['aux', 'namespace', 'python']
                self.assertEqual(labels(graph), expected)
                aux = next(n for n in graph['graph'] if n['outputs'][0].startswith(PREFIX)
                           and n['outputs'][0].endswith('.auxcpp.o'))
                source = lib.node_by_output(graph, next(i for i in aux['inputs'] if i.endswith('.auxcpp')))
                self.assertIn(source['uid'], aux['deps'])
                if count > 1:
                    self.assertTrue(source['outputs'][0].startswith(PREFIX + 'join_'))
                    self.assertGreater(len([i for i in source['inputs'] if i.endswith('.auxcpp')]), 1)

    def test_proto_library_collects_joined_objects_when_their_commands_run(self):
        files = fixture('compressed', 80)
        files['schema/ya.make'] = files['schema/ya.make'].replace(
            'PY3_LIBRARY()', 'PROTO_LIBRARY()\nEXCLUDE_TAGS(CPP_PROTO)')
        graph = lib.make(files, 'app', '--target-platform', 'default-linux-x86_64')
        self.check_graph(graph)
        self.assertEqual(labels(graph), ['resource'] * 3 + ['namespace', 'python', 'aux', 'asm'])
        for output in archive_members(graph)[1][-2:]:
            self.assertTrue(output.endswith('.py3.o'), output)


if __name__ == '__main__':
    unittest.main(verbosity=2)
