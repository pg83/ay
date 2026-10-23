import collections
import unittest

import lib
from test_python_event_resource_groups import fixture, producer_payloads, resource_nodes


def sources(count, start=0):
    return [f'leaf_{i:04}.proto' for i in range(start, start + count)]


def files_for(calls, python2=False, grpc=True):
    body = ('GRPC()\n' if grpc else '') + ''.join('SRCS(' + ' '.join(call) + ')\n' for call in calls)
    files = fixture(body, python2=python2)
    for call in calls:
        for name in call:
            files['maple/' + name] = 'syntax = "proto2";\nmessage Item {}\n'
    return files


def items(graph):
    result = []
    for node in resource_nodes(graph):
        args = node['cmds'][0]['cmd_args'][2:]
        result.extend(zip(args[::2], args[1::2]))
    return collections.Counter(result)


class PythonProtoResourcePartitionsTest(unittest.TestCase):
    def check(self, count, python2=False, grpc=True):
        names = sources(count)
        rows_per_schema = 2 if grpc else 1
        schema_boundary = 1024 // rows_per_schema
        actual = lib.make(files_for([names], python2, grpc), 'reader')
        expected = lib.make(files_for([names[:schema_boundary], names[schema_boundary:]], python2, grpc), 'reader')
        self.assertEqual(items(actual), items(expected))
        self.assertEqual(producer_payloads(actual), producer_payloads(expected))
        self.assertEqual(actual['result'], expected['result'])
        resources = resource_nodes(actual)
        row_ids = set()
        for node in resources:
            files = [p for p in node['inputs'] if '__intpy' in p]
            self.assertTrue(files)
            batches = {int(p.rsplit('leaf_', 1)[1][:4]) // schema_boundary for p in files}
            self.assertLessEqual(len(batches), 1)
            row_ids.update(int(p.rsplit('leaf_', 1)[1][:4]) for p in files)
            cc = lib.node_by_output(actual, node['outputs'][0] + ('.py2.o' if python2 else '.py3.o'))
            self.assertIn(node['uid'], cc['deps'])
            for path in files:
                producer = lib.node_by_output(actual, path)
                self.assertIn(producer['uid'], node['deps'])
        self.assertEqual(row_ids, set(range(count)))
        self.assertEqual(sum(items(actual).values()), count * rows_per_schema * (3 if python2 else 4))
        return actual

    def test_grpc_generated_source_rows_reset_the_outer_batch(self):
        for python2 in (False, True):
            self.check(513, python2)

    def test_exact_boundary_does_not_add_an_empty_batch(self):
        graph = self.check(512)
        self.assertTrue(resource_nodes(graph))

    def test_ordinary_proto_counts_each_generated_python_module(self):
        self.check(1025, grpc=False)

    def test_small_separate_callbacks_keep_their_partial_batches(self):
        calls = [sources(2), sources(2, 2)]
        graph = lib.make(files_for(calls), 'reader')
        self.assertEqual(len(resource_nodes(graph)), 2)
        for node in resource_nodes(graph):
            ids = {int(p.rsplit('leaf_', 1)[1][:4]) // 2 for p in node['inputs'] if '__intpy' in p}
            self.assertEqual(len(ids), 1)


if __name__ == '__main__':
    unittest.main()
